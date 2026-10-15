# v0.3.7 migration notes

This release updates Apple Ads Platform API v1 compatibility for App Store advertising. Tool names, default read-only mode, credential handling, and explicit preview/apply gates remain unchanged. It does not introduce background optimization or migrate advertising campaigns.

## Build baseline

Source builds now require Go 1.26.9 or newer. Release binaries and the pinned Docker builder use the patched Go 1.26 line to address standard-library vulnerabilities detected by the release security checks.

## Request pacing

The client reads `RateLimit-Limit`, `RateLimit-Remaining`, and `RateLimit-Reset` on responses. Modern `Reset` is a duration in seconds. Legacy rate-limit headers remain available as output fallbacks; their reset value is not assumed to use modern delta semantics.

A `429` waits for `Retry-After`, then modern `RateLimit-Reset`, then exponential fallback starting at two seconds. Server-requested waits are never shortened to five seconds. A low remaining quota (fewer than five requests) pauses the next request in that profile; one read probes the refreshed window before waiting peers continue. If a low-quota response has no usable timing header, the same bounded fallback delay applies.

Reads retain a maximum of three attempts. Total waiting per client operation is limited to 60 seconds and any earlier caller deadline. If the required delay exceeds the available waiting budget, the client returns a structured rate-limit error with safe timing details instead of sending early. Cooldown coordination is in memory within one profile in one server process, not a distributed quota across clients.

Positive numeric timing values outside the supported duration range fail closed with `RATE_LIMIT_TIMING_INVALID`; the profile stays blocked rather than guessing a shorter delay. Investigate the upstream header before restarting the server. A newer valid in-flight rate-limit response can also replace that state.

A mutation encountering an active cooldown fails before dispatch, including while a read probe is needed to confirm the refreshed window. An apply receipt is still single-use once consumed; inspect the operation and create a fresh preview after the cooldown when necessary. Timeouts and server failures after dispatch remain ambiguous and must be verified rather than repeated.

## Bid strategies

Every supplied type and goal must be a matching pair:

| Type | Goal |
| --- | --- |
| `MANUAL_CPT` | `TAP` |
| `MAX_CONVERSIONS` | `INSTALL` |

Campaign creation requires a strategy. An ad group may omit its strategy at creation to inherit its parent's settings. An explicitly supplied manual creation strategy requires a positive bid. An automated creation strategy may include a positive bid ceiling.

Manual bid-only updates remain supported. For an ad group under an automated campaign, an update may echo the parent's type and goal with a zero bid; a nonzero bid or a conflicting strategy is rejected. Monetary values use the account currency. Decimal strings are not restricted to two fractional digits.

Shared validation covers generic updates, specialized previews, and optimizer-generated changes. Parent campaign and account evidence used for validation is bound to the receipt; drift requires a fresh preview.

Explicit user-requested `MAX_CONVERSIONS` previews no longer require 14 completed reporting days and 70 historical installs. They still validate the App Store placement, billing, currency, and write permissions, and warn that statistical readiness has not been established. This does not promise that Apple will accept every account-dependent request.

Optimizer recommendations retain the existing minimum of 14 completed days and five tap installs per day on average, or stricter policy settings. Apple's budget guidance for five daily conversions and the two-week learning period are recommendations, not a historical eligibility requirement for explicit requests.

## Reports and deprecated CPA cap

`groupBy: ["storefront"]` is rejected with a migration hint to `countryOrRegion`; the client never silently changes the request. Storefront metadata, filters, and app-search storefront inputs remain available.

General reports preserve date strings, including hourly timestamps. The optimizer continues to consume completed daily UTC intervals and rejects malformed dates rather than silently counting them as zero.

`cpaCap` and `ad_group_cpa_cap_preview` remain available for compatibility but are deprecated. Schemas and tool descriptions expose the deprecation, and applicable previews contain a warning. Existing values are not converted to a bid strategy, and no removal date is assumed.

## Preview warnings and validation

Preview objects may contain an additive `warnings` array of objects with `code` and `message`. Warnings are retained for `operations_inspect`. Consumers should display them alongside the proposed changes before applying a receipt.

MCP input schemas reject invalid statuses, nonnumeric correlation IDs, mismatched strategy pairs, and malformed bid amounts before the handler. Context-dependent rules, such as the parent's strategy and currency, are checked by the handler. IDs and money remain string-based; bulk correlation IDs continue accepting nonnegative integers and decimal strings within the runtime's integer range.

Validation for this release uses synthetic unit, HTTP, MCP, concurrency, and receipt tests plus cross-platform CI and security/distribution checks. Live advertising mutations are not part of this release's acceptance. Older live test results do not establish v0.3.7 runtime acceptance.

Release archives include only public migration and roadmap documents. Private account exports, local research reports, credentials, and fixture identifiers must not enter commits, release notes, or archives.

## References

- [Apple rate limits](https://developer.apple.com/documentation/apple-ads-platform-api/rate-limits)
- [Bid strategy creation](https://developer.apple.com/documentation/apple-ads-platform-api/bidstrategycreate)
- [Ad group updates and automatic bidding](https://developer.apple.com/documentation/apple-ads-platform-api/put-adgroups-_id_)
- [Maximize Conversions best practices](https://ads.apple.com/app-store/best-practices/maximize-conversions)
