//go:build live_read_only

package live

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestMCPV03OptimizationReadOnly(t *testing.T) {
	policy := os.Getenv("APPLE_ADS_LIVE_OPTIMIZATION_POLICY")
	if os.Getenv("APPLE_ADS_LIVE_READ_ONLY") != "1" || policy == "" {
		t.Skip("v0.3 optimization acceptance requires the read-only opt-in and a named policy")
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
	if accountID == "" {
		t.Fatal("APPLE_ADS_AD_ACCOUNT_ID is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPReadOnlyHelper$")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_LIVE_HELPER=1", "APPLE_ADS_ALLOW_WRITES=false")
	client := mcp.NewClient(&mcp.Implementation{Name: "live-v03-optimizer-test", Version: "0.3.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	serverInfo := callReadTool(t, ctx, session, "server_info", map[string]any{})
	if !containsReadFieldValue(serverInfo, "contractVersion", "0.3") {
		t.Fatal("server_info did not advertise the v0.3 contract")
	}
	callReadTool(t, ctx, session, "optimization_policies_list", map[string]any{})
	input := map[string]any{"profile": profile.Name, "adAccountId": accountID, "policy": policy}
	policyResult := callReadTool(t, ctx, session, "optimization_policy_get", input)
	if !containsReadFieldValue(policyResult, "name", policy) {
		t.Fatal("optimization_policy_get returned a different policy")
	}
	callReadTool(t, ctx, session, "optimization_baseline", input)
	callReadTool(t, ctx, session, "optimization_plan", input)
	callReadTool(t, ctx, session, "optimization_history", input)

	account := callReadTool(t, ctx, session, "ad_account_get", map[string]any{"profile": profile.Name, "adAccountId": accountID})
	if containsReadFieldValue(account, "paymentModel", "PAYG") {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "shared_budget_create_preview", Arguments: map[string]any{
			"profile": profile.Name, "adAccountId": accountID, "name": "Apple Ads MCP v0.3 PAYG Eligibility Probe",
			"startTime": time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
			"value":     map[string]any{"amount": "1.00", "currency": "USD"}, "billingProfile": "not-resolved-on-payg",
		}})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		if !result.IsError || !bytes.Contains(bytes.ToLower(data), []byte("not_eligible")) {
			t.Fatalf("PAYG shared-budget preview did not return not_eligible: %s", data)
		}
	}
}

func callReadTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) any {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.IsError || result.StructuredContent == nil {
		data, _ := json.Marshal(result.StructuredContent)
		t.Fatalf("%s returned an MCP error: %s", name, data)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		t.Fatal(err)
	}
	return normalized
}

func containsReadFieldValue(value any, field, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[field].(string); ok && current == expected {
			return true
		}
		for _, item := range typed {
			if containsReadFieldValue(item, field, expected) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsReadFieldValue(item, field, expected) {
				return true
			}
		}
	}
	return false
}
