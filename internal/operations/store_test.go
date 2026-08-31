package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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
	readStates   map[string]any
	pagination   appleads.Pagination
}

type barrierExecutor struct {
	state     any
	arrived   chan struct{}
	release   chan struct{}
	writes    atomic.Int32
	ambiguous bool
}

type dispatchBarrierExecutor struct {
	state   any
	started chan struct{}
	release chan struct{}
	writes  atomic.Int32
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
	if value, exists := f.readStates[operation.Path()]; exists {
		return appleads.Result{Data: value, Status: 200, Pagination: f.pagination}, nil
	}
	return appleads.Result{Data: f.state, Status: 200, Pagination: f.pagination}, nil
}

func (f *barrierExecutor) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if operation.IsMutation() {
		f.writes.Add(1)
		if f.ambiguous {
			return appleads.Result{}, &appleads.AmbiguousWriteError{Cause: errors.New("timeout")}
		}
		return appleads.Result{Data: map[string]any{"updated": true}, Status: 200}, nil
	}
	if f.arrived != nil {
		f.arrived <- struct{}{}
		<-f.release
	}
	return appleads.Result{Data: f.state, Status: 200}, nil
}

func (f *dispatchBarrierExecutor) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if operation.IsMutation() {
		f.writes.Add(1)
		f.started <- struct{}{}
		<-f.release
		return appleads.Result{Data: map[string]any{"updated": true}, Status: 200}, nil
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
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); !errors.Is(err, ErrReceiptUsed) {
		t.Fatalf("expected receipt reuse error, got %v", err)
	}
}

func TestExpiredReceiptTombstoneSurvivesRecordPruning(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Minute)
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "pause", []string{"123"}, map[string]any{"status": "PAUSED"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, _, err := store.Inspect(preview.Receipt); !errors.Is(err, ErrReceiptExpired) {
		t.Fatalf("inspect error=%v", err)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); !errors.Is(err, ErrReceiptExpired) {
		t.Fatalf("apply error=%v", err)
	}
	if _, _, err := store.Inspect("unknown"); !errors.Is(err, ErrReceiptNotFound) {
		t.Fatalf("unknown error=%v", err)
	}
}

func TestBulkCreateAddsDirectVerificationReadsForReturnedIDs(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{
		state: map[string]any{"items": []any{}},
		mutationData: map[string]any{"items": []any{
			map[string]any{"correlationId": "0", "success": true, "result": map[string]any{"id": "101"}},
		}},
		readStates: map[string]any{},
	}
	query, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"pageSize": 200}})
	mutation, _ := appleads.BulkResource("keywords", "create", map[string]any{"items": []any{}})
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, []VerificationRead{{Name: "inventory", Operation: query}}, mutation, PreviewOptions{Items: []OperationItemPreview{{
		CorrelationID: "0", Resource: "keywords", Action: "create", After: map[string]any{"text": "音声メモ", "status": "ENABLED"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err != nil {
		t.Fatal(err)
	}
	executor.readStates["keywords/101"] = map[string]any{"id": "101", "text": "音声メモ", "status": "ENABLED"}
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || verification.Status != "verified" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
	foundDirect := false
	for _, object := range verification.Objects {
		if object.Name == "direct_item_0" && object.Status == "changed" {
			foundDirect = true
		}
	}
	if !foundDirect {
		t.Fatalf("verification=%+v", verification)
	}
}

func TestBulkItemErrorsPreserveSafeFieldsWithoutRequestData(t *testing.T) {
	failure := map[string]any{
		"code": "INVALID_VALUE", "message": "invalid keyword",
		"info": map[string]any{"field": "text", "requestBody": `{"authorization":"secret"}`},
	}
	safe := publicBulkError(failure)
	data, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	if !strings.Contains(encoded, "INVALID_VALUE") || !strings.Contains(encoded, "invalid keyword") || !strings.Contains(encoded, `"field":"text"`) {
		t.Fatalf("safe error=%s", encoded)
	}
	if strings.Contains(encoded, "requestBody") || strings.Contains(encoded, "secret") {
		t.Fatalf("request data leaked: %s", encoded)
	}
	redacted, err := json.Marshal(redactPrivateData(map[string]any{"errors": []any{failure}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(redacted), "requestBody") || strings.Contains(string(redacted), "secret") {
		t.Fatalf("result data leaked: %s", redacted)
	}
}

func TestIndependentSameScopeReceiptsDriftButOneBulkReceiptAppliesOnce(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"items": []any{}}}
	verify, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"pageSize": 200}})
	firstMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "voice"})
	secondMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "notes"})
	first, err := store.Preview(context.Background(), executor, "owner", "456", "first", nil, map[string]any{"text": "voice"}, verify, firstMutation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Preview(context.Background(), executor, "owner", "456", "second", nil, map[string]any{"text": "notes"}, verify, secondMutation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), executor, first.Receipt); err != nil {
		t.Fatal(err)
	}
	executor.state = map[string]any{"items": []any{map[string]any{"id": "101", "text": "voice"}}}
	if _, err := store.Apply(context.Background(), executor, second.Receipt); !errors.Is(err, ErrStateDrift) {
		t.Fatalf("second same-scope receipt error=%v", err)
	}

	store = NewStore()
	executor = &fakeExecutor{
		state: map[string]any{"items": []any{}},
		mutationData: map[string]any{"items": []any{
			map[string]any{"correlationId": 0, "success": true, "result": map[string]any{"id": "201"}},
			map[string]any{"correlationId": 1, "success": true, "result": map[string]any{"id": "202"}},
		}},
	}
	bulkMutation, _ := appleads.BulkResource("keywords", "create", map[string]any{"items": []any{}})
	bulk, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, []VerificationRead{{Name: "inventory", Operation: verify}}, bulkMutation, PreviewOptions{Items: []OperationItemPreview{
		{CorrelationID: "0", Resource: "keywords", After: map[string]any{"text": "voice"}},
		{CorrelationID: "1", Resource: "keywords", After: map[string]any{"text": "notes"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(context.Background(), executor, bulk.Receipt)
	if err != nil || receipt.Status != "applied" || executor.writes != 1 {
		t.Fatalf("receipt=%+v err=%v writes=%d", receipt, err, executor.writes)
	}
}

func TestConcurrentSameScopeReceiptsReserveBeforeDispatch(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprintf("ambiguous_%t", ambiguous), func(t *testing.T) {
			store := NewStore()
			executor := &barrierExecutor{state: map[string]any{"items": []any{}}, ambiguous: ambiguous}
			verify, _ := appleads.ResourceQuery("keywords", map[string]any{"filters": []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 10}}})
			firstMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "voice"})
			secondMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "notes"})
			first, err := store.Preview(context.Background(), executor, "owner", "456", "first", nil, map[string]any{"text": "voice"}, verify, firstMutation)
			if err != nil {
				t.Fatal(err)
			}
			second, err := store.Preview(context.Background(), executor, "owner", "456", "second", nil, map[string]any{"text": "notes"}, verify, secondMutation)
			if err != nil {
				t.Fatal(err)
			}

			executor.arrived = make(chan struct{}, 2)
			executor.release = make(chan struct{})
			results := make(chan error, 2)
			go func() {
				_, applyErr := store.Apply(context.Background(), executor, first.Receipt)
				results <- applyErr
			}()
			go func() {
				_, applyErr := store.Apply(context.Background(), executor, second.Receipt)
				results <- applyErr
			}()
			<-executor.arrived
			<-executor.arrived
			close(executor.release)

			firstErr := <-results
			secondErr := <-results
			driftCount := 0
			for _, applyErr := range []error{firstErr, secondErr} {
				if errors.Is(applyErr, ErrStateDrift) {
					driftCount++
					continue
				}
				if applyErr != nil {
					t.Fatalf("unexpected apply error: %v", applyErr)
				}
			}
			if driftCount != 1 || executor.writes.Load() != 1 {
				t.Fatalf("driftCount=%d writes=%d errors=[%v %v]", driftCount, executor.writes.Load(), firstErr, secondErr)
			}
			_, previewErr := store.Preview(context.Background(), executor, "owner", "456", "fresh", nil, map[string]any{"text": "fresh"}, verify, firstMutation)
			if ambiguous && !errors.Is(previewErr, ErrStateDrift) {
				t.Fatalf("fresh preview must remain blocked after ambiguous write: %v", previewErr)
			}
			if !ambiguous && previewErr != nil {
				t.Fatalf("fresh preview must be available after a completed write: %v", previewErr)
			}
		})
	}
}

func TestCompositeReceiptReservesEveryScopeBeforeDispatch(t *testing.T) {
	store := NewStore()
	executor := &dispatchBarrierExecutor{
		state:   map[string]any{"items": []any{}},
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	scope10, _ := appleads.ResourceQuery("keywords", map[string]any{"filters": []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 10}}})
	scope20, _ := appleads.ResourceQuery("keywords", map[string]any{"filters": []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 20}}})
	scope30, _ := appleads.ResourceQuery("keywords", map[string]any{"filters": []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 30}}})
	firstMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "first"})
	secondMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "second"})
	first, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "first", nil, map[string]any{"text": "first"}, []VerificationRead{
		{Name: "scope_10", Operation: scope10},
		{Name: "scope_20", Operation: scope20},
	}, firstMutation, PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "second", nil, map[string]any{"text": "second"}, []VerificationRead{
		{Name: "scope_20", Operation: scope20},
		{Name: "scope_30", Operation: scope30},
	}, secondMutation, PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstResult := make(chan error, 1)
	go func() {
		_, applyErr := store.Apply(context.Background(), executor, first.Receipt)
		firstResult <- applyErr
	}()
	<-executor.started
	if _, err := store.Apply(context.Background(), executor, second.Receipt); !errors.Is(err, ErrStateDrift) {
		t.Fatalf("overlapping composite receipt error=%v", err)
	}
	close(executor.release)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if executor.writes.Load() != 1 {
		t.Fatalf("writes=%d", executor.writes.Load())
	}
}

func TestSemanticMutationScopeBlocksDifferentVerificationShapes(t *testing.T) {
	bulkQuery, _ := appleads.ResourceQuery("keywords", map[string]any{
		"filters": []any{
			map[string]any{"field": "campaignId", "operator": "EQUALS", "value": 100},
			map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 10},
		},
		"pagination": map[string]any{"offset": 0, "pageSize": 200, "fetchTotalCount": true},
	})
	genericQuery, _ := appleads.ResourceQuery("keywords", map[string]any{
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 10}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	directGet, _ := appleads.ResourceGet("keywords", "501")
	genericMutation, _ := appleads.ResourceCreate("keywords", map[string]any{"adGroupId": "10", "text": "first", "matchType": "EXACT"})
	directMutation, _ := appleads.ResourceUpdate("keywords", "501", map[string]any{"status": "PAUSED"})
	scope := MutationScope("inventory", "keywords", "100", "10")
	for _, test := range []struct {
		name          string
		firstRead     appleads.Operation
		firstMutation appleads.Operation
	}{
		{name: "generic create and bulk create", firstRead: genericQuery, firstMutation: genericMutation},
		{name: "direct get and bulk", firstRead: directGet, firstMutation: directMutation},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewStore()
			executor := &dispatchBarrierExecutor{state: []any{}, started: make(chan struct{}, 1), release: make(chan struct{})}
			bulkMutation, _ := appleads.BulkResource("keywords", "create", map[string]any{"items": []any{}})
			first, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "generic", nil, map[string]any{"text": "first"}, []VerificationRead{{Name: "generic", Operation: test.firstRead, Scopes: []string{scope}}}, test.firstMutation, PreviewOptions{})
			if err != nil {
				t.Fatal(err)
			}
			second, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, []VerificationRead{{Name: "bulk", Operation: bulkQuery, Scopes: []string{scope}}}, bulkMutation, PreviewOptions{})
			if err != nil {
				t.Fatal(err)
			}
			firstResult := make(chan error, 1)
			go func() {
				_, applyErr := store.Apply(context.Background(), executor, first.Receipt)
				firstResult <- applyErr
			}()
			<-executor.started
			if _, err := store.Apply(context.Background(), executor, second.Receipt); !errors.Is(err, ErrStateDrift) {
				t.Fatalf("overlapping semantic receipt error=%v", err)
			}
			close(executor.release)
			if err := <-firstResult; err != nil {
				t.Fatal(err)
			}
			if executor.writes.Load() != 1 {
				t.Fatalf("writes=%d", executor.writes.Load())
			}
		})
	}
}

func TestReceiptHashIncludesPaginationAndRejectsIncompleteInventory(t *testing.T) {
	t.Run("pagination metadata", func(t *testing.T) {
		store := NewStore()
		executor := &fakeExecutor{
			state:      []any{map[string]any{"id": "1"}},
			pagination: appleads.Pagination{Offset: 0, PageSize: 200, Total: 1},
		}
		verify, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"offset": 0, "pageSize": 200}})
		mutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "voice"})
		preview, err := store.Preview(context.Background(), executor, "owner", "456", "create", nil, map[string]any{"text": "voice"}, verify, mutation)
		if err != nil {
			t.Fatal(err)
		}
		executor.pagination.Total = 2
		if _, err := store.Apply(context.Background(), executor, preview.Receipt); !errors.Is(err, ErrStateDrift) {
			t.Fatalf("pagination-only drift error=%v", err)
		}
		if executor.writes != 0 {
			t.Fatalf("writes=%d", executor.writes)
		}
	})

	t.Run("page 200 grows to 201", func(t *testing.T) {
		store := NewStore()
		items := make([]any, 200)
		for index := range items {
			items[index] = map[string]any{"id": fmt.Sprint(index + 1)}
		}
		executor := &fakeExecutor{
			state:      items,
			pagination: appleads.Pagination{Offset: 0, PageSize: 200, Total: 200},
		}
		verify, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"offset": 0, "pageSize": 200}})
		mutation, _ := appleads.ResourceCreate("keywords", map[string]any{"text": "voice"})
		preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "create", nil, map[string]any{"text": "voice"}, []VerificationRead{{Name: "inventory", Operation: verify, RequireComplete: true}}, mutation, PreviewOptions{})
		if err != nil {
			t.Fatal(err)
		}
		executor.pagination = appleads.Pagination{Offset: 0, PageSize: 200, Total: 201, Next: "offset:200"}
		if _, err := store.Apply(context.Background(), executor, preview.Receipt); !errors.Is(err, ErrStateDrift) {
			t.Fatalf("incomplete inventory error=%v", err)
		}
		if executor.writes != 0 {
			t.Fatalf("writes=%d", executor.writes)
		}
	})
}

func TestBulkVerificationReadsReturnedIDsAfterInventoryGrowsPastOnePage(t *testing.T) {
	store := NewStore()
	initial := make([]any, 190)
	for index := range initial {
		initial[index] = map[string]any{"id": fmt.Sprint(index + 1)}
	}
	executor := &fakeExecutor{
		state:      initial,
		pagination: appleads.Pagination{Offset: 0, PageSize: 200, Total: 190},
		mutationData: map[string]any{"items": []any{
			map[string]any{"correlationId": "0", "success": true, "result": map[string]any{"id": "501"}},
		}},
		readStates: map[string]any{},
	}
	query, _ := appleads.ResourceQuery("keywords", map[string]any{"pagination": map[string]any{"offset": 0, "pageSize": 200, "fetchTotalCount": true}})
	mutation, _ := appleads.BulkResource("keywords", "create", map[string]any{"items": []any{}})
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "bulk", nil, map[string]any{"items": []any{}}, []VerificationRead{{Name: "inventory", Operation: query, RequireComplete: true}}, mutation, PreviewOptions{Items: []OperationItemPreview{{
		CorrelationID: "0", Resource: "keywords", Action: "create", After: map[string]any{"text": "voice", "status": "ENABLED"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err != nil {
		t.Fatal(err)
	}
	grown := make([]any, 200)
	copy(grown, initial)
	for index := len(initial); index < len(grown); index++ {
		grown[index] = map[string]any{"id": fmt.Sprint(index + 1)}
	}
	executor.state = grown
	executor.pagination = appleads.Pagination{Offset: 0, PageSize: 200, Total: 218, Next: "offset:200"}
	executor.readStates["keywords/501"] = map[string]any{"id": "501", "text": "voice", "status": "ENABLED"}
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || verification.Status != "verified" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
	for _, object := range verification.Objects {
		if object.Name == "item_0" && object.Status == "matched" {
			return
		}
	}
	t.Fatalf("direct item verification missing: %+v", verification.Objects)
}

func TestExpiredReceiptTombstonesKeepFullTTLUnderCapacityPressure(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store := NewStoreForTest(func() time.Time { return now }, time.Second)
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	oldestReceipt := ""
	for index := 0; index < maxStoredReceiptStates; index++ {
		preview, err := store.Preview(context.Background(), executor, "owner", "456", "pause", nil, map[string]any{"status": "PAUSED"}, verify, mutation)
		if err != nil {
			t.Fatalf("preview %d: %v", index, err)
		}
		if index == 0 {
			oldestReceipt = preview.Receipt
		}
		now = now.Add(2 * time.Second)
		if _, _, err := store.Inspect(preview.Receipt); !errors.Is(err, ErrReceiptExpired) {
			t.Fatalf("expire %d: %v", index, err)
		}
	}
	if _, err := store.Preview(context.Background(), executor, "owner", "456", "pause", nil, map[string]any{"status": "PAUSED"}, verify, mutation); err == nil || !strings.Contains(err.Error(), "receipt capacity reached") {
		t.Fatalf("capacity error=%v", err)
	}
	if _, _, err := store.Inspect(oldestReceipt); !errors.Is(err, ErrReceiptExpired) {
		t.Fatalf("oldest tombstone was evicted early: %v", err)
	}
}

func TestApplyPreflightFailsBeforeWriteAndConsumesReceipt(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"status": "ENABLED"}}
	verify, _ := appleads.ResourceGet("campaigns", "123")
	mutation, _ := appleads.ResourceUpdate("campaigns", "123", map[string]any{"status": "PAUSED"})
	preview, err := store.Preview(context.Background(), executor, "owner", "456", "pause", []string{"123"}, map[string]any{"status": "PAUSED"}, verify, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyWithPreflight(context.Background(), executor, preview.Receipt, func(OperationPreview) error { return errors.New("disk unavailable") }); err == nil {
		t.Fatal("expected preflight persistence failure")
	}
	if executor.writes != 0 {
		t.Fatalf("writes=%d", executor.writes)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err == nil {
		t.Fatal("preflight-failed receipt must be consumed")
	}
}

func TestCreateVerificationReadsReturnedIDAndMatchesExpectedFields(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"result": []any{}}, mutationData: map[string]any{"id": "900"}}
	query, _ := appleads.ResourceQuery("campaigns", map[string]any{"pagination": map[string]any{"pageSize": 200}})
	mutation, _ := appleads.ResourceCreate("campaigns", map[string]any{"name": "fixture", "status": "PAUSED"})
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "create", nil, map[string]any{"name": "fixture", "status": "PAUSED"}, []VerificationRead{{Name: "inventory", Operation: query}}, mutation, PreviewOptions{Create: &CreateExpectation{Resource: "campaigns", Expected: map[string]any{"name": "fixture", "status": "PAUSED"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); err != nil {
		t.Fatal(err)
	}
	executor.state = map[string]any{"id": "900", "name": "fixture", "status": "PAUSED"}
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || verification.Status != "verified" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
	found := false
	for _, object := range verification.Objects {
		if object.Name == "created_target" && object.Status == "matched" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verification=%+v", verification)
	}
}

func TestAmbiguousCreateWithoutReturnedIDRemainsInconclusive(t *testing.T) {
	store := NewStore()
	executor := &fakeExecutor{state: map[string]any{"result": []any{}}, ambiguous: true}
	query, _ := appleads.ResourceQuery("campaigns", map[string]any{"pagination": map[string]any{"pageSize": 200}})
	mutation, _ := appleads.ResourceCreate("campaigns", map[string]any{"name": "fixture", "status": "PAUSED"})
	preview, err := store.PreviewComposite(context.Background(), executor, "owner", "456", "create", nil, map[string]any{"name": "fixture", "status": "PAUSED"}, []VerificationRead{{Name: "inventory", Operation: query}}, mutation, PreviewOptions{Create: &CreateExpectation{Resource: "campaigns", Expected: map[string]any{"name": "fixture", "status": "PAUSED"}}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := store.Apply(context.Background(), executor, preview.Receipt); err != nil || receipt.Status != "unknown" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	executor.ambiguous = false
	executor.state = map[string]any{"result": []any{map[string]any{"id": "901", "name": "fixture", "status": "PAUSED"}}}
	executor.readStates = map[string]any{"campaigns/901": map[string]any{"id": "901", "name": "fixture", "status": "PAUSED"}}
	verification, err := store.Verify(context.Background(), executor, preview.Receipt)
	if err != nil || verification.Status != "inconclusive" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
}

func TestVerifyRecoveryMatchesBeforeAndAfterWithoutStoredReceipt(t *testing.T) {
	executor := &fakeExecutor{readStates: map[string]any{
		"campaigns/100": map[string]any{"id": "100", "status": "PAUSED"},
		"keywords/200":  map[string]any{"id": "200", "bid": map[string]any{"amount": "1.00", "currency": "USD"}},
	}}
	verification, err := VerifyRecovery(context.Background(), executor, "opaque-receipt", "owner", "456", []RecoveryItem{
		{CorrelationID: "pause", CampaignID: "100", ResourceType: "campaign", Resource: "campaigns", TargetID: "100", Action: "pause", Before: map[string]any{"status": "ENABLED"}, After: map[string]any{"status": "PAUSED"}},
		{CorrelationID: "bid", CampaignID: "100", ResourceType: "keyword", Resource: "keywords", TargetID: "200", Action: "bid_increase", Before: map[string]any{"bid": map[string]any{"amount": "1.00", "currency": "USD"}}, After: map[string]any{"bid": map[string]any{"amount": "1.10", "currency": "USD"}}},
	})
	if err != nil || verification.Status != "verified" || len(verification.Objects) != 2 {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
	if verification.Objects[0].Status != "matched_after" || verification.Objects[1].Status != "matched_before" {
		t.Fatalf("objects=%+v", verification.Objects)
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
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); !errors.Is(err, ErrStateDrift) {
		t.Fatalf("expected ordered array drift rejection, got %v", err)
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
