# Migrating to v0.2

v0.2 keeps existing MCP tool names but deliberately tightens mutation inputs before the v1.0 compatibility freeze.

## Typed payloads

Create and update previews now accept resource-specific payload schemas. Existing compatible JSON fields retain their names. Unknown fields and Maps-only values are rejected before a receipt is created.

Notable normalized values:

- IDs are decimal strings.
- Money is `{ "amount": "decimal string", "currency": "ISO 4217" }` at the public MCP boundary.
- Dates use `YYYY-MM-DD`; timestamps use ISO 8601.
- `promotedObjectType` is fixed to `APPSTORE_APP`.
- App Store geo lookup fixes `supplySource` to `APPSTORE`.
- Ad-group create requires `startTime`; Apple accepts its local ISO 8601 form such as `2026-08-25T12:00:00.000` in the ad-account timezone.
- Resource names reject control characters and the vertical bar (`|`) before preview because Apple rejects that separator.

## Query and report inputs

Inventory lists now use bounded typed filters, sorting, and pagination. The maximum page size is 200 and continuation is returned through `next`.

Reports use endpoint-specific selectors, fields, filters, and groupings. A generic arbitrary query payload is no longer accepted. The campaign-report selector now uses the campaign endpoint's documented selector shape.

## Bulk results

Bulk keyword and negative-keyword previews accept at most 100 items in one campaign/ad-group scope. Each item needs a unique `correlationId`. Apply may return partial success and item-level `applied`, `failed`, or `unknown`; clients must not assume rollback.

## Recommendation actions

Recommendation apply previews require `maximumAmount`. If `appliedAmount` is omitted, Apple's current suggestion is used only when it is within that cap and has the account currency. Apply and dismiss actions re-read both the recommendation and promoted campaign before writing.

## Placement-specific ads

Search Results continues to support keyword and negative-keyword management, but v0.2 rejects explicit `Ad` resources for that placement after live API validation confirmed the endpoint restriction. Explicit ads for Search Tab, Today Tab, and Product Pages remain subject to app, storefront, creative, and account eligibility.

A Default Product Page creative is unique for one app and ad account. Query `creatives_query` and reuse the current creative instead of attempting duplicate creation.

## Removed assumptions

No prior tool name was removed. Clients that relied on arbitrary mutation or report fields must update to the schemas returned by `tools/list`. The server still exposes no raw HTTP tool, DELETE operation, account mutation, or shared-budget mutation.
