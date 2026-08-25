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

1. Select one named policy whose profile, ad account, app, currency, and campaign IDs match the request.
2. In `learning`, read the 28-day baseline, last 7 versus prior 7 windows, daily P25-P75 CPI range, budget utilization, and Apple recommendations. Do not create an apply receipt.
3. Ask the operator to set the business `targetInstallCPA`; never derive it automatically from Apple or historical CPI.
4. In `active`, review `optimization_plan`, its minimum-data checks, cooldowns, caps, and reasons before preview.
5. Preview the whole bounded plan once. Verify the action order: pauses/decreases, then strategy/bids, then budgets, then allowed resumes/increases.
6. Apply the same receipt once and verify every item. A failed dependency is skipped; an ambiguous result stops all remaining actions.
7. Review `optimization_history` before the next run.

Do not apply Apple's recommendation solely because it exists. Show Apple's amount, the business target, the policy cap, and the final calculated amount separately. The optimizer never deletes. It can retest only an object it previously paused and only when the policy permits retest.

## Shared budgets

1. Confirm the selected account and `paymentModel` through `ad_account_get`.
2. Stop with `not_eligible` for `PAYG`; continue only for `LOC`.
3. Resolve a local billing profile by name and keep its private fields out of the conversation.
4. Preview create/update or campaign assign/unassign, verify the selected account and currency, apply once, and read back both budget and campaigns.
5. Unassign every campaign before considering shared-budget deletion.

## Resource lifecycle

Use delete only for an explicitly authorized irreversible cleanup. Confirm exact name or keyword text, pause the parent campaign, inspect the entire bounded cascade, preview one resource, apply once, and verify `deleted: true`. Never retry a DELETE after timeout and never delete a business object merely because it performs poorly.
