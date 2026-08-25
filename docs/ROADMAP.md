# Roadmap

## v0.2 — safe App Store campaign operator

v0.2 delivers bounded account inventory, typed App Store schemas, centralized placement and targeting validation, specialized campaign-management previews, bulk targeting and negative keyword operations, capped recommendation actions, composite drift protection, and item-level verification.

The release covers all four App Store placements. Live availability remains an Apple account/app/storefront decision. Manual acceptance uses separate clearly named `PAUSED` campaigns and never enables delivery.

Deletion, account mutation, shared-budget mutation, budget orders, Apple Maps, and legacy v5 remain intentionally unavailable.

## v0.3 — lifecycle and budget foundations

- Design soft-delete previews with full parent/child inventory revalidation and explicit cascade impact.
- Add typed shared-budget and budget-order operations only when the current Platform API exposes a complete App Store contract.
- Expand DPP/CPP creative diagnostics and placement-specific rejection guidance.
- Add exportable, redacted local acceptance summaries without account data.
- Track new App Store resources and enums discovered by the weekly upstream audit.

No automatic budget optimization or mass recommendation apply is planned for v0.3.

## v1.0 — stable production contract

- Freeze stable MCP tool names and schemas under a compatibility policy.
- Complete upstream MCP conformance coverage as stdio runner support becomes available.
- Publish provenance and signing policy for binaries and containers.
- Define long-term deprecation windows and machine-readable schema diffs.
- Require repeatable PAUSED acceptance evidence for every supported placement.

The machine-readable status is in [operations.json](../api-contract/operations.json). The scheduled audit compares the official Apple Java client release and App Store endpoint inventory with the pinned [upstream baseline](../api-contract/upstream-baseline.json).
