package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

const (
	TokenEndpoint = "https://appleid.apple.com/auth/oauth2/token"
	TokenScope    = "searchadsorg"
	tokenAudience = "https://appleid.apple.com"
	maxTokenBody  = 1 << 20
	maxKeyBody    = 32 << 10
)

type Token struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

type Source struct {
	profile    config.Profile
	httpClient *http.Client
	endpoint   string
	now        func() time.Time

	mu    sync.Mutex
	token Token
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func NewSource(profile config.Profile) *Source {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxIdleConns = 20
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	return newSource(profile, &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: rejectRedirect}, TokenEndpoint, time.Now)
}

func NewSourceForTest(profile config.Profile, client *http.Client, endpoint string, now func() time.Time) *Source {
	return newSource(profile, client, endpoint, now)
}

func newSource(profile config.Profile, client *http.Client, endpoint string, now func() time.Time) *Source {
	return &Source{profile: profile, httpClient: client, endpoint: endpoint, now: now}
}

func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.token.AccessToken != "" && now.Add(60*time.Second).Before(s.token.ExpiresAt) {
		return s.token.AccessToken, nil
	}
	token, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token = token
	return token.AccessToken, nil
}

func (s *Source) Invalidate() {
	s.mu.Lock()
	s.token = Token{}
	s.mu.Unlock()
}

func (s *Source) ClientSecret(now time.Time) (string, error) {
	data, err := readPrivateKey(s.profile.PrivateKeyPath)
	if err != nil {
		return "", errors.New("private key is unavailable or insecure")
	}
	privateKey, err := parsePrivateKey(data)
	if err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"iss": s.profile.TeamID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"aud": tokenAudience,
		"sub": s.profile.ClientID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = s.profile.KeyID
	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("sign client secret: %w", err)
	}
	return signed, nil
}

func readPrivateKey(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("private key is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("private key permissions are not owner-only")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxKeyBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxKeyBody {
		return nil, errors.New("private key exceeds size limit")
	}
	return data, nil
}

func rejectRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (s *Source) fetch(ctx context.Context) (Token, error) {
	now := s.now()
	secret, err := s.ClientSecret(now)
	if err != nil {
		return Token{}, fmt.Errorf("create client secret: %w", err)
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.profile.ClientID},
		"client_secret": {secret},
		"scope":         {TokenScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("request access token: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenBody+1))
	if err != nil {
		return Token{}, fmt.Errorf("read token response: %w", err)
	}
	if len(body) > maxTokenBody {
		return Token{}, errors.New("token response exceeds size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Token{}, fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}
	var decoded tokenResponse
	if err := decodeJSON(body, &decoded); err != nil {
		return Token{}, fmt.Errorf("decode token response: %w", err)
	}
	if decoded.AccessToken == "" || decoded.ExpiresIn <= 0 {
		return Token{}, errors.New("token response is missing access_token or expires_in")
	}
	return Token{
		AccessToken: decoded.AccessToken,
		TokenType:   decoded.TokenType,
		ExpiresAt:   now.Add(time.Duration(decoded.ExpiresIn) * time.Second),
	}, nil
}

func parsePrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ecdsaKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS8 private key is not ECDSA")
		}
		if ecdsaKey.Curve.Params().BitSize != 256 {
			return nil, errors.New("private key must use the P-256 curve")
		}
		return ecdsaKey, nil
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ECDSA private key: %w", err)
	}
	if key.Curve.Params().BitSize != 256 {
		return nil, errors.New("private key must use the P-256 curve")
	}
	return key, nil
}

func decodeJSON(data []byte, out any) error {
	return json.Unmarshal(data, out)
}
