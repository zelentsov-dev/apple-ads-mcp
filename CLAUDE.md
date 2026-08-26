# Apple Ads MCP

## Product

`apple-ads-mcp` is a public, local-first Model Context Protocol server for the Apple Ads Platform API v1. It supports App Store advertising only. Apple Maps and the legacy Campaign Management API v5 are out of scope.

The project is independent and is not affiliated with, endorsed by, or sponsored by Apple Inc.

## Architecture

- Go 1.26.7+
- Official MCP Go SDK
- `cmd/apple-ads-mcp` contains only CLI wiring
- Production code lives under `internal/`
- `stdout` is reserved for MCP JSON-RPC; diagnostics go to `stderr`
- Production Apple API and OAuth base URLs are fixed constants
- Credentials remain local and tokens are cached only in memory

## Safety

- The default server mode is read-only.
- Write access requires the server flag, profile permission, Apple role, preview, and a single-use receipt.
- Never add a raw HTTP passthrough tool.
- Never log private keys, client secrets, access tokens, authorization headers, or request bodies containing credentials.
- Read operations may retry bounded transient failures. Mutations must not retry after an ambiguous transport failure.
- Identifiers and decimal money amounts remain strings in public MCP schemas.

## Development

- Use idiomatic Go and wrap errors with context.
- Keep exported surface area small; prefer `internal/` packages.
- Add table-driven tests for validation and failure behavior.
- Run `go test ./...`, `go vet ./...`, skill validation, and plugin validation before handoff.
- Do not perform live Apple Ads mutations from automated tests.
