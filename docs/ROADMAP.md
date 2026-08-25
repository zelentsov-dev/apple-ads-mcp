# Roadmap

## v0.2 — safe App Store campaign operator

v0.2 delivers bounded account inventory, typed App Store schemas, centralized placement and targeting validation, specialized campaign-management previews, bulk targeting and negative keyword operations, capped recommendation actions, composite drift protection, and item-level verification.

The release covers all four App Store placements. Live availability remains an Apple account/app/storefront decision. Manual acceptance uses separate clearly named `PAUSED` campaigns and never enables delivery.

Account mutation, Apple Maps, and legacy v5 remain intentionally unavailable.

## v0.3 — on-demand optimization and lifecycle

- Named learning and active policies bind explicit accounts, apps, campaigns, budget caps, and permissions.
- A balanced Apple-only optimizer produces bounded 28-day baselines and on-demand plans with 7/7 comparisons and cooldowns.
- One composite receipt applies up to 100 ordered actions with full drift checks, dependencies, partial outcomes, and no retry or rollback.
- Typed shared-budget create/update/assign/unassign supports eligible `LOC` accounts while private billing PII stays local.
- Separately gated lifecycle tools require exact expected text, PAUSED parents, bounded cascade inventory, and post-delete verification.

There is no scheduler, background spending, automatic recommendation apply, or optimizer-generated deletion.

## v0.4 — attribution and decision quality

- Add optional external-attribution adapters without weakening Apple-only operation safety.
- Compare install, trial, subscription, and revenue outcomes when a verified attribution contract is available.
- Add redacted exportable decision summaries and policy simulation fixtures.
- Expand DPP/CPP creative diagnostics and placement-specific rejection guidance.
- Track new App Store resources and enums discovered by the weekly upstream audit.

## v1.0 — stable production contract

- Freeze stable MCP tool names and schemas under a compatibility policy.
- Complete upstream MCP conformance coverage as stdio runner support becomes available.
- Publish provenance and signing policy for binaries and containers.
- Define long-term deprecation windows and machine-readable schema diffs.
- Require repeatable PAUSED acceptance evidence for every supported placement.

The machine-readable status is in [operations.json](../api-contract/operations.json). The scheduled audit compares the official Apple Java client release and App Store endpoint inventory with the pinned [upstream baseline](../api-contract/upstream-baseline.json).
