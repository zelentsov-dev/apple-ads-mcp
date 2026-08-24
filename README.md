# Apple Ads MCP

Local-first Model Context Protocol server for the Apple Ads Platform API v1. It gives Codex, Claude, and other MCP clients typed tools for App Store campaign research, reporting, and safe campaign management.

This project is independent and is not affiliated with, endorsed by, or sponsored by Apple Inc. Apple Ads is a trademark of Apple Inc.

## Status

The v0.1 read-only intelligence layer and receipt-gated write foundation are implemented. A private, read-only acceptance has passed against an operator-controlled Apple Ads account. Apple Maps and the legacy Campaign Management API v5 are intentionally excluded.

## Requirements

- An Apple Ads API user and ES256 private key
- Go 1.26.6+ when building from source
- macOS, Linux, or Windows

## Build

```bash
go build -o bin/apple-ads-mcp ./cmd/apple-ads-mcp
```

## Configure

Create a profile interactively:

```bash
apple-ads-mcp config init
```

Or copy `accounts.example.json` to `~/.config/apple-ads-mcp/accounts.json`. On POSIX systems, set the config and private key to mode `0600`; the server rejects files readable by group or other users. The config stores metadata and the private-key path, never the private key itself.

```bash
chmod 600 ~/.config/apple-ads-mcp/accounts.json /absolute/path/to/apple-ads-private-key.pem
```

Validate authentication and discover accessible ad accounts:

```bash
apple-ads-mcp auth doctor --profile example-profile
apple-ads-mcp accounts discover --profile example-profile
```

## Run as MCP

```bash
apple-ads-mcp serve --stdio
```

Write tools are disabled unless the process starts with `--allow-writes`, the selected profile has `allowWrites: true`, and Apple ACLs show a write role for the explicit ad account. Mutations still require preview and a single-use receipt.

Every account-scoped MCP tool requires explicit `profile` and `adAccountId` inputs. Account discovery takes only `profile` because its purpose is to find the available account IDs.

## Tool families

- Access, profiles, authentication, ad accounts, and organizations
- Owned-app search, app details, and storefront eligibility
- Keyword, phrase, category, and target-CPA suggestions
- Search popularity, impression share, five report levels, and recommendations
- Change history, account health, app opportunities, and campaign audit
- Read-only Default and Custom Product Page discovery
- Specialized create/update previews followed by `operations_apply`

There is no generic raw request tool. Responses provide structured content, concise text, bounded arrays, pagination metadata, rate-limit metadata, and structured Apple errors.

### Claude Code

```bash
claude mcp add apple-ads -- apple-ads-mcp serve --stdio
```

### Codex plugin

The repository includes `.codex-plugin/plugin.json`, `.mcp.json`, and the `apple-ads-operator` skill. The plugin expects `apple-ads-mcp` to be available in `PATH`.

### Docker / OCI

The release workflow publishes `ghcr.io/zelentsov-dev/apple-ads-mcp`. Mount `accounts.json` and private keys read-only, set `APPLE_ADS_MCP_CONFIG` to the mounted config path, and run with the host user when owner-only key permissions are used.

## Configuration precedence

1. `--config`
2. `APPLE_ADS_MCP_CONFIG`
3. `~/.config/apple-ads-mcp/accounts.json`
4. Single-profile `APPLE_ADS_*` environment variables

Required environment variables for a single profile are `APPLE_ADS_CLIENT_ID`, `APPLE_ADS_TEAM_ID`, `APPLE_ADS_KEY_ID`, and `APPLE_ADS_PRIVATE_KEY_PATH`. Optional variables are `APPLE_ADS_PROFILE`, `APPLE_ADS_AD_ACCOUNT_ID`, and `APPLE_ADS_ALLOW_WRITES`.

When a config file is loaded, non-empty `APPLE_ADS_*` credential values override the selected profile. Set `APPLE_ADS_PROFILE` for a multi-profile file; a single profile is selected automatically.

## Security model

- OAuth tokens stay in memory.
- MCP identifiers and money amounts are serialized as strings.
- List responses are bounded and paginated.
- Production endpoints cannot be overridden through user configuration.
- Read requests use bounded transient retries and respect `Retry-After`.
- Mutations never retry an ambiguous transport failure.
- There is no raw HTTP request tool.

## Development

```bash
go test ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

Live tests are opt-in and read-only. The project has no Apple Ads sandbox and never performs live mutations in CI.

The integration suite launches the real stdio server as a child process through the official Go SDK. The upstream MCP conformance runner currently accepts server URLs but cannot launch stdio servers, so runner coverage remains a v1.0 gate until upstream stdio support is available.

See [the roadmap](docs/ROADMAP.md), [the endpoint matrix](api-contract/operations.json), and [the security policy](SECURITY.md).

The primary contract is [Apple Ads Platform API v1](https://developer.apple.com/documentation/apple-ads-platform-api). The [Apple-maintained Java client](https://github.com/apple/apple-ads-platform-api-java) is a secondary model and endpoint reference, not a runtime dependency.
