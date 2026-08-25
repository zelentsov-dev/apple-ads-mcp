package tools

import (
	"strings"
	"testing"
)

func TestToolCatalogIsUniqueAndClassified(t *testing.T) {
	seen := make(map[string]struct{})
	all := append(ReadSpecs(), MutationSpecs()...)
	for _, item := range all {
		if item.Name == "raw_request" {
			t.Fatal("raw_request must not exist")
		}
		if strings.Contains(item.Name, "ad_account_update") || strings.Contains(item.Name, "raw_request") || strings.Contains(item.Name, "maps") {
			t.Fatalf("out-of-scope mutation tool exposed: %s", item.Name)
		}
		if item.Name == "" || item.Description == "" || item.Class == "" {
			t.Fatalf("incomplete tool spec: %+v", item)
		}
		if _, ok := seen[item.Name]; ok {
			t.Fatalf("duplicate tool name %q", item.Name)
		}
		seen[item.Name] = struct{}{}
		switch item.Class {
		case "read", "mutation_preview", "mutation":
		default:
			t.Fatalf("tool %q has unknown class %q", item.Name, item.Class)
		}
	}
	if len(all) < 105 {
		t.Fatalf("unexpectedly small tool catalog: %d", len(all))
	}
	for _, required := range []string{
		"optimization_baseline", "optimization_plan", "optimization_plan_preview",
		"shared_budget_create_preview", "shared_budget_update_preview", "campaign_shared_budget_assign_preview",
		"campaign_delete_preview", "creative_delete_preview", "shared_budget_delete_preview",
	} {
		if _, exists := seen[required]; !exists {
			t.Fatalf("required v0.3 tool is missing: %s", required)
		}
	}
}

func TestV021ToolNamesRemainAvailable(t *testing.T) {
	seen := make(map[string]struct{})
	for _, item := range append(ReadSpecs(), MutationSpecs()...) {
		seen[item.Name] = struct{}{}
	}
	legacy := []string{
		"server_info", "profiles_list", "auth_check", "ad_accounts_list", "ad_account_get", "advertiser_resources_list", "org_get",
		"apps_search", "apps_get", "apps_eligibility", "app_locale_details", "supported_app_languages", "app_store_geo_search",
		"app_rejection_reasons_query", "app_rejection_reason_get", "product_page_get", "product_pages_query", "product_page_locales",
		"keyword_suggestions", "phrase_suggestions", "category_suggestions", "target_cpa_suggestions", "search_term_popularity", "impression_share",
		"campaign_report", "ad_group_report", "ad_report", "keyword_report", "search_term_report", "daily_budget_recommendations", "target_cpa_recommendations",
		"change_history", "change_history_detail", "campaign_status_reason_details", "account_health", "app_opportunities", "campaign_audit",
		"campaign_get", "ad_group_get", "keyword_get", "negative_keyword_get", "ad_get", "creative_get", "shared_budget_get",
		"campaigns_query", "ad_groups_query", "keywords_query", "negative_keywords_query", "ads_query", "creatives_query", "shared_budgets_query", "campaign_inventory",
		"campaign_create_preview", "campaign_update_preview", "campaign_pause_preview", "campaign_resume_preview", "campaign_daily_budget_preview", "campaign_countries_preview", "campaign_schedule_preview",
		"ad_group_create_preview", "ad_group_update_preview", "ad_group_pause_preview", "ad_group_resume_preview", "ad_group_schedule_preview", "ad_group_search_match_preview", "ad_group_targeting_preview", "ad_group_bid_preview", "ad_group_cpa_cap_preview",
		"keyword_create_preview", "keyword_update_preview", "keyword_bid_preview", "keyword_pause_preview", "keyword_resume_preview", "negative_keyword_create_preview", "negative_keyword_update_preview",
		"ad_create_preview", "ad_update_preview", "ad_pause_preview", "ad_resume_preview", "creative_create_preview", "creative_update_preview",
		"keywords_bulk_create_preview", "keywords_bulk_update_preview", "negative_keywords_bulk_create_preview", "negative_keywords_bulk_update_preview",
		"daily_budget_recommendation_apply_preview", "daily_budget_recommendation_dismiss_preview", "target_cpa_recommendation_apply_preview", "target_cpa_recommendation_dismiss_preview",
		"operations_apply", "operations_inspect", "operations_verify",
	}
	for _, name := range legacy {
		if _, exists := seen[name]; !exists {
			t.Fatalf("v0.2.1 tool was removed: %s", name)
		}
	}
}

func TestBoundedResponse(t *testing.T) {
	items := make([]any, MaxItems+50)
	bounded, truncated := boundData(map[string]any{"items": items})
	if !truncated {
		t.Fatal("expected truncation")
	}
	if got := len(bounded.(map[string]any)["items"].([]any)); got != MaxItems {
		t.Fatalf("bounded size=%d", got)
	}
}

func TestAccountWriteRoles(t *testing.T) {
	data := map[string]any{"acls": []any{
		map[string]any{"adAccount": map[string]any{"id": "123"}, "roles": []any{"API Account Read Only"}},
		map[string]any{"adAccount": map[string]any{"id": "456"}, "roles": []any{"API Campaign Manager"}},
	}}
	roles, found := accountRoles(data, "456")
	if !found || len(roles) != 1 || roles[0] != "API Campaign Manager" {
		t.Fatalf("roles=%v found=%v", roles, found)
	}
	if !isWriteRole(roles[0]) || isWriteRole("API Account Read Only") {
		t.Fatalf("unexpected write-role classification: %v", roles)
	}
	if _, found := accountRoles(data, "789"); found {
		t.Fatal("unexpected account match")
	}
}

func TestOwnedAppMatchUsesStringIdentifiers(t *testing.T) {
	data := []any{map[string]any{"adamId": "7654321098", "name": "Example App"}}
	if !containsAdamID(data, "7654321098") || containsAdamID(data, "123") {
		t.Fatal("owned app matching failed")
	}
}

func TestAppleMapsPayloadsAreRejectedByGuard(t *testing.T) {
	for _, payload := range []map[string]any{
		{"supplySource": "MAPS"},
		{"creativeType": "LOCAL_ADS_SEARCH_CREATIVE"},
		{"targeting": map[string]any{"businessBrandId": "brand"}},
	} {
		if !containsMapsValue(payload) && payload["creativeType"] != "LOCAL_ADS_SEARCH_CREATIVE" {
			t.Fatalf("Maps payload was not detected: %v", payload)
		}
	}
	if containsMapsValue(map[string]any{"promotedObjectType": "APPSTORE_APP"}) {
		t.Fatal("App Store payload was incorrectly rejected")
	}
	if stringField(map[string]any{}, "campaignId") != "" {
		t.Fatal("missing ID must remain empty")
	}
}

func TestMutationPayloadBounds(t *testing.T) {
	if err := validateMutationPayload(map[string]any{"name": strings.Repeat("x", 1<<20)}); err == nil {
		t.Fatal("expected payload size rejection")
	}
	value := any("leaf")
	for range 22 {
		value = map[string]any{"nested": value}
	}
	if err := validateMutationPayload(map[string]any{"root": value}); err == nil {
		t.Fatal("expected payload depth rejection")
	}
}
