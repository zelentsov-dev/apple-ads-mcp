package optimization

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

const (
	maxPolicyBody = 1 << 20
	maxCampaigns  = 20
)

type PolicyFile struct {
	Policies []Policy `json:"policies"`
}

type Policy struct {
	Name                   string          `json:"name"`
	Profile                string          `json:"profile"`
	AdAccountID            string          `json:"adAccountId"`
	PromotedObjectID       string          `json:"promotedObjectId"`
	CampaignIDs            []string        `json:"campaignIds"`
	Mode                   string          `json:"mode"`
	TargetInstallCPA       *appleads.Money `json:"targetInstallCPA,omitempty"`
	MaxTotalDailyBudget    appleads.Money  `json:"maxTotalDailyBudget"`
	MaxCampaignDailyBudget appleads.Money  `json:"maxCampaignDailyBudget"`
	Permissions            Permissions     `json:"permissions"`
	Preset                 string          `json:"preset"`
	Thresholds             Thresholds      `json:"thresholds,omitempty"`
}

type Permissions struct {
	Budget   bool `json:"budget"`
	Bid      bool `json:"bid"`
	Strategy bool `json:"strategy"`
	Pause    bool `json:"pause"`
	Resume   bool `json:"resume"`
	Retest   bool `json:"retest"`
}

type Thresholds struct {
	MinimumCompletedDays      int    `json:"minimumCompletedDays,omitempty"`
	CooldownHours             int    `json:"cooldownHours,omitempty"`
	ChangeStepPercent         string `json:"changeStepPercent,omitempty"`
	MaximumChangePercent      string `json:"maximumChangePercent,omitempty"`
	IncreaseMinimumInstalls   int    `json:"increaseMinimumInstalls,omitempty"`
	IncreaseMaximumCPARatio   string `json:"increaseMaximumCPARatio,omitempty"`
	IncreaseBudgetUtilization string `json:"increaseBudgetUtilization,omitempty"`
	DecreaseMinimumInstalls   int    `json:"decreaseMinimumInstalls,omitempty"`
	DecreaseMinimumCPARatio   string `json:"decreaseMinimumCPARatio,omitempty"`
	PauseSpendMultiple        string `json:"pauseSpendMultiple,omitempty"`
	PauseMinimumCPARatio      string `json:"pauseMinimumCPARatio,omitempty"`
	PauseMinimumInstalls      int    `json:"pauseMinimumInstalls,omitempty"`
	MaxConversionsMinimumDays int    `json:"maxConversionsMinimumDays,omitempty"`
	MaxConversionsDailyAvg    string `json:"maxConversionsDailyAverage,omitempty"`
	RetestDailyBudgetCap      string `json:"retestDailyBudgetCap,omitempty"`
}

func BalancedThresholds() Thresholds {
	return Thresholds{
		MinimumCompletedDays:      14,
		CooldownHours:             72,
		ChangeStepPercent:         "10",
		MaximumChangePercent:      "20",
		IncreaseMinimumInstalls:   20,
		IncreaseMaximumCPARatio:   "0.90",
		IncreaseBudgetUtilization: "0.80",
		DecreaseMinimumInstalls:   10,
		DecreaseMinimumCPARatio:   "1.25",
		PauseSpendMultiple:        "3.00",
		PauseMinimumCPARatio:      "1.50",
		PauseMinimumInstalls:      10,
		MaxConversionsMinimumDays: 14,
		MaxConversionsDailyAvg:    "5.00",
		RetestDailyBudgetCap:      "5.00",
	}
}

func DefaultPolicyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "apple-ads-mcp", "optimization-policies.json"), nil
}

func LoadPolicies(path string) (PolicyFile, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("APPLE_ADS_OPTIMIZATION_POLICIES"))
	}
	if path == "" {
		var err error
		path, err = DefaultPolicyPath()
		if err != nil {
			return PolicyFile{}, "", err
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return PolicyFile{}, path, fmt.Errorf("read optimization policies: %w", err)
	}
	defer file.Close()
	if err := requireSecureRegular(file, "optimization policies"); err != nil {
		return PolicyFile{}, path, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPolicyBody+1))
	if err != nil {
		return PolicyFile{}, path, fmt.Errorf("read optimization policies: %w", err)
	}
	if len(data) > maxPolicyBody {
		return PolicyFile{}, path, errors.New("optimization policies exceed size limit")
	}
	var policies PolicyFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policies); err != nil {
		return PolicyFile{}, path, fmt.Errorf("decode optimization policies: %w", err)
	}
	if err := policies.Validate(); err != nil {
		return PolicyFile{}, path, fmt.Errorf("validate optimization policies: %w", err)
	}
	return policies, path, nil
}

func LoadPoliciesOptional(path string) (PolicyFile, string, error) {
	policies, source, err := LoadPolicies(path)
	if errors.Is(err, os.ErrNotExist) || err != nil && strings.Contains(err.Error(), "no such file") {
		return PolicyFile{}, source, nil
	}
	return policies, source, err
}

func SavePolicies(path string, policies PolicyFile) error {
	if err := policies.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		return fmt.Errorf("encode optimization policies: %w", err)
	}
	return secureCreate(path, append(data, '\n'))
}

func AddPolicy(path string, policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	policies, _, err := LoadPolicies(path)
	if errors.Is(err, os.ErrNotExist) || err != nil && strings.Contains(err.Error(), "no such file") {
		return SavePolicies(path, PolicyFile{Policies: []Policy{policy}})
	}
	if err != nil {
		return err
	}
	for _, existing := range policies.Policies {
		if strings.EqualFold(existing.Name, policy.Name) {
			return fmt.Errorf("optimization policy %q already exists", policy.Name)
		}
	}
	policies.Policies = append(policies.Policies, policy)
	if err := policies.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		return fmt.Errorf("encode optimization policies: %w", err)
	}
	return secureReplace(path, append(data, '\n'))
}

func (f PolicyFile) Validate() error {
	if len(f.Policies) == 0 {
		return errors.New("at least one optimization policy is required")
	}
	seen := make(map[string]struct{}, len(f.Policies))
	for index := range f.Policies {
		if err := f.Policies[index].Validate(); err != nil {
			return fmt.Errorf("policy %d: %w", index+1, err)
		}
		key := strings.ToLower(f.Policies[index].Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate optimization policy %q", f.Policies[index].Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (p Policy) Validate() error {
	for field, value := range map[string]string{
		"name": p.Name, "profile": p.Profile, "adAccountId": p.AdAccountID,
		"promotedObjectId": p.PromotedObjectID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if !validLocalName(p.Name) {
		return errors.New("name may contain only letters, digits, hyphens, and underscores")
	}
	if len(p.CampaignIDs) == 0 || len(p.CampaignIDs) > maxCampaigns {
		return fmt.Errorf("campaignIds must contain 1 to %d IDs", maxCampaigns)
	}
	seen := map[string]struct{}{}
	for _, id := range p.CampaignIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("campaignIds must not contain empty IDs")
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate campaignId %q", id)
		}
		seen[id] = struct{}{}
	}
	if p.Mode != "learning" && p.Mode != "active" {
		return errors.New("mode must be learning or active")
	}
	if p.Mode == "learning" && p.TargetInstallCPA != nil {
		return errors.New("learning policy must not set targetInstallCPA")
	}
	if p.Mode == "active" && p.TargetInstallCPA == nil {
		return errors.New("active policy requires targetInstallCPA")
	}
	if p.Preset != "balanced" {
		return errors.New("preset must be balanced")
	}
	if err := p.MaxTotalDailyBudget.ValidatePositive(); err != nil {
		return fmt.Errorf("maxTotalDailyBudget: %w", err)
	}
	if err := p.MaxCampaignDailyBudget.ValidatePositive(); err != nil {
		return fmt.Errorf("maxCampaignDailyBudget: %w", err)
	}
	if p.MaxTotalDailyBudget.Currency != p.MaxCampaignDailyBudget.Currency {
		return errors.New("budget cap currencies must match")
	}
	if p.TargetInstallCPA != nil {
		if err := p.TargetInstallCPA.ValidatePositive(); err != nil {
			return fmt.Errorf("targetInstallCPA: %w", err)
		}
		if p.TargetInstallCPA.Currency != p.MaxTotalDailyBudget.Currency {
			return errors.New("targetInstallCPA currency must match budget caps")
		}
	}
	if p.Permissions.Retest && !p.Permissions.Resume {
		return errors.New("retest permission requires resume permission")
	}
	return validateTightenedThresholds(p.Thresholds)
}

func validLocalName(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func (f PolicyFile) Resolve(name string) (Policy, error) {
	for _, policy := range f.Policies {
		if strings.EqualFold(policy.Name, strings.TrimSpace(name)) {
			policy.Thresholds = resolvedThresholds(policy.Thresholds)
			return policy, nil
		}
	}
	return Policy{}, fmt.Errorf("optimization policy %q not found", name)
}

func resolvedThresholds(value Thresholds) Thresholds {
	base := BalancedThresholds()
	mergeInt := func(value, fallback int) int {
		if value == 0 {
			return fallback
		}
		return value
	}
	mergeString := func(value, fallback string) string {
		if value == "" {
			return fallback
		}
		return value
	}
	return Thresholds{
		MinimumCompletedDays:      mergeInt(value.MinimumCompletedDays, base.MinimumCompletedDays),
		CooldownHours:             mergeInt(value.CooldownHours, base.CooldownHours),
		ChangeStepPercent:         mergeString(value.ChangeStepPercent, base.ChangeStepPercent),
		MaximumChangePercent:      mergeString(value.MaximumChangePercent, base.MaximumChangePercent),
		IncreaseMinimumInstalls:   mergeInt(value.IncreaseMinimumInstalls, base.IncreaseMinimumInstalls),
		IncreaseMaximumCPARatio:   mergeString(value.IncreaseMaximumCPARatio, base.IncreaseMaximumCPARatio),
		IncreaseBudgetUtilization: mergeString(value.IncreaseBudgetUtilization, base.IncreaseBudgetUtilization),
		DecreaseMinimumInstalls:   mergeInt(value.DecreaseMinimumInstalls, base.DecreaseMinimumInstalls),
		DecreaseMinimumCPARatio:   mergeString(value.DecreaseMinimumCPARatio, base.DecreaseMinimumCPARatio),
		PauseSpendMultiple:        mergeString(value.PauseSpendMultiple, base.PauseSpendMultiple),
		PauseMinimumCPARatio:      mergeString(value.PauseMinimumCPARatio, base.PauseMinimumCPARatio),
		PauseMinimumInstalls:      mergeInt(value.PauseMinimumInstalls, base.PauseMinimumInstalls),
		MaxConversionsMinimumDays: mergeInt(value.MaxConversionsMinimumDays, base.MaxConversionsMinimumDays),
		MaxConversionsDailyAvg:    mergeString(value.MaxConversionsDailyAvg, base.MaxConversionsDailyAvg),
		RetestDailyBudgetCap:      mergeString(value.RetestDailyBudgetCap, base.RetestDailyBudgetCap),
	}
}

func validateTightenedThresholds(value Thresholds) error {
	resolved := resolvedThresholds(value)
	base := BalancedThresholds()
	checks := []struct {
		name      string
		value     string
		baseline  string
		direction int
	}{
		{"changeStepPercent", resolved.ChangeStepPercent, base.ChangeStepPercent, -1},
		{"maximumChangePercent", resolved.MaximumChangePercent, base.MaximumChangePercent, -1},
		{"increaseMaximumCPARatio", resolved.IncreaseMaximumCPARatio, base.IncreaseMaximumCPARatio, -1},
		{"increaseBudgetUtilization", resolved.IncreaseBudgetUtilization, base.IncreaseBudgetUtilization, 1},
		{"decreaseMinimumCPARatio", resolved.DecreaseMinimumCPARatio, base.DecreaseMinimumCPARatio, 1},
		{"pauseSpendMultiple", resolved.PauseSpendMultiple, base.PauseSpendMultiple, 1},
		{"pauseMinimumCPARatio", resolved.PauseMinimumCPARatio, base.PauseMinimumCPARatio, 1},
		{"maxConversionsDailyAverage", resolved.MaxConversionsDailyAvg, base.MaxConversionsDailyAvg, 1},
		{"retestDailyBudgetCap", resolved.RetestDailyBudgetCap, base.RetestDailyBudgetCap, -1},
	}
	for _, check := range checks {
		actual, err := positiveRat(check.value)
		if err != nil {
			return fmt.Errorf("%s: %w", check.name, err)
		}
		baseline, _ := positiveRat(check.baseline)
		comparison := actual.Cmp(baseline)
		if check.direction < 0 && comparison > 0 || check.direction > 0 && comparison < 0 {
			return fmt.Errorf("%s may only tighten the balanced preset", check.name)
		}
	}
	if resolved.MinimumCompletedDays < base.MinimumCompletedDays || resolved.CooldownHours < base.CooldownHours ||
		resolved.IncreaseMinimumInstalls < base.IncreaseMinimumInstalls || resolved.DecreaseMinimumInstalls < base.DecreaseMinimumInstalls ||
		resolved.PauseMinimumInstalls < base.PauseMinimumInstalls || resolved.MaxConversionsMinimumDays < base.MaxConversionsMinimumDays {
		return errors.New("integer thresholds may only tighten the balanced preset")
	}
	if ratOrZeroPolicy(resolved.ChangeStepPercent).Cmp(ratOrZeroPolicy(resolved.MaximumChangePercent)) > 0 {
		return errors.New("changeStepPercent must not exceed maximumChangePercent")
	}
	return nil
}

func ratOrZeroPolicy(value string) *big.Rat {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return new(big.Rat)
	}
	return parsed
}

func positiveRat(value string) (*big.Rat, error) {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok || parsed.Sign() <= 0 {
		return nil, errors.New("must be a positive decimal string")
	}
	return parsed, nil
}

func secureCreate(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create private directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write private file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync private file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close private file: %w", err)
	}
	return nil
}

func secureReplace(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create private directory: %w", err)
	}
	if existing, err := os.Open(path); err == nil {
		if secureErr := requireSecureRegular(existing, "private configuration"); secureErr != nil {
			_ = existing.Close()
			return secureErr
		}
		_ = existing.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect private configuration: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".private-*.tmp")
	if err != nil {
		return fmt.Errorf("create private temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if runtime.GOOS != "windows" {
		if err := temporary.Chmod(0o600); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("protect private temporary file: %w", err)
		}
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write private temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync private temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close private temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace private configuration: %w", err)
	}
	return nil
}

func requireSecureRegular(file *os.File, label string) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s path must reference a regular file", label)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s permissions must deny group and other access; current mode is %04o", label, info.Mode().Perm())
	}
	return nil
}
