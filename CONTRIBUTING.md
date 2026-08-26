# Contributing

Use Go 1.26.7 or newer and keep the server compatible with the official MCP Go SDK v1.7 line.

Before opening a pull request:

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

New Apple endpoints must have a specialized operation constructor, a bounded MCP schema, an operation classification, and HTTP tests. Update `api-contract/upstream-baseline.json` only after reviewing a new official Apple Java client release. Do not add a raw request tool, configurable production base URL, secret logging, automatic mutation retry, deletion without inventory revalidation, or background budget changes.

Automatic live CI is opt-in and read-only. Manual write acceptance must use explicit local gates, clearly named `PAUSED` fixtures, and direct API verification. Never place Apple credentials, account exports, fixture IDs, or private acceptance reports in commits, logs, issues, or pull requests.

Run the v0.2 mutation acceptance only against an operator-controlled account:

```bash
APPLE_ADS_PROFILE=profile-name \
APPLE_ADS_AD_ACCOUNT_ID=account-id \
APPLE_ADS_LIVE_ADAM_ID=app-adam-id \
APPLE_ADS_LIVE_APP_QUERY=app-name \
APPLE_ADS_LIVE_STOREFRONT=US \
APPLE_ADS_ALLOW_WRITES=true \
APPLE_ADS_LIVE_WRITE=CREATE_PAUSED_FIXTURES \
go test -tags=live_write ./internal/live -run '^TestMCPV02PausedFixtures$' -count=1
```

The test caps fixture budget and bids, creates only `PAUSED` objects, never retries an ambiguous write, verifies every receipt, and leaves fixtures in place for inspection. An ineligible placement is recorded as `not_eligible` rather than treated as a product failure.

After the fixtures exist, run the complete operator acceptance against the same owner-controlled account:

```bash
APPLE_ADS_PROFILE=profile-name \
APPLE_ADS_AD_ACCOUNT_ID=account-id \
APPLE_ADS_LIVE_ADAM_ID=app-adam-id \
APPLE_ADS_LIVE_APP_QUERY=app-name \
APPLE_ADS_LIVE_STOREFRONT=US \
APPLE_ADS_ALLOW_WRITES=true \
APPLE_ADS_LIVE_WRITE=FULL_PAUSED_ACCEPTANCE \
go test -tags=live_write ./internal/live -run '^TestMCPV02OperatorAcceptance$' -count=1
```

This acceptance enumerates the full MCP catalog, runs the read and preview matrices, applies bounded changes only to PAUSED fixtures, restores the exact original budget and keyword bid, verifies receipts and readback, and finishes by checking that every fixture remains PAUSED with zero spend. Known upstream suggestion and impression-share responses must remain structured Apple errors with their expected status and any response code Apple supplies. Live identifiers and app names come only from session environment variables and must never be committed.

Run the v0.3 read-only optimizer acceptance against an explicitly named policy whose campaigns are controlled by the operator:

```bash
APPLE_ADS_PROFILE=profile-name \
APPLE_ADS_AD_ACCOUNT_ID=account-id \
APPLE_ADS_LIVE_OPTIMIZATION_POLICY=policy-name \
go test -tags=live_read_only ./internal/live -run '^TestMCPV03OptimizationReadOnly$' -count=1
```

Destructive acceptance is never automatic. It requires freshly created, clearly named `PAUSED` fixtures, profile `allowDeletes: true`, both server flags, the session delete variable, and the exact confirmation value used by the dedicated live test. Never point it at an existing business campaign.
