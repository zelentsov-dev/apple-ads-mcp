package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestRealStdioTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestStdioHelperProcess")
	command.Env = append(os.Environ(), "APPLE_ADS_MCP_STDIO_HELPER=1")
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "server_info", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil {
		t.Fatalf("stdio result=%+v", result)
	}
}

func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("APPLE_ADS_MCP_STDIO_HELPER") != "1" {
		return
	}
	err := RunStdio(context.Background(), appleads.NewManager(config.Config{}, "none"), false)
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
