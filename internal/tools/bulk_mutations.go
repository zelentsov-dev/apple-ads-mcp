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

func (s *Service) registerBulkMutationTools(server *mcp.Server, store *operations.Store) {
	addPreviewTool(server, mutationSpec("keywords_bulk_create_preview"), s.keywordsBulkCreatePreview(store))
	addPreviewTool(server, mutationSpec("keywords_bulk_update_preview"), s.keywordsBulkUpdatePreview(store))
	addPreviewTool(server, mutationSpec("negative_keywords_bulk_create_preview"), s.negativeKeywordsBulkCreatePreview(store))
	addPreviewTool(server, mutationSpec("negative_keywords_bulk_update_preview"), s.negativeKeywordsBulkUpdatePreview(store))
}

func (s *Service) keywordsBulkCreatePreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, BulkKeywordCreateInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input BulkKeywordCreateInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if !decimalIDPattern.MatchString(input.AdGroupID) {
			return failedPreview(errors.New("adGroupId must be a decimal string"))
		}
		if err := validateBulkCount(len(input.Items)); err != nil {
			return failedPreview(err)
		}
		campaignID, placement, err := s.searchResultsScopeForAdGroup(ctx, input.AccountInput, input.AdGroupID)
		if err != nil {
			return failedPreview(err)
		}
		items := make([]any, 0, len(input.Items))
		previews := make([]operations.OperationItemPreview, 0, len(input.Items))
		correlations := map[string]struct{}{}
		duplicates := map[string]struct{}{}
		for _, item := range input.Items {
			if err := validateCorrelationID(item.CorrelationID, correlations); err != nil {
				return failedPreview(err)
			}
			payload, err := typedPayloadMap(KeywordCreatePayload{AdGroupID: input.AdGroupID, Text: strings.TrimSpace(item.Text), MatchType: item.MatchType, Bid: item.Bid, Status: item.Status})
			if err != nil {
				return failedPreview(err)
			}
			if err := validateKeywordPayload(payload, true); err != nil {
				return failedPreview(err)
			}
			key := strings.ToLower(strings.TrimSpace(item.Text)) + "\x00" + item.MatchType
			if _, exists := duplicates[key]; exists {
				return failedPreview(fmt.Errorf("duplicate keyword %q with match type %s", item.Text, item.MatchType))
			}
			duplicates[key] = struct{}{}
			items = append(items, map[string]any{"correlationId": wireID(item.CorrelationID), "data": payload})
			previews = append(previews, operations.OperationItemPreview{CorrelationID: item.CorrelationID, After: payload})
		}
		return s.bulkPreview(ctx, store, input.AccountInput, "keywords", "create", "keywords_bulk_create", []string{campaignID, input.AdGroupID}, placement, items, previews, scopeQuery("adGroupId", input.AdGroupID))
	}
}

func (s *Service) keywordsBulkUpdatePreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, BulkKeywordUpdateInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input BulkKeywordUpdateInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if !decimalIDPattern.MatchString(input.AdGroupID) {
			return failedPreview(errors.New("adGroupId must be a decimal string"))
		}
		if err := validateBulkCount(len(input.Items)); err != nil {
			return failedPreview(err)
		}
		campaignID, placement, err := s.searchResultsScopeForAdGroup(ctx, input.AccountInput, input.AdGroupID)
		if err != nil {
			return failedPreview(err)
		}
		items := make([]any, 0, len(input.Items))
		previews := make([]operations.OperationItemPreview, 0, len(input.Items))
		correlations := map[string]struct{}{}
		targets := map[string]struct{}{}
		for _, item := range input.Items {
			if err := validateCorrelationID(item.CorrelationID, correlations); err != nil {
				return failedPreview(err)
			}
			if !decimalIDPattern.MatchString(item.ID) {
				return failedPreview(errors.New("keyword IDs must be decimal strings"))
			}
			if _, exists := targets[item.ID]; exists {
				return failedPreview(fmt.Errorf("duplicate keyword ID %s", item.ID))
			}
			targets[item.ID] = struct{}{}
			payload, err := typedPayloadMap(KeywordUpdatePayload{Bid: item.Bid, Status: item.Status})
			if err != nil {
				return failedPreview(err)
			}
			if err := validateKeywordPayload(payload, false); err != nil {
				return failedPreview(err)
			}
			if len(payload) == 0 {
				return failedPreview(errors.New("each keyword update must change bid or status"))
			}
			payload["id"] = item.ID
			items = append(items, map[string]any{"correlationId": wireID(item.CorrelationID), "data": payload})
			previews = append(previews, operations.OperationItemPreview{CorrelationID: item.CorrelationID, TargetID: item.ID, After: payload})
		}
		return s.bulkPreview(ctx, store, input.AccountInput, "keywords", "update", "keywords_bulk_update", []string{campaignID, input.AdGroupID}, placement, items, previews, scopeQuery("adGroupId", input.AdGroupID))
	}
}

func (s *Service) negativeKeywordsBulkCreatePreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, BulkNegativeKeywordCreateInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input BulkNegativeKeywordCreateInput) (*mcp.CallToolResult, PreviewOutput, error) {
		campaignID, adGroupID, placement, err := s.negativeKeywordScope(ctx, input.AccountInput, input.CampaignID, input.AdGroupID)
		if err != nil {
			return failedPreview(err)
		}
		if err := validateBulkCount(len(input.Items)); err != nil {
			return failedPreview(err)
		}
		items := make([]any, 0, len(input.Items))
		previews := make([]operations.OperationItemPreview, 0, len(input.Items))
		correlations := map[string]struct{}{}
		duplicates := map[string]struct{}{}
		for _, item := range input.Items {
			if err := validateCorrelationID(item.CorrelationID, correlations); err != nil {
				return failedPreview(err)
			}
			payload, err := typedPayloadMap(NegativeKeywordCreatePayload{CampaignID: input.CampaignID, AdGroupID: input.AdGroupID, Text: strings.TrimSpace(item.Text), MatchType: item.MatchType, Status: item.Status})
			if err != nil {
				return failedPreview(err)
			}
			if err := validateNegativeKeywordPayload(payload, true); err != nil {
				return failedPreview(err)
			}
			key := strings.ToLower(strings.TrimSpace(item.Text)) + "\x00" + item.MatchType
			if _, exists := duplicates[key]; exists {
				return failedPreview(fmt.Errorf("duplicate negative keyword %q with match type %s", item.Text, item.MatchType))
			}
			duplicates[key] = struct{}{}
			items = append(items, map[string]any{"correlationId": wireID(item.CorrelationID), "data": payload})
			previews = append(previews, operations.OperationItemPreview{CorrelationID: item.CorrelationID, After: payload})
		}
		parents := []string{campaignID}
		scopeField, scopeID := "campaignId", campaignID
		if adGroupID != "" {
			parents = append(parents, adGroupID)
			scopeField, scopeID = "adGroupId", adGroupID
		}
		return s.bulkPreview(ctx, store, input.AccountInput, "negative-keywords", "create", "negative_keywords_bulk_create", parents, placement, items, previews, scopeQuery(scopeField, scopeID))
	}
}

func (s *Service) negativeKeywordsBulkUpdatePreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, BulkNegativeKeywordUpdateInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input BulkNegativeKeywordUpdateInput) (*mcp.CallToolResult, PreviewOutput, error) {
		campaignID, adGroupID, placement, err := s.negativeKeywordScope(ctx, input.AccountInput, input.CampaignID, input.AdGroupID)
		if err != nil {
			return failedPreview(err)
		}
		if err := validateBulkCount(len(input.Items)); err != nil {
			return failedPreview(err)
		}
		items := make([]any, 0, len(input.Items))
		previews := make([]operations.OperationItemPreview, 0, len(input.Items))
		correlations := map[string]struct{}{}
		targets := map[string]struct{}{}
		for _, item := range input.Items {
			if err := validateCorrelationID(item.CorrelationID, correlations); err != nil {
				return failedPreview(err)
			}
			if !decimalIDPattern.MatchString(item.ID) {
				return failedPreview(errors.New("negative keyword IDs must be decimal strings"))
			}
			if _, exists := targets[item.ID]; exists {
				return failedPreview(fmt.Errorf("duplicate negative keyword ID %s", item.ID))
			}
			targets[item.ID] = struct{}{}
			payload, err := typedPayloadMap(NegativeKeywordUpdatePayload{Status: item.Status})
			if err != nil {
				return failedPreview(err)
			}
			if err := validateNegativeKeywordPayload(payload, false); err != nil {
				return failedPreview(err)
			}
			if len(payload) == 0 {
				return failedPreview(errors.New("each negative keyword update must change status"))
			}
			payload["id"] = item.ID
			items = append(items, map[string]any{"correlationId": wireID(item.CorrelationID), "data": payload})
			previews = append(previews, operations.OperationItemPreview{CorrelationID: item.CorrelationID, TargetID: item.ID, After: payload})
		}
		parents := []string{campaignID}
		scopeField, scopeID := "campaignId", campaignID
		if adGroupID != "" {
			parents = append(parents, adGroupID)
			scopeField, scopeID = "adGroupId", adGroupID
		}
		return s.bulkPreview(ctx, store, input.AccountInput, "negative-keywords", "update", "negative_keywords_bulk_update", parents, placement, items, previews, scopeQuery(scopeField, scopeID))
	}
}

func (s *Service) bulkPreview(ctx context.Context, store *operations.Store, account AccountInput, resource, action, name string, parentIDs []string, placement string, items []any, previews []operations.OperationItemPreview, query map[string]any) (*mcp.CallToolResult, PreviewOutput, error) {
	body := map[string]any{"allowPartialSuccess": true, "items": items}
	if err := s.ensurePayloadCurrency(ctx, account, body); err != nil {
		return failedPreview(err)
	}
	if err := s.validateWrite(ctx, account, body); err != nil {
		return failedPreview(err)
	}
	verify, err := appleads.ResourceQuery(resource, query)
	if err != nil {
		return failedPreview(err)
	}
	current, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, verify)
	if err != nil {
		return failedPreview(fmt.Errorf("read affected inventory: %w", err))
	}
	if current.Pagination.Next != "" || current.Pagination.Total > 200 {
		return failedPreview(errors.New("selected inventory exceeds 200 objects and cannot be bound to one safe bulk receipt"))
	}
	if action == "update" {
		for _, item := range previews {
			if !containsResourceID(current.Data, item.TargetID) {
				return failedPreview(fmt.Errorf("target %s is not present in the selected scope", item.TargetID))
			}
		}
	}
	mutation, err := appleads.BulkResource(resource, action, body)
	if err != nil {
		return failedPreview(err)
	}
	targetIDs := make([]string, 0, len(previews))
	for _, item := range previews {
		if item.TargetID != "" {
			targetIDs = append(targetIDs, item.TargetID)
		}
	}
	impact := &operations.OperationImpact{SpendAffecting: true, Placement: placement, ParentIDs: parentIDs, ObjectCount: len(items)}
	if currencies := collectFieldValues(body, "currency", MaxItems); len(currencies) > 0 {
		impact.Currency = strings.ToUpper(currencies[0])
	}
	preview, err := store.PreviewComposite(ctx, s.manager, account.Profile, account.AdAccountID, name, targetIDs, body, []operations.VerificationRead{{Name: "affected_inventory", Operation: verify}}, mutation, operations.PreviewOptions{Impact: impact, Items: previews})
	if err != nil {
		return failedPreview(err)
	}
	return previewSuccess(preview)
}

func (s *Service) searchResultsScopeForAdGroup(ctx context.Context, account AccountInput, adGroupID string) (string, string, error) {
	campaignID, err := s.parentID(ctx, account, "adgroups", adGroupID, nil, "campaignId", false)
	if err != nil {
		return "", "", err
	}
	if err := s.ensureCampaignPlacement(ctx, account, campaignID, "APPSTORE_SEARCH_RESULTS"); err != nil {
		return "", "", err
	}
	return campaignID, "APPSTORE_SEARCH_RESULTS", nil
}

func (s *Service) negativeKeywordScope(ctx context.Context, account AccountInput, campaignID, adGroupID string) (string, string, string, error) {
	if (campaignID == "") == (adGroupID == "") {
		return "", "", "", errors.New("exactly one of campaignId or adGroupId is required")
	}
	if adGroupID != "" {
		if !decimalIDPattern.MatchString(adGroupID) {
			return "", "", "", errors.New("adGroupId must be a decimal string")
		}
		resolved, placement, err := s.searchResultsScopeForAdGroup(ctx, account, adGroupID)
		return resolved, adGroupID, placement, err
	}
	if !decimalIDPattern.MatchString(campaignID) {
		return "", "", "", errors.New("campaignId must be a decimal string")
	}
	if err := s.ensureCampaignPlacement(ctx, account, campaignID, "APPSTORE_SEARCH_RESULTS"); err != nil {
		return "", "", "", err
	}
	return campaignID, "", "APPSTORE_SEARCH_RESULTS", nil
}

func validateBulkCount(count int) error {
	if count < 1 || count > 100 {
		return errors.New("bulk operations require 1 to 100 items")
	}
	return nil
}

func validateCorrelationID(value string, seen map[string]struct{}) error {
	if !decimalIDPattern.MatchString(value) {
		return errors.New("each correlationId must be a positive decimal string")
	}
	if _, exists := seen[value]; exists {
		return fmt.Errorf("duplicate correlationId %s", value)
	}
	seen[value] = struct{}{}
	return nil
}

func scopeQuery(scopeField, scopeID string) map[string]any {
	filters := []any{map[string]any{"field": scopeField, "operator": "EQUALS", "value": wireID(scopeID)}}
	if scopeField == "campaignId" {
		filters = append(filters, map[string]any{"field": "adGroupId", "operator": "IS_NULL"})
	}
	return map[string]any{
		"filters":    filters,
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	}
}

func containsResourceID(value any, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if id, exists := typed["id"]; exists && fmt.Sprint(id) == expected {
			return true
		}
		for _, item := range typed {
			if containsResourceID(item, expected) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsResourceID(item, expected) {
				return true
			}
		}
	}
	return false
}
