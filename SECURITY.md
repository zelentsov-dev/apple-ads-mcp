# Security

## Reporting

Do not open a public issue containing credentials, tokens, account identifiers, campaign data, or private keys. Report a suspected vulnerability through GitHub private vulnerability reporting for this repository.

## Credential handling

- Store only profile metadata and the private-key path in `accounts.json`.
- Keep the ES256 private key in a separate file with owner-only permissions on POSIX systems.
- Prefer read-only Apple Ads roles until mutation tools are required.
- Run the server without `--allow-writes` for research and reporting.
- Keep `allowDeletes: false` unless an explicitly authorized lifecycle task requires it.
- Rotate the Apple Ads API key immediately if its contents may have been exposed.

OAuth tokens and operation receipts exist only in process memory. Logs never include private keys, JWT client secrets, access tokens, authorization headers, token response bodies, or private billing payloads.

## Write safety

A write requires Apple authorization, the server flag, profile opt-in, a specialized preview, a non-expired single-use receipt, and a matching current-state hash. Bulk operations bind the entire scoped inventory and report item-level outcomes without promising rollback. Recommendation actions require a currency-matched hard cap. A timed-out write is reported as `committed_unverified` and is never retried automatically.

Optimization is on-demand and policy-bound. Local policy, billing, and history files require owner-only permissions. Billing PII is resolved only from a local profile name and never appears in MCP schemas, logs, history, or receipt previews.

Delete requires separate server, profile, and session gates, an exact expected name or text, a PAUSED parent where applicable, and complete bounded inventory revalidation. A delete timeout is never retried. Verify `deleted: true` before continuing.
