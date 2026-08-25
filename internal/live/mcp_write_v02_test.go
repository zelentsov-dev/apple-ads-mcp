//go:build live_write

package live

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/mcpserver"
)

type placementFixture struct {
	placement string
	label     string
	campaign  string
	adGroup   string
}

func TestMCPV02PausedFixtures(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_WRITE") != "CREATE_PAUSED_FIXTURES" {
		t.Skip("live write acceptance requires an explicit fixture confirmation")
	}
	if !strings.EqualFold(os.Getenv("APPLE_ADS_ALLOW_WRITES"), "true") {
		t.Fatal("APPLE_ADS_ALLOW_WRITES=true is required")
	}
	cfg, _, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := cfg.ResolveProfile(os.Getenv("APPLE_ADS_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	if !profile.AllowWrites {
		t.Fatal("the session-resolved profile must allow writes")
	}
	accountID := os.Getenv("APPLE_ADS_AD_ACCOUNT_ID")
	adamID := os.Getenv("APPLE_ADS_LIVE_ADAM_ID")
	if accountID == "" || adamID == "" {
		t.Fatal("APPLE_ADS_AD_ACCOUNT_ID and APPLE_ADS_LIVE_ADAM_ID are required")
	}
	storefront := strings.ToUpper(os.Getenv("APPLE_ADS_LIVE_STOREFRONT"))
	if storefront == "" {
		storefront = "US"
	}
	dailyBudget := decimalEnv(t, "APPLE_ADS_LIVE_DAILY_BUDGET", "5.00", "20.00")
	defaultBid := decimalEnv(t, "APPLE_ADS_LIVE_DEFAULT_BID", "0.25", "5.00")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPV02WriteHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_WRITE_HELPER=1", "APPLE_ADS_ALLOW_WRITES=true")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-v02-write-test", Version: "0.2.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	requireToolInventory(t, ctx, session)
	accountArgs := map[string]any{"profile": profile.Name, "adAccountId": accountID}
	health := callTool(t, ctx, session, "account_health", map[string]any{"profile": profile.Name, "adAccountId": accountID, "adamId": adamID})
	if ready, ok := findBoolField(health, "ready"); !ok || !ready {
		readiness, _ := findObjectField(health, "readiness")
		t.Fatalf("selected account is not ready for App Store campaign acceptance: %v", readiness)
	}
	account := callTool(t, ctx, session, "ad_account_get", accountArgs)
	currency, ok := findStringField(account, "currency")
	if !ok || len(currency) != 3 {
		t.Fatal("selected ad account did not return a currency")
	}
	currency = strings.ToUpper(currency)

	date := time.Now().UTC().Format("2006-01-02")
	fixtures := []placementFixture{
		{placement: "APPSTORE_SEARCH_RESULTS", label: "Search Results"},
		{placement: "APPSTORE_SEARCH_TAB", label: "Search Tab"},
		{placement: "APPSTORE_TODAY_TAB", label: "Today Tab"},
		{placement: "APPSTORE_PRODUCT_PAGES", label: "Product Pages"},
	}
	created := make([]placementFixture, 0, len(fixtures))
	for _, fixture := range fixtures {
		if !placementEligible(t, ctx, session, profile.Name, accountID, adamID, storefront, fixture.placement) {
			t.Logf("%s: not_eligible", fixture.label)
			continue
		}
		name := fmt.Sprintf("Apple Ads MCP v0.2 Validation - %s - %s", fixture.label, date)
		campaignPreview := map[string]any{
			"profile": profile.Name, "adAccountId": accountID,
			"payload": map[string]any{
				"name": name, "billingEvent": "TAPS", "promotedObjectType": "APPSTORE_APP", "promotedObjectId": adamID, "status": "PAUSED",
				"dailyBudget": map[string]any{"value": map[string]any{"amount": dailyBudget, "currency": currency}},
				"targeting": map[string]any{
					"supplyPlacement": map[string]any{"include": []string{fixture.placement}},
					"countryOrRegion": map[string]any{"include": []string{storefront}},
				},
				"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP"},
			},
		}
		campaignID := findResourceIDByField(callTool(t, ctx, session, "campaigns_query", map[string]any{
			"profile": profile.Name, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "name", "operator": "EQUALS", "value": name}},
			"pagination": map[string]any{"offset": 0, "pageSize": 200},
		}), "name", name)
		if campaignID == "" {
			campaignID = previewApplyCreate(t, ctx, session, "campaign_create_preview", campaignPreview)
		}
		campaign := callTool(t, ctx, session, "campaign_get", map[string]any{"profile": profile.Name, "adAccountId": accountID, "id": campaignID})
		requireFieldValue(t, campaign, "status", "PAUSED")
		requireFieldValue(t, campaign, "name", name)
		requireFieldValue(t, campaign, "promotedObjectId", adamID)

		adGroupName := fmt.Sprintf("Validation Ad Group - %s", fixture.label)
		adGroupID := findResourceIDByField(callTool(t, ctx, session, "ad_groups_query", map[string]any{
			"profile": profile.Name, "adAccountId": accountID,
			"filters": []any{
				map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID},
				map[string]any{"field": "name", "operator": "EQUALS", "value": adGroupName},
			},
			"pagination": map[string]any{"offset": 0, "pageSize": 200},
		}), "name", adGroupName)
		if adGroupID == "" {
			adGroupID = previewApplyCreate(t, ctx, session, "ad_group_create_preview", map[string]any{
				"profile": profile.Name, "adAccountId": accountID,
				"payload": map[string]any{
					"name": adGroupName, "campaignId": campaignID,
					"startTime":    time.Now().UTC().Add(5 * time.Minute).Format("2006-01-02T15:04:05.000"),
					"pricingModel": "CPT", "automatedKeywordsOptIn": false, "status": "PAUSED",
					"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP", "bid": map[string]any{"amount": defaultBid, "currency": currency}},
				},
			})
		}
		adGroup := callTool(t, ctx, session, "ad_group_get", map[string]any{"profile": profile.Name, "adAccountId": accountID, "id": adGroupID})
		requireFieldValue(t, adGroup, "status", "PAUSED")
		fixture.campaign = campaignID
		fixture.adGroup = adGroupID
		created = append(created, fixture)
	}
	if len(created) == 0 {
		t.Fatal("Apple returned no eligible App Store placement for the fixture app and storefront")
	}

	for _, fixture := range created {
		if fixture.placement == "APPSTORE_SEARCH_RESULTS" {
			runSearchResultsBulkAcceptance(t, ctx, session, profile.Name, accountID, fixture.adGroup, currency, defaultBid, date)
		}
	}

	creativeID := createCreative(t, ctx, session, profile.Name, accountID, adamID, "DEFAULT_PRODUCT_PAGE", "", date)
	if waitForCreative(t, ctx, session, profile.Name, accountID, creativeID) {
		for _, fixture := range created {
			createAdWhenEligible(t, ctx, session, profile.Name, accountID, fixture, creativeID, "Default Product Page", date)
		}
	}

	pages := callTool(t, ctx, session, "product_pages_query", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	if productPageID := findProductPageID(pages, adamID); productPageID != "" {
		customCreativeID := createCreative(t, ctx, session, profile.Name, accountID, adamID, "CUSTOM_PRODUCT_PAGE", productPageID, date)
		if waitForCreative(t, ctx, session, profile.Name, accountID, customCreativeID) {
			createAdWhenEligible(t, ctx, session, profile.Name, accountID, created[0], customCreativeID, "Custom Product Page", date)
		}
	}

	for _, fixture := range created {
		inventory := callTool(t, ctx, session, "campaign_inventory", map[string]any{
			"profile": profile.Name, "adAccountId": accountID, "campaignId": fixture.campaign, "pageSize": 200,
		})
		requireFieldValue(t, inventory, "status", "PAUSED")
		if containsFieldValue(inventory, "status", "ENABLED") {
			t.Fatal("a PAUSED acceptance fixture contains an enabled delivery object")
		}
		report := callTool(t, ctx, session, "campaign_report", map[string]any{
			"profile": profile.Name, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "id", "operator": "EQUALS", "value": fixture.campaign}},
			"fields":     []string{"id", "name", "status", "localSpend"},
			"timeRange":  map[string]any{"start": date, "end": date, "timeZone": "UTC", "granularity": "DAILY"},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
		if hasNonZeroMoneyField(report, "localSpend") {
			t.Fatal("a PAUSED acceptance fixture returned non-zero spend")
		}
	}
}

func TestMCPV02WriteHelper(t *testing.T) {
	if os.Getenv("APPLE_ADS_MCP_LIVE_WRITE_HELPER") != "1" {
		return
	}
	cfg, source, err := config.Load("")
	if err != nil {
		os.Exit(2)
	}
	if err := mcpserver.RunStdio(context.Background(), appleads.NewManager(cfg, source), true); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func requireToolInventory(t *testing.T, ctx context.Context, session *mcp.ClientSession) {
	t.Helper()
	required := map[string]bool{
		"account_health": false, "campaigns_query": false, "campaign_inventory": false,
		"campaign_create_preview": false, "ad_group_create_preview": false,
		"keywords_bulk_create_preview": false, "negative_keywords_bulk_create_preview": false,
		"creative_create_preview": false, "ad_create_preview": false,
		"operations_inspect": false, "operations_apply": false, "operations_verify": false,
	}
	params := &mcp.ListToolsParams{}
	for {
		result, err := session.ListTools(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range result.Tools {
			if _, ok := required[tool.Name]; ok {
				required[tool.Name] = true
			}
		}
		if result.NextCursor == "" {
			break
		}
		params.Cursor = result.NextCursor
	}
	for name, found := range required {
		if !found {
			t.Fatalf("tools/list did not expose %s", name)
		}
	}
}

func placementEligible(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, adamID, storefront, placement string) bool {
	t.Helper()
	result := callTool(t, ctx, session, "apps_eligibility", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamID},
			map[string]any{"field": "supplyPlacement", "operator": "EQUALS", "value": placement},
			map[string]any{"field": "countryOrRegion", "operator": "EQUALS", "value": storefront},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	return containsEligibility(result, adamID, storefront, placement, "ELIGIBLE")
}

func runSearchResultsBulkAcceptance(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, adGroupID, currency, bid, date string) {
	t.Helper()
	correlation := time.Now().UnixNano()
	keywords := callTool(t, ctx, session, "keywords_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	keywordItems := make([]any, 0, 2)
	for index, text := range []string{"voisia", "voice transcription"} {
		if !containsFieldValue(keywords, "text", text) {
			keywordItems = append(keywordItems, map[string]any{
				"correlationId": strconv.FormatInt(correlation+int64(index), 10), "text": text, "matchType": "EXACT",
				"bid": map[string]any{"amount": bid, "currency": currency}, "status": "PAUSED",
			})
		}
	}
	if len(keywordItems) > 0 {
		previewApply(t, ctx, session, "keywords_bulk_create_preview", map[string]any{
			"profile": profile, "adAccountId": accountID, "adGroupId": adGroupID, "items": keywordItems,
		})
		keywords = callTool(t, ctx, session, "keywords_query", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID}},
			"pagination": map[string]any{"offset": 0, "pageSize": 200},
		})
	}
	if !containsFieldValue(keywords, "text", "voisia") {
		t.Fatal("bulk targeting keyword readback failed")
	}
	negatives := callTool(t, ctx, session, "negative_keywords_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	negativeText := "validation exclusion " + date
	if !containsFieldValue(negatives, "text", negativeText) {
		previewApply(t, ctx, session, "negative_keywords_bulk_create_preview", map[string]any{
			"profile": profile, "adAccountId": accountID, "adGroupId": adGroupID,
			"items": []any{
				map[string]any{"correlationId": strconv.FormatInt(correlation+2, 10), "text": negativeText, "matchType": "EXACT", "status": "PAUSED"},
			},
		})
		negatives = callTool(t, ctx, session, "negative_keywords_query", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID}},
			"pagination": map[string]any{"offset": 0, "pageSize": 200},
		})
	}
	if !containsFieldValue(negatives, "text", negativeText) {
		t.Fatal("bulk negative keyword readback failed")
	}
}

func createCreative(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, adamID, creativeType, productPageID, date string) string {
	t.Helper()
	creatives := callTool(t, ctx, session, "creatives_query", map[string]any{
		"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	if creativeID := findCreativeID(creatives, adamID, creativeType, productPageID); creativeID != "" {
		return creativeID
	}
	parameters := map[string]any{"adamId": adamID}
	label := "Default Product Page"
	if productPageID != "" {
		parameters["productPageId"] = productPageID
		label = "Custom Product Page"
	}
	return previewApplyCreate(t, ctx, session, "creative_create_preview", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"payload": map[string]any{
			"name":         fmt.Sprintf("Apple Ads MCP v0.2 Validation - %s - %s", label, date),
			"creativeType": creativeType, "creativeSpec": map[string]any{},
			"destination": map[string]any{"destinationType": "APP_STORE_PRODUCT_PAGE", "parameters": parameters},
		},
	})
}

func waitForCreative(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, creativeID string) bool {
	t.Helper()
	for attempt := 0; attempt < 12; attempt++ {
		creative := callTool(t, ctx, session, "creative_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": creativeID})
		status, _ := findStringField(creative, "systemStatus")
		if status == "VALID" {
			return true
		}
		if status == "INVALID" || status == "REJECTED" {
			return false
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
	return false
}

func createAdWhenEligible(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID string, fixture placementFixture, creativeID, label, date string) {
	t.Helper()
	ads := callTool(t, ctx, session, "ads_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": fixture.adGroup},
			map[string]any{"field": "creativeId", "operator": "EQUALS", "value": creativeID},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	if adID := findResourceIDByFields(ads, map[string]string{"adGroupId": fixture.adGroup, "creativeId": creativeID}); adID != "" {
		ad := callTool(t, ctx, session, "ad_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": adID})
		requireFieldValue(t, ad, "status", "PAUSED")
		return
	}
	preview, errText := callToolMaybeError(ctx, session, "ad_create_preview", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"payload": map[string]any{
			"adGroupId": fixture.adGroup, "creativeId": creativeID,
			"name": fmt.Sprintf("Apple Ads MCP v0.2 Validation - %s - %s - %s", label, fixture.label, date), "status": "PAUSED",
		},
	})
	if errText != "" {
		lower := strings.ToLower(errText)
		if strings.Contains(lower, "not eligible") || strings.Contains(lower, "blocked") || strings.Contains(lower, "not supported") {
			t.Logf("%s / %s: not_eligible", fixture.label, label)
			return
		}
		t.Fatalf("ad_create_preview failed: %s", errText)
	}
	receipt, ok := previewReceipt(preview)
	if !ok {
		t.Fatal("ad preview did not return a receipt")
	}
	adID, applyErr := applyReceiptCreateMaybe(ctx, session, receipt)
	if applyErr != "" {
		lower := strings.ToLower(applyErr)
		if strings.Contains(lower, "not eligible") || strings.Contains(lower, "blocked") || strings.Contains(lower, "not supported") {
			t.Logf("%s / %s: not_eligible", fixture.label, label)
			return
		}
		t.Fatalf("ad operations_apply failed: %s", applyErr)
	}
	ad := callTool(t, ctx, session, "ad_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": adID})
	requireFieldValue(t, ad, "status", "PAUSED")
}

func previewApplyCreate(t *testing.T, ctx context.Context, session *mcp.ClientSession, tool string, arguments map[string]any) string {
	t.Helper()
	preview := callTool(t, ctx, session, tool, arguments)
	receipt, ok := previewReceipt(preview)
	if !ok {
		t.Fatalf("%s did not return a receipt", tool)
	}
	return applyReceiptCreate(t, ctx, session, receipt)
}

func applyReceiptCreate(t *testing.T, ctx context.Context, session *mcp.ClientSession, receipt string) string {
	t.Helper()
	id, errText := applyReceiptCreateMaybe(ctx, session, receipt)
	if errText != "" {
		t.Fatalf("operations_apply failed: %s", errText)
	}
	return id
}

func applyReceiptCreateMaybe(ctx context.Context, session *mcp.ClientSession, receipt string) (string, string) {
	if _, errText := callToolMaybeError(ctx, session, "operations_inspect", map[string]any{"receipt": receipt}); errText != "" {
		return "", errText
	}
	applied, errText := callToolMaybeError(ctx, session, "operations_apply", map[string]any{"receipt": receipt})
	if errText != "" {
		return "", errText
	}
	receiptObject, ok := objectField(applied, "receipt")
	if !ok {
		return "", "operation apply did not return a receipt object"
	}
	status, ok := findStringField(receiptObject, "status")
	if !ok || (status != "applied" && status != "partial") {
		return "", fmt.Sprintf("operation apply returned status %q", status)
	}
	resultObject, ok := objectField(receiptObject, "result")
	if !ok {
		return "", "create operation did not return a result object"
	}
	id, ok := findStringField(resultObject, "id")
	if !ok || id == "" {
		return "", "create operation did not return a resource ID"
	}
	if _, errText := callToolMaybeError(ctx, session, "operations_verify", map[string]any{"receipt": receipt}); errText != "" {
		return "", errText
	}
	return id, ""
}

func previewApply(t *testing.T, ctx context.Context, session *mcp.ClientSession, tool string, arguments map[string]any) {
	t.Helper()
	preview := callTool(t, ctx, session, tool, arguments)
	receipt, ok := previewReceipt(preview)
	if !ok {
		t.Fatalf("%s did not return a receipt", tool)
	}
	callTool(t, ctx, session, "operations_inspect", map[string]any{"receipt": receipt})
	applied := callTool(t, ctx, session, "operations_apply", map[string]any{"receipt": receipt})
	receiptObject, ok := objectField(applied, "receipt")
	if !ok {
		t.Fatalf("%s apply did not return a receipt object", tool)
	}
	status, ok := findStringField(receiptObject, "status")
	if !ok || (status != "applied" && status != "partial") {
		t.Fatalf("%s apply returned status %q", tool, status)
	}
	callTool(t, ctx, session, "operations_verify", map[string]any{"receipt": receipt})
}

func callTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) any {
	t.Helper()
	result, errText := callToolMaybeError(ctx, session, name, arguments)
	if errText != "" {
		t.Fatalf("%s failed: %s", name, errText)
	}
	return result
}

func callToolMaybeError(ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) (any, string) {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, err.Error()
	}
	if result.IsError {
		if result.StructuredContent != nil {
			if data, marshalErr := json.Marshal(normalizeJSON(result.StructuredContent)); marshalErr == nil {
				return nil, string(data)
			}
		}
		parts := make([]string, 0, len(result.Content))
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				parts = append(parts, text.Text)
			}
		}
		return nil, strings.Join(parts, "; ")
	}
	if result.StructuredContent == nil {
		return nil, "missing structuredContent"
	}
	return normalizeJSON(result.StructuredContent), ""
}

func normalizeJSON(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return value
	}
	return normalized
}

func previewReceipt(value any) (string, bool) {
	preview, ok := objectField(value, "preview")
	if !ok {
		return "", false
	}
	return findStringField(preview, "receipt")
}

func objectField(value any, field string) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	item, ok := object[field].(map[string]any)
	return item, ok
}

func findObjectField(value any, field string) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[field].(map[string]any); ok {
			return item, true
		}
		for _, item := range typed {
			if found, ok := findObjectField(item, field); ok {
				return found, true
			}
		}
	case []any:
		for _, item := range typed {
			if found, ok := findObjectField(item, field); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func findStringField(value any, field string) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[field]; ok {
			switch scalar := item.(type) {
			case string:
				return scalar, true
			case json.Number:
				return scalar.String(), true
			}
		}
		for _, item := range typed {
			if found, ok := findStringField(item, field); ok {
				return found, true
			}
		}
	case []any:
		for _, item := range typed {
			if found, ok := findStringField(item, field); ok {
				return found, true
			}
		}
	}
	return "", false
}

func findBoolField(value any, field string) (bool, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[field].(bool); ok {
			return item, true
		}
		for _, item := range typed {
			if found, ok := findBoolField(item, field); ok {
				return found, true
			}
		}
	case []any:
		for _, item := range typed {
			if found, ok := findBoolField(item, field); ok {
				return found, true
			}
		}
	}
	return false, false
}

func containsFieldValue(value any, field, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[field]; ok && fmt.Sprint(current) == expected {
			return true
		}
		for _, item := range typed {
			if containsFieldValue(item, field, expected) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsFieldValue(item, field, expected) {
				return true
			}
		}
	}
	return false
}

func containsEligibility(value any, adamID, storefront, placement, state string) bool {
	switch typed := value.(type) {
	case map[string]any:
		id, idOK := scalarString(typed["adamId"])
		country, countryOK := scalarString(typed["countryOrRegion"])
		currentPlacement, placementOK := scalarString(typed["supplyPlacement"])
		currentState, stateOK := scalarString(typed["state"])
		if idOK && countryOK && placementOK && stateOK && id == adamID && strings.EqualFold(country, storefront) && currentPlacement == placement && currentState == state {
			return true
		}
		for _, item := range typed {
			if containsEligibility(item, adamID, storefront, placement, state) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsEligibility(item, adamID, storefront, placement, state) {
				return true
			}
		}
	}
	return false
}

func scalarString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case json.Number:
		return typed.String(), true
	default:
		return "", false
	}
}

func requireFieldValue(t *testing.T, value any, field, expected string) {
	t.Helper()
	if !containsFieldValue(value, field, expected) {
		t.Fatalf("readback did not contain %s=%s", field, expected)
	}
}

func findProductPageID(value any, adamID string) string {
	switch typed := value.(type) {
	case map[string]any:
		currentAdamID, adamOK := scalarString(typed["adamId"])
		id, idOK := scalarString(typed["id"])
		if adamOK && idOK && currentAdamID == adamID && id != "" {
			return id
		}
		for _, item := range typed {
			if found := findProductPageID(item, adamID); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findProductPageID(item, adamID); found != "" {
				return found
			}
		}
	}
	return ""
}

func findResourceIDByField(value any, field, expected string) string {
	return findResourceIDByFields(value, map[string]string{field: expected})
}

func findResourceIDByFields(value any, expected map[string]string) string {
	switch typed := value.(type) {
	case map[string]any:
		matches := true
		for field, value := range expected {
			current, ok := scalarString(typed[field])
			if !ok || current != value {
				matches = false
				break
			}
		}
		if matches {
			if id, ok := scalarString(typed["id"]); ok {
				return id
			}
		}
		for _, item := range typed {
			if found := findResourceIDByFields(item, expected); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findResourceIDByFields(item, expected); found != "" {
				return found
			}
		}
	}
	return ""
}

func findCreativeID(value any, adamID, creativeType, productPageID string) string {
	switch typed := value.(type) {
	case map[string]any:
		currentType, typeOK := scalarString(typed["creativeType"])
		if currentType == "" {
			currentType, typeOK = scalarString(typed["type"])
		}
		currentAdamID, adamOK := findStringField(typed["destination"], "adamId")
		currentProductPageID, _ := findStringField(typed["destination"], "productPageId")
		if typeOK && adamOK && currentType == creativeType && currentAdamID == adamID && currentProductPageID == productPageID {
			if id, ok := scalarString(typed["id"]); ok {
				return id
			}
		}
		for _, item := range typed {
			if found := findCreativeID(item, adamID, creativeType, productPageID); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findCreativeID(item, adamID, creativeType, productPageID); found != "" {
				return found
			}
		}
	}
	return ""
}

func hasNonZeroMoneyField(value any, field string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if money, ok := typed[field]; ok {
			if amount, found := findStringField(money, "amount"); found {
				parsed, valid := new(big.Rat).SetString(amount)
				return !valid || parsed.Sign() != 0
			}
		}
		for _, item := range typed {
			if hasNonZeroMoneyField(item, field) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if hasNonZeroMoneyField(item, field) {
				return true
			}
		}
	}
	return false
}

func decimalEnv(t *testing.T, name, fallback, maximum string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		value = fallback
	}
	parsed, ok := new(big.Rat).SetString(value)
	if !ok || parsed.Sign() <= 0 {
		t.Fatalf("%s must be a positive decimal", name)
	}
	cap, _ := new(big.Rat).SetString(maximum)
	if parsed.Cmp(cap) > 0 {
		t.Fatalf("%s exceeds the fixture safety maximum", name)
	}
	return value
}
