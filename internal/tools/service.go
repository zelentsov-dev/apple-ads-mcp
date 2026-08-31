package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

type appleAdsManager interface {
	Do(context.Context, string, string, appleads.Operation) (appleads.Result, error)
	Profile(string) (config.Profile, error)
	Profiles() []config.PublicProfile
}

type Service struct {
	manager      appleAdsManager
	allowWrites  bool
	allowDeletes bool
	version      string
	policyPath   string
	billingPath  string
	historyRoot  string
	historyOnce  sync.Once
	historyStore *optimization.HistoryStore
	historyErr   error
	now          func() time.Time
}

func (s *Service) optimizationHistoryStore() (*optimization.HistoryStore, error) {
	s.historyOnce.Do(func() {
		s.historyStore, s.historyErr = optimization.NewHistoryStore(s.historyRoot)
	})
	return s.historyStore, s.historyErr
}

func NewService(manager *appleads.Manager, allowWrites bool, version string) *Service {
	return NewServiceWithOptions(manager, allowWrites, false, version, "", "", "")
}

func NewServiceWithOptions(manager *appleads.Manager, allowWrites, allowDeletes bool, version, policyPath, billingPath, historyRoot string) *Service {
	return &Service{manager: manager, allowWrites: allowWrites, allowDeletes: allowDeletes, version: version, policyPath: policyPath, billingPath: billingPath, historyRoot: historyRoot, now: time.Now}
}

func ReadSpecs() []Spec {
	return []Spec{
		{Name: "server_info", Description: "Show server version, safety mode, API family, and response limits.", Class: "read"},
		{Name: "profiles_list", Description: "List configured profile names without exposing credentials.", Class: "read"},
		{Name: "auth_check", Description: "Validate OAuth credentials and return the caller identity.", Class: "read"},
		{Name: "ad_accounts_list", Description: "Discover Apple Ads account ACLs available to a profile.", Class: "read"},
		{Name: "ad_account_get", Description: "Read sanitized account currency, timezone, payment model, features, and delegations.", Class: "read"},
		{Name: "advertiser_resources_list", Description: "List App Store content-provider delegations available to a profile.", Class: "read"},
		{Name: "org_get", Description: "Read one Apple Ads organization.", Class: "read"},
		{Name: "apps_search", Description: "Search apps; an empty result for a non-ASCII owned-app query gets one bounded exact-name fallback.", Class: "read"},
		{Name: "apps_get", Description: "Read an App Store app by Adam ID.", Class: "read"},
		{Name: "apps_eligibility", Description: "Check App Store advertising eligibility.", Class: "read"},
		{Name: "app_locale_details", Description: "Query compact locale metadata, or full assets for one required languageCode.", Class: "read"},
		{Name: "supported_app_languages", Description: "Query bounded App Store advertising language metadata.", Class: "read"},
		{Name: "app_store_geo_search", Description: "Search eligible App Store countries, administrative areas, and localities.", Class: "read"},
		{Name: "app_rejection_reasons_query", Description: "Query bounded App Store app rejection reasons.", Class: "read"},
		{Name: "app_rejection_reason_get", Description: "Read one App Store app rejection reason.", Class: "read"},
		{Name: "product_page_get", Description: "Read a Default or Custom Product Page by ID.", Class: "read"},
		{Name: "product_pages_query", Description: "Query bounded Default and Custom Product Pages.", Class: "read"},
		{Name: "product_page_locales", Description: "Query bounded Product Page locale details.", Class: "read"},
		{Name: "keyword_suggestions", Description: "Get keyword opportunities for an App Store app.", Class: "read"},
		{Name: "phrase_suggestions", Description: "Get phrase opportunities for an App Store app.", Class: "read"},
		{Name: "category_suggestions", Description: "Get category opportunities for an App Store app.", Class: "read"},
		{Name: "target_cpa_suggestions", Description: "Get target CPA suggestions.", Class: "read"},
		{Name: "search_term_popularity", Description: "Read search-term popularity insights.", Class: "read"},
		{Name: "impression_share", Description: "Read app impression-share insights through a typed UTC request.", Class: "read"},
		{Name: "campaign_report", Description: "Read bounded campaign performance rows.", Class: "read"},
		{Name: "ad_group_report", Description: "Read bounded ad-group performance rows.", Class: "read"},
		{Name: "ad_report", Description: "Read bounded ad performance rows.", Class: "read"},
		{Name: "keyword_report", Description: "Read bounded keyword performance rows, optionally including EMPTY_METRICS.", Class: "read"},
		{Name: "search_term_report", Description: "Read bounded search-term performance rows.", Class: "read"},
		{Name: "daily_budget_recommendations", Description: "Read daily-budget recommendations without applying them.", Class: "read"},
		{Name: "target_cpa_recommendations", Description: "Read target-CPA recommendations without applying them.", Class: "read"},
		{Name: "change_history", Description: "Read bounded Apple Ads change history for a required typed event window.", Class: "read"},
		{Name: "change_history_detail", Description: "Read bounded field-level details for one change-history entry.", Class: "read"},
		{Name: "campaign_status_reason_details", Description: "Read App Store campaign limited-status reason details.", Class: "read"},
		{Name: "account_health", Description: "Collect a read-only identity, access, and account baseline.", Class: "read"},
		{Name: "app_opportunities", Description: "Collect independently classified eligibility and suggestion sections for an app.", Class: "read"},
		{Name: "campaign_audit", Description: "Collect campaign, ad-group, keyword, and search-term report baselines.", Class: "read"},
		{Name: "campaign_get", Description: "Read one campaign by ID for current-state verification.", Class: "read"},
		{Name: "ad_group_get", Description: "Read one ad group by ID for current-state verification.", Class: "read"},
		{Name: "keyword_get", Description: "Read one targeting keyword by ID for current-state verification.", Class: "read"},
		{Name: "negative_keyword_get", Description: "Read one negative keyword by ID for current-state verification.", Class: "read"},
		{Name: "ad_get", Description: "Read one ad by ID for current-state verification.", Class: "read"},
		{Name: "creative_get", Description: "Read one creative by ID for current-state verification.", Class: "read"},
		{Name: "shared_budget_get", Description: "Read one shared budget by ID for current-state verification.", Class: "read"},
		{Name: "campaigns_query", Description: "Query bounded campaigns with endpoint-specific filters.", Class: "read"},
		{Name: "ad_groups_query", Description: "Query bounded ad groups with endpoint-specific filters.", Class: "read"},
		{Name: "keywords_query", Description: "Query bounded targeting-keyword inventory with a required typed scope.", Class: "read"},
		{Name: "negative_keywords_query", Description: "Query bounded negative-keyword inventory with a valid campaign or ad-group scope.", Class: "read"},
		{Name: "ads_query", Description: "Query bounded ads with endpoint-specific filters.", Class: "read"},
		{Name: "creatives_query", Description: "Query bounded App Store creatives with endpoint-specific filters.", Class: "read"},
		{Name: "shared_budgets_query", Description: "Query bounded shared-budget metadata without invoice contacts.", Class: "read"},
		{Name: "campaign_inventory", Description: "Read a campaign and bounded child inventory for safe auditing.", Class: "read"},
		{Name: "optimization_policies_list", Description: "List named local optimization policies without credentials or account data.", Class: "read"},
		{Name: "optimization_policy_get", Description: "Read one named optimization policy with resolved balanced thresholds.", Class: "read"},
		{Name: "optimization_baseline", Description: "Build a bounded 28-day Apple Ads performance baseline for a named policy.", Class: "read"},
		{Name: "optimization_plan", Description: "Build an on-demand bounded optimization plan without creating a receipt.", Class: "read"},
		{Name: "optimization_history", Description: "Read bounded local decisions and verification history for a named policy.", Class: "read"},
	}
}

func (s *Service) RegisterReadTools(server *mcp.Server) {
	s.registerNoInput(server)
	s.registerAccess(server)
	s.registerApps(server)
	s.registerQueries(server)
	s.registerAudits(server)
	s.registerResources(server)
	s.registerOptimizationReadTools(server)
}

func (s *Service) registerNoInput(server *mcp.Server) {
	addReadTool(server, ReadSpecs()[0], func(context.Context, *mcp.CallToolRequest, NoInput) (*mcp.CallToolResult, Output, error) {
		mode := "read-only"
		if s.allowWrites {
			mode = "writes require profile opt-in and a valid preview receipt"
		}
		if s.allowWrites && s.allowDeletes {
			mode = "writes and separately gated deletes require profile opt-in and a valid preview receipt"
		}
		output := Output{Summary: "Apple Ads MCP is ready", Data: map[string]any{
			"name": "apple-ads-mcp", "version": s.version, "api": "Apple Ads Platform API v1",
			"contractVersion": "0.3", "baseUrl": appleads.BaseURL, "mode": mode, "maxItemsPerArray": MaxItems,
			"placements": []string{"APPSTORE_SEARCH_RESULTS", "APPSTORE_SEARCH_TAB", "APPSTORE_TODAY_TAB", "APPSTORE_PRODUCT_PAGES"},
			"legacyV5":   false, "appleMaps": false, "rawRequestTool": false,
		}}
		return textResult(output.Summary, false), output, nil
	})
	addReadTool(server, ReadSpecs()[1], func(context.Context, *mcp.CallToolRequest, NoInput) (*mcp.CallToolResult, Output, error) {
		profiles := s.manager.Profiles()
		output := Output{Summary: fmt.Sprintf("Found %d configured profile(s)", len(profiles)), Data: profiles}
		return textResult(output.Summary, false), output, nil
	})
}

func (s *Service) registerAccess(server *mcp.Server) {
	addReadTool(server, spec("auth_check"), func(ctx context.Context, _ *mcp.CallToolRequest, input ProfileInput) (*mcp.CallToolResult, Output, error) {
		result, err := s.manager.Do(ctx, input.Profile, "", appleads.Me())
		return handled("OAuth credentials are valid", result, err)
	})
	addReadTool(server, spec("ad_accounts_list"), func(ctx context.Context, _ *mcp.CallToolRequest, input ProfileInput) (*mcp.CallToolResult, Output, error) {
		result, err := s.manager.Do(ctx, input.Profile, "", appleads.ACLs())
		return handled("Available ad accounts loaded", result, err)
	})
	addReadTool(server, spec("ad_account_get"), func(ctx context.Context, _ *mcp.CallToolRequest, input AccountInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input); err != nil {
			return failed(err)
		}
		op, err := appleads.AdAccount(input.AdAccountID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("Ad account readiness metadata loaded", result, err)
	})
	addReadTool(server, spec("advertiser_resources_list"), func(ctx context.Context, _ *mcp.CallToolRequest, input AccountInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input); err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, appleads.AdvertiserResources())
		return handled("App Store content-provider delegations loaded", result, err)
	})
	addReadTool(server, spec("org_get"), func(ctx context.Context, _ *mcp.CallToolRequest, input OrgInput) (*mcp.CallToolResult, Output, error) {
		if strings.TrimSpace(input.Profile) == "" {
			return failed(fmt.Errorf("profile is required"))
		}
		if strings.TrimSpace(input.OrgID) == "" {
			return failed(fmt.Errorf("orgId is required"))
		}
		op, err := appleads.Org(input.OrgID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, "", op)
		return handled("Organization loaded", result, err)
	})
}

func (s *Service) registerApps(server *mcp.Server) {
	addReadTool(server, spec("apps_search"), s.appsSearch)
	addReadTool(server, spec("apps_get"), func(ctx context.Context, _ *mcp.CallToolRequest, input AppInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		op, err := appleads.App(input.AdamID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("App loaded", result, err)
	})
	addReadTool(server, spec("apps_eligibility"), s.queryHandler("App eligibility loaded", func(body any) (appleads.Operation, error) {
		return appleads.AppsEligibility(body), nil
	}))
	addReadTool(server, spec("app_locale_details"), s.appLocaleDetails)
	addReadTool(server, spec("supported_app_languages"), s.queryHandler("Supported App Store languages loaded", func(body any) (appleads.Operation, error) {
		return appleads.SupportedAppLanguages(body), nil
	}))
	addReadTool(server, spec("app_store_geo_search"), func(ctx context.Context, _ *mcp.CallToolRequest, input GeoSearchInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		op, err := appleads.AppStoreGeoSearch(appleads.GeoSearchParams{Query: input.Query, Entity: input.Entity, CountryCode: input.CountryCode, Eligible: input.Eligible, Offset: input.Offset, PageSize: input.PageSize})
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("App Store geographies loaded", result, err)
	})
	addReadTool(server, spec("app_rejection_reasons_query"), s.queryHandler("App rejection reasons loaded", func(body any) (appleads.Operation, error) {
		return appleads.RejectionReasonsQuery(body), nil
	}))
	addReadTool(server, spec("app_rejection_reason_get"), func(ctx context.Context, _ *mcp.CallToolRequest, input RejectionReasonInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		op, err := appleads.RejectionReason(input.RejectionReasonID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("App rejection reason loaded", result, err)
	})
	addReadTool(server, spec("product_page_get"), func(ctx context.Context, _ *mcp.CallToolRequest, input ProductPageInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		op, err := appleads.ProductPage(input.ProductPageID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("Product Page loaded", result, err)
	})
	addReadTool(server, spec("product_pages_query"), s.queryHandler("Product Pages loaded", func(body any) (appleads.Operation, error) {
		return appleads.ProductPagesQuery(body), nil
	}))
	addReadTool(server, spec("product_page_locales"), s.queryHandler("Product Page locale details loaded", func(body any) (appleads.Operation, error) {
		return appleads.ProductPageLocales(body), nil
	}))
}

func (s *Service) appsSearch(ctx context.Context, _ *mcp.CallToolRequest, input AppsSearchInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	op, err := appleads.SearchApps(appleads.SearchAppsParams{Query: input.Query, ReturnOwnedApps: input.ReturnOwnedApps, CPIDs: input.CPIDs, Storefronts: input.Storefronts, Offset: input.Offset, Limit: input.Limit})
	if err != nil {
		return failed(err)
	}
	result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
	if err != nil {
		return failed(err)
	}
	summary := "App search completed"
	if input.ReturnOwnedApps && hasNonASCII(input.Query) && resultDataEmpty(result.Data) {
		fallbackOp, fallbackErr := appleads.SearchApps(appleads.SearchAppsParams{
			ReturnOwnedApps: true, CPIDs: input.CPIDs, Storefronts: input.Storefronts, Limit: MaxItems,
		})
		if fallbackErr != nil {
			return failed(fallbackErr)
		}
		fallback, fallbackErr := s.manager.Do(ctx, input.Profile, input.AdAccountID, fallbackOp)
		if fallbackErr != nil {
			return failed(fallbackErr)
		}
		matches := exactOwnedAppMatches(fallback.Data, input.Query)
		if len(matches) > 0 {
			result = appleads.Result{
				Data: matches, Status: fallback.Status, RateLimit: fallback.RateLimit,
				Pagination: appleads.Pagination{Offset: 0, PageSize: len(matches), Total: len(matches)},
			}
			summary += "; exact Unicode name match resolved from bounded owned-app inventory"
		} else {
			summary += "; no exact Unicode name match was found in bounded owned-app inventory"
		}
	}
	return handled(summary, result, nil)
}

func (s *Service) appLocaleDetails(ctx context.Context, _ *mcp.CallToolRequest, input AppLocaleDetailsInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	if _, err := numericAdamID(input.AdamID); err != nil {
		return failed(err)
	}
	if input.IncludeAssets && strings.TrimSpace(input.LanguageCode) == "" {
		return failed(errors.New("includeAssets=true requires languageCode so assets are limited to one locale"))
	}
	queryInput, err := addShortcutFilters(input.QueryInput, []QueryFilterInput{shortcutEquals("languageCode", input.LanguageCode)})
	if err != nil {
		return failed(err)
	}
	request, err := queryInput.boundedRequest()
	if err != nil {
		return failed(err)
	}
	op, err := appleads.AppLocaleDetails(input.AdamID, request)
	if err != nil {
		return failed(err)
	}
	result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
	if err != nil {
		return failed(err)
	}
	summary := "App locale details loaded"
	if !input.IncludeAssets {
		result.Data = compactLocaleDetails(result.Data)
		summary += "; compact output includes asset counts instead of full asset payloads"
	}
	return handled(summary, result, nil)
}

func (s *Service) registerQueries(server *mcp.Server) {
	for _, item := range []struct{ name, summary, kind string }{
		{"keyword_suggestions", "Keyword suggestions loaded", "keywords"},
		{"phrase_suggestions", "Phrase suggestions loaded", "phrases"},
		{"category_suggestions", "Category suggestions loaded", "categories"},
		{"target_cpa_suggestions", "Target CPA suggestions loaded", "target-cpas"},
	} {
		kind := item.kind
		addReadTool(server, spec(item.name), s.queryHandler(item.summary, func(body any) (appleads.Operation, error) { return appleads.Suggestion(kind, body) }))
	}
	addReadTool(server, spec("search_term_popularity"), s.queryHandler("Search-term popularity loaded", func(body any) (appleads.Operation, error) {
		return appleads.Insight("search-term-popularity", body)
	}))
	addReadTool(server, spec("impression_share"), s.impressionShare)
	for _, item := range []struct{ name, summary, kind string }{
		{"campaign_report", "Campaign report loaded", "campaigns"},
		{"ad_group_report", "Ad-group report loaded", "adgroups"},
		{"ad_report", "Ad report loaded", "ads"},
		{"search_term_report", "Search-term report loaded", "searchterms"},
	} {
		kind := item.kind
		addReadTool(server, spec(item.name), s.reportHandler(item.summary, kind))
	}
	addReadTool(server, spec("keyword_report"), s.keywordReport)
	for _, item := range []struct{ name, summary, kind string }{
		{"daily_budget_recommendations", "Daily-budget recommendations loaded", "daily-budgets"},
		{"target_cpa_recommendations", "Target-CPA recommendations loaded", "target-cpas"},
	} {
		kind := item.kind
		addReadTool(server, spec(item.name), s.queryHandler(item.summary, func(body any) (appleads.Operation, error) { return appleads.Recommendation(kind, body) }))
	}
	addReadTool(server, spec("change_history"), s.changeHistory)
	addReadTool(server, spec("change_history_detail"), func(ctx context.Context, _ *mcp.CallToolRequest, input ChangeHistoryDetailInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		if input.Limit == 0 {
			input.Limit = 100
		}
		op, err := appleads.ChangeHistoryDetail(input.DetailID, input.Offset, input.Limit)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("Change-history details loaded", result, err)
	})
	addReadTool(server, spec("campaign_status_reason_details"), func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignStatusReasonInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		op, err := appleads.CampaignStatusReasonDetails(input.CampaignID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled("Campaign status reason details loaded", result, err)
	})
}

func (s *Service) registerAudits(server *mcp.Server) {
	addReadTool(server, spec("account_health"), func(ctx context.Context, _ *mcp.CallToolRequest, input AccountHealthInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		identity, err := s.manager.Do(ctx, input.Profile, "", appleads.Me())
		if err != nil {
			return failed(err)
		}
		accounts, err := s.manager.Do(ctx, input.Profile, "", appleads.ACLs())
		if err != nil {
			return failed(err)
		}
		roles, found := accountRoles(accounts.Data, input.AdAccountID)
		if !found {
			return failed(fmt.Errorf("ad account %s is not present in the profile ACL", input.AdAccountID))
		}
		accountOperation, err := appleads.AdAccount(input.AdAccountID)
		if err != nil {
			return failed(err)
		}
		account, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, accountOperation)
		if err != nil {
			return failed(err)
		}
		resources, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, appleads.AdvertiserResources())
		if err != nil {
			return failed(err)
		}
		manualFeature := containsStringValue(account.Data, "APPSTORE_APP_MANUAL")
		accountCPIDs := contentProviderResourceIDs(account.Data)
		availableCPIDs := contentProviderResourceIDs(resources.Data)
		visibleDelegatedCPIDs := intersectStrings(accountCPIDs, availableCPIDs)
		contentProvider := len(accountCPIDs) > 0
		currency := findStringField(account.Data, "currency")
		readiness := map[string]any{
			"aclPresent": true, "appStoreManualFeature": manualFeature,
			"contentProviderDelegation": contentProvider, "delegatedContentProviderIds": accountCPIDs,
			"availableContentProviderIds": availableCPIDs, "delegationVisibleInAdvertiserResources": len(visibleDelegatedCPIDs) > 0,
			"currencyConfigured": currency != "", "currency": currency,
		}
		readiness["ready"] = manualFeature && contentProvider && currency != ""
		data := map[string]any{
			"identity": identity.Data, "accounts": accounts.Data, "selectedAccountRoles": roles,
			"adAccount": account.Data, "contentProviderDelegations": resources.Data,
			"readiness": readiness,
		}
		if strings.TrimSpace(input.AdamID) != "" {
			ownedOperation, err := appleads.SearchApps(appleads.SearchAppsParams{Query: input.AdamID, ReturnOwnedApps: true, Limit: 20})
			if err != nil {
				return failed(err)
			}
			owned, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, ownedOperation)
			if err != nil {
				return failed(err)
			}
			isOwned := containsAdamID(owned.Data, input.AdamID)
			data["appOwnership"] = map[string]any{"adamId": input.AdamID, "owned": isOwned}
			readiness["appOwned"] = isOwned
			readiness["ready"] = readiness["ready"].(bool) && isOwned
		}
		if orgID := findStringField(identity.Data, "orgId"); orgID != "" {
			op, err := appleads.Org(orgID)
			if err != nil {
				return failed(err)
			}
			org, err := s.manager.Do(ctx, input.Profile, "", op)
			if err != nil {
				return failed(err)
			}
			data["organization"] = org.Data
		}
		bounded, _ := boundData(sanitizePublicData(data))
		output := Output{Summary: "Account identity, App Store delegation, and readiness baseline loaded", Data: bounded}
		return textResult(output.Summary, false), output, nil
	})
	addReadTool(server, spec("app_opportunities"), s.appOpportunities)
	addReadTool(server, spec("campaign_audit"), s.campaignAudit)
}

func contentProviderResourceIDs(value any) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if fmt.Sprint(typed["resourceType"]) == "CONTENT_PROVIDER" {
				id := fmt.Sprint(typed["resourceId"])
				if id != "" && id != "<nil>" {
					if _, exists := seen[id]; !exists {
						seen[id] = struct{}{}
						result = append(result, id)
					}
				}
			}
			for _, item := range typed {
				visit(item)
			}
		case []any:
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(value)
	return result
}

func intersectStrings(left, right []string) []string {
	available := make(map[string]struct{}, len(right))
	for _, value := range right {
		available[value] = struct{}{}
	}
	result := make([]string, 0)
	for _, value := range left {
		if _, ok := available[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func (s *Service) registerResources(server *mcp.Server) {
	registerResourceGet(server, s, "campaign_get", "campaigns", "Campaign loaded", func(input CampaignResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.CampaignID, "campaignId")
		return input.AccountInput, id, err
	})
	registerResourceGet(server, s, "ad_group_get", "adgroups", "Ad group loaded", func(input AdGroupResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.AdGroupID, "adGroupId")
		return input.AccountInput, id, err
	})
	registerResourceGet(server, s, "keyword_get", "keywords", "Keyword loaded", func(input KeywordResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.KeywordID, "keywordId")
		return input.AccountInput, id, err
	})
	registerResourceGet(server, s, "negative_keyword_get", "negative-keywords", "Negative keyword loaded", func(input NegativeKeywordResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.NegativeKeywordID, "negativeKeywordId")
		return input.AccountInput, id, err
	})
	registerResourceGet(server, s, "ad_get", "ads", "Ad loaded", func(input AdResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.AdID, "adId")
		return input.AccountInput, id, err
	})
	registerResourceGet(server, s, "creative_get", "creatives", "Creative loaded", func(input CreativeResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.CreativeID, "creativeId")
		return input.AccountInput, id, err
	})
	registerResourceGet(server, s, "shared_budget_get", "shared-budgets", "Shared budget loaded", func(input SharedBudgetResourceInput) (AccountInput, string, error) {
		id, err := resolveResourceAlias(input.ID, input.SharedBudgetID, "sharedBudgetId")
		return input.AccountInput, id, err
	})

	addReadTool(server, spec("campaigns_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.CampaignID, "campaignId")
		if err != nil {
			return failed(err)
		}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "campaigns", "Campaigns loaded", []QueryFilterInput{shortcutEquals("id", id)}, nil)
	})
	addReadTool(server, spec("ad_groups_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.AdGroupID, "adGroupId")
		if err != nil {
			return failed(err)
		}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "adgroups", "Ad groups loaded", []QueryFilterInput{shortcutEquals("id", id), shortcutEquals("campaignId", input.CampaignID)}, nil)
	})
	addReadTool(server, spec("keywords_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input KeywordQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.KeywordID, "keywordId")
		if err != nil {
			return failed(err)
		}
		shortcuts := []QueryFilterInput{shortcutEquals("id", id), shortcutEquals("campaignId", input.CampaignID), shortcutEquals("adGroupId", input.AdGroupID)}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "keywords", "Keywords loaded", shortcuts, validateKeywordQueryScope)
	})
	addReadTool(server, spec("negative_keywords_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input NegativeKeywordQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.NegativeKeywordID, "negativeKeywordId")
		if err != nil {
			return failed(err)
		}
		shortcuts := []QueryFilterInput{shortcutEquals("id", id), shortcutEquals("campaignId", input.CampaignID), shortcutEquals("adGroupId", input.AdGroupID)}
		if strings.TrimSpace(input.CampaignID) != "" && strings.TrimSpace(input.AdGroupID) == "" && strings.TrimSpace(id) == "" {
			shortcuts = append(shortcuts, QueryFilterInput{Field: "adGroupId", Operator: "IS_NULL"})
		}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "negative-keywords", "Negative keywords loaded", shortcuts, validateNegativeKeywordQueryScope)
	})
	addReadTool(server, spec("ads_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input AdQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.AdID, "adId")
		if err != nil {
			return failed(err)
		}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "ads", "Ads loaded", []QueryFilterInput{shortcutEquals("id", id), shortcutEquals("campaignId", input.CampaignID), shortcutEquals("adGroupId", input.AdGroupID)}, nil)
	})
	addReadTool(server, spec("creatives_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input CreativeQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.CreativeID, "creativeId")
		if err != nil {
			return failed(err)
		}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "creatives", "Creatives loaded", []QueryFilterInput{shortcutEquals("id", id)}, nil)
	})
	addReadTool(server, spec("shared_budgets_query"), func(ctx context.Context, _ *mcp.CallToolRequest, input SharedBudgetQueryInput) (*mcp.CallToolResult, Output, error) {
		id, err := resolveOptionalAlias(input.ID, input.SharedBudgetID, "sharedBudgetId")
		if err != nil {
			return failed(err)
		}
		return s.resourceQuery(ctx, input.AccountInput, input.QueryInput, "shared-budgets", "Shared budgets loaded without invoice contacts", []QueryFilterInput{shortcutEquals("id", id)}, nil)
	})
	addReadTool(server, spec("campaign_inventory"), s.campaignInventory)
}

func registerResourceGet[In any](server *mcp.Server, service *Service, name, resource, summary string, resolve func(In) (AccountInput, string, error)) {
	addReadTool(server, spec(name), func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Output, error) {
		account, id, err := resolve(input)
		if err != nil {
			return failed(err)
		}
		if err := validateAccount(account); err != nil {
			return failed(err)
		}
		op, err := appleads.ResourceGet(resource, id)
		if err != nil {
			return failed(err)
		}
		result, err := service.manager.Do(ctx, account.Profile, account.AdAccountID, op)
		return handled(summary, result, err)
	})
}

func resolveResourceAlias(legacy, alias, name string) (string, error) {
	id, err := resolveOptionalAlias(legacy, alias, name)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("id or %s is required", name)
	}
	return id, nil
}

func resolveOptionalAlias(legacy, alias, name string) (string, error) {
	legacy = strings.TrimSpace(legacy)
	alias = strings.TrimSpace(alias)
	if legacy != "" && alias != "" && legacy != alias {
		return "", fmt.Errorf("id and %s must match when both are provided", name)
	}
	if alias != "" {
		return alias, nil
	}
	return legacy, nil
}

func shortcutEquals(field, value string) QueryFilterInput {
	if strings.TrimSpace(value) == "" {
		return QueryFilterInput{}
	}
	return QueryFilterInput{Field: field, Operator: "EQUALS", Value: value}
}

func addShortcutFilters(input QueryInput, shortcuts []QueryFilterInput) (QueryInput, error) {
	existing := make(map[string]struct{}, len(input.Filters))
	for _, filter := range input.Filters {
		existing[strings.TrimSpace(filter.Field)] = struct{}{}
	}
	for _, shortcut := range shortcuts {
		if shortcut.Field == "" {
			continue
		}
		if shortcut.Operator != "IS_NULL" && shortcut.Value == nil {
			continue
		}
		if values, ok := shortcut.Value.(string); ok && strings.TrimSpace(values) == "" {
			continue
		}
		if _, conflict := existing[shortcut.Field]; conflict {
			return QueryInput{}, fmt.Errorf("%s cannot be provided both as a shortcut and in filters", shortcut.Field)
		}
		existing[shortcut.Field] = struct{}{}
		input.Filters = append(input.Filters, shortcut)
	}
	return input, nil
}

func (s *Service) resourceQuery(ctx context.Context, account AccountInput, input QueryInput, resource, summary string, shortcuts []QueryFilterInput, validateScope func([]QueryFilterInput) error) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(account); err != nil {
		return failed(err)
	}
	input.AccountInput = account
	merged, err := addShortcutFilters(input, shortcuts)
	if err != nil {
		return failed(err)
	}
	if validateScope != nil {
		if err := validateScope(merged.Filters); err != nil {
			return failed(err)
		}
	}
	request, err := merged.boundedRequest()
	if err != nil {
		return failed(err)
	}
	if err := normalizeRequestIDFilters(request, map[string]struct{}{"id": {}}); err != nil {
		return failed(err)
	}
	op, err := appleads.ResourceQuery(resource, request)
	if err != nil {
		return failed(err)
	}
	result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, op)
	if err != nil {
		return failed(err)
	}
	if resource == "ads" && resultDataEmpty(result.Data) {
		summary += "; no explicit Ad objects were returned, which is valid for Search Results inventory"
	}
	return handled(summary, result, nil)
}

func validateKeywordQueryScope(filters []QueryFilterInput) error {
	hasScope := false
	for index, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		operator := strings.ToUpper(strings.TrimSpace(filter.Operator))
		switch field {
		case "id":
			if err := validateQueryIDScopeFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 0); err != nil {
				return err
			}
			hasScope = true
		case "campaignId":
			if err := validateQueryIDScopeFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true}, 0); err != nil {
				return err
			}
			hasScope = true
		case "adGroupId":
			if err := validateQueryIDScopeFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 1000); err != nil {
				return err
			}
			hasScope = true
		case "text":
			if err := validateQueryStringFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "STARTS_WITH": true}, 80, nil); err != nil {
				return err
			}
		case "matchType":
			if err := validateQueryStringFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 0, map[string]bool{"EXACT": true, "BROAD": true}); err != nil {
				return err
			}
		case "status":
			if err := validateQueryStringFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 0, map[string]bool{"ENABLED": true, "PAUSED": true}); err != nil {
				return err
			}
		case "deleted":
			if operator != "EQUALS" {
				return fmt.Errorf("filters[%d].operator %s is not supported for %s", index, operator, field)
			}
			if _, ok := filter.Value.(bool); !ok {
				return fmt.Errorf("filters[%d].value must be a boolean for %s EQUALS", index, field)
			}
		default:
			return fmt.Errorf("filters[%d].field %q is not supported by keywords_query", index, filter.Field)
		}
	}
	if hasScope {
		return nil
	}
	return errors.New("keywords_query requires id, campaignId, or adGroupId as a shortcut or filter")
}

func validateNegativeKeywordQueryScope(filters []QueryFilterInput) error {
	hasID := false
	hasCampaign := false
	adGroupOperator := ""
	for index, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		operator := strings.ToUpper(strings.TrimSpace(filter.Operator))
		switch field {
		case "id":
			if err := validateQueryIDScopeFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 0); err != nil {
				return err
			}
			hasID = true
		case "campaignId":
			if err := validateQueryIDScopeFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true}, 0); err != nil {
				return err
			}
			hasCampaign = true
		case "adGroupId":
			allowed := map[string]bool{"EQUALS": true, "IN": true, "NOT_EQUALS": true, "IS_NULL": true, "IS_NOT_NULL": true}
			if operator == "IS_NULL" || operator == "IS_NOT_NULL" {
				if filter.Value != nil {
					return fmt.Errorf("filters[%d].value must be omitted for %s", index, operator)
				}
			} else if err := validateQueryIDScopeFilter(index, field, operator, filter.Value, allowed, 1000); err != nil {
				return err
			}
			if !allowed[operator] {
				return fmt.Errorf("filters[%d].operator %s is not supported for %s", index, operator, field)
			}
			adGroupOperator = operator
		case "text":
			if err := validateQueryStringFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "STARTS_WITH": true}, 80, nil); err != nil {
				return err
			}
		case "matchType":
			if err := validateQueryStringFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 0, map[string]bool{"EXACT": true, "BROAD": true}); err != nil {
				return err
			}
		case "status":
			if err := validateQueryStringFilter(index, field, operator, filter.Value, map[string]bool{"EQUALS": true, "IN": true}, 0, map[string]bool{"ENABLED": true, "PAUSED": true}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("filters[%d].field %q is not supported by negative_keywords_query", index, filter.Field)
		}
	}
	if hasID || adGroupOperator == "EQUALS" || adGroupOperator == "IN" {
		return nil
	}
	if hasCampaign && (adGroupOperator == "IS_NULL" || adGroupOperator == "IS_NOT_NULL" || adGroupOperator == "NOT_EQUALS") {
		return nil
	}
	return errors.New("negative_keywords_query requires id, adGroupId, or campaignId with an adGroupId scope; campaign-level queries use adGroupId IS_NULL")
}

func validateQueryStringFilter(index int, field, operator string, value any, allowed map[string]bool, maxLength int, valuesAllowed map[string]bool) error {
	if !allowed[operator] {
		return fmt.Errorf("filters[%d].operator %s is not supported for %s", index, operator, field)
	}
	values, list, err := queryStringFilterValues(value)
	if err != nil {
		return fmt.Errorf("filters[%d].value %w", index, err)
	}
	if operator == "IN" {
		if !list || len(values) == 0 {
			return fmt.Errorf("filters[%d].value must be a non-empty string array for %s IN", index, field)
		}
		if len(values) > 1000 {
			return fmt.Errorf("filters[%d].value supports at most 1000 values for %s IN", index, field)
		}
	} else if list || len(values) != 1 {
		return fmt.Errorf("filters[%d].value must be one string for %s %s", index, field, operator)
	}
	for _, item := range values {
		if item == "" {
			return fmt.Errorf("filters[%d].value must not contain empty strings", index)
		}
		if maxLength > 0 && len([]rune(item)) > maxLength {
			return fmt.Errorf("filters[%d].value strings must not exceed %d characters for %s", index, maxLength, field)
		}
		if valuesAllowed != nil && !valuesAllowed[item] {
			return fmt.Errorf("filters[%d].value %q is not supported for %s", index, item, field)
		}
	}
	return nil
}

func queryStringFilterValues(value any) ([]string, bool, error) {
	switch typed := value.(type) {
	case string:
		return []string{strings.TrimSpace(typed)}, false, nil
	case []string:
		result := make([]string, len(typed))
		for index, item := range typed {
			result[index] = strings.TrimSpace(item)
		}
		return result, true, nil
	case []any:
		result := make([]string, len(typed))
		for index, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, true, errors.New("must contain only strings")
			}
			result[index] = strings.TrimSpace(text)
		}
		return result, true, nil
	default:
		return nil, false, errors.New("must be a string or string array")
	}
}

func validateQueryIDScopeFilter(index int, field, operator string, value any, allowed map[string]bool, maxItems int) error {
	if !allowed[operator] {
		return fmt.Errorf("filters[%d].operator %s is not supported for %s", index, operator, field)
	}
	if operator == "IN" {
		count, ok := queryIDListLength(value)
		if !ok || count == 0 {
			return fmt.Errorf("filters[%d].value must be a non-empty ID array for %s IN", index, field)
		}
		if maxItems > 0 && count > maxItems {
			return fmt.Errorf("filters[%d].value supports at most %d IDs for %s IN", index, maxItems, field)
		}
		if _, err := numericAdamID(value); err != nil {
			return fmt.Errorf("filters[%d].value must contain positive decimal IDs", index)
		}
		return nil
	}
	if _, list := queryIDListLength(value); list {
		return fmt.Errorf("filters[%d].value must be one positive decimal ID for %s %s", index, field, operator)
	}
	if _, err := numericAdamID(value); err != nil {
		return fmt.Errorf("filters[%d].value must be one positive decimal ID for %s %s", index, field, operator)
	}
	return nil
}

func queryIDListLength(value any) (int, bool) {
	switch typed := value.(type) {
	case []any:
		return len(typed), true
	case []string:
		return len(typed), true
	default:
		return 0, false
	}
}

func (s *Service) appOpportunities(ctx context.Context, _ *mcp.CallToolRequest, input AppOpportunityInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	if strings.TrimSpace(input.AdamID) == "" {
		return failed(fmt.Errorf("adamId is required"))
	}
	if len(input.CountriesOrRegions) == 0 {
		return failed(fmt.Errorf("at least one countryOrRegion is required"))
	}
	if err := validateTextValues("countriesOrRegions", input.CountriesOrRegions, 50, 8); err != nil {
		return failed(err)
	}
	if err := validateTextValues("terms", input.Terms, 50, 128); err != nil {
		return failed(err)
	}
	baseFilters := []any{
		map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{input.AdamID}},
		map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
	}
	keywordFilters := append([]any{}, baseFilters...)
	if len(input.CountriesOrRegions) > 0 {
		keywordFilters = append(keywordFilters, map[string]any{"field": "countriesOrRegions", "operator": "IN", "value": input.CountriesOrRegions})
	}
	if len(input.Terms) > 0 {
		keywordFilters = append(keywordFilters, map[string]any{"field": "terms", "operator": "IN", "value": input.Terms})
	}
	ownedOp, err := appleads.SearchApps(appleads.SearchAppsParams{Query: input.AdamID, ReturnOwnedApps: true, Storefronts: input.CountriesOrRegions, Limit: 50})
	if err != nil {
		return failed(err)
	}
	owned, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, ownedOp)
	if err != nil {
		return failed(err)
	}
	if !containsAdamID(owned.Data, input.AdamID) {
		return failed(fmt.Errorf("ownership response from Apple did not confirm adamId %s for this account and storefront selection", input.AdamID))
	}
	adamID, err := numericAdamID(input.AdamID)
	if err != nil {
		return failed(err)
	}
	eligibilityFilters := []any{
		map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamID},
	}
	if len(input.CountriesOrRegions) > 0 {
		eligibilityFilters = append(eligibilityFilters, map[string]any{"field": "countryOrRegion", "operator": "IN", "value": input.CountriesOrRegions})
	}
	eligibilityBody := map[string]any{"filters": eligibilityFilters, "pagination": map[string]any{"offset": 0, "pageSize": 50}}
	data := map[string]any{"ownedApp": opportunitySection(owned.Data, nil)}
	eligibility, eligibilityErr := s.manager.Do(ctx, input.Profile, input.AdAccountID, appleads.AppsEligibility(eligibilityBody))
	data["eligibility"] = opportunitySection(eligibility.Data, eligibilityErr)
	discoveryFilters := append(append([]any{}, baseFilters...), map[string]any{"field": "queryType", "operator": "EQUALS", "value": []string{"SUGGESTION"}})
	targetCPAFilters := append(append([]any{}, baseFilters...), map[string]any{"field": "countryOrRegion", "operator": "IN", "value": input.CountriesOrRegions})
	requests := []struct {
		kind       string
		filters    []any
		pagination bool
	}{
		{kind: "keywords", filters: keywordFilters, pagination: true},
		{kind: "phrases", filters: discoveryFilters, pagination: true},
		{kind: "categories", filters: discoveryFilters, pagination: true},
		{kind: "target-cpas", filters: targetCPAFilters},
	}
	failedKinds := make([]string, 0, len(requests)+1)
	emptyKinds := make([]string, 0, len(requests)+1)
	if eligibilityErr != nil {
		failedKinds = append(failedKinds, "eligibility")
	} else if resultDataEmpty(eligibility.Data) {
		emptyKinds = append(emptyKinds, "eligibility")
	}
	for _, suggestion := range requests {
		request := map[string]any{"filters": suggestion.filters}
		if suggestion.pagination {
			request["pagination"] = map[string]any{"offset": 0, "pageSize": 50}
		}
		op, _ := appleads.Suggestion(suggestion.kind, request)
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		data[suggestion.kind] = opportunitySection(result.Data, err)
		if err != nil {
			failedKinds = append(failedKinds, suggestion.kind)
			continue
		}
		if resultDataEmpty(result.Data) {
			emptyKinds = append(emptyKinds, suggestion.kind)
		}
	}
	bounded, truncated := boundData(data)
	summary := "App eligibility and promotion opportunities loaded"
	if len(failedKinds) > 0 {
		summary += "; unavailable sections: " + strings.Join(failedKinds, ", ")
	}
	if len(emptyKinds) > 0 {
		summary += "; no current result: " + strings.Join(emptyKinds, ", ")
	}
	if truncated {
		summary += "; response arrays were capped at 200 items"
	}
	output := Output{Summary: summary, Data: bounded}
	return textResult(summary, false), output, nil
}

func opportunitySection(data any, err error) map[string]any {
	if err != nil {
		return map[string]any{"status": "upstream_error", "error": errorOutput(err).Error}
	}
	if resultDataEmpty(data) {
		return map[string]any{"status": "empty", "data": data}
	}
	return map[string]any{"status": "ok", "data": data}
}

func (s *Service) campaignAudit(ctx context.Context, _ *mcp.CallToolRequest, input QueryInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	data := make(map[string]any)
	request, err := input.boundedRequest()
	if err != nil {
		return failed(err)
	}
	for _, kind := range []string{"campaigns", "adgroups", "keywords", "searchterms"} {
		op, _ := appleads.Report(kind, queryRequest(request))
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		if err != nil {
			data[kind+"Error"] = errorOutput(err).Error
			continue
		}
		data[kind] = result.Data
	}
	bounded, truncated := boundData(data)
	summary := "Campaign audit baseline loaded"
	if truncated {
		summary += "; response arrays were capped at 200 items"
	}
	output := Output{Summary: summary, Data: bounded}
	return textResult(summary, false), output, nil
}

func (s *Service) queryHandler(summary string, operation func(any) (appleads.Operation, error)) func(context.Context, *mcp.CallToolRequest, QueryInput) (*mcp.CallToolResult, Output, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input QueryInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		request, err := input.boundedRequest()
		if err != nil {
			return failed(err)
		}
		op, err := operation(request)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled(summary, result, err)
	}
}

func (s *Service) reportHandler(summary, kind string) func(context.Context, *mcp.CallToolRequest, QueryInput) (*mcp.CallToolResult, Output, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input QueryInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failed(err)
		}
		request, err := input.reportRequest(kind)
		if err != nil {
			return failed(err)
		}
		op, err := appleads.Report(kind, request)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		return handled(summary, result, err)
	}
}

func (s *Service) keywordReport(ctx context.Context, _ *mcp.CallToolRequest, input KeywordReportInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	request, err := keywordReportRequest(input)
	if err != nil {
		return failed(err)
	}
	op, err := appleads.Report("keywords", request)
	if err != nil {
		return failed(err)
	}
	result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
	if err != nil {
		return failed(err)
	}
	summary := "Keyword report loaded"
	if !input.IncludeZeroMetrics {
		summary += "; zero-impression keywords may be absent, so use keywords_query as the inventory source"
	}
	return handled(summary, result, nil)
}

func keywordReportRequest(input KeywordReportInput) (map[string]any, error) {
	if input.IncludeZeroMetrics {
		options := &QueryOptionsInput{}
		if input.Options != nil {
			options.IncludeRows = append([]string(nil), input.Options.IncludeRows...)
			options.ImpressionShareReportType = input.Options.ImpressionShareReportType
		}
		if !containsFold(options.IncludeRows, "EMPTY_METRICS") {
			options.IncludeRows = append(options.IncludeRows, "EMPTY_METRICS")
		}
		input.Options = options
	}
	return input.reportRequest("keywords")
}

func (s *Service) changeHistory(ctx context.Context, _ *mcp.CallToolRequest, input ChangeHistoryInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	request, err := changeHistoryRequestAt(input, s.nowUTC())
	if err != nil {
		return failed(err)
	}
	result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, appleads.ChangeHistory(request))
	return handled("Change history loaded", result, err)
}

func changeHistoryRequestAt(input ChangeHistoryInput, now time.Time) (map[string]any, error) {
	start, end, err := parseDateWindow(input.Start, input.End, "change history")
	if err != nil {
		return nil, err
	}
	if end.After(addCalendarMonths(start, 6)) {
		return nil, errors.New("change history window must not exceed six months")
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if start.Before(addCalendarMonths(today, -6)) {
		return nil, errors.New("change history start must be within the last six months")
	}
	if len(input.Fields) > 0 || len(input.GroupBy) > 0 || input.TimeRange != nil || input.Options != nil {
		return nil, errors.New("change_history does not accept fields, groupBy, timeRange, or generic options; use start, end, metadata, and timeZone")
	}
	shortcuts := []QueryFilterInput{{Field: "eventTime", Operator: "BETWEEN", Value: []string{input.Start, input.End}}}
	for _, item := range []struct {
		name   string
		field  string
		values []string
	}{
		{"entityTypes", "entityType", input.EntityTypes},
		{"entityIds", "entityId", input.EntityIDs},
		{"eventTypes", "eventType", input.EventTypes},
		{"userTypes", "userType", input.UserTypes},
		{"userIds", "userId", input.UserIDs},
		{"transactionIds", "txnId", input.TransactionIDs},
		{"campaignIds", "campaignId", input.CampaignIDs},
		{"adGroupIds", "adGroupId", input.AdGroupIDs},
		{"adAccountIds", "adAccountId", input.AdAccountIDs},
	} {
		if len(item.values) == 0 {
			continue
		}
		if err := validateTextValues(item.name, item.values, 50, 128); err != nil {
			return nil, err
		}
		shortcuts = append(shortcuts, QueryFilterInput{Field: item.field, Operator: "IN", Value: item.values})
	}
	if err := validateAuditEnum("eventTypes", input.EventTypes, "CREATE", "UPDATE", "DELETE"); err != nil {
		return nil, err
	}
	if err := validateAuditEnum("userTypes", input.UserTypes, "CUSTOMER", "CUSTOMER_API", "APPLE_SUPPORT"); err != nil {
		return nil, err
	}
	merged, err := addShortcutFilters(input.QueryInput, shortcuts)
	if err != nil {
		return nil, err
	}
	request, err := auditQueryRequest(merged)
	if err != nil {
		return nil, err
	}
	metadata := strings.ToLower(strings.TrimSpace(input.Metadata))
	if metadata == "" {
		metadata = "latest"
	}
	if metadata != "none" && metadata != "latest" && metadata != "snapshot" {
		return nil, errors.New("change_history metadata must be none, latest, or snapshot")
	}
	timeZone := strings.ToUpper(strings.TrimSpace(input.TimeZone))
	if timeZone == "" {
		timeZone = "UTC"
	}
	if timeZone != "UTC" && timeZone != "ORTZ" {
		return nil, errors.New("change_history timeZone must be UTC or ORTZ")
	}
	options := map[string]any{"metadata": metadata, "timeZone": timeZone}
	if input.NeedTotals != nil {
		options["needTotals"] = strconv.FormatBool(*input.NeedTotals)
	}
	request["options"] = options
	return request, nil
}

func (s *Service) nowUTC() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func addCalendarMonths(value time.Time, months int) time.Time {
	year, month, day := value.Date()
	target := time.Date(year, month+time.Month(months), 1, value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), value.Location())
	lastDay := time.Date(target.Year(), target.Month()+1, 0, value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), value.Location()).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(target.Year(), target.Month(), day, value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), value.Location())
}

func auditQueryRequest(input QueryInput) (map[string]any, error) {
	if len(input.Filters) > 50 {
		return nil, errors.New("at most 50 audit filters are allowed")
	}
	filters := make([]QueryFilterInput, len(input.Filters))
	copy(filters, input.Filters)
	for index := range filters {
		filters[index].Field = strings.TrimSpace(filters[index].Field)
		filters[index].Operator = strings.ToUpper(strings.TrimSpace(filters[index].Operator))
		if err := validateAuditFilter(&filters[index]); err != nil {
			return nil, fmt.Errorf("filters[%d]: %w", index, err)
		}
	}
	if len(input.Sorting) > 5 {
		return nil, errors.New("at most 5 audit sorting fields are allowed")
	}
	for index, sorting := range input.Sorting {
		field := strings.TrimSpace(sorting.Field)
		if !auditFieldSupported(field) {
			return nil, fmt.Errorf("sorting[%d].field %q is not supported by change history", index, sorting.Field)
		}
		order := strings.ToUpper(strings.TrimSpace(sorting.Order))
		if order != "ASC" && order != "DESC" {
			return nil, fmt.Errorf("sorting[%d].order must be ASC or DESC", index)
		}
		input.Sorting[index].Field = field
		input.Sorting[index].Order = order
	}
	request := map[string]any{"filters": filters}
	if len(input.Sorting) > 0 {
		request["sorting"] = input.Sorting
	}
	if input.Pagination != nil {
		request["pagination"] = map[string]any{"offset": input.Pagination.Offset, "pageSize": input.Pagination.PageSize}
	}
	return boundedQueryRequest(request)
}

func validateAuditFilter(filter *QueryFilterInput) error {
	allowed := map[string]map[string]struct{}{
		"eventTime":   {"BETWEEN": {}},
		"entityType":  {"IN": {}},
		"entityId":    {"EQUALS": {}, "IN": {}},
		"eventType":   {"IN": {}},
		"userType":    {"IN": {}},
		"userId":      {"EQUALS": {}, "IN": {}},
		"txnId":       {"EQUALS": {}, "IN": {}},
		"adAccountId": {"EQUALS": {}, "IN": {}},
		"campaignId":  {"EQUALS": {}, "IN": {}},
		"adGroupId":   {"EQUALS": {}, "IN": {}},
	}
	operators, exists := allowed[filter.Field]
	if !exists {
		return fmt.Errorf("field %q is not supported by change history", filter.Field)
	}
	if _, exists := operators[filter.Operator]; !exists {
		return fmt.Errorf("operator %q is not supported for %s", filter.Operator, filter.Field)
	}
	values, err := auditFilterStrings(filter.Value)
	if err != nil {
		return err
	}
	if filter.Operator == "EQUALS" && len(values) != 1 {
		return errors.New("EQUALS requires one string value")
	}
	if filter.Operator == "EQUALS" {
		filter.Value = values[0]
	} else {
		filter.Value = values
	}
	if filter.Field == "eventTime" {
		if len(values) != 2 {
			return errors.New("eventTime BETWEEN requires two dates")
		}
		for _, value := range values {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return errors.New("eventTime BETWEEN values must use YYYY-MM-DD")
			}
		}
	}
	if filter.Field == "eventType" {
		return validateAuditEnum("eventType", values, "CREATE", "UPDATE", "DELETE")
	}
	if filter.Field == "userType" {
		return validateAuditEnum("userType", values, "CUSTOMER", "CUSTOMER_API", "APPLE_SUPPORT")
	}
	return nil
}

func auditFieldSupported(field string) bool {
	switch field {
	case "eventTime", "entityType", "entityId", "eventType", "userType", "userId", "txnId", "adAccountId", "campaignId", "adGroupId":
		return true
	default:
		return false
	}
}

func auditFilterStrings(value any) ([]string, error) {
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return nil, errors.New("audit filter values must not be empty")
		}
		return []string{typed}, nil
	case []string:
		if err := validateTextValues("audit filter values", typed, 50, 128); err != nil {
			return nil, err
		}
		values := make([]string, len(typed))
		for index, value := range typed {
			values[index] = strings.TrimSpace(value)
			if values[index] == "" {
				return nil, errors.New("audit filter values must not be empty")
			}
		}
		return values, nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("audit filter values must be strings")
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return nil, errors.New("audit filter values must not be empty")
			}
			values = append(values, text)
		}
		if err := validateTextValues("audit filter values", values, 50, 128); err != nil {
			return nil, err
		}
		return values, nil
	default:
		return nil, errors.New("audit filter value must be a string or string array")
	}
}

func validateAuditEnum(name string, values []string, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	for _, value := range values {
		if _, exists := allowedSet[strings.TrimSpace(value)]; !exists {
			return fmt.Errorf("%s contains unsupported value %q", name, value)
		}
	}
	return nil
}

func (s *Service) impressionShare(ctx context.Context, _ *mcp.CallToolRequest, input ImpressionShareInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	body, err := impressionShareRequest(input)
	if err != nil {
		return failed(err)
	}
	op, err := appleads.Insight("impression-share", body)
	if err != nil {
		return failed(err)
	}
	result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
	return handled("Impression share loaded", result, err)
}

func impressionShareRequest(input ImpressionShareInput) (map[string]any, error) {
	if !decimalIDPattern.MatchString(strings.TrimSpace(input.AdamID)) {
		return nil, errors.New("impression_share adamId must be a decimal string")
	}
	start, end, err := parseDateWindow(input.Start, input.End, "impression share")
	if err != nil {
		return nil, err
	}
	granularity := strings.ToUpper(strings.TrimSpace(input.Granularity))
	days := int(end.Sub(start).Hours()/24) + 1
	switch granularity {
	case "DAILY":
		if days > 30 {
			return nil, errors.New("DAILY impression_share windows must not exceed 30 days")
		}
	case "WEEKLY_SUN_SAT":
		if start.Weekday() != time.Sunday {
			return nil, errors.New("WEEKLY_SUN_SAT impression_share windows must start on Sunday")
		}
		if days > 28 {
			return nil, errors.New("WEEKLY_SUN_SAT impression_share windows must not exceed four weeks")
		}
	default:
		return nil, errors.New("impression_share granularity must be DAILY or WEEKLY_SUN_SAT")
	}
	reportType := strings.ToUpper(strings.TrimSpace(input.ReportType))
	if reportType != "FIRST_SLOT" && reportType != "ALL_SLOTS" {
		return nil, errors.New("impression_share reportType must be FIRST_SLOT or ALL_SLOTS")
	}
	filters := []any{map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": strings.TrimSpace(input.AdamID)}}
	if country := strings.ToUpper(strings.TrimSpace(input.Country)); country != "" {
		if !alpha2(country) {
			return nil, errors.New("impression_share country must be an ISO alpha-2 code")
		}
		filters = append(filters, map[string]any{"field": "countryOrRegion", "operator": "EQUALS", "value": country})
	}
	pagination := map[string]any{}
	if input.Pagination != nil {
		pagination["offset"] = input.Pagination.Offset
		pagination["pageSize"] = input.Pagination.PageSize
	}
	boundedPagination, err := boundedQueryRequest(map[string]any{"pagination": pagination})
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"filters":    filters,
		"options":    map[string]any{"impressionShareReportType": reportType},
		"timeRange":  map[string]any{"start": input.Start, "end": input.End, "granularity": granularity},
		"pagination": boundedPagination["pagination"],
	}
	return body, nil
}

func parseDateWindow(startValue, endValue, name string) (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01-02", strings.TrimSpace(startValue))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%s start must use YYYY-MM-DD", name)
	}
	end, err := time.Parse("2006-01-02", strings.TrimSpace(endValue))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%s end must use YYYY-MM-DD", name)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%s end must not be before start", name)
	}
	return start, end, nil
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func (s *Service) campaignInventory(ctx context.Context, _ *mcp.CallToolRequest, input CampaignInventoryInput) (*mcp.CallToolResult, Output, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return failed(err)
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	if input.PageSize < 1 || input.PageSize > MaxItems {
		return failed(fmt.Errorf("pageSize must be between 1 and %d", MaxItems))
	}
	campaignOperation, err := appleads.ResourceGet("campaigns", input.CampaignID)
	if err != nil {
		return failed(err)
	}
	campaign, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, campaignOperation)
	if err != nil {
		return failed(err)
	}
	data := map[string]any{"campaign": campaign.Data}
	adGroupRequest := map[string]any{
		"filters":    []any{map[string]any{"field": "campaignId", "operator": "EQUALS", "value": wireID(input.CampaignID)}},
		"pagination": map[string]any{"offset": 0, "pageSize": input.PageSize},
	}
	adGroupOperation, err := appleads.ResourceQuery("adgroups", adGroupRequest)
	if err != nil {
		return failed(err)
	}
	adGroups, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, adGroupOperation)
	if err != nil {
		return failed(fmt.Errorf("load adGroups inventory: %w", err))
	}
	data["adGroups"] = adGroups.Data
	adGroupIDs := collectFieldValues(adGroups.Data, "id", input.PageSize)
	keywords, err := s.loadAdGroupChildren(ctx, input.AccountInput, "keywords", adGroupIDs, input.PageSize)
	if err != nil {
		return failed(fmt.Errorf("load keywords inventory: %w", err))
	}
	data["keywords"] = keywords
	ads, err := s.loadAdGroupChildren(ctx, input.AccountInput, "ads", adGroupIDs, input.PageSize)
	if err != nil {
		return failed(fmt.Errorf("load ads inventory: %w", err))
	}
	data["ads"] = ads
	campaignNegativeOperation, err := appleads.ResourceQuery("negative-keywords", map[string]any{
		"filters": []any{
			map[string]any{"field": "campaignId", "operator": "EQUALS", "value": wireID(input.CampaignID)},
			map[string]any{"field": "adGroupId", "operator": "IS_NULL"},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": input.PageSize},
	})
	if err != nil {
		return failed(err)
	}
	campaignNegativeKeywords, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, campaignNegativeOperation)
	if err != nil {
		return failed(fmt.Errorf("load campaign negativeKeywords inventory: %w", err))
	}
	negativeKeywords := resultItems(campaignNegativeKeywords.Data, input.PageSize)
	adGroupNegativeKeywords, err := s.loadAdGroupChildren(ctx, input.AccountInput, "negative-keywords", adGroupIDs, input.PageSize-len(negativeKeywords))
	if err != nil {
		return failed(fmt.Errorf("load negativeKeywords inventory: %w", err))
	}
	data["negativeKeywords"] = append(negativeKeywords, adGroupNegativeKeywords...)
	creativeIDs := collectFieldValues(data["ads"], "creativeId", input.PageSize)
	creatives := make([]any, 0, len(creativeIDs))
	for _, creativeID := range creativeIDs {
		op, err := appleads.ResourceGet("creatives", creativeID)
		if err != nil {
			return failed(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		if err != nil {
			return failed(fmt.Errorf("load creative %s: %w", creativeID, err))
		}
		creatives = append(creatives, result.Data)
	}
	data["creatives"] = creatives
	bounded, truncated := boundData(sanitizePublicData(data))
	summary := "Campaign and bounded child inventory loaded"
	if truncated {
		summary += "; response arrays were capped at 200 items"
	}
	output := Output{Summary: summary, Data: bounded}
	return textResult(summary, false), output, nil
}

func (s *Service) loadAdGroupChildren(ctx context.Context, account AccountInput, resource string, adGroupIDs []string, limit int) ([]any, error) {
	items := make([]any, 0)
	for _, adGroupID := range adGroupIDs {
		remaining := limit - len(items)
		if remaining <= 0 {
			break
		}
		request := map[string]any{
			"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": wireID(adGroupID)}},
			"pagination": map[string]any{"offset": 0, "pageSize": remaining},
		}
		operation, err := appleads.ResourceQuery(resource, request)
		if err != nil {
			return nil, err
		}
		result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, operation)
		if err != nil {
			return nil, err
		}
		switch value := result.Data.(type) {
		case []any:
			if len(value) > remaining {
				value = value[:remaining]
			}
			items = append(items, value...)
		case nil:
		default:
			items = append(items, value)
		}
	}
	return items, nil
}

func resultItems(value any, limit int) []any {
	if limit <= 0 || value == nil {
		return nil
	}
	if items, ok := value.([]any); ok {
		if len(items) > limit {
			items = items[:limit]
		}
		return append([]any(nil), items...)
	}
	return []any{value}
}

func collectFieldValues(value any, field string, limit int) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	var visit func(any)
	visit = func(current any) {
		if len(result) >= limit {
			return
		}
		switch typed := current.(type) {
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			if item, ok := typed[field]; ok && item != nil {
				text := fmt.Sprint(item)
				if _, exists := seen[text]; !exists && text != "" {
					seen[text] = struct{}{}
					result = append(result, text)
				}
			}
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(value)
	return result
}

func containsStringValue(value any, expected string) bool {
	switch typed := value.(type) {
	case string:
		return typed == expected
	case []any:
		for _, item := range typed {
			if containsStringValue(item, expected) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if containsStringValue(item, expected) {
				return true
			}
		}
	}
	return false
}

func containsAdamID(value any, adamID string) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if containsAdamID(item, adamID) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if (key == "adamId" || key == "id") && fmt.Sprint(item) == adamID {
				return true
			}
			if containsAdamID(item, adamID) {
				return true
			}
		}
	}
	return false
}

func hasNonASCII(value string) bool {
	for _, character := range value {
		if character > 127 {
			return true
		}
	}
	return false
}

func resultDataEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case []any:
		return len(typed) == 0
	case map[string]any:
		if len(typed) == 0 {
			return true
		}
		for _, field := range []string{"result", "rows", "items"} {
			if nested, exists := typed[field]; exists {
				return resultDataEmpty(nested)
			}
		}
	}
	return false
}

func exactOwnedAppMatches(value any, query string) []any {
	query = strings.TrimSpace(query)
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0)
	for _, item := range items {
		app, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringField(app, "appName"))
		if name == "" {
			name = strings.TrimSpace(stringField(app, "name"))
		}
		if name == query {
			result = append(result, app)
		}
	}
	return result
}

func compactLocaleDetails(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		locale, ok := item.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}
		compact := make(map[string]any)
		for _, field := range []string{"adamId", "language", "languageCode", "isPrimaryLocale", "appName", "subTitle", "promotionalText", "shortDescription", "deviceClasses"} {
			if fieldValue, exists := locale[field]; exists {
				compact[field] = fieldValue
			}
		}
		counts := make(map[string]any)
		if assetsByDevice, ok := locale["assetsByDevice"].(map[string]any); ok {
			for device, rawGroup := range assetsByDevice {
				group, _ := rawGroup.(map[string]any)
				assets, _ := group["assets"].([]any)
				fallbacks, _ := group["appPreviewDeviceFallBackDevices"].([]any)
				counts[device] = map[string]any{"assets": len(assets), "fallbackDevices": len(fallbacks)}
			}
		}
		compact["assetCountsByDevice"] = counts
		result = append(result, compact)
	}
	return result
}

func addReadTool[In any](server *mcp.Server, item Spec, handler mcp.ToolHandlerFor[In, Output]) {
	open := true
	mcp.AddTool(server, &mcp.Tool{
		Name: item.Name, Description: item.Description,
		Annotations: &mcp.ToolAnnotations{Title: strings.ReplaceAll(item.Name, "_", " "), ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &open},
	}, handler)
}

func handled(summary string, result appleads.Result, err error) (*mcp.CallToolResult, Output, error) {
	if err != nil {
		return failed(err)
	}
	output := success(summary, result)
	return textResult(output.Summary, false), output, nil
}

func failed(err error) (*mcp.CallToolResult, Output, error) {
	output := errorOutput(err)
	return failureTextResult(output.Summary, output.Error), output, nil
}

func textResult(summary string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: summary}}, IsError: isError}
}

func failureTextResult(summary string, diagnostic *ErrorOutput) *mcp.CallToolResult {
	data, err := json.Marshal(map[string]any{"summary": summary, "error": diagnostic})
	if err != nil {
		return textResult(summary, true)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, IsError: true}
}

func spec(name string) Spec {
	for _, item := range ReadSpecs() {
		if item.Name == name {
			return item
		}
	}
	panic("missing tool spec: " + name)
}
