package operations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

type fakeExecutor struct {
	state     any
	writes    int
	ambiguous bool
	onRead    func()
}

func (f *fakeExecutor) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if operation.IsMutation() {
		f.writes++
		if f.ambiguous {
			return appleads.Result{}, &appleads.AmbiguousWriteError{Cause: errors.New("timeout")}
		}
		return appleads.Result{Data: map[string]any{"updated": true}, Status: 200}, nil
	}
	if f.onRead != nil {
		f.onRead()
	}
	return appleads.Result{Data: f.state, Status: 200}, nil
}

func TestReceiptApplySingleUseAndBinding(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, 10*time.Minute)
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "campaign_pause", []string{"123"}, map[string]any{"status": "PAUSED"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Profile != "owner" || preview.AdAccountID != "456" || preview.PayloadHash == "" {
		t.Fatalf("preview=%+v", preview)
	}
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil || receipt.Status != "applied" || executor.writes != 1 {
		t.Fatalf("receipt=%+v err=%v writes=%d", receipt, err, executor.writes)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err == nil {
		t.Fatal("expected receipt reuse to fail")
	}
}

func TestReceiptExpiryAndDrift(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, _ := store.Preview(context.Background(), executor, "owner", "456", "pause", nil, map[string]any{"status": "PAUSED"}, verify, mutation)
	executor.state = map[string]any{"status": "PAUSED"}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err == nil {
		t.Fatal("expected drift rejection")
	}
	executor.state = map[string]any{"status": "ENABLED"}
	now = now.Add(2 * time.Minute)
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err == nil {
		t.Fatal("expected expiry rejection")
	}
}

func TestAmbiguousWriteReceipt(t *testing.T) {
	now := time.Now()
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{"name": "before"}, ambiguous: true}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"name": "after"})
	preview, _ := store.Preview(context.Background(), executor, "owner", "456", "update", nil, map[string]any{"name": "after"}, verify, mutation)
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil || receipt.Status != "unknown" || receipt.Verification != "committed_unverified" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	executor.ambiguous = false
	executor.state = map[string]any{"name": "after"}
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || verification.Status != "inconclusive" || !verification.Used {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
}

func TestReceiptExpiryIsRecheckedAfterVerification(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "pause", []string{"123"}, map[string]any{"status": "PAUSED"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	executor.onRead = func() { now = now.Add(2 * time.Minute) }
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err == nil {
		t.Fatal("expected expiry after verification")
	}
	if executor.writes != 0 {
		t.Fatalf("writes=%d", executor.writes)
	}
}

func TestReceiptByteCapacity(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"name": "small"})
	_, err := store.Preview(context.Background(), executor, "owner", "456", "update", []string{"123"}, map[string]any{"name": strings.Repeat("x", 9<<20)}, verify, mutation)
	if err == nil {
		t.Fatal("expected receipt byte capacity rejection")
	}
}
