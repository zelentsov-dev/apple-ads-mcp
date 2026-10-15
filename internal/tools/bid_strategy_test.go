package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

func strategyFixture(strategyType string) map[string]any {
	goal := "TAP"
	if strategyType == "MAX_CONVERSIONS" {
		goal = "INSTALL"
	}
	return map[string]any{
		"id": "100", "name": "Synthetic campaign", "promotedObjectType": "APPSTORE_APP", "billingEvent": "TAPS", "status": "PAUSED",
		"targeting":   map[string]any{"supplyPlacement": map[string]any{"include": []any{"APPSTORE_SEARCH_RESULTS"}}},
		"bidStrategy": map[string]any{"bidStrategyType": strategyType, "bidStrategyGoal": goal, "bid": map[string]any{"amount": "1.125", "currency": "USD"}},
	}
}

func strategyManager(strategyType string) *v036Manager {
	return &v036Manager{
		profile: config.Profile{Name: "synthetic", AllowWrites: true},
		responses: map[string][]appleads.Result{
			"acls":            {{Data: map[string]any{"acls": []any{map[string]any{"adAccount": map[string]any{"id": "123"}, "roles": []any{"API Campaign Manager"}}}}}},
			"ad-accounts/123": {{Data: map[string]any{"currency": "USD", "privateBillingField": "must-not-be-exposed"}}},
			"campaigns/100":   {{Data: strategyFixture(strategyType)}},
			"adgroups/10":     {{Data: map[string]any{"id": "10", "campaignId": "100", "pricingModel": "CPT", "status": "PAUSED"}}},
		},
	}
}

func TestBidStrategyCreateAndUpdateContract(t *testing.T) {
	bid := map[string]any{"amount": "0.00125", "currency": "USD"}
	for _, test := range []struct {
		name                    string
		value                   any
		create, required, valid bool
	}{
		{"required campaign", nil, true, true, false},
		{"inherited group", nil, true, false, true},
		{"empty object", map[string]any{}, false, false, false},
		{"type only", map[string]any{"bidStrategyType": "MANUAL_CPT"}, false, false, false},
		{"goal only", map[string]any{"bidStrategyGoal": "TAP"}, false, false, false},
		{"wrong pair", map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "TAP"}, false, false, false},
		{"manual creation requires bid", map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP"}, true, true, false},
		{"manual precision", map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP", "bid": bid}, true, true, true},
		{"auto creation ceiling", map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "INSTALL", "bid": bid}, true, true, true},
		{"auto creation no ceiling", map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "INSTALL"}, true, true, true},
		{"partial manual bid", map[string]any{"bid": bid}, false, false, true},
		{"create bid only", map[string]any{"bid": bid}, true, false, false},
		{"auto creation zero", map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "INSTALL", "bid": map[string]any{"amount": "0", "currency": "USD"}}, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateBidStrategyForOperation(test.value, test.create, test.required)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
}

func TestAdGroupContextPreventsStrategyBypass(t *testing.T) {
	for _, test := range []struct {
		name, parent, amount, strategyType, goal string
		create, valid                            bool
	}{
		{"manual partial", "MANUAL_CPT", "0.00125", "", "", false, true},
		{"auto partial rejected", "MAX_CONVERSIONS", "1", "", "", false, false},
		{"auto nonzero rejected", "MAX_CONVERSIONS", "1", "MAX_CONVERSIONS", "INSTALL", false, false},
		{"auto zero echo", "MAX_CONVERSIONS", "0.00", "MAX_CONVERSIONS", "INSTALL", false, true},
		{"auto create ceiling", "MAX_CONVERSIONS", "1.125", "MAX_CONVERSIONS", "INSTALL", true, true},
		{"incompatible manual", "MAX_CONVERSIONS", "1", "MANUAL_CPT", "TAP", false, false},
		{"incompatible auto", "MANUAL_CPT", "0", "MAX_CONVERSIONS", "INSTALL", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requested := map[string]any{"bid": map[string]any{"amount": test.amount, "currency": "USD"}}
			if test.strategyType != "" {
				requested["bidStrategyType"], requested["bidStrategyGoal"] = test.strategyType, test.goal
			}
			err := validateAdGroupStrategyContext(strategyFixture(test.parent), map[string]any{"pricingModel": "CPT"}, requested, test.create)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
	if err := validateAdGroupStrategyContext(strategyFixture("MAX_CONVERSIONS"), map[string]any{"pricingModel": "CPT"}, nil, true); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitStrategyPreviewNeedsNoHistoricalReadAndPersistsWarnings(t *testing.T) {
	for _, specialized := range []bool{false, true} {
		manager := strategyManager("MANUAL_CPT")
		service := &Service{manager: manager, allowWrites: true}
		store := operations.NewStore()
		account := AccountInput{Profile: "synthetic", AdAccountID: "123"}
		strategy := BidStrategy{BidStrategyType: "MAX_CONVERSIONS", BidStrategyGoal: "INSTALL"}
		var output PreviewOutput
		var err error
		if specialized {
			_, output, err = service.campaignBidStrategyPreview(store)(t.Context(), nil, CampaignBidStrategyPreviewInput{AccountInput: account, CampaignID: "100", Strategy: strategy})
		} else {
			_, output, err = typedUpdatePreview[CampaignUpdatePayload](service, store, "campaigns", "campaign_update_preview")(t.Context(), nil, TypedUpdatePreviewInput[CampaignUpdatePayload]{AccountInput: account, ID: "100", Payload: CampaignUpdatePayload{BidStrategy: &strategy}})
		}
		if err != nil || output.Error != nil || output.Preview == nil {
			t.Fatalf("specialized=%v output=%+v err=%v", specialized, output, err)
		}
		preview, used, err := store.Inspect(output.Preview.Receipt)
		if err != nil || used || len(preview.Warnings) != 2 {
			t.Fatalf("preview=%+v used=%v err=%v", preview, used, err)
		}
		for _, path := range manager.calls {
			if strings.Contains(path, "reports/") {
				t.Fatalf("unexpected report request %s", path)
			}
		}
	}
}

func TestAdGroupPreviewBindsParentAndAccount(t *testing.T) {
	for _, drift := range []string{"campaigns/100", "ad-accounts/123"} {
		manager := strategyManager("MANUAL_CPT")
		service := &Service{manager: manager, allowWrites: true}
		store := operations.NewStore()
		_, output, err := service.adGroupBidPreview(store)(t.Context(), nil, AdGroupBidPreviewInput{AccountInput: AccountInput{Profile: "synthetic", AdAccountID: "123"}, AdGroupID: "10", Amount: "1.125", Currency: "USD"})
		if err != nil || output.Error != nil || output.Preview == nil {
			t.Fatalf("output=%+v err=%v", output, err)
		}
		if strings.Contains(fmt.Sprint(output.Preview.Before), "privateBillingField") {
			t.Fatal("account details leaked into preview")
		}
		if drift == "campaigns/100" {
			manager.responses[drift] = []appleads.Result{{Data: strategyFixture("MAX_CONVERSIONS")}}
		} else {
			manager.responses[drift] = []appleads.Result{{Data: map[string]any{"currency": "EUR"}}}
		}
		_, err = store.Apply(t.Context(), manager, output.Preview.Receipt)
		if !errors.Is(err, operations.ErrStateDrift) {
			t.Fatalf("drift=%s err=%v", drift, err)
		}
	}
}

func TestLegacyCPACapWarningsSurviveInspect(t *testing.T) {
	manager := strategyManager("MANUAL_CPT")
	service := &Service{manager: manager, allowWrites: true}
	store := operations.NewStore()
	_, output, err := service.adGroupCPACapPreview(store)(t.Context(), nil, AdGroupBidPreviewInput{AccountInput: AccountInput{Profile: "synthetic", AdAccountID: "123"}, AdGroupID: "10", Amount: "2.125", Currency: "USD"})
	if err != nil || output.Error != nil || output.Preview == nil {
		t.Fatalf("output=%+v err=%v", output, err)
	}
	preview, _, err := store.Inspect(output.Preview.Receipt)
	if err != nil || len(preview.Warnings) != 1 || preview.Warnings[0].Code != "CPA_CAP_DEPRECATED" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
}

func TestOptimizationStrategyWirePayloadIncludesGoal(t *testing.T) {
	_, payload, err := optimizationMutation(optimization.PlanAction{ResourceType: "campaign", Action: "bid_strategy", After: map[string]any{"bidStrategy": "MAX_CONVERSIONS"}})
	if err != nil || stringField(payload["bidStrategy"].(map[string]any), "bidStrategyGoal") != "INSTALL" {
		t.Fatalf("payload=%v err=%v", payload, err)
	}
}

func TestCampaignEligibilityUsesDocumentedFields(t *testing.T) {
	campaign := strategyFixture("MANUAL_CPT")
	if !campaignEvidenceFromObject("100", campaign).MaxConversionsEligible {
		t.Fatal("documented contract should establish eligibility without undocumented flags")
	}
	campaign["billingEvent"] = "IMPRESSIONS"
	campaign["unrelated"] = map[string]any{"value": "MAX_CONVERSIONS", "maxConversionsEligible": true}
	if campaignEvidenceFromObject("100", campaign).MaxConversionsEligible {
		t.Fatal("unrelated strings or flags must not authorize a strategy")
	}
}

func TestCampaignStrategyValidatesFinalTargeting(t *testing.T) {
	for _, withStrategy := range []bool{true, false} {
		manager := strategyManager("MAX_CONVERSIONS")
		service := &Service{manager: manager, allowWrites: true}
		payload := map[string]any{"targeting": map[string]any{"supplyPlacement": map[string]any{"include": []any{"APPSTORE_SEARCH_TAB"}}}}
		if withStrategy {
			payload["bidStrategy"] = map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "INSTALL"}
		}
		_, _, err := service.bidStrategyEvidence(t.Context(), AccountInput{Profile: "synthetic", AdAccountID: "123"}, "campaigns", "100", payload, false)
		if err == nil || !strings.Contains(err.Error(), "APPSTORE_SEARCH_RESULTS") {
			t.Fatalf("withStrategy=%v err=%v", withStrategy, err)
		}
		_, output, err := service.updatePreviewPayload(t.Context(), operations.NewStore(), "campaigns", "campaign_update_preview", AccountInput{Profile: "synthetic", AdAccountID: "123"}, "100", payload)
		if err != nil || output.Error == nil || output.Preview != nil {
			t.Fatalf("withStrategy=%v output=%+v err=%v", withStrategy, output, err)
		}
	}
}

func TestAutomaticStrategyRejectsNegativeAndEmptyPairFields(t *testing.T) {
	for _, strategy := range []map[string]any{
		{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "INSTALL", "bid": map[string]any{"amount": "-0.1", "currency": "USD"}},
		{"bidStrategyType": "", "bidStrategyGoal": "", "bid": map[string]any{"amount": "1", "currency": "USD"}},
	} {
		if err := validateBidStrategy(strategy); err == nil {
			t.Fatalf("invalid strategy accepted: %v", strategy)
		}
	}
}

type optimizerReportDriftManager struct {
	*receiptScopeManager
	reportReads int
}

func (m *optimizerReportDriftManager) Do(ctx context.Context, profile, account string, operation appleads.Operation) (appleads.Result, error) {
	result, err := m.receiptScopeManager.Do(ctx, profile, account, operation)
	if operation.Path() == "reports/apps/campaigns/query" {
		m.reportReads++
		if m.reportReads > 1 {
			result.Data = []any{optimizationReportRow(m.now, "100", "campaign", "10.00", 1)}
		}
	}
	return result, err
}

func TestOptimizerRejectsReportChangeBetweenPlanningAndReceipt(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	policyPath := filepath.Join(root, "policies.json")
	policy := optimization.Policy{Name: "synthetic", Profile: "owner", AdAccountID: "123", PromotedObjectID: "456", CampaignIDs: []string{"100"}, Mode: "active", TargetInstallCPA: &appleads.Money{Amount: "10.00", Currency: "USD"}, MaxTotalDailyBudget: appleads.Money{Amount: "100.00", Currency: "USD"}, MaxCampaignDailyBudget: appleads.Money{Amount: "50.00", Currency: "USD"}, MaxBid: &appleads.Money{Amount: "5.00", Currency: "USD"}, Permissions: optimization.Permissions{Bid: true}, Preset: "balanced"}
	if err := optimization.SavePolicies(policyPath, optimization.PolicyFile{Policies: []optimization.Policy{policy}}); err != nil {
		t.Fatal(err)
	}
	manager := &optimizerReportDriftManager{receiptScopeManager: &receiptScopeManager{campaignStatus: "ENABLED", now: now}}
	service := &Service{manager: manager, allowWrites: true, policyPath: policyPath, historyRoot: filepath.Join(root, "history"), now: func() time.Time { return now }}
	_, output, err := service.optimizationPlanPreview(operations.NewStore())(t.Context(), nil, OptimizationPolicyInput{AccountInput: AccountInput{Profile: "owner", AdAccountID: "123"}, Policy: "synthetic"})
	if err != nil || output.Preview != nil || output.Error == nil || !strings.Contains(output.Error.Message, "drift") || manager.reportReads != 2 || manager.writes.Load() != 0 {
		t.Fatalf("output=%+v reads=%d writes=%d err=%v", output, manager.reportReads, manager.writes.Load(), err)
	}
}

func TestOptimizerCaptureBindsEveryReportPage(t *testing.T) {
	policy := optimization.Policy{Profile: "synthetic", AdAccountID: "123", MaxTotalDailyBudget: appleads.Money{Currency: "USD"}}
	start, end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	page := func(offset, count int) appleads.Result {
		rows := make([]any, count)
		for i := range rows {
			rows[i] = map[string]any{"metadata": map[string]any{"id": fmt.Sprint(offset + i + 1), "status": "ENABLED"}, "granularMetrics": []any{}}
		}
		return appleads.Result{Data: rows, Pagination: appleads.Pagination{Offset: offset, PageSize: MaxItems, Total: MaxItems + 1}}
	}
	first, last := page(0, MaxItems), page(MaxItems, 1)
	first.Pagination.Next = "next-page"
	changed := page(MaxItems, 1)
	changed.Data.([]any)[0].(map[string]any)["metadata"].(map[string]any)["status"] = "PAUSED"
	manager := &v036Manager{responses: map[string][]appleads.Result{"reports/apps/adgroups/query": {first, last, first, changed}}}
	capture := &optimizationEvidenceCapture{appleAdsManager: manager}
	service := &Service{manager: capture}
	items, err := service.optimizationBiddables(t.Context(), policy, "adgroups", "100", start, end)
	if err != nil || len(items) != MaxItems+1 || len(capture.reads) != 2 {
		t.Fatalf("items=%d reads=%d err=%v", len(items), len(capture.reads), err)
	}
	write, _ := appleads.ResourceUpdate("campaigns", "100", map[string]any{"status": "PAUSED"})
	_, err = operations.NewStore().PreviewComposite(t.Context(), manager, "synthetic", "123", "synthetic", []string{"100"}, map[string]any{"status": "PAUSED"}, capture.reads, write, operations.PreviewOptions{})
	if !errors.Is(err, operations.ErrStateDrift) {
		t.Fatalf("second-page drift must be bound: %v", err)
	}
}
