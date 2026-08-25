package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
)

type CampaignStatePreviewInput struct {
	AccountInput
	CampaignID string `json:"campaignId" jsonschema:"campaign ID as a string"`
}

type AdGroupBidPreviewInput struct {
	AccountInput
	AdGroupID string `json:"adGroupId" jsonschema:"ad group ID as a string"`
	Amount    string `json:"amount" jsonschema:"decimal bid amount"`
	Currency  string `json:"currency" jsonschema:"ISO 4217 currency"`
}

type ReceiptInput struct {
	Receipt string `json:"receipt" jsonschema:"one-time operation receipt returned by a preview tool"`
}

type PreviewOutput struct {
	Summary string                       `json:"summary"`
	Preview *operations.OperationPreview `json:"preview,omitempty"`
	Error   *ErrorOutput                 `json:"error,omitempty"`
}

type ApplyOutput struct {
	Summary      string                            `json:"summary"`
	Receipt      *operations.OperationReceipt      `json:"receipt,omitempty"`
	Preview      *operations.OperationPreview      `json:"preview,omitempty"`
	Used         bool                              `json:"used,omitempty"`
	Verification *operations.OperationVerification `json:"verification,omitempty"`
	Error        *ErrorOutput                      `json:"error,omitempty"`
}

func MutationSpecs() []Spec {
	return []Spec{
		{Name: "campaign_create_preview", Description: "Preview creation of one campaign and return a ten-minute receipt.", Class: "mutation_preview"},
		{Name: "campaign_update_preview", Description: "Preview a campaign update after reading current state.", Class: "mutation_preview"},
		{Name: "campaign_pause_preview", Description: "Preview pausing one campaign.", Class: "mutation_preview"},
		{Name: "campaign_resume_preview", Description: "Preview resuming one campaign.", Class: "mutation_preview"},
		{Name: "campaign_daily_budget_preview", Description: "Preview a campaign daily-budget update with typed money.", Class: "mutation_preview"},
		{Name: "campaign_countries_preview", Description: "Preview replacing campaign country targeting.", Class: "mutation_preview"},
		{Name: "campaign_schedule_preview", Description: "Preview a campaign start and end schedule update.", Class: "mutation_preview"},
		{Name: "ad_group_create_preview", Description: "Preview creation of one ad group.", Class: "mutation_preview"},
		{Name: "ad_group_update_preview", Description: "Preview an ad-group update.", Class: "mutation_preview"},
		{Name: "ad_group_pause_preview", Description: "Preview pausing one ad group.", Class: "mutation_preview"},
		{Name: "ad_group_resume_preview", Description: "Preview resuming one ad group.", Class: "mutation_preview"},
		{Name: "ad_group_schedule_preview", Description: "Preview an ad-group start and end schedule update.", Class: "mutation_preview"},
		{Name: "ad_group_search_match_preview", Description: "Preview enabling or disabling Apple Search Match for a Search Results ad group.", Class: "mutation_preview"},
		{Name: "ad_group_targeting_preview", Description: "Preview App Store-only ad-group targeting.", Class: "mutation_preview"},
		{Name: "ad_group_bid_preview", Description: "Preview an ad-group default bid update with decimal amount and currency.", Class: "mutation_preview"},
		{Name: "ad_group_cpa_cap_preview", Description: "Preview an ad-group CPA cap update with decimal amount and currency.", Class: "mutation_preview"},
		{Name: "keyword_create_preview", Description: "Preview creation of one targeting keyword.", Class: "mutation_preview"},
		{Name: "keyword_update_preview", Description: "Preview a targeting keyword update.", Class: "mutation_preview"},
		{Name: "keyword_bid_preview", Description: "Preview a targeting keyword bid update.", Class: "mutation_preview"},
		{Name: "keyword_pause_preview", Description: "Preview pausing one targeting keyword.", Class: "mutation_preview"},
		{Name: "keyword_resume_preview", Description: "Preview resuming one targeting keyword.", Class: "mutation_preview"},
		{Name: "negative_keyword_create_preview", Description: "Preview creation of one negative keyword.", Class: "mutation_preview"},
		{Name: "negative_keyword_update_preview", Description: "Preview a negative keyword update.", Class: "mutation_preview"},
		{Name: "ad_create_preview", Description: "Preview creation of one ad.", Class: "mutation_preview"},
		{Name: "ad_update_preview", Description: "Preview an ad update.", Class: "mutation_preview"},
		{Name: "ad_pause_preview", Description: "Preview pausing one ad.", Class: "mutation_preview"},
		{Name: "ad_resume_preview", Description: "Preview resuming one ad.", Class: "mutation_preview"},
		{Name: "creative_create_preview", Description: "Preview creation of one creative.", Class: "mutation_preview"},
		{Name: "creative_update_preview", Description: "Preview a creative update.", Class: "mutation_preview"},
		{Name: "keywords_bulk_create_preview", Description: "Preview up to 100 targeting keyword creates as one drift-bound operation.", Class: "mutation_preview"},
		{Name: "keywords_bulk_update_preview", Description: "Preview up to 100 targeting keyword updates as one drift-bound operation.", Class: "mutation_preview"},
		{Name: "negative_keywords_bulk_create_preview", Description: "Preview up to 100 negative keyword creates in one scope.", Class: "mutation_preview"},
		{Name: "negative_keywords_bulk_update_preview", Description: "Preview up to 100 negative keyword updates in one scope.", Class: "mutation_preview"},
		{Name: "daily_budget_recommendation_apply_preview", Description: "Preview applying one daily-budget recommendation under an explicit maximum.", Class: "mutation_preview"},
		{Name: "daily_budget_recommendation_dismiss_preview", Description: "Preview dismissing one daily-budget recommendation.", Class: "mutation_preview"},
		{Name: "target_cpa_recommendation_apply_preview", Description: "Preview applying one target-CPA recommendation under an explicit maximum.", Class: "mutation_preview"},
		{Name: "target_cpa_recommendation_dismiss_preview", Description: "Preview dismissing one target-CPA recommendation.", Class: "mutation_preview"},
		{Name: "operations_apply", Description: "Apply exactly one non-expired, drift-free preview receipt.", Class: "mutation"},
		{Name: "operations_inspect", Description: "Inspect receipt binding, expiry, and use state without applying it.", Class: "read"},
		{Name: "operations_verify", Description: "Re-read a receipt target after an ambiguous write and return current state.", Class: "read"},
	}
}

func (s *Service) RegisterMutationTools(server *mcp.Server, store *operations.Store) {
	registerTypedResource[CampaignCreatePayload, CampaignUpdatePayload](server, s, store, "campaign_create_preview", "campaign_update_preview", "campaigns")
	registerTypedResource[AdGroupCreatePayload, AdGroupUpdatePayload](server, s, store, "ad_group_create_preview", "ad_group_update_preview", "adgroups")
	registerTypedResource[KeywordCreatePayload, KeywordUpdatePayload](server, s, store, "keyword_create_preview", "keyword_update_preview", "keywords")
	registerTypedResource[NegativeKeywordCreatePayload, NegativeKeywordUpdatePayload](server, s, store, "negative_keyword_create_preview", "negative_keyword_update_preview", "negative-keywords")
	registerTypedResource[AdCreatePayload, AdUpdatePayload](server, s, store, "ad_create_preview", "ad_update_preview", "ads")
	registerTypedResource[CreativeCreatePayload, CreativeUpdatePayload](server, s, store, "creative_create_preview", "creative_update_preview", "creatives")
	addPreviewTool(server, mutationSpec("campaign_pause_preview"), s.campaignStatePreview(store, "PAUSED", "campaign_pause"))
	addPreviewTool(server, mutationSpec("campaign_resume_preview"), s.campaignStatePreview(store, "ENABLED", "campaign_resume"))
	s.registerSpecializedMutationTools(server, store)
	s.registerBulkMutationTools(server, store)
	s.registerRecommendationMutationTools(server, store)
	addPreviewTool(server, mutationSpec("ad_group_bid_preview"), s.adGroupBidPreview(store))
	addPreviewTool(server, mutationSpec("ad_group_cpa_cap_preview"), s.adGroupCPACapPreview(store))

	destructive := false
	open := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "operations_apply", Description: mutationSpec("operations_apply").Description,
		Annotations: &mcp.ToolAnnotations{Title: "apply operation", ReadOnlyHint: false, IdempotentHint: false, DestructiveHint: &destructive, OpenWorldHint: &open},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReceiptInput) (*mcp.CallToolResult, ApplyOutput, error) {
		preview, _, err := store.Inspect(input.Receipt)
		if err != nil {
			return failedApply(err)
		}
		if err := s.writeAllowed(ctx, preview.Profile, preview.AdAccountID); err != nil {
			return failedApply(err)
		}
		receipt, err := store.Apply(ctx, s.manager, input.Receipt)
		if err != nil {
			return failedApply(err)
		}
		summary := "Operation applied and Apple returned a response"
		if receipt.Status == "unknown" {
			summary = "Write outcome is unknown; verify current Apple state before another change"
		} else if receipt.Status == "partial" {
			summary = "Apple partially applied the operation; inspect item results and verify current state"
		} else if receipt.Status == "failed" {
			summary = "Apple returned item-level failures; no automatic retry was attempted"
		}
		output := ApplyOutput{Summary: summary, Receipt: &receipt}
		return textResult(summary, receipt.Status == "unknown"), output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "operations_verify", Description: mutationSpec("operations_verify").Description,
		Annotations: &mcp.ToolAnnotations{Title: "verify operation", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &open},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReceiptInput) (*mcp.CallToolResult, ApplyOutput, error) {
		verification, err := store.Verify(ctx, s.manager, input.Receipt)
		if err != nil {
			return failedApply(err)
		}
		output := ApplyOutput{Summary: "Current Apple state loaded for receipt verification", Verification: &verification}
		return textResult(output.Summary, false), output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "operations_inspect", Description: mutationSpec("operations_inspect").Description,
		Annotations: &mcp.ToolAnnotations{Title: "inspect operation", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &open},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input ReceiptInput) (*mcp.CallToolResult, ApplyOutput, error) {
		preview, used, err := store.Inspect(input.Receipt)
		if err != nil {
			return failedApply(err)
		}
		output := ApplyOutput{Summary: "Operation receipt loaded", Preview: &preview, Used: used}
		return textResult(output.Summary, false), output, nil
	})
}

func registerTypedResource[CreatePayload, UpdatePayload any](server *mcp.Server, service *Service, store *operations.Store, createName, updateName, resource string) {
	addPreviewTool(server, mutationSpec(createName), typedCreatePreview[CreatePayload](service, store, resource, createName))
	addPreviewTool(server, mutationSpec(updateName), typedUpdatePreview[UpdatePayload](service, store, resource, updateName))
}

func typedCreatePreview[Payload any](service *Service, store *operations.Store, resource, name string) func(context.Context, *mcp.CallToolRequest, TypedCreatePreviewInput[Payload]) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input TypedCreatePreviewInput[Payload]) (*mcp.CallToolResult, PreviewOutput, error) {
		payload, err := typedPayloadMap(input.Payload)
		if err != nil {
			return failedPreview(err)
		}
		return service.createPreviewPayload(ctx, store, resource, name, input.AccountInput, payload)
	}
}

func typedUpdatePreview[Payload any](service *Service, store *operations.Store, resource, name string) func(context.Context, *mcp.CallToolRequest, TypedUpdatePreviewInput[Payload]) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input TypedUpdatePreviewInput[Payload]) (*mcp.CallToolResult, PreviewOutput, error) {
		payload, err := typedPayloadMap(input.Payload)
		if err != nil {
			return failedPreview(err)
		}
		return service.updatePreviewPayload(ctx, store, resource, name, input.AccountInput, input.ID, payload)
	}
}

func (s *Service) createPreviewPayload(ctx context.Context, store *operations.Store, resource, name string, account AccountInput, payload map[string]any) (*mcp.CallToolResult, PreviewOutput, error) {
	payload = queryRequest(payload)
	if resource == "campaigns" {
		if value := stringField(payload, "adAccountId"); value != "" && value != account.AdAccountID {
			return failedPreview(errors.New("campaign payload adAccountId does not match the explicit ad account"))
		}
		payload["adAccountId"] = account.AdAccountID
	}
	if err := validateTypedResourcePayload(resource, true, payload); err != nil {
		return failedPreview(err)
	}
	if err := s.validateWrite(ctx, account, payload); err != nil {
		return failedPreview(err)
	}
	if err := s.ensureAppStoreMutation(ctx, account, resource, "", payload, true); err != nil {
		return failedPreview(err)
	}
	verify, err := appleads.ResourceQuery(resource, createVerificationQuery(resource, payload))
	if err != nil {
		return failedPreview(err)
	}
	mutation, err := appleads.ResourceCreate(resource, payload)
	if err != nil {
		return failedPreview(err)
	}
	impact, err := s.resourceImpact(ctx, account, resource, "", payload, true)
	if err != nil {
		return failedPreview(err)
	}
	preview, err := store.PreviewComposite(ctx, s.manager, account.Profile, account.AdAccountID, name, nil, payload, []operations.VerificationRead{{Name: "affected_inventory", Operation: verify}}, mutation, operations.PreviewOptions{Impact: impact})
	if err != nil {
		return failedPreview(err)
	}
	return previewSuccess(preview)
}

func createVerificationQuery(resource string, payload map[string]any) map[string]any {
	request := map[string]any{"pagination": map[string]any{"offset": 0, "pageSize": MaxItems}}
	var parentField string
	switch resource {
	case "adgroups":
		parentField = "campaignId"
	case "keywords", "ads":
		parentField = "adGroupId"
	case "negative-keywords":
		if adGroupID := stringField(payload, "adGroupId"); adGroupID != "" {
			request["filters"] = []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": wireID(adGroupID)}}
		} else if campaignID := stringField(payload, "campaignId"); campaignID != "" {
			request["filters"] = []any{
				map[string]any{"field": "campaignId", "operator": "EQUALS", "value": wireID(campaignID)},
				map[string]any{"field": "adGroupId", "operator": "IS_NULL"},
			}
		}
	}
	if parentField != "" {
		if parentID := stringField(payload, parentField); parentID != "" {
			request["filters"] = []any{map[string]any{"field": parentField, "operator": "EQUALS", "value": wireID(parentID)}}
		}
	}
	return request
}

func wireID(value string) any {
	converted, err := numericAdamID(value)
	if err != nil {
		return value
	}
	return converted
}

func (s *Service) updatePreviewPayload(ctx context.Context, store *operations.Store, resource, name string, account AccountInput, id string, payload map[string]any) (*mcp.CallToolResult, PreviewOutput, error) {
	if err := validateTypedResourcePayload(resource, false, payload); err != nil {
		return failedPreview(err)
	}
	if err := s.validateWrite(ctx, account, payload); err != nil {
		return failedPreview(err)
	}
	if err := s.ensureAppStoreMutation(ctx, account, resource, id, payload, false); err != nil {
		return failedPreview(err)
	}
	verify, err := appleads.ResourceGet(resource, id)
	if err != nil {
		return failedPreview(err)
	}
	mutation, err := appleads.ResourceUpdate(resource, id, payload)
	if err != nil {
		return failedPreview(err)
	}
	impact, err := s.resourceImpact(ctx, account, resource, id, payload, false)
	if err != nil {
		return failedPreview(err)
	}
	preview, err := store.PreviewComposite(ctx, s.manager, account.Profile, account.AdAccountID, name, []string{id}, payload, []operations.VerificationRead{{Name: "current", Operation: verify}}, mutation, operations.PreviewOptions{Impact: impact})
	if err != nil {
		return failedPreview(err)
	}
	return previewSuccess(preview)
}

func (s *Service) campaignStatePreview(store *operations.Store, status, name string) func(context.Context, *mcp.CallToolRequest, CampaignStatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignStatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		payload := map[string]any{"status": status}
		return s.updatePreviewPayload(ctx, store, "campaigns", name, input.AccountInput, input.CampaignID, payload)
	}
}

func (s *Service) adGroupBidPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		money := appleads.Money{Amount: input.Amount, Currency: strings.ToUpper(input.Currency)}
		if err := money.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		payload := map[string]any{"bidStrategy": map[string]any{"bid": money}}
		return s.updatePreviewPayload(ctx, store, "adgroups", "ad_group_bid", input.AccountInput, input.AdGroupID, payload)
	}
}

func (s *Service) adGroupCPACapPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		money := appleads.Money{Amount: input.Amount, Currency: strings.ToUpper(input.Currency)}
		if err := money.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		payload := map[string]any{"cpaCap": map[string]any{"value": money}}
		return s.updatePreviewPayload(ctx, store, "adgroups", "ad_group_cpa_cap", input.AccountInput, input.AdGroupID, payload)
	}
}

func (s *Service) validateWrite(ctx context.Context, account AccountInput, payload map[string]any) error {
	if err := validateAccount(account); err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.New("payload must not be empty")
	}
	if err := validateMutationPayload(payload); err != nil {
		return err
	}
	return s.writeAllowed(ctx, account.Profile, account.AdAccountID)
}

func validateMutationPayload(payload map[string]any) error {
	if err := appleads.ValidateRequestBody(payload); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return errors.New("payload is not valid JSON")
	}
	var normalized any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return errors.New("payload is not valid JSON")
	}
	return validateMutationValue(normalized, 0)
}

func validateMutationValue(value any, depth int) error {
	if depth > 20 {
		return errors.New("payload nesting exceeds 20 levels")
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > 200 {
			return errors.New("payload objects must contain at most 200 fields")
		}
		for key, item := range typed {
			if len(key) > 128 {
				return errors.New("payload field names must not exceed 128 characters")
			}
			if err := validateMutationValue(item, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) > 200 {
			return errors.New("payload arrays must contain at most 200 items")
		}
		for _, item := range typed {
			if err := validateMutationValue(item, depth+1); err != nil {
				return err
			}
		}
	case string:
		if len(typed) > 8192 {
			return errors.New("payload strings must not exceed 8192 bytes")
		}
	case json.Number, bool, nil:
	default:
		return fmt.Errorf("payload contains unsupported value type %T", value)
	}
	return nil
}

func (s *Service) writeAllowed(ctx context.Context, profileName, adAccountID string) error {
	if !s.allowWrites {
		return errors.New("server is read-only; restart with --allow-writes")
	}
	profile, err := s.manager.Profile(profileName)
	if err != nil {
		return err
	}
	if !profile.AllowWrites {
		return fmt.Errorf("profile %q does not allow writes", profile.Name)
	}
	result, err := s.manager.Do(ctx, profileName, "", appleads.ACLs())
	if err != nil {
		return fmt.Errorf("check Apple write role: %w", err)
	}
	roles, found := accountRoles(result.Data, adAccountID)
	if !found {
		return fmt.Errorf("ad account %q is not present in the profile ACL", adAccountID)
	}
	for _, role := range roles {
		if isWriteRole(role) {
			return nil
		}
	}
	return fmt.Errorf("Apple ACL for ad account %q has no recognized write role; roles: %s", adAccountID, strings.Join(roles, ", "))
}

func isWriteRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "api campaign manager", "api account manager", "api read & write", "account admin", "admin":
		return true
	default:
		return false
	}
}

func (s *Service) ensureAppStoreMutation(ctx context.Context, account AccountInput, resource, id string, payload map[string]any, create bool) error {
	if containsMapsValue(payload) {
		return errors.New("Apple Maps payloads are outside this server's scope")
	}
	if err := s.ensurePayloadCurrency(ctx, account, payload); err != nil {
		return err
	}
	switch resource {
	case "campaigns":
		if create {
			if value, _ := payload["promotedObjectType"].(string); value != "APPSTORE_APP" {
				return errors.New("campaign create requires promotedObjectType APPSTORE_APP")
			}
			adamID := stringField(payload, "promotedObjectId")
			if adamID == "" {
				return errors.New("campaign create requires promotedObjectId")
			}
			search, err := appleads.SearchApps(appleads.SearchAppsParams{Query: adamID, ReturnOwnedApps: true, Limit: 20})
			if err != nil {
				return err
			}
			owned, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, search)
			if err != nil {
				return fmt.Errorf("verify promoted app ownership: %w", err)
			}
			if !containsAdamID(owned.Data, adamID) {
				return fmt.Errorf("Apple did not confirm promotedObjectId %s as an owned app", adamID)
			}
			appOperation, err := appleads.App(adamID)
			if err != nil {
				return err
			}
			appDetails, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, appOperation)
			if err != nil {
				return fmt.Errorf("read promoted app storefronts: %w", err)
			}
			if err := ensureCampaignStorefronts(payload, appDetails.Data); err != nil {
				return err
			}
			if err := s.ensureCampaignEligibility(ctx, account, adamID, payload); err != nil {
				return err
			}
			return nil
		}
		current, err := s.readResource(ctx, account, resource, id)
		if err != nil {
			return err
		}
		if findStringField(current, "promotedObjectType") != "APPSTORE_APP" {
			return errors.New("only APPSTORE_APP campaigns are supported")
		}
		if requested := stringField(payload, "promotedObjectType"); requested != "" && requested != "APPSTORE_APP" {
			return errors.New("campaign promotedObjectType cannot leave APPSTORE_APP scope")
		}
		if requested := stringField(payload, "promotedObjectId"); requested != "" && requested != findStringField(current, "promotedObjectId") {
			return errors.New("campaign promotedObjectId cannot be changed")
		}
		currentPlacement := stringSlice(findMapField(findMapField(current, "targeting"), "supplyPlacement")["include"])
		requestedPlacement := stringSlice(findMapField(findMapField(payload, "targeting"), "supplyPlacement")["include"])
		if len(requestedPlacement) > 0 && (len(currentPlacement) != 1 || requestedPlacement[0] != currentPlacement[0]) {
			return errors.New("campaign placement cannot be changed after creation")
		}
		if len(stringSlice(findMapField(findMapField(payload, "targeting"), "countryOrRegion")["include"])) > 0 {
			adamID := findStringField(current, "promotedObjectId")
			app, appErr := appleads.App(adamID)
			if appErr != nil {
				return appErr
			}
			appResult, appErr := s.manager.Do(ctx, account.Profile, account.AdAccountID, app)
			if appErr != nil {
				return fmt.Errorf("verify campaign storefronts: %w", appErr)
			}
			if appErr := ensureCampaignStorefronts(payload, appResult.Data); appErr != nil {
				return appErr
			}
			eligibilityTargeting := cloneObject(findMapField(current, "targeting"))
			if eligibilityTargeting == nil {
				return errors.New("current campaign has no targeting for eligibility validation")
			}
			eligibilityPayload := map[string]any{"targeting": eligibilityTargeting}
			eligibilityTargeting["countryOrRegion"] = cloneObject(findMapField(findMapField(payload, "targeting"), "countryOrRegion"))
			if appErr := s.ensureCampaignEligibility(ctx, account, adamID, eligibilityPayload); appErr != nil {
				return appErr
			}
		}
		return nil
	case "adgroups":
		campaignID, err := s.parentID(ctx, account, resource, id, payload, "campaignId", create)
		if err != nil {
			return err
		}
		if err := s.ensureAppStoreCampaign(ctx, account, campaignID); err != nil {
			return err
		}
		if enabled, ok := payload["automatedKeywordsOptIn"].(bool); ok && enabled {
			return s.ensureCampaignPlacement(ctx, account, campaignID, "APPSTORE_SEARCH_RESULTS")
		}
		return nil
	case "keywords", "ads":
		adGroupID, err := s.parentID(ctx, account, resource, id, payload, "adGroupId", create)
		if err != nil {
			return err
		}
		campaignID, err := s.parentID(ctx, account, "adgroups", adGroupID, nil, "campaignId", false)
		if err != nil {
			return err
		}
		if err := s.ensureAppStoreCampaign(ctx, account, campaignID); err != nil {
			return err
		}
		placement, err := s.campaignPlacement(ctx, account, campaignID)
		if err != nil {
			return err
		}
		if resource == "keywords" && placement != "APPSTORE_SEARCH_RESULTS" {
			return fmt.Errorf("keywords require APPSTORE_SEARCH_RESULTS; campaign placement is %s", placement)
		}
		if resource == "ads" {
			if !explicitAdsSupported(placement) {
				return fmt.Errorf("explicit ads are not supported for campaign placement %s", placement)
			}
			creativeID := stringField(payload, "creativeId")
			if create && creativeID == "" {
				return errors.New("ad create requires creativeId")
			}
			if creativeID != "" {
				if err := s.ensureAppStoreCreative(ctx, account, creativeID, placement); err != nil {
					return err
				}
			}
		}
		return nil
	case "negative-keywords":
		campaignID := stringField(payload, "campaignId")
		adGroupID := stringField(payload, "adGroupId")
		if !create {
			current, err := s.readResource(ctx, account, resource, id)
			if err != nil {
				return err
			}
			campaignID = findStringField(current, "campaignId")
			adGroupID = findStringField(current, "adGroupId")
			if requested := stringField(payload, "campaignId"); requested != "" && requested != campaignID {
				return errors.New("negative keyword campaignId cannot be changed")
			}
			if requested := stringField(payload, "adGroupId"); requested != "" && requested != adGroupID {
				return errors.New("negative keyword adGroupId cannot be changed")
			}
		}
		if campaignID == "" && adGroupID != "" {
			var err error
			campaignID, err = s.parentID(ctx, account, "adgroups", adGroupID, nil, "campaignId", false)
			if err != nil {
				return err
			}
		}
		if campaignID == "" {
			return errors.New("negative keyword must resolve to a campaign or ad group")
		}
		if err := s.ensureAppStoreCampaign(ctx, account, campaignID); err != nil {
			return err
		}
		return s.ensureCampaignPlacement(ctx, account, campaignID, "APPSTORE_SEARCH_RESULTS")
	case "creatives":
		creativeType := stringField(payload, "creativeType")
		if !create && creativeType == "" {
			current, err := s.readResource(ctx, account, resource, id)
			if err != nil {
				return err
			}
			creativeType = findStringField(current, "creativeType")
		}
		if creativeType != "DEFAULT_PRODUCT_PAGE" && creativeType != "CUSTOM_PRODUCT_PAGE" {
			return fmt.Errorf("creative type %q is outside App Store scope", creativeType)
		}
		if create {
			destination := findMapField(payload, "destination")
			parameters := findMapField(destination, "parameters")
			adamID := stringField(parameters, "adamId")
			search, err := appleads.SearchApps(appleads.SearchAppsParams{Query: adamID, ReturnOwnedApps: true, Limit: 20})
			if err != nil {
				return err
			}
			owned, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, search)
			if err != nil {
				return fmt.Errorf("verify creative app ownership: %w", err)
			}
			if !containsAdamID(owned.Data, adamID) {
				return fmt.Errorf("Apple did not confirm creative adamId %s as an owned app", adamID)
			}
			if productPageID := stringField(parameters, "productPageId"); productPageID != "" {
				op, err := appleads.ProductPage(productPageID)
				if err != nil {
					return err
				}
				page, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, op)
				if err != nil {
					return fmt.Errorf("verify Custom Product Page: %w", err)
				}
				if pageAdamID := findStringField(page.Data, "adamId"); pageAdamID != "" && pageAdamID != adamID {
					return errors.New("Custom Product Page belongs to a different app")
				}
			}
		}
	case "shared-budgets":
		return nil
	}
	return nil
}

func explicitAdsSupported(placement string) bool {
	return placement != "APPSTORE_SEARCH_RESULTS"
}

func (s *Service) ensureAppStoreCampaign(ctx context.Context, account AccountInput, campaignID string) error {
	current, err := s.readResource(ctx, account, "campaigns", campaignID)
	if err != nil {
		return err
	}
	value := findStringField(current, "promotedObjectType")
	if value != "APPSTORE_APP" {
		return fmt.Errorf("campaign %s has promotedObjectType %q; only APPSTORE_APP is supported", campaignID, value)
	}
	return nil
}

func (s *Service) campaignPlacement(ctx context.Context, account AccountInput, campaignID string) (string, error) {
	current, err := s.readResource(ctx, account, "campaigns", campaignID)
	if err != nil {
		return "", err
	}
	targeting := findMapField(current, "targeting")
	placement := findMapField(targeting, "supplyPlacement")
	values := stringSlice(placement["include"])
	if len(values) != 1 {
		return "", fmt.Errorf("campaign %s does not have exactly one App Store placement", campaignID)
	}
	return values[0], nil
}

func (s *Service) ensureCampaignPlacement(ctx context.Context, account AccountInput, campaignID, expected string) error {
	placement, err := s.campaignPlacement(ctx, account, campaignID)
	if err != nil {
		return err
	}
	if placement != expected {
		return fmt.Errorf("campaign %s uses %s; this operation requires %s", campaignID, placement, expected)
	}
	return nil
}

func (s *Service) ensurePayloadCurrency(ctx context.Context, account AccountInput, payload map[string]any) error {
	currencies := collectFieldValues(payload, "currency", MaxItems)
	if len(currencies) == 0 {
		return nil
	}
	op, err := appleads.AdAccount(account.AdAccountID)
	if err != nil {
		return err
	}
	result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, op)
	if err != nil {
		return fmt.Errorf("read account currency: %w", err)
	}
	accountCurrency := strings.ToUpper(findStringField(result.Data, "currency"))
	if accountCurrency == "" {
		return errors.New("Apple ad account response did not include currency")
	}
	for _, currency := range currencies {
		if strings.ToUpper(currency) != accountCurrency {
			return fmt.Errorf("money currency %s does not match ad account currency %s", currency, accountCurrency)
		}
	}
	return nil
}

func ensureCampaignStorefronts(payload map[string]any, app any) error {
	targeting := findMapField(payload, "targeting")
	countries := stringSlice(findMapField(targeting, "countryOrRegion")["include"])
	if len(countries) == 0 {
		return nil
	}
	available := map[string]struct{}{}
	for _, storefront := range findStringSliceField(app, "availableStorefronts") {
		available[strings.ToUpper(storefront)] = struct{}{}
	}
	if len(available) == 0 {
		return errors.New("Apple did not return availableStorefronts for the promoted app")
	}
	for _, country := range countries {
		if _, ok := available[strings.ToUpper(country)]; !ok {
			return fmt.Errorf("promoted app is not available in storefront %s", country)
		}
	}
	return nil
}

func (s *Service) ensureCampaignEligibility(ctx context.Context, account AccountInput, adamID string, payload map[string]any) error {
	targeting := findMapField(payload, "targeting")
	countries := stringSlice(findMapField(targeting, "countryOrRegion")["include"])
	placements := stringSlice(findMapField(targeting, "supplyPlacement")["include"])
	if len(countries) == 0 || len(placements) != 1 {
		return errors.New("campaign eligibility requires countries and exactly one placement")
	}
	body := map[string]any{
		"filters": []any{
			map[string]any{"field": "adamId", "operator": "EQUALS", "value": wireID(adamID)},
			map[string]any{"field": "supplyPlacement", "operator": "EQUALS", "value": placements[0]},
			map[string]any{"field": "countryOrRegion", "operator": "IN", "value": countries},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	}
	result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, appleads.AppsEligibility(body))
	if err != nil {
		return fmt.Errorf("verify app placement eligibility: %w", err)
	}
	for _, country := range countries {
		if !hasEligiblePlacement(result.Data, adamID, placements[0], country) {
			return fmt.Errorf("Apple did not confirm app %s as eligible for %s in %s", adamID, placements[0], country)
		}
	}
	return nil
}

func hasEligiblePlacement(value any, adamID, placement, country string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if fmt.Sprint(typed["adamId"]) == adamID && fmt.Sprint(typed["supplyPlacement"]) == placement && strings.EqualFold(fmt.Sprint(typed["countryOrRegion"]), country) && fmt.Sprint(typed["state"]) == "ELIGIBLE" {
			return true
		}
		for _, item := range typed {
			if hasEligiblePlacement(item, adamID, placement, country) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if hasEligiblePlacement(item, adamID, placement, country) {
				return true
			}
		}
	}
	return false
}

func findStringSliceField(value any, field string) []string {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[field]; ok {
			if values := stringSlice(item); len(values) > 0 {
				return values
			}
		}
		for _, item := range typed {
			if values := findStringSliceField(item, field); len(values) > 0 {
				return values
			}
		}
	case []any:
		for _, item := range typed {
			if values := findStringSliceField(item, field); len(values) > 0 {
				return values
			}
		}
	}
	return nil
}

func (s *Service) ensureAppStoreCreative(ctx context.Context, account AccountInput, creativeID, placement string) error {
	current, err := s.readResource(ctx, account, "creatives", creativeID)
	if err != nil {
		return err
	}
	creativeType := findStringField(current, "creativeType")
	if creativeType != "DEFAULT_PRODUCT_PAGE" && creativeType != "CUSTOM_PRODUCT_PAGE" {
		return fmt.Errorf("creative %s has type %q; only App Store creatives are supported", creativeID, creativeType)
	}
	if status := findStringField(current, "systemStatus"); status != "VALID" {
		return fmt.Errorf("creative %s has systemStatus %q; ads require a VALID creative", creativeID, status)
	}
	eligibility := findMapField(current, "eligibility")
	if blocked := eligibility["blockedGroups"]; blocked != nil && containsStringValue(blocked, placement) {
		return fmt.Errorf("creative %s is blocked for placement %s", creativeID, placement)
	}
	if allowed := eligibility["allowedGroups"]; hasPlacementValue(allowed) && !containsStringValue(allowed, placement) {
		return fmt.Errorf("creative %s is not eligible for placement %s", creativeID, placement)
	}
	return nil
}

func hasPlacementValue(value any) bool {
	for _, placement := range []string{"APPSTORE_SEARCH_RESULTS", "APPSTORE_SEARCH_TAB", "APPSTORE_TODAY_TAB", "APPSTORE_PRODUCT_PAGES"} {
		if containsStringValue(value, placement) {
			return true
		}
	}
	return false
}

func (s *Service) resourceImpact(ctx context.Context, account AccountInput, resource, id string, payload map[string]any, create bool) (*operations.OperationImpact, error) {
	impact := &operations.OperationImpact{SpendAffecting: resource != "creatives", ObjectCount: 1}
	if currencies := collectFieldValues(payload, "currency", MaxItems); len(currencies) > 0 {
		impact.Currency = strings.ToUpper(currencies[0])
	}
	switch resource {
	case "campaigns":
		if create {
			impact.ParentIDs = []string{stringField(payload, "promotedObjectId")}
			placements := stringSlice(findMapField(findMapField(payload, "targeting"), "supplyPlacement")["include"])
			if len(placements) == 1 {
				impact.Placement = placements[0]
			}
			return impact, nil
		}
		placement, err := s.campaignPlacement(ctx, account, id)
		if err != nil {
			return nil, err
		}
		impact.Placement = placement
		impact.ParentIDs = []string{id}
	case "adgroups":
		campaignID, err := s.parentID(ctx, account, resource, id, payload, "campaignId", create)
		if err != nil {
			return nil, err
		}
		placement, err := s.campaignPlacement(ctx, account, campaignID)
		if err != nil {
			return nil, err
		}
		impact.Placement = placement
		impact.ParentIDs = []string{campaignID}
	case "keywords", "ads":
		adGroupID, err := s.parentID(ctx, account, resource, id, payload, "adGroupId", create)
		if err != nil {
			return nil, err
		}
		campaignID, err := s.parentID(ctx, account, "adgroups", adGroupID, nil, "campaignId", false)
		if err != nil {
			return nil, err
		}
		placement, err := s.campaignPlacement(ctx, account, campaignID)
		if err != nil {
			return nil, err
		}
		impact.Placement = placement
		impact.ParentIDs = []string{campaignID, adGroupID}
	case "negative-keywords":
		campaignID := stringField(payload, "campaignId")
		adGroupID := stringField(payload, "adGroupId")
		if !create {
			current, err := s.readResource(ctx, account, resource, id)
			if err != nil {
				return nil, err
			}
			campaignID = findStringField(current, "campaignId")
			adGroupID = findStringField(current, "adGroupId")
		}
		if campaignID == "" && adGroupID != "" {
			var err error
			campaignID, err = s.parentID(ctx, account, "adgroups", adGroupID, nil, "campaignId", false)
			if err != nil {
				return nil, err
			}
		}
		placement, err := s.campaignPlacement(ctx, account, campaignID)
		if err != nil {
			return nil, err
		}
		impact.Placement = placement
		impact.ParentIDs = []string{campaignID}
		if adGroupID != "" {
			impact.ParentIDs = append(impact.ParentIDs, adGroupID)
		}
	case "creatives":
		impact.SpendAffecting = false
	}
	return impact, nil
}

func (s *Service) parentID(ctx context.Context, account AccountInput, resource, id string, payload map[string]any, field string, create bool) (string, error) {
	if create {
		value := stringField(payload, field)
		if value == "" {
			return "", fmt.Errorf("%s is required", field)
		}
		return value, nil
	}
	current, err := s.readResource(ctx, account, resource, id)
	if err != nil {
		return "", err
	}
	value := findStringField(current, field)
	if value == "" {
		return "", fmt.Errorf("current %s %s has no %s", resource, id, field)
	}
	if requested := stringField(payload, field); requested != "" && requested != value {
		return "", fmt.Errorf("%s cannot be changed", field)
	}
	return value, nil
}

func (s *Service) readResource(ctx context.Context, account AccountInput, resource, id string) (any, error) {
	op, err := appleads.ResourceGet(resource, id)
	if err != nil {
		return nil, err
	}
	result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, op)
	if err != nil {
		return nil, fmt.Errorf("verify App Store resource lineage: %w", err)
	}
	return result.Data, nil
}

func containsMapsValue(value any) bool {
	switch typed := value.(type) {
	case string:
		upper := strings.ToUpper(typed)
		return strings.Contains(upper, "MAPS") || strings.Contains(upper, "LOCAL_ADS")
	case []any:
		for _, item := range typed {
			if containsMapsValue(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			switch key {
			case "businessBrandId", "businessOrgId", "locationGroupId":
				return true
			}
			if containsMapsValue(item) {
				return true
			}
		}
	}
	return false
}

func stringField(value map[string]any, field string) string {
	if value == nil {
		return ""
	}
	item, ok := value[field]
	if !ok || item == nil {
		return ""
	}
	return fmt.Sprint(item)
}

func findStringField(value any, field string) string {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[field]; ok && item != nil {
			return fmt.Sprint(item)
		}
		for _, item := range typed {
			if found := findStringField(item, field); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findStringField(item, field); found != "" {
				return found
			}
		}
	}
	return ""
}

func accountRoles(data any, adAccountID string) ([]string, bool) {
	root, ok := data.(map[string]any)
	if !ok {
		return nil, false
	}
	items, ok := root["acls"].([]any)
	if !ok {
		return nil, false
	}
	for _, item := range items {
		acl, ok := item.(map[string]any)
		if !ok {
			continue
		}
		account, ok := acl["adAccount"].(map[string]any)
		if !ok || fmt.Sprint(account["id"]) != adAccountID {
			continue
		}
		values, _ := acl["roles"].([]any)
		roles := make([]string, 0, len(values))
		for _, value := range values {
			if role, ok := value.(string); ok {
				roles = append(roles, role)
			}
		}
		return roles, true
	}
	return nil, false
}

func addPreviewTool[In any](server *mcp.Server, item Spec, handler mcp.ToolHandlerFor[In, PreviewOutput]) {
	destructive := false
	open := true
	mcp.AddTool(server, &mcp.Tool{
		Name: item.Name, Description: item.Description,
		Annotations: &mcp.ToolAnnotations{Title: strings.ReplaceAll(item.Name, "_", " "), ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &destructive, OpenWorldHint: &open},
	}, handler)
}

func previewSuccess(preview operations.OperationPreview) (*mcp.CallToolResult, PreviewOutput, error) {
	output := PreviewOutput{Summary: "Preview created; no write has occurred", Preview: &preview}
	return textResult(output.Summary, false), output, nil
}

func failedPreview(err error) (*mcp.CallToolResult, PreviewOutput, error) {
	failure := errorOutput(err)
	output := PreviewOutput{Summary: failure.Summary, Error: failure.Error}
	return textResult(output.Summary, true), output, nil
}

func failedApply(err error) (*mcp.CallToolResult, ApplyOutput, error) {
	failure := errorOutput(err)
	output := ApplyOutput{Summary: failure.Summary, Error: failure.Error}
	return textResult(output.Summary, true), output, nil
}

func mutationSpec(name string) Spec {
	for _, item := range MutationSpecs() {
		if item.Name == name {
			return item
		}
	}
	panic("missing mutation tool spec: " + name)
}
