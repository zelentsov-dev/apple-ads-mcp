package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
)

func (s *Service) registerRecommendationMutationTools(server *mcp.Server, store *operations.Store) {
	addPreviewTool(server, mutationSpec("daily_budget_recommendation_apply_preview"), s.recommendationPreview(store, "daily-budgets", "apply"))
	addPreviewTool(server, mutationSpec("daily_budget_recommendation_dismiss_preview"), s.recommendationPreview(store, "daily-budgets", "dismiss"))
	addPreviewTool(server, mutationSpec("target_cpa_recommendation_apply_preview"), s.recommendationPreview(store, "target-cpas", "apply"))
	addPreviewTool(server, mutationSpec("target_cpa_recommendation_dismiss_preview"), s.recommendationPreview(store, "target-cpas", "dismiss"))
}

func (s *Service) recommendationPreview(store *operations.Store, kind, action string) func(context.Context, *mcp.CallToolRequest, RecommendationActionPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input RecommendationActionPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if strings.TrimSpace(input.RecommendationID) == "" {
			return failedPreview(errors.New("recommendationId is required"))
		}
		if !decimalIDPattern.MatchString(input.PromotedObjectID) {
			return failedPreview(errors.New("promotedObjectId must be a decimal Adam ID string"))
		}
		queryBody := recommendationQueryBody(input.RecommendationID, input.PromotedObjectID)
		query, err := appleads.Recommendation(kind, queryBody)
		if err != nil {
			return failedPreview(err)
		}
		result, err := s.manager.Do(ctx, input.Profile, input.AdAccountID, query)
		if err != nil {
			return failedPreview(fmt.Errorf("read recommendation: %w", err))
		}
		recommendation := findObjectByFields(result.Data, map[string]string{"id": input.RecommendationID, "promotedObjectId": input.PromotedObjectID})
		if recommendation == nil {
			return failedPreview(errors.New("recommendation is not present for the selected promoted object"))
		}
		if promotedType := stringField(recommendation, "promotedObjectType"); promotedType != "APPSTORE_APP" {
			return failedPreview(fmt.Errorf("recommendation promotedObjectType is %q; only APPSTORE_APP is supported", promotedType))
		}
		campaignID := stringField(recommendation, "campaignId")
		if !decimalIDPattern.MatchString(campaignID) {
			return failedPreview(errors.New("recommendation does not resolve to an App Store campaign"))
		}
		if err := s.ensureAppStoreCampaign(ctx, input.AccountInput, campaignID); err != nil {
			return failedPreview(err)
		}
		campaignRead, err := appleads.ResourceGet("campaigns", campaignID)
		if err != nil {
			return failedPreview(err)
		}
		item := map[string]any{
			"id":                 input.RecommendationID,
			"promotedObjectId":   input.PromotedObjectID,
			"promotedObjectType": "APPSTORE_APP",
		}
		previewPayload := map[string]any{"action": action, "recommendation": item}
		impact := &operations.OperationImpact{SpendAffecting: action == "apply", ParentIDs: []string{campaignID, input.PromotedObjectID}, ObjectCount: 1}
		if action == "apply" {
			if input.MaximumAmount == nil {
				return failedPreview(errors.New("maximumAmount is required when applying a recommendation"))
			}
			if err := input.MaximumAmount.ValidatePositive(); err != nil {
				return failedPreview(fmt.Errorf("maximumAmount: %w", err))
			}
			suggestedField, appliedField := "suggestedDailyBudgetAmount", "appliedDailyBudget"
			if kind == "target-cpas" {
				suggestedField, appliedField = "recommendedTargetCPA", "appliedTargetCPA"
			}
			suggested, err := moneyFromObject(recommendation[suggestedField])
			if err != nil {
				return failedPreview(fmt.Errorf("recommendation from Apple %s: %w", suggestedField, err))
			}
			applied := suggested
			if input.AppliedAmount != nil {
				applied = *input.AppliedAmount
				if err := applied.ValidatePositive(); err != nil {
					return failedPreview(fmt.Errorf("appliedAmount: %w", err))
				}
			}
			if compared, err := compareMoney(applied, *input.MaximumAmount); err != nil {
				return failedPreview(err)
			} else if compared > 0 {
				return failedPreview(errors.New("applied recommendation amount exceeds maximumAmount"))
			}
			if err := s.ensurePayloadCurrency(ctx, input.AccountInput, map[string]any{"appliedAmount": moneyMap(applied), "maximumAmount": moneyMap(*input.MaximumAmount)}); err != nil {
				return failedPreview(err)
			}
			item[appliedField] = moneyMap(applied)
			previewPayload["maximumAmount"] = moneyMap(*input.MaximumAmount)
			previewPayload["suggestedAmount"] = moneyMap(suggested)
			impact.Currency = strings.ToUpper(applied.Currency)
			maximumAmount := *input.MaximumAmount
			maximumAmount.Currency = strings.ToUpper(maximumAmount.Currency)
			impact.MaximumAmount = &maximumAmount
		}
		if err := s.validateWrite(ctx, input.AccountInput, previewPayload); err != nil {
			return failedPreview(err)
		}
		mutation, err := appleads.RecommendationAction(kind, action, []any{item})
		if err != nil {
			return failedPreview(err)
		}
		placement, err := s.campaignPlacement(ctx, input.AccountInput, campaignID)
		if err != nil {
			return failedPreview(err)
		}
		impact.Placement = placement
		name := "daily_budget_recommendation_" + action
		if kind == "target-cpas" {
			name = "target_cpa_recommendation_" + action
		}
		preview, err := store.PreviewComposite(ctx, s.manager, input.Profile, input.AdAccountID, name, []string{input.RecommendationID, campaignID}, previewPayload, []operations.VerificationRead{
			{Name: "recommendation", Operation: query},
			{Name: "campaign", Operation: campaignRead},
		}, mutation, operations.PreviewOptions{Impact: impact})
		if err != nil {
			return failedPreview(err)
		}
		return previewSuccess(preview)
	}
}

func recommendationQueryBody(recommendationID, promotedObjectID string) map[string]any {
	return map[string]any{
		"filters": []any{
			map[string]any{"field": "id", "operator": "EQUALS", "value": []string{recommendationID}},
			map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{promotedObjectID}},
			map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 10},
	}
}

func findObjectByFields(value any, expected map[string]string) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		matches := true
		for field, wanted := range expected {
			if fmt.Sprint(typed[field]) != wanted {
				matches = false
				break
			}
		}
		if matches {
			return typed
		}
		for _, item := range typed {
			if found := findObjectByFields(item, expected); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findObjectByFields(item, expected); found != nil {
				return found
			}
		}
	}
	return nil
}

func moneyFromObject(value any) (appleads.Money, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return appleads.Money{}, errors.New("money object is missing")
	}
	money := appleads.Money{Amount: stringField(object, "amount"), Currency: strings.ToUpper(stringField(object, "currency"))}
	if err := money.ValidatePositive(); err != nil {
		return appleads.Money{}, err
	}
	return money, nil
}

func moneyMap(value appleads.Money) map[string]any {
	return map[string]any{"amount": value.Amount, "currency": strings.ToUpper(value.Currency)}
}
