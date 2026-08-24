//go:build live_read_only

package live

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/mcpserver"
)

func TestMCPReadOnlyAcceptance(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_READ_ONLY") != "1" {
		t.Skip("live read-only acceptance is opt-in")
	}
	cfg, _, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := cfg.ResolveProfile(os.Getenv("APPLE_ADS_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := os.Getenv("APPLE_ADS_AD_ACCOUNT_ID")
	query := os.Getenv("APPLE_ADS_LIVE_APP_QUERY")
	adamID := os.Getenv("APPLE_ADS_LIVE_ADAM_ID")
	storefront := os.Getenv("APPLE_ADS_LIVE_STOREFRONT")
	if accountID == "" || query == "" || adamID == "" {
		t.Fatal("live MCP acceptance requires account, app query, and Adam ID")
	}
	if storefront == "" {
		storefront = "US"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPReadOnlyHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_HELPER=1", "APPLE_ADS_ALLOW_WRITES=false")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-read-only-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for _, call := range []struct {
		name      string
		arguments map[string]any
		validate  func(any) bool
	}{
		{name: "auth_check", arguments: map[string]any{"profile": profile.Name}},
		{name: "apps_search", arguments: map[string]any{
			"profile": profile.Name, "adAccountId": accountID, "query": query,
			"returnOwnedApps": true, "storefronts": []string{storefront}, "limit": 20,
		}, validate: func(value any) bool { return containsFieldValue(value, "adamId", adamID) }},
		{name: "keyword_suggestions", arguments: map[string]any{
			"profile": profile.Name, "adAccountId": accountID,
			"filters": []any{
				map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{adamID}},
				map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
				map[string]any{"field": "countriesOrRegions", "operator": "IN", "value": []string{storefront}},
			},
			"pagination": map[string]any{"offset": 0, "pageSize": 50},
		}, validate: hasNonEmptyArray},
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.arguments})
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if result.IsError || result.StructuredContent == nil {
			t.Fatalf("%s returned an MCP error", call.name)
		}
		if call.validate != nil && !call.validate(result.StructuredContent) {
			t.Fatalf("%s returned no expected data", call.name)
		}
	}
}

func TestMCPReadOnlyHelper(t *testing.T) {
	if os.Getenv("APPLE_ADS_MCP_LIVE_HELPER") != "1" {
		return
	}
	cfg, source, err := config.Load("")
	if err != nil {
		os.Exit(2)
	}
	if err := mcpserver.RunStdio(context.Background(), appleads.NewManager(cfg, source), false); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
