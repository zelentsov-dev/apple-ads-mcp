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
	return s.loadLocked(policy)
}

func (s *HistoryStore) Append(policy string, entry HistoryEntry) error {
	if err := validateHistoryEntry(entry); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history, err := s.loadLocked(policy)
	if err != nil {
		return err
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	history.Entries = append(history.Entries, entry)
	if len(history.Entries) > maxHistoryEntries {
		history.Entries = append([]HistoryEntry(nil), history.Entries[len(history.Entries)-maxHistoryEntries:]...)
	}
	return s.writeLocked(policy, history)
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
	if len(history.Entries) > maxHistoryEntries {
		history.Entries = append([]HistoryEntry(nil), history.Entries[len(history.Entries)-maxHistoryEntries:]...)
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
	if err := os.Rename(temporaryPath, path); err != nil {
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
