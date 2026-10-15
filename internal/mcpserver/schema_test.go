package mcpserver

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"io"
	"math"
	"reflect"
	"testing"
)

func TestMCPMutationSchemasExposeWriteConstraints(t *testing.T) {
	clientSession := connectTestServer(t, New(appleads.NewManager(config.Config{}, "none"), false, io.Discard))
	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}

	statusCases := []struct {
		tool string
		path []string
	}{
		{"campaign_create_preview", []string{"payload", "status"}},
		{"campaign_update_preview", []string{"payload", "status"}},
		{"ad_group_create_preview", []string{"payload", "status"}},
		{"ad_group_update_preview", []string{"payload", "status"}},
		{"keyword_create_preview", []string{"payload", "status"}},
		{"keyword_update_preview", []string{"payload", "status"}},
		{"negative_keyword_create_preview", []string{"payload", "status"}},
		{"negative_keyword_update_preview", []string{"payload", "status"}},
		{"ad_create_preview", []string{"payload", "status"}},
		{"ad_update_preview", []string{"payload", "status"}},
		{"keywords_bulk_create_preview", []string{"items", "[]", "status"}},
		{"keywords_bulk_update_preview", []string{"items", "[]", "status"}},
		{"negative_keywords_bulk_create_preview", []string{"items", "[]", "status"}},
		{"negative_keywords_bulk_update_preview", []string{"items", "[]", "status"}},
	}
	for _, test := range statusCases {
		t.Run(test.tool+"_status", func(t *testing.T) {
			node := toolSchemaNode(t, tools[test.tool], test.path...)
			if !reflect.DeepEqual(node["enum"], []any{"ENABLED", "PAUSED"}) {
				t.Fatalf("enum=%v", node["enum"])
			}
		})
	}

	for _, toolName := range []string{
		"keywords_bulk_create_preview",
		"keywords_bulk_update_preview",
		"negative_keywords_bulk_create_preview",
		"negative_keywords_bulk_update_preview",
	} {
		t.Run(toolName+"_correlation", func(t *testing.T) {
			node := toolSchemaNode(t, tools[toolName], "items", "[]", "correlationId")
			branches, ok := node["oneOf"].([]any)
			if !ok || len(branches) != 2 {
				t.Fatalf("oneOf=%v", node["oneOf"])
			}
			integer := branches[0].(map[string]any)
			if integer["type"] != "integer" || integer["minimum"] != float64(0) || integer["exclusiveMaximum"] != math.Exp2(63) {
				t.Fatalf("integer branch=%v", integer)
			}
			decimalString := branches[1].(map[string]any)
			if decimalString["type"] != "string" || decimalString["minLength"] != float64(1) || decimalString["pattern"] != `^[0-9]+$` {
				t.Fatalf("string branch=%v", decimalString)
			}
		})
	}

}

func TestMCPMutationSchemasRejectInvalidArgumentsBeforeHandlers(t *testing.T) {
	clientSession := connectTestServer(t, New(appleads.NewManager(config.Config{}, "none"), false, io.Discard))
	keyword := func(status, amount string) map[string]any {
		payload := map[string]any{"adGroupId": "456", "text": "voice notes", "matchType": "EXACT", "status": status}
		if amount != "" {
			payload["bid"] = map[string]any{"amount": amount, "currency": "USD"}
		}
		return map[string]any{"profile": "owner", "adAccountId": "123", "payload": payload}
	}
	bulk := func(correlationID any) map[string]any {
		return map[string]any{
			"profile": "owner", "adAccountId": "123", "adGroupId": "456",
			"items": []any{map[string]any{
				"correlationId": correlationID, "text": "voice notes", "matchType": "EXACT", "status": "PAUSED",
			}},
		}
	}

	invalidCases := []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{"active_status", "keyword_create_preview", keyword("ACTIVE", "")},
		{"descriptive_correlation", "keywords_bulk_create_preview", bulk("k1")},
		{"zero_bid", "keyword_create_preview", keyword("PAUSED", "0")},
		{"leading_zero_bid", "keyword_create_preview", keyword("PAUSED", "01.20")},
	}
	for _, test := range invalidCases {
		t.Run(test.name, func(t *testing.T) {
			result := callTool(t, clientSession, test.tool, test.arguments)
			if !result.IsError || result.StructuredContent != nil {
				t.Fatalf("result=%+v", result)
			}
		})
	}

	validCases := []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{"enabled_status", "keyword_create_preview", keyword("ENABLED", "")},
		{"one_decimal_bid", "keyword_create_preview", keyword("PAUSED", "1.2")},
		{"two_decimal_bid", "keyword_create_preview", keyword("PAUSED", "1.20")},
		{"three_decimal_bid", "keyword_create_preview", keyword("PAUSED", "1.234")},
		{"subcent_bid", "keyword_create_preview", keyword("PAUSED", "0.001")},
		{"integer_correlation", "keywords_bulk_create_preview", bulk(0)},
		{"digit_string_correlation", "keywords_bulk_create_preview", bulk("1")},
	}
	for _, test := range validCases {
		t.Run(test.name, func(t *testing.T) {
			result := callTool(t, clientSession, test.tool, test.arguments)
			if !result.IsError || result.StructuredContent == nil {
				t.Fatalf("result=%+v", result)
			}
			structured := result.StructuredContent.(map[string]any)
			diagnostic := structured["error"].(map[string]any)
			if diagnostic["type"] != "write_gate_error" {
				t.Fatalf("diagnostic=%v", diagnostic)
			}
		})
	}
}

func connectTestServer(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "contract-test", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		serverSession.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clientSession.Close()
		serverSession.Close()
	})
	return clientSession
}

func toolSchemaNode(t *testing.T, tool *mcp.Tool, path ...string) map[string]any {
	t.Helper()
	if tool == nil {
		t.Fatal("tool is missing")
	}
	node := tool.InputSchema.(map[string]any)
	for _, part := range path {
		if part == "[]" {
			node = node["items"].(map[string]any)
			continue
		}
		properties := node["properties"].(map[string]any)
		node = properties[part].(map[string]any)
	}
	return node
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMCPStrategySchemaPairsAndDeprecatedCap(t *testing.T) {
	session := connectTestServer(t, New(appleads.NewManager(config.Config{}, "none"), false, io.Discard))
	for _, test := range []struct {
		name     string
		strategy map[string]any
		valid    bool
	}{
		{"manual", map[string]any{"bidStrategyType": "MANUAL_CPT", "bidStrategyGoal": "TAP", "bid": map[string]any{"amount": "0.001", "currency": "USD"}}, true},
		{"auto_echo", map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "INSTALL", "bid": map[string]any{"amount": "0.00", "currency": "USD"}}, true},
		{"bid_only", map[string]any{"bid": map[string]any{"amount": "1.234", "currency": "USD"}}, true},
		{"missing_goal", map[string]any{"bidStrategyType": "MANUAL_CPT"}, false},
		{"missing_type", map[string]any{"bidStrategyGoal": "TAP"}, false},
		{"mismatched", map[string]any{"bidStrategyType": "MAX_CONVERSIONS", "bidStrategyGoal": "TAP"}, false},
		{"maps", map[string]any{"bidStrategyType": "MANUAL_CPM", "bidStrategyGoal": "IMPRESSION"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := callTool(t, session, "ad_group_update_preview", map[string]any{
				"profile": "test", "adAccountId": "123", "id": "456", "payload": map[string]any{"bidStrategy": test.strategy},
			})
			if !result.IsError {
				t.Fatalf("read-only server must reject write preview: %+v", result)
			}
			if test.valid {
				diagnostic, ok := result.StructuredContent.(map[string]any)
				if !ok || diagnostic["error"].(map[string]any)["type"] != "write_gate_error" {
					t.Fatalf("valid schema failed: %+v", result)
				}
			} else if result.StructuredContent != nil {
				t.Fatalf("invalid pair reached handler: %+v", result)
			}
		})
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "ad_group_create_preview" || tool.Name == "ad_group_update_preview" {
			if toolSchemaNode(t, tool, "payload", "cpaCap")["deprecated"] != true {
				t.Fatal("cpaCap annotation is missing")
			}
		}
	}
}
