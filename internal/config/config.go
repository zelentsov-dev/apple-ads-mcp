package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const configDirName = "apple-ads-mcp"

const maxConfigBody = 1 << 20

type Config struct {
	Profiles []Profile `json:"profiles" jsonschema:"configured Apple Ads profiles"`
}

type Profile struct {
	Name               string `json:"name" jsonschema:"unique local profile name"`
	ClientID           string `json:"clientId" jsonschema:"Apple Ads OAuth client ID"`
	TeamID             string `json:"teamId" jsonschema:"Apple Ads OAuth team ID"`
	KeyID              string `json:"keyId" jsonschema:"Apple Ads public key ID"`
	PrivateKeyPath     string `json:"privateKeyPath" jsonschema:"absolute path to the ES256 private key"`
	DefaultAdAccountID string `json:"defaultAdAccountId,omitempty" jsonschema:"default Apple Ads ad account ID"`
	AllowWrites        bool   `json:"allowWrites,omitempty" jsonschema:"whether this profile permits mutation previews and applies"`
}

type PublicProfile struct {
	Name             string `json:"name"`
	AllowWrites      bool   `json:"allowWrites"`
	CredentialSource string `json:"credentialSource"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config", configDirName, "accounts.json"), nil
}

func Load(explicitPath string) (Config, string, error) {
	path := strings.TrimSpace(explicitPath)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("APPLE_ADS_MCP_CONFIG"))
	}
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return Config{}, "", err
		}
		if _, err := os.Stat(defaultPath); err == nil {
			path = defaultPath
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, "", fmt.Errorf("inspect default config: %w", err)
		}
	}

	if path != "" {
		cfg, err := loadFile(path)
		if err != nil {
			return Config{}, "", err
		}
		cfg, err = applyEnvironmentOverrides(cfg)
		if err != nil {
			return Config{}, "", err
		}
		return cfg, path, nil
	}

	cfg, ok, err := fromEnvironment()
	if err != nil {
		return Config{}, "", err
	}
	if !ok {
		return Config{}, "", errors.New("no Apple Ads profiles configured; run `apple-ads-mcp config init` or set APPLE_ADS_* variables")
	}
	return cfg, "environment", nil
}

func LoadOptional(explicitPath string) (Config, string, error) {
	cfg, source, err := Load(explicitPath)
	if err != nil && strings.Contains(err.Error(), "no Apple Ads profiles configured") {
		return Config{}, "none", nil
	}
	return cfg, source, err
}

func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return errors.New("config path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("inspect config directory: %w", err)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return errors.New("config directory must not be writable by group or other users")
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	if len(c.Profiles) == 0 {
		return errors.New("at least one profile is required")
	}
	seen := make(map[string]struct{}, len(c.Profiles))
	for i, profile := range c.Profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("profile %d: %w", i+1, err)
		}
		key := strings.ToLower(profile.Name)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate profile name %q", profile.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (p Profile) Validate() error {
	required := map[string]string{
		"name":           p.Name,
		"clientId":       p.ClientID,
		"teamId":         p.TeamID,
		"keyId":          p.KeyID,
		"privateKeyPath": p.PrivateKeyPath,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if !filepath.IsAbs(p.PrivateKeyPath) {
		return errors.New("privateKeyPath must be absolute")
	}
	return nil
}

func (p Profile) ValidatePrivateKeyFile() error {
	info, err := os.Stat(p.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("inspect private key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("private key path must reference a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("private key permissions must deny group and other access; current mode is %04o", info.Mode().Perm())
	}
	return nil
}

func (c Config) ResolveProfile(name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		if len(c.Profiles) == 1 {
			return c.Profiles[0], nil
		}
		return Profile{}, errors.New("profile is required when multiple profiles are configured")
	}
	for _, profile := range c.Profiles {
		if strings.EqualFold(profile.Name, name) {
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("profile %q not found", name)
}

func (c Config) PublicProfiles(source string) []PublicProfile {
	profiles := make([]PublicProfile, 0, len(c.Profiles))
	for _, profile := range c.Profiles {
		profiles = append(profiles, PublicProfile{
			Name:             profile.Name,
			AllowWrites:      profile.AllowWrites,
			CredentialSource: publicCredentialSource(source),
		})
	}
	return profiles
}

func publicCredentialSource(source string) string {
	switch source {
	case "environment", "none":
		return source
	default:
		return "file"
	}
}

func loadFile(path string) (Config, error) {
	if !filepath.IsAbs(path) {
		return Config{}, errors.New("config path must be absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("inspect config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("config path must reference a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return Config{}, fmt.Errorf("config permissions must deny group and other access; current mode is %04o", info.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBody+1))
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) > maxConfigBody {
		return Config{}, errors.New("config exceeds size limit")
	}
	var cfg Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func fromEnvironment() (Config, bool, error) {
	clientID := strings.TrimSpace(os.Getenv("APPLE_ADS_CLIENT_ID"))
	teamID := strings.TrimSpace(os.Getenv("APPLE_ADS_TEAM_ID"))
	keyID := strings.TrimSpace(os.Getenv("APPLE_ADS_KEY_ID"))
	keyPath := strings.TrimSpace(os.Getenv("APPLE_ADS_PRIVATE_KEY_PATH"))
	if clientID == "" && teamID == "" && keyID == "" && keyPath == "" {
		return Config{}, false, nil
	}
	allowWrites, err := strconv.ParseBool(defaultString(os.Getenv("APPLE_ADS_ALLOW_WRITES"), "false"))
	if err != nil {
		return Config{}, false, fmt.Errorf("parse APPLE_ADS_ALLOW_WRITES: %w", err)
	}
	cfg := Config{Profiles: []Profile{{
		Name:               defaultString(os.Getenv("APPLE_ADS_PROFILE"), "default"),
		ClientID:           clientID,
		TeamID:             teamID,
		KeyID:              keyID,
		PrivateKeyPath:     keyPath,
		DefaultAdAccountID: strings.TrimSpace(os.Getenv("APPLE_ADS_AD_ACCOUNT_ID")),
		AllowWrites:        allowWrites,
	}}}
	if err := cfg.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("validate environment config: %w", err)
	}
	return cfg, true, nil
}

func applyEnvironmentOverrides(cfg Config) (Config, error) {
	values := map[string]string{
		"clientId":       strings.TrimSpace(os.Getenv("APPLE_ADS_CLIENT_ID")),
		"teamId":         strings.TrimSpace(os.Getenv("APPLE_ADS_TEAM_ID")),
		"keyId":          strings.TrimSpace(os.Getenv("APPLE_ADS_KEY_ID")),
		"privateKeyPath": strings.TrimSpace(os.Getenv("APPLE_ADS_PRIVATE_KEY_PATH")),
		"adAccountId":    strings.TrimSpace(os.Getenv("APPLE_ADS_AD_ACCOUNT_ID")),
	}
	allowWritesValue := strings.TrimSpace(os.Getenv("APPLE_ADS_ALLOW_WRITES"))
	hasOverride := allowWritesValue != ""
	for _, value := range values {
		hasOverride = hasOverride || value != ""
	}
	if !hasOverride {
		return cfg, nil
	}
	profileName := strings.TrimSpace(os.Getenv("APPLE_ADS_PROFILE"))
	if profileName == "" {
		if len(cfg.Profiles) != 1 {
			return Config{}, errors.New("APPLE_ADS_PROFILE is required when overriding a multi-profile config")
		}
		profileName = cfg.Profiles[0].Name
	}
	index := -1
	for i := range cfg.Profiles {
		if strings.EqualFold(cfg.Profiles[i].Name, profileName) {
			index = i
			break
		}
	}
	if index < 0 {
		return Config{}, fmt.Errorf("override profile %q not found", profileName)
	}
	profile := &cfg.Profiles[index]
	if values["clientId"] != "" {
		profile.ClientID = values["clientId"]
	}
	if values["teamId"] != "" {
		profile.TeamID = values["teamId"]
	}
	if values["keyId"] != "" {
		profile.KeyID = values["keyId"]
	}
	if values["privateKeyPath"] != "" {
		profile.PrivateKeyPath = values["privateKeyPath"]
	}
	if values["adAccountId"] != "" {
		profile.DefaultAdAccountID = values["adAccountId"]
	}
	if allowWritesValue != "" {
		allowWrites, err := strconv.ParseBool(allowWritesValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse APPLE_ADS_ALLOW_WRITES: %w", err)
		}
		profile.AllowWrites = allowWrites
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate environment overrides: %w", err)
	}
	return cfg, nil
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
