# Roadmap

## v0.1 — public foundation

Profiles, ES256 OAuth, account discovery, owned-app search, eligibility, suggestions, insights, reports, recommendations, change history, account health, app opportunities, and campaign audits are implemented.

Receipt-gated create/update flows for campaigns, ad groups, targeting keywords, negative keywords, ads, and App Store creatives are also present. Pause, resume, ad-group bid, and CPA-cap previews are included. Deletion and automatic budget increases stay unavailable.

Private live acceptance verified read-only intelligence and paused campaign creation through direct API readback without placing account-specific evidence in the public repository.

## v0.2 — broader campaign management

Expand typed mutation payloads, improve report selectors, add recommendation preview/apply flows, and verify every supported supply source. Add deletion only after inventory-level revalidation tests cover the complete parent-child resource graph.

## v0.3 — full App Store Ads coverage

Complete and verify typed models for ads, creatives, read-only Custom Product Pages, shared budgets, and recommendation actions. Shared budgets remain read-only until App Store-only impact can be proven. Apple Ads Platform API v1 does not currently document budget-order endpoints, so the MCP does not invent them. Add deletion only after inventory-level revalidation tests cover every supported resource.

## v1.0 — production hardening

Freeze stable tool schemas, publish compatibility policy, complete official MCP conformance coverage, generate signed release artifacts, update the Homebrew formula with release checksums, and publish the OCI package metadata to the MCP Registry.

Current transport coverage uses a real child-process stdio session through the official Go SDK. The official conformance runner requires a URL for server tests and does not yet support launching stdio servers, so full runner coverage is intentionally not claimed.

The machine-readable status is in `api-contract/operations.json`. A scheduled workflow audits Apple documentation, the official Java client, the MCP SDK, and authentication dependencies.
