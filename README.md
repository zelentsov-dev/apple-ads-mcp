# Apple Ads MCP

[![CI](https://github.com/zelentsov-dev/apple-ads-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/zelentsov-dev/apple-ads-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zelentsov-dev/apple-ads-mcp)](https://github.com/zelentsov-dev/apple-ads-mcp/releases)
[![Go](https://img.shields.io/badge/Go-1.26.7%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/github/license/zelentsov-dev/apple-ads-mcp)](LICENSE)

Apple Ads MCP is a local-first [Model Context Protocol](https://modelcontextprotocol.io/) server for the [Apple Ads Platform API v1](https://developer.apple.com/documentation/apple-ads-platform-api). It gives Codex, Claude, and other MCP clients typed tools to research, inspect, and safely operate App Store advertising accounts.

The server is read-only by default. Credentials stay on your machine, every account-scoped call names an explicit profile and ad account, and every mutation requires a preview plus a short-lived single-use receipt.

Apple Ads MCP is independent open-source software. It is not affiliated with, endorsed by, or sponsored by Apple Inc. Apple Ads and App Store are trademarks of Apple Inc.

## Quick start

Install the released binary with Homebrew on macOS or Linux:

```bash
brew install zelentsov-dev/tap/apple-ads-mcp
apple-ads-mcp version
```

Create a local read-only profile. The command stores an absolute private-key path; never paste the private key into a chat, issue, log, or repository:

```bash
apple-ads-mcp config init
apple-ads-mcp auth doctor --profile production-read-only
apple-ads-mcp accounts discover --profile production-read-only
```

Register the server with an absolute Homebrew path so desktop apps and IDE extensions do not depend on their inherited shell `PATH`.

Codex:

```bash
codex mcp add apple-ads -- "$(brew --prefix)/bin/apple-ads-mcp" serve --stdio
codex mcp list
```

Claude Code:

```bash
claude mcp add --scope user apple-ads -- "$(brew --prefix)/bin/apple-ads-mcp" serve --stdio
claude mcp get apple-ads
```

Restart an already running desktop app, CLI session, or IDE extension, then start a new conversation. A safe first request is:

> Call `server_info`, then run `account_health` for my explicit profile, ad account, and app. Do not enable writes.

Other useful read-only starters:

- “List the apps I can advertise and explain any eligibility gaps.”
- “Audit my current Apple Ads campaign structure without changing anything.”
- “Find keyword opportunities and separate Apple evidence from your inferences.”

## Current release

`v0.3.5` is the current release. Its archive names end with the semantic version so Homebrew cannot infer `64` from the `arm64` suffix. This packaging-only fix does not change MCP tool names or schemas. Formula-scoped trust for Homebrew 6 from v0.3.4, exact-path verification from v0.3.3, and one-command onboarding from v0.3.2 remain intact.

Optimization is never autonomous: the server has no scheduler and never changes spend in the background. A read-only session can build a baseline and plan. Applying a plan still requires every write gate, an active named policy, one receipt, a fresh report and inventory drift check, and item-level verification.

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
| Optimization | named local policies, 28-day baselines, learning/active plans, composite preview/apply/verify, and bounded history |
| Shared budgets | typed LOC-only create/update/assign/unassign previews with private local billing profiles |
| Lifecycle | separately gated campaign, ad-group, keyword, negative-keyword, ad, creative, and shared-budget delete previews |
| Verification | `operations_inspect`, `operations_apply`, `operations_verify`, and direct resource readback |

Every list is bounded to 200 items and uses `next` pagination. Responses include concise text plus MCP `structuredContent`; raw Apple envelopes and billing contact PII are not exposed.

There is no raw-request tool, account/delegation mutation, automatic scheduler, Apple Maps surface, or legacy Campaign Management API v5 support.

## Safety model

A write is possible only when all gates pass:

1. The Apple API user has a compatible write role.
2. The server starts with `--allow-writes`.
3. The selected profile has `allowWrites: true`.
4. A specialized `*_preview` tool validates the account, app, storefront, placement, currency, and payload.
5. `operations_apply` receives the same unexpired receipt.
6. The affected inventory still matches the state captured during preview.

Preview receipts expire after ten minutes and are single-use for apply. Bulk receipts bind the entire inventory snapshot and return item-level `applied`, `failed`, or `unknown` states. Apple may partially accept a batch; the server never promises rollback. If an optimization apply has an unresolved outcome, a sanitized recovery recipe remains in owner-only local history so `operations_verify` can reconcile the supplied receipt after a process restart or normal preview eviction.

Mutation requests are not retried after a timeout. The result is `committed_unverified`, or item-level `unknown`, until `operations_verify` and direct readback establish the actual state.

Recommendation apply operations require an explicit `maximumAmount`. The recommendation currency must match the ad account, the proposed amount must stay under the cap, and the recommendation plus promoted campaign are re-read before apply.

An irreversible delete has five additional gates: `--allow-deletes`, profile `allowDeletes: true`, session-only `APPLE_ADS_ALLOW_DELETES=true`, an exact expected object name or keyword text, and a specialized delete receipt. Campaigns and parents must be `PAUSED`; cascade inventory is bounded and hashed; creatives must have no referencing ads; shared budgets must have no assignments. DELETE is never retried after an ambiguous result.

## Release validation

The v0.3 release candidate passed the complete v0.2 compatibility suite, MCP stdio contract tests, unit and HTTP tests, the race detector, static analysis, distribution validation, and owner-controlled live acceptance against Apple Ads API v1.

Live acceptance confirmed read-only optimization baselines and plans, `PAUSED` fixture creation and readback, budget/bid/strategy updates, pause/resume, receipt drift checks, item verification, and deletion of disposable campaigns, ad groups, keywords, and negative keywords. All retained fixtures remained `PAUSED` with zero spend.

Account-dependent paths have an explicit evidence boundary:

- Active optimization-plan apply and `MAX_CONVERSIONS` remain automated-test verified until a deliberately selected mature campaign satisfies the minimum 14-day data and Apple eligibility gates.
- Shared-budget create/update/assignment/delete remain contract- and HTTP-test verified because the live acceptance account uses `PAYG`; Apple requires `LOC`.
- Default Product Page ad and creative deletion is confirmed as `not_eligible` because Apple does not allow those resources to be deleted individually.

These boundaries do not weaken the default safety model: unsupported or insufficiently evidenced operations fail closed and never become successful empty responses.

## Install

| Platform | Homebrew | GitHub archive | OCI image |
| --- | --- | --- | --- |
| macOS arm64 / amd64 | Yes | `.tar.gz` | Linux container only |
| Linux arm64 / amd64 | Yes | `.tar.gz` | Yes |
| Windows arm64 / amd64 | No | `.zip` | Linux container only |

### Homebrew

The official tap installs a checksum-pinned release binary:

```bash
brew install zelentsov-dev/tap/apple-ads-mcp
```

Upgrade later releases with:

```bash
brew update
brew upgrade apple-ads-mcp
```

### GitHub release

Download the archive for your platform from [GitHub Releases](https://github.com/zelentsov-dev/apple-ads-mcp/releases). Releases include SHA-256 checksums and SPDX SBOMs.

Apple silicon example:

```bash
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.3.5/apple-ads-mcp_darwin_arm64_0.3.5.tar.gz
curl -LO https://github.com/zelentsov-dev/apple-ads-mcp/releases/download/v0.3.5/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf apple-ads-mcp_darwin_arm64_0.3.5.tar.gz
sudo install -m 0755 apple-ads-mcp /usr/local/bin/apple-ads-mcp
```

Windows users should verify `checksums.txt`, extract the matching ZIP, and register the executable with its absolute path.

### OCI image

```bash
docker pull ghcr.io/zelentsov-dev/apple-ads-mcp:0.3.5
```

The image runs `serve --stdio` by default. Mount `accounts.json` and its referenced private key read-only.

### Build from source

```bash
git clone https://github.com/zelentsov-dev/apple-ads-mcp.git
cd apple-ads-mcp
go build -trimpath -o ./apple-ads-mcp ./cmd/apple-ads-mcp
```

Go 1.26.7 or newer is required.

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
      "allowWrites": false,
      "allowDeletes": false
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

Configuration precedence is `--config`, `APPLE_ADS_MCP_CONFIG`, the default file, then single-profile `APPLE_ADS_*` environment variables. Supported single-profile variables are `APPLE_ADS_PROFILE`, `APPLE_ADS_CLIENT_ID`, `APPLE_ADS_TEAM_ID`, `APPLE_ADS_KEY_ID`, `APPLE_ADS_PRIVATE_KEY_PATH`, `APPLE_ADS_AD_ACCOUNT_ID`, `APPLE_ADS_ALLOW_WRITES`, and `APPLE_ADS_ALLOW_DELETES`. For a file-backed profile, environment variables do not silently grant persistent write or delete permission.

## Connect an MCP client

The server uses stdio and writes MCP JSON-RPC only to `stdout`:

```bash
apple-ads-mcp serve --stdio
```

For clients with a CLI, prefer an absolute binary path.

Codex global configuration:

```bash
codex mcp add apple-ads -- "$(brew --prefix)/bin/apple-ads-mcp" serve --stdio
codex mcp list
```

Codex Desktop, the Codex CLI, and the IDE extension share the same host configuration. Restart an already running client and open a new conversation after adding the server.

Claude Code user configuration:

```bash
claude mcp add --scope user apple-ads -- "$(brew --prefix)/bin/apple-ads-mcp" serve --stdio
claude mcp get apple-ads
```

Claude project configuration can use the repository's `.mcp.json`, but each workspace/server may remain pending until the user approves it. Reconnect from `/mcp` or restart Claude Code after approval.

Generic MCP configuration for clients without a registration command:

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

Replace `apple-ads-mcp` with the absolute executable path when the client does not inherit the Homebrew or shell `PATH`. On Apple silicon the default Homebrew path is `/opt/homebrew/bin/apple-ads-mcp`; on Intel macOS it is normally `/usr/local/bin/apple-ads-mcp`.

### Plugin and skill bundle

Release archives include `.codex-plugin/plugin.json`, `.mcp.json`, and the `apple-ads-operator` skill. These files describe the server and safe operating workflow for compatible plugin loaders, but they do not install the executable, register the MCP server, or grant Apple Ads access by themselves. This repository is not currently distributed through a public Codex plugin marketplace; Homebrew plus client registration is the supported public setup path.

### Verify the connection

After restarting the client, confirm that `apple-ads` is enabled or connected and ask it to call `server_info`. After local credentials are ready, use the explicit profile and account for `auth_check`, `ad_accounts_list`, and `account_health`.

All setup verification must remain read-only. Do not add `--allow-writes`, `--allow-deletes`, `allowWrites`, or `allowDeletes` while troubleshooting installation or authentication.

### Troubleshooting

- **Executable not found:** use `"$(brew --prefix)/bin/apple-ads-mcp"` in the client configuration instead of a bare command.
- **Server already exists:** remove only that client's existing `apple-ads` entry, then add it again with the absolute path.
- **Claude shows pending approval:** approve the project MCP server, reconnect from `/mcp`, and start a new conversation.
- **A running client still lacks tools:** fully restart the desktop app, CLI session, or IDE extension after changing MCP configuration.
- **Authentication fails:** run `apple-ads-mcp auth doctor --profile <profile>` locally and confirm file permissions and non-secret identifiers; never paste the private key into chat.
- **Homebrew rejects a downloaded local formula:** install from the trusted tap with `brew install zelentsov-dev/tap/apple-ads-mcp`.

### Agent-led setup

An agent may diagnose and perform installation only with the user's approval. It should verify the binary, use an absolute path, register the intended client scope, restart the client, and stop after read-only health checks. It must never request private-key contents or silently enable mutation gates.

## Enable writes for one session

Keep persistent profiles read-only. For one explicitly authorized operator session, enable both local gates:

```bash
APPLE_ADS_PROFILE=production-read-only \
APPLE_ADS_ALLOW_WRITES=true \
apple-ads-mcp serve --stdio --allow-writes
```

This does not change anything by itself. Every mutation still needs explicit `profile`, `adAccountId`, a specialized preview, receipt apply, and verification.

## Configure on-demand optimization

Create a named local policy:

```bash
apple-ads-mcp optimization policy init
apple-ads-mcp optimization policy validate --name mature-product-balanced
apple-ads-mcp optimization doctor --policy mature-product-balanced
```

Policies are stored in `~/.config/apple-ads-mcp/optimization-policies.json` with mode `0600`. Each policy binds one profile, one ad account, one promoted app, and at most 20 campaign IDs. `learning` mode requires no business target and only returns evidence. `active` mode requires `targetInstallCPA`, total and per-campaign daily-budget caps, and explicit permissions for budget, bid, strategy, pause, resume, and retest. A policy that allows bid changes must also set a positive, account-currency `maxBid`; the daily-budget cap is never reused as a bid cap.

The `balanced` preset requires exactly 28 unique consecutive completed UTC days ending yesterday, compares the latest 7 days with the previous 7, applies a 72-hour cooldown, normally proposes 10% changes, and never exceeds 20% per run. Missing, duplicate, future, malformed, or overflowing report evidence fails closed. It can propose `MAX_CONVERSIONS` only for eligible Search Results Search Match inventory averaging at least five tap installs per day over 14 days. It never proposes deletion.

Typical agent flow:

1. Call `optimization_baseline` and `optimization_plan` in a read-only server.
2. Review Apple recommendations separately from calculated actions.
3. Start an authorized write session and call `optimization_plan_preview`.
4. Inspect `OperationImpact`, apply the same receipt once, then call `operations_verify`.
5. Review bounded local history under `~/.local/share/apple-ads-mcp/optimization/`.

Immediately before the first Apple write, the server durably persists a receipt-hash-bound `applying` intent and sanitized typed verification recipe under an inter-process file lock. POSIX commits sync the containing directory; Windows uses replace-with-write-through semantics. An unknown result, inconclusive verification, or interrupted history update blocks later optimization plans until `operations_verify` conclusively reconciles every affected item. Recovery survives process restart and normal in-memory receipt expiry, but requires the original opaque receipt from the operator. A reconciled `matched` or `matched_after` action starts cooldown from its original intent/apply time; `matched_before` does not. A campaign can be auto-resumed only when a verified optimizer-owned pause still has the same Apple `modificationTime`; any later manual change revokes that permission. No receipt, credential, raw Apple envelope, or billing contact is stored in optimization history.

## Shared budgets and lifecycle operations

Shared-budget mutations are available only when Apple reports the account payment model as `LOC`. Create and update operations bind the budget exclusively to the explicitly selected ad account. Campaign assignment tools preserve unrelated existing assignments, and cross-account shared budgets fail closed. Initialize private billing data locally:

```bash
apple-ads-mcp billing profile init
```

`~/.config/apple-ads-mcp/billing-profiles.json` must be `0600`. MCP inputs contain only the local `billingProfile` name. Buyer names and email addresses are not returned in tool output, logs, history, or receipt previews. `PAYG` accounts return `not_eligible` without attempting a write.

For an explicitly authorized destructive maintenance session:

```bash
APPLE_ADS_ALLOW_WRITES=true \
APPLE_ADS_ALLOW_DELETES=true \
apple-ads-mcp serve --stdio --allow-writes --allow-deletes
```

The selected file-backed profile must already contain `allowDeletes: true`; `allowWrites` may be enabled for only the current process with `APPLE_ADS_ALLOW_WRITES=true`. The server and session delete gates remain independent. Use a specialized `*_delete_preview`, compare the full cascade impact and exact expected text, apply once, and verify `deleted: true`. Do not use lifecycle operations as part of an optimization plan.

## Operational limits

- Apple provides no public Ads API sandbox. Live mutation acceptance is manual, explicitly enabled, uses clearly named `PAUSED` fixtures, and is never part of automatic CI.
- Eligibility, suggestions, popularity, recommendations, reports, and creative support vary by account and storefront.
- Cold accounts may have no target-CPA or daily-budget recommendations.
- Campaign and object names are validated by Apple; the web UI and API may accept different punctuation.
- Apple rejects the vertical-bar character (`|`) in campaign names; use a hyphenated readable name instead. Ad-group creation requires an ISO 8601 `startTime` in the ad-account timezone.
- Reports can lag and remain empty until delivery occurs.
- Apple rejects individual deletion of an ad that uses the Default Product Page (`CAN_NOT_DELETE_DPP_CREATIVE_AD`). The server returns `not_eligible` during preview; use lifecycle deletion only for disposable Custom Product Page ads and creatives.
- Trial and subscription attribution belongs to your attribution stack; the Apple Ads Platform API alone does not prove keyword-to-trial attribution.

### Known Apple API responses

As of 2026-08-25, the documented v1 request shapes used by this server have produced two repeatable upstream behaviors during live acceptance:

- `phrase_suggestions` and `category_suggestions` may return Apple HTTP `500` for a `SUGGESTION` query even when `keyword_suggestions` and `target_cpa_suggestions` work for the same owned app.
- `impression_share` may return Apple HTTP `400` on a cold or fully paused account. An owner-controlled acceptance account supplied no code or diagnostic details; public reproductions have also reported code `INVALID_VALUE`. Missing recent delivery may be relevant, but it is not a confirmed Apple prerequisite or workaround.

The server returns these as bounded structured Apple errors, including whether the upstream body was empty or non-JSON; it does not convert them into empty or successful results. The request shapes match the [current Apple Ads Platform API](https://developer.apple.com/documentation/apple-ads-platform-api) and the [official Apple Java client](https://github.com/apple/apple-ads-platform-api-java). Comparable public v1 reproductions are recorded in [App Store Connect CLI PR #2057](https://github.com/rorkai/App-Store-Connect-CLI/pull/2057) and [PR #2020](https://github.com/rorkai/App-Store-Connect-CLI/pull/2020). Recheck after Apple changes the API or the account has meaningful delivery history.

## Compatibility

v0.3 preserves all v0.2.1 tool names and schemas, then adds optimization, shared-budget mutation, bid-strategy, and lifecycle tools. v0.3.1 adds a required `maxBid` when bid permission is enabled and fail-closed reconciliation rules; see the [v0.3.1 migration notes](docs/MIGRATION-v0.3.1.md). v0.3.2 changes distribution and onboarding only. v0.3.3 switches release verification to the exact tap formula path, v0.3.4 adds formula-scoped trust for Homebrew 6, and v0.3.5 disambiguates Homebrew version detection by placing the semantic version at the end of archive names. None of these packaging releases changes MCP tool names or schemas. Older clients should also read the [v0.3 notes](docs/MIGRATION-v0.3.md) and [v0.2 notes](docs/MIGRATION-v0.2.md).

The public tool schema is not frozen before v1.0. API-family scope and operation status are tracked in the [machine-readable operation matrix](api-contract/operations.json). The official Java client baseline and App Store endpoint inventory are tracked in [upstream-baseline.json](api-contract/upstream-baseline.json).

## Development

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 -checks 'all,-ST1000,-ST1005' ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
go run github.com/zricethezav/gitleaks/v8@v8.29.1 dir --no-banner --redact .
go run github.com/goreleaser/goreleaser/v2@v2.17.1 check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
npx --yes markdownlint-cli2@0.23.2 '**/*.md'
python3 scripts/validate_distribution.py
python3 scripts/audit_upstream.py
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and the [roadmap](docs/ROADMAP.md).

## License

[MIT](LICENSE)
