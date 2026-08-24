package operations

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

type Executor interface {
	Do(context.Context, string, string, appleads.Operation) (appleads.Result, error)
}

type OperationPreview struct {
	Receipt       string         `json:"receipt"`
	ExpiresAt     string         `json:"expiresAt"`
	Profile       string         `json:"profile"`
	AdAccountID   string         `json:"adAccountId"`
	Operation     string         `json:"operation"`
	TargetIDs     []string       `json:"targetIds,omitempty"`
	Before        any            `json:"before,omitempty"`
	After         map[string]any `json:"after"`
	Diff          map[string]any `json:"diff"`
	CurrentHash   string         `json:"currentStateHash"`
	PayloadHash   string         `json:"payloadHash"`
	RequiresApply bool           `json:"requiresApply"`
}

type OperationReceipt struct {
	Receipt      string           `json:"receipt"`
	Status       string           `json:"status"`
	Operation    string           `json:"operation"`
	Profile      string           `json:"profile"`
	AdAccountID  string           `json:"adAccountId"`
	AppliedAt    string           `json:"appliedAt,omitempty"`
	Result       *appleads.Result `json:"result,omitempty"`
	Verification string           `json:"verification,omitempty"`
}

type OperationVerification struct {
	Receipt      string `json:"receipt"`
	Status       string `json:"status"`
	Used         bool   `json:"used"`
	Current      any    `json:"current,omitempty"`
	CurrentHash  string `json:"currentStateHash"`
	PreviewHash  string `json:"previewStateHash"`
	ExpectedDiff any    `json:"expectedDiff"`
}

type record struct {
	preview  OperationPreview
	verify   appleads.Operation
	mutation appleads.Operation
	used     bool
	size     int
}

type Store struct {
	mu      sync.Mutex
	records map[string]*record
	now     func() time.Time
	ttl     time.Duration
	total   int
}

const (
	maxStoredReceipts    = 1000
	maxStoredReceiptData = 32 << 20
)

func NewStore() *Store {
	return &Store{records: make(map[string]*record), now: time.Now, ttl: 10 * time.Minute}
}

func NewStoreForTest(now func() time.Time, ttl time.Duration) *Store {
	return &Store{records: make(map[string]*record), now: now, ttl: ttl}
}

func (s *Store) Preview(ctx context.Context, executor Executor, profile, adAccountID, name string, targetIDs []string, payload map[string]any, verify, mutation appleads.Operation) (OperationPreview, error) {
	if !mutation.IsMutation() {
		return OperationPreview{}, errors.New("preview requires a mutation operation")
	}
	current, err := executor.Do(ctx, profile, adAccountID, verify)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("read current state: %w", err)
	}
	currentHash, err := valueHash(current.Data)
	if err != nil {
		return OperationPreview{}, err
	}
	payloadHash, err := valueHash(payload)
	if err != nil {
		return OperationPreview{}, err
	}
	currentData, err := json.Marshal(current.Data)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("size operation state: %w", err)
	}
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("size operation payload: %w", err)
	}
	recordSize := len(currentData)*2 + len(payloadData)*4 + 4096
	receipt, err := randomReceipt()
	if err != nil {
		return OperationPreview{}, err
	}
	preview := OperationPreview{
		Receipt:       receipt,
		ExpiresAt:     s.now().Add(s.ttl).UTC().Format(time.RFC3339),
		Profile:       profile,
		AdAccountID:   adAccountID,
		Operation:     name,
		TargetIDs:     append([]string(nil), targetIDs...),
		Before:        current.Data,
		After:         cloneMap(payload),
		Diff:          map[string]any{"set": cloneMap(payload)},
		CurrentHash:   currentHash,
		PayloadHash:   payloadHash,
		RequiresApply: true,
	}
	s.mu.Lock()
	s.pruneExpiredLocked()
	if len(s.records) >= maxStoredReceipts || recordSize > maxStoredReceiptData-s.total {
		s.mu.Unlock()
		return OperationPreview{}, errors.New("receipt capacity reached; wait for existing previews to expire")
	}
	s.records[receipt] = &record{preview: preview, verify: verify, mutation: mutation, size: recordSize}
	s.total += recordSize
	s.mu.Unlock()
	return preview, nil
}

func (s *Store) pruneExpiredLocked() {
	now := s.now()
	for receipt, item := range s.records {
		expiresAt, err := time.Parse(time.RFC3339, item.preview.ExpiresAt)
		if err != nil || !now.Before(expiresAt) {
			s.total -= item.size
			delete(s.records, receipt)
		}
	}
}

func (s *Store) Apply(ctx context.Context, executor Executor, receipt string) (OperationReceipt, error) {
	s.mu.Lock()
	record, ok := s.records[receipt]
	if !ok {
		s.mu.Unlock()
		return OperationReceipt{}, errors.New("receipt not found")
	}
	if record.used {
		s.mu.Unlock()
		return OperationReceipt{}, errors.New("receipt has already been used")
	}
	expiresAt, err := time.Parse(time.RFC3339, record.preview.ExpiresAt)
	if err != nil || !s.now().Before(expiresAt) {
		s.mu.Unlock()
		return OperationReceipt{}, errors.New("receipt has expired")
	}
	if deadline, ok := ctx.Deadline(); !ok || expiresAt.Before(deadline) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, expiresAt)
		defer cancel()
	}
	preview := record.preview
	verify := record.verify
	mutation := record.mutation
	s.mu.Unlock()

	current, err := executor.Do(ctx, preview.Profile, preview.AdAccountID, verify)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("re-read current state: %w", err)
	}
	currentHash, err := valueHash(current.Data)
	if err != nil {
		return OperationReceipt{}, err
	}
	if currentHash != preview.CurrentHash {
		return OperationReceipt{}, errors.New("current state drifted after preview; create a new preview")
	}

	s.mu.Lock()
	if record.used {
		s.mu.Unlock()
		return OperationReceipt{}, errors.New("receipt has already been used")
	}
	if !s.now().Before(expiresAt) {
		s.mu.Unlock()
		return OperationReceipt{}, errors.New("receipt has expired")
	}
	record.used = true
	s.mu.Unlock()

	result, err := executor.Do(ctx, preview.Profile, preview.AdAccountID, mutation)
	receiptResult := OperationReceipt{
		Receipt:     receipt,
		Operation:   preview.Operation,
		Profile:     preview.Profile,
		AdAccountID: preview.AdAccountID,
		AppliedAt:   s.now().UTC().Format(time.RFC3339),
	}
	if err != nil {
		var ambiguous *appleads.AmbiguousWriteError
		if errors.As(err, &ambiguous) {
			receiptResult.Status = "unknown"
			receiptResult.Verification = "committed_unverified"
			return receiptResult, nil
		}
		return OperationReceipt{}, err
	}
	receiptResult.Status = "applied"
	receiptResult.Verification = "response_received"
	receiptResult.Result = &result
	return receiptResult, nil
}

func (s *Store) Inspect(receipt string) (OperationPreview, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[receipt]
	if !ok {
		return OperationPreview{}, false, errors.New("receipt not found")
	}
	return record.preview, record.used, nil
}

func (s *Store) Verify(ctx context.Context, executor Executor, receipt string) (OperationVerification, error) {
	s.mu.Lock()
	record, ok := s.records[receipt]
	if !ok {
		s.mu.Unlock()
		return OperationVerification{}, errors.New("receipt not found")
	}
	preview := record.preview
	verify := record.verify
	used := record.used
	s.mu.Unlock()
	current, err := executor.Do(ctx, preview.Profile, preview.AdAccountID, verify)
	if err != nil {
		return OperationVerification{}, fmt.Errorf("read current state for verification: %w", err)
	}
	hash, err := valueHash(current.Data)
	if err != nil {
		return OperationVerification{}, err
	}
	status := "inconclusive"
	if len(preview.TargetIDs) > 0 {
		status = "changed"
		if hash == preview.CurrentHash {
			status = "unchanged"
		}
	}
	return OperationVerification{
		Receipt: receipt, Status: status, Used: used, Current: current.Data,
		CurrentHash: hash, PreviewHash: preview.CurrentHash, ExpectedDiff: preview.Diff,
	}, nil
}

func valueHash(value any) (string, error) {
	canonical, err := canonicalizeForHash(value, "")
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("hash operation state: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalizeForHash(value any, field string) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			canonical, err := canonicalizeForHash(item, key)
			if err != nil {
				return nil, err
			}
			result[key] = canonical
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		items := make([]struct {
			value any
			data  []byte
		}, len(typed))
		for index, item := range typed {
			canonical, err := canonicalizeForHash(item, "")
			if err != nil {
				return nil, err
			}
			result[index] = canonical
			if !unorderedHashField(field) {
				continue
			}
			data, err := json.Marshal(canonical)
			if err != nil {
				return nil, fmt.Errorf("hash operation state: %w", err)
			}
			items[index] = struct {
				value any
				data  []byte
			}{value: canonical, data: data}
		}
		if !unorderedHashField(field) {
			return result, nil
		}
		sort.SliceStable(items, func(i, j int) bool {
			return bytes.Compare(items[i].data, items[j].data) < 0
		})
		for index, item := range items {
			result[index] = item.value
		}
		return result, nil
	default:
		return value, nil
	}
}

func unorderedHashField(field string) bool {
	switch field {
	case "systemStatusReasons", "countries", "countryOrRegionCodes":
		return true
	default:
		return false
	}
}

func randomReceipt() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create operation receipt: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
