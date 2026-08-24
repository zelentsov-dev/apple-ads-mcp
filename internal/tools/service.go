package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

type Service struct {
	manager     *appleads.Manager
	allowWrites bool
	version     string
}

func NewService(manager *appleads.Manager, allowWrites bool, version string) *Service {
	return &Service{manager: manager, allowWrites: allowWrites, version: version}
}

func ReadSpecs() []Spec {
	return []Spec{
		{Name: "server_info", Description: "Show server version, safety mode, API family, and response limits.", Class: "read"},
		{Name: "profiles_list", Description: "List configured profile names without exposing credentials.", Class: "read"},
		{Name: "auth_check", Description: "Validate OAuth credentials and return the caller identity.", Class: "read"},
		{Name: "ad_accounts_list", Description: "Discover Apple Ads account ACLs available to a profile.", Class: "read"},
		{Name: "org_get", Description: "Read one Apple Ads organization.", Class: "read"},
		{Name: "apps_search", Description: "Search apps and optionally restrict results to owned apps.", Class: "read"},
		{Name: "apps_get", Description: "Read an App Store app by Adam ID.", Class: "read"},
		{Name: "apps_eligibility", Description: "Check App Store advertising eligibility.", Class: "read"},
		{Name: "product_page_get", Description: "Read a Default or Custom Product Page by ID.", Class: "read"},
		{Name: "product_pages_query", Description: "Query bounded Default and Custom Product Pages.", Class: "read"},
		{Name: "product_page_locales", Description: "Query bounded Product Page locale details.", Class: "read"},
		{Name: "keyword_suggestions", Description: "Get keyword opportunities for an App Store app.", Class: "read"},
		{Name: "phrase_suggestions", Description: "Get phrase opportunities for an App Store app.", Class: "read"},
		{Name: "category_suggestions", Description: "Get category opportunities for an App Store app.", Class: "read"},
		{Name: "target_cpa_suggestions", Description: "Get target CPA suggestions.", Class: "read"},
		{Name: "search_term_popularity", Description: "Read search-term popularity insights.", Class: "read"},
		{Name: "impression_share", Description: "Read app impression-share insights.", Class: "read"},
		{Name: "campaign_report", Description: "Read bounded campaign performance rows.", Class: "read"},
		{Name: "ad_group_report", Description: "Read bounded ad-group performance rows.", Class: "read"},
		{Name: "ad_report", Description: "Read bounded ad performance rows.", Class: "read"},
		{Name: "keyword_report", Description: "Read bounded keyword performance rows.", Class: "read"},
		{Name: "search_term_report", Description: "Read bounded search-term performance rows.", Class: "read"},
		{Name: "daily_budget_recommendations", Description: "Read daily-budget recommendations without applying them.", Class: "read"},
		{Name: "target_cpa_recommendations", Description: "Read target-CPA recommendations without applying them.", Class: "read"},
		{Name: "change_history", Description: "Read bounded Apple Ads change history.", Class: "read"},
		{Name: "account_health", Description: "Collect a read-only identity, access, and account baseline.", Class: "read"},
		{Name: "app_opportunities", Description: "Collect eligibility and suggestion opportunities for an app.", Class: "read"},
		{Name: "campaign_audit", Description: "Collect campaign, ad-group, keyword, and search-term report baselines.", Class: "read"},
		{Name: "campaign_get", Description: "Read one campaign by ID for current-state verification.", Class: "read"},
		{Name: "ad_group_get", Description: "Read one ad group by ID for current-state verification.", Class: "read"},
		{Name: "keyword_get", Description: "Read one targeting keyword by ID for current-state verification.", Class: "read"},
		{Name: "negative_keyword_get", Description: "Read one negative keyword by ID for current-state verification.", Class: "read"},
		{Name: "ad_get", Description: "Read one ad by ID for current-state verification.", Class: "read"},
		{Name: "creative_get", Description: "Read one creative by ID for current-state verification.", Class: "read"},
		{Name: "shared_budget_get", Description: "Read one shared budget by ID for current-state verification.", Class: "read"},
	}
}

func (s *Service) RegisterReadTools(server *mcp.Server) {
	s.registerNoInput(server)
	s.registerAccess(server)
	s.registerApps(server)
	s.registerQueries(server)
	s.registerAudits(server)
	s.registerResources(server)
}

func (s *Service) registerNoInput(server *mcp.Server) {
	addReadTool(server, ReadSpecs()[0], func(context.Context, *mcp.CallToolRequest, NoInput) (*mcp.CallToolResult, Output, error) {
		mode := "read-only"
		if s.allowWrites {
			mode = "writes require profile opt-in and a valid preview receipt"
		}
		output := Output{Summary: "Apple Ads MCP is ready", Data: map[string]any{
			"name": "apple-ads-mcp", "version": s.version, "api": "Apple Ads Platform API v1",
			"baseUrl": appleads.BaseURL, "mode": mode, "maxItemsPerArray": MaxItems,
			"legacyV5": false, "appleMaps": false, "rawRequestTool": false,
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
	addReadTool(server, spec("apps_search"), func(ctx context.Context, _ *mcp.CallToolRequest, input AppsSearchInput) (*mcp.CallToolResult, Output, error) {
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
		return handled("App search completed", result, err)
	})
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
	for _, item := range []struct{ name, summary, kind string }{
		{"search_term_popularity", "Search-term popularity loaded", "search-term-popularity"},
		{"impression_share", "Impression share loaded", "impression-share"},
	} {
		kind := item.kind
		addReadTool(server, spec(item.name), s.queryHandler(item.summary, func(body any) (appleads.Operation, error) { return appleads.Insight(kind, body) }))
	}
	for _, item := range []struct{ name, summary, kind string }{
		{"campaign_report", "Campaign report loaded", "campaigns"},
		{"ad_group_report", "Ad-group report loaded", "adgroups"},
		{"ad_report", "Ad report loaded", "ads"},
		{"keyword_report", "Keyword report loaded", "keywords"},
		{"search_term_report", "Search-term report loaded", "searchterms"},
	} {
		kind := item.kind
		addReadTool(server, spec(item.name), s.queryHandler(item.summary, func(body any) (appleads.Operation, error) { return appleads.Report(kind, body) }))
	}
	for _, item := range []struct{ name, summary, kind string }{
		{"daily_budget_recommendations", "Daily-budget recommendations loaded", "daily-budgets"},
		{"target_cpa_recommendations", "Target-CPA recommendations loaded", "target-cpas"},
	} {
		kind := item.kind
		addReadTool(server, spec(item.name), s.queryHandler(item.summary, func(body any) (appleads.Operation, error) { return appleads.Recommendation(kind, body) }))
	}
	addReadTool(server, spec("change_history"), s.queryHandler("Change history loaded", func(body any) (appleads.Operation, error) { return appleads.ChangeHistory(body), nil }))
}

func (s *Service) registerAudits(server *mcp.Server) {
	addReadTool(server, spec("account_health"), func(ctx context.Context, _ *mcp.CallToolRequest, input AccountInput) (*mcp.CallToolResult, Output, error) {
		if err := validateAccount(input); err != nil {
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
		data := map[string]any{"identity": identity.Data, "accounts": accounts.Data, "selectedAccountRoles": roles}
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
		output := Output{Summary: "Account identity, organization, and access baseline loaded", Data: data}
		return textResult(output.Summary, false), output, nil
	})
	addReadTool(server, spec("app_opportunities"), s.appOpportunities)
	addReadTool(server, spec("campaign_audit"), s.campaignAudit)
}

func (s *Service) registerResources(server *mcp.Server) {
	for _, item := range []struct{ name, resource, summary string }{
		{"campaign_get", "campaigns", "Campaign loaded"},
		{"ad_group_get", "adgroups", "Ad group loaded"},
		{"keyword_get", "keywords", "Keyword loaded"},
		{"negative_keyword_get", "negative-keywords", "Negative keyword loaded"},
		{"ad_get", "ads", "Ad loaded"},
		{"creative_get", "creatives", "Creative loaded"},
		{"shared_budget_get", "shared-budgets", "Shared budget loaded"},
	} {
		resource := item.resource
		summary := item.summary
		addReadTool(server, spec(item.name), func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceInput) (*mcp.CallToolResult, Output, error) {
			if err := validateAccount(input.AccountInput); err != nil {
				return failed(err)
			}
			op, err := appleads.ResourceGet(resource, input.ID)
			if err != nil {
				return failed(err)
			}
			result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
			return handled(summary, result, err)
		})
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
		return failed(fmt.Errorf("Apple did not confirm adamId %s as an owned app for this account and storefront selection", input.AdamID))
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
	eligibility, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, appleads.AppsEligibility(eligibilityBody))
	if err != nil {
		return failed(err)
	}
	data := map[string]any{"ownedApp": owned.Data, "eligibility": eligibility.Data}
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
	failedKinds := make([]string, 0, len(requests))
	emptyKinds := make([]string, 0, len(requests))
	for _, suggestion := range requests {
		request := map[string]any{"filters": suggestion.filters}
		if suggestion.pagination {
			request["pagination"] = map[string]any{"offset": 0, "pageSize": 50}
		}
		op, _ := appleads.Suggestion(suggestion.kind, request)
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, op)
		if err != nil {
			data[suggestion.kind+"Error"] = errorOutput(err).Error
			failedKinds = append(failedKinds, suggestion.kind)
			continue
		}
		data[suggestion.kind] = result.Data
		if result.Data == nil {
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
	return textResult(output.Summary, true), output, nil
}

func textResult(summary string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: summary}}, IsError: isError}
}

func spec(name string) Spec {
	for _, item := range ReadSpecs() {
		if item.Name == name {
			return item
		}
	}
	panic("missing tool spec: " + name)
}
