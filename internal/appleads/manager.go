package appleads

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/auth"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

type Manager struct {
	config config.Config
	source string

	mu      sync.Mutex
	clients map[string]*Client
}

func NewManager(cfg config.Config, source string) *Manager {
	return &Manager{config: cfg, source: source, clients: make(map[string]*Client)}
}

func (m *Manager) Profiles() []config.PublicProfile {
	return m.config.PublicProfiles(m.source)
}

func (m *Manager) Resolve(profileName, adAccountID string) (config.Profile, string, error) {
	profile, err := m.config.ResolveProfile(profileName)
	if err != nil {
		return config.Profile{}, "", err
	}
	account := strings.TrimSpace(adAccountID)
	if account == "" {
		account = strings.TrimSpace(profile.DefaultAdAccountID)
	}
	if account == "" {
		return config.Profile{}, "", errors.New("adAccountId is required because the profile has no default")
	}
	return profile, account, nil
}

func (m *Manager) Profile(profileName string) (config.Profile, error) {
	return m.config.ResolveProfile(profileName)
}

func (m *Manager) Do(ctx context.Context, profileName, adAccountID string, operation Operation) (Result, error) {
	profile, err := m.config.ResolveProfile(profileName)
	if err != nil {
		return Result{}, err
	}
	account := strings.TrimSpace(adAccountID)
	if operation.RequiresAccount() {
		_, account, err = m.Resolve(profileName, adAccountID)
		if err != nil {
			return Result{}, err
		}
	}
	return m.client(profile).Do(ctx, account, operation)
}

func (m *Manager) client(profile config.Profile) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client, ok := m.clients[strings.ToLower(profile.Name)]; ok {
		return client
	}
	client := NewClient(auth.NewSource(profile))
	m.clients[strings.ToLower(profile.Name)] = client
	return client
}

func (m *Manager) ValidateCredentials(ctx context.Context, profileName, adAccountID string) (Result, error) {
	result, err := m.Do(ctx, profileName, adAccountID, Me())
	if err != nil {
		return Result{}, fmt.Errorf("authenticate profile: %w", err)
	}
	return result, nil
}
