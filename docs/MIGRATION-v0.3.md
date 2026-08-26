# Migrating to v0.3

v0.3 preserves all v0.2.1 MCP tool names and input schemas. Existing read-only and receipt-gated campaign workflows continue to work without configuration changes.

## New local files

- Optimization policies: `~/.config/apple-ads-mcp/optimization-policies.json`
- Private billing profiles: `~/.config/apple-ads-mcp/billing-profiles.json`
- Optimization history: `~/.local/share/apple-ads-mcp/optimization/`

On POSIX systems, configuration and state files must be regular owner-only files with mode `0600`. The CLI creates them atomically. These paths can be overridden with server flags or `APPLE_ADS_OPTIMIZATION_POLICIES` and `APPLE_ADS_BILLING_PROFILES`.

## Optimization

`optimization_baseline` and `optimization_plan` are read-only. A policy in `learning` mode never creates an apply receipt. An `active` policy requires an explicit target install CPA, currency-matched budget caps, and per-action permissions. Starting with v0.3.1, bid permission also requires an independent currency-matched `maxBid`; see [MIGRATION-v0.3.1.md](MIGRATION-v0.3.1.md).

`optimization_plan_preview` can bind up to 100 ordered actions affecting at most 20 campaigns to one ten-minute receipt. Apply re-reads all inventory and report fingerprints. A confirmed independent Apple `4xx` can leave unrelated actions eligible to continue; an ambiguous write result stops the remaining plan. There is no retry or rollback.

## Delete gates

Delete tools are unavailable unless all of the following are true:

- the server uses `--allow-writes` and `--allow-deletes`;
- the selected profile has `allowWrites: true` and `allowDeletes: true`;
- the process has `APPLE_ADS_ALLOW_DELETES=true`;
- the Apple role permits the write;
- a specialized delete preview matches the expected name or keyword text and current cascade inventory.

Deletion is irreversible and never part of an optimization plan. A timeout is not retried. Verification must establish `deleted: true` before any dependent operation.

## Shared budgets

Shared-budget create and update tools accept a local `billingProfile` name instead of buyer names or emails. Billing PII is loaded in memory only after Apple reports an eligible `LOC` payment model. `PAYG` accounts return `not_eligible`. Cross-account shared-budget creation and account mutations remain unavailable.

## Compatibility boundary

The MCP contract is still pre-v1.0. v0.3 adds tools and output fields but does not remove or rename v0.2.1 tools. Clients should continue to reject unknown output fields safely and should never cache receipts beyond their expiration.
