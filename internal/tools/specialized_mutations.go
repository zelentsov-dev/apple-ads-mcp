package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
)

func (s *Service) registerSpecializedMutationTools(server *mcp.Server, store *operations.Store) {
	addPreviewTool(server, mutationSpec("campaign_daily_budget_preview"), s.campaignDailyBudgetPreview(store))
	addPreviewTool(server, mutationSpec("campaign_countries_preview"), s.campaignCountriesPreview(store))
	addPreviewTool(server, mutationSpec("campaign_schedule_preview"), s.resourceSchedulePreview(store, "campaigns", "campaign_schedule"))
	addPreviewTool(server, mutationSpec("ad_group_pause_preview"), s.resourceStatePreview(store, "adgroups", "ad_group_pause", "PAUSED"))
	addPreviewTool(server, mutationSpec("ad_group_resume_preview"), s.resourceStatePreview(store, "adgroups", "ad_group_resume", "ENABLED"))
	addPreviewTool(server, mutationSpec("ad_group_schedule_preview"), s.resourceSchedulePreview(store, "adgroups", "ad_group_schedule"))
	addPreviewTool(server, mutationSpec("ad_group_search_match_preview"), s.adGroupSearchMatchPreview(store))
	addPreviewTool(server, mutationSpec("ad_group_targeting_preview"), s.adGroupTargetingPreview(store))
	addPreviewTool(server, mutationSpec("keyword_bid_preview"), s.keywordBidPreview(store))
	addPreviewTool(server, mutationSpec("keyword_pause_preview"), s.resourceStatePreview(store, "keywords", "keyword_pause", "PAUSED"))
	addPreviewTool(server, mutationSpec("keyword_resume_preview"), s.resourceStatePreview(store, "keywords", "keyword_resume", "ENABLED"))
	addPreviewTool(server, mutationSpec("ad_pause_preview"), s.resourceStatePreview(store, "ads", "ad_pause", "PAUSED"))
	addPreviewTool(server, mutationSpec("ad_resume_preview"), s.resourceStatePreview(store, "ads", "ad_resume", "ENABLED"))
}

func (s *Service) campaignDailyBudgetPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, CampaignMoneyPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignMoneyPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := input.Amount.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		dailyBudget := MoneyValue{Value: input.Amount}
		payload, err := typedPayloadMap(CampaignUpdatePayload{DailyBudget: &dailyBudget})
		if err != nil {
			return failedPreview(err)
		}
		return s.updatePreviewPayload(ctx, store, "campaigns", "campaign_daily_budget", input.AccountInput, input.CampaignID, payload)
	}
}

func (s *Service) campaignCountriesPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, CampaignCountriesPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignCountriesPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		countries, err := normalizeCountryCodes(input.Countries)
		if err != nil {
			return failedPreview(err)
		}
		current, err := s.readResource(ctx, input.AccountInput, "campaigns", input.CampaignID)
		if err != nil {
			return failedPreview(err)
		}
		targeting := cloneObject(findMapField(current, "targeting"))
		if targeting == nil {
			return failedPreview(errors.New("current campaign has no targeting object"))
		}
		targeting["countryOrRegion"] = map[string]any{"include": countries}
		payload := map[string]any{"targeting": targeting}
		return s.updatePreviewPayload(ctx, store, "campaigns", "campaign_countries", input.AccountInput, input.CampaignID, payload)
	}
}

func (s *Service) resourceSchedulePreview(store *operations.Store, resource, name string) func(context.Context, *mcp.CallToolRequest, SchedulePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input SchedulePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		payload := map[string]any{}
		if input.StartTime != nil {
			payload["startTime"] = *input.StartTime
		}
		if input.EndTime != nil {
			payload["endTime"] = *input.EndTime
		}
		if len(payload) == 0 {
			return failedPreview(errors.New("at least one of startTime or endTime is required"))
		}
		return s.updatePreviewPayload(ctx, store, resource, name, input.AccountInput, input.ID, payload)
	}
}

func (s *Service) resourceStatePreview(store *operations.Store, resource, name, status string) func(context.Context, *mcp.CallToolRequest, ResourceStatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceStatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		return s.updatePreviewPayload(ctx, store, resource, name, input.AccountInput, input.ID, map[string]any{"status": status})
	}
}

func (s *Service) adGroupSearchMatchPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, AdGroupSearchMatchPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupSearchMatchPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if input.Enabled {
			campaignID, err := s.parentID(ctx, input.AccountInput, "adgroups", input.AdGroupID, nil, "campaignId", false)
			if err != nil {
				return failedPreview(err)
			}
			if err := s.ensureCampaignPlacement(ctx, input.AccountInput, campaignID, "APPSTORE_SEARCH_RESULTS"); err != nil {
				return failedPreview(err)
			}
		}
		return s.updatePreviewPayload(ctx, store, "adgroups", "ad_group_search_match", input.AccountInput, input.AdGroupID, map[string]any{"automatedKeywordsOptIn": input.Enabled})
	}
}

func (s *Service) adGroupTargetingPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, AdGroupTargetingPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupTargetingPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		payload, err := typedPayloadMap(AdGroupUpdatePayload{Targeting: &input.Targeting})
		if err != nil {
			return failedPreview(err)
		}
		return s.updatePreviewPayload(ctx, store, "adgroups", "ad_group_targeting", input.AccountInput, input.AdGroupID, payload)
	}
}

func (s *Service) keywordBidPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, KeywordBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input KeywordBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := input.Bid.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		if err := validateAccount(input.AccountInput); err != nil {
			return failedPreview(err)
		}
		if err := s.localWriteAllowed(input.Profile); err != nil {
			return failedPreview(err)
		}
		if strings.TrimSpace(input.CampaignID) != "" || strings.TrimSpace(input.AdGroupID) != "" {
			current, err := s.readResource(ctx, input.AccountInput, "keywords", input.KeywordID)
			if err != nil {
				return failedPreview(err)
			}
			for _, assertion := range []struct{ field, expected string }{{"campaignId", input.CampaignID}, {"adGroupId", input.AdGroupID}} {
				field, expected := assertion.field, assertion.expected
				expected = strings.TrimSpace(expected)
				if expected == "" {
					continue
				}
				if !decimalIDPattern.MatchString(expected) {
					return failedPreview(errors.New(field + " assertion must be a decimal string"))
				}
				if actual := findStringField(current, field); actual != expected {
					return failedPreview(errors.New(field + " assertion does not match the current keyword lineage"))
				}
			}
		}
		payload, err := typedPayloadMap(KeywordUpdatePayload{Bid: &input.Bid})
		if err != nil {
			return failedPreview(err)
		}
		return s.updatePreviewPayload(ctx, store, "keywords", "keyword_bid", input.AccountInput, input.KeywordID, payload)
	}
}

func normalizeCountryCodes(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 200 {
		return nil, errors.New("countries must contain 1 to 200 ISO alpha-2 codes")
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if !alpha2(value) {
			return nil, errors.New("countries must contain ISO alpha-2 codes")
		}
		if _, exists := seen[value]; exists {
			return nil, errors.New("countries must not contain duplicates")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func findMapField(value any, field string) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[field].(map[string]any); ok {
			return item
		}
		for _, item := range typed {
			if found := findMapField(item, field); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findMapField(item, field); found != nil {
				return found
			}
		}
	}
	return nil
}

func cloneObject(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		if object, ok := item.(map[string]any); ok {
			result[key] = cloneObject(object)
			continue
		}
		if array, ok := item.([]any); ok {
			result[key] = append([]any(nil), array...)
			continue
		}
		result[key] = item
	}
	return result
}
