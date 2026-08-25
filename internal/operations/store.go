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

type VerificationRead struct {
	Name          string
	Operation     appleads.Operation
	ExpectDeleted bool
}

type PreviewOptions struct {
	Impact *OperationImpact
	Items  []OperationItemPreview
}

type SequenceStep struct {
	Item     OperationItemPreview
	Mutation appleads.Operation
}

type record struct {
	preview  OperationPreview
	verify   []VerificationRead
	mutation appleads.Operation
	sequence []SequenceStep
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
	return s.PreviewComposite(ctx, executor, profile, adAccountID, name, targetIDs, payload, []VerificationRead{{Name: "current", Operation: verify}}, mutation, PreviewOptions{})
}

func (s *Store) PreviewComposite(ctx context.Context, executor Executor, profile, adAccountID, name string, targetIDs []string, payload map[string]any, verify []VerificationRead, mutation appleads.Operation, options PreviewOptions) (OperationPreview, error) {
	if !mutation.IsMutation() {
		return OperationPreview{}, errors.New("preview requires a mutation operation")
	}
	if len(verify) == 0 {
		return OperationPreview{}, errors.New("preview requires at least one verification read")
	}
	current, _, err := readVerificationState(ctx, executor, profile, adAccountID, verify)
	if err != nil {
		return OperationPreview{}, fmt.Errorf("read current state: %w", err)
	}
	currentHash, err := valueHash(current)
	if err != nil {
		return OperationPreview{}, err
	}
	payloadHash, err := valueHash(payload)
	if err != nil {
		return OperationPreview{}, err
	}
	currentData, err := json.Marshal(current)
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
	if len(s.records) >= maxStoredReceipts || recordSize > maxStoredReceiptData-s.total {
		s.mu.Unlock()
		return OperationPreview{}, errors.New("receipt capacity reached; wait for existing previews to expire")
	}
	s.records[receipt] = &record{preview: preview, verify: append([]VerificationRead(nil), verify...), mutation: mutation, size: recordSize}
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
			s.total -= item.size
			delete(s.records, preview.Receipt)
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
	sequence := append([]SequenceStep(nil), record.sequence...)
	s.mu.Unlock()

	current, _, err := readVerificationState(ctx, executor, preview.Profile, preview.AdAccountID, verify)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("re-read current state: %w", err)
	}
	currentHash, err := valueHash(current)
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

	if len(sequence) > 0 {
		return s.applySequence(ctx, executor, receipt, preview, sequence), nil
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
			return receiptResult, nil
		}
		return OperationReceipt{}, err
	}
	receiptResult.Items = resultItemStatuses(result.Data, preview.Items)
	if len(receiptResult.Items) > 0 {
		s.mu.Lock()
		targetIDs := make([]string, 0, len(receiptResult.Items))
		for index := range record.preview.Items {
			if index < len(receiptResult.Items) && receiptResult.Items[index].TargetID != "" {
				record.preview.Items[index].TargetID = receiptResult.Items[index].TargetID
				targetIDs = append(targetIDs, receiptResult.Items[index].TargetID)
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
	current, objects, err := readVerificationState(ctx, executor, preview.Profile, preview.AdAccountID, verify)
	if err != nil {
		return OperationVerification{}, fmt.Errorf("read current state for verification: %w", err)
	}
	hash, err := valueHash(current)
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
	for index := range objects {
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
		if item.TargetID != "" {
			if containsResultID(current, item.TargetID) {
				itemStatus = "present"
			} else {
				itemStatus = "missing"
			}
		}
		objects = append(objects, ObjectVerification{Name: "item_" + item.CorrelationID, Status: itemStatus, Current: findResultObject(current, item.TargetID)})
	}
	current = redactPrivateData(current)
	return OperationVerification{
		Receipt: receipt, Status: status, Used: used, Current: current,
		CurrentHash: hash, PreviewHash: preview.CurrentHash, ExpectedDiff: preview.Diff,
		Objects: objects,
	}, nil
}

func readVerificationState(ctx context.Context, executor Executor, profile, adAccountID string, reads []VerificationRead) (any, []ObjectVerification, error) {
	values := make(map[string]any, len(reads))
	objects := make([]ObjectVerification, 0, len(reads))
	firstName := ""
	for index, read := range reads {
		name := read.Name
		if name == "" {
			name = fmt.Sprintf("object_%d", index+1)
		}
		if _, exists := values[name]; exists {
			return nil, nil, fmt.Errorf("duplicate verification read name %q", name)
		}
		if index == 0 {
			firstName = name
		}
		result, err := executor.Do(ctx, profile, adAccountID, read.Operation)
		if err != nil {
			var apiError *appleads.APIError
			if read.ExpectDeleted && errors.As(err, &apiError) && apiError.HTTPStatus == 404 {
				deleted := map[string]any{"deleted": true}
				values[name] = deleted
				objects = append(objects, ObjectVerification{Name: name, Status: "deleted", Current: deleted})
				continue
			}
			return nil, nil, fmt.Errorf("read %s: %w", name, err)
		}
		values[name] = result.Data
		objects = append(objects, ObjectVerification{Name: name, Status: "read", Current: result.Data})
	}
	if len(reads) == 1 {
		return values[firstName], objects, nil
	}
	return values, objects, nil
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
				current.Error = failure
			} else if failures, exists := typed["errors"]; exists && failures != nil {
				current.Status = "failed"
				current.Error = failures
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

func containsResultID(value any, expected string) bool {
	return findResultObject(value, expected) != nil
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
			result[key] = redactPrivateData(item)
		}
		return result
	default:
		return value
	}
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
