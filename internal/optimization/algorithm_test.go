package optimization

import (
	"fmt"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

func TestBalancedPlanThresholds(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		spend        string
		installs     int64
		budget       string
		appleRec     bool
		expectAction string
	}{
		{"increase", "18.00", 2, "20.00", false, "budget_increase"},
		{"increase with recommendation", "9.00", 2, "20.00", true, "budget_increase"},
		{"decrease", "26.00", 2, "50.00", false, "budget_decrease"},
		{"pause no installs", "3.00", 0, "50.00", false, "pause"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := validPolicy()
			policy.Permissions = Permissions{Budget: true, Pause: true}
			evidence := campaignEvidence(now, test.spend, test.installs, test.budget)
			if test.appleRec {
				evidence.AppleBudgetRecommendation = &appleads.Money{Amount: "25.00", Currency: "USD"}
			}
			plan, err := BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Actions) == 0 || plan.Actions[0].Action != test.expectAction {
				t.Fatalf("actions=%+v", plan.Actions)
			}
		})
	}
}

func TestLearningCooldownAndStrategy(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	evidence := campaignEvidence(now, "30.00", 5, "50.00")
	evidence.SearchMatch = true
	evidence.MaxConversionsEligible = true
	policy := validPolicy()
	policy.Mode = "learning"
	policy.TargetInstallCPA = nil
	plan, err := BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now)
	if err != nil || plan.ApplyAllowed || len(plan.Actions) != 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}

	policy = validPolicy()
	policy.Permissions = Permissions{Strategy: true}
	plan, err = BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now)
	if err != nil || len(plan.Actions) != 1 || plan.Actions[0].Action != "bid_strategy" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}

	history := History{Entries: []HistoryEntry{{Actions: []HistoryAction{{CampaignID: "789", ResourceType: "campaign", Action: "budget_increase", Status: "applied", OccurredAt: now.Add(-time.Hour).Format(time.RFC3339)}}}}}
	plan, err = BuildPlan(validPolicy(), []CampaignEvidence{campaignEvidence(now, "18.00", 2, "20.00")}, history, now)
	if err != nil || len(plan.Actions) != 0 || !plan.Baseline.Campaigns[0].CooldownActive {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestBaselinePercentilesAndCompletedDays(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	evidence := campaignEvidence(now, "10.00", 1, "20.00")
	evidence.Daily = append(evidence.Daily, DailyMetric{Date: appleads.Date(now.Format("2006-01-02")), Spend: appleads.Money{Amount: "999", Currency: "USD"}, TapInstalls: 1})
	baseline, err := BuildBaseline(validPolicy(), []CampaignEvidence{evidence}, History{}, now)
	if err != nil {
		t.Fatal(err)
	}
	metrics := baseline.Campaigns[0].Last28Days
	if metrics.Days != 28 || metrics.DailyCPIP25 == "" || metrics.DailyCPIP75 == "" || metrics.Spend.Amount == "999" {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestBudgetCapsRejectUnsafePlan(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	policy := validPolicy()
	policy.MaxTotalDailyBudget.Amount = "10.00"
	policy.MaxCampaignDailyBudget.Amount = "10.00"
	evidence := campaignEvidence(now, "0", 0, "20.00")
	if _, err := BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now); err == nil {
		t.Fatal("expected budget cap rejection")
	}
}

func TestEnabledCampaignRequiresRunningSystemStatus(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	evidence := campaignEvidence(now, "18.00", 2, "20.00")
	evidence.SystemStatus = ""
	plan, err := BuildPlan(validPolicy(), []CampaignEvidence{evidence}, History{}, now)
	if err != nil || len(plan.Actions) != 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestBaselineRejectsMalformedAppleEvidence(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*CampaignEvidence)
	}{
		{"missing budget", func(value *CampaignEvidence) { value.DailyBudget.Amount = "" }},
		{"negative spend", func(value *CampaignEvidence) { value.Daily[0].Spend.Amount = "-1.00" }},
		{"invalid date", func(value *CampaignEvidence) { value.Daily[0].Date = "2026-99-99" }},
		{"negative count", func(value *CampaignEvidence) { value.Daily[0].TapInstalls = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := campaignEvidence(now, "1.00", 1, "20.00")
			test.mutate(&evidence)
			if _, err := BuildBaseline(validPolicy(), []CampaignEvidence{evidence}, History{}, now); err == nil {
				t.Fatal("expected malformed evidence rejection")
			}
		})
	}
}

func TestBiddableActionsUseBalancedCPIGuardrails(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	policy := validPolicy()
	policy.Permissions = Permissions{Bid: true}
	evidence := campaignEvidence(now, "0", 0, "50.00")
	evidence.Biddables = []BiddableEvidence{{
		ResourceType: "keyword", ResourceID: "100", Status: "ENABLED",
		Bid:   &appleads.Money{Amount: "1.00", Currency: "USD"},
		Daily: campaignEvidence(now, "1.00", 2, "50.00").Daily,
	}}
	plan, err := BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now)
	if err != nil || len(plan.Actions) != 1 || plan.Actions[0].Action != "bid_increase" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	evidence.Biddables[0].Daily = campaignEvidence(now, "20.00", 2, "50.00").Daily
	plan, err = BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now)
	if err != nil || len(plan.Actions) != 0 {
		t.Fatalf("target-level CPI must not trigger an increase: plan=%+v err=%v", plan, err)
	}
	evidence.Biddables[0].Daily = campaignEvidence(now, "26.00", 2, "50.00").Daily
	plan, err = BuildPlan(policy, []CampaignEvidence{evidence}, History{}, now)
	if err != nil || len(plan.Actions) != 1 || plan.Actions[0].Action != "bid_decrease" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func campaignEvidence(now time.Time, spend string, installs int64, budget string) CampaignEvidence {
	daily := make([]DailyMetric, 0, 28)
	for day := 28; day >= 1; day-- {
		daily = append(daily, DailyMetric{
			Date:  appleads.Date(now.AddDate(0, 0, -day).Format("2006-01-02")),
			Spend: appleads.Money{Amount: spend, Currency: "USD"}, Taps: max64(installs*3, 1), Impressions: 100, TapInstalls: installs,
		})
	}
	return CampaignEvidence{CampaignID: "789", Name: fmt.Sprintf("fixture-%s", spend), Status: "ENABLED", SystemStatus: "RUNNING", Placement: "APPSTORE_SEARCH_RESULTS", BidStrategy: "MANUAL_CPT", DailyBudget: appleads.Money{Amount: budget, Currency: "USD"}, Daily: daily}
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
