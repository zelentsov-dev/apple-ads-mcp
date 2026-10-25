//go:build live_write

package live

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	toolcatalog "github.com/zelentsov-dev/apple-ads-mcp/internal/tools"
)

func TestMCPV02OperatorAcceptance(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_WRITE") != "FULL_PAUSED_ACCEPTANCE" {
		t.Skip("full operator acceptance requires explicit confirmation")
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
	appQuery := strings.TrimSpace(os.Getenv("APPLE_ADS_LIVE_APP_QUERY"))
	if accountID == "" || adamID == "" || appQuery == "" {
		t.Fatal("APPLE_ADS_AD_ACCOUNT_ID, APPLE_ADS_LIVE_ADAM_ID, and APPLE_ADS_LIVE_APP_QUERY are required")
	}
	storefront := strings.ToUpper(os.Getenv("APPLE_ADS_LIVE_STOREFRONT"))
	if storefront == "" {
		storefront = "US"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPV02WriteHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_WRITE_HELPER=1", "APPLE_ADS_ALLOW_WRITES=true")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-v02-operator-test", Version: "0.2.1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	requireCompleteToolInventory(t, ctx, session)
	account := map[string]any{"profile": profile.Name, "adAccountId": accountID}
	date := time.Now().UTC().Format("2006-01-02")
	fixtureName := fmt.Sprintf("Apple Ads MCP v0.2 Validation - Search Results - %s", date)
	campaigns := callTool(t, ctx, session, "campaigns_query", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "name", "operator": "EQUALS", "value": fixtureName}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	campaignID := findResourceIDByField(campaigns, "name", fixtureName)
	if campaignID == "" {
		t.Fatalf("missing PAUSED Search Results fixture %q", fixtureName)
	}
	adGroups := callTool(t, ctx, session, "ad_groups_query", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	adGroupID, ok := findFirstID(adGroups)
	if !ok {
		t.Fatal("Search Results fixture has no ad group")
	}
	keywords := callTool(t, ctx, session, "keywords_query", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	keywordID := findResourceIDByField(keywords, "status", "PAUSED")
	if keywordID == "" {
		t.Fatal("Search Results fixture has no PAUSED validation keyword")
	}
	negatives := callTool(t, ctx, session, "negative_keywords_query", map[string]any{
		"profile": profile.Name, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": adGroupID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	negativeID, hasNegative := findFirstID(negatives)

	readState := runV02ReadMatrix(t, ctx, session, account, profile.Name, accountID, adamID, appQuery, storefront, campaignID, adGroupID, keywordID, negativeID, hasNegative, date)
	runV02PreviewMatrix(t, ctx, session, account, adamID, storefront, campaignID, adGroupID, keywordID, negativeID, hasNegative, readState)
	runV02ApplyMatrix(t, ctx, session, profile.Name, accountID, campaignID, adGroupID, keywordID, negativeID, hasNegative, readState.currency)
	requirePausedZeroSpend(t, ctx, session, profile.Name, accountID, date)
}

type v02ReadState struct {
	currency   string
	creativeID string
	adID       string
}

func runV02ReadMatrix(t *testing.T, ctx context.Context, session *mcp.ClientSession, account map[string]any, profile, accountID, adamID, appQuery, storefront, campaignID, adGroupID, keywordID, negativeID string, hasNegative bool, date string) v02ReadState {
	t.Helper()
	callTool(t, ctx, session, "server_info", map[string]any{})
	callTool(t, ctx, session, "profiles_list", map[string]any{})
	auth := callTool(t, ctx, session, "auth_check", map[string]any{"profile": profile})
	callTool(t, ctx, session, "ad_accounts_list", map[string]any{"profile": profile})
	adAccount := callTool(t, ctx, session, "ad_account_get", account)
	currency, ok := findStringField(adAccount, "currency")
	if !ok || len(currency) != 3 {
		t.Fatal("ad_account_get returned no ISO currency")
	}
	currency = strings.ToUpper(currency)
	callTool(t, ctx, session, "advertiser_resources_list", account)
	health := callTool(t, ctx, session, "account_health", map[string]any{"profile": profile, "adAccountId": accountID, "adamId": adamID})
	if ready, found := findBoolField(health, "ready"); !found || !ready {
		t.Fatal("account_health did not confirm readiness")
	}
	if orgID, found := findStringField(auth, "orgId"); found && orgID != "" {
		callTool(t, ctx, session, "org_get", map[string]any{"profile": profile, "orgId": orgID})
	} else {
		t.Log("org_get: not_applicable because OAuth identity returned no orgId")
	}
	apps := callTool(t, ctx, session, "apps_search", map[string]any{
		"profile": profile, "adAccountId": accountID, "query": appQuery, "returnOwnedApps": true,
		"storefronts": []string{storefront}, "limit": 20,
	})
	if !containsFieldValue(apps, "adamId", adamID) {
		t.Fatal("apps_search did not return the owned fixture app")
	}
	callTool(t, ctx, session, "apps_get", map[string]any{"profile": profile, "adAccountId": accountID, "adamId": adamID})
	callTool(t, ctx, session, "apps_eligibility", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamID},
			map[string]any{"field": "countryOrRegion", "operator": "IN", "value": []string{storefront}},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	callTool(t, ctx, session, "app_locale_details", map[string]any{
		"profile": profile, "adAccountId": accountID, "adamId": adamID,
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	callTool(t, ctx, session, "supported_app_languages", map[string]any{
		"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	callTool(t, ctx, session, "app_store_geo_search", map[string]any{
		"profile": profile, "adAccountId": accountID, "query": "United States", "entity": "Country", "countryCode": storefront,
		"eligible": true, "offset": 0, "pageSize": 50,
	})
	rejections := callTool(t, ctx, session, "app_rejection_reasons_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	if rejectionID, found := findFirstID(rejections); found {
		callTool(t, ctx, session, "app_rejection_reason_get", map[string]any{"profile": profile, "adAccountId": accountID, "rejectionReasonId": rejectionID})
	} else {
		t.Log("app_rejection_reason_get: not_applicable because Apple returned no rejection reasons")
	}
	pages := callTool(t, ctx, session, "product_pages_query", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters":    []any{map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamID}},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	if pageID := findProductPageID(pages, adamID); pageID != "" {
		callTool(t, ctx, session, "product_page_get", map[string]any{"profile": profile, "adAccountId": accountID, "productPageId": pageID})
		callTool(t, ctx, session, "product_page_locales", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "productPageId", "operator": "EQUALS", "value": pageID}},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
	} else {
		t.Log("product_page_get/product_page_locales: not_applicable because Apple returned no Product Page")
	}

	baseSuggestionFilters := []any{
		map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{adamID}},
		map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
	}
	keywordFilters := append(append([]any{}, baseSuggestionFilters...), map[string]any{"field": "countriesOrRegions", "operator": "IN", "value": []string{storefront}})
	discoveryFilters := append(append([]any{}, baseSuggestionFilters...), map[string]any{"field": "queryType", "operator": "EQUALS", "value": []string{"SUGGESTION"}})
	targetCPAFilters := append(append([]any{}, baseSuggestionFilters...), map[string]any{"field": "countryOrRegion", "operator": "IN", "value": []string{storefront}})
	callTool(t, ctx, session, "keyword_suggestions", map[string]any{"profile": profile, "adAccountId": accountID, "filters": keywordFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}})
	callKnownUpstreamLiveTool(t, ctx, session, "phrase_suggestions", map[string]any{"profile": profile, "adAccountId": accountID, "filters": discoveryFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}}, 500)
	callKnownUpstreamLiveTool(t, ctx, session, "category_suggestions", map[string]any{"profile": profile, "adAccountId": accountID, "filters": discoveryFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}}, 500)
	callTool(t, ctx, session, "target_cpa_suggestions", map[string]any{"profile": profile, "adAccountId": accountID, "filters": targetCPAFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}})
	lastSunday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -int(time.Now().UTC().Weekday()))
	popularityStart := lastSunday.AddDate(0, 0, -7)
	popularityEnd := popularityStart.AddDate(0, 0, 6)
	callTool(t, ctx, session, "search_term_popularity", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "countryOrRegion", "operator": "EQUALS", "value": storefront},
			map[string]any{"field": "genre", "operator": "EQUALS", "value": "PRODUCTIVITY_UTILITIES"},
		},
		"fields":     []string{"rankInGenre", "searchPopularityInGenre", "searchPopularity1to100", "searchPopularity1to5"},
		"timeRange":  map[string]any{"start": popularityStart.Format("2006-01-02"), "end": popularityEnd.Format("2006-01-02"), "granularity": "WEEKLY_SUN_SAT"},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	})
	callKnownUpstreamLiveTool(t, ctx, session, "impression_share", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"filters": []any{
			map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": adamID},
		},
		"options":    map[string]any{"impressionShareReportType": "ALL_SLOTS"},
		"timeRange":  map[string]any{"start": popularityStart.Format("2006-01-02"), "end": popularityEnd.Format("2006-01-02"), "timeZone": "UTC", "granularity": "WEEKLY_SUN_SAT"},
		"pagination": map[string]any{"offset": 0, "pageSize": 50},
	}, 400)

	timeRange := map[string]any{"start": date, "end": date, "timeZone": "UTC", "granularity": "DAILY"}
	reportCalls := []struct {
		name     string
		filter   map[string]any
		fields   []string
		timeZone string
	}{
		{name: "campaign_report", filter: map[string]any{"field": "id", "operator": "EQUALS", "value": campaignID}, fields: []string{"id", "name", "status", "localSpend"}},
		{name: "ad_group_report", filter: map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID}, fields: []string{"id", "campaignId", "name", "status", "localSpend"}},
		{name: "ad_report", filter: map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID}, fields: []string{"id", "campaignId", "adGroupId", "name", "status", "localSpend"}},
		{name: "keyword_report", filter: map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID}, fields: []string{"id", "campaignId", "adGroupId", "text", "status", "localSpend"}},
		{name: "search_term_report", filter: map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID}, fields: []string{"campaignId", "adGroupId", "searchTermText", "localSpend"}, timeZone: "ORTZ"},
	}
	for _, item := range reportCalls {
		rangeCopy := map[string]any{}
		for key, value := range timeRange {
			rangeCopy[key] = value
		}
		if item.timeZone != "" {
			rangeCopy["timeZone"] = item.timeZone
		}
		callTool(t, ctx, session, item.name, map[string]any{
			"profile": profile, "adAccountId": accountID, "filters": []any{item.filter}, "fields": item.fields,
			"timeRange": rangeCopy, "pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
	}
	recommendationFilters := []any{
		map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{adamID}},
		map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
	}
	callTool(t, ctx, session, "daily_budget_recommendations", map[string]any{"profile": profile, "adAccountId": accountID, "filters": recommendationFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}})
	callTool(t, ctx, session, "target_cpa_recommendations", map[string]any{"profile": profile, "adAccountId": accountID, "filters": recommendationFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}})
	changes := callTool(t, ctx, session, "change_history", map[string]any{"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 50}})
	if detailID, found := findStringField(changes, "detailId"); found && detailID != "" {
		callTool(t, ctx, session, "change_history_detail", map[string]any{"profile": profile, "adAccountId": accountID, "detailId": detailID, "offset": 0, "limit": 50})
	} else {
		t.Log("change_history_detail: not_applicable because Apple returned no detailId")
	}
	callOptionalLiveTool(t, ctx, session, "campaign_status_reason_details", map[string]any{"profile": profile, "adAccountId": accountID, "campaignId": campaignID}, "not found", "no data", "not limited")
	callTool(t, ctx, session, "app_opportunities", map[string]any{"profile": profile, "adAccountId": accountID, "adamId": adamID, "countriesOrRegions": []string{storefront}, "terms": []string{"voice transcription"}})
	callTool(t, ctx, session, "campaign_audit", map[string]any{"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 50}, "timeRange": timeRange})

	callTool(t, ctx, session, "campaign_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": campaignID})
	callTool(t, ctx, session, "ad_group_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": adGroupID})
	callTool(t, ctx, session, "keyword_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": keywordID})
	if hasNegative {
		callTool(t, ctx, session, "negative_keyword_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": negativeID})
	}
	callTool(t, ctx, session, "campaign_inventory", map[string]any{"profile": profile, "adAccountId": accountID, "campaignId": campaignID, "pageSize": 200})
	callTool(t, ctx, session, "shared_budgets_query", map[string]any{"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 50}})

	creatives := callTool(t, ctx, session, "creatives_query", map[string]any{"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 200}})
	creativeID := findCreativeID(creatives, adamID, "DEFAULT_PRODUCT_PAGE", "")
	if creativeID != "" {
		callTool(t, ctx, session, "creative_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": creativeID})
	}
	adID := findPausedFixtureAd(t, ctx, session, profile, accountID, date)
	if adID != "" {
		callTool(t, ctx, session, "ad_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": adID})
	}
	if budgets := callTool(t, ctx, session, "shared_budgets_query", map[string]any{"profile": profile, "adAccountId": accountID, "pagination": map[string]any{"offset": 0, "pageSize": 50}}); budgets != nil {
		if budgetID, found := findFirstID(budgets); found {
			callTool(t, ctx, session, "shared_budget_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": budgetID})
		} else {
			t.Log("shared_budget_get: not_applicable because Apple returned no shared budget")
		}
	}
	t.Log("read-only v0.2 matrix: pass")
	return v02ReadState{currency: currency, creativeID: creativeID, adID: adID}
}

func runV02PreviewMatrix(t *testing.T, ctx context.Context, session *mcp.ClientSession, account map[string]any, adamID, storefront, campaignID, adGroupID, keywordID, negativeID string, hasNegative bool, state v02ReadState) {
	t.Helper()
	profile := fmt.Sprint(account["profile"])
	accountID := fmt.Sprint(account["adAccountId"])
	future := time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02T15:04:05.000")
	previewOnly(t, ctx, session, "campaign_create_preview", map[string]any{
		"profile": profile, "adAccountId": accountID,
		"payload": map[string]any{
			"name": fmt.Sprintf("Apple Ads MCP v0.2 Preview Only - %d", time.Now().UnixNano()), "billingEvent": "TAPS",
			"promotedObjectType": "APPSTORE_APP", "promotedObjectId": adamID, "status": "PAUSED",
			"dailyBudget": map[string]any{"value": map[string]any{"amount": "5.00", "currency": state.currency}},
			"targeting": map[string]any{
				"supplyPlacement": map[string]any{"include": []string{"APPSTORE_SEARCH_RESULTS"}},
				"countryOrRegion": map[string]any{"include": []string{storefront}},
			},
			"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP"},
		},
	})
	previewOnly(t, ctx, session, "campaign_update_preview", mergeAccount(account, map[string]any{"id": campaignID, "payload": map[string]any{"status": "PAUSED"}}))
	previewOnly(t, ctx, session, "campaign_pause_preview", mergeAccount(account, map[string]any{"campaignId": campaignID}))
	previewOnly(t, ctx, session, "campaign_resume_preview", mergeAccount(account, map[string]any{"campaignId": campaignID}))
	previewOnly(t, ctx, session, "campaign_daily_budget_preview", mergeAccount(account, map[string]any{"campaignId": campaignID, "amount": map[string]any{"amount": "5.00", "currency": state.currency}}))
	previewOnly(t, ctx, session, "campaign_countries_preview", mergeAccount(account, map[string]any{"campaignId": campaignID, "countries": []string{storefront}}))
	previewOnly(t, ctx, session, "campaign_schedule_preview", mergeAccount(account, map[string]any{"id": campaignID, "startTime": future}))

	previewOnly(t, ctx, session, "ad_group_create_preview", mergeAccount(account, map[string]any{"payload": map[string]any{
		"name": fmt.Sprintf("Apple Ads MCP v0.2 Preview Only - %d", time.Now().UnixNano()), "campaignId": campaignID,
		"startTime": future, "pricingModel": "CPT", "automatedKeywordsOptIn": false, "status": "PAUSED",
		"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP", "bid": map[string]any{"amount": "0.25", "currency": state.currency}},
	}}))
	previewOnly(t, ctx, session, "ad_group_update_preview", mergeAccount(account, map[string]any{"id": adGroupID, "payload": map[string]any{"status": "PAUSED"}}))
	previewOnly(t, ctx, session, "ad_group_pause_preview", mergeAccount(account, map[string]any{"id": adGroupID}))
	previewOnly(t, ctx, session, "ad_group_resume_preview", mergeAccount(account, map[string]any{"id": adGroupID}))
	previewOnly(t, ctx, session, "ad_group_schedule_preview", mergeAccount(account, map[string]any{"id": adGroupID, "startTime": future}))
	previewOnly(t, ctx, session, "ad_group_search_match_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "enabled": false}))
	previewOnly(t, ctx, session, "ad_group_targeting_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "targeting": map[string]any{"deviceClass": map[string]any{"include": []string{"IPHONE"}}}}))
	previewOnly(t, ctx, session, "ad_group_bid_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "amount": "0.25", "currency": state.currency}))
	previewOnly(t, ctx, session, "ad_group_cpa_cap_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "amount": "1.00", "currency": state.currency}))

	previewOnly(t, ctx, session, "keyword_create_preview", mergeAccount(account, map[string]any{"payload": map[string]any{
		"adGroupId": adGroupID, "text": fmt.Sprintf("preview only %d", time.Now().UnixNano()), "matchType": "EXACT",
		"bid": map[string]any{"amount": "0.25", "currency": state.currency}, "status": "PAUSED",
	}}))
	previewOnly(t, ctx, session, "keyword_update_preview", mergeAccount(account, map[string]any{"id": keywordID, "payload": map[string]any{"status": "PAUSED"}}))
	previewOnly(t, ctx, session, "keyword_bid_preview", mergeAccount(account, map[string]any{"keywordId": keywordID, "bid": map[string]any{"amount": "0.25", "currency": state.currency}}))
	previewOnly(t, ctx, session, "keyword_pause_preview", mergeAccount(account, map[string]any{"id": keywordID}))
	previewOnly(t, ctx, session, "keyword_resume_preview", mergeAccount(account, map[string]any{"id": keywordID}))
	previewOnly(t, ctx, session, "negative_keyword_create_preview", mergeAccount(account, map[string]any{"payload": map[string]any{
		"adGroupId": adGroupID, "text": fmt.Sprintf("preview exclusion %d", time.Now().UnixNano()), "matchType": "EXACT", "status": "PAUSED",
	}}))
	if hasNegative {
		previewOnly(t, ctx, session, "negative_keyword_update_preview", mergeAccount(account, map[string]any{"id": negativeID, "payload": map[string]any{"status": "PAUSED"}}))
	}
	if state.creativeID != "" {
		previewOnly(t, ctx, session, "creative_create_preview", mergeAccount(account, map[string]any{"payload": map[string]any{
			"name": fmt.Sprintf("Apple Ads MCP v0.2 Preview Only - %d", time.Now().UnixNano()), "creativeType": "DEFAULT_PRODUCT_PAGE", "creativeSpec": map[string]any{},
			"destination": map[string]any{"destinationType": "APP_STORE_PRODUCT_PAGE", "parameters": map[string]any{"adamId": adamID}},
		}}))
		previewOnly(t, ctx, session, "creative_update_preview", mergeAccount(account, map[string]any{"id": state.creativeID, "payload": map[string]any{"name": fmt.Sprintf("Apple Ads MCP v0.2 Validation - Preview %d", time.Now().UnixNano())}}))
	}
	if state.adID != "" {
		previewOnly(t, ctx, session, "ad_update_preview", mergeAccount(account, map[string]any{"id": state.adID, "payload": map[string]any{"status": "PAUSED"}}))
		previewOnly(t, ctx, session, "ad_pause_preview", mergeAccount(account, map[string]any{"id": state.adID}))
		previewOnly(t, ctx, session, "ad_resume_preview", mergeAccount(account, map[string]any{"id": state.adID}))
	} else {
		t.Log("ad update/pause/resume previews: not_applicable because Apple returned no eligible fixture ad")
	}
	previewOnly(t, ctx, session, "keywords_bulk_create_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
		map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "text": fmt.Sprintf("bulk preview %d", time.Now().UnixNano()), "matchType": "EXACT", "bid": map[string]any{"amount": "0.25", "currency": state.currency}, "status": "PAUSED"},
	}}))
	previewOnly(t, ctx, session, "keywords_bulk_update_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
		map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "id": keywordID, "bid": map[string]any{"amount": "0.25", "currency": state.currency}, "status": "PAUSED"},
	}}))
	previewOnly(t, ctx, session, "negative_keywords_bulk_create_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
		map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "text": fmt.Sprintf("bulk exclusion %d", time.Now().UnixNano()), "matchType": "EXACT", "status": "PAUSED"},
	}}))
	if hasNegative {
		previewOnly(t, ctx, session, "negative_keywords_bulk_update_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
			map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "id": negativeID, "status": "PAUSED"},
		}}))
	}
	runRecommendationPreviews(t, ctx, session, account, adamID)
	t.Log("mutation preview v0.2 matrix: pass; resume receipts were not applied")
}

func runRecommendationPreviews(t *testing.T, ctx context.Context, session *mcp.ClientSession, account map[string]any, adamID string) {
	t.Helper()
	filters := []any{
		map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{adamID}},
		map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
	}
	for _, item := range []struct {
		query, apply, dismiss, moneyField string
	}{
		{query: "daily_budget_recommendations", apply: "daily_budget_recommendation_apply_preview", dismiss: "daily_budget_recommendation_dismiss_preview", moneyField: "suggestedDailyBudgetAmount"},
		{query: "target_cpa_recommendations", apply: "target_cpa_recommendation_apply_preview", dismiss: "target_cpa_recommendation_dismiss_preview", moneyField: "recommendedTargetCPA"},
	} {
		result := callTool(t, ctx, session, item.query, mergeAccount(account, map[string]any{"filters": filters, "pagination": map[string]any{"offset": 0, "pageSize": 50}}))
		recommendation := findObjectWithFields(result, "id", "promotedObjectId", item.moneyField)
		if recommendation == nil || fmt.Sprint(recommendation["promotedObjectId"]) != adamID {
			t.Logf("%s/%s: not_applicable because Apple returned no recommendation for the fixture app", item.apply, item.dismiss)
			continue
		}
		id := fmt.Sprint(recommendation["id"])
		money, ok := recommendation[item.moneyField].(map[string]any)
		if !ok {
			t.Fatalf("%s returned a recommendation without %s", item.query, item.moneyField)
		}
		previewOnly(t, ctx, session, item.apply, mergeAccount(account, map[string]any{"recommendationId": id, "promotedObjectId": adamID, "maximumAmount": money}))
		previewOnly(t, ctx, session, item.dismiss, mergeAccount(account, map[string]any{"recommendationId": id, "promotedObjectId": adamID}))
	}
}

func runV02ApplyMatrix(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, campaignID, adGroupID, keywordID, negativeID string, hasNegative bool, currency string) {
	t.Helper()
	account := map[string]any{"profile": profile, "adAccountId": accountID}
	campaign := callTool(t, ctx, session, "campaign_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": campaignID})
	originalBudget, ok := findMoneyAmount(campaign, "dailyBudget")
	if !ok {
		t.Fatal("fixture campaign returned no daily budget")
	}
	testBudget := incrementDecimal(t, originalBudget, "20.00")
	previewApply(t, ctx, session, "campaign_daily_budget_preview", mergeAccount(account, map[string]any{"campaignId": campaignID, "amount": map[string]any{"amount": testBudget, "currency": currency}}))
	budgetRestored := false
	defer func() {
		if !budgetRestored {
			cleanupPreviewApply(t, ctx, session, "campaign_daily_budget_preview", mergeAccount(account, map[string]any{"campaignId": campaignID, "amount": map[string]any{"amount": originalBudget, "currency": currency}}))
		}
	}()
	campaign = callTool(t, ctx, session, "campaign_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": campaignID})
	if amount, found := findMoneyAmount(campaign, "dailyBudget"); !found || amount != testBudget {
		t.Fatal("campaign daily-budget apply readback failed")
	}
	previewApply(t, ctx, session, "campaign_daily_budget_preview", mergeAccount(account, map[string]any{"campaignId": campaignID, "amount": map[string]any{"amount": originalBudget, "currency": currency}}))
	campaign = callTool(t, ctx, session, "campaign_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": campaignID})
	if amount, found := findMoneyAmount(campaign, "dailyBudget"); !found || amount != originalBudget {
		t.Fatal("campaign daily-budget restoration readback failed")
	}
	budgetRestored = true
	keyword := callTool(t, ctx, session, "keyword_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": keywordID})
	requireFieldValue(t, keyword, "status", "PAUSED")
	originalBid, ok := findMoneyAmount(keyword, "bid")
	if !ok {
		t.Fatal("fixture keyword returned no bid")
	}
	testBid := incrementDecimal(t, originalBid, "5.00")
	previewApply(t, ctx, session, "keywords_bulk_update_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
		map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "id": keywordID, "bid": map[string]any{"amount": testBid, "currency": currency}, "status": "PAUSED"},
	}}))
	keywordRestored := false
	defer func() {
		if !keywordRestored {
			cleanupPreviewApply(t, ctx, session, "keywords_bulk_update_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
				map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "id": keywordID, "bid": map[string]any{"amount": originalBid, "currency": currency}, "status": "PAUSED"},
			}}))
		}
	}()
	keyword = callTool(t, ctx, session, "keyword_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": keywordID})
	if amount, found := findMoneyAmount(keyword, "bid"); !found || amount != testBid {
		t.Fatal("bulk keyword update readback failed")
	}
	previewApply(t, ctx, session, "keywords_bulk_update_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
		map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "id": keywordID, "bid": map[string]any{"amount": originalBid, "currency": currency}, "status": "PAUSED"},
	}}))
	keyword = callTool(t, ctx, session, "keyword_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": keywordID})
	if amount, found := findMoneyAmount(keyword, "bid"); !found || amount != originalBid {
		t.Fatal("bulk keyword bid restoration readback failed")
	}
	keywordRestored = true
	if hasNegative {
		negative := callTool(t, ctx, session, "negative_keyword_get", map[string]any{"profile": profile, "adAccountId": accountID, "id": negativeID})
		requireFieldValue(t, negative, "status", "PAUSED")
		previewApply(t, ctx, session, "negative_keywords_bulk_update_preview", mergeAccount(account, map[string]any{"adGroupId": adGroupID, "items": []any{
			map[string]any{"correlationId": fmt.Sprint(time.Now().UnixNano()), "id": negativeID, "status": "PAUSED"},
		}}))
	}
	t.Log("safe apply/verify v0.2 matrix: pass; budget and keyword bid restored")
}

func findMoneyAmount(value any, field string) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if money, ok := typed[field]; ok {
			if amount, found := findStringField(money, "amount"); found {
				return amount, true
			}
		}
		for _, item := range typed {
			if amount, ok := findMoneyAmount(item, field); ok {
				return amount, true
			}
		}
	case []any:
		for _, item := range typed {
			if amount, ok := findMoneyAmount(item, field); ok {
				return amount, true
			}
		}
	}
	return "", false
}

func incrementDecimal(t *testing.T, value, maximum string) string {
	t.Helper()
	current, ok := new(big.Rat).SetString(value)
	if !ok || current.Sign() <= 0 {
		t.Fatalf("invalid live fixture decimal %q", value)
	}
	result := new(big.Rat).Add(current, big.NewRat(1, 100))
	cap, _ := new(big.Rat).SetString(maximum)
	if result.Cmp(cap) > 0 {
		t.Fatalf("live fixture decimal %s exceeds safety maximum %s", result.FloatString(2), maximum)
	}
	return result.FloatString(2)
}

func requirePausedZeroSpend(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, date string) {
	t.Helper()
	for _, label := range []string{"Search Results", "Search Tab", "Today Tab", "Product Pages"} {
		name := fmt.Sprintf("Apple Ads MCP v0.2 Validation - %s - %s", label, date)
		campaigns := callTool(t, ctx, session, "campaigns_query", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "name", "operator": "EQUALS", "value": name}},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
		campaignID := findResourceIDByField(campaigns, "name", name)
		if campaignID == "" {
			continue
		}
		inventory := callTool(t, ctx, session, "campaign_inventory", map[string]any{"profile": profile, "adAccountId": accountID, "campaignId": campaignID, "pageSize": 200})
		requireFieldValue(t, inventory, "status", "PAUSED")
		if containsFieldValue(inventory, "status", "ENABLED") {
			t.Fatalf("fixture %q contains an enabled delivery object", name)
		}
		report := callTool(t, ctx, session, "campaign_report", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "id", "operator": "EQUALS", "value": campaignID}},
			"fields":     []string{"id", "name", "status", "localSpend"},
			"timeRange":  map[string]any{"start": date, "end": date, "timeZone": "UTC", "granularity": "DAILY"},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
		if hasNonZeroMoneyField(report, "localSpend") {
			t.Fatalf("fixture %q returned non-zero spend", name)
		}
	}
	t.Log("final delivery guard: all available fixtures are PAUSED with zero spend")
}

func requireCompleteToolInventory(t *testing.T, ctx context.Context, session *mcp.ClientSession) {
	t.Helper()
	listed := map[string]bool{}
	params := &mcp.ListToolsParams{}
	for {
		result, err := session.ListTools(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range result.Tools {
			listed[tool.Name] = true
		}
		if result.NextCursor == "" {
			break
		}
		params.Cursor = result.NextCursor
	}
	for _, spec := range append(toolcatalog.ReadSpecs(), toolcatalog.MutationSpecs()...) {
		if !listed[spec.Name] {
			t.Fatalf("tools/list did not expose %s", spec.Name)
		}
	}
	t.Logf("tools/list: %d tools exposed; complete v0.2 catalog present", len(listed))
}

func previewOnly(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) {
	t.Helper()
	preview := callTool(t, ctx, session, name, arguments)
	receipt, ok := previewReceipt(preview)
	if !ok {
		t.Fatalf("%s returned no receipt", name)
	}
	inspected := callTool(t, ctx, session, "operations_inspect", map[string]any{"receipt": receipt})
	if used, found := findBoolField(inspected, "used"); found && used {
		t.Fatalf("%s preview receipt was unexpectedly used", name)
	}
}

func cleanupPreviewApply(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) {
	t.Helper()
	preview, errText := callToolMaybeError(ctx, session, name, arguments)
	if errText != "" {
		t.Errorf("%s cleanup preview failed: %s", name, errText)
		return
	}
	receipt, ok := previewReceipt(preview)
	if !ok {
		t.Errorf("%s cleanup preview returned no receipt", name)
		return
	}
	for _, tool := range []string{"operations_inspect", "operations_apply", "operations_verify"} {
		if _, errText := callToolMaybeError(ctx, session, tool, map[string]any{"receipt": receipt}); errText != "" {
			t.Errorf("%s cleanup failed: %s", tool, errText)
			return
		}
	}
}

func callOptionalLiveTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any, allowed ...string) any {
	t.Helper()
	value, errText := callToolMaybeError(ctx, session, name, arguments)
	if errText == "" {
		return value
	}
	lower := strings.ToLower(errText)
	for _, text := range allowed {
		if strings.Contains(lower, strings.ToLower(text)) {
			t.Logf("%s: not_applicable (%s)", name, text)
			return nil
		}
	}
	t.Fatalf("%s failed: %s", name, errText)
	return nil
}

func callKnownUpstreamLiveTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any, expectedStatus int, expectedCodes ...string) any {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s transport failed: %v", name, err)
	}
	if !result.IsError {
		if result.StructuredContent == nil {
			t.Fatalf("%s returned no structuredContent", name)
		}
		return normalizeJSON(result.StructuredContent)
	}
	structured := normalizeJSON(result.StructuredContent)
	errorValue, ok := objectField(structured, "error")
	if !ok {
		t.Fatalf("%s returned no structured Apple error", name)
	}
	errorType, _ := findStringField(errorValue, "type")
	httpStatusText, found := findStringField(errorValue, "httpStatus")
	httpStatus := 0
	if found {
		_, _ = fmt.Sscan(httpStatusText, &httpStatus)
	}
	if errorType != "apple_api_error" || !found || httpStatus != expectedStatus {
		t.Fatalf("%s returned unexpected error metadata: type=%q status=%d", name, errorType, httpStatus)
	}
	code, _ := findStringField(errorValue, "code")
	responseFormat, _ := findStringField(errorValue, "responseFormat")
	if len(expectedCodes) > 0 {
		matched := false
		for _, expected := range expectedCodes {
			if code == expected {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("%s returned unexpected Apple error code %q", name, code)
		}
	}
	t.Logf("%s: known upstream response HTTP %d code=%q responseFormat=%q", name, httpStatus, code, responseFormat)
	return nil
}

func findPausedFixtureAd(t *testing.T, ctx context.Context, session *mcp.ClientSession, profile, accountID, date string) string {
	t.Helper()
	for _, label := range []string{"Search Tab", "Today Tab", "Product Pages"} {
		name := fmt.Sprintf("Apple Ads MCP v0.2 Validation - %s - %s", label, date)
		campaigns := callTool(t, ctx, session, "campaigns_query", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "name", "operator": "EQUALS", "value": name}},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
		campaignID := findResourceIDByField(campaigns, "name", name)
		if campaignID == "" {
			continue
		}
		ads := callTool(t, ctx, session, "ads_query", map[string]any{
			"profile": profile, "adAccountId": accountID,
			"filters":    []any{map[string]any{"field": "campaignId", "operator": "EQUALS", "value": campaignID}},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		})
		if id, found := findFirstID(ads); found {
			return id
		}
	}
	return ""
}

func mergeAccount(account map[string]any, values map[string]any) map[string]any {
	result := make(map[string]any, len(account)+len(values))
	for key, value := range account {
		result[key] = value
	}
	for key, value := range values {
		result[key] = value
	}
	return result
}

func findFirstID(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if id, ok := scalarString(typed["id"]); ok && id != "" {
			return id, true
		}
		for _, item := range typed {
			if id, ok := findFirstID(item); ok {
				return id, true
			}
		}
	case []any:
		for _, item := range typed {
			if id, ok := findFirstID(item); ok {
				return id, true
			}
		}
	}
	return "", false
}

func findObjectWithFields(value any, fields ...string) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		matches := true
		for _, field := range fields {
			if typed[field] == nil {
				matches = false
				break
			}
		}
		if matches {
			return typed
		}
		for _, item := range typed {
			if found := findObjectWithFields(item, fields...); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findObjectWithFields(item, fields...); found != nil {
				return found
			}
		}
	}
	return nil
}
