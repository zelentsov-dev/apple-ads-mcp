package optimization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
