# Apple Ads MCP

[![CI](https://github.com/zelentsov-dev/apple-ads-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/zelentsov-dev/apple-ads-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zelentsov-dev/apple-ads-mcp)](https://github.com/zelentsov-dev/apple-ads-mcp/releases)
[![Go](https://img.shields.io/badge/Go-1.26.6%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/github/license/zelentsov-dev/apple-ads-mcp)](LICENSE)

Apple Ads MCP is a local-first [Model Context Protocol](https://modelcontextprotocol.io/) server for the [Apple Ads Platform API v1](https://developer.apple.com/documentation/apple-ads-platform-api). It gives Codex, Claude, and other MCP clients typed tools to research, inspect, and safely operate App Store advertising accounts.

The server is read-only by default. Credentials stay on your machine, every account-scoped call names an explicit profile and ad account, and every mutation requires a preview plus a short-lived single-use receipt.

Apple Ads MCP is independent open-source software. It is not affiliated with, endorsed by, or sponsored by Apple Inc. Apple Ads and App Store are trademarks of Apple Inc.

## Current release

`v0.2.1` is the current release. It fixes specialized ad-group bid and CPA-cap previews, hardens bounded structured Apple error diagnostics, and adds full PAUSED operator acceptance. It includes all v0.2 capabilities: typed App Store campaign operations, bounded inventory queries, bulk keyword workflows, recommendation actions with hard caps, and centralized placement validation.

The release supports the four App Store placements exposed by Apple:

- Search Results
- Search Tab
- Today Tab
- Product Pages

Placement availability still depends on the selected account, app, storefront, creative, and Apple's eligibility response. An unavailable placement is reported as `not_eligible`; the server does not attempt to bypass Apple validation.

Keywords and negative keywords are Search Results resources. The Platform API rejects explicit `Ad` resources for Search Results campaigns, so that placement is operated through its campaign, ad group, keywords, and product-page configuration; explicit ads remain eligibility-gated for the other placements. A Default Product Page creative is unique per app and ad account, so operators should query and reuse it instead of attempting to create a duplicate.

## What it can do

| Area | Main tools |
| --- | --- |
| Access and readiness | `auth_check`, `ad_accounts_list`, `ad_account_get`, `advertiser_resources_list`, `account_health` |
| Apps and storefronts | `apps_search`, `apps_get`, `apps_eligibility`, `app_locale_details`, `supported_app_languages`, `app_store_geo_search` |
| Inventory | `campaigns_query`, `ad_groups_query`, `keywords_query`, `negative_keywords_query`, `ads_query`, `creatives_query`, `shared_budgets_query`, `campaign_inventory` |
| Diagnostics | rejection reasons, campaign status reasons, change history, account and campaign audits |
| Research | keyword, phrase, category, target-CPA suggestions, search popularity, impression share |
| Reports | typed campaign, ad-group, ad, keyword, and search-term reports |
| Campaign operation | create/update, budget, countries, schedule, pause, and resume previews |
| Ad-group operation | create/update, schedule, targeting, Search Match, bid, CPA cap, pause, and resume previews |
| Keywords | individual and bulk targeting/negative keyword create/update, bid, pause, and resume previews |
| Ads and creatives | typed App Store creative and ad create/update, pause, and resume previews |
| Recommendations | read, apply-preview, and dismiss-preview for daily budget and target CPA |
| Verification | `operations_inspect`, `operations_apply`, `operations_verify`, and direct resource readback |

Every list is bounded to 200 items and uses `next` pagination. Responses include concise text plus MCP `structuredContent`; raw Apple envelopes and billing contact PII are not exposed.

There is no raw-request tool, DELETE tool, account mutation, shared-budget mutation, Apple Maps surface, or legacy Campaign Management API v5 support.

## Safety model

A write is possible only when all gates pass:

1. The Apple API user has a compatible write role.
2. The server starts with `--allow-writes`.
3. The selected profile has `allowWrites: true`.
4. A specialized `*_preview` tool validates the account, app, storefront, placement, currency, and payload.
5. `operations_apply` receives the same unexpired receipt.
6. The affected inventory still matches the state captured during preview.

Receipts expire after ten minutes and are single-use. Bulk receipts bind the entire inventory snapshot and return item-level `applied`, `failed`, or `unknown` states. Apple may partially accept a batch; the server never promises rollback.

Mutation requests are not retried after a timeout. The result is `committed_unverified`, or item-level `unknown`, until `operations_verify` and direct readback establish the actual state.

Recommendation apply operations require an explicit `maximumAmount`. The recommendation currency must match the ad account, the proposed amount must stay under the cap, and the recommendation plus promoted campaign are re-read before apply.

## Install

### GitHub release

Download the archive for your platform from [GitHub Releases](https://github.com/zelentsov-dev/apple-ads-mcp/releases). Releases include SHA-256 checksums and SPDX SBOMs.

Apple silicon example:

```bash
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.2.1/apple-ads-mcp_0.2.1_darwin_arm64.tar.gz
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.2.1/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf apple-ads-mcp_0.2.1_darwin_arm64.tar.gz
sudo install -m 0755 apple-ads-mcp /usr/local/bin/apple-ads-mcp
```

### Homebrew formula

Each release publishes a checksum-pinned formula:

```bash
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.2.1/apple-ads-mcp.rb
brew install --formula ./apple-ads-mcp.rb
```

### OCI image

```bash
docker pull ghcr.io/zelentsov-dev/apple-ads-mcp:0.2.1
```

The image runs `serve --stdio` by default. Mount `accounts.json` and its referenced private key read-only.

### Build from source

```bash
git clone https://github.com/zelentsov-dev/apple-ads-mcp.git
cd apple-ads-mcp
go build -trimpath -o ./bin/apple-ads-mcp ./cmd/apple-ads-mcp
```

Go 1.26.6 or newer is required.

## Apple API setup

Apple Ads API access is separate from App Store Connect access. An Apple Ads Account Admin must add an API user with the appropriate role. Prefer `API Account Read Only` until campaign changes are needed.

Generate an ES256 key pair locally:

```bash
umask 077
openssl ecparam -name prime256v1 -genkey -noout -out apple-ads-private-key.pem
openssl ec -in apple-ads-private-key.pem -pubout -out apple-ads-public-key.pem
chmod 600 apple-ads-private-key.pem
```

Upload only the public key in Apple Ads. Never paste the private key into an agent conversation, issue, log, or repository. After Apple registers the key, record the client ID, team ID, and key ID shown in the Apple Ads UI.

## Configure

Create a profile interactively:

```bash
apple-ads-mcp config init
```

The default configuration path is `~/.config/apple-ads-mcp/accounts.json`:

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

On POSIX systems, the configuration and private key must be owner-only:

```bash
chmod 600 ~/.config/apple-ads-mcp/accounts.json /absolute/path/to/apple-ads-private-key.pem
```

Validate authentication and discover accessible ad accounts:

```bash
apple-ads-mcp auth doctor --profile production-read-only
apple-ads-mcp accounts discover --profile production-read-only
```

Configuration precedence is `--config`, `APPLE_ADS_MCP_CONFIG`, the default file, then single-profile `APPLE_ADS_*` environment variables. Supported overrides are `APPLE_ADS_PROFILE`, `APPLE_ADS_CLIENT_ID`, `APPLE_ADS_TEAM_ID`, `APPLE_ADS_KEY_ID`, `APPLE_ADS_PRIVATE_KEY_PATH`, `APPLE_ADS_AD_ACCOUNT_ID`, and `APPLE_ADS_ALLOW_WRITES`.

## Connect an MCP client

Run the stdio server:

```bash
apple-ads-mcp serve --stdio
```

Generic MCP configuration:

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

Release archives include the Codex plugin metadata and the `apple-ads-operator` skill.

## Enable writes for one session

Keep persistent profiles read-only. For one explicitly authorized operator session, enable both local gates:

```bash
APPLE_ADS_PROFILE=production-read-only \
APPLE_ADS_ALLOW_WRITES=true \
apple-ads-mcp serve --stdio --allow-writes
```

This does not change anything by itself. Every mutation still needs explicit `profile`, `adAccountId`, a specialized preview, receipt apply, and verification.

## Operational limits

- Apple provides no public Ads API sandbox. Live mutation acceptance is manual, explicitly enabled, uses clearly named `PAUSED` fixtures, and is never part of automatic CI.
- Eligibility, suggestions, popularity, recommendations, reports, and creative support vary by account and storefront.
- Cold accounts may have no target-CPA or daily-budget recommendations.
- Campaign and object names are validated by Apple; the web UI and API may accept different punctuation.
- Apple rejects the vertical-bar character (`|`) in campaign names; use a hyphenated readable name instead. Ad-group creation requires an ISO 8601 `startTime` in the ad-account timezone.
- Reports can lag and remain empty until delivery occurs.
- Trial and subscription attribution belongs to your attribution stack; the Apple Ads Platform API alone does not prove keyword-to-trial attribution.

### Known Apple API responses

As of 2026-08-25, the documented v1 request shapes used by this server have produced two repeatable upstream behaviors during live acceptance:

- `phrase_suggestions` and `category_suggestions` may return Apple HTTP `500` for a `SUGGESTION` query even when `keyword_suggestions` and `target_cpa_suggestions` work for the same owned app.
- `impression_share` may return Apple HTTP `400` on a cold or fully paused account. An owner-controlled acceptance account supplied no code or diagnostic details; public reproductions have also reported code `INVALID_VALUE`. Missing recent delivery may be relevant, but it is not a confirmed Apple prerequisite or workaround.

The server returns these as bounded structured Apple errors, including whether the upstream body was empty or non-JSON; it does not convert them into empty or successful results. The request shapes match the [current Apple Ads Platform API](https://developer.apple.com/documentation/apple-ads-platform-api) and the [official Apple Java client](https://github.com/apple/apple-ads-platform-api-java). Comparable public v1 reproductions are recorded in [App Store Connect CLI PR #2057](https://github.com/rorkai/App-Store-Connect-CLI/pull/2057) and [PR #2020](https://github.com/rorkai/App-Store-Connect-CLI/pull/2020). Recheck after Apple changes the API or the account has meaningful delivery history.

## Compatibility

v0.2 keeps existing tool names but replaces open mutation payloads with resource-specific schemas. Unknown fields are rejected. See [v0.2 migration notes](docs/MIGRATION-v0.2.md) before upgrading an automated client.

The public tool schema is not frozen before v1.0. API-family scope and operation status are tracked in the [machine-readable operation matrix](api-contract/operations.json). The official Java client baseline and App Store endpoint inventory are tracked in [upstream-baseline.json](api-contract/upstream-baseline.json).

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

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and the [roadmap](docs/ROADMAP.md).

## License

[MIT](LICENSE)
