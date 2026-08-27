package mcpserver

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
	toolset "github.com/zelentsov-dev/apple-ads-mcp/internal/tools"
)

var Version = "0.3.2"

type Options struct {
	AllowWrites  bool
	AllowDeletes bool
	PolicyPath   string
	BillingPath  string
	HistoryRoot  string
}

func New(manager *appleads.Manager, allowWrites bool, logWriter io.Writer) *mcp.Server {
	return NewWithOptions(manager, Options{AllowWrites: allowWrites}, logWriter)
}

func NewWithOptions(manager *appleads.Manager, options Options, logWriter io.Writer) *mcp.Server {
	if logWriter == nil {
		logWriter = os.Stderr
	}
	logger := slog.New(slog.NewJSONHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo}))
	server := mcp.NewServer(&mcp.Implementation{
		Name: "apple-ads-mcp", Version: Version, WebsiteURL: "https://github.com/zelentsov-dev/apple-ads-mcp",
	}, &mcp.ServerOptions{
		Instructions: "Use explicit profile and adAccountId values. Verify app ownership before recommendations. Writes require a preview receipt and operations_apply. Optimization is on-demand. Deletes require separate destructive gates.",
		Logger:       logger,
		PageSize:     100,
	})
	service := toolset.NewServiceWithOptions(manager, options.AllowWrites, options.AllowDeletes, Version, options.PolicyPath, options.BillingPath, options.HistoryRoot)
	service.RegisterReadTools(server)
	service.RegisterMutationTools(server, operations.NewStore())
	return server
}

func RunStdio(ctx context.Context, manager *appleads.Manager, allowWrites bool) error {
	return New(manager, allowWrites, os.Stderr).Run(ctx, &mcp.StdioTransport{})
}

func RunStdioWithOptions(ctx context.Context, manager *appleads.Manager, options Options) error {
	return NewWithOptions(manager, options, os.Stderr).Run(ctx, &mcp.StdioTransport{})
}
