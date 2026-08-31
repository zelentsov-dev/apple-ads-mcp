//go:build live_write

package live

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestMCPV036PausedKeywordBulk2And28(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_WRITE") != "V036_PAUSED_BULK_2_28" {
		t.Skip("v0.3.6 bulk acceptance requires an explicit mutation confirmation")
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
	accountID := strings.TrimSpace(os.Getenv("APPLE_ADS_AD_ACCOUNT_ID"))
	adGroupID := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_AD_GROUP_ID"))
	if accountID == "" || adGroupID == "" {
		t.Fatal("v0.3.6 bulk acceptance requires an ad account and an existing PAUSED Search Results ad group")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPV02WriteHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_WRITE_HELPER=1", "APPLE_ADS_ALLOW_WRITES=true")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-v036-bulk-test", Version: "0.3.6"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	account := map[string]any{"profile": profile.Name, "adAccountId": accountID}
	adGroup := callTool(t, ctx, session, "ad_group_get", mergeAccount(account, map[string]any{"adGroupId": adGroupID}))
	requireFieldValue(t, adGroup, "status", "PAUSED")
	campaignID, ok := findStringField(adGroup, "campaignId")
	if !ok || campaignID == "" {
		t.Fatal("the selected ad group did not return campaignId")
	}
	campaign := callTool(t, ctx, session, "campaign_get", mergeAccount(account, map[string]any{"campaignId": campaignID}))
	requireFieldValue(t, campaign, "status", "PAUSED")
	accountState := callTool(t, ctx, session, "ad_account_get", account)
	currency, ok := findStringField(accountState, "currency")
	if !ok || len(currency) != 3 {
		t.Fatal("the selected ad account did not return a currency")
	}
	currency = strings.ToUpper(currency)
	bid := decimalEnv(t, "APPLE_ADS_LIVE_DEFAULT_BID", "0.25", "5.00")
	prefix := fmt.Sprintf("mcp036-%d", time.Now().UTC().Unix())
	createdTexts := make([]string, 0, 30)
	for _, fixture := range []struct {
		count   int
		withBid bool
	}{{count: 2}, {count: 28, withBid: true}} {
		items := make([]any, 0, fixture.count)
		for index := 0; index < fixture.count; index++ {
			text := fmt.Sprintf("%s-%02d-%02d", prefix, fixture.count, index)
			if index == fixture.count-1 {
				text = fmt.Sprintf("音声メモ-%s-%02d", prefix, fixture.count)
			}
			item := map[string]any{
				"correlationId": index, "text": text, "matchType": "EXACT", "status": "ENABLED",
			}
			if fixture.withBid {
				item["bid"] = map[string]any{"amount": bid, "currency": currency}
			}
			items = append(items, item)
			createdTexts = append(createdTexts, text)
		}
		preview := callTool(t, ctx, session, "keywords_bulk_create_preview", mergeAccount(account, map[string]any{
			"adGroupId": adGroupID, "items": items,
		}))
		receipt, ok := previewReceipt(preview)
		if !ok || receipt == "" {
			t.Fatalf("%d-item preview did not return one receipt", fixture.count)
		}
		applied := callTool(t, ctx, session, "operations_apply", map[string]any{"receipt": receipt})
		receiptObject, ok := objectField(applied, "receipt")
		if !ok {
			t.Fatalf("%d-item apply did not return a receipt object", fixture.count)
		}
		status, _ := findStringField(receiptObject, "status")
		if status != "applied" {
			t.Fatalf("%d-item bulk returned status %q", fixture.count, status)
		}
		createdIDs := v036CollectStringFields(receiptObject, "targetId")
		if len(createdIDs) != fixture.count {
			t.Fatalf("%d-item bulk retained %d created IDs: %v", fixture.count, len(createdIDs), createdIDs)
		}
		t.Logf("%d-item bulk created IDs: %s", fixture.count, strings.Join(createdIDs, ","))
		verification := callTool(t, ctx, session, "operations_verify", map[string]any{"receipt": receipt})
		if !containsFieldValue(verification, "status", "verified") {
			t.Fatalf("%d-item bulk did not verify every returned object ID", fixture.count)
		}
	}

	keywords := callTool(t, ctx, session, "keywords_query", mergeAccount(account, map[string]any{
		"campaignId": campaignID, "adGroupId": adGroupID, "pagination": map[string]any{"offset": 0, "pageSize": 200},
	}))
	for _, text := range createdTexts {
		if !containsFieldValue(keywords, "text", text) {
			t.Fatalf("bulk readback did not contain %q", text)
		}
	}
	requireFieldValue(t, callTool(t, ctx, session, "ad_group_get", mergeAccount(account, map[string]any{"adGroupId": adGroupID})), "status", "PAUSED")
	requireFieldValue(t, callTool(t, ctx, session, "campaign_get", mergeAccount(account, map[string]any{"campaignId": campaignID})), "status", "PAUSED")
}

func v036CollectStringFields(value any, field string) []string {
	result := make([]string, 0)
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[field]; ok {
			if scalar, ok := scalarString(current); ok && scalar != "" {
				result = append(result, scalar)
			}
		}
		for _, item := range typed {
			result = append(result, v036CollectStringFields(item, field)...)
		}
	case []any:
		for _, item := range typed {
			result = append(result, v036CollectStringFields(item, field)...)
		}
	}
	return result
}
