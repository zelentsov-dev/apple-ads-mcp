package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

type fakeExecutor struct {
	state        any
	mutationData any
	writes       int
	ambiguous    bool
	onRead       func()
}

type sequenceExecutor struct {
	state      any
	writes     int
	failAt     int
	failure    error
	deleteRead bool
}

func (f *sequenceExecutor) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if !operation.IsMutation() {
		if f.deleteRead {
			return appleads.Result{}, &appleads.APIError{HTTPStatus: 404, Message: "Not Found"}
		}
		return appleads.Result{Data: f.state, Status: 200}, nil
	}
	f.writes++
	if f.failAt == f.writes {
		return appleads.Result{}, f.failure
	}
	return appleads.Result{Data: map[string]any{"id": operation.Path()}, Status: 200}, nil
}

func (f *fakeExecutor) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if operation.IsMutation() {
		f.writes++
		if f.ambiguous {
			return appleads.Result{}, &appleads.AmbiguousWriteError{Cause: errors.New("timeout")}
		}
		data := f.mutationData
		if data == nil {
			data = map[string]any{"updated": true}
		}
		return appleads.Result{Data: data, Status: 200}, nil
	}
	if f.onRead != nil {
		f.onRead()
	}
	return appleads.Result{Data: f.state, Status: 200}, nil
}

func TestBulkReceiptReportsPartialAndUnknownItems(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, 10*time.Minute)
	executor := &fakeExecutor{
		state: map[string]any{"items": []any{}},
		mutationData: map[string]any{"items": []any{
			map[string]any{"correlationId": "1", "success": true, "result": map[string]any{"id": "101"}},
			map[string]any{"correlationId": "2", "success": false, "error": map[string]any{"message": "invalid"}},
		}},
	}
	verify, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"pageSize": 200}})
	mutation, _ := appleads.BulkResource("keywords", "create", map[string]any{"items": []any{}})
	items := []OperationItemPreview{
		{CorrelationID: "1", After: map[string]any{"text": "voice"}},
		{CorrelationID: "2", After: map[string]any{"text": "notes"}},
	}
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, []VerificationRead{{Name: "inventory", Operation: verify}}, mutation, PreviewOptions{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "partial" || len(receipt.Items) != 2 || receipt.Items[0].Status != "applied" || receipt.Items[0].TargetID != "101" || receipt.Items[1].Status != "failed" {
		t.Fatalf("receipt=%+v", receipt)
	}

	store = NewStoreForTest(func() time.Time { return now }, 10*time.Minute)
	executor = &fakeExecutor{state: map[string]any{"items": []any{}}, ambiguous: true}
	preview, err = store.PreviewComposite(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, []VerificationRead{{Name: "inventory", Operation: verify}}, mutation, PreviewOptions{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil || receipt.Status != "unknown" || len(receipt.Items) != 2 || receipt.Items[0].Status != "unknown" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
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

func TestReceiptOutputsRedactPrivateBillingData(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{
		state: map[string]any{
			"id":               "123",
			"invoiceDetail":    map[string]any{"billingEmail": "private@example.com"},
			"primaryBuyerName": "Private Buyer",
		},
		mutationData: map[string]any{
			"id":              "123",
			"billing_contact": map[string]any{"email": "private@example.com"},
			"name":            "Public Budget",
		},
	}
	verify, _ := appleads.ResourceGet("shared-budgets", "123")
	mutation, _ := appleads.ResourceUpdate("shared-budgets", "123", map[string]any{"name": "Public Budget"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "shared_budget_update", []string{"123"}, map[string]any{"name": "Public Budget"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateBillingData(t, preview.Before)
	inspected, _, err := store.Inspect(preview.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateBillingData(t, inspected.Before)
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateBillingData(t, receipt.Result)
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateBillingData(t, verification)
}

func assertNoPrivateBillingData(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"private@example.com", "private buyer", "invoicedetail", "billing_contact", "primarybuyer"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("private billing data leaked: %s", data)
		}
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

func TestReceiptIgnoresUnorderedArrayOrdering(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{
		"systemStatusReasons": []any{"AD_GROUPS_MISSING", "PAUSED_BY_USER"},
		"targeting":           map[string]any{"countries": []any{"US", "JP"}},
		"sharedBudgets":       []any{map[string]any{"budgetId": "10"}, map[string]any{"budgetId": "20"}},
	}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "pause", nil, map[string]any{"status": "PAUSED"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	executor.state = map[string]any{
		"systemStatusReasons": []any{"PAUSED_BY_USER", "AD_GROUPS_MISSING"},
		"targeting":           map[string]any{"countries": []any{"JP", "US"}},
		"sharedBudgets":       []any{map[string]any{"budgetId": "20"}, map[string]any{"budgetId": "10"}},
	}
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil || receipt.Status != "applied" || executor.writes != 1 {
		t.Fatalf("receipt=%+v err=%v writes=%d", receipt, err, executor.writes)
	}
}

func TestReceiptIgnoresQueryResultOrdering(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{
		"data": map[string]any{"result": []any{
			map[string]any{"id": "1", "text": "voice"},
			map[string]any{"id": "2", "text": "notes"},
		}},
	}}
	verify, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"pageSize": 200}})
	mutation, _ := appleads.BulkResource("keywords", "update", map[string]any{"items": []any{}})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	executor.state = map[string]any{
		"data": map[string]any{"result": []any{
			map[string]any{"id": "2", "text": "notes"},
			map[string]any{"id": "1", "text": "voice"},
		}},
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err != nil {
		t.Fatalf("query result ordering must not cause drift: %v", err)
	}
}

func TestReceiptRejectsOrderedArrayDrift(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{
		"creativeOrder": []any{"default", "seasonal"},
	}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "pause", nil, map[string]any{"status": "PAUSED"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	executor.state = map[string]any{
		"creativeOrder": []any{"seasonal", "default"},
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err == nil {
		t.Fatal("expected ordered array drift rejection")
	}
	if executor.writes != 0 {
		t.Fatalf("writes=%d", executor.writes)
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
	if err != nil || verification.Status != "inconclusive" || !verification.Used || len(verification.Objects) != 1 || verification.Objects[0].Status != "changed" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
}

func TestCompositeVerificationReportsEachObject(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"status": "PAUSED"}}
	first, _ := appleads.ResourceGet("campaigns", "123")
	second, _ := appleads.ResourceGet("adgroups", "456")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"dailyBudget": map[string]any{"amount": "10.00", "currency": "USD"}})
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "recommendation", []string{"123", "456"}, map[string]any{"maximumAmount": "10.00"}, []VerificationRead{
		{Name: "recommendation", Operation: first},
		{Name: "campaign", Operation: second},
	}, mutation, PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	executor.state = map[string]any{"status": "ENABLED"}
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || len(verification.Objects) != 2 {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
	for _, object := range verification.Objects {
		if object.Status != "changed" {
			t.Fatalf("object=%+v", object)
		}
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

func TestSequenceReceiptByteCapacityIncludesMutationBodies(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	steps := make([]SequenceStep, 0, 64)
	for index := 0; index < 64; index++ {
		mutation, _ := appleads.ResourceUpdate("campaigns", fmt.Sprint(index+1), map[string]any{"private": strings.Repeat("x", 600<<10)})
		steps = append(steps, SequenceStep{
			Item:     OperationItemPreview{CorrelationID: fmt.Sprintf("item-%d", index+1), After: map[string]any{"status": "PAUSED"}},
			Mutation: mutation,
		})
	}
	if _, err := store.PreviewSequence(context.Background(), executor, "owner", "10", "plan", nil, []VerificationRead{{Name: "inventory", Operation: verify}}, steps, nil); err == nil {
		t.Fatal("expected sequence receipt byte capacity rejection")
	}
}

func TestSequenceContinuesIndependentClientFailuresAndSkipsDependencies(t *testing.T) {
	store := NewStore()
	executor := &sequenceExecutor{state: map[string]any{"status": "ENABLED"}, failAt: 1, failure: &appleads.APIError{HTTPStatus: 400, Code: "INVALID", Message: "invalid"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	first, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	second, _ := appleads.ResourceUpdate("campaigns", "456", map[string]any{"status": "PAUSED"})
	third, _ := appleads.ResourceUpdate("campaigns", "789", map[string]any{"status": "PAUSED"})
	preview, err := store.PreviewSequence(context.Background(), executor, "owner", "10", "plan", []string{"123", "456", "789"}, []VerificationRead{{Name: "inventory", Operation: verify}}, []SequenceStep{
		{Item: OperationItemPreview{CorrelationID: "a", CampaignID: "123", TargetID: "123", Action: "pause", After: map[string]any{"status": "PAUSED"}}, Mutation: first},
		{Item: OperationItemPreview{CorrelationID: "b", CampaignID: "456", TargetID: "456", Action: "pause", After: map[string]any{"status": "PAUSED"}}, Mutation: second},
		{Item: OperationItemPreview{CorrelationID: "c", CampaignID: "789", TargetID: "789", Action: "pause", After: map[string]any{"status": "PAUSED"}, DependsOn: []string{"a"}}, Mutation: third},
	}, &OperationImpact{SpendAffecting: true, ObjectCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "partial" || executor.writes != 2 || receipt.Items[0].Status != "failed" || receipt.Items[1].Status != "applied" || receipt.Items[2].Status != "skipped" {
		t.Fatalf("receipt=%+v writes=%d", receipt, executor.writes)
	}
}

func TestSequenceStopsAfterAmbiguousWrite(t *testing.T) {
	store := NewStore()
	executor := &sequenceExecutor{state: map[string]any{"status": "ENABLED"}, failAt: 2, failure: &appleads.AmbiguousWriteError{Cause: errors.New("timeout")}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	steps := make([]SequenceStep, 0, 3)
	for index, id := range []string{"123", "456", "789"} {
		mutation, _ := appleads.ResourceUpdate("campaigns", id, map[string]any{"status": "PAUSED"})
		steps = append(steps, SequenceStep{Item: OperationItemPreview{CorrelationID: string(rune('a' + index)), TargetID: id, After: map[string]any{"status": "PAUSED"}}, Mutation: mutation})
	}
	preview, err := store.PreviewSequence(context.Background(), executor, "owner", "10", "plan", nil, []VerificationRead{{Name: "inventory", Operation: verify}}, steps, nil)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	if err != nil || receipt.Status != "unknown" || executor.writes != 2 || receipt.Items[0].Status != "applied" || receipt.Items[1].Status != "unknown" || receipt.Items[2].Status != "not_attempted" {
		t.Fatalf("receipt=%+v writes=%d err=%v", receipt, executor.writes, err)
	}
}

func TestDeleteVerificationTreatsNotFoundAsDeleted(t *testing.T) {
	store := NewStore()
	executor := &sequenceExecutor{state: map[string]any{"id": "123", "name": "fixture"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceDelete("campaigns", "123")
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "10", "delete", []string{"123"}, map[string]any{"expectedText": "fixture"}, []VerificationRead{{Name: "target", Operation: verify, ExpectDeleted: true}}, mutation, PreviewOptions{Impact: &OperationImpact{Destructive: true, ObjectCount: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err != nil {
		t.Fatal(err)
	}
	executor.deleteRead = true
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || len(verification.Objects) != 1 || verification.Objects[0].Status != "deleted" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
}
