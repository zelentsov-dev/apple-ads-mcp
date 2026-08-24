package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestClientSecretClaims(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	profile, key := testProfile(t)
	source := NewSourceForTest(profile, http.DefaultClient, TokenEndpoint, func() time.Time { return now })
	secret, err := source.ClientSecret(now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(secret, func(token *jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(tokenAudience), jwt.WithSubject(profile.ClientID), jwt.WithIssuer(profile.TeamID), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil || !parsed.Valid {
		t.Fatalf("invalid client secret: %v", err)
	}
	if parsed.Header["kid"] != profile.KeyID || parsed.Method.Alg() != "ES256" {
		t.Fatalf("unexpected JWT header: %+v", parsed.Header)
	}
}

func TestTokenCacheConcurrentRefresh(t *testing.T) {
	profile, _ := testProfile(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != TokenScope {
			t.Errorf("unexpected form: %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "token_type": "Bearer", "expires_in": 3600})
	}))
	defer server.Close()
	source := NewSourceForTest(profile, server.Client(), server.URL, time.Now)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if token, err := source.Token(context.Background()); err != nil || token != "token" {
				t.Errorf("token=%q err=%v", token, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("token endpoint called %d times", calls.Load())
	}
}

func TestTokenErrorDoesNotLeakBody(t *testing.T) {
	profile, _ := testProfile(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"client_secret":"do-not-leak"}`))
	}))
	defer server.Close()
	source := NewSourceForTest(profile, server.Client(), server.URL, time.Now)
	_, err := source.Token(context.Background())
	if err == nil || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTokenClientRejectsRedirect(t *testing.T) {
	profile, _ := testProfile(t)
	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client := redirect.Client()
	client.CheckRedirect = rejectRedirect
	source := NewSourceForTest(profile, client, redirect.URL, time.Now)
	if _, err := source.Token(context.Background()); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target called %d times", targetCalls.Load())
	}
}

func TestClientSecretRejectsInsecureKeyWithoutPathLeak(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not available")
	}
	profile, _ := testProfile(t)
	if err := os.Chmod(profile.PrivateKeyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	source := NewSourceForTest(profile, http.DefaultClient, TokenEndpoint, time.Now)
	_, err := source.ClientSecret(time.Now())
	if err == nil || strings.Contains(err.Error(), profile.PrivateKeyPath) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testProfile(t *testing.T) (config.Profile, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.Profile{Name: "test", ClientID: "client", TeamID: "team", KeyID: "key", PrivateKeyPath: path}, key
}
