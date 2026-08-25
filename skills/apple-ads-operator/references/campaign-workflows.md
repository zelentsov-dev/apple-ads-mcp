# Campaign workflows

## Keyword research

1. Confirm ownership and eligibility for every target storefront.
2. Collect keyword and phrase suggestions for the app.
3. Query search-term popularity for the same markets.
4. If campaigns exist, compare suggestions with search-term and keyword reports.
5. Separate proposed targeting keywords from negative-keyword candidates and show the evidence for each.
6. Use bulk previews only after removing local duplicates and assigning a unique `correlationId` to every item.

## Campaign audit

1. Read `campaign_inventory`, then campaign, ad-group, keyword, and search-term reports for one explicit date range.
2. Read current budgets, bids, statuses, recommendations, and recent change history.
3. Distinguish Apple-provided recommendations from the agent's inference.
4. Report under-spend, expensive terms, irrelevant terms, coverage gaps, and data insufficiency separately.

## Campaign creation

1. Confirm app ownership, placement eligibility, account currency, countries, budget, dates, bid strategy, product page, and desired initial status. Ad groups require an ISO 8601 `startTime` in the account timezone.
2. Use one campaign per placement because Apple fixes placement at campaign level.
3. Prefer a `PAUSED` initial state and clear fixture or business names.
4. Preview the campaign, ad group, keywords, negatives, creative, and ad assignment before applying any part.
5. Apply only receipts that match the authorized plan. Verify and re-read every created resource before continuing to its children.
6. For Search Results, separate exact targeting from discovery and use exact negatives to avoid internal competition when the user's plan requires it.

Use `creatives_query` before creative creation. Reuse the existing Default Product Page creative because Apple permits only one per app and ad account. Do not create an explicit `Ad` resource for Search Results because the Platform API rejects it for that placement. Other placements remain subject to Apple eligibility and creative validation.

Placement-incompatible targeting, Maps-only values, an unexpected storefront, a currency mismatch, or an ineligible product page is a stop condition, not a reason to weaken validation.

## Optimization

Change one decision category at a time: targeting, negatives, bids, budget, creative, or status. Use a comparable report window and preserve the before state. Do not apply Apple's recommendation solely because it exists. For a recommendation apply, show Apple's amount, the operator cap, and the final amount separately.
