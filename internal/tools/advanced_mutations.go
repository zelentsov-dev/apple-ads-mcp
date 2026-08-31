package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

func (s *Service) registerAdvancedMutationTools(server *mcp.Server, store *operations.Store) {
	addPreviewTool(server, mutationSpec("campaign_bid_strategy_preview"), s.campaignBidStrategyPreview(store))
	addPreviewTool(server, mutationSpec("optimization_plan_preview"), s.optimizationPlanPreview(store))
	addPreviewTool(server, mutationSpec("shared_budget_create_preview"), s.sharedBudgetCreatePreview(store))
	addPreviewTool(server, mutationSpec("shared_budget_update_preview"), s.sharedBudgetUpdatePreview(store))
	addPreviewTool(server, mutationSpec("campaign_shared_budget_assign_preview"), s.campaignSharedBudgetPreview(store, true))
	addPreviewTool(server, mutationSpec("campaign_shared_budget_unassign_preview"), s.campaignSharedBudgetPreview(store, false))
	for _, item := range []struct {
		name     string
		resource string
	}{
		{"campaign_delete_preview", "campaigns"},
		{"ad_group_delete_preview", "adgroups"},
		{"keyword_delete_preview", "keywords"},
		{"negative_keyword_delete_preview", "negative-keywords"},
		{"ad_delete_preview", "ads"},
		{"creative_delete_preview", "creatives"},
		{"shared_budget_delete_preview", "shared-budgets"},
	} {
		addDeletePreviewTool(server, mutationSpec(item.name), s.deletePreview(store, item.resource, item.name))
	}
}

func (s *Service) campaignBidStrategyPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, CampaignBidStrategyPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignBidStrategyPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		payload, err := typedPayloadMap(CampaignUpdatePayload{BidStrategy: &input.Strategy})
		if err != nil {
			return failedPreview(err)
		}
		if err := validateBidStrategy(payload["bidStrategy"]); err != nil {
			return failedPreview(err)
		}
		if strings.EqualFold(input.Strategy.BidStrategyType, "MAX_CONVERSIONS") {
			if err := s.ensureMaxConversionsEligible(ctx, input.AccountInput, input.CampaignID); err != nil {
				return failedPreview(err)
			}
		}
		return s.updatePreviewPayload(ctx, store, "campaigns", "campaign_bid_strategy", input.AccountInput, input.CampaignID, payload)
	}
}

func (s *Service) optimizationPlanPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, OptimizationPolicyInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input OptimizationPolicyInput) (*mcp.CallToolResult, PreviewOutput, error) {
		policy, history, err := s.resolveOptimizationPolicy(input)
		if err != nil {
			return failedPreview(err)
		}
		if policy.Mode != "active" {
			return failedPreview(errors.New("learning policy cannot create an apply receipt"))
		}
		if err := s.writeAllowed(ctx, input.Profile, input.AdAccountID); err != nil {
			return failedPreview(err)
		}
		now := s.nowUTC()
		evidence, _, err := s.optimizationEvidence(ctx, policy, now)
		if err != nil {
			return failedPreview(err)
		}
		plan, err := optimization.BuildPlan(policy, evidence, history, now)
		if err != nil {
			return failedPreview(err)
		}
		if len(plan.Actions) == 0 {
			return failedPreview(errors.New("optimization plan contains no eligible actions"))
		}
		steps := make([]operations.SequenceStep, 0, len(plan.Actions))
		verify := make([]operations.VerificationRead, 0, len(plan.Actions))
		seenReads := map[string]struct{}{}
		targetIDs := make([]string, 0, len(plan.Actions))
		for _, action := range plan.Actions {
			resource, payload, err := optimizationMutation(action)
			if err != nil {
				return failedPreview(err)
			}
			if err := s.ensureAppStoreMutation(ctx, input.AccountInput, resource, action.ResourceID, payload, false); err != nil {
				return failedPreview(err)
			}
			mutation, _ := appleads.ResourceUpdate(resource, action.ResourceID, payload)
			steps = append(steps, operations.SequenceStep{Item: operations.OperationItemPreview{
				CorrelationID: action.CorrelationID, CampaignID: action.CampaignID,
				ResourceType: action.ResourceType, Action: action.Action, TargetID: action.ResourceID,
				Before: optimizationExpectedBefore(action, resource), After: payload, Reason: action.Reason, DependsOn: action.DependsOn,
			}, Mutation: mutation})
			key := resource + "/" + action.ResourceID
			if _, exists := seenReads[key]; !exists {
				read, _ := appleads.ResourceGet(resource, action.ResourceID)
				impact, err := s.resourceImpact(ctx, input.AccountInput, resource, action.ResourceID, payload, false)
				if err != nil {
					return failedPreview(err)
				}
				scopes := resourceMutationScopes(resource, impact, payload, false)
				scopes = append(scopes, operations.ObjectMutationScope(action.CampaignID))
				verify = append(verify, operations.VerificationRead{Name: strings.ReplaceAll(key, "/", "_"), Operation: read, Scopes: scopes})
				seenReads[key] = struct{}{}
				targetIDs = append(targetIDs, action.ResourceID)
			}
		}
		reportEnd := now.UTC().AddDate(0, 0, -1)
		reportStart := reportEnd.AddDate(0, 0, -27)
		for _, campaignID := range policy.CampaignIDs {
			campaignReport, err := optimizationCampaignReportOperation(policy, "campaigns", campaignID, reportStart, reportEnd)
			if err != nil {
				return failedPreview(err)
			}
			verify = append(verify, operations.VerificationRead{Name: "report_campaign_" + campaignID, Operation: campaignReport})
			for _, kind := range []string{"adgroups", "keywords"} {
				report, err := optimizationBiddableReportOperation(policy, kind, campaignID, reportStart, reportEnd)
				if err != nil {
					return failedPreview(err)
				}
				verify = append(verify, operations.VerificationRead{Name: "report_" + kind + "_" + campaignID, Operation: report})
			}
		}
		preview, err := store.PreviewSequence(ctx, s.manager, input.Profile, input.AdAccountID, "optimization_plan", targetIDs, verify, steps, &operations.OperationImpact{
			SpendAffecting: true, ObjectCount: len(steps), Currency: policy.MaxTotalDailyBudget.Currency,
			MaximumAmount: &policy.MaxTotalDailyBudget, Policy: policy.Name,
		})
		if err != nil {
			return failedPreview(err)
		}
		if err := s.recordOptimizationPreview(preview, plan.Baseline); err != nil {
			return failedPreview(fmt.Errorf("persist optimization preview history: %w", err))
		}
		return previewSuccess(preview)
	}
}

func optimizationExpectedBefore(action optimization.PlanAction, resource string) map[string]any {
	switch action.Action {
	case "budget", "budget_increase", "budget_decrease":
		if money, ok := moneyFromAnyTool(action.Before["dailyBudget"]); ok {
			return map[string]any{"dailyBudget": map[string]any{"value": money}}
		}
	case "bid_increase", "bid_decrease":
		if money, ok := moneyFromAnyTool(action.Before["bid"]); ok {
			if resource == "adgroups" {
				return map[string]any{"bidStrategy": map[string]any{"bid": money}}
			}
			return map[string]any{"bid": money}
		}
	}
	return action.Before
}

func optimizationMutation(action optimization.PlanAction) (string, map[string]any, error) {
	resource := map[string]string{"campaign": "campaigns", "ad_group": "adgroups", "keyword": "keywords"}[action.ResourceType]
	if resource == "" {
		return "", nil, fmt.Errorf("unsupported optimization resource type %q", action.ResourceType)
	}
	switch action.Action {
	case "pause":
		return resource, map[string]any{"status": "PAUSED"}, nil
	case "resume":
		return resource, map[string]any{"status": "ENABLED"}, nil
	case "budget", "budget_increase", "budget_decrease":
		money, ok := moneyFromAnyTool(action.After["dailyBudget"])
		if !ok {
			return "", nil, errors.New("optimization budget action has no typed money")
		}
		return "campaigns", map[string]any{"dailyBudget": map[string]any{"value": money}}, nil
	case "bid_strategy":
		return "campaigns", map[string]any{"bidStrategy": map[string]any{"bidStrategyType": fmt.Sprint(action.After["bidStrategy"])}}, nil
	case "bid_increase", "bid_decrease":
		money, ok := moneyFromAnyTool(action.After["bid"])
		if !ok {
			return "", nil, errors.New("optimization bid action has no typed money")
		}
		if resource == "adgroups" {
			return resource, map[string]any{"bidStrategy": map[string]any{"bid": money}}, nil
		}
		return resource, map[string]any{"bid": money}, nil
	default:
		return "", nil, fmt.Errorf("unsupported optimization action %q", action.Action)
	}
}

func moneyFromAnyTool(value any) (appleads.Money, bool) {
	switch typed := value.(type) {
	case appleads.Money:
		return typed, true
	case map[string]any:
		return appleads.Money{Amount: fmt.Sprint(typed["amount"]), Currency: fmt.Sprint(typed["currency"])}, true
	default:
		return appleads.Money{}, false
	}
}

func (s *Service) ensureMaxConversionsEligible(ctx context.Context, account AccountInput, campaignID string) error {
	if err := s.ensureCampaignPlacement(ctx, account, campaignID, "APPSTORE_SEARCH_RESULTS"); err != nil {
		return err
	}
	campaign, err := s.readResource(ctx, account, "campaigns", campaignID)
	if err != nil {
		return err
	}
	if !containsStringValue(campaign, "MAX_CONVERSIONS") && !containsTrueField(campaign, "maxConversionsEligible") {
		return errors.New("campaign metadata from Apple does not confirm MAX_CONVERSIONS eligibility")
	}
	end := time.Now().UTC().AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -13)
	policy := optimization.Policy{
		Profile: account.Profile, AdAccountID: account.AdAccountID,
		MaxTotalDailyBudget: appleads.Money{Amount: "1", Currency: "USD"},
	}
	accountOperation, _ := appleads.AdAccount(account.AdAccountID)
	accountResult, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, accountOperation)
	if err != nil {
		return err
	}
	policy.MaxTotalDailyBudget.Currency = strings.ToUpper(findStringField(accountResult.Data, "currency"))
	metrics, err := s.optimizationReport(ctx, policy, "campaigns", campaignID, start, end)
	if err != nil {
		return fmt.Errorf("verify MAX_CONVERSIONS performance: %w", err)
	}
	installs := int64(0)
	for _, metric := range metrics {
		installs += metric.TapInstalls
	}
	if len(metrics) < 14 || installs < 70 {
		return errors.New("MAX_CONVERSIONS requires 14 completed reporting days and at least five tap installs per day on average")
	}
	request := map[string]any{
		"filters":    []any{map[string]any{"field": "campaignId", "operator": "EQUALS", "value": wireID(campaignID)}},
		"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
	}
	query, _ := appleads.ResourceQuery("adgroups", request)
	adGroups, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, query)
	if err != nil {
		return err
	}
	if !containsTrueField(adGroups.Data, "automatedKeywordsOptIn") {
		return errors.New("MAX_CONVERSIONS requires an eligible Search Match ad group")
	}
	return nil
}

func (s *Service) sharedBudgetCreatePreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, SharedBudgetCreatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input SharedBudgetCreatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := s.ensureLOC(ctx, input.AccountInput); err != nil {
			return failedPreview(err)
		}
		if err := input.Value.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		billing, privateHash, err := s.resolveBillingProfile(input.BillingProfile)
		if err != nil {
			return failedPreview(err)
		}
		privatePayload := map[string]any{
			"name": input.Name, "startTime": input.StartTime, "value": input.Value,
			"adAccountIds": []any{wireID(input.AdAccountID)}, "invoiceDetail": billing.InvoiceDetail(),
		}
		if input.EndTime != nil {
			privatePayload["endTime"] = *input.EndTime
		}
		if err := s.ensurePayloadCurrency(ctx, input.AccountInput, privatePayload); err != nil {
			return failedPreview(err)
		}
		if err := s.validateWrite(ctx, input.AccountInput, privatePayload); err != nil {
			return failedPreview(err)
		}
		mutation, _ := appleads.ResourceCreate("shared-budgets", privatePayload)
		verification, _ := appleads.ResourceQuery("shared-budgets", map[string]any{
			"filters":    []any{map[string]any{"field": "name", "operator": "EQUALS", "value": input.Name}},
			"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
		})
		publicPayload := map[string]any{"name": input.Name, "startTime": input.StartTime, "value": input.Value, "billingProfile": input.BillingProfile, "privatePayloadHash": privateHash}
		verificationPayload := map[string]any{"name": input.Name, "startTime": input.StartTime, "value": input.Value}
		if input.EndTime != nil {
			publicPayload["endTime"] = *input.EndTime
			verificationPayload["endTime"] = *input.EndTime
		}
		preview, err := store.PreviewComposite(ctx, s.manager, input.Profile, input.AdAccountID, "shared_budget_create", nil, publicPayload, []operations.VerificationRead{{Name: "shared_budget_inventory", Operation: verification, Scopes: []string{inventoryMutationScope("shared-budgets")}}}, mutation, operations.PreviewOptions{Impact: &operations.OperationImpact{SpendAffecting: true, ObjectCount: 1, Currency: input.Value.Currency, PrivateHash: privateHash}, Create: &operations.CreateExpectation{Resource: "shared-budgets", Expected: verificationPayload}})
		if err != nil {
			return failedPreview(err)
		}
		return previewSuccess(preview)
	}
}

func (s *Service) sharedBudgetUpdatePreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, SharedBudgetUpdatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input SharedBudgetUpdatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := s.ensureLOC(ctx, input.AccountInput); err != nil {
			return failedPreview(err)
		}
		payload := map[string]any{}
		publicPayload := map[string]any{}
		privateHash := ""
		if input.Name != nil {
			payload["name"], publicPayload["name"] = *input.Name, *input.Name
		}
		if input.StartTime != nil {
			payload["startTime"], publicPayload["startTime"] = *input.StartTime, *input.StartTime
		}
		if input.EndTime != nil {
			payload["endTime"], publicPayload["endTime"] = *input.EndTime, *input.EndTime
		}
		if input.Value != nil {
			if err := input.Value.ValidatePositive(); err != nil {
				return failedPreview(err)
			}
			payload["value"], publicPayload["value"] = *input.Value, *input.Value
		}
		if input.BillingProfile != "" {
			billing, hash, err := s.resolveBillingProfile(input.BillingProfile)
			if err != nil {
				return failedPreview(err)
			}
			payload["invoiceDetail"] = billing.InvoiceDetail()
			publicPayload["billingProfile"] = input.BillingProfile
			publicPayload["privatePayloadHash"] = hash
			privateHash = hash
		}
		if len(payload) == 0 {
			return failedPreview(errors.New("shared budget update requires at least one field"))
		}
		if _, err := s.ensureSharedBudgetAccount(ctx, input.AccountInput, input.SharedBudgetID); err != nil {
			return failedPreview(err)
		}
		if err := s.ensurePayloadCurrency(ctx, input.AccountInput, payload); err != nil {
			return failedPreview(err)
		}
		if err := s.validateWrite(ctx, input.AccountInput, payload); err != nil {
			return failedPreview(err)
		}
		verify, _ := appleads.ResourceGet("shared-budgets", input.SharedBudgetID)
		mutation, _ := appleads.ResourceUpdate("shared-budgets", input.SharedBudgetID, payload)
		preview, err := store.PreviewComposite(ctx, s.manager, input.Profile, input.AdAccountID, "shared_budget_update", []string{input.SharedBudgetID}, publicPayload, []operations.VerificationRead{{Name: "shared_budget", Operation: verify, Scopes: []string{inventoryMutationScope("shared-budgets")}}}, mutation, operations.PreviewOptions{Impact: &operations.OperationImpact{SpendAffecting: input.Value != nil, ObjectCount: 1, PrivateHash: privateHash}})
		if err != nil {
			return failedPreview(err)
		}
		return previewSuccess(preview)
	}
}

func (s *Service) campaignSharedBudgetPreview(store *operations.Store, assign bool) func(context.Context, *mcp.CallToolRequest, CampaignSharedBudgetPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignSharedBudgetPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := s.ensureLOC(ctx, input.AccountInput); err != nil {
			return failedPreview(err)
		}
		if _, err := s.ensureSharedBudgetAccount(ctx, input.AccountInput, input.SharedBudgetID); err != nil {
			return failedPreview(err)
		}
		campaign, err := s.readResource(ctx, input.AccountInput, "campaigns", input.CampaignID)
		if err != nil {
			return failedPreview(err)
		}
		budgetIDs, err := nextSharedBudgetAssignments(campaign, input.SharedBudgetID, assign)
		if err != nil {
			return failedPreview(err)
		}
		for _, budgetID := range budgetIDs {
			if _, err := s.ensureSharedBudgetAccount(ctx, input.AccountInput, budgetID); err != nil {
				return failedPreview(err)
			}
		}
		sharedBudgets := make([]any, 0, len(budgetIDs))
		for _, budgetID := range budgetIDs {
			sharedBudgets = append(sharedBudgets, map[string]any{"budgetId": wireID(budgetID)})
		}
		name := "campaign_shared_budget_unassign"
		if assign {
			name = "campaign_shared_budget_assign"
		}
		return s.updatePreviewPayloadWithScopes(ctx, store, "campaigns", name, input.AccountInput, input.CampaignID, map[string]any{"sharedBudgets": sharedBudgets}, []string{operations.ObjectMutationScope(input.SharedBudgetID)})
	}
}

func (s *Service) ensureSharedBudgetAccount(ctx context.Context, account AccountInput, sharedBudgetID string) (any, error) {
	current, err := s.readResource(ctx, account, "shared-budgets", sharedBudgetID)
	if err != nil {
		return nil, err
	}
	if err := validateSharedBudgetAccount(current, account.AdAccountID); err != nil {
		return nil, err
	}
	return current, nil
}

func validateSharedBudgetAccount(value any, adAccountID string) error {
	accountIDs := findStringSliceField(value, "adAccountIds")
	if len(accountIDs) != 1 || accountIDs[0] != adAccountID {
		return errors.New("not_eligible: shared budget must belong exclusively to the explicitly selected ad account")
	}
	return nil
}

func nextSharedBudgetAssignments(campaign any, selectedID string, assign bool) ([]string, error) {
	ids := collectFieldValues(campaign, "budgetId", MaxItems+1)
	if len(ids) > MaxItems {
		return nil, fmt.Errorf("campaign has more than %d shared budget assignments", MaxItems)
	}
	seen := make(map[string]struct{}, len(ids)+1)
	result := make([]string, 0, len(ids)+1)
	found := false
	for _, id := range ids {
		if id == selectedID {
			found = true
			if !assign {
				continue
			}
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if assign {
		if found {
			return nil, errors.New("shared budget is already assigned to the campaign")
		}
		result = append(result, selectedID)
	} else if !found {
		return nil, errors.New("shared budget is not assigned to the campaign")
	}
	sort.Strings(result)
	return result, nil
}

func (s *Service) ensureLOC(ctx context.Context, account AccountInput) error {
	if err := validateAccount(account); err != nil {
		return err
	}
	operation, _ := appleads.AdAccount(account.AdAccountID)
	result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, operation)
	if err != nil {
		return err
	}
	paymentModel := strings.ToUpper(findStringField(result.Data, "paymentModel"))
	if paymentModel == "PAYG" {
		return errors.New("not_eligible: shared budgets require an LOC account; selected account uses PAYG")
	}
	if paymentModel != "LOC" {
		return fmt.Errorf("not_eligible: shared budget payment model is %q, expected LOC", paymentModel)
	}
	return nil
}

func (s *Service) resolveBillingProfile(name string) (optimization.BillingProfile, string, error) {
	profiles, _, err := optimization.LoadBillingProfiles(s.billingPath)
	if err != nil {
		return optimization.BillingProfile{}, "", err
	}
	profile, err := profiles.Resolve(name)
	if err != nil {
		return optimization.BillingProfile{}, "", err
	}
	hash, err := profile.PrivateHash()
	return profile, hash, err
}

func (s *Service) deletePreview(store *operations.Store, resource, name string) func(context.Context, *mcp.CallToolRequest, DeletePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input DeletePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := validateAccount(input.AccountInput); err != nil {
			return failedPreview(err)
		}
		if err := s.deleteAllowed(ctx, input.Profile, input.AdAccountID); err != nil {
			return failedPreview(err)
		}
		if resource == "shared-budgets" {
			if err := s.ensureLOC(ctx, input.AccountInput); err != nil {
				return failedPreview(err)
			}
		}
		current, err := s.readResource(ctx, input.AccountInput, resource, input.ID)
		if err != nil {
			return failedPreview(err)
		}
		if resource == "shared-budgets" {
			if err := validateSharedBudgetAccount(current, input.AdAccountID); err != nil {
				return failedPreview(err)
			}
		}
		actualText := firstNonEmptyTool(findStringField(current, "name"), findStringField(current, "text"))
		if strings.TrimSpace(input.ExpectedText) == "" || input.ExpectedText != actualText {
			return failedPreview(errors.New("expectedText must exactly match the current resource name or keyword text"))
		}
		verify, parentIDs, cascadeCount, err := s.deleteInventory(ctx, input.AccountInput, resource, input.ID, current)
		if err != nil {
			return failedPreview(err)
		}
		mutation, _ := appleads.ResourceDelete(resource, input.ID)
		payload := map[string]any{"id": input.ID, "expectedText": input.ExpectedText, "cascadeObjects": cascadeCount}
		preview, err := store.PreviewComposite(ctx, s.manager, input.Profile, input.AdAccountID, strings.TrimSuffix(name, "_preview"), []string{input.ID}, payload, verify, mutation, operations.PreviewOptions{Impact: &operations.OperationImpact{Destructive: true, ParentIDs: parentIDs, ObjectCount: cascadeCount}})
		if err != nil {
			return failedPreview(err)
		}
		return previewSuccess(preview)
	}
}

func (s *Service) deleteInventory(ctx context.Context, account AccountInput, resource, id string, current any) ([]operations.VerificationRead, []string, int, error) {
	currentOperation, _ := appleads.ResourceGet(resource, id)
	reads := []operations.VerificationRead{{Name: "target", Operation: currentOperation, ExpectDeleted: true}}
	parentIDs := []string{}
	cascadeCount := 1
	scopeSet := map[string]struct{}{}
	scopes := make([]string, 0, 16)
	addScope := func(scope string) {
		if scope == "" {
			return
		}
		if _, exists := scopeSet[scope]; exists {
			return
		}
		scopeSet[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	queryRequest := func(name, childResource string, request map[string]any, inventoryScopes ...string) ([]string, error) {
		operation, _ := appleads.ResourceQuery(childResource, request)
		result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, operation)
		if err != nil {
			return nil, err
		}
		if result.Pagination.Total > MaxItems || result.Pagination.Next != "" {
			return nil, fmt.Errorf("%s cascade contains more than %d objects and cannot be safely bounded", name, MaxItems)
		}
		ids := collectFieldValues(result.Data, "id", MaxItems)
		cascadeCount += len(ids)
		if cascadeCount > MaxItems {
			return nil, fmt.Errorf("delete cascade contains more than %d objects and cannot be safely bounded", MaxItems)
		}
		for _, inventoryScope := range inventoryScopes {
			addScope(inventoryScope)
		}
		for _, childID := range ids {
			addScope(operations.ObjectMutationScope(childID))
		}
		reads = append(reads, operations.VerificationRead{Name: name, Operation: operation})
		return ids, nil
	}
	query := func(name, childResource, field, value string, inventoryScopes ...string) ([]string, error) {
		return queryRequest(name, childResource, map[string]any{
			"filters":    []any{map[string]any{"field": field, "operator": "EQUALS", "value": wireID(value)}},
			"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
		}, inventoryScopes...)
	}
	requirePaused := func(campaignID string) error {
		campaign, err := s.readResource(ctx, account, "campaigns", campaignID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(findStringField(campaign, "status"), "PAUSED") {
			return errors.New("destructive child operations require the parent campaign to be PAUSED")
		}
		parentIDs = append(parentIDs, campaignID)
		addScope(operations.ObjectMutationScope(campaignID))
		return nil
	}
	switch resource {
	case "campaigns":
		if !strings.EqualFold(findStringField(current, "status"), "PAUSED") {
			return nil, nil, 0, errors.New("campaign must be PAUSED before delete preview")
		}
		addScope(inventoryMutationScope("campaigns"))
		adGroupIDs, err := query("ad_groups", "adgroups", "campaignId", id, inventoryMutationScope("adgroups", id))
		if err != nil {
			return nil, nil, 0, err
		}
		for _, adGroupID := range adGroupIDs {
			for _, childResource := range []string{"keywords", "negative-keywords", "ads"} {
				addScope(inventoryMutationScope(childResource, id, adGroupID))
			}
		}
		for _, child := range []struct{ name, resource string }{{"keywords", "keywords"}, {"ads", "ads"}} {
			if _, err := query(child.name, child.resource, "campaignId", id); err != nil {
				return nil, nil, 0, err
			}
		}
		if _, err := queryRequest("campaign_negative_keywords", "negative-keywords", scopeQuery("campaignId", id), inventoryMutationScope("negative-keywords", id)); err != nil {
			return nil, nil, 0, err
		}
		if _, err := queryRequest("ad_group_negative_keywords", "negative-keywords", map[string]any{
			"filters": []any{
				map[string]any{"field": "campaignId", "operator": "EQUALS", "value": wireID(id)},
				map[string]any{"field": "adGroupId", "operator": "IS_NOT_NULL"},
			},
			"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
		}); err != nil {
			return nil, nil, 0, err
		}
	case "adgroups":
		campaignID := findStringField(current, "campaignId")
		if err := requirePaused(campaignID); err != nil {
			return nil, nil, 0, err
		}
		addScope(inventoryMutationScope("adgroups", campaignID))
		for _, child := range []struct{ name, resource string }{{"keywords", "keywords"}, {"negative_keywords", "negative-keywords"}, {"ads", "ads"}} {
			if _, err := query(child.name, child.resource, "adGroupId", id, inventoryMutationScope(child.resource, campaignID, id)); err != nil {
				return nil, nil, 0, err
			}
		}
	case "keywords":
		adGroupID := findStringField(current, "adGroupId")
		adGroup, err := s.readResource(ctx, account, "adgroups", adGroupID)
		if err != nil {
			return nil, nil, 0, err
		}
		parentIDs = append(parentIDs, adGroupID)
		addScope(operations.ObjectMutationScope(adGroupID))
		campaignID := findStringField(adGroup, "campaignId")
		if err := requirePaused(campaignID); err != nil {
			return nil, nil, 0, err
		}
		addScope(inventoryMutationScope("keywords", campaignID, adGroupID))
	case "ads":
		defaultProductPage := containsStringValue(current, "DEFAULT_PRODUCT_PAGE")
		creativeID := findStringField(current, "creativeId")
		if creativeID == "" {
			creativeID = findStringField(findMapField(current, "creative"), "id")
		}
		if creativeID != "" {
			creative, err := s.readResource(ctx, account, "creatives", creativeID)
			if err != nil {
				return nil, nil, 0, err
			}
			defaultProductPage = defaultProductPage || containsStringValue(creative, "DEFAULT_PRODUCT_PAGE")
		}
		if defaultProductPage {
			return nil, nil, 0, errors.New("not_eligible: Apple does not allow deleting an ad that uses the Default Product Page creative")
		}
		adGroupID := findStringField(current, "adGroupId")
		adGroup, err := s.readResource(ctx, account, "adgroups", adGroupID)
		if err != nil {
			return nil, nil, 0, err
		}
		parentIDs = append(parentIDs, adGroupID)
		addScope(operations.ObjectMutationScope(adGroupID))
		campaignID := findStringField(adGroup, "campaignId")
		if err := requirePaused(campaignID); err != nil {
			return nil, nil, 0, err
		}
		addScope(inventoryMutationScope("ads", campaignID, adGroupID))
	case "negative-keywords":
		campaignID := findStringField(current, "campaignId")
		adGroupID := findStringField(current, "adGroupId")
		if campaignID == "" {
			adGroup, err := s.readResource(ctx, account, "adgroups", adGroupID)
			if err != nil {
				return nil, nil, 0, err
			}
			parentIDs = append(parentIDs, adGroupID)
			campaignID = findStringField(adGroup, "campaignId")
		}
		if adGroupID != "" {
			addScope(operations.ObjectMutationScope(adGroupID))
		}
		if err := requirePaused(campaignID); err != nil {
			return nil, nil, 0, err
		}
		parents := []string{campaignID}
		if adGroupID != "" {
			parents = append(parents, adGroupID)
		}
		addScope(inventoryMutationScope("negative-keywords", parents...))
	case "creatives":
		if containsStringValue(current, "DEFAULT_PRODUCT_PAGE") {
			return nil, nil, 0, errors.New("not_eligible: Apple does not allow deleting the Default Product Page creative")
		}
		operation, _ := appleads.ResourceQuery("ads", map[string]any{
			"filters":    []any{map[string]any{"field": "creativeId", "operator": "EQUALS", "value": wireID(id)}},
			"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
		})
		result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, operation)
		if err != nil {
			return nil, nil, 0, err
		}
		if len(collectFieldValues(result.Data, "id", MaxItems)) > 0 {
			return nil, nil, 0, errors.New("creative is still referenced by one or more ads")
		}
		addScope(inventoryMutationScope("creatives"))
		reads = append(reads, operations.VerificationRead{Name: "referencing_ads", Operation: operation})
	case "shared-budgets":
		operation, _ := appleads.ResourceQuery("campaigns", map[string]any{
			"pagination": map[string]any{"offset": 0, "pageSize": MaxItems},
		})
		result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, operation)
		if err != nil {
			return nil, nil, 0, err
		}
		if result.Pagination.Total > MaxItems || result.Pagination.Next != "" {
			return nil, nil, 0, fmt.Errorf("shared budget assignment verification requires at most %d campaigns", MaxItems)
		}
		for _, budgetID := range collectFieldValues(result.Data, "budgetId", MaxItems+1) {
			if budgetID != id {
				continue
			}
			return nil, nil, 0, errors.New("shared budget is still assigned to one or more campaigns")
		}
		addScope(inventoryMutationScope("shared-budgets"))
		reads = append(reads, operations.VerificationRead{Name: "assigned_campaigns", Operation: operation})
	}
	reads[0].Scopes = scopes
	return reads, parentIDs, cascadeCount, nil
}

func containsTrueField(value any, field string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[field].(bool); ok && current {
			return true
		}
		for _, item := range typed {
			if containsTrueField(item, field) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsTrueField(item, field) {
				return true
			}
		}
	}
	return false
}

func addDeletePreviewTool[In any](server *mcp.Server, item Spec, handler mcp.ToolHandlerFor[In, PreviewOutput]) {
	destructive := true
	open := true
	mcp.AddTool(server, &mcp.Tool{
		Name: item.Name, Description: item.Description,
		Annotations: &mcp.ToolAnnotations{Title: strings.ReplaceAll(item.Name, "_", " "), ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &destructive, OpenWorldHint: &open},
	}, handler)
}

func (s *Service) recordOptimizationApply(preview operations.OperationPreview, receipt operations.OperationReceipt) error {
	if preview.Impact == nil || preview.Impact.Policy == "" {
		return nil
	}
	store, err := s.optimizationHistoryStore()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(receipt.Receipt))
	actions := make([]optimization.HistoryAction, 0, len(receipt.Items))
	for _, item := range receipt.Items {
		var previewItem operations.OperationItemPreview
		for _, candidate := range preview.Items {
			if candidate.CorrelationID == item.CorrelationID {
				previewItem = candidate
				break
			}
		}
		actions = append(actions, optimization.HistoryAction{
			CorrelationID: item.CorrelationID, CampaignID: item.CampaignID, ResourceType: item.ResourceType,
			Resource:   optimizationResourceName(item.ResourceType),
			ResourceID: item.TargetID, Action: item.Action, Status: item.Status, Reason: previewItem.Reason,
			Before: historyMap(previewItem.Before), After: previewItem.After, OccurredAt: receipt.AppliedAt,
		})
	}
	return store.Append(preview.Impact.Policy, optimization.HistoryEntry{
		Policy: preview.Impact.Policy, Profile: receipt.Profile, AdAccountID: receipt.AdAccountID,
		CreatedAt: receipt.AppliedAt, ReceiptHash: hex.EncodeToString(sum[:]), Status: receipt.Status, Actions: actions,
	})
}

func (s *Service) recordOptimizationIntent(preview operations.OperationPreview) error {
	if preview.Impact == nil || preview.Impact.Policy == "" {
		return nil
	}
	store, err := s.optimizationHistoryStore()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	actions := make([]optimization.HistoryAction, 0, len(preview.Items))
	for _, item := range preview.Items {
		actions = append(actions, optimization.HistoryAction{
			CorrelationID: item.CorrelationID, CampaignID: item.CampaignID, ResourceType: item.ResourceType,
			Resource:   optimizationResourceName(item.ResourceType),
			ResourceID: item.TargetID, Action: item.Action, Status: "pending", Reason: item.Reason,
			Before: historyMap(item.Before), After: item.After, OccurredAt: now,
		})
	}
	sum := sha256.Sum256([]byte(preview.Receipt))
	return store.BeginIntent(preview.Impact.Policy, optimization.HistoryEntry{
		Policy: preview.Impact.Policy, Profile: preview.Profile, AdAccountID: preview.AdAccountID,
		CreatedAt: now, ReceiptHash: hex.EncodeToString(sum[:]), Status: "applying", Actions: actions,
	})
}

func (s *Service) recordOptimizationPreview(preview operations.OperationPreview, baseline optimization.Baseline) error {
	if preview.Impact == nil || preview.Impact.Policy == "" {
		return nil
	}
	store, err := s.optimizationHistoryStore()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(preview.Receipt))
	actions := make([]optimization.HistoryAction, 0, len(preview.Items))
	for _, item := range preview.Items {
		actions = append(actions, optimization.HistoryAction{
			CorrelationID: item.CorrelationID, CampaignID: item.CampaignID, ResourceType: item.ResourceType,
			Resource:   optimizationResourceName(item.ResourceType),
			ResourceID: item.TargetID, Action: item.Action, Status: "previewed", Reason: item.Reason,
			Before: historyMap(item.Before), After: item.After, OccurredAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return store.Append(preview.Impact.Policy, optimization.HistoryEntry{
		Policy: preview.Impact.Policy, Profile: preview.Profile, AdAccountID: preview.AdAccountID,
		ReceiptHash: hex.EncodeToString(sum[:]), Status: "previewed", Actions: actions,
		PerformanceBefore: optimization.PerformanceSnapshots(baseline),
	})
}

func (s *Service) recordOptimizationVerification(ctx context.Context, preview operations.OperationPreview, verification operations.OperationVerification) error {
	if preview.Impact == nil || preview.Impact.Policy == "" {
		return nil
	}
	store, err := s.optimizationHistoryStore()
	if err != nil {
		return err
	}
	actions := make([]optimization.HistoryAction, 0, len(preview.Items))
	for _, item := range preview.Items {
		status := "unknown"
		var current map[string]any
		for _, object := range verification.Objects {
			if object.Name == "item_"+item.CorrelationID {
				status = object.Status
				current = historyMap(object.Current)
				break
			}
		}
		actions = append(actions, optimization.HistoryAction{CorrelationID: item.CorrelationID, CampaignID: item.CampaignID, ResourceType: item.ResourceType, Resource: optimizationResourceName(item.ResourceType), ResourceID: item.TargetID, Action: item.Action, Status: status, After: current, OccurredAt: time.Now().UTC().Format(time.RFC3339)})
	}
	sum := sha256.Sum256([]byte(preview.Receipt))
	entry := optimization.HistoryEntry{Policy: preview.Impact.Policy, Profile: preview.Profile, AdAccountID: preview.AdAccountID, ReceiptHash: hex.EncodeToString(sum[:]), Status: "verification_" + verification.Status, Verification: actions}
	policies, _, loadErr := optimization.LoadPolicies(s.policyPath)
	if loadErr == nil {
		if policy, resolveErr := policies.Resolve(preview.Impact.Policy); resolveErr == nil {
			now := time.Now()
			if evidence, _, evidenceErr := s.optimizationEvidence(ctx, policy, now); evidenceErr == nil {
				if baseline, baselineErr := optimization.BuildBaseline(policy, evidence, optimization.History{}, now); baselineErr == nil {
					entry.PerformanceAfter = optimization.PerformanceSnapshots(baseline)
				}
			}
		}
	}
	return store.Append(preview.Impact.Policy, entry)
}

func (s *Service) recoverOptimizationVerification(ctx context.Context, receipt string) (operations.OperationPreview, operations.OperationVerification, error) {
	sum := sha256.Sum256([]byte(receipt))
	receiptHash := hex.EncodeToString(sum[:])
	policies, _, err := optimization.LoadPolicies(s.policyPath)
	if err != nil {
		return operations.OperationPreview{}, operations.OperationVerification{}, err
	}
	store, err := s.optimizationHistoryStore()
	if err != nil {
		return operations.OperationPreview{}, operations.OperationVerification{}, err
	}
	for _, policy := range policies.Policies {
		history, loadErr := store.Load(policy.Name)
		if loadErr != nil {
			return operations.OperationPreview{}, operations.OperationVerification{}, loadErr
		}
		entry, exists := optimization.ReconciliationEntry(history, receiptHash)
		if !exists {
			continue
		}
		if !strings.EqualFold(entry.Policy, policy.Name) || !strings.EqualFold(entry.Profile, policy.Profile) || entry.AdAccountID != policy.AdAccountID {
			return operations.OperationPreview{}, operations.OperationVerification{}, errors.New("persisted optimization recovery identity does not match the named policy")
		}
		items := make([]operations.RecoveryItem, 0, len(entry.Actions))
		previewItems := make([]operations.OperationItemPreview, 0, len(entry.Actions))
		for _, action := range entry.Actions {
			resource := optimizationResourceName(action.ResourceType)
			if resource == "" {
				return operations.OperationPreview{}, operations.OperationVerification{}, fmt.Errorf("unsupported persisted optimization resource type %q", action.ResourceType)
			}
			if action.Resource != "" && action.Resource != resource {
				return operations.OperationPreview{}, operations.OperationVerification{}, errors.New("persisted optimization recovery resource does not match its typed action")
			}
			items = append(items, operations.RecoveryItem{
				CorrelationID: action.CorrelationID, CampaignID: action.CampaignID,
				ResourceType: action.ResourceType, Resource: resource, TargetID: action.ResourceID,
				Action: action.Action, Before: action.Before, After: action.After,
			})
			previewItems = append(previewItems, operations.OperationItemPreview{
				CorrelationID: action.CorrelationID, CampaignID: action.CampaignID,
				ResourceType: action.ResourceType, TargetID: action.ResourceID,
				Action: action.Action, Reason: action.Reason, Before: action.Before, After: action.After,
			})
		}
		verification, verifyErr := operations.VerifyRecovery(ctx, s.manager, receipt, entry.Profile, entry.AdAccountID, items)
		if verifyErr != nil {
			return operations.OperationPreview{}, operations.OperationVerification{}, verifyErr
		}
		preview := operations.OperationPreview{
			Receipt: receipt, Profile: entry.Profile, AdAccountID: entry.AdAccountID,
			Operation: "optimization_plan_recovery", Items: previewItems,
			Impact: &operations.OperationImpact{Policy: entry.Policy, ObjectCount: len(previewItems)},
		}
		return preview, verification, nil
	}
	return operations.OperationPreview{}, operations.OperationVerification{}, operations.ErrReceiptNotFound
}

func optimizationResourceName(resourceType string) string {
	return map[string]string{"campaign": "campaigns", "ad_group": "adgroups", "keyword": "keywords"}[resourceType]
}

func historyMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}
