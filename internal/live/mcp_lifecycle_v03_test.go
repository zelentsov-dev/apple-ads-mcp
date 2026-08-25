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

func TestMCPV03PausedLifecycleAcceptance(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_WRITE") != "V03_PAUSED_LIFECYCLE" {
		t.Skip("v0.3 lifecycle acceptance requires an explicit fixture confirmation")
	}
	if !strings.EqualFold(os.Getenv("APPLE_ADS_ALLOW_WRITES"), "true") || !strings.EqualFold(os.Getenv("APPLE_ADS_ALLOW_DELETES"), "true") {
		t.Fatal("APPLE_ADS_ALLOW_WRITES=true and APPLE_ADS_ALLOW_DELETES=true are required")
	}
	cfg, _, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := cfg.ResolveProfile(os.Getenv("APPLE_ADS_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	if !profile.AllowWrites || !profile.AllowDeletes {
		t.Fatal("the selected file-backed profile must explicitly allow writes and deletes")
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

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPV02WriteHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_WRITE_HELPER=1", "APPLE_ADS_MCP_LIVE_DELETE_HELPER=true", "APPLE_ADS_ALLOW_WRITES=true", "APPLE_ADS_ALLOW_DELETES=true")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-v03-lifecycle-test", Version: "0.3.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	account := callTool(t, ctx, session, "ad_account_get", map[string]any{"profile": profile.Name, "adAccountId": accountID})
	currency, ok := findStringField(account, "currency")
	if !ok || len(currency) != 3 {
		t.Fatal("selected ad account did not return an ISO currency")
	}
	currency = strings.ToUpper(currency)
	date := time.Now().UTC().Format("2006-01-02")
	name := fmt.Sprintf("Apple Ads MCP v0.3 Validation - Lifecycle - %s", date)
	campaignID := createV03Campaign(t, ctx, session, profile.Name, accountID, adamID, storefront, currency, dailyBudget, name)

	previewApply(t, ctx, session, "campaign_bid_strategy_preview", map[string]any{
		"profile": profile.Name, "adAccountId": accountID, "campaignId": campaignID,
		"strategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP"},
	})

	adGroupName := "Apple Ads MCP v0.3 Validation - Reversible Ad Group"
	adGroupID := createV03AdGroup(t, ctx, session, profile.Name, accountID, campaignID, currency, defaultBid, adGroupName)
	previewApply(t, ctx, session, "ad_group_resume_preview", map[string]any{"profile": profile.Name, "adAccountId": accountID, "id": adGroupID})
	previewApply(t, ctx, session, "ad_group_pause_preview", map[string]any{"profile": profile.Name, "adAccountId": accountID, "id": adGroupID})
	readback := callTool(t, ctx, session, "ad_group_get", map[string]any{"profile": profile.Name, "adAccountId": accountID, "id": adGroupID})
	requireFieldValue(t, readback, "status", "PAUSED")

	keywordText := fmt.Sprintf("mcp v03 delete keyword %s", date)
	keywordID := previewApplyCreate(t, ctx, session, "keyword_create_preview", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"payload": map[string]any{"adGroupId": adGroupID, "text": keywordText, "matchType": "EXACT", "bid": map[string]any{"amount": defaultBid, "currency": currency}, "status": "PAUSED"},
	})
	previewApplyDelete(t, ctx, session, "keyword_delete_preview", profile.Name, accountID, keywordID, keywordText)

	negativeText := fmt.Sprintf("mcp v03 delete negative %s", date)
	negativeID := previewApplyCreate(t, ctx, session, "negative_keyword_create_preview", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"payload": map[string]any{"adGroupId": adGroupID, "text": negativeText, "matchType": "EXACT", "status": "PAUSED"},
	})
	previewApplyDelete(t, ctx, session, "negative_keyword_delete_preview", profile.Name, accountID, negativeID, negativeText)

	deleteGroupName := "Apple Ads MCP v0.3 Validation - Delete Ad Group"
	deleteGroupID := createV03AdGroup(t, ctx, session, profile.Name, accountID, campaignID, currency, defaultBid, deleteGroupName)
	previewApplyDelete(t, ctx, session, "ad_group_delete_preview", profile.Name, accountID, deleteGroupID, deleteGroupName)

	deleteCampaignName := fmt.Sprintf("Apple Ads MCP v0.3 Validation - Delete Campaign - %s", date)
	deleteCampaignID := createV03Campaign(t, ctx, session, profile.Name, accountID, adamID, storefront, currency, dailyBudget, deleteCampaignName)
	previewApplyDelete(t, ctx, session, "campaign_delete_preview", profile.Name, accountID, deleteCampaignID, deleteCampaignName)

	runV03AdAndCreativeLifecycle(t, ctx, session, profile.Name, accountID, adamID, storefront, currency, dailyBudget, defaultBid, date)
	previewApplyDelete(t, ctx, session, "campaign_delete_preview", profile.Name, accountID, campaignID, name)
	t.Log("v0.3 PAUSED lifecycle acceptance completed with verified deletes and no enabled campaign")
}

func createV03Campaign(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, adamID, storefront, currency, dailyBudget, name string) string {
	t.Helper()
	return createV03CampaignForPlacement(t, ctx, session, profile, accountID, adamID, storefront, currency, dailyBudget, name, "APPSTORE_SEARCH_RESULTS")
}

func createV03CampaignForPlacement(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, adamID, storefront, currency, dailyBudget, name, placement string) string {
	t.Helper()
	if existing := findResourceIDByField(callTool(t, ctx, session, "campaigns_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "name", "operator": "EQUALS", "value": name}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	}), "name", name); existing != "" {
		return existing
	}
	return previewApplyCreate(t, ctx, session, "campaign_create_preview", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"payload": map[string]any{
			"name": name, "billingEvent": "TAPS", "promotedObjectType": "APPSTORE_APP", "promotedObjectId": adamID, "status": "PAUSED",
			"dailyBudget": map[string]any{"value": map[string]any{"amount": dailyBudget, "currency": currency}},
			"targeting": map[string]any{
				"supplyPlacement": map[string]any{"include": []string{placement}},
				"countryOrRegion": map[string]any{"include": []string{storefront}},
			},
			"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP"},
		},
	})
}

func runV03AdAndCreativeLifecycle(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, adamID, storefront, currency, dailyBudget, defaultBid, date string) {
	t.Helper()
	if !placementEligible(t, ctx, session, profile, accountID, adamID, storefront, "APPSTORE_SEARCH_TAB") {
		t.Log("ad_delete_preview: not_eligible because Search Tab is unavailable")
		return
	}
	creatives := callTool(t, ctx, session, "creatives_query", map[string]any{
		"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	creativeID := findCreativeID(creatives, adamID, "DEFAULT_PRODUCT_PAGE", "")
	creativeName := fmt.Sprintf("Apple Ads MCP v0.3 Validation - Disposable DPP - %s", date)
	createdCreative := false
	if creativeID == "" {
		creativeID = previewApplyCreate(t, ctx, session, "creative_create_preview", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"payload": map[string]any{
				"name": creativeName, "creativeType": "DEFAULT_PRODUCT_PAGE", "creativeSpec": map[string]any{},
				"destination": map[string]any{"destinationType": "APP_STORE_PRODUCT_PAGE", "parameters": map[string]any{"adamId": adamID}},
			},
		})
		createdCreative = true
	}

	campaignName := fmt.Sprintf("Apple Ads MCP v0.3 Validation - Ad Delete - %s", date)
	campaignID := createV03CampaignForPlacement(t, ctx, session, profile, accountID, adamID, storefront, currency, dailyBudget, campaignName, "APPSTORE_SEARCH_TAB")
	adGroupName := "Apple Ads MCP v0.3 Validation - Ad Delete Group"
	adGroupID := createV03AdGroup(t, ctx, session, profile, accountID, campaignID, currency, defaultBid, adGroupName)
	adName := fmt.Sprintf("Apple Ads MCP v0.3 Validation - Delete Ad - %s", date)
	adID := findResourceIDByFields(callTool(t, ctx, session, "ads_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID},
			map[string]any{"field": "name", "operator": "EQUALS", "value": adName},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	}), map[string]string{"adGroupId": adGroupID, "name": adName})
	if adID == "" {
		adID = previewApplyCreate(t, ctx, session, "ad_create_preview", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"payload": map[string]any{"adGroupId": adGroupID, "creativeId": creativeID, "name": adName, "status": "PAUSED"},
		})
	}
	_, errText := callToolMaybeError(ctx, session, "ad_delete_preview", map[string]any{"profile": profile, "adAccountId": accountID, "id": adID, "expectedText": adName})
	if !strings.Contains(errText, "not_eligible") {
		t.Fatalf("DPP ad delete preview did not return not_eligible: %s", errText)
	}
	t.Log("ad_delete_preview: not_eligible because Apple does not allow deleting a Default Product Page ad")
	ad := callTool(t, ctx, session, "ad_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": adID})
	requireFieldValue(t, ad, "status", "PAUSED")
	campaign := callTool(t, ctx, session, "campaign_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": campaignID})
	requireFieldValue(t, campaign, "status", "PAUSED")
	report := callTool(t, ctx, session, "campaign_report", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "id", "operator": "EQUALS", "value": campaignID}},
		"fields":     []string{"id", "status", "localSpend"},
		"timeRange":  map[string]any{"start": date, "end": date, "timeZone": "UTC", "granularity": "DAILY"},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	if hasNonZeroMoneyField(report, "localSpend") {
		t.Fatal("the retained DPP ad fixture returned non-zero spend")
	}
	if createdCreative {
		_, errText := callToolMaybeError(ctx, session, "creative_delete_preview", map[string]any{"profile": profile, "adAccountId": accountID, "id": creativeID, "expectedText": creativeName})
		if !strings.Contains(errText, "not_eligible") {
			t.Fatalf("DPP creative delete preview did not return not_eligible: %s", errText)
		}
		t.Log("creative_delete_preview: not_eligible because Apple does not allow deleting the Default Product Page creative")
	} else {
		t.Log("creative_delete_preview: not_eligible because the available Default Product Page creative was not created by this acceptance run")
	}
}

func createV03AdGroup(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, campaignID, currency, defaultBid, name string) string {
	t.Helper()
	if existing := findResourceIDByFields(callTool(t, ctx, session, "ad_groups_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID},
			map[string]any{"field": "name", "operator": "EQUALS", "value": name},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	}), map[string]string{"campaignId": campaignID, "name": name}); existing != "" {
		return existing
	}
	return previewApplyCreate(t, ctx, session, "ad_group_create_preview", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"payload": map[string]any{
			"name": name, "campaignId": campaignID, "startTime": time.Now().UTC().Add(30 * time.Minute).Format("2006-01-02T15:04:05.000"),
			"pricingModel": "CPT", "automatedKeywordsOptIn": false, "status": "PAUSED",
			"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP", "bid": map[string]any{"amount": defaultBid, "currency": currency}},
		},
	})
}

func previewApplyDelete(t *testing.T, ctx context.Context, session *mcp.ClientSession, tool, profile, accountID, id, expectedText string) {
	t.Helper()
	preview := callTool(t, ctx, session, tool, map[string]any{"profile": profile, "adAccountId": accountID, "id": id, "expectedText": expectedText})
	receipt, ok := previewReceipt(preview)
	if !ok {
		t.Fatalf("%s did not return a receipt", tool)
	}
	callTool(t, ctx, session, "operations_apply", map[string]any{"receipt": receipt})
	verification := callTool(t, ctx, session, "operations_verify", map[string]any{"receipt": receipt})
	if !containsFieldValue(verification, "status", "deleted") {
		t.Fatalf("%s verification did not confirm deleted=true", tool)
	}
}
