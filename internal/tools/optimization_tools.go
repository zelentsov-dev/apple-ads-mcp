package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

func (s *Service) registerOptimizationReadTools(server *mcp.Server) {
	addReadTool(server, spec("optimization_policies_list"), func(context.Context, *mcp.CallToolRequest, NoInput) (*mcp.CallToolResult, Output, error) {
		policies, source, err := optimization.LoadPoliciesOptional(s.policyPath)
		if err != nil {
			return failed(err)
		}
		items := make([]any, 0, len(policies.Policies))
		for _, policy := range policies.Policies {
			items = append(items, map[string]any{
				"name": policy.Name, "profile": policy.Profile, "adAccountId": policy.AdAccountID,
				"promotedObjectId": policy.PromotedObjectID, "mode": policy.Mode, "campaignCount": len(policy.CampaignIDs),
			})
		}
		output := Output{Summary: fmt.Sprintf("Found %d local optimization policy or policies", len(items)), Data: map[string]any{"source": publicLocalSource(source), "policies": items}}
		return textResult(output.Summary, false), output, nil
	})
	addReadTool(server, spec("optimization_policy_get"), s.optimizationPolicyHandler(func(_ context.Context, policy optimization.Policy, _ optimization.History) (any, string, error) {
		return policy, "Optimization policy loaded with resolved balanced thresholds", nil
	}))
	addReadTool(server, spec("optimization_baseline"), s.optimizationPolicyHandler(func(ctx context.Context, policy optimization.Policy, history optimization.History) (any, string, error) {
		evidence, warnings, err := s.optimizationEvidence(ctx, policy)
		if err != nil {
			return nil, "", err
		}
		baseline, err := optimization.BuildBaseline(policy, evidence, history, time.Now())
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"baseline": baseline, "warnings": warnings}, "Apple Ads optimization baseline built from completed days", nil
	}))
	addReadTool(server, spec("optimization_plan"), s.optimizationPolicyHandler(func(ctx context.Context, policy optimization.Policy, history optimization.History) (any, string, error) {
		evidence, warnings, err := s.optimizationEvidence(ctx, policy)
		if err != nil {
			return nil, "", err
		}
		plan, err := optimization.BuildPlan(policy, evidence, history, time.Now())
		if err != nil {
			return nil, "", err
		}
		plan.Warnings = append(plan.Warnings, warnings...)
		return plan, fmt.Sprintf("Optimization plan built with %d bounded action(s); no receipt was created", len(plan.Actions)), nil
	}))
	addReadTool(server, spec("optimization_history"), s.optimizationPolicyHandler(func(_ context.Context, _ optimization.Policy, history optimization.History) (any, string, error) {
		return history, fmt.Sprintf("Loaded %d bounded local optimization history entries", len(history.Entries)), nil
	}))
}

func (s *Service) optimizationPolicyHandler(handler func(context.Context, optimization.Policy, optimization.History) (any, string, error)) func(context.Context, *mcp.CallToolRequest, OptimizationPolicyInput) (*mcp.CallToolResult, Output, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input OptimizationPolicyInput) (*mcp.CallToolResult, Output, error) {
		policy, history, err := s.resolveOptimizationPolicy(input)
		if err != nil {
			return failed(err)
		}
		data, summary, err := handler(ctx, policy, history)
		if err != nil {
			return failed(err)
		}
		bounded, truncated := boundData(data)
		if truncated {
			summary += "; response arrays were capped at 200 items"
		}
		output := Output{Summary: summary, Data: bounded}
		return textResult(summary, false), output, nil
	}
}

func (s *Service) resolveOptimizationPolicy(input OptimizationPolicyInput) (optimization.Policy, optimization.History, error) {
	if err := validateAccount(input.AccountInput); err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	policies, _, err := optimization.LoadPolicies(s.policyPath)
	if err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	policy, err := policies.Resolve(input.Policy)
	if err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	if !strings.EqualFold(policy.Profile, input.Profile) || policy.AdAccountID != input.AdAccountID {
		return optimization.Policy{}, optimization.History{}, errors.New("explicit profile and adAccountId do not match the named optimization policy")
	}
	store, err := optimization.NewHistoryStore(s.historyRoot)
	if err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	history, err := store.Load(policy.Name)
	if err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	return policy, history, nil
}

func (s *Service) optimizationEvidence(ctx context.Context, policy optimization.Policy) ([]optimization.CampaignEvidence, []string, error) {
	evidence := make([]optimization.CampaignEvidence, 0, len(policy.CampaignIDs))
	warnings := make([]string, 0)
	end := time.Now().UTC().AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -27)
	for _, campaignID := range policy.CampaignIDs {
		campaignOperation, err := appleads.ResourceGet("campaigns", campaignID)
		if err != nil {
			return nil, nil, err
		}
		campaignResult, err := s.manager.Do(ctx, policy.Profile, policy.AdAccountID, campaignOperation)
		if err != nil {
			return nil, nil, fmt.Errorf("read campaign %s: %w", campaignID, err)
		}
		campaign := campaignEvidenceFromObject(campaignID, campaignResult.Data)
		if campaign.DailyBudget.Currency != policy.MaxTotalDailyBudget.Currency {
			return nil, nil, fmt.Errorf("campaign %s currency %q does not match policy currency %q", campaignID, campaign.DailyBudget.Currency, policy.MaxTotalDailyBudget.Currency)
		}
		campaign.Daily, err = s.optimizationReport(ctx, policy, "campaigns", campaignID, start, end)
		if err != nil {
			return nil, nil, fmt.Errorf("read campaign report %s: %w", campaignID, err)
		}
		adGroups, adGroupWarning := s.optimizationBiddables(ctx, policy, "adgroups", campaignID, start, end)
		if adGroupWarning != "" {
			warnings = append(warnings, adGroupWarning)
		}
		keywords, keywordWarning := s.optimizationBiddables(ctx, policy, "keywords", campaignID, start, end)
		if keywordWarning != "" {
			warnings = append(warnings, keywordWarning)
		}
		campaign.Biddables = append(adGroups, keywords...)
		for _, item := range adGroups {
			if item.ResourceType == "ad_group" && item.SearchMatch {
				campaign.SearchMatch = true
			}
		}
		campaign.MaxConversionsEligible = campaign.MaxConversionsEligible && campaign.Placement == "APPSTORE_SEARCH_RESULTS" && campaign.SearchMatch
		evidence = append(evidence, campaign)
	}
	if err := s.attachOptimizationRecommendations(ctx, policy, evidence); err != nil {
		warnings = append(warnings, "Apple recommendations were unavailable: "+publicErrorMessage(err))
	}
	return evidence, warnings, nil
}

func (s *Service) optimizationReport(ctx context.Context, policy optimization.Policy, kind, resourceID string, start, end time.Time) ([]optimization.DailyMetric, error) {
	operation, err := optimizationCampaignReportOperation(policy, kind, resourceID, start, end)
	if err != nil {
		return nil, err
	}
	result, err := s.manager.Do(ctx, policy.Profile, policy.AdAccountID, operation)
	if err != nil {
		return nil, err
	}
	return dailyMetrics(result.Data, policy.MaxTotalDailyBudget.Currency), nil
}

func optimizationCampaignReportOperation(policy optimization.Policy, kind, resourceID string, start, end time.Time) (appleads.Operation, error) {
	request, err := (QueryInput{
		AccountInput: AccountInput{Profile: policy.Profile, AdAccountID: policy.AdAccountID},
		Filters:      []QueryFilterInput{{Field: "id", Operator: "EQUALS", Value: resourceID}},
		Fields:       []string{"localSpend", "impressions", "taps", "tapInstalls"},
		TimeRange:    &TimeRangeInput{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"), Granularity: "DAILY", TimeZone: "UTC"},
		Pagination:   &PaginationInput{Offset: 0, PageSize: MaxItems},
	}).reportRequest(kind)
	if err != nil {
		return appleads.Operation{}, err
	}
	operation, err := appleads.Report(kind, request)
	return operation, err
}

func (s *Service) optimizationBiddables(ctx context.Context, policy optimization.Policy, kind, campaignID string, start, end time.Time) ([]optimization.BiddableEvidence, string) {
	operation, err := optimizationBiddableReportOperation(policy, kind, campaignID, start, end)
	if err != nil {
		return nil, kind + " report validation failed: " + err.Error()
	}
	result, err := s.manager.Do(ctx, policy.Profile, policy.AdAccountID, operation)
	if err != nil {
		return nil, kind + " report unavailable: " + publicErrorMessage(err)
	}
	resourceType := strings.TrimSuffix(kind, "s")
	if kind == "adgroups" {
		resourceType = "ad_group"
	}
	return biddableRows(result.Data, resourceType, policy.MaxTotalDailyBudget.Currency), ""
}

func optimizationBiddableReportOperation(policy optimization.Policy, kind, campaignID string, start, end time.Time) (appleads.Operation, error) {
	fields := []string{"id", "campaignId", "status", "localSpend", "impressions", "taps", "tapInstalls"}
	if kind == "adgroups" {
		fields = append(fields, "name", "bidStrategy", "automatedKeywordsOptIn")
	} else {
		fields = append(fields, "text", "bid")
	}
	request, err := (QueryInput{
		AccountInput: AccountInput{Profile: policy.Profile, AdAccountID: policy.AdAccountID},
		Filters:      []QueryFilterInput{{Field: "campaignId", Operator: "EQUALS", Value: campaignID}},
		Fields:       fields,
		TimeRange:    &TimeRangeInput{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"), Granularity: "DAILY", TimeZone: "UTC"},
		Pagination:   &PaginationInput{Offset: 0, PageSize: MaxItems},
	}).reportRequest(kind)
	if err != nil {
		return appleads.Operation{}, err
	}
	operation, err := appleads.Report(kind, request)
	return operation, err
}

func (s *Service) attachOptimizationRecommendations(ctx context.Context, policy optimization.Policy, evidence []optimization.CampaignEvidence) error {
	request := map[string]any{
		"filters":    []any{map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": wireID(policy.PromotedObjectID)}},
		"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
	}
	for _, kind := range []string{"daily-budgets", "target-cpas"} {
		operation, _ := appleads.Recommendation(kind, request)
		result, err := s.manager.Do(ctx, policy.Profile, policy.AdAccountID, operation)
		if err != nil {
			return err
		}
		for index := range evidence {
			object := findObjectByFields(result.Data, map[string]string{"campaignId": evidence[index].CampaignID})
			if object == nil {
				continue
			}
			field := "suggestedDailyBudgetAmount"
			if kind == "target-cpas" {
				field = "recommendedTargetCPA"
			}
			money, err := moneyFromObject(object[field])
			if err != nil {
				continue
			}
			if kind == "daily-budgets" {
				evidence[index].AppleBudgetRecommendation = &money
			} else {
				evidence[index].AppleTargetCPA = &money
			}
		}
	}
	return nil
}

func campaignEvidenceFromObject(campaignID string, value any) optimization.CampaignEvidence {
	budget := findMapField(value, "dailyBudget")
	if nested := findMapField(budget, "value"); nested != nil {
		budget = nested
	}
	strategy := findMapField(value, "bidStrategy")
	return optimization.CampaignEvidence{
		CampaignID:             campaignID,
		Name:                   findStringField(value, "name"),
		Status:                 strings.ToUpper(findStringField(value, "status")),
		SystemStatus:           strings.ToUpper(findStringField(value, "systemStatus")),
		Placement:              strings.ToUpper(firstNonEmptyTool(findStringField(value, "supplyPlacement"), findStringField(value, "adChannelType"))),
		BidStrategy:            strings.ToUpper(firstNonEmptyTool(findStringField(strategy, "bidStrategyType"), findStringField(value, "bidStrategyType"))),
		DailyBudget:            appleads.Money{Amount: findStringField(budget, "amount"), Currency: strings.ToUpper(findStringField(budget, "currency"))},
		MaxConversionsEligible: containsStringValue(value, "MAX_CONVERSIONS") || containsTrueField(value, "maxConversionsEligible"),
	}
}

func dailyMetrics(value any, currency string) []optimization.DailyMetric {
	rows := mapsWithField(value, "date")
	result := make([]optimization.DailyMetric, 0, len(rows))
	for _, row := range rows {
		date := findStringField(row, "date")
		if date == "" {
			continue
		}
		spend := moneyInField(row, "localSpend", currency)
		result = append(result, optimization.DailyMetric{
			Date: appleads.Date(date), Spend: spend, Taps: optimization.ParseInt(row["taps"]),
			Impressions: optimization.ParseInt(row["impressions"]), TapInstalls: optimization.ParseInt(row["tapInstalls"]),
		})
	}
	return result
}

func biddableRows(value any, resourceType, currency string) []optimization.BiddableEvidence {
	rows := mapsWithField(value, "id")
	byID := map[string]*optimization.BiddableEvidence{}
	for _, row := range rows {
		id := findStringField(row, "id")
		date := findStringField(row, "date")
		if id == "" || date == "" {
			continue
		}
		item := byID[id]
		if item == nil {
			item = &optimization.BiddableEvidence{ResourceType: resourceType, ResourceID: id, Name: firstNonEmptyTool(findStringField(row, "name"), findStringField(row, "text")), Status: strings.ToUpper(findStringField(row, "status"))}
			item.SearchMatch, _ = row["automatedKeywordsOptIn"].(bool)
			if _, exists := row["bid"]; exists {
				bid := moneyInField(row, "bid", currency)
				item.Bid = &bid
			} else if strategy := findMapField(row, "bidStrategy"); strategy != nil {
				if _, exists := strategy["bid"]; exists {
					bid := moneyInField(strategy, "bid", currency)
					item.Bid = &bid
				}
			}
			byID[id] = item
		}
		item.Daily = append(item.Daily, optimization.DailyMetric{Date: appleads.Date(date), Spend: moneyInField(row, "localSpend", currency), Taps: optimization.ParseInt(row["taps"]), Impressions: optimization.ParseInt(row["impressions"]), TapInstalls: optimization.ParseInt(row["tapInstalls"])})
	}
	result := make([]optimization.BiddableEvidence, 0, len(byID))
	for _, item := range byID {
		result = append(result, *item)
	}
	return result
}

func mapsWithField(value any, field string) []map[string]any {
	result := make([]map[string]any, 0)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if _, exists := typed[field]; exists {
				result = append(result, typed)
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func moneyInField(value map[string]any, field, fallbackCurrency string) appleads.Money {
	current := value[field]
	if object, ok := current.(map[string]any); ok {
		if nested, ok := object["value"].(map[string]any); ok {
			object = nested
		}
		return appleads.Money{Amount: findStringField(object, "amount"), Currency: strings.ToUpper(firstNonEmptyTool(findStringField(object, "currency"), fallbackCurrency))}
	}
	amount := fmt.Sprint(current)
	if current == nil || amount == "<nil>" {
		amount = "0"
	}
	return appleads.Money{Amount: amount, Currency: fallbackCurrency}
}

func firstNonEmptyTool(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func publicLocalSource(path string) string {
	if path == "" {
		return "none"
	}
	return "file"
}

func publicErrorMessage(err error) string {
	var apiError *appleads.APIError
	if errors.As(err, &apiError) {
		return apiError.Error()
	}
	return "request failed"
}
