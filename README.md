# Apple Ads MCP

[![CI](https://github.com/zelentsov-dev/apple-ads-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/zelentsov-dev/apple-ads-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zelentsov-dev/apple-ads-mcp)](https://github.com/zelentsov-dev/apple-ads-mcp/releases)
[![Go](https://img.shields.io/badge/Go-1.26.6%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/github/license/zelentsov-dev/apple-ads-mcp)](LICENSE)

A local-first [Model Context Protocol](https://modelcontextprotocol.io/) server for the [Apple Ads Platform API v1](https://developer.apple.com/documentation/apple-ads-platform-api). It gives Codex, Claude, and other MCP clients typed tools for App Store advertising research, reporting, auditing, and receipt-gated campaign management.

Apple Ads MCP is independent open-source software. It is not affiliated with, endorsed by, or sponsored by Apple Inc. Apple Ads and App Store are trademarks of Apple Inc.

## Why Apple Ads MCP

- **Agent-first tools:** discover owned apps, research keywords, inspect reports, audit campaigns, and prepare narrowly scoped changes.
- **Local credentials:** the private key stays on your machine; OAuth tokens and operation receipts stay in process memory.
- **Read-only by default:** research and reporting work without enabling mutations.
- **Explicit account routing:** every account-scoped tool requires a profile and `adAccountId`.
- **Safe mutations:** every write requires a preview, a ten-minute single-use receipt, and a current-state recheck.
- **Bounded output:** structured MCP content, capped arrays, pagination, rate-limit metadata, and structured Apple errors.
- **Portable runtime:** one Go binary for macOS, Linux, and Windows, plus an OCI image.

## Release status

`v0.1.0` is the first public release. The complete local test suite, race detector, static analysis, vulnerability scan, secret scan, distribution validation, and real MCP stdio tests pass.

A private live acceptance against an operator-controlled Apple Ads account verified:

- OAuth, account discovery, organization and owned-app lookup;
- storefront availability, keyword suggestions, popularity insights, and bounded reports;
- preview → receipt → apply → direct API readback;
- paused campaign, ad-group, targeting-keyword, and negative-keyword creation;
- drift rejection, single-use receipts, read-only restoration, and zero ad spend.

No credentials, account exports, private campaign data, or live receipts are stored in this repository.

## Capabilities

### Read tools

| Area | Tools |
| --- | --- |
| Server and access | `server_info`, `profiles_list`, `auth_check`, `ad_accounts_list`, `org_get` |
| Apps and product pages | `apps_search`, `apps_get`, `apps_eligibility`, `product_page_get`, `product_pages_query`, `product_page_locales` |
| Suggestions | `keyword_suggestions`, `phrase_suggestions`, `category_suggestions`, `target_cpa_suggestions` |
| Insights | `search_term_popularity`, `impression_share` |
| Reports | campaign, ad-group, ad, keyword, and search-term reports |
| Recommendations | daily-budget and target-CPA recommendations, read-only |
| Audits | `change_history`, `account_health`, `app_opportunities`, `campaign_audit` |
| Verification | direct get tools for campaigns, ad groups, keywords, ads, creatives, and shared budgets |

### Receipt-gated write tools

- Create and update campaigns, ad groups, targeting keywords, and negative keywords.
- Use supply-specific preview foundations for ads and App Store creatives; support still depends on the Apple placement and product-page combination.
- Preview campaign pause/resume, ad-group bids, and CPA caps.
- Apply exactly one preview through `operations_apply`.
- Inspect or verify an ambiguous operation through `operations_inspect` and `operations_verify`.

There is no generic raw-request tool. Apple Maps, legacy Campaign Management API v5, automatic budget increases, destructive deletion, and recommendation apply/dismiss operations are intentionally excluded.

## Safety model

A mutation is possible only when all of the following are true:

1. The Apple user has a recognized API write role for the explicit ad account.
2. The server starts with `--allow-writes`.
3. The selected profile has `allowWrites: true`.
4. A specialized `*_preview` tool returns a receipt.
5. `operations_apply` receives the same unexpired receipt.
6. The current Apple resource still matches the state read during preview.

Receipts expire after ten minutes and are single-use. Mutations are never retried after an ambiguous transport failure. The server returns `committed_unverified` and requires a direct state check before any follow-up write.

For normal research and reporting, run the server without `--allow-writes` and keep every profile at `allowWrites: false`.

## Install

### GitHub Releases

Download the archive for your platform from [GitHub Releases](https://github.com/zelentsov-dev/apple-ads-mcp/releases). Each release includes SHA-256 checksums and SPDX SBOMs.

Example for Apple silicon:

```bash
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.1.0/apple-ads-mcp_0.1.0_darwin_arm64.tar.gz
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.1.0/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf apple-ads-mcp_0.1.0_darwin_arm64.tar.gz
sudo install -m 0755 apple-ads-mcp /usr/local/bin/apple-ads-mcp
```

### Homebrew formula

The release publishes a checksum-pinned formula as an asset:

```bash
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.1.0/apple-ads-mcp.rb
brew install --formula ./apple-ads-mcp.rb
```

### OCI image

```bash
docker pull ghcr.io/zelentsov-dev/apple-ads-mcp:0.1.0
```

The image runs `serve --stdio` by default. Mount `accounts.json` and the referenced private key read-only. If the key is owner-only, run the container with the corresponding host UID/GID.

### Build from source

```bash
git clone https://github.com/zelentsov-dev/apple-ads-mcp.git
cd apple-ads-mcp
go build -trimpath -o ./bin/apple-ads-mcp ./cmd/apple-ads-mcp
```

Go 1.26.6 or newer is required.

## Apple Ads API setup

Apple Ads API access is separate from App Store Connect access. An Apple Ads Account Admin must add an API user with an appropriate role. Prefer `API Account Read Only` until you need campaign changes.

Generate an ES256 key pair locally:

```bash
umask 077
openssl ecparam -name prime256v1 -genkey -noout -out apple-ads-private-key.pem
openssl ec -in apple-ads-private-key.pem -pubout -out apple-ads-public-key.pem
chmod 600 apple-ads-private-key.pem
```

Upload only the public key in Apple Ads. Keep the private key outside the repository and never paste it into an agent conversation, issue, or log.

After Apple registers the public key, record the non-secret client ID, team ID, and key ID shown in the Apple Ads UI.

## Configure

Create a profile interactively:

```bash
apple-ads-mcp config init
```

The default configuration path is:

```text
~/.config/apple-ads-mcp/accounts.json
```

Equivalent JSON:

```json
{
  "profiles": [
    {
      "name": "production-read-only",
      "clientId": "SEARCHADS.example-client-id",
      "teamId": "SEARCHADS.example-team-id",
      "keyId": "EXAMPLEKEY",
      "privateKeyPath": "/absolute/path/to/apple-ads-private-key.pem",
      "defaultAdAccountId": "123456789",
      "allowWrites": false
    }
  ]
}
```

On POSIX systems both files must be owner-only:

```bash
chmod 600 ~/.config/apple-ads-mcp/accounts.json /absolute/path/to/apple-ads-private-key.pem
```

The configuration stores only profile metadata and the private-key path. It never embeds the private key.

Validate authentication and discover accessible accounts:

```bash
apple-ads-mcp auth doctor --profile production-read-only
apple-ads-mcp accounts discover --profile production-read-only
```

Configuration precedence:

1. `--config`
2. `APPLE_ADS_MCP_CONFIG`
3. `~/.config/apple-ads-mcp/accounts.json`
4. Single-profile `APPLE_ADS_*` environment variables

Supported environment variables are `APPLE_ADS_PROFILE`, `APPLE_ADS_CLIENT_ID`, `APPLE_ADS_TEAM_ID`, `APPLE_ADS_KEY_ID`, `APPLE_ADS_PRIVATE_KEY_PATH`, `APPLE_ADS_AD_ACCOUNT_ID`, and `APPLE_ADS_ALLOW_WRITES`.

## Connect an MCP client

Start the server over stdio:

```bash
apple-ads-mcp serve --stdio
```

Generic MCP configuration for Codex, Claude Desktop, or another stdio client:

```json
{
  "mcpServers": {
    "apple-ads": {
      "command": "apple-ads-mcp",
      "args": ["serve", "--stdio"]
    }
  }
}
```

Claude Code:

```bash
claude mcp add apple-ads -- apple-ads-mcp serve --stdio
```

The release archive also contains the Codex plugin metadata and the `apple-ads-operator` skill. The skill routes onboarding, research, campaign workflows, and mutation safety without placing credentials in prompts.

## Enable writes for one session

Keep `allowWrites: false` in the persistent profile whenever possible. For an explicitly authorized maintenance session, enable both gates temporarily:

```bash
APPLE_ADS_PROFILE=production-read-only \
APPLE_ADS_ALLOW_WRITES=true \
apple-ads-mcp serve --stdio --allow-writes
```

The Apple ACL, explicit `profile`, explicit `adAccountId`, preview, receipt binding, expiry, and drift checks still apply. Starting the server with write gates does not itself change anything.

## Known platform behavior

- Apple Ads has no public sandbox. Automated live CI is opt-in and strictly read-only.
- Availability, suggestions, popularity, recommendations, and reports vary by account, app, storefront, and campaign history.
- Cold accounts may return no target-CPA or budget recommendations.
- Apple may return endpoint-specific validation or server errors. The MCP preserves bounded safe details for `4xx` responses and keeps retryable server errors redacted.
- During live acceptance, Apple rejected a separate Ad object for the `APPSTORE_SEARCH_RESULTS` supply source. Search Results used the app's published Default Product Page instead.
- Campaign names are validated by Apple; some punctuation accepted by the web UI may be rejected by the API.
- Performance reports remain empty until delivery data exists. Attribution from an install to a subscription or trial belongs to your attribution stack, not this API.
- Full upstream MCP conformance-runner coverage remains pending because the current runner accepts server URLs but cannot launch a stdio child process. This repository tests a real stdio session through the official Go SDK.

## Development

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
go run github.com/zricethezav/gitleaks/v8@v8.29.1 dir --no-banner --redact .
go run github.com/goreleaser/goreleaser/v2@v2.17.1 check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
npx --yes markdownlint-cli2@0.23.2 '**/*.md'
python3 scripts/validate_distribution.py
```

Live tests require explicit local credentials and never run mutations automatically. See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), the [roadmap](docs/ROADMAP.md), and the machine-readable [operation matrix](api-contract/operations.json).

## Release artifacts

Every tagged release publishes:

- macOS, Linux, and Windows archives for AMD64 and ARM64 where supported;
- `checksums.txt` with SHA-256 digests;
- SPDX software bills of materials generated with Syft;
- a checksum-pinned Homebrew formula;
- multi-architecture OCI images for Linux AMD64 and ARM64;
- MCP Registry package metadata.

## License

[MIT](LICENSE)
