package optimization

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	maxHistoryEntries = 200
	maxHistoryBody    = 8 << 20
)

type History struct {
	Entries []HistoryEntry `json:"entries"`
}

type HistoryEntry struct {
	Policy            string                        `json:"policy"`
	Profile           string                        `json:"profile"`
	AdAccountID       string                        `json:"adAccountId"`
	CreatedAt         string                        `json:"createdAt"`
	ReceiptHash       string                        `json:"receiptHash,omitempty"`
	Status            string                        `json:"status"`
	Actions           []HistoryAction               `json:"actions,omitempty"`
	Verification      []HistoryAction               `json:"verification,omitempty"`
	PerformanceBefore []CampaignPerformanceSnapshot `json:"performanceBefore,omitempty"`
	PerformanceAfter  []CampaignPerformanceSnapshot `json:"performanceAfter,omitempty"`
}

type CampaignPerformanceSnapshot struct {
	CampaignID    string        `json:"campaignId"`
	Last28Days    MetricSummary `json:"last28Days"`
	Last7Days     MetricSummary `json:"last7Days"`
	Previous7Days MetricSummary `json:"previous7Days"`
}

func PerformanceSnapshots(baseline Baseline) []CampaignPerformanceSnapshot {
	result := make([]CampaignPerformanceSnapshot, 0, len(baseline.Campaigns))
	for _, campaign := range baseline.Campaigns {
		result = append(result, CampaignPerformanceSnapshot{CampaignID: campaign.Campaign.CampaignID, Last28Days: campaign.Last28Days, Last7Days: campaign.Last7Days, Previous7Days: campaign.Previous7Days})
	}
	return result
}

type HistoryAction struct {
	CorrelationID string         `json:"correlationId"`
	CampaignID    string         `json:"campaignId"`
	ResourceType  string         `json:"resourceType"`
	Resource      string         `json:"resource,omitempty"`
	ResourceID    string         `json:"resourceId"`
	Action        string         `json:"action"`
	Status        string         `json:"status"`
	Reason        string         `json:"reason,omitempty"`
	Before        map[string]any `json:"before,omitempty"`
	After         map[string]any `json:"after,omitempty"`
	OccurredAt    string         `json:"occurredAt"`
}

type HistoryStore struct {
	root string
	mu   sync.Mutex
}

func DefaultHistoryRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "apple-ads-mcp", "optimization"), nil
}

func NewHistoryStore(root string) (*HistoryStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("APPLE_ADS_OPTIMIZATION_STATE_DIR"))
	}
	if root == "" {
		var err error
		root, err = DefaultHistoryRoot()
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("optimization state directory must be absolute")
	}
	return &HistoryStore{root: root}, nil
}

func (s *HistoryStore) Load(policy string) (History, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lock(policy)
	if err != nil {
		return History{}, err
	}
	defer unlock()
	return s.loadLocked(policy)
}

func (s *HistoryStore) Append(policy string, entry HistoryEntry) error {
	if err := validateHistoryEntry(entry); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lock(policy)
	if err != nil {
		return err
	}
	defer unlock()
	history, err := s.loadLocked(policy)
	if err != nil {
		return err
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	history.Entries = append(history.Entries, entry)
	history.Entries, err = boundHistoryEntries(history.Entries)
	if err != nil {
		return err
	}
	return s.writeLocked(policy, history)
}

func (s *HistoryStore) BeginIntent(policy string, entry HistoryEntry) error {
	if entry.Status != "applying" {
		return errors.New("optimization intent status must be applying")
	}
	if strings.TrimSpace(entry.ReceiptHash) == "" || strings.TrimSpace(entry.Profile) == "" || strings.TrimSpace(entry.AdAccountID) == "" {
		return errors.New("optimization intent requires receiptHash, profile, and adAccountId")
	}
	if len(entry.Actions) == 0 || len(entry.Actions) > 100 {
		return errors.New("optimization intent requires 1 to 100 recovery actions")
	}
	for _, action := range entry.Actions {
		if strings.TrimSpace(action.CorrelationID) == "" || strings.TrimSpace(action.Resource) == "" || strings.TrimSpace(action.ResourceID) == "" || strings.TrimSpace(action.Action) == "" {
			return errors.New("optimization recovery action requires correlationId, resource, resourceId, and action")
		}
	}
	if err := validateHistoryEntry(entry); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lock(policy)
	if err != nil {
		return err
	}
	defer unlock()
	history, err := s.loadLocked(policy)
	if err != nil {
		return err
	}
	if HistoryRequiresReconciliation(history) {
		return errors.New("a previous optimization write requires operations_verify before another apply")
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	history.Entries = append(history.Entries, entry)
	history.Entries, err = boundHistoryEntries(history.Entries)
	if err != nil {
		return err
	}
	return s.writeLocked(policy, history)
}

func HistoryRequiresReconciliation(history History) bool {
	for _, unresolved := range receiptReconciliationStates(history) {
		if unresolved {
			return true
		}
	}
	return false
}

func ReceiptRequiresReconciliation(history History, receiptHash string) bool {
	return receiptReconciliationStates(history)[strings.TrimSpace(receiptHash)]
}

func ReconciliationEntry(history History, receiptHash string) (HistoryEntry, bool) {
	receiptHash = strings.TrimSpace(receiptHash)
	if receiptHash == "" || !ReceiptRequiresReconciliation(history, receiptHash) {
		return HistoryEntry{}, false
	}
	for _, entry := range history.Entries {
		if entry.ReceiptHash == receiptHash && entry.Status == "applying" && len(entry.Actions) > 0 {
			return entry, true
		}
	}
	for _, entry := range history.Entries {
		if entry.ReceiptHash == receiptHash && len(entry.Actions) > 0 {
			return entry, true
		}
	}
	return HistoryEntry{}, false
}

func receiptReconciliationStates(history History) map[string]bool {
	unresolved := map[string]bool{}
	for _, entry := range history.Entries {
		if entry.ReceiptHash == "" {
			continue
		}
		switch {
		case entry.Status == "applying":
			unresolved[entry.ReceiptHash] = true
		case entry.Status == "unknown":
			unresolved[entry.ReceiptHash] = true
		case entry.Status == "verification_verified":
			unresolved[entry.ReceiptHash] = false
		case strings.HasPrefix(entry.Status, "verification_"):
			unresolved[entry.ReceiptHash] = true
		default:
			unknown := false
			for _, action := range entry.Actions {
				if action.Status == "unknown" || action.Status == "pending" {
					unknown = true
					break
				}
			}
			unresolved[entry.ReceiptHash] = unknown
		}
	}
	return unresolved
}

func boundHistoryEntries(entries []HistoryEntry) ([]HistoryEntry, error) {
	entries = materializeHistoryActionCarriers(entries)
	if len(entries) <= maxHistoryEntries {
		return append([]HistoryEntry(nil), entries...), nil
	}
	states := receiptReconciliationStates(History{Entries: entries})
	protected := make(map[int]struct{})
	for receiptHash, unresolved := range states {
		if !unresolved {
			continue
		}
		protectedIndex := -1
		for index, entry := range entries {
			if entry.ReceiptHash == receiptHash && entry.Status == "applying" && len(entry.Actions) > 0 {
				protectedIndex = index
				break
			}
		}
		if protectedIndex < 0 {
			for index, entry := range entries {
				if entry.ReceiptHash == receiptHash && len(entry.Actions) > 0 {
					protectedIndex = index
					break
				}
			}
		}
		if protectedIndex < 0 {
			return nil, fmt.Errorf("unresolved receipt %s has no recoverable intent", receiptHash)
		}
		protected[protectedIndex] = struct{}{}
	}
	if len(protected) > maxHistoryEntries {
		return nil, errors.New("unresolved optimization receipts exceed bounded history capacity")
	}
	selected := make(map[int]struct{}, maxHistoryEntries)
	for index := range protected {
		selected[index] = struct{}{}
	}
	for index := len(entries) - 1; index >= 0 && len(selected) < maxHistoryEntries; index-- {
		selected[index] = struct{}{}
	}
	result := make([]HistoryEntry, 0, len(selected))
	for index, entry := range entries {
		if _, exists := selected[index]; exists {
			result = append(result, entry)
		}
	}
	return result, nil
}

func materializeHistoryActionCarriers(entries []HistoryEntry) []HistoryEntry {
	result := append([]HistoryEntry(nil), entries...)
	carriers := make(map[string][]HistoryAction)
	for index, entry := range result {
		receiptHash := strings.TrimSpace(entry.ReceiptHash)
		if receiptHash == "" {
			continue
		}
		if entry.Status == "verification_verified" && len(entry.Verification) > 0 {
			entry.Actions = mergeHistoryActions(carriers[receiptHash], entry.Actions)
			result[index] = entry
		}
		if len(entry.Actions) > 0 {
			carriers[receiptHash] = mergeHistoryActions(carriers[receiptHash], entry.Actions)
		}
	}
	for receiptHash, unresolved := range receiptReconciliationStates(History{Entries: result}) {
		if !unresolved {
			continue
		}
		for index, entry := range result {
			if entry.ReceiptHash == receiptHash && entry.Status == "applying" && len(entry.Actions) > 0 {
				entry.Actions = append([]HistoryAction(nil), carriers[receiptHash]...)
				result[index] = entry
				break
			}
		}
	}
	return result
}

func mergeHistoryActions(existing, incoming []HistoryAction) []HistoryAction {
	result := append([]HistoryAction(nil), existing...)
	positions := make(map[string]int, len(result))
	for index, action := range result {
		positions[historyActionIdentity(action)] = index
	}
	for _, action := range incoming {
		identity := historyActionIdentity(action)
		if index, exists := positions[identity]; exists {
			result[index] = mergeHistoryAction(result[index], action)
			continue
		}
		positions[identity] = len(result)
		result = append(result, action)
	}
	return result
}

func historyActionIdentity(action HistoryAction) string {
	if action.CorrelationID != "" {
		return "correlation:" + action.CorrelationID
	}
	return strings.Join([]string{action.CampaignID, action.ResourceType, action.ResourceID, action.Action}, "\x00")
}

func (s *HistoryStore) lock(policy string) (func(), error) {
	path, err := s.path(policy)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create optimization state directory: %w", err)
	}
	return lockHistoryFile(path + ".lock")
}

func validateHistoryEntry(entry HistoryEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("validate optimization history entry: %w", err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("validate optimization history entry: %w", err)
	}
	var visit func(any) error
	visit = func(current any) error {
		switch typed := current.(type) {
		case map[string]any:
			for key, item := range typed {
				normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
				if strings.Contains(normalized, "privatekey") || strings.Contains(normalized, "clientsecret") || strings.Contains(normalized, "accesstoken") || normalized == "authorization" || strings.Contains(normalized, "invoicedetail") || strings.Contains(normalized, "billingemail") || strings.HasPrefix(normalized, "primarybuyer") || strings.HasPrefix(normalized, "buyeremail") {
					return fmt.Errorf("optimization history must not contain private field %q", key)
				}
				if err := visit(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range typed {
				if err := visit(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value)
}

func (s *HistoryStore) loadLocked(policy string) (History, error) {
	path, err := s.path(policy)
	if err != nil {
		return History{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return History{}, nil
	}
	if err != nil {
		return History{}, fmt.Errorf("read optimization history: %w", err)
	}
	defer file.Close()
	if err := requireSecureRegular(file, "optimization history"); err != nil {
		return History{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxHistoryBody+1))
	if err != nil {
		return History{}, fmt.Errorf("read optimization history: %w", err)
	}
	if len(data) > maxHistoryBody {
		return History{}, errors.New("optimization history exceeds size limit")
	}
	var history History
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&history); err != nil {
		return History{}, fmt.Errorf("decode optimization history: %w", err)
	}
	history.Entries, err = boundHistoryEntries(history.Entries)
	if err != nil {
		return History{}, fmt.Errorf("bound optimization history: %w", err)
	}
	return history, nil
}

func (s *HistoryStore) writeLocked(policy string, history History) error {
	path, err := s.path(policy)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create optimization state directory: %w", err)
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("encode optimization history: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("create optimization history temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if runtime.GOOS != "windows" {
		if err := temporary.Chmod(0o600); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("protect optimization history: %w", err)
		}
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write optimization history: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync optimization history: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close optimization history: %w", err)
	}
	if err := replaceHistoryFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace optimization history: %w", err)
	}
	return nil
}

func (s *HistoryStore) path(policy string) (string, error) {
	policy = strings.TrimSpace(policy)
	if policy == "" {
		return "", errors.New("policy is required")
	}
	if !validLocalName(policy) {
		return "", errors.New("policy name may contain only letters, digits, hyphens, and underscores")
	}
	return filepath.Join(s.root, strings.ToLower(policy)+".json"), nil
}
