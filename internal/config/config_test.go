package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	t.Setenv("APPLE_ADS_CLIENT_ID", "environment-client")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accounts.json")
	cfg := Config{Profiles: []Profile{{Name: "file", ClientID: "file-client", TeamID: "team", KeyID: "key", PrivateKeyPath: keyPath}}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, source, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if source != path || loaded.Profiles[0].ClientID != "environment-client" {
		t.Fatalf("explicit config did not win: source=%q config=%+v", source, loaded)
	}
}

func TestEnvironmentProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPLE_ADS_CLIENT_ID", "client")
	t.Setenv("APPLE_ADS_TEAM_ID", "team")
	t.Setenv("APPLE_ADS_KEY_ID", "key")
	t.Setenv("APPLE_ADS_PRIVATE_KEY_PATH", keyPath)
	t.Setenv("APPLE_ADS_ALLOW_WRITES", "true")
	t.Setenv("APPLE_ADS_ALLOW_DELETES", "true")
	cfg, source, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if source != "environment" || !cfg.Profiles[0].AllowWrites || !cfg.Profiles[0].AllowDeletes {
		t.Fatalf("unexpected environment config: source=%q config=%+v", source, cfg)
	}
}

func TestDeleteEnvironmentGateDoesNotOverrideFileOptIn(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accounts.json")
	if err := Save(path, Config{Profiles: []Profile{{Name: "file", ClientID: "client", TeamID: "team", KeyID: "key", PrivateKeyPath: keyPath, AllowWrites: true}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPLE_ADS_ALLOW_DELETES", "true")
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profiles[0].AllowDeletes {
		t.Fatal("session delete gate must not replace the profile allowDeletes opt-in")
	}
}

func TestMultiProfileOverrideRequiresSelection(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accounts.json")
	cfg := Config{Profiles: []Profile{
		{Name: "one", ClientID: "client-1", TeamID: "team", KeyID: "key", PrivateKeyPath: keyPath},
		{Name: "two", ClientID: "client-2", TeamID: "team", KeyID: "key", PrivateKeyPath: keyPath},
	}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPLE_ADS_CLIENT_ID", "override")
	t.Setenv("APPLE_ADS_PROFILE", "")
	if _, _, err := Load(path); err == nil {
		t.Fatal("expected explicit profile requirement")
	}
	t.Setenv("APPLE_ADS_PROFILE", "two")
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profiles[0].ClientID != "client-1" || loaded.Profiles[1].ClientID != "override" {
		t.Fatalf("unexpected override: %+v", loaded.Profiles)
	}
}

func TestSavePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "accounts.json")
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Profiles: []Profile{{Name: "default", ClientID: "client", TeamID: "team", KeyID: "key", PrivateKeyPath: keyPath}}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %04o", info.Mode().Perm())
		}
	}
	if err := Save(path, cfg); err == nil {
		t.Fatal("expected exclusive config creation")
	}
}

func TestPublicProfilesRedactPathsAndAccountIDs(t *testing.T) {
	cfg := Config{Profiles: []Profile{{Name: "default", DefaultAdAccountID: "123", AllowWrites: true, AllowDeletes: true}}}
	profiles := cfg.PublicProfiles("/config/accounts.json")
	if len(profiles) != 1 || profiles[0].CredentialSource != "file" || profiles[0].Name != "default" || !profiles[0].AllowWrites || !profiles[0].AllowDeletes {
		t.Fatalf("profiles=%+v", profiles)
	}
}

func TestPrivateKeyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not available")
	}
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := Profile{PrivateKeyPath: path}
	if err := profile.ValidatePrivateKeyFile(); err == nil {
		t.Fatal("expected insecure key permissions to fail")
	}
}

func TestLoadRejectsInsecureConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not available")
	}
	path := filepath.Join(t.TempDir(), "accounts.json")
	data := `{"profiles":[{"name":"x","clientId":"c","teamId":"t","keyId":"k","privateKeyPath":"/config/key.p8"}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("expected insecure config permissions to fail")
	}
}

func TestProductionBaseURLCannotBeConfigured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	data := `{"profiles":[{"name":"x","clientId":"c","teamId":"t","keyId":"k","privateKeyPath":"/tmp/key","baseUrl":"https://example.com"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("expected unknown baseUrl field to fail")
	}
}
