# Tool routing

## Access and inventory

- Identity or OAuth check: `auth_check`
- Accessible accounts and roles: `ad_accounts_list`
- Selected ad-account metadata and currency: `ad_account_get`
- Organization state: `org_get`
- Content-provider delegation: `advertiser_resources_list`
- Owned app lookup: `apps_search`, then `apps_get`
- Storefront eligibility: `apps_eligibility`
- Locales and supported languages: `app_locale_details`, `supported_app_languages`
- App Store geo lookup: `app_store_geo_search`
- Full readiness: `account_health`

## Bounded inventory

- Campaigns: `campaigns_query`
- Ad groups: `ad_groups_query`
- Targeting keywords: `keywords_query`
- Negative keywords: `negative_keywords_query`
- Ads and creatives: `ads_query`, `creatives_query`
- Shared budgets, read-only: `shared_budgets_query`
- One campaign and bounded children: `campaign_inventory`

Use `pagination.pageSize` up to 200 and continue with `next`. Prefer a campaign-scoped inventory call before any multi-object change.

## Opportunity research

- Related terms: `keyword_suggestions`, `phrase_suggestions`, `category_suggestions`
- Demand signals: `search_term_popularity`
- Competitive visibility: `impression_share`
- Consolidated app view: `app_opportunities`

Treat suggestions, popularity, and impression share as different signals. Do not present any one of them as guaranteed performance.

## Performance and audit

- Campaign/ad group/ad/keyword/search-term performance: `campaign_report`, `ad_group_report`, `ad_report`, `keyword_report`, `search_term_report`
- Apple recommendations: `daily_budget_recommendations`, `target_cpa_recommendations`
- Historical changes: `change_history`
- One historical change: `change_history_detail`
- App rejection details: `app_rejection_reasons_query`, `app_rejection_reason_get`
- Campaign delivery reason: `campaign_status_reason_details`
- Consolidated current state: `account_health`, `campaign_audit`

Keep report ranges explicit and bounded. Paginate instead of requesting an entire account history.

Report tools accept endpoint-specific selectors, fields, filters, groupings, and bounded pagination. Do not invent a generic report request or place an arbitrary Apple envelope inside the input.

## Mutations

Use the most specific preview tool: budget, countries, schedule, targeting, Search Match, bid, pause, or resume. Use bulk preview tools only for at most 100 unique correlation IDs in one campaign/ad-group scope.

Apply only through `operations_apply`. After apply, call `operations_verify` and the matching get/query tool. After an ambiguous result, verify before any new write; `operations_inspect` only reports receipt metadata. There is no raw API or delete tool.

For recommendation apply, always provide `maximumAmount`. Use Apple's current suggestion only when it is in the account currency and does not exceed the cap. Do not simulate `apply all` by issuing repeated calls.
