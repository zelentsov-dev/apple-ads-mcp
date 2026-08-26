package optimization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBillingProfilePrivateStorageAndHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "billing-profiles.json")
	profile := BillingProfile{Name: "loc", PrimaryBuyerName: "Buyer", PrimaryBuyerEmail: "buyer@example.com", BillingEmail: "billing@example.com"}
	if err := SaveBillingProfiles(path, BillingProfileFile{Profiles: []BillingProfile{profile}}); err != nil {
		t.Fatal(err)
	}
	profiles, _, err := LoadBillingProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := profiles.Resolve("loc")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := resolved.PrivateHash()
	if err != nil || len(hash) != 64 {
		t.Fatalf("hash=%q err=%v", hash, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%04o", info.Mode().Perm())
		}
	}
}

func TestAddBillingProfilePreservesExistingFileAndRejectsDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "billing-profiles.json")
	first := BillingProfile{Name: "primary", PrimaryBuyerName: "Buyer", PrimaryBuyerEmail: "buyer@example.com", BillingEmail: "billing@example.com"}
	if err := AddBillingProfile(path, first); err != nil {
		t.Fatal(err)
	}
	second := BillingProfile{Name: "secondary", PrimaryBuyerName: "Second", PrimaryBuyerEmail: "second@example.com", BillingEmail: "finance@example.com"}
	if err := AddBillingProfile(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadBillingProfiles(path)
	if err != nil || len(loaded.Profiles) != 2 {
		t.Fatalf("profiles=%+v err=%v", loaded.Profiles, err)
	}
	if err := AddBillingProfile(path, second); err == nil {
		t.Fatal("expected duplicate billing profile rejection")
	}
}

func TestHistoryIsAtomicBoundedAndCredentialFree(t *testing.T) {
	root := t.TempDir()
	store, err := NewHistoryStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxHistoryEntries+10; index++ {
		if err := store.Append("policy", HistoryEntry{Policy: "policy", Profile: "owner", AdAccountID: "123", Status: "applied"}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := store.Load("policy")
	if err != nil || len(history.Entries) != maxHistoryEntries {
		t.Fatalf("entries=%d err=%v", len(history.Entries), err)
	}
	data, err := os.ReadFile(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || strings.Contains(strings.ToLower(string(data)), "privatekey") || strings.Contains(strings.ToLower(string(data)), "billingemail") {
		t.Fatalf("unsafe history=%s", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, "policy.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%04o", info.Mode().Perm())
		}
	}
}

func TestHistoryRejectsPrivateFields(t *testing.T) {
	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entry := HistoryEntry{Policy: "policy", Actions: []HistoryAction{{Before: map[string]any{"billingEmail": "private@example.com"}}}}
	if err := store.Append("policy", entry); err == nil {
		t.Fatal("expected private history field rejection")
	}
}

func TestHistoryIntentIsExclusiveAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	first, err := NewHistoryStore(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewHistoryStore(root)
	if err != nil {
		t.Fatal(err)
	}
	stores := []*HistoryStore{first, second}
	results := make(chan error, len(stores))
	var wait sync.WaitGroup
	for index, store := range stores {
		wait.Add(1)
		go func(index int, store *HistoryStore) {
			defer wait.Done()
			results <- store.BeginIntent("policy", HistoryEntry{Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: string(rune('a' + index)), Status: "applying", Actions: []HistoryAction{{CorrelationID: "budget", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "budget_increase", Status: "pending", After: map[string]any{"dailyBudget": "11"}}}})
		}(index, store)
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful intents=%d", successes)
	}
	history, err := first.Load("policy")
	if err != nil || !HistoryRequiresReconciliation(history) {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestHistoryRetentionPreservesUnresolvedRecoveryIntent(t *testing.T) {
	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	intent := HistoryEntry{
		Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "applying",
		Actions: []HistoryAction{{CorrelationID: "budget", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "budget_increase", Status: "pending", Before: map[string]any{"dailyBudget": "10"}, After: map[string]any{"dailyBudget": "11"}, OccurredAt: "2026-08-24T10:00:00Z"}},
	}
	if err := store.BeginIntent("policy", intent); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxHistoryEntries+10; index++ {
		if err := store.Append("policy", HistoryEntry{Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "verification_inconclusive"}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := store.Load("policy")
	if err != nil || len(history.Entries) != maxHistoryEntries || !HistoryRequiresReconciliation(history) {
		t.Fatalf("entries=%d unresolved=%v err=%v", len(history.Entries), HistoryRequiresReconciliation(history), err)
	}
	entry, ok := ReconciliationEntry(history, "receipt-hash")
	if !ok || len(entry.Actions) != 1 || entry.Actions[0].Resource != "campaigns" {
		t.Fatalf("entry=%+v ok=%v", entry, ok)
	}
	if err := store.Append("policy", HistoryEntry{Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "verification_verified"}); err != nil {
		t.Fatal(err)
	}
	history, err = store.Load("policy")
	if err != nil || HistoryRequiresReconciliation(history) {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestHistoryTerminalCompactionPreservesReconciledActionState(t *testing.T) {
	for _, verificationStatus := range []string{"matched", "matched_after", "matched_before"} {
		t.Run(verificationStatus, func(t *testing.T) {
			store, err := NewHistoryStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			intentTime := "2026-08-24T10:00:00Z"
			intent := HistoryEntry{
				Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "applying",
				Actions: []HistoryAction{{CorrelationID: "pause", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "pause", Status: "pending", Before: map[string]any{"status": "ENABLED"}, After: map[string]any{"status": "PAUSED"}, OccurredAt: intentTime}},
			}
			if err := store.BeginIntent("policy", intent); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < maxHistoryEntries+10; index++ {
				if err := store.Append("policy", HistoryEntry{Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "verification_inconclusive"}); err != nil {
					t.Fatal(err)
				}
			}
			verificationTime := "2026-08-24T11:00:00Z"
			if err := store.Append("policy", HistoryEntry{
				Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "verification_verified",
				Verification: []HistoryAction{{CorrelationID: "pause", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "pause", Status: verificationStatus, After: map[string]any{"modificationTime": verificationTime}, OccurredAt: verificationTime}},
			}); err != nil {
				t.Fatal(err)
			}
			history, err := store.Load("policy")
			if err != nil {
				t.Fatal(err)
			}
			if HistoryRequiresReconciliation(history) {
				t.Fatal("terminal verification must resolve reconciliation")
			}
			terminal := history.Entries[len(history.Entries)-1]
			if terminal.Status != "verification_verified" || len(terminal.Actions) != 1 || terminal.Actions[0].OccurredAt != intentTime || terminal.Actions[0].Resource != "campaigns" {
				t.Fatalf("terminal=%+v", terminal)
			}
			lastChange, optimizerOwnedPause := historyState(history, "1", verificationTime)
			if verificationStatus == "matched_before" {
				if !lastChange.IsZero() || optimizerOwnedPause {
					t.Fatalf("lastChange=%s owned=%v", lastChange.Format(time.RFC3339), optimizerOwnedPause)
				}
				return
			}
			if lastChange.Format(time.RFC3339) != intentTime || !optimizerOwnedPause {
				t.Fatalf("lastChange=%s owned=%v", lastChange.Format(time.RFC3339), optimizerOwnedPause)
			}
		})
	}
}

func TestHistoryTerminalCompactionPreservesUnknownApplyTime(t *testing.T) {
	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	intent := HistoryEntry{
		Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "applying",
		Actions: []HistoryAction{{CorrelationID: "budget", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "budget_increase", Status: "pending", OccurredAt: "2026-08-24T10:00:00Z"}},
	}
	if err := store.BeginIntent("policy", intent); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("policy", HistoryEntry{
		Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "unknown",
		Actions: []HistoryAction{{CorrelationID: "budget", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "budget_increase", Status: "unknown", OccurredAt: "2026-08-24T10:01:00Z"}},
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxHistoryEntries+10; index++ {
		if err := store.Append("policy", HistoryEntry{Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "verification_inconclusive"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Append("policy", HistoryEntry{
		Policy: "policy", Profile: "owner", AdAccountID: "123", ReceiptHash: "receipt-hash", Status: "verification_verified",
		Verification: []HistoryAction{{CorrelationID: "budget", CampaignID: "1", ResourceType: "campaign", Resource: "campaigns", ResourceID: "1", Action: "budget_increase", Status: "matched_after", OccurredAt: "2026-08-24T11:00:00Z"}},
	}); err != nil {
		t.Fatal(err)
	}
	history, err := store.Load("policy")
	if err != nil {
		t.Fatal(err)
	}
	lastChange, _ := historyState(history, "1", "")
	if lastChange.Format(time.RFC3339) != "2026-08-24T10:01:00Z" {
		t.Fatalf("lastChange=%s", lastChange.Format(time.RFC3339))
	}
}
