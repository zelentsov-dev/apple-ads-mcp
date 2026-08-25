package mcpserver

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestMCPHandshakeSchemasAndStructuredOutput(t *testing.T) {
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := New(appleads.NewManager(config.Config{}, "none"), false, io.Discard)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "contract-test", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) < 40 {
		t.Fatalf("listed %d tools", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.Name == "raw_request" || tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("invalid tool contract: %+v", tool)
		}
		if tool.Name == "keyword_suggestions" {
			schema := tool.InputSchema.(map[string]any)
			properties := schema["properties"].(map[string]any)
			if _, raw := properties["request"]; raw {
				t.Fatal("query tools must not expose a raw request object")
			}
			if _, typed := properties["filters"]; !typed {
				t.Fatal("query tools must expose typed filters")
			}
		}
		if tool.Name == "campaign_create_preview" || tool.Name == "creative_create_preview" {
			schema := tool.InputSchema.(map[string]any)
			if schema["additionalProperties"] != false {
				t.Fatalf("%s input schema must reject unknown fields", tool.Name)
			}
			properties := schema["properties"].(map[string]any)
			payload := properties["payload"].(map[string]any)
			if payload["additionalProperties"] != false {
				t.Fatalf("%s payload must be resource-specific", tool.Name)
			}
		}
		if tool.Name == "ad_group_create_preview" {
			schema := tool.InputSchema.(map[string]any)
			payload := schema["properties"].(map[string]any)["payload"].(map[string]any)
			required := payload["required"].([]any)
			found := false
			for _, field := range required {
				found = found || field == "startTime"
			}
			if !found {
				t.Fatal("ad-group create schema must require startTime")
			}
		}
		if tool.Name == "shared_budget_create_preview" {
			schema := tool.InputSchema.(map[string]any)
			properties := schema["properties"].(map[string]any)
			for _, forbidden := range []string{"invoiceDetail", "billingEmail", "primaryBuyerEmail", "adAccountIds"} {
				if _, exists := properties[forbidden]; exists {
					t.Fatalf("shared budget schema exposes private or server-owned field %s", forbidden)
				}
			}
			if _, exists := properties["billingProfile"]; !exists {
				t.Fatal("shared budget schema must reference a local billing profile")
			}
		}
		if strings.Contains(tool.Name, "delete_preview") {
			schema := tool.InputSchema.(map[string]any)
			properties := schema["properties"].(map[string]any)
			if _, exists := properties["expectedText"]; !exists {
				t.Fatalf("%s must require expectedText", tool.Name)
			}
		}
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "server_info", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil || len(result.Content) == 0 {
		t.Fatalf("unexpected server_info result: %+v", result)
	}
}
