//go:build live_read_only

package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestMCPV036ReadOnlyP2Acceptance(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_V036_READ_ONLY") != "P2_MATRIX" {
		t.Skip("v0.3.6 P2 acceptance requires an explicit opt-in")
	}
	cfg, _, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := cfg.ResolveProfile(os.Getenv("APPLE_ADS_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := strings.TrimSpace(os.Getenv("APPLE_ADS_AD_ACCOUNT_ID"))
	adamID := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_ADAM_ID"))
	storefront := strings.ToUpper(strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_STOREFRONT")))
	unicodeQuery := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_UNICODE_APP_QUERY"))
	unicodeAdamID := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_UNICODE_APP_ADAM_ID"))
	languageCode := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_LANGUAGE_CODE"))
	adGroupID := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_AD_GROUP_ID"))
	if storefront == "" {
		storefront = "US"
	}
	if accountID == "" || adamID == "" || unicodeQuery == "" || languageCode == "" || adGroupID == "" {
		t.Fatal("v0.3.6 P2 acceptance requires account, Adam ID, Unicode app query, language code, and ad group ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPReadOnlyHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_HELPER=1", "APPLE_ADS_ALLOW_WRITES=false")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-v036-read-only-test", Version: "0.3.6"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	account := map[string]any{"profile": profile.Name, "adAccountId": accountID}
	adGroup := callV036ReadTool(t, ctx, session, "ad_group_get", mergeV036(account, map[string]any{"adGroupId": adGroupID}), false)
	campaignID, ok := v036FindStringField(adGroup, "campaignId")
	if !ok || campaignID == "" {
		t.Fatal("the selected ad group did not return campaignId")
	}
	search := callV036ReadTool(t, ctx, session, "apps_search", mergeV036(account, map[string]any{
		"query": unicodeQuery, "returnOwnedApps": true, "storefronts": []string{storefront}, "limit": 50,
	}), false)
	if unicodeAdamID != "" {
		if !containsFieldValue(search, "adamId", unicodeAdamID) {
			t.Fatalf("exact Unicode owned-app lookup did not return Adam ID %s", unicodeAdamID)
		}
	} else {
		if !v036ResultDataEmpty(search) {
			t.Fatalf("Unicode owned-app lookup without an expected Adam ID returned an inexact match: %v", search)
		}
		if !strings.Contains(v036Summary(search), "no exact Unicode name match was found in bounded owned-app inventory") {
			t.Fatalf("Unicode owned-app lookup did not exercise the bounded exact-match fallback: %v", search)
		}
	}
	callV036ReadTool(t, ctx, session, "app_locale_details", mergeV036(account, map[string]any{"adamId": adamID}), false)
	callV036ReadTool(t, ctx, session, "app_locale_details", mergeV036(account, map[string]any{
		"adamId": adamID, "languageCode": languageCode, "includeAssets": true,
	}), false)
	callV036ReadTool(t, ctx, session, "keywords_query", mergeV036(account, map[string]any{"adGroupId": adGroupID}), false)
	callV036ReadTool(t, ctx, session, "negative_keywords_query", mergeV036(account, map[string]any{"adGroupId": adGroupID}), false)
	callV036ReadTool(t, ctx, session, "ads_query", mergeV036(account, map[string]any{"adGroupId": adGroupID}), false)

	end := time.Now().UTC().Add(-24 * time.Hour)
	start := end.AddDate(0, 0, -6)
	window := map[string]any{"start": start.Format("2006-01-02"), "end": end.Format("2006-01-02")}
	callV036ReadTool(t, ctx, session, "keyword_report", mergeV036(account, map[string]any{
		"includeZeroMetrics": true,
		"filters": []any{
			map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID},
			map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID},
		},
		"timeRange": map[string]any{
			"start": window["start"], "end": window["end"], "timeZone": "UTC", "granularity": "DAILY",
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	}), false)
	callV036ReadTool(t, ctx, session, "change_history", mergeV036(account, window), false)
	impression := mergeV036(account, map[string]any{
		"adamId": adamID, "country": storefront, "start": window["start"], "end": window["end"],
		"granularity": "DAILY", "reportType": "ALL_SLOTS",
	})
	callV036ReadTool(t, ctx, session, "impression_share", impression, true)
	opportunities := callV036ReadTool(t, ctx, session, "app_opportunities", mergeV036(account, map[string]any{
		"adamId": adamID, "countriesOrRegions": []string{storefront},
	}), false)
	for _, section := range []string{"eligibility", "keywords", "phrases", "categories", "target-cpas"} {
		if status := v036SectionStatus(opportunities, section); status != "ok" && status != "empty" && status != "upstream_error" {
			t.Fatalf("app_opportunities section %s has invalid status %q", section, status)
		}
	}
}

func callV036ReadTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any, allowAppleError bool) any {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.StructuredContent == nil {
		t.Fatalf("%s returned no structured content", name)
	}
	value := normalizeV036(result.StructuredContent)
	if !result.IsError {
		return value
	}
	if !allowAppleError || !containsFieldValue(value, "type", "apple_api_error") {
		t.Fatalf("%s returned an unexpected error: %v", name, value)
	}
	status, ok := findV036Number(value, "httpStatus")
	if !ok || status < 400 {
		t.Fatalf("%s returned an invalid Apple diagnostic: %v", name, value)
	}
	return value
}

func mergeV036(base, extra map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range extra {
		result[key] = value
	}
	return result
}

func normalizeV036(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return value
	}
	return result
}

func findV036Number(value any, field string) (float64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if number, ok := typed[field].(float64); ok {
			return number, true
		}
		for _, item := range typed {
			if number, ok := findV036Number(item, field); ok {
				return number, true
			}
		}
	case []any:
		for _, item := range typed {
			if number, ok := findV036Number(item, field); ok {
				return number, true
			}
		}
	}
	return 0, false
}

func v036SectionStatus(value any, section string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if data, ok := object["data"].(map[string]any); ok {
		object = data
	}
	item, ok := object[section].(map[string]any)
	if !ok {
		return ""
	}
	return fmt.Sprint(item["status"])
}

func v036Summary(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return fmt.Sprint(object["summary"])
}

func v036ResultDataEmpty(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	switch data := object["data"].(type) {
	case nil:
		return true
	case []any:
		return len(data) == 0
	case map[string]any:
		return len(data) == 0
	default:
		return false
	}
}

func v036FindStringField(value any, field string) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if result, ok := typed[field].(string); ok {
			return result, true
		}
		for _, item := range typed {
			if result, ok := v036FindStringField(item, field); ok {
				return result, true
			}
		}
	case []any:
		for _, item := range typed {
			if result, ok := v036FindStringField(item, field); ok {
				return result, true
			}
		}
	}
	return "", false
}
