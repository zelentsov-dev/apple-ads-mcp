package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

type recoveryManager struct {
	states map[string]any
}

func (m *recoveryManager) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	return appleads.Result{Data: m.states[operation.Path()], Status: 200}, nil
}

func (*recoveryManager) Profile(string) (config.Profile, error) {
	return config.Profile{}, nil
}

func (*recoveryManager) Profiles() []config.PublicProfile {
	return nil
}

func TestOptimizationVerificationRecoversAfterInMemoryReceiptLoss(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "optimization-policies.json")
	policy := optimization.Policy{
		Name: "recovery", Profile: "owner", AdAccountID: "123", PromotedObjectID: "456",
		CampaignIDs: []string{"789"}, Mode: "active",
		TargetInstallCPA:       &appleads.Money{Amount: "10.00", Currency: "USD"},
		MaxTotalDailyBudget:    appleads.Money{Amount: "100.00", Currency: "USD"},
		MaxCampaignDailyBudget: appleads.Money{Amount: "50.00", Currency: "USD"},
		Permissions:            optimization.Permissions{Budget: true}, Preset: "balanced",
	}
	if err := optimization.SavePolicies(policyPath, optimization.PolicyFile{Policies: []optimization.Policy{policy}}); err != nil {
		t.Fatal(err)
	}
	historyRoot := filepath.Join(root, "history")
	historyStore, err := optimization.NewHistoryStore(historyRoot)
	if err != nil {
		t.Fatal(err)
	}
	receipt := "opaque-restart-receipt"
	sum := sha256.Sum256([]byte(receipt))
	receiptHash := hex.EncodeToString(sum[:])
	if err := historyStore.BeginIntent(policy.Name, optimization.HistoryEntry{
		Policy: policy.Name, Profile: policy.Profile, AdAccountID: policy.AdAccountID,
		ReceiptHash: receiptHash, Status: "applying",
		Actions: []optimization.HistoryAction{{
			CorrelationID: "pause", CampaignID: "789", ResourceType: "campaign", Resource: "campaigns",
			ResourceID: "789", Action: "pause", Status: "pending", OccurredAt: "2026-08-24T10:00:00Z",
			Before: map[string]any{"status": "ENABLED"}, After: map[string]any{"status": "PAUSED"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		manager:    &recoveryManager{states: map[string]any{"campaigns/789": map[string]any{"id": "789", "status": "PAUSED"}}},
		policyPath: policyPath, historyRoot: historyRoot,
	}
	preview, verification, err := service.recoverOptimizationVerification(context.Background(), receipt)
	if err != nil || verification.Status != "verified" || preview.Impact == nil || preview.Impact.Policy != policy.Name {
		t.Fatalf("preview=%+v verification=%+v err=%v", preview, verification, err)
	}
	if err := service.recordOptimizationVerification(context.Background(), preview, verification); err != nil {
		t.Fatal(err)
	}
	history, err := historyStore.Load(policy.Name)
	if err != nil || optimization.HistoryRequiresReconciliation(history) {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	data, err := os.ReadFile(filepath.Join(historyRoot, policy.Name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), receipt) {
		t.Fatal("raw receipt was persisted in optimization history")
	}
}
