# Migrating to v0.3.1

v0.3.1 is a safety-focused corrective release for the v0.3 optimizer. Existing MCP tool names are unchanged.

## Policy change

Policies with `permissions.bid: true` must add a positive `maxBid` in the same currency as `maxTotalDailyBudget` and `maxCampaignDailyBudget`. Bid increases are capped only by `maxBid`; a campaign daily-budget cap is no longer treated as a bid cap. A current managed bid above `maxBid` rejects the baseline until the operator explicitly adjusts the bid or policy cap.

## Evidence change

Optimization requires exactly 28 unique consecutive completed UTC dates ending yesterday for campaign and biddable reports. Requests include Apple's `EMPTY_METRICS` rows. A row with all four requested metrics absent is normalized to zero; a row with only some requested metrics absent is rejected. Missing dates, duplicate dates, unexpected dates, malformed counts, integer overflow, invalid money, and incomplete pagination now reject the baseline instead of becoming zero or disappearing.

Report `fields` now accept metric columns only, as required by the Apple Ads Platform API v1. Resource identifiers, status, bid configuration, and other entity properties are returned by Apple in each row's `metadata` object and must not be placed in `fields`. Existing metadata fields remain valid for endpoint-supported filters and sorting.

## Reconciliation change

Before the first Apple write in an optimization plan, the server durably persists a receipt-hash-bound `applying` intent and sanitized typed verification recipe. History updates use an inter-process file lock, parent-directory sync on POSIX, and replace-with-write-through on Windows. An unresolved intent or inconclusive item is retained through bounded-history compaction and blocks new optimization plans until `operations_verify` establishes the actual state of every affected object. The original receipt can be verified after a server restart or normal in-memory receipt eviction; the receipt itself is not written to history.

Reconciliation now participates in cooldown calculation. `matched` and `matched_after` actions are treated as applied at their original intent/apply time. `matched_before` actions are treated as not applied.

Auto-resume requires a verified optimizer-owned pause whose recorded Apple `modificationTime` still matches the campaign. A later manual edit, resume, or pause revokes optimizer ownership.

## Create verification

Successful creates retain the returned resource ID and `operations_verify` performs a direct typed GET against that ID. An ambiguous create without an Apple-returned ID always remains inconclusive; a bounded inventory page is not used as proof of unique creation. Re-read inventory manually and resolve the duplicate risk before issuing any new mutation.
