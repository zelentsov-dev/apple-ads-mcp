# Tool routing

## Access and inventory

- Identity or OAuth check: `auth_check`
- Accessible accounts and roles: `ad_accounts_list`
- Selected ad-account metadata and currency: `ad_account_get`
- Organization state: `org_get`
- Content-provider delegation: `advertiser_resources_list`
- Owned app lookup: `apps_search`, then `apps_get`
- Storefront eligibility: `apps_eligibility`
- Locales and supported languages: compact `app_locale_details`, then `includeAssets=true` with one exact `languageCode` only when assets are needed; `supported_app_languages`
- App Store geo lookup: `app_store_geo_search`
- Full readiness: `account_health`

## Bounded inventory

- Campaigns: `campaigns_query`
- Ad groups: `ad_groups_query`
- Targeting keywords: scoped `keywords_query`; use it rather than a report for complete inventory, including zero-impression keywords
- Negative keywords: `negative_keywords_query` with an explicit keyword ID, ad-group scope, or campaign-level `adGroupId IS_NULL` scope
- Ads and creatives: `ads_query`, `creatives_query`
- Shared budgets: `shared_budgets_query`, `shared_budget_get`
- One campaign and bounded children: `campaign_inventory`

Use `pagination.pageSize` up to 200 and continue with `next`. Prefer a campaign-scoped inventory call before any multi-object change.

## Opportunity research

- Related terms: `keyword_suggestions`, `phrase_suggestions`, `category_suggestions`
- Demand signals: `search_term_popularity`
- Competitive visibility: typed UTC `impression_share` with Adam ID, country, range, granularity, and report type
- Consolidated app view: `app_opportunities`; inspect each section's `ok`, `empty`, or `upstream_error` status independently

Treat suggestions, popularity, and impression share as different signals. Do not present any one of them as guaranteed performance.

## Performance and audit

- Campaign/ad group/ad/keyword/search-term performance: `campaign_report`, `ad_group_report`, `ad_report`, `keyword_report`, `search_term_report`
- Apple recommendations: `daily_budget_recommendations`, `target_cpa_recommendations`
- Historical changes: `change_history` with required `start` and `end`; metadata defaults to `latest`
- One historical change: `change_history_detail`
- App rejection details: `app_rejection_reasons_query`, `app_rejection_reason_get`
- Campaign delivery reason: `campaign_status_reason_details`
- Consolidated current state: `account_health`, `campaign_audit`

Keep report ranges explicit and bounded. Paginate instead of requesting an entire account history.

Report tools accept endpoint-specific selectors, fields, filters, groupings, and bounded pagination. Do not invent a generic report request or place an arbitrary Apple envelope inside the input.

## Optimization

- Policies: `optimization_policies_list`, `optimization_policy_get`
- Evidence: `optimization_baseline`
- Read-only decisions: `optimization_plan`
- Local bounded history: `optimization_history`
- Authorized composite mutation: `optimization_plan_preview`

Use a `learning` policy to establish a 28-day baseline and show Apple recommendations without an apply receipt. Use `active` only when the policy contains the operator's target install CPA, caps, and permissions. Optimization is on-demand; never imply that the server will run later by itself.

## Mutations

Use the most specific preview tool: budget, countries, schedule, targeting, Search Match, bid, pause, or resume. Use bulk preview tools only for at most 100 unique non-negative integer correlation IDs. `keywords_bulk_create_preview` can bind multiple per-item ad groups into one Apple bulk request and one aggregate receipt; the top-level `adGroupId` remains the default for items that omit it.

Apply only through `operations_apply`. After apply, call `operations_verify` and the matching get/query tool. After an ambiguous result, verify before any new write; `operations_inspect` only reports receipt metadata. There is no raw API tool.

Use `campaign_bid_strategy_preview` for MANUAL_CPT or an Apple-eligible Search Results `MAX_CONVERSIONS` change. Use typed shared-budget previews only when `ad_account_get` reports `LOC`; MCP input contains a local `billingProfile` name, never billing PII.

Delete tools are specialized by resource. Use them only for an explicitly authorized irreversible task after reading the full cascade inventory and exact expected text. Delete is never part of optimization.

For recommendation apply, always provide `maximumAmount`. Use Apple's current suggestion only when it is in the account currency and does not exceed the cap. Do not simulate `apply all` by issuing repeated calls.
