# v0.3.6 release notes and migration

v0.3.6 fixes error diagnostics, targeting-keyword bulk creation, receipt classification, and several read contracts. Tool names remain stable. Existing `id`, query `filters`, and top-level keyword bulk `adGroupId` inputs remain supported.

## Error contract

Failed MCP calls now return the same diagnostic object in text content and structured content. The object classifies validation, write gates, expired, used, missing, and drifted receipts, Apple API failures, transport failures, and ambiguous writes. It includes a stable type, message, code, retryability, safe details, and an actionable hint.

Apple `4xx` responses include a bounded allowlist of Apple code, message, and detail fields. Request bodies, credentials, arbitrary fields, and Apple `5xx` bodies are never returned. A server write gate reports exactly `server is in read-only mode; restart with --allow-writes`.

Expired receipts leave bounded one-hour tombstones after their full records are pruned. This preserves `receipt_expired` instead of degrading immediately to `receipt_not_found`.

## Keyword bulk create

`correlationId` accepts a non-negative integer or decimal-string representation, including `0`, normalizes it to a canonical Apple integer, and detects duplicates after normalization. Descriptive values, negative numbers, fractional values, and integers outside Apple's signed 64-bit range fail local validation.

`keywords_bulk_create_preview` retains its top-level `adGroupId` default and adds optional per-item `adGroupId`. One preview may therefore bind multiple exact campaign and ad-group keyword scopes into one aggregate state hash, one receipt, and one Apple `keywords/bulk-create` request. Scope reservations are semantic rather than tied to a query-body fingerprint: every receipt binds canonical target-object scopes, while creates and parent deletes additionally bind affected inventory and bounded cascade scopes. Generic, bulk, delete, recommendation, shared-budget, and optimization operations against overlapping objects therefore conflict before dispatch even when their verification reads use different shapes; unrelated ad-group inventories can still apply in parallel. Apple item-level success and failure remain visible, created IDs are retained, and verification reads every returned object ID directly even when a successful bulk create grows the source inventory beyond one page.

This does not relax drift protection. Two independent previews from the same inventory snapshot remain mutually incompatible after the first apply. Combine related creates into one bulk preview when they must be applied together.

## Read contract changes

- Resource get tools accept their named ID, such as `campaignId`, `adGroupId`, or `keywordId`, while preserving legacy `id`. If both are present they must match.
- Resource query tools add named ID and scope shortcuts while retaining advanced `filters`. The same field cannot appear in both forms.
- `keywords_query` requires an ID, campaign, or ad-group scope and validates its complete endpoint-specific filter matrix, including the Apple spelling `STARTS_WITH`. `negative_keywords_query` requires an ID, a non-null ad-group scope, or a campaign scope paired with `adGroupId IS_NULL`, and rejects unsupported fields and field/operator/value combinations locally.
- `keyword_bid_preview` accepts optional `campaignId` and `adGroupId` lineage assertions.
- `keyword_report.includeZeroMetrics=true` requests `EMPTY_METRICS`. Without it, the summary points to `keywords_query` as the complete inventory source.
- `change_history` requires `start` and `end`, sends `eventTime BETWEEN`, rejects windows whose start is older than the six-month UTC lookback boundary, exposes typed entity, event, user, transaction, campaign, ad-group, and ad-account audit filters, and defaults to `metadata=latest` in UTC.
- An empty `ads_query` result explains that Search Results campaigns can legitimately have no explicit Ad objects.

## P2 endpoint behavior

- `impression_share` has a typed Adam ID, optional country, date range, `DAILY` or `WEEKLY_SUN_SAT` granularity, and `FIRST_SLOT` or `ALL_SLOTS` report type. UTC is fixed locally and is not sent as a request field. Apple errors remain errors.
- `app_opportunities` returns independent `ok`, `empty`, or `upstream_error` sections so an Apple failure for phrases or categories does not discard successful eligibility, keyword, or target-CPA data.
- `apps_search` performs one bounded owned-app fallback only when a non-ASCII owned-app query returns no rows, then uses exact local name comparison. It does not transliterate or fuzzy-match public catalog results.
- `app_locale_details` returns compact locale metadata and asset counts by default. Full assets require `includeAssets=true` and one exact `languageCode`.

## Acceptance boundary

Automated source, MCP, HTTP, race, static, vulnerability, secret, and distribution gates are required before tagging. Runtime confirmation additionally requires an owner-controlled read-only P2 matrix and a separately authorized `PAUSED` keyword bulk fixture with 2 and 28 items. Both live gates passed against Apple Ads API v1 on 2026-08-31: the aggregate 2-item and 28-item receipts applied successfully, every returned keyword ID verified directly, and the campaign and ad group remained `PAUSED`.

Commit, tag, release publication, and live mutations remain separate owner-authorized actions.
