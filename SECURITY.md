# Security

## Reporting

Do not open a public issue containing credentials, tokens, account identifiers, campaign data, or private keys. Report a suspected vulnerability through GitHub private vulnerability reporting for this repository.

## Credential handling

- Store only profile metadata and the private-key path in `accounts.json`.
- Keep the ES256 private key in a separate file with owner-only permissions on POSIX systems.
- Prefer read-only Apple Ads roles until mutation tools are required.
- Run the server without `--allow-writes` for research and reporting.
- Rotate the Apple Ads API key immediately if its contents may have been exposed.

OAuth tokens and operation receipts exist only in process memory. Logs never include private keys, JWT client secrets, access tokens, authorization headers, or token response bodies.

## Write safety

A write requires Apple authorization, the server flag, profile opt-in, a specialized preview, a non-expired single-use receipt, and a matching current-state hash. Bulk operations bind the entire scoped inventory and report item-level outcomes without promising rollback. Recommendation actions require a currency-matched hard cap. A timed-out write is reported as `committed_unverified` and is never retried automatically.
