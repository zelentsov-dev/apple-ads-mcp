package optimization

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

type DailyMetric struct {
	Date        appleads.Date  `json:"date"`
	Spend       appleads.Money `json:"spend"`
	Taps        int64          `json:"taps"`
	Impressions int64          `json:"impressions"`
	TapInstalls int64          `json:"tapInstalls"`
}

type BiddableEvidence struct {
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	Bid          *appleads.Money `json:"bid,omitempty"`
	SearchMatch  bool            `json:"searchMatch,omitempty"`
	Daily        []DailyMetric   `json:"daily"`
}

type CampaignEvidence struct {
	CampaignID                string             `json:"campaignId"`
	Name                      string             `json:"name"`
	Status                    string             `json:"status"`
	SystemStatus              string             `json:"systemStatus,omitempty"`
	Placement                 string             `json:"placement"`
	SearchMatch               bool               `json:"searchMatch"`
	BidStrategy               string             `json:"bidStrategy"`
	DailyBudget               appleads.Money     `json:"dailyBudget"`
	Daily                     []DailyMetric      `json:"daily"`
	Biddables                 []BiddableEvidence `json:"biddables,omitempty"`
	AppleBudgetRecommendation *appleads.Money    `json:"appleBudgetRecommendation,omitempty"`
	AppleTargetCPA            *appleads.Money    `json:"appleTargetCPARecommendation,omitempty"`
	MaxConversionsEligible    bool               `json:"maxConversionsEligible"`
	ModificationTime          string             `json:"modificationTime,omitempty"`
}

type MetricSummary struct {
	Days              int            `json:"days"`
	Spend             appleads.Money `json:"spend"`
	Taps              int64          `json:"taps"`
	Impressions       int64          `json:"impressions"`
	TapInstalls       int64          `json:"tapInstalls"`
	TapInstallCPI     string         `json:"tapInstallCPI,omitempty"`
	TapInstallRate    string         `json:"tapInstallRate,omitempty"`
	TTR               string         `json:"ttr,omitempty"`
	CPT               string         `json:"cpt,omitempty"`
	BudgetUtilization string         `json:"budgetUtilization,omitempty"`
	DailyCPIP25       string         `json:"dailyCPIP25,omitempty"`
	DailyCPIP75       string         `json:"dailyCPIP75,omitempty"`
}

type CampaignBaseline struct {
	Campaign                  CampaignEvidence `json:"campaign"`
	Last28Days                MetricSummary    `json:"last28Days"`
	Last7Days                 MetricSummary    `json:"last7Days"`
	Previous7Days             MetricSummary    `json:"previous7Days"`
	MinimumDataSatisfied      bool             `json:"minimumDataSatisfied"`
	CooldownActive            bool             `json:"cooldownActive"`
	CooldownUntil             string           `json:"cooldownUntil,omitempty"`
	OptimizerPaused           bool             `json:"optimizerPaused"`
	AppleRecommendationsShown bool             `json:"appleRecommendationsShown"`
}

type Baseline struct {
	Policy                 Policy             `json:"policy"`
	GeneratedAt            string             `json:"generatedAt"`
	ReconciliationRequired bool               `json:"reconciliationRequired"`
	Campaigns              []CampaignBaseline `json:"campaigns"`
}

type PlanAction struct {
	CorrelationID string         `json:"correlationId"`
	Order         int            `json:"order"`
	CampaignID    string         `json:"campaignId"`
	ResourceType  string         `json:"resourceType"`
	ResourceID    string         `json:"resourceId"`
	Action        string         `json:"action"`
	Before        map[string]any `json:"before"`
	After         map[string]any `json:"after"`
	Reason        string         `json:"reason"`
	DependsOn     []string       `json:"dependsOn,omitempty"`
}

type Plan struct {
	Policy       string       `json:"policy"`
	Mode         string       `json:"mode"`
	GeneratedAt  string       `json:"generatedAt"`
	Baseline     Baseline     `json:"baseline"`
	Actions      []PlanAction `json:"actions"`
	ApplyAllowed bool         `json:"applyAllowed"`
	Warnings     []string     `json:"warnings,omitempty"`
}

func BuildBaseline(policy Policy, evidence []CampaignEvidence, history History, now time.Time) (Baseline, error) {
	if err := policy.Validate(); err != nil {
		return Baseline{}, err
	}
	policy.Thresholds = resolvedThresholds(policy.Thresholds)
	if len(evidence) > maxCampaigns {
		return Baseline{}, fmt.Errorf("baseline supports at most %d campaigns", maxCampaigns)
	}
	result := Baseline{Policy: policy, GeneratedAt: now.UTC().Format(time.RFC3339), ReconciliationRequired: HistoryRequiresReconciliation(history)}
	for _, campaign := range evidence {
		if err := validateCampaignEvidence(campaign, policy.MaxTotalDailyBudget.Currency); err != nil {
			return Baseline{}, err
		}
		metrics, err := completedWindow(campaign.Daily, now, 28)
		if err != nil {
			return Baseline{}, fmt.Errorf("campaign %s completed-day evidence: %w", campaign.CampaignID, err)
		}
		last28 := metrics
		for _, biddable := range campaign.Biddables {
			if policy.Permissions.Bid && biddable.Bid != nil && ratOrZero(biddable.Bid.Amount).Cmp(ratOrZero(policy.MaxBid.Amount)) > 0 {
				return Baseline{}, fmt.Errorf("campaign %s biddable %s current bid exceeds maxBid", campaign.CampaignID, biddable.ResourceID)
			}
			if _, err := completedWindow(biddable.Daily, now, 28); err != nil {
				return Baseline{}, fmt.Errorf("campaign %s biddable %s completed-day evidence: %w", campaign.CampaignID, biddable.ResourceID, err)
			}
		}
		last14 := tail(metrics, 14)
		last7 := tail(last14, 7)
		previous7 := head(last14, maxInt(0, len(last14)-7))
		last28Summary, err := summarize(last28, campaign.DailyBudget)
		if err != nil {
			return Baseline{}, fmt.Errorf("campaign %s last 28 days: %w", campaign.CampaignID, err)
		}
		last7Summary, err := summarize(last7, campaign.DailyBudget)
		if err != nil {
			return Baseline{}, fmt.Errorf("campaign %s last 7 days: %w", campaign.CampaignID, err)
		}
		previous7Summary, err := summarize(previous7, campaign.DailyBudget)
		if err != nil {
			return Baseline{}, fmt.Errorf("campaign %s previous 7 days: %w", campaign.CampaignID, err)
		}
		lastChange, optimizerPaused := historyState(history, campaign.CampaignID, campaign.ModificationTime)
		cooldownUntil := lastChange.Add(time.Duration(policy.Thresholds.CooldownHours) * time.Hour)
		baseline := CampaignBaseline{
			Campaign:                  campaign,
			Last28Days:                last28Summary,
			Last7Days:                 last7Summary,
			Previous7Days:             previous7Summary,
			MinimumDataSatisfied:      len(metrics) >= policy.Thresholds.MinimumCompletedDays,
			CooldownActive:            !lastChange.IsZero() && now.Before(cooldownUntil),
			OptimizerPaused:           optimizerPaused,
			AppleRecommendationsShown: campaign.AppleBudgetRecommendation != nil || campaign.AppleTargetCPA != nil,
		}
		if baseline.CooldownActive {
			baseline.CooldownUntil = cooldownUntil.UTC().Format(time.RFC3339)
		}
		result.Campaigns = append(result.Campaigns, baseline)
	}
	return result, nil
}

func validateCampaignEvidence(campaign CampaignEvidence, currency string) error {
	if strings.TrimSpace(campaign.CampaignID) == "" {
		return errors.New("campaign evidence is missing campaignId")
	}
	if err := campaign.DailyBudget.ValidatePositive(); err != nil {
		return fmt.Errorf("campaign %s daily budget: %w", campaign.CampaignID, err)
	}
	if campaign.DailyBudget.Currency != currency {
		return fmt.Errorf("campaign %s currency does not match policy", campaign.CampaignID)
	}
	validateMetrics := func(label string, metrics []DailyMetric) error {
		for index, metric := range metrics {
			if _, err := time.Parse("2006-01-02", string(metric.Date)); err != nil {
				return fmt.Errorf("campaign %s %s metric %d has an invalid date", campaign.CampaignID, label, index+1)
			}
			if err := metric.Spend.Validate(); err != nil || ratOrZero(metric.Spend.Amount).Sign() < 0 {
				if err == nil {
					err = errors.New("money amount must not be negative")
				}
				return fmt.Errorf("campaign %s %s metric %d spend: %w", campaign.CampaignID, label, index+1, err)
			}
			if metric.Spend.Currency != currency {
				return fmt.Errorf("campaign %s %s metric %d currency does not match policy", campaign.CampaignID, label, index+1)
			}
			if metric.Taps < 0 || metric.Impressions < 0 || metric.TapInstalls < 0 {
				return fmt.Errorf("campaign %s %s metric %d contains a negative count", campaign.CampaignID, label, index+1)
			}
		}
		return nil
	}
	if err := validateMetrics("campaign", campaign.Daily); err != nil {
		return err
	}
	for index, biddable := range campaign.Biddables {
		if strings.TrimSpace(biddable.ResourceID) == "" {
			return fmt.Errorf("campaign %s biddable %d is missing resourceId", campaign.CampaignID, index+1)
		}
		if biddable.Bid != nil {
			if err := biddable.Bid.ValidatePositive(); err != nil {
				return fmt.Errorf("campaign %s biddable %s bid: %w", campaign.CampaignID, biddable.ResourceID, err)
			}
			if biddable.Bid.Currency != currency {
				return fmt.Errorf("campaign %s biddable %s bid currency does not match policy", campaign.CampaignID, biddable.ResourceID)
			}
		}
		if err := validateMetrics("biddable "+biddable.ResourceID, biddable.Daily); err != nil {
			return err
		}
	}
	for label, recommendation := range map[string]*appleads.Money{
		"daily budget recommendation": campaign.AppleBudgetRecommendation,
		"target CPA recommendation":   campaign.AppleTargetCPA,
	} {
		if recommendation == nil {
			continue
		}
		if err := recommendation.ValidatePositive(); err != nil {
			return fmt.Errorf("campaign %s %s: %w", campaign.CampaignID, label, err)
		}
		if recommendation.Currency != currency {
			return fmt.Errorf("campaign %s %s currency does not match policy", campaign.CampaignID, label)
		}
	}
	return nil
}

func BuildPlan(policy Policy, evidence []CampaignEvidence, history History, now time.Time) (Plan, error) {
	baseline, err := BuildBaseline(policy, evidence, history, now)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Policy: policy.Name, Mode: policy.Mode, GeneratedAt: now.UTC().Format(time.RFC3339), Baseline: baseline}
	if baseline.ReconciliationRequired {
		plan.Warnings = []string{"A previous optimization write has an unresolved outcome. Run operations_verify for that receipt before creating another plan."}
		return plan, nil
	}
	if policy.Mode == "learning" {
		plan.Warnings = []string{"Learning mode returns evidence only. Set an explicit targetInstallCPA and switch the policy to active before previewing changes."}
		return plan, nil
	}
	target, err := positiveRat(policy.TargetInstallCPA.Amount)
	if err != nil {
		return Plan{}, err
	}
	thresholds := baseline.Policy.Thresholds
	for _, item := range baseline.Campaigns {
		systemBlocked := item.Campaign.Status == "ENABLED" && item.Campaign.SystemStatus != "RUNNING"
		if !item.MinimumDataSatisfied || item.CooldownActive || systemBlocked {
			continue
		}
		campaignActions, err := campaignPlan(item, baseline.Policy, thresholds, target, now)
		if err != nil {
			return Plan{}, fmt.Errorf("campaign %s plan: %w", item.Campaign.CampaignID, err)
		}
		plan.Actions = append(plan.Actions, campaignActions...)
		if len(plan.Actions) > 100 {
			return Plan{}, errors.New("optimization plan exceeds 100 actions")
		}
	}
	sort.SliceStable(plan.Actions, func(i, j int) bool {
		if plan.Actions[i].Order == plan.Actions[j].Order {
			return plan.Actions[i].CorrelationID < plan.Actions[j].CorrelationID
		}
		return plan.Actions[i].Order < plan.Actions[j].Order
	})
	if err := enforcePlanBudgetCaps(&plan, baseline.Policy); err != nil {
		return Plan{}, err
	}
	plan.ApplyAllowed = len(plan.Actions) > 0
	return plan, nil
}

func campaignPlan(item CampaignBaseline, policy Policy, thresholds Thresholds, target *big.Rat, now time.Time) ([]PlanAction, error) {
	campaign := item.Campaign
	last14, err := combine(item.Last7Days, item.Previous7Days)
	if err != nil {
		return nil, err
	}
	actions := make([]PlanAction, 0, 8)
	add := func(order int, resourceType, resourceID, action, reason string, before, after map[string]any) string {
		id := fmt.Sprintf("%s-%03d", campaign.CampaignID, len(actions)+1)
		actions = append(actions, PlanAction{CorrelationID: id, Order: order, CampaignID: campaign.CampaignID, ResourceType: resourceType, ResourceID: resourceID, Action: action, Before: before, After: after, Reason: reason})
		return id
	}
	if policy.Permissions.Pause && shouldPause(item, last14, target, thresholds) && campaign.Status != "PAUSED" {
		add(10, "campaign", campaign.CampaignID, "pause", "CPI/spend pause guardrail was met in both completed seven-day windows", map[string]any{"status": campaign.Status}, map[string]any{"status": "PAUSED"})
		return actions, nil
	}
	if policy.Permissions.Resume && policy.Permissions.Retest && item.OptimizerPaused && campaign.Status == "PAUSED" {
		cap := minimumMoney(campaign.DailyBudget, appleads.Money{Amount: thresholds.RetestDailyBudgetCap, Currency: campaign.DailyBudget.Currency})
		id := add(40, "campaign", campaign.CampaignID, "budget", "Optimizer-owned pause is eligible for a bounded retest", map[string]any{"dailyBudget": campaign.DailyBudget}, map[string]any{"dailyBudget": cap})
		actions = append(actions, PlanAction{CorrelationID: fmt.Sprintf("%s-%03d", campaign.CampaignID, len(actions)+1), Order: 50, CampaignID: campaign.CampaignID, ResourceType: "campaign", ResourceID: campaign.CampaignID, Action: "resume", Before: map[string]any{"status": "PAUSED"}, After: map[string]any{"status": "ENABLED"}, Reason: "Retest was explicitly allowed for a campaign paused by this optimizer", DependsOn: []string{id}})
		return actions, nil
	}
	if campaign.Status != "ENABLED" {
		return actions, nil
	}
	if policy.Permissions.Budget {
		if shouldIncrease(item, last14, target, thresholds, campaign.AppleBudgetRecommendation != nil) {
			after := steppedMoney(campaign.DailyBudget, thresholds.ChangeStepPercent, true, policy.MaxCampaignDailyBudget)
			if after.Amount != campaign.DailyBudget.Amount {
				add(50, "campaign", campaign.CampaignID, "budget_increase", "Efficient campaign is budget-constrained or has an Apple recommendation", map[string]any{"dailyBudget": campaign.DailyBudget}, map[string]any{"dailyBudget": after})
			}
		} else if shouldDecrease(item, target, thresholds) {
			after := steppedMoney(campaign.DailyBudget, thresholds.ChangeStepPercent, false, policy.MaxCampaignDailyBudget)
			add(20, "campaign", campaign.CampaignID, "budget_decrease", "CPI exceeded the target in both completed seven-day windows", map[string]any{"dailyBudget": campaign.DailyBudget}, map[string]any{"dailyBudget": after})
		}
	}
	if policy.Permissions.Strategy && campaign.BidStrategy == "MANUAL_CPT" && campaign.Placement == "APPSTORE_SEARCH_RESULTS" && campaign.SearchMatch && campaign.MaxConversionsEligible && last14.Days >= thresholds.MaxConversionsMinimumDays {
		average := new(big.Rat).Quo(new(big.Rat).SetInt64(last14.TapInstalls), big.NewRat(int64(last14.Days), 1))
		minimum, _ := positiveRat(thresholds.MaxConversionsDailyAvg)
		if average.Cmp(minimum) >= 0 {
			add(30, "campaign", campaign.CampaignID, "bid_strategy", "Search Match campaign meets Apple eligibility and completed-data thresholds", map[string]any{"bidStrategy": campaign.BidStrategy}, map[string]any{"bidStrategy": "MAX_CONVERSIONS"})
		}
	}
	if policy.Permissions.Bid {
		for _, biddable := range campaign.Biddables {
			if biddable.Bid == nil || biddable.Status != "ENABLED" {
				continue
			}
			complete, _ := completedWindow(biddable.Daily, now, 28)
			metrics := tail(complete, 14)
			if len(metrics) < thresholds.MinimumCompletedDays {
				continue
			}
			summary, err := summarize(metrics, campaign.DailyBudget)
			if err != nil {
				return nil, fmt.Errorf("biddable %s summary: %w", biddable.ResourceID, err)
			}
			cpi := last14Rat(summary)
			increaseRatio, _ := positiveRat(thresholds.IncreaseMaximumCPARatio)
			increaseThreshold := new(big.Rat).Mul(target, increaseRatio)
			last7, err := summarize(tail(metrics, 7), campaign.DailyBudget)
			if err != nil {
				return nil, fmt.Errorf("biddable %s last 7 days: %w", biddable.ResourceID, err)
			}
			previous7, err := summarize(head(metrics, len(metrics)-7), campaign.DailyBudget)
			if err != nil {
				return nil, fmt.Errorf("biddable %s previous 7 days: %w", biddable.ResourceID, err)
			}
			decreaseRatio, _ := positiveRat(thresholds.DecreaseMinimumCPARatio)
			decreaseThreshold := new(big.Rat).Mul(target, decreaseRatio)
			if summary.TapInstalls >= int64(thresholds.IncreaseMinimumInstalls) && cpi != nil && cpi.Cmp(increaseThreshold) <= 0 {
				after := steppedMoney(*biddable.Bid, thresholds.ChangeStepPercent, true, *policy.MaxBid)
				add(30, biddable.ResourceType, biddable.ResourceID, "bid_increase", "Biddable object has sufficient installs below target CPI", map[string]any{"bid": *biddable.Bid}, map[string]any{"bid": after})
			} else if last7.TapInstalls >= int64(thresholds.DecreaseMinimumInstalls) && previous7.TapInstalls >= int64(thresholds.DecreaseMinimumInstalls) &&
				last14Rat(last7) != nil && last14Rat(previous7) != nil && last14Rat(last7).Cmp(decreaseThreshold) >= 0 && last14Rat(previous7).Cmp(decreaseThreshold) >= 0 {
				after := steppedMoney(*biddable.Bid, thresholds.ChangeStepPercent, false, *policy.MaxBid)
				add(20, biddable.ResourceType, biddable.ResourceID, "bid_decrease", "Biddable object has sufficient installs above target CPI", map[string]any{"bid": *biddable.Bid}, map[string]any{"bid": after})
			}
		}
	}
	return actions, nil
}

func shouldIncrease(item CampaignBaseline, combined MetricSummary, target *big.Rat, thresholds Thresholds, hasAppleRecommendation bool) bool {
	cpi := last14Rat(combined)
	maximum, _ := positiveRat(thresholds.IncreaseMaximumCPARatio)
	utilization, _ := positiveRat(thresholds.IncreaseBudgetUtilization)
	actualUtilization := ratOrZero(combined.BudgetUtilization)
	return combined.TapInstalls >= int64(thresholds.IncreaseMinimumInstalls) && cpi != nil && cpi.Cmp(new(big.Rat).Mul(target, maximum)) <= 0 && (actualUtilization.Cmp(utilization) >= 0 || hasAppleRecommendation)
}

func shouldDecrease(item CampaignBaseline, target *big.Rat, thresholds Thresholds) bool {
	minimum, _ := positiveRat(thresholds.DecreaseMinimumCPARatio)
	threshold := new(big.Rat).Mul(target, minimum)
	return item.Last7Days.TapInstalls >= int64(thresholds.DecreaseMinimumInstalls) && item.Previous7Days.TapInstalls >= int64(thresholds.DecreaseMinimumInstalls) &&
		last14Rat(item.Last7Days) != nil && last14Rat(item.Previous7Days) != nil && last14Rat(item.Last7Days).Cmp(threshold) >= 0 && last14Rat(item.Previous7Days).Cmp(threshold) >= 0
}

func shouldPause(item CampaignBaseline, combined MetricSummary, target *big.Rat, thresholds Thresholds) bool {
	spendMultiple, _ := positiveRat(thresholds.PauseSpendMultiple)
	if combined.TapInstalls == 0 && ratOrZero(combined.Spend.Amount).Cmp(new(big.Rat).Mul(target, spendMultiple)) >= 0 {
		return true
	}
	minimumRatio, _ := positiveRat(thresholds.PauseMinimumCPARatio)
	threshold := new(big.Rat).Mul(target, minimumRatio)
	return item.Last7Days.TapInstalls >= int64(thresholds.PauseMinimumInstalls) && item.Previous7Days.TapInstalls >= int64(thresholds.PauseMinimumInstalls) &&
		last14Rat(item.Last7Days) != nil && last14Rat(item.Previous7Days) != nil && last14Rat(item.Last7Days).Cmp(threshold) >= 0 && last14Rat(item.Previous7Days).Cmp(threshold) >= 0
}

func summarize(metrics []DailyMetric, budget appleads.Money) (MetricSummary, error) {
	spend := new(big.Rat)
	taps := int64(0)
	impressions := int64(0)
	installs := int64(0)
	dailyCPIs := make([]*big.Rat, 0, len(metrics))
	for _, metric := range metrics {
		amount := ratOrZero(metric.Spend.Amount)
		spend.Add(spend, amount)
		var err error
		taps, err = addCount(taps, metric.Taps, "taps")
		if err != nil {
			return MetricSummary{}, err
		}
		impressions, err = addCount(impressions, metric.Impressions, "impressions")
		if err != nil {
			return MetricSummary{}, err
		}
		installs, err = addCount(installs, metric.TapInstalls, "tapInstalls")
		if err != nil {
			return MetricSummary{}, err
		}
		if metric.TapInstalls > 0 {
			dailyCPIs = append(dailyCPIs, new(big.Rat).Quo(amount, big.NewRat(metric.TapInstalls, 1)))
		}
	}
	result := MetricSummary{Days: len(metrics), Spend: appleads.Money{Amount: decimal(spend), Currency: budget.Currency}, Taps: taps, Impressions: impressions, TapInstalls: installs}
	if installs > 0 {
		result.TapInstallCPI = decimal(new(big.Rat).Quo(spend, big.NewRat(installs, 1)))
	}
	if taps > 0 {
		result.TapInstallRate = decimal(new(big.Rat).Quo(big.NewRat(installs, 1), big.NewRat(taps, 1)))
		result.CPT = decimal(new(big.Rat).Quo(spend, big.NewRat(taps, 1)))
	}
	if impressions > 0 {
		result.TTR = decimal(new(big.Rat).Quo(big.NewRat(taps, 1), big.NewRat(impressions, 1)))
	}
	if len(metrics) > 0 {
		dailyBudget := ratOrZero(budget.Amount)
		if dailyBudget.Sign() > 0 {
			result.BudgetUtilization = decimal(new(big.Rat).Quo(spend, new(big.Rat).Mul(dailyBudget, big.NewRat(int64(len(metrics)), 1))))
		}
	}
	if len(dailyCPIs) > 0 {
		sort.Slice(dailyCPIs, func(i, j int) bool { return dailyCPIs[i].Cmp(dailyCPIs[j]) < 0 })
		result.DailyCPIP25 = decimal(percentile(dailyCPIs, 0.25))
		result.DailyCPIP75 = decimal(percentile(dailyCPIs, 0.75))
	}
	return result, nil
}

func completedWindow(metrics []DailyMetric, now time.Time, days int) ([]DailyMetric, error) {
	if days <= 0 {
		return nil, errors.New("completed-day window must be positive")
	}
	byDate := make(map[string]DailyMetric, len(metrics))
	for _, metric := range metrics {
		date := string(metric.Date)
		if _, exists := byDate[date]; exists {
			return nil, fmt.Errorf("duplicate date %s", date)
		}
		byDate[date] = metric
	}
	end := startOfUTCDay(now).AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -(days - 1))
	result := make([]DailyMetric, 0, days)
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		metric, exists := byDate[key]
		if !exists {
			return nil, fmt.Errorf("missing completed date %s", key)
		}
		result = append(result, metric)
	}
	if len(byDate) != days {
		return nil, fmt.Errorf("expected exactly %d completed dates ending %s; received %d", days, end.Format("2006-01-02"), len(byDate))
	}
	return result, nil
}

func startOfUTCDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func historyState(history History, campaignID, currentModificationTime string) (time.Time, bool) {
	var latest time.Time
	var pauseReceipt string
	var pauseModificationTime string
	for _, applied := range effectiveHistoryActions(history) {
		action := applied.Action
		if action.CampaignID != campaignID {
			continue
		}
		occurred, err := time.Parse(time.RFC3339, action.OccurredAt)
		if err != nil || occurred.Before(latest) {
			continue
		}
		latest = occurred
		if action.ResourceType == "campaign" && action.Action == "pause" {
			pauseReceipt = applied.ReceiptHash
			pauseModificationTime = findHistoryString(applied.VerifiedAfter, "modificationTime")
		}
		if action.ResourceType == "campaign" && action.Action == "resume" {
			pauseReceipt = ""
			pauseModificationTime = ""
		}
	}
	if pauseReceipt == "" || currentModificationTime == "" {
		return latest, false
	}
	return latest, pauseModificationTime != "" && pauseModificationTime == currentModificationTime
}

type effectiveHistoryAction struct {
	Action        HistoryAction
	ReceiptHash   string
	VerifiedAfter map[string]any
}

func effectiveHistoryActions(history History) []effectiveHistoryAction {
	type state struct {
		effectiveHistoryAction
		known   bool
		applied bool
	}
	states := map[string]*state{}
	order := make([]string, 0)
	for entryIndex, entry := range history.Entries {
		for actionIndex, action := range entry.Actions {
			if entry.Status == "previewed" || action.Status == "previewed" {
				continue
			}
			key := historyActionKey(entry.ReceiptHash, action, entryIndex, actionIndex)
			current, exists := states[key]
			if !exists {
				current = &state{effectiveHistoryAction: effectiveHistoryAction{Action: action, ReceiptHash: entry.ReceiptHash}}
				states[key] = current
				order = append(order, key)
			}
			current.Action = mergeHistoryAction(current.Action, action)
			switch action.Status {
			case "applied":
				current.known, current.applied = true, true
			case "failed", "skipped", "not_attempted":
				current.known, current.applied = true, false
			case "unknown", "pending":
				current.known = false
			}
		}
		if entry.Status != "verification_verified" {
			continue
		}
		for actionIndex, action := range entry.Verification {
			key := historyActionKey(entry.ReceiptHash, action, entryIndex, actionIndex)
			current, exists := states[key]
			if !exists {
				continue
			}
			switch action.Status {
			case "matched", "matched_after":
				current.known, current.applied = true, true
				current.VerifiedAfter = action.After
			case "matched_before":
				current.known, current.applied = true, false
				current.VerifiedAfter = nil
			}
		}
	}
	result := make([]effectiveHistoryAction, 0, len(states))
	for _, key := range order {
		current := states[key]
		if current.known && current.applied {
			result = append(result, current.effectiveHistoryAction)
		}
	}
	return result
}

func historyActionKey(receiptHash string, action HistoryAction, entryIndex, actionIndex int) string {
	if receiptHash == "" {
		return fmt.Sprintf("legacy:%d:%d", entryIndex, actionIndex)
	}
	if action.CorrelationID != "" {
		return receiptHash + ":" + action.CorrelationID
	}
	return receiptHash + ":" + action.CampaignID + ":" + action.ResourceType + ":" + action.ResourceID + ":" + action.Action
}

func mergeHistoryAction(existing, incoming HistoryAction) HistoryAction {
	result := incoming
	if result.CorrelationID == "" {
		result.CorrelationID = existing.CorrelationID
	}
	if result.CampaignID == "" {
		result.CampaignID = existing.CampaignID
	}
	if result.ResourceType == "" {
		result.ResourceType = existing.ResourceType
	}
	if result.Resource == "" {
		result.Resource = existing.Resource
	}
	if result.ResourceID == "" {
		result.ResourceID = existing.ResourceID
	}
	if result.Action == "" {
		result.Action = existing.Action
	}
	if result.Reason == "" {
		result.Reason = existing.Reason
	}
	if result.Before == nil {
		result.Before = existing.Before
	}
	if result.After == nil {
		result.After = existing.After
	}
	if result.OccurredAt == "" || result.Status == "pending" && existing.OccurredAt != "" {
		result.OccurredAt = existing.OccurredAt
	}
	return result
}

func findHistoryString(value map[string]any, field string) string {
	if current, ok := value[field]; ok {
		return fmt.Sprint(current)
	}
	for _, current := range value {
		if nested, ok := current.(map[string]any); ok {
			if found := findHistoryString(nested, field); found != "" {
				return found
			}
		}
	}
	return ""
}

func enforcePlanBudgetCaps(plan *Plan, policy Policy) error {
	total := new(big.Rat)
	for _, campaign := range plan.Baseline.Campaigns {
		budget := ratOrZero(campaign.Campaign.DailyBudget.Amount)
		for _, action := range plan.Actions {
			if action.CampaignID == campaign.Campaign.CampaignID && strings.HasPrefix(action.Action, "budget") {
				if next, ok := moneyFromAny(action.After["dailyBudget"]); ok {
					budget = ratOrZero(next.Amount)
				}
			}
		}
		if budget.Cmp(ratOrZero(policy.MaxCampaignDailyBudget.Amount)) > 0 {
			return fmt.Errorf("campaign %s would exceed maxCampaignDailyBudget", campaign.Campaign.CampaignID)
		}
		total.Add(total, budget)
	}
	if total.Cmp(ratOrZero(policy.MaxTotalDailyBudget.Amount)) > 0 {
		return errors.New("optimization plan would exceed maxTotalDailyBudget")
	}
	return nil
}

func combine(left, right MetricSummary) (MetricSummary, error) {
	spend := new(big.Rat).Add(ratOrZero(left.Spend.Amount), ratOrZero(right.Spend.Amount))
	taps, err := addCount(left.Taps, right.Taps, "taps")
	if err != nil {
		return MetricSummary{}, err
	}
	impressions, err := addCount(left.Impressions, right.Impressions, "impressions")
	if err != nil {
		return MetricSummary{}, err
	}
	installs, err := addCount(left.TapInstalls, right.TapInstalls, "tapInstalls")
	if err != nil {
		return MetricSummary{}, err
	}
	result := MetricSummary{Days: left.Days + right.Days, Spend: appleads.Money{Amount: decimal(spend), Currency: left.Spend.Currency}, Taps: taps, Impressions: impressions, TapInstalls: installs}
	if result.TapInstalls > 0 {
		result.TapInstallCPI = decimal(new(big.Rat).Quo(spend, big.NewRat(result.TapInstalls, 1)))
	}
	weightedDays := int64(result.Days)
	if weightedDays > 0 {
		leftSpend := ratOrZero(left.BudgetUtilization)
		rightSpend := ratOrZero(right.BudgetUtilization)
		result.BudgetUtilization = decimal(new(big.Rat).Quo(new(big.Rat).Add(new(big.Rat).Mul(leftSpend, big.NewRat(int64(left.Days), 1)), new(big.Rat).Mul(rightSpend, big.NewRat(int64(right.Days), 1))), big.NewRat(weightedDays, 1)))
	}
	return result, nil
}

func addCount(current, next int64, field string) (int64, error) {
	if current < 0 || next < 0 {
		return 0, fmt.Errorf("%s total contains a negative value", field)
	}
	if current > (1<<63-1)-next {
		return 0, fmt.Errorf("%s total exceeds 64-bit integer range", field)
	}
	return current + next, nil
}

func steppedMoney(current appleads.Money, percent string, increase bool, cap appleads.Money) appleads.Money {
	value := ratOrZero(current.Amount)
	step, _ := positiveRat(percent)
	factor := new(big.Rat).Quo(step, big.NewRat(100, 1))
	change := new(big.Rat).Mul(value, factor)
	if increase {
		value.Add(value, change)
		if value.Cmp(ratOrZero(cap.Amount)) > 0 {
			value = ratOrZero(cap.Amount)
		}
	} else {
		value.Sub(value, change)
	}
	return appleads.Money{Amount: decimalFloorCents(value), Currency: current.Currency}
}

func minimumMoney(left, right appleads.Money) appleads.Money {
	if ratOrZero(left.Amount).Cmp(ratOrZero(right.Amount)) <= 0 {
		return left
	}
	return right
}

func percentile(values []*big.Rat, percentile float64) *big.Rat {
	if len(values) == 1 {
		return values[0]
	}
	index := int(float64(len(values)-1) * percentile)
	return values[index]
}

func last14Rat(summary MetricSummary) *big.Rat {
	if summary.TapInstallCPI == "" {
		return nil
	}
	return ratOrZero(summary.TapInstallCPI)
}

func ratOrZero(value string) *big.Rat {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return new(big.Rat)
	}
	return parsed
}

func decimal(value *big.Rat) string {
	return strings.TrimRight(strings.TrimRight(value.FloatString(6), "0"), ".")
}

func decimalFloorCents(value *big.Rat) string {
	scaled := new(big.Rat).Mul(value, big.NewRat(100, 1))
	quotient := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	return new(big.Rat).Quo(new(big.Rat).SetInt(quotient), big.NewRat(100, 1)).FloatString(2)
}

func tail[T any](values []T, size int) []T {
	if len(values) <= size {
		return append([]T(nil), values...)
	}
	return append([]T(nil), values[len(values)-size:]...)
}

func head[T any](values []T, size int) []T {
	if size > len(values) {
		size = len(values)
	}
	return append([]T(nil), values[:size]...)
}

func moneyFromAny(value any) (appleads.Money, bool) {
	switch typed := value.(type) {
	case appleads.Money:
		return typed, true
	case map[string]any:
		return appleads.Money{Amount: fmt.Sprint(typed["amount"]), Currency: fmt.Sprint(typed["currency"])}, true
	default:
		return appleads.Money{}, false
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func ParseCount(value any, field string) (int64, error) {
	if value == nil {
		return 0, fmt.Errorf("%s is missing", field)
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return 0, fmt.Errorf("%s is missing", field)
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative 64-bit integer", field)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must be non-negative", field)
	}
	return parsed, nil
}
