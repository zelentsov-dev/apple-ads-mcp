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
	"strings"
	"sync"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

type Executor interface {
	Do(context.Context, string, string, appleads.Operation) (appleads.Result, error)
}

type OperationPreview struct {
	Receipt       string                 `json:"receipt"`
	ExpiresAt     string                 `json:"expiresAt"`
	Profile       string                 `json:"profile"`
	AdAccountID   string                 `json:"adAccountId"`
	Operation     string                 `json:"operation"`
	TargetIDs     []string               `json:"targetIds,omitempty"`
	Before        any                    `json:"before,omitempty"`
	After         map[string]any         `json:"after"`
	Diff          map[string]any         `json:"diff"`
	CurrentHash   string                 `json:"currentStateHash"`
	PayloadHash   string                 `json:"payloadHash"`
	RequiresApply bool                   `json:"requiresApply"`
	Impact        *OperationImpact       `json:"impact,omitempty"`
	Items         []OperationItemPreview `json:"items,omitempty"`
}

type OperationImpact struct {
	SpendAffecting bool            `json:"spendAffecting"`
	Destructive    bool            `json:"destructive,omitempty"`
	Placement      string          `json:"placement,omitempty"`
	ParentIDs      []string        `json:"parentIds,omitempty"`
	ObjectCount    int             `json:"objectCount"`
	Currency       string          `json:"currency,omitempty"`
	MaximumAmount  *appleads.Money `json:"maximumAmount,omitempty"`
	Policy         string          `json:"policy,omitempty"`
	PrivateHash    string          `json:"privatePayloadHash,omitempty"`
}

type OperationItemPreview struct {
	CorrelationID string         `json:"correlationId"`
	CampaignID    string         `json:"campaignId,omitempty"`
	ResourceType  string         `json:"resourceType,omitempty"`
	Resource      string         `json:"resource,omitempty"`
	Action        string         `json:"action,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	TargetID      string         `json:"targetId,omitempty"`
	Before        any            `json:"before,omitempty"`
	After         map[string]any `json:"after"`
	DependsOn     []string       `json:"dependsOn,omitempty"`
}

type OperationItemStatus struct {
	CorrelationID string `json:"correlationId"`
	CampaignID    string `json:"campaignId,omitempty"`
	ResourceType  string `json:"resourceType,omitempty"`
	Action        string `json:"action,omitempty"`
	TargetID      string `json:"targetId,omitempty"`
	Status        string `json:"status"`
	Error         any    `json:"error,omitempty"`
}

type OperationReceipt struct {
	Receipt      string                `json:"receipt"`
	Status       string                `json:"status"`
	Operation    string                `json:"operation"`
	Profile      string                `json:"profile"`
	AdAccountID  string                `json:"adAccountId"`
	AppliedAt    string                `json:"appliedAt,omitempty"`
	Result       *appleads.Result      `json:"result,omitempty"`
	Verification string                `json:"verification,omitempty"`
	Items        []OperationItemStatus `json:"items,omitempty"`
}

type OperationVerification struct {
	Receipt      string               `json:"receipt"`
	Status       string               `json:"status"`
	Used         bool                 `json:"used"`
	Current      any                  `json:"current,omitempty"`
	CurrentHash  string               `json:"currentStateHash"`
	PreviewHash  string               `json:"previewStateHash"`
	ExpectedDiff any                  `json:"expectedDiff"`
	Objects      []ObjectVerification `json:"objects,omitempty"`
}

type ObjectVerification struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Current any    `json:"current,omitempty"`
}

type RecoveryItem struct {
	CorrelationID string         `json:"correlationId"`
	CampaignID    string         `json:"campaignId,omitempty"`
	ResourceType  string         `json:"resourceType"`
	Resource      string         `json:"resource"`
	TargetID      string         `json:"targetId"`
	Action        string         `json:"action"`
	Before        map[string]any `json:"before,omitempty"`
	After         map[string]any `json:"after"`
}

type VerificationRead struct {
	Name            string
	Operation       appleads.Operation
	ExpectDeleted   bool
	RequireComplete bool
	Scopes          []string
}

type PreviewOptions struct {
	Impact *OperationImpact
	Items  []OperationItemPreview
	Create *CreateExpectation
}

type CreateExpectation struct {
	Resource string
	Expected map[string]any
}

type SequenceStep struct {
	Item     OperationItemPreview
	Mutation appleads.Operation
}

type record struct {
	preview  OperationPreview
	verify   []VerificationRead
	scopes   map[string]uint64
	mutation appleads.Operation
	sequence []SequenceStep
	create   *CreateExpectation
	outcomes map[string]string
	used     bool
	size     int
}

type scopeState struct {
	generation uint64
	references int
	reservedBy string
}

type Store struct {
	mu         sync.Mutex
	records    map[string]*record
	tombstones map[string]time.Time
	scopes     map[string]*scopeState
	now        func() time.Time
	ttl        time.Duration
	total      int
}

const (
	maxStoredReceiptStates = 1000
	maxStoredReceiptData   = 32 << 20
	receiptTombstoneTTL    = time.Hour
)

var (
	ErrReceiptNotFound        = errors.New("receipt not found")
	ErrReceiptExpired         = errors.New("receipt has expired")
	ErrReceiptUsed            = errors.New("receipt has already been used")
	ErrStateDrift             = errors.New("current state drifted after preview; create a new preview")
	ErrVerificationIncomplete = errors.New("verification inventory is incomplete")
)

func NewStore() *Store {
	return &Store{records: make(map[string]*record), tombstones: make(map[string]time.Time), scopes: make(map[string]*scopeState), now: time.Now, ttl: 10 * time.Minute}
}

func NewStoreForTest(now func() time.Time, ttl time.Duration) *Store {
	return &Store{records: make(map[string]*record), tombstones: make(map[string]time.Time), scopes: make(map[string]*scopeState), now: now, ttl: ttl}
}

func (s *Store) Preview(ctx context.Context, executor Executor, profile, adAccountID, name string, targetIDs []string, payload map[string]any, verify, mutation appleads.Operation) (OperationPreview, error) {
	return s.PreviewComposite(ctx, executor, profile, adAccountID, name, targetIDs, payload, []VerificationRead{{Name: "current", Operation: verify}}, mutation, PreviewOptions{})
}

func (s *Store) PreviewComposite(ctx context.Context, executor Executor, profile, adAccountID, name string, targetIDs []string, payload map[string]any, verify []VerificationRead, mutation appleads.Operation, options PreviewOptions) (OperationPreview, error) {
	if !mutation.IsMutation() {
		return OperationPreview{}, errors.New("preview requires a mutation operation")
	}
	if len(verify) == 0 {
		return OperationPreview{}, errors.New("preview requires at least one verification read")
	}
	scopeKeys, err := verificationScopeKeys(adAccountID, targetIDs, verify)
	if err != nil {
		return OperationPreview{}, err
	}
	s.mu.Lock()
	s.pruneExpiredLocked()
	if len(s.records)+len(s.tombstones) >= maxStoredReceiptStates {
		s.mu.Unlock()
		return OperationPreview{}, errors.New("receipt capacity reached; wait for existing previews or expired receipt tombstones to expire")
	}
	if !s.scopesAvailableLocked(scopeKeys) {
		s.mu.Unlock()
		return OperationPreview{}, ErrStateDrift
	}
	scopeGenerations := s.scopeGenerationsLocked(scopeKeys)
	s.mu.Unlock()

	hashState, current, _, err := readVerificationState(ctx, executor, profile, adAccountID, verify, true)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("read current state: %w", err)
	}
	currentHash, err := valueHash(hashState)
	if err != nil {
		return OperationPreview{}, err
	}
	payloadHash, err := valueHash(payload)
	if err != nil {
		return OperationPreview{}, err
	}
	currentData, err := json.Marshal(hashState)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("size operation state: %w", err)
	}
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("size operation payload: %w", err)
	}
	mutationSize, err := mutation.EncodedBodySize()
	if err != nil {
		return OperationPreview{}, err
	}
	recordSize := len(currentData)*2 + len(payloadData)*4 + mutationSize + 4096
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
		Before:        redactPrivateData(current),
		After:         cloneMap(payload),
		Diff:          map[string]any{"set": cloneMap(payload)},
		CurrentHash:   currentHash,
		PayloadHash:   payloadHash,
		RequiresApply: true,
		Impact:        options.Impact,
		Items:         append([]OperationItemPreview(nil), options.Items...),
	}
	s.mu.Lock()
	s.pruneExpiredLocked()
	if !s.scopeGenerationsMatchLocked(scopeGenerations) || !s.scopesAvailableLocked(scopeKeys) {
		s.mu.Unlock()
		return OperationPreview{}, ErrStateDrift
	}
	if len(s.records)+len(s.tombstones) >= maxStoredReceiptStates || recordSize > maxStoredReceiptData-s.total {
		s.mu.Unlock()
		return OperationPreview{}, errors.New("receipt capacity reached; wait for existing previews or expired receipt tombstones to expire")
	}
	s.retainScopesLocked(scopeGenerations)
	s.records[receipt] = &record{preview: preview, verify: append([]VerificationRead(nil), verify...), scopes: scopeGenerations, mutation: mutation, create: cloneCreateExpectation(options.Create), size: recordSize}
	s.total += recordSize
	s.mu.Unlock()
	return preview, nil
}

func (s *Store) PreviewSequence(ctx context.Context, executor Executor, profile, adAccountID, name string, targetIDs []string, verify []VerificationRead, steps []SequenceStep, impact *OperationImpact) (OperationPreview, error) {
	if len(steps) == 0 || len(steps) > 100 {
		return OperationPreview{}, errors.New("sequence must contain 1 to 100 mutation steps")
	}
	if len(verify) == 0 {
		return OperationPreview{}, errors.New("sequence preview requires at least one verification read")
	}
	items := make([]OperationItemPreview, 0, len(steps))
	payloadItems := make([]any, 0, len(steps))
	seen := map[string]struct{}{}
	sequenceSize := 0
	for _, step := range steps {
		if !step.Mutation.IsMutation() {
			return OperationPreview{}, errors.New("sequence contains a non-mutation operation")
		}
		bodySize, err := step.Mutation.EncodedBodySize()
		if err != nil {
			return OperationPreview{}, err
		}
		sequenceSize += bodySize
		if strings.TrimSpace(step.Item.CorrelationID) == "" {
			return OperationPreview{}, errors.New("sequence correlationId is required")
		}
		if _, exists := seen[step.Item.CorrelationID]; exists {
			return OperationPreview{}, fmt.Errorf("duplicate sequence correlationId %q", step.Item.CorrelationID)
		}
		seen[step.Item.CorrelationID] = struct{}{}
		items = append(items, step.Item)
		payloadItems = append(payloadItems, map[string]any{
			"correlationId": step.Item.CorrelationID,
			"campaignId":    step.Item.CampaignID,
			"resourceType":  step.Item.ResourceType,
			"action":        step.Item.Action,
			"reason":        step.Item.Reason,
			"targetId":      step.Item.TargetID,
			"after":         step.Item.After,
			"dependsOn":     step.Item.DependsOn,
		})
	}
	for _, item := range items {
		for _, dependency := range item.DependsOn {
			if _, exists := seen[dependency]; !exists {
				return OperationPreview{}, fmt.Errorf("sequence dependency %q does not exist", dependency)
			}
		}
	}
	payload := map[string]any{"actions": payloadItems}
	placeholder, _ := appleads.ResourceUpdate("campaigns", "sequence-placeholder", map[string]any{"status": "PAUSED"})
	preview, err := s.PreviewComposite(ctx, executor, profile, adAccountID, name, targetIDs, payload, verify, placeholder, PreviewOptions{Impact: impact, Items: items})
	if err != nil {
		return OperationPreview{}, err
	}
	s.mu.Lock()
	if item, exists := s.records[preview.Receipt]; exists {
		if sequenceSize > maxStoredReceiptData-s.total {
			s.deleteRecordLocked(preview.Receipt, item)
			s.mu.Unlock()
			return OperationPreview{}, errors.New("receipt capacity reached; sequence payload is too large")
		}
		item.sequence = append([]SequenceStep(nil), steps...)
		item.size += sequenceSize
		s.total += sequenceSize
	}
	s.mu.Unlock()
	return preview, nil
}

func (s *Store) pruneExpiredLocked() {
	now := s.now()
	for receipt, expiredAt := range s.tombstones {
		if !now.Before(expiredAt.Add(receiptTombstoneTTL)) {
			delete(s.tombstones, receipt)
		}
	}
	for receipt, item := range s.records {
		expiresAt, err := time.Parse(time.RFC3339, item.preview.ExpiresAt)
		if err != nil || !now.Before(expiresAt) {
			s.deleteRecordLocked(receipt, item)
			s.addExpiredTombstoneLocked(receipt, now)
		}
	}
}

func (s *Store) addExpiredTombstoneLocked(receipt string, expiredAt time.Time) {
	if s.tombstones == nil {
		s.tombstones = make(map[string]time.Time)
	}
	s.tombstones[receipt] = expiredAt
}

func (s *Store) deleteRecordLocked(receipt string, item *record) {
	s.total -= item.size
	delete(s.records, receipt)
	s.clearScopesReservationLocked(item.scopes, receipt)
	s.releaseScopesLocked(item.scopes)
}

func verificationScopeKeys(adAccountID string, targetIDs []string, reads []VerificationRead) ([]string, error) {
	seen := make(map[string]struct{}, len(reads)+len(targetIDs))
	keys := make([]string, 0, len(reads)+len(targetIDs))
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		key := adAccountID + "\x00semantic\x00" + ObjectMutationScope(targetID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for _, read := range reads {
		if read.Operation.IsMutation() {
			return nil, errors.New("verification scope requires read operations")
		}
		if len(read.Scopes) > 0 {
			for _, scope := range read.Scopes {
				scope = strings.TrimSpace(scope)
				if scope == "" {
					return nil, errors.New("verification scope must not be empty")
				}
				key := adAccountID + "\x00semantic\x00" + scope
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
		fingerprint, err := read.Operation.VerificationScopeKey()
		if err != nil {
			return nil, err
		}
		key := adAccountID + "\x00read\x00" + fingerprint
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func MutationScope(parts ...string) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func ObjectMutationScope(id string) string {
	return MutationScope("object", strings.TrimSpace(id))
}

func (s *Store) scopeGenerationsLocked(keys []string) map[string]uint64 {
	result := make(map[string]uint64, len(keys))
	for _, key := range keys {
		if state := s.scopes[key]; state != nil {
			result[key] = state.generation
			continue
		}
		result[key] = 0
	}
	return result
}

func (s *Store) scopeGenerationsMatchLocked(expected map[string]uint64) bool {
	for key, generation := range expected {
		current := uint64(0)
		if state := s.scopes[key]; state != nil {
			current = state.generation
		}
		if current != generation {
			return false
		}
	}
	return true
}

func (s *Store) scopesAvailableLocked(keys []string) bool {
	for _, key := range keys {
		if state := s.scopes[key]; state != nil && state.reservedBy != "" {
			return false
		}
	}
	return true
}

func (s *Store) retainScopesLocked(scopes map[string]uint64) {
	if s.scopes == nil {
		s.scopes = make(map[string]*scopeState)
	}
	for key, generation := range scopes {
		state := s.scopes[key]
		if state == nil {
			state = &scopeState{generation: generation}
			s.scopes[key] = state
		}
		state.references++
	}
}

func (s *Store) releaseScopesLocked(scopes map[string]uint64) {
	for key := range scopes {
		state := s.scopes[key]
		if state == nil {
			continue
		}
		state.references--
		if state.references == 0 {
			delete(s.scopes, key)
		}
	}
}

func (s *Store) reserveScopesLocked(expected map[string]uint64, receipt string) bool {
	if !s.scopeGenerationsMatchLocked(expected) {
		return false
	}
	for key := range expected {
		state := s.scopes[key]
		if state == nil || state.reservedBy != "" {
			return false
		}
	}
	for key := range expected {
		state := s.scopes[key]
		state.generation++
		state.reservedBy = receipt
	}
	return true
}

func (s *Store) clearScopesReservationLocked(scopes map[string]uint64, receipt string) {
	for key := range scopes {
		if state := s.scopes[key]; state != nil && state.reservedBy == receipt {
			state.reservedBy = ""
		}
	}
}

func (s *Store) clearReceiptReservation(record *record, receipt string) {
	s.mu.Lock()
	s.clearScopesReservationLocked(record.scopes, receipt)
	s.mu.Unlock()
}

func (s *Store) missingReceiptErrorLocked(receipt string) error {
	if _, expired := s.tombstones[receipt]; expired {
		return ErrReceiptExpired
	}
	return ErrReceiptNotFound
}

func (s *Store) Apply(ctx context.Context, executor Executor, receipt string) (OperationReceipt, error) {
	return s.ApplyWithPreflight(ctx, executor, receipt, nil)
}

func (s *Store) ApplyWithPreflight(ctx context.Context, executor Executor, receipt string, preflight func(OperationPreview) error) (OperationReceipt, error) {
	s.mu.Lock()
	s.pruneExpiredLocked()
	record, ok := s.records[receipt]
	if !ok {
		err := s.missingReceiptErrorLocked(receipt)
		s.mu.Unlock()
		return OperationReceipt{}, err
	}
	if record.used {
		s.mu.Unlock()
		return OperationReceipt{}, ErrReceiptUsed
	}
	if !s.scopeGenerationsMatchLocked(record.scopes) {
		s.mu.Unlock()
		return OperationReceipt{}, ErrStateDrift
	}
	expiresAt, err := time.Parse(time.RFC3339, record.preview.ExpiresAt)
	if err != nil || !s.now().Before(expiresAt) {
		s.deleteRecordLocked(receipt, record)
		s.addExpiredTombstoneLocked(receipt, s.now())
		s.mu.Unlock()
		return OperationReceipt{}, ErrReceiptExpired
	}
	if deadline, ok := ctx.Deadline(); !ok || expiresAt.Before(deadline) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, expiresAt)
		defer cancel()
	}
	preview := record.preview
	verify := record.verify
	mutation := record.mutation
	sequence := append([]SequenceStep(nil), record.sequence...)
	s.mu.Unlock()

	current, _, _, err := readVerificationState(ctx, executor, preview.Profile, preview.AdAccountID, verify, true)
	if err != nil {
		if errors.Is(err, ErrVerificationIncomplete) {
			return OperationReceipt{}, ErrStateDrift
		}
		return OperationReceipt{}, fmt.Errorf("re-read current state: %w", err)
	}
	currentHash, err := valueHash(current)
	if err != nil {
		return OperationReceipt{}, err
	}
	if currentHash != preview.CurrentHash {
		return OperationReceipt{}, ErrStateDrift
	}

	s.mu.Lock()
	currentRecord, exists := s.records[receipt]
	if !exists || currentRecord != record {
		err := s.missingReceiptErrorLocked(receipt)
		s.mu.Unlock()
		return OperationReceipt{}, err
	}
	if record.used {
		s.mu.Unlock()
		return OperationReceipt{}, ErrReceiptUsed
	}
	if !s.now().Before(expiresAt) {
		s.deleteRecordLocked(receipt, record)
		s.addExpiredTombstoneLocked(receipt, s.now())
		s.mu.Unlock()
		return OperationReceipt{}, ErrReceiptExpired
	}
	if !s.reserveScopesLocked(record.scopes, receipt) {
		s.mu.Unlock()
		return OperationReceipt{}, ErrStateDrift
	}
	record.used = true
	s.mu.Unlock()
	if preflight != nil {
		if err := preflight(preview); err != nil {
			s.clearReceiptReservation(record, receipt)
			return OperationReceipt{}, fmt.Errorf("persist write intent: %w", err)
		}
	}

	if len(sequence) > 0 {
		result := s.applySequence(ctx, executor, receipt, preview, sequence)
		s.storeOutcomes(record, result.Items)
		if result.Status != "unknown" {
			s.clearReceiptReservation(record, receipt)
		}
		return result, nil
	}
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
			receiptResult.Items = unknownItemStatuses(preview.Items)
			s.storeOutcomes(record, receiptResult.Items)
			return receiptResult, nil
		}
		s.clearReceiptReservation(record, receipt)
		return OperationReceipt{}, err
	}
	s.clearReceiptReservation(record, receipt)
	receiptResult.Items = resultItemStatuses(result.Data, preview.Items)
	s.storeOutcomes(record, receiptResult.Items)
	if len(receiptResult.Items) > 0 {
		s.mu.Lock()
		targetIDs := make([]string, 0, len(receiptResult.Items))
		for index := range record.preview.Items {
			if index < len(receiptResult.Items) && receiptResult.Items[index].TargetID != "" {
				record.preview.Items[index].TargetID = receiptResult.Items[index].TargetID
				targetIDs = append(targetIDs, receiptResult.Items[index].TargetID)
				if resource := record.preview.Items[index].Resource; resource != "" {
					if read, readErr := appleads.ResourceGet(resource, receiptResult.Items[index].TargetID); readErr == nil {
						record.verify = appendVerificationRead(record.verify, VerificationRead{Name: "direct_item_" + record.preview.Items[index].CorrelationID, Operation: read})
					}
				}
			}
		}
		if len(targetIDs) > 0 {
			record.preview.TargetIDs = targetIDs
		}
		s.mu.Unlock()
	}
	if len(preview.TargetIDs) == 0 && len(preview.Items) == 0 {
		if createdID := findFirstResultID(result.Data); createdID != "" {
			s.mu.Lock()
			record.preview.TargetIDs = []string{createdID}
			if record.create != nil {
				if read, readErr := appleads.ResourceGet(record.create.Resource, createdID); readErr == nil {
					record.verify = append(record.verify, VerificationRead{Name: "created_target", Operation: read})
				}
			}
			s.mu.Unlock()
		}
	}
	receiptResult.Status = aggregateItemStatus(receiptResult.Items)
	if receiptResult.Status == "" {
		receiptResult.Status = "applied"
	}
	receiptResult.Verification = "response_received"
	result.Data = redactPrivateData(result.Data)
	receiptResult.Result = &result
	return receiptResult, nil
}

func (s *Store) applySequence(ctx context.Context, executor Executor, receipt string, preview OperationPreview, sequence []SequenceStep) OperationReceipt {
	result := OperationReceipt{
		Receipt: receipt, Operation: preview.Operation, Profile: preview.Profile, AdAccountID: preview.AdAccountID,
		AppliedAt: s.now().UTC().Format(time.RFC3339), Verification: "response_received",
	}
	statuses := make(map[string]string, len(sequence))
	stop := false
	for _, step := range sequence {
		status := OperationItemStatus{
			CorrelationID: step.Item.CorrelationID, CampaignID: step.Item.CampaignID,
			ResourceType: step.Item.ResourceType, Action: step.Item.Action, TargetID: step.Item.TargetID,
		}
		if stop {
			status.Status = "not_attempted"
			result.Items = append(result.Items, status)
			statuses[step.Item.CorrelationID] = status.Status
			continue
		}
		for _, dependency := range step.Item.DependsOn {
			if statuses[dependency] != "applied" {
				status.Status = "skipped"
				status.Error = "dependency was not applied"
				break
			}
		}
		if status.Status == "skipped" {
			result.Items = append(result.Items, status)
			statuses[step.Item.CorrelationID] = status.Status
			continue
		}
		response, err := executor.Do(ctx, preview.Profile, preview.AdAccountID, step.Mutation)
		if err != nil {
			var ambiguous *appleads.AmbiguousWriteError
			if errors.As(err, &ambiguous) {
				status.Status = "unknown"
				status.Error = "write outcome is unknown"
				result.Verification = "committed_unverified"
				stop = true
			} else {
				status.Status = "failed"
				status.Error = publicOperationError(err)
				var apiError *appleads.APIError
				if !errors.As(err, &apiError) || apiError.HTTPStatus < 400 || apiError.HTTPStatus >= 500 {
					stop = true
				}
			}
		} else {
			status.Status = "applied"
			if status.TargetID == "" {
				status.TargetID = findFirstResultID(response.Data)
			}
		}
		result.Items = append(result.Items, status)
		statuses[step.Item.CorrelationID] = status.Status
	}
	result.Status = aggregateItemStatus(result.Items)
	if containsItemStatus(result.Items, "unknown") {
		result.Status = "unknown"
	}
	return result
}

func publicOperationError(err error) any {
	var apiError *appleads.APIError
	if errors.As(err, &apiError) {
		return apiError
	}
	return "operation failed"
}

func containsItemStatus(items []OperationItemStatus, status string) bool {
	for _, item := range items {
		if item.Status == status {
			return true
		}
	}
	return false
}

func (s *Store) Inspect(receipt string) (OperationPreview, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked()
	record, ok := s.records[receipt]
	if !ok {
		return OperationPreview{}, false, s.missingReceiptErrorLocked(receipt)
	}
	return record.preview, record.used, nil
}

func (s *Store) Verify(ctx context.Context, executor Executor, receipt string) (OperationVerification, error) {
	s.mu.Lock()
	record, ok := s.records[receipt]
	if !ok {
		err := s.missingReceiptErrorLocked(receipt)
		s.mu.Unlock()
		return OperationVerification{}, err
	}
	preview := record.preview
	verify := record.verify
	create := cloneCreateExpectation(record.create)
	outcomes := cloneStringMap(record.outcomes)
	used := record.used
	s.mu.Unlock()
	hashState, current, objects, err := readVerificationState(ctx, executor, preview.Profile, preview.AdAccountID, verify, false)
	if err != nil {
		return OperationVerification{}, fmt.Errorf("read current state for verification: %w", err)
	}
	hash, err := valueHash(hashState)
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
	for index := range objects {
		objects[index].Current = redactPrivateData(objects[index].Current)
	}
	if create != nil {
		createdID := ""
		if len(preview.TargetIDs) == 1 {
			createdID = preview.TargetIDs[0]
		}
		var created any
		for _, object := range objects {
			if object.Name == "created_target" {
				created = object.Current
				break
			}
		}
		if created == nil && createdID != "" {
			read, readErr := appleads.ResourceGet(create.Resource, createdID)
			if readErr == nil {
				result, resultErr := executor.Do(ctx, preview.Profile, preview.AdAccountID, read)
				if resultErr == nil {
					created = result.Data
					s.mu.Lock()
					record.preview.TargetIDs = []string{createdID}
					record.verify = appendVerificationRead(record.verify, VerificationRead{Name: "created_target", Operation: read})
					s.mu.Unlock()
				}
			}
		}
		createdStatus := "inconclusive"
		if created != nil {
			createdStatus = "mismatch"
			if matchesExpectedSubset(created, create.Expected) {
				createdStatus = "matched"
			}
		}
		objects = appendOrReplaceObject(objects, ObjectVerification{Name: "created_target", Status: createdStatus, Current: redactPrivateData(created)})
		if createdStatus == "matched" {
			status = "verified"
		} else {
			status = "inconclusive"
		}
	}
	for index := range objects {
		if create != nil && objects[index].Name == "created_target" {
			continue
		}
		if current, ok := objects[index].Current.(map[string]any); ok {
			if deleted, ok := current["deleted"].(bool); ok && deleted {
				objects[index].Status = "deleted"
				continue
			}
		}
		before := preview.Before
		if values, ok := preview.Before.(map[string]any); ok && len(verify) > 1 {
			before = values[objects[index].Name]
		}
		beforeHash, beforeErr := valueHash(before)
		currentObjectHash, currentErr := valueHash(objects[index].Current)
		if beforeErr == nil && currentErr == nil && beforeHash == currentObjectHash {
			objects[index].Status = "unchanged"
		} else {
			objects[index].Status = "changed"
		}
	}
	for _, object := range objects {
		if object.Name == "target" && object.Status == "deleted" {
			status = "deleted"
			break
		}
	}
	for _, item := range preview.Items {
		itemStatus := "unknown"
		currentItem := findResultObject(current, item.TargetID)
		if item.TargetID != "" {
			if currentItem != nil {
				switch outcomes[item.CorrelationID] {
				case "failed", "skipped", "not_attempted":
					if matchesExpectedSubset(currentItem, objectMap(item.Before)) {
						itemStatus = "matched_before"
					} else {
						itemStatus = "mismatch"
					}
				case "unknown":
					if matchesExpectedSubset(currentItem, item.After) {
						itemStatus = "matched_after"
					} else if matchesExpectedSubset(currentItem, objectMap(item.Before)) {
						itemStatus = "matched_before"
					} else {
						itemStatus = "mismatch"
					}
				default:
					if matchesExpectedSubset(currentItem, item.After) {
						itemStatus = "matched"
					} else {
						itemStatus = "mismatch"
					}
				}
			} else {
				itemStatus = "missing"
			}
		}
		objects = append(objects, ObjectVerification{Name: "item_" + item.CorrelationID, Status: itemStatus, Current: currentItem})
	}
	if len(preview.Items) > 0 {
		status = "verified"
		for _, object := range objects {
			if strings.HasPrefix(object.Name, "item_") && !strings.HasPrefix(object.Status, "matched") {
				status = "inconclusive"
				break
			}
		}
	}
	current = redactPrivateData(current)
	return OperationVerification{
		Receipt: receipt, Status: status, Used: used, Current: current,
		CurrentHash: hash, PreviewHash: preview.CurrentHash, ExpectedDiff: preview.Diff,
		Objects: objects,
	}, nil
}

func VerifyRecovery(ctx context.Context, executor Executor, receipt, profile, adAccountID string, items []RecoveryItem) (OperationVerification, error) {
	if strings.TrimSpace(receipt) == "" || strings.TrimSpace(profile) == "" || strings.TrimSpace(adAccountID) == "" {
		return OperationVerification{}, errors.New("recovery receipt, profile, and adAccountId are required")
	}
	if len(items) == 0 || len(items) > 100 {
		return OperationVerification{}, errors.New("recovery requires 1 to 100 sanitized items")
	}
	current := make(map[string]any, len(items))
	objects := make([]ObjectVerification, 0, len(items))
	expected := make([]any, 0, len(items))
	status := "verified"
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.CorrelationID) == "" || strings.TrimSpace(item.Resource) == "" || strings.TrimSpace(item.TargetID) == "" {
			return OperationVerification{}, errors.New("recovery item correlationId, resource, and targetId are required")
		}
		if _, exists := seen[item.CorrelationID]; exists {
			return OperationVerification{}, fmt.Errorf("duplicate recovery correlationId %q", item.CorrelationID)
		}
		seen[item.CorrelationID] = struct{}{}
		read, err := appleads.ResourceGet(item.Resource, item.TargetID)
		if err != nil {
			return OperationVerification{}, fmt.Errorf("build recovery read for %s: %w", item.CorrelationID, err)
		}
		result, err := executor.Do(ctx, profile, adAccountID, read)
		if err != nil {
			return OperationVerification{}, fmt.Errorf("read recovery target %s: %w", item.CorrelationID, err)
		}
		itemStatus := "mismatch"
		if matchesExpectedSubset(result.Data, item.After) {
			itemStatus = "matched_after"
		} else if matchesExpectedSubset(result.Data, item.Before) {
			itemStatus = "matched_before"
		} else {
			status = "inconclusive"
		}
		current[item.CorrelationID] = result.Data
		objects = append(objects, ObjectVerification{Name: "item_" + item.CorrelationID, Status: itemStatus, Current: redactPrivateData(result.Data)})
		expected = append(expected, map[string]any{
			"correlationId": item.CorrelationID, "campaignId": item.CampaignID,
			"resourceType": item.ResourceType, "resource": item.Resource,
			"targetId": item.TargetID, "action": item.Action,
			"before": item.Before, "after": item.After,
		})
	}
	currentHash, err := valueHash(current)
	if err != nil {
		return OperationVerification{}, err
	}
	previewHash, err := valueHash(expected)
	if err != nil {
		return OperationVerification{}, err
	}
	return OperationVerification{
		Receipt: receipt, Status: status, Used: true, Current: redactPrivateData(current),
		CurrentHash: currentHash, PreviewHash: previewHash,
		ExpectedDiff: redactPrivateData(map[string]any{"recovery": expected}), Objects: objects,
	}, nil
}

func (s *Store) storeOutcomes(record *record, items []OperationItemStatus) {
	if len(items) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.outcomes == nil {
		record.outcomes = make(map[string]string, len(items))
	}
	for _, item := range items {
		record.outcomes[item.CorrelationID] = item.Status
	}
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func objectMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

func cloneCreateExpectation(value *CreateExpectation) *CreateExpectation {
	if value == nil {
		return nil
	}
	return &CreateExpectation{Resource: value.Resource, Expected: cloneMap(value.Expected)}
}

func appendVerificationRead(values []VerificationRead, value VerificationRead) []VerificationRead {
	for _, existing := range values {
		if existing.Name == value.Name {
			return values
		}
	}
	return append(values, value)
}

func appendOrReplaceObject(values []ObjectVerification, value ObjectVerification) []ObjectVerification {
	for index := range values {
		if values[index].Name == value.Name {
			values[index] = value
			return values
		}
	}
	return append(values, value)
}

func matchesExpectedSubset(current any, expected map[string]any) bool {
	if len(expected) == 0 {
		return false
	}
	currentMap, ok := current.(map[string]any)
	if !ok {
		return false
	}
	for key, expectedValue := range expected {
		currentValue, exists := currentMap[key]
		if !exists {
			return false
		}
		expectedMap, nested := expectedValue.(map[string]any)
		if nested {
			if !matchesExpectedSubset(currentValue, expectedMap) {
				return false
			}
			continue
		}
		expectedHash, expectedErr := valueHash(expectedValue)
		currentHash, currentErr := valueHash(currentValue)
		if expectedErr != nil || currentErr != nil || expectedHash != currentHash {
			return false
		}
	}
	return true
}

func readVerificationState(ctx context.Context, executor Executor, profile, adAccountID string, reads []VerificationRead, requireComplete bool) (any, any, []ObjectVerification, error) {
	hashValues := make(map[string]any, len(reads))
	currentValues := make(map[string]any, len(reads))
	objects := make([]ObjectVerification, 0, len(reads))
	firstName := ""
	for index, read := range reads {
		name := read.Name
		if name == "" {
			name = fmt.Sprintf("object_%d", index+1)
		}
		if _, exists := hashValues[name]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate verification read name %q", name)
		}
		if index == 0 {
			firstName = name
		}
		result, err := executor.Do(ctx, profile, adAccountID, read.Operation)
		if err != nil {
			var apiError *appleads.APIError
			if read.ExpectDeleted && errors.As(err, &apiError) && apiError.HTTPStatus == 404 {
				deleted := map[string]any{"deleted": true}
				hashValues[name] = verificationHashValue(deleted, appleads.Pagination{})
				currentValues[name] = deleted
				objects = append(objects, ObjectVerification{Name: name, Status: "deleted", Current: deleted})
				continue
			}
			return nil, nil, nil, fmt.Errorf("read %s: %w", name, err)
		}
		if requireComplete && read.RequireComplete && !verificationResultComplete(result) {
			return nil, nil, nil, fmt.Errorf("read %s: %w", name, ErrVerificationIncomplete)
		}
		hashValues[name] = verificationHashValue(result.Data, result.Pagination)
		currentValues[name] = result.Data
		objects = append(objects, ObjectVerification{Name: name, Status: "read", Current: result.Data})
	}
	if len(reads) == 1 {
		return hashValues[firstName], currentValues[firstName], objects, nil
	}
	return hashValues, currentValues, objects, nil
}

func verificationHashValue(data any, pagination appleads.Pagination) map[string]any {
	return map[string]any{
		"data": data,
		"pagination": map[string]any{
			"offset":       pagination.Offset,
			"pageSize":     pagination.PageSize,
			"totalResults": pagination.Total,
			"next":         pagination.Next,
		},
	}
}

func verificationResultComplete(result appleads.Result) bool {
	if result.Pagination.Offset != 0 || result.Pagination.Next != "" {
		return false
	}
	count, ok := verificationCollectionSize(result.Data)
	if !ok {
		return false
	}
	return result.Pagination.Total == count
}

func verificationCollectionSize(value any) (int, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, true
	case []any:
		return len(typed), true
	case map[string]any:
		for _, key := range []string{"items", "result", "data"} {
			if nested, exists := typed[key]; exists {
				return verificationCollectionSize(nested)
			}
		}
		return 0, false
	default:
		return 0, false
	}
}

func unknownItemStatuses(items []OperationItemPreview) []OperationItemStatus {
	result := make([]OperationItemStatus, 0, len(items))
	for _, item := range items {
		result = append(result, OperationItemStatus{CorrelationID: item.CorrelationID, CampaignID: item.CampaignID, ResourceType: item.ResourceType, Action: item.Action, TargetID: item.TargetID, Status: "unknown"})
	}
	return result
}

func resultItemStatuses(data any, previews []OperationItemPreview) []OperationItemStatus {
	if len(previews) == 0 {
		return nil
	}
	byCorrelation := make(map[string]OperationItemStatus, len(previews))
	for _, preview := range previews {
		byCorrelation[preview.CorrelationID] = OperationItemStatus{CorrelationID: preview.CorrelationID, CampaignID: preview.CampaignID, ResourceType: preview.ResourceType, Action: preview.Action, TargetID: preview.TargetID, Status: "unknown"}
	}
	walkResultItems(data, byCorrelation)
	result := make([]OperationItemStatus, 0, len(previews))
	for _, preview := range previews {
		result = append(result, byCorrelation[preview.CorrelationID])
	}
	return result
}

func walkResultItems(value any, statuses map[string]OperationItemStatus) {
	switch typed := value.(type) {
	case map[string]any:
		correlationID := fmt.Sprint(typed["correlationId"])
		if current, ok := statuses[correlationID]; ok && correlationID != "<nil>" {
			current.TargetID = firstNonEmpty(current.TargetID, fmt.Sprint(typed["id"]), findResultID(typed["result"]), findResultID(typed["data"]))
			if failure, exists := typed["error"]; exists && failure != nil {
				current.Status = "failed"
				current.Error = publicBulkError(failure)
			} else if failures, exists := typed["errors"]; exists && failures != nil {
				current.Status = "failed"
				current.Error = publicBulkError(failures)
			} else if success, exists := typed["success"].(bool); exists && !success {
				current.Status = "failed"
				current.Error = "Apple returned success=false"
			} else {
				current.Status = "applied"
			}
			statuses[correlationID] = current
		}
		for _, item := range typed {
			walkResultItems(item, statuses)
		}
	case []any:
		for _, item := range typed {
			walkResultItems(item, statuses)
		}
	}
}

func findResultID(value any) string {
	if object, ok := value.(map[string]any); ok {
		if id, exists := object["id"]; exists && id != nil {
			return fmt.Sprint(id)
		}
	}
	return ""
}

func findFirstResultID(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if id, exists := typed["id"]; exists && id != nil {
			return fmt.Sprint(id)
		}
		if result, exists := typed["result"]; exists {
			if id := findFirstResultID(result); id != "" {
				return id
			}
		}
		for key, item := range typed {
			if key == "result" {
				continue
			}
			if id := findFirstResultID(item); id != "" {
				return id
			}
		}
	case []any:
		for _, item := range typed {
			if id := findFirstResultID(item); id != "" {
				return id
			}
		}
	}
	return ""
}

func findResultObject(value any, expected string) any {
	if expected == "" {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		if id, exists := typed["id"]; exists && fmt.Sprint(id) == expected {
			return typed
		}
		for _, item := range typed {
			if found := findResultObject(item, expected); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findResultObject(item, expected); found != nil {
				return found
			}
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func aggregateItemStatus(items []OperationItemStatus) string {
	if len(items) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Status]++
	}
	if counts["applied"] == len(items) {
		return "applied"
	}
	if counts["failed"] == len(items) {
		return "failed"
	}
	if counts["unknown"] == len(items) {
		return "unknown"
	}
	return "partial"
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
	case "systemStatusReasons", "countries", "countryOrRegionCodes", "sharedBudgets", "result", "include", "exclude":
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

func redactPrivateData(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactPrivateData(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if privateDataKey(key) {
				continue
			}
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if normalized == "error" || normalized == "errors" {
				if item == nil {
					result[key] = nil
					continue
				}
				result[key] = publicBulkError(item)
				continue
			}
			result[key] = redactPrivateData(item)
		}
		return result
	default:
		return value
	}
}

func publicBulkError(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for _, key := range []string{"code", "errorCode", "message"} {
			if text, ok := typed[key].(string); ok {
				limit := 1024
				if key != "message" {
					limit = 128
				}
				if text = boundedErrorText(text, limit); text != "" {
					result[key] = text
				}
			}
		}
		if info, ok := typed["info"].(map[string]any); ok {
			safeInfo := map[string]any{}
			for key, item := range info {
				safeKey := boundedErrorText(key, 128)
				if len(safeInfo) == 20 || !safeBulkInfoKey(safeKey) {
					continue
				}
				if text, ok := item.(string); ok {
					if text = boundedErrorText(text, 1024); text != "" {
						safeInfo[safeKey] = text
					}
				}
			}
			if len(safeInfo) > 0 {
				result["info"] = safeInfo
			}
		}
		for _, key := range []string{"details", "errors", "error"} {
			if nested, exists := typed[key]; exists {
				if safe := publicBulkError(nested); safe != nil {
					result[key] = safe
				}
			}
		}
		if len(result) == 0 {
			return "Apple returned an item-level failure"
		}
		return result
	case []any:
		if len(typed) > 20 {
			typed = typed[:20]
		}
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			if safe := publicBulkError(item); safe != nil {
				result = append(result, safe)
			}
		}
		return result
	default:
		return "Apple returned an item-level failure"
	}
}

func safeBulkInfoKey(value string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(value))
	switch normalized {
	case "field", "parameter", "path", "location", "reason", "index", "correlationid", "resource", "resourceid", "selector":
		return true
	default:
		return false
	}
}

func boundedErrorText(value string, limit int) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return strings.TrimSpace(value)
}

func privateDataKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	return strings.Contains(normalized, "invoicedetail") ||
		strings.Contains(normalized, "invoicecontact") ||
		strings.Contains(normalized, "billingemail") ||
		strings.Contains(normalized, "billingcontact") ||
		strings.HasPrefix(normalized, "primarybuyer") ||
		strings.HasPrefix(normalized, "buyeremail")
}
