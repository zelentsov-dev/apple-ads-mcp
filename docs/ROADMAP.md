# Roadmap

## v0.1 — read-only intelligence

Profiles, ES256 OAuth, account discovery, owned-app search, eligibility, suggestions, insights, reports, recommendations, change history, account health, app opportunities, and campaign audits.

The private, opt-in acceptance verifies owned-app discovery, storefront eligibility, and bounded keyword opportunities without placing account-specific evidence in the public repository.

## v0.2 — campaign management

Receipt-gated campaign, ad-group, targeting keyword, and negative-keyword create/update flows. Pause, resume, ad-group bid, and CPA-cap previews are included. Deletion and automatic budget increases stay unavailable.

## v0.3 — full App Store Ads coverage

Complete and verify typed models for ads, creatives, read-only Custom Product Pages, shared budgets, and recommendation actions. Shared budgets remain read-only until App Store-only impact can be proven. Apple Ads Platform API v1 does not currently document budget-order endpoints, so the MCP does not invent them. Add deletion only after inventory-level revalidation tests cover every supported resource.

## v1.0 — production hardening

Freeze stable tool schemas, publish compatibility policy, complete official MCP conformance coverage, generate signed release artifacts, update the Homebrew formula with release checksums, and publish the OCI package metadata to the MCP Registry.

Current transport coverage uses a real child-process stdio session through the official Go SDK. The official conformance runner requires a URL for server tests and does not yet support launching stdio servers, so full runner coverage is intentionally not claimed.

The machine-readable status is in `api-contract/operations.json`. A scheduled workflow audits Apple documentation, the official Java client, the MCP SDK, and authentication dependencies.
