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

type CreatePreviewInput struct {
	AccountInput
	Payload map[string]any `json:"payload" jsonschema:"complete create payload for the named resource"`
}

type UpdatePreviewInput struct {
	AccountInput
	ID      string         `json:"id" jsonschema:"target resource ID as a string"`
	Payload map[string]any `json:"payload" jsonschema:"fields to update"`
}

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
		{Name: "ad_group_create_preview", Description: "Preview creation of one ad group.", Class: "mutation_preview"},
		{Name: "ad_group_update_preview", Description: "Preview an ad-group update.", Class: "mutation_preview"},
		{Name: "ad_group_bid_preview", Description: "Preview an ad-group default bid update with decimal amount and currency.", Class: "mutation_preview"},
		{Name: "ad_group_cpa_cap_preview", Description: "Preview an ad-group CPA cap update with decimal amount and currency.", Class: "mutation_preview"},
		{Name: "keyword_create_preview", Description: "Preview creation of one targeting keyword.", Class: "mutation_preview"},
		{Name: "keyword_update_preview", Description: "Preview a targeting keyword update.", Class: "mutation_preview"},
		{Name: "negative_keyword_create_preview", Description: "Preview creation of one negative keyword.", Class: "mutation_preview"},
		{Name: "negative_keyword_update_preview", Description: "Preview a negative keyword update.", Class: "mutation_preview"},
		{Name: "ad_create_preview", Description: "Preview creation of one ad.", Class: "mutation_preview"},
		{Name: "ad_update_preview", Description: "Preview an ad update.", Class: "mutation_preview"},
		{Name: "creative_create_preview", Description: "Preview creation of one creative.", Class: "mutation_preview"},
		{Name: "creative_update_preview", Description: "Preview a creative update.", Class: "mutation_preview"},
		{Name: "operations_apply", Description: "Apply exactly one non-expired, drift-free preview receipt.", Class: "mutation"},
		{Name: "operations_inspect", Description: "Inspect receipt binding, expiry, and use state without applying it.", Class: "read"},
		{Name: "operations_verify", Description: "Re-read a receipt target after an ambiguous write and return current state.", Class: "read"},
	}
}

func (s *Service) RegisterMutationTools(server *mcp.Server, store *operations.Store) {
	for _, item := range []struct{ create, update, resource string }{
		{"campaign_create_preview", "campaign_update_preview", "campaigns"},
		{"ad_group_create_preview", "ad_group_update_preview", "adgroups"},
		{"keyword_create_preview", "keyword_update_preview", "keywords"},
		{"negative_keyword_create_preview", "negative_keyword_update_preview", "negative-keywords"},
		{"ad_create_preview", "ad_update_preview", "ads"},
		{"creative_create_preview", "creative_update_preview", "creatives"},
	} {
		resource := item.resource
		addPreviewTool(server, mutationSpec(item.create), s.createPreview(store, resource, item.create))
		addPreviewTool(server, mutationSpec(item.update), s.updatePreview(store, resource, item.update))
	}
	addPreviewTool(server, mutationSpec("campaign_pause_preview"), s.campaignStatePreview(store, "PAUSED", "campaign_pause"))
	addPreviewTool(server, mutationSpec("campaign_resume_preview"), s.campaignStatePreview(store, "ENABLED", "campaign_resume"))
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

func (s *Service) createPreview(store *operations.Store, resource, name string) func(context.Context, *mcp.CallToolRequest, CreatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CreatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		input.Payload = queryRequest(input.Payload)
		if resource == "campaigns" {
			if value := stringField(input.Payload, "adAccountId"); value != "" && value != input.AdAccountID {
				return failedPreview(errors.New("campaign payload adAccountId does not match the explicit ad account"))
			}
			input.Payload["adAccountId"] = input.AdAccountID
		}
		if err := s.validateWrite(ctx, input.AccountInput, input.Payload); err != nil {
			return failedPreview(err)
		}
		if err := s.ensureAppStoreMutation(ctx, input.AccountInput, resource, "", input.Payload, true); err != nil {
			return failedPreview(err)
		}
		verify, err := appleads.ResourceQuery(resource, createVerificationQuery(resource, input.Payload))
		if err != nil {
			return failedPreview(err)
		}
		mutation, err := appleads.ResourceCreate(resource, input.Payload)
		if err != nil {
			return failedPreview(err)
		}
		preview, err := store.Preview(ctx, s.manager, input.Profile, input.AdAccountID, name, nil, input.Payload, verify, mutation)
		if err != nil {
			return failedPreview(err)
		}
		return previewSuccess(preview)
	}
}

func createVerificationQuery(resource string, payload map[string]any) map[string]any {
	request := map[string]any{"pagination": map[string]any{"offset": 0, "pageSize": MaxItems}}
	var parentField string
	switch resource {
	case "adgroups":
		parentField = "campaignId"
	case "keywords", "negative-keywords", "ads":
		parentField = "adGroupId"
	}
	if parentField != "" {
		if parentID := stringField(payload, parentField); parentID != "" {
			request["filters"] = []any{map[string]any{"field": parentField, "operator": "EQUALS", "value": parentID}}
		}
	}
	return request
}

func (s *Service) updatePreview(store *operations.Store, resource, name string) func(context.Context, *mcp.CallToolRequest, UpdatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input UpdatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		if err := s.validateWrite(ctx, input.AccountInput, input.Payload); err != nil {
			return failedPreview(err)
		}
		if err := s.ensureAppStoreMutation(ctx, input.AccountInput, resource, input.ID, input.Payload, false); err != nil {
			return failedPreview(err)
		}
		verify, err := appleads.ResourceGet(resource, input.ID)
		if err != nil {
			return failedPreview(err)
		}
		mutation, err := appleads.ResourceUpdate(resource, input.ID, input.Payload)
		if err != nil {
			return failedPreview(err)
		}
		preview, err := store.Preview(ctx, s.manager, input.Profile, input.AdAccountID, name, []string{input.ID}, input.Payload, verify, mutation)
		if err != nil {
			return failedPreview(err)
		}
		return previewSuccess(preview)
	}
}

func (s *Service) campaignStatePreview(store *operations.Store, status, name string) func(context.Context, *mcp.CallToolRequest, CampaignStatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CampaignStatePreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		payload := map[string]any{"status": status}
		return s.updatePreview(store, "campaigns", name)(ctx, nil, UpdatePreviewInput{AccountInput: input.AccountInput, ID: input.CampaignID, Payload: payload})
	}
}

func (s *Service) adGroupBidPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		money := appleads.Money{Amount: input.Amount, Currency: strings.ToUpper(input.Currency)}
		if err := money.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		payload := map[string]any{"bidStrategy": map[string]any{"bid": money}}
		return s.updatePreview(store, "adgroups", "ad_group_bid")(ctx, nil, UpdatePreviewInput{AccountInput: input.AccountInput, ID: input.AdGroupID, Payload: payload})
	}
}

func (s *Service) adGroupCPACapPreview(store *operations.Store) func(context.Context, *mcp.CallToolRequest, AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input AdGroupBidPreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
		money := appleads.Money{Amount: input.Amount, Currency: strings.ToUpper(input.Currency)}
		if err := money.ValidatePositive(); err != nil {
			return failedPreview(err)
		}
		payload := map[string]any{"cpaCap": map[string]any{"value": money}}
		return s.updatePreview(store, "adgroups", "ad_group_cpa_cap")(ctx, nil, UpdatePreviewInput{AccountInput: input.AccountInput, ID: input.AdGroupID, Payload: payload})
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
		return nil
	case "adgroups":
		campaignID, err := s.parentID(ctx, account, resource, id, payload, "campaignId", create)
		if err != nil {
			return err
		}
		return s.ensureAppStoreCampaign(ctx, account, campaignID)
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
		if resource == "ads" {
			creativeID := stringField(payload, "creativeId")
			if create && creativeID == "" {
				return errors.New("ad create requires creativeId")
			}
			if creativeID != "" {
				if err := s.ensureAppStoreCreative(ctx, account, creativeID); err != nil {
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
		return s.ensureAppStoreCampaign(ctx, account, campaignID)
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
	case "shared-budgets":
		return nil
	}
	return nil
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

func (s *Service) ensureAppStoreCreative(ctx context.Context, account AccountInput, creativeID string) error {
	current, err := s.readResource(ctx, account, "creatives", creativeID)
	if err != nil {
		return err
	}
	creativeType := findStringField(current, "creativeType")
	if creativeType != "DEFAULT_PRODUCT_PAGE" && creativeType != "CUSTOM_PRODUCT_PAGE" {
		return fmt.Errorf("creative %s has type %q; only App Store creatives are supported", creativeID, creativeType)
	}
	return nil
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
