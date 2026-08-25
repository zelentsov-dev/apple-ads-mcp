package optimization

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

func validPolicy() Policy {
	return Policy{
		Name: "voice-app", Profile: "owner", AdAccountID: "123", PromotedObjectID: "456",
		CampaignIDs: []string{"789"}, Mode: "active",
		TargetInstallCPA:       &appleads.Money{Amount: "10.00", Currency: "USD"},
		MaxTotalDailyBudget:    appleads.Money{Amount: "100.00", Currency: "USD"},
		MaxCampaignDailyBudget: appleads.Money{Amount: "50.00", Currency: "USD"},
		Permissions:            Permissions{Budget: true, Bid: true, Strategy: true, Pause: true}, Preset: "balanced",
	}
}

func TestPolicyModesCapsAndTightening(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Policy)
		valid  bool
	}{
		{"active", func(*Policy) {}, true},
		{"learning", func(p *Policy) { p.Mode = "learning"; p.TargetInstallCPA = nil }, true},
		{"active target missing", func(p *Policy) { p.TargetInstallCPA = nil }, false},
		{"learning target present", func(p *Policy) { p.Mode = "learning" }, false},
		{"currency mismatch", func(p *Policy) { p.MaxCampaignDailyBudget.Currency = "EUR" }, false},
		{"unsafe name", func(p *Policy) { p.Name = "../policy" }, false},
		{"looser step", func(p *Policy) { p.Thresholds.ChangeStepPercent = "11" }, false},
		{"looser cooldown", func(p *Policy) { p.Thresholds.CooldownHours = 48 }, false},
		{"looser retest cap", func(p *Policy) { p.Thresholds.RetestDailyBudgetCap = "6.00" }, false},
		{"tighter retest cap", func(p *Policy) { p.Thresholds.RetestDailyBudgetCap = "3.00" }, true},
		{"tighter", func(p *Policy) {
			p.Thresholds.CooldownHours = 96
			p.Thresholds.IncreaseMinimumInstalls = 30
			p.Thresholds.ChangeStepPercent = "5"
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := validPolicy()
			test.mutate(&policy)
			err := policy.Validate()
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestPolicyCampaignLimitAndPermissions(t *testing.T) {
	policy := validPolicy()
	for index := 0; index < 20; index++ {
		policy.CampaignIDs = append(policy.CampaignIDs, string(rune('a'+index)))
	}
	if err := policy.Validate(); err == nil {
		t.Fatal("expected maximum campaign limit")
	}
}

func TestPolicyFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "optimization-policies.json")
	if err := SavePolicies(path, PolicyFile{Policies: []Policy{validPolicy()}}); err != nil {
		t.Fatal(err)
	}
	loaded, source, err := LoadPolicies(path)
	if err != nil || source != path || len(loaded.Policies) != 1 {
		t.Fatalf("loaded=%+v source=%q err=%v", loaded, source, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%04o", info.Mode().Perm())
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadPolicies(path); err == nil {
			t.Fatal("expected insecure permissions rejection")
		}
	}
}

func TestAddPolicyPreservesExistingFileAndRejectsDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "optimization-policies.json")
	first := validPolicy()
	if err := AddPolicy(path, first); err != nil {
		t.Fatal(err)
	}
	second := validPolicy()
	second.Name = "second-app"
	second.CampaignIDs = []string{"999"}
	if err := AddPolicy(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadPolicies(path)
	if err != nil || len(loaded.Policies) != 2 {
		t.Fatalf("policies=%+v err=%v", loaded.Policies, err)
	}
	if err := AddPolicy(path, second); err == nil {
		t.Fatal("expected duplicate policy rejection")
	}
}
