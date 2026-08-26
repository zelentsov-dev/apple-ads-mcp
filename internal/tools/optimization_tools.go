package tools

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
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
		now := time.Now()
		evidence, warnings, err := s.optimizationEvidence(ctx, policy, now)
		if err != nil {
			return nil, "", err
		}
		baseline, err := optimization.BuildBaseline(policy, evidence, history, now)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"baseline": baseline, "warnings": warnings}, "Apple Ads optimization baseline built from completed days", nil
	}))
	addReadTool(server, spec("optimization_plan"), s.optimizationPolicyHandler(func(ctx context.Context, policy optimization.Policy, history optimization.History) (any, string, error) {
		now := time.Now()
		evidence, warnings, err := s.optimizationEvidence(ctx, policy, now)
		if err != nil {
			return nil, "", err
		}
		plan, err := optimization.BuildPlan(policy, evidence, history, now)
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
	store, err := s.optimizationHistoryStore()
	if err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	history, err := store.Load(policy.Name)
	if err != nil {
		return optimization.Policy{}, optimization.History{}, err
	}
	return policy, history, nil
}

func (s *Service) optimizationEvidence(ctx context.Context, policy optimization.Policy, now time.Time) ([]optimization.CampaignEvidence, []string, error) {
	evidence := make([]optimization.CampaignEvidence, 0, len(policy.CampaignIDs))
	warnings := make([]string, 0)
	end := now.UTC().AddDate(0, 0, -1)
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
		adGroups, err := s.optimizationBiddables(ctx, policy, "adgroups", campaignID, start, end)
		if err != nil {
			return nil, nil, fmt.Errorf("read ad-group report %s: %w", campaignID, err)
		}
		keywords, err := s.optimizationBiddables(ctx, policy, "keywords", campaignID, start, end)
		if err != nil {
			return nil, nil, fmt.Errorf("read keyword report %s: %w", campaignID, err)
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
	rows, err := s.optimizationReportRows(ctx, policy, func(offset int) (appleads.Operation, error) {
		return optimizationCampaignReportOperationAtOffset(policy, kind, resourceID, start, end, offset)
	})
	if err != nil {
		return nil, err
	}
	return dailyMetrics(rows, resourceID, policy.MaxTotalDailyBudget.Currency)
}

func optimizationCampaignReportOperation(policy optimization.Policy, kind, resourceID string, start, end time.Time) (appleads.Operation, error) {
	return optimizationCampaignReportOperationAtOffset(policy, kind, resourceID, start, end, 0)
}

func optimizationCampaignReportOperationAtOffset(policy optimization.Policy, kind, resourceID string, start, end time.Time, offset int) (appleads.Operation, error) {
	request, err := optimizationCampaignReportRequest(policy, kind, resourceID, start, end, offset)
	if err != nil {
		return appleads.Operation{}, err
	}
	return appleads.Report(kind, request)
}

func optimizationCampaignReportRequest(policy optimization.Policy, kind, resourceID string, start, end time.Time, offset int) (map[string]any, error) {
	request, err := (QueryInput{
		AccountInput: AccountInput{Profile: policy.Profile, AdAccountID: policy.AdAccountID},
		Filters:      []QueryFilterInput{{Field: "id", Operator: "EQUALS", Value: resourceID}},
		Fields:       []string{"localSpend", "impressions", "taps", "tapInstalls"},
		TimeRange:    &TimeRangeInput{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"), Granularity: "DAILY", TimeZone: "UTC"},
		Pagination:   &PaginationInput{Offset: offset, PageSize: MaxItems},
		Options:      &QueryOptionsInput{IncludeRows: []string{"EMPTY_METRICS"}},
	}).reportRequest(kind)
	return request, err
}

func (s *Service) optimizationBiddables(ctx context.Context, policy optimization.Policy, kind, campaignID string, start, end time.Time) ([]optimization.BiddableEvidence, error) {
	rows, err := s.optimizationReportRows(ctx, policy, func(offset int) (appleads.Operation, error) {
		return optimizationBiddableReportOperationAtOffset(policy, kind, campaignID, start, end, offset)
	})
	if err != nil {
		return nil, err
	}
	resourceType := strings.TrimSuffix(kind, "s")
	if kind == "adgroups" {
		resourceType = "ad_group"
	}
	return biddableRows(rows, resourceType, policy.MaxTotalDailyBudget.Currency)
}

func optimizationBiddableReportOperation(policy optimization.Policy, kind, campaignID string, start, end time.Time) (appleads.Operation, error) {
	return optimizationBiddableReportOperationAtOffset(policy, kind, campaignID, start, end, 0)
}

func optimizationBiddableReportOperationAtOffset(policy optimization.Policy, kind, campaignID string, start, end time.Time, offset int) (appleads.Operation, error) {
	request, err := optimizationBiddableReportRequest(policy, kind, campaignID, start, end, offset)
	if err != nil {
		return appleads.Operation{}, err
	}
	return appleads.Report(kind, request)
}

func optimizationBiddableReportRequest(policy optimization.Policy, kind, campaignID string, start, end time.Time, offset int) (map[string]any, error) {
	request, err := (QueryInput{
		AccountInput: AccountInput{Profile: policy.Profile, AdAccountID: policy.AdAccountID},
		Filters:      []QueryFilterInput{{Field: "campaignId", Operator: "EQUALS", Value: campaignID}},
		Fields:       []string{"localSpend", "impressions", "taps", "tapInstalls"},
		TimeRange:    &TimeRangeInput{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"), Granularity: "DAILY", TimeZone: "UTC"},
		Pagination:   &PaginationInput{Offset: offset, PageSize: MaxItems},
		Options:      &QueryOptionsInput{IncludeRows: []string{"EMPTY_METRICS"}},
	}).reportRequest(kind)
	return request, err
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
		ModificationTime:       findStringField(value, "modificationTime"),
	}
}

const maxOptimizationReportRows = 1000

func (s *Service) optimizationReportRows(ctx context.Context, policy optimization.Policy, operation func(int) (appleads.Operation, error)) ([]map[string]any, error) {
	return collectOptimizationReportRows(func(offset int) (appleads.Result, error) {
		op, err := operation(offset)
		if err != nil {
			return appleads.Result{}, err
		}
		return s.manager.Do(ctx, policy.Profile, policy.AdAccountID, op)
	})
}

func collectOptimizationReportRows(fetch func(int) (appleads.Result, error)) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	offset := 0
	for {
		result, err := fetch(offset)
		if err != nil {
			return nil, err
		}
		page, err := reportRows(result.Data)
		if err != nil {
			return nil, err
		}
		if result.Pagination.Total > maxOptimizationReportRows || len(rows)+len(page) > maxOptimizationReportRows {
			return nil, fmt.Errorf("optimization report exceeds the %d-row safety limit", maxOptimizationReportRows)
		}
		rows = append(rows, page...)
		if result.Pagination.Next == "" {
			if result.Pagination.Total > 0 && len(rows) != result.Pagination.Total {
				return nil, fmt.Errorf("optimization report pagination ended after %d of %d rows", len(rows), result.Pagination.Total)
			}
			break
		}
		step := result.Pagination.PageSize
		if step <= 0 {
			step = len(page)
		}
		next := result.Pagination.Offset + step
		if next <= offset || len(page) == 0 {
			return nil, errors.New("optimization report pagination did not advance")
		}
		offset = next
	}
	return rows, nil
}

func reportRows(value any) ([]map[string]any, error) {
	switch typed := value.(type) {
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for index, value := range typed {
			row, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("report row %d must be an object", index+1)
			}
			rows = append(rows, row)
		}
		return rows, nil
	case map[string]any:
		if len(typed) == 0 {
			return nil, nil
		}
		for _, field := range []string{"rows", "result", "data"} {
			if nested, exists := typed[field]; exists {
				return reportRows(nested)
			}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("report result must contain rows; received fields: %s", strings.Join(keys, ", "))
	case nil:
		return nil, nil
	default:
		return nil, errors.New("report result must be an array")
	}
}

func dailyMetrics(rows []map[string]any, expectedID, currency string) ([]optimization.DailyMetric, error) {
	result := make([]optimization.DailyMetric, 0, 28)
	found := false
	for index, row := range rows {
		metadata, ok := row["metadata"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("campaign report row %d is missing metadata", index+1)
		}
		if directString(metadata, "id") != expectedID {
			continue
		}
		found = true
		metrics, err := granularDailyMetrics(row, currency)
		if err != nil {
			return nil, fmt.Errorf("campaign report row %d: %w", index+1, err)
		}
		result = append(result, metrics...)
	}
	if !found {
		return nil, fmt.Errorf("campaign report did not return metadata.id %s", expectedID)
	}
	return result, nil
}

func biddableRows(rows []map[string]any, resourceType, currency string) ([]optimization.BiddableEvidence, error) {
	result := make([]optimization.BiddableEvidence, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		metadata, ok := row["metadata"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("biddable report row %d is missing metadata", index+1)
		}
		id := directString(metadata, "id")
		if id == "" {
			return nil, fmt.Errorf("biddable report row %d is missing metadata.id", index+1)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("biddable report contains duplicate metadata.id %s", id)
		}
		seen[id] = struct{}{}
		item := optimization.BiddableEvidence{
			ResourceType: resourceType, ResourceID: id,
			Name:   firstNonEmptyTool(directString(metadata, "name"), directString(metadata, "text")),
			Status: strings.ToUpper(directString(metadata, "status")),
		}
		if value, exists := metadata["automatedKeywordsOptIn"]; exists {
			searchMatch, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("biddable %s automatedKeywordsOptIn must be boolean", id)
			}
			item.SearchMatch = searchMatch
		}
		bidValue, hasBid := metadata["bid"]
		if !hasBid {
			if strategy, ok := metadata["bidStrategy"].(map[string]any); ok {
				bidValue, hasBid = strategy["bid"]
			}
		}
		if hasBid && bidValue != nil {
			bid, err := parseMoney(bidValue, currency, false)
			if err != nil {
				return nil, fmt.Errorf("biddable %s bid: %w", id, err)
			}
			if err := bid.ValidatePositive(); err != nil {
				return nil, fmt.Errorf("biddable %s bid: %w", id, err)
			}
			item.Bid = &bid
		}
		metrics, err := granularDailyMetrics(row, currency)
		if err != nil {
			return nil, fmt.Errorf("biddable %s: %w", id, err)
		}
		item.Daily = metrics
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ResourceID < result[j].ResourceID })
	return result, nil
}

func granularDailyMetrics(row map[string]any, currency string) ([]optimization.DailyMetric, error) {
	values, ok := row["granularMetrics"].([]any)
	if !ok {
		return nil, errors.New("granularMetrics must be an array")
	}
	result := make([]optimization.DailyMetric, 0, len(values))
	for index, value := range values {
		metric, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("granular metric %d must be an object", index+1)
		}
		date := directString(metric, "date")
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return nil, fmt.Errorf("granular metric %d has invalid date", index+1)
		}
		requestedFields := []string{"localSpend", "taps", "impressions", "tapInstalls"}
		present := 0
		missing := make([]string, 0, len(requestedFields))
		for _, field := range requestedFields {
			value, exists := metric[field]
			if exists && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" && fmt.Sprint(value) != "<nil>" {
				present++
				continue
			}
			missing = append(missing, field)
		}
		if present == 0 {
			result = append(result, optimization.DailyMetric{Date: appleads.Date(date), Spend: appleads.Money{Amount: "0", Currency: currency}})
			continue
		}
		if present != len(requestedFields) {
			return nil, fmt.Errorf("granular metric %d is partially empty; missing %s", index+1, strings.Join(missing, ", "))
		}
		spend, err := parseMoney(metric["localSpend"], currency, false)
		if err != nil {
			return nil, fmt.Errorf("granular metric %d localSpend: %w", index+1, err)
		}
		taps, err := optimization.ParseCount(metric["taps"], "taps")
		if err != nil {
			return nil, err
		}
		impressions, err := optimization.ParseCount(metric["impressions"], "impressions")
		if err != nil {
			return nil, err
		}
		installs, err := optimization.ParseCount(metric["tapInstalls"], "tapInstalls")
		if err != nil {
			return nil, err
		}
		result = append(result, optimization.DailyMetric{Date: appleads.Date(date), Spend: spend, Taps: taps, Impressions: impressions, TapInstalls: installs})
	}
	return result, nil
}

func parseMoney(value any, fallbackCurrency string, missingIsZero bool) (appleads.Money, error) {
	if value == nil {
		if missingIsZero {
			return appleads.Money{Amount: "0", Currency: fallbackCurrency}, nil
		}
		return appleads.Money{}, errors.New("money value is missing")
	}
	if wrapper, ok := value.(map[string]any); ok {
		if nested, ok := wrapper["value"].(map[string]any); ok {
			wrapper = nested
		}
		money := appleads.Money{Amount: directString(wrapper, "amount"), Currency: strings.ToUpper(firstNonEmptyTool(directString(wrapper, "currency"), fallbackCurrency))}
		if err := money.Validate(); err != nil {
			return appleads.Money{}, err
		}
		amount, ok := new(big.Rat).SetString(money.Amount)
		if !ok || amount.Sign() < 0 {
			return appleads.Money{}, errors.New("money amount must be non-negative")
		}
		if money.Currency != fallbackCurrency {
			return appleads.Money{}, fmt.Errorf("currency %s does not match account currency %s", money.Currency, fallbackCurrency)
		}
		return money, nil
	}
	money := appleads.Money{Amount: strings.TrimSpace(fmt.Sprint(value)), Currency: fallbackCurrency}
	if err := money.Validate(); err != nil {
		return appleads.Money{}, err
	}
	amount, ok := new(big.Rat).SetString(money.Amount)
	if !ok || amount.Sign() < 0 {
		return appleads.Money{}, errors.New("money amount must be non-negative")
	}
	return money, nil
}

func directString(value map[string]any, field string) string {
	current, exists := value[field]
	if !exists || current == nil {
		return ""
	}
	result := fmt.Sprint(current)
	if result == "<nil>" {
		return ""
	}
	return result
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
