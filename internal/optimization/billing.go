package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
)

type BillingProfileFile struct {
	Profiles []BillingProfile `json:"profiles"`
}

type BillingProfile struct {
	Name              string `json:"name"`
	PrimaryBuyerName  string `json:"primaryBuyerName"`
	PrimaryBuyerEmail string `json:"primaryBuyerEmail"`
	BillingEmail      string `json:"billingEmail"`
	OrderNumber       string `json:"orderNumber,omitempty"`
	ClientName        string `json:"clientName,omitempty"`
}

func DefaultBillingPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "apple-ads-mcp", "billing-profiles.json"), nil
}

func LoadBillingProfiles(path string) (BillingProfileFile, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("APPLE_ADS_BILLING_PROFILES"))
	}
	if path == "" {
		var err error
		path, err = DefaultBillingPath()
		if err != nil {
			return BillingProfileFile{}, "", err
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return BillingProfileFile{}, path, fmt.Errorf("read billing profiles: %w", err)
	}
	defer file.Close()
	if err := requireSecureRegular(file, "billing profiles"); err != nil {
		return BillingProfileFile{}, path, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPolicyBody+1))
	if err != nil {
		return BillingProfileFile{}, path, fmt.Errorf("read billing profiles: %w", err)
	}
	if len(data) > maxPolicyBody {
		return BillingProfileFile{}, path, errors.New("billing profiles exceed size limit")
	}
	var profiles BillingProfileFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profiles); err != nil {
		return BillingProfileFile{}, path, fmt.Errorf("decode billing profiles: %w", err)
	}
	if err := profiles.Validate(); err != nil {
		return BillingProfileFile{}, path, fmt.Errorf("validate billing profiles: %w", err)
	}
	return profiles, path, nil
}

func SaveBillingProfiles(path string, profiles BillingProfileFile) error {
	if err := profiles.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("encode billing profiles: %w", err)
	}
	return secureCreate(path, append(data, '\n'))
}

func AddBillingProfile(path string, profile BillingProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	profiles, _, err := LoadBillingProfiles(path)
	if errors.Is(err, os.ErrNotExist) || err != nil && strings.Contains(err.Error(), "no such file") {
		return SaveBillingProfiles(path, BillingProfileFile{Profiles: []BillingProfile{profile}})
	}
	if err != nil {
		return err
	}
	for _, existing := range profiles.Profiles {
		if strings.EqualFold(existing.Name, profile.Name) {
			return fmt.Errorf("billing profile %q already exists", profile.Name)
		}
	}
	profiles.Profiles = append(profiles.Profiles, profile)
	if err := profiles.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("encode billing profiles: %w", err)
	}
	return secureReplace(path, append(data, '\n'))
}

func (f BillingProfileFile) Validate() error {
	if len(f.Profiles) == 0 {
		return errors.New("at least one billing profile is required")
	}
	seen := map[string]struct{}{}
	for index, profile := range f.Profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("billing profile %d: %w", index+1, err)
		}
		key := strings.ToLower(profile.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate billing profile %q", profile.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (p BillingProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.PrimaryBuyerName) == "" {
		return errors.New("name and primaryBuyerName are required")
	}
	for field, value := range map[string]string{"primaryBuyerEmail": p.PrimaryBuyerEmail, "billingEmail": p.BillingEmail} {
		address, err := mail.ParseAddress(strings.TrimSpace(value))
		if err != nil || address.Address != strings.TrimSpace(value) {
			return fmt.Errorf("%s must be a valid email address", field)
		}
	}
	return nil
}

func (f BillingProfileFile) Resolve(name string) (BillingProfile, error) {
	for _, profile := range f.Profiles {
		if strings.EqualFold(profile.Name, strings.TrimSpace(name)) {
			return profile, nil
		}
	}
	return BillingProfile{}, fmt.Errorf("billing profile %q not found", name)
}

func (p BillingProfile) InvoiceDetail() map[string]any {
	value := map[string]any{
		"primaryBuyerName":  p.PrimaryBuyerName,
		"primaryBuyerEmail": p.PrimaryBuyerEmail,
		"billingEmail":      p.BillingEmail,
	}
	if p.OrderNumber != "" {
		value["orderNumber"] = p.OrderNumber
	}
	if p.ClientName != "" {
		value["clientName"] = p.ClientName
	}
	return value
}

func (p BillingProfile) PrivateHash() (string, error) {
	data, err := json.Marshal(p.InvoiceDetail())
	if err != nil {
		return "", fmt.Errorf("encode private billing payload: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
