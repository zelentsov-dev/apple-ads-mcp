# Tool routing

## Access and inventory

- Identity or OAuth check: `auth_check`
- Accessible accounts and roles: `ad_accounts_list`
- Organization state: `org_get`
- Owned app lookup: `apps_search`, then `apps_get`
- Storefront eligibility: `apps_eligibility`

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
- Consolidated current state: `account_health`, `campaign_audit`

Keep report ranges explicit and bounded. Paginate instead of requesting an entire account history.

Query tools expose common top-level fields: `filters`, `sorting`, `pagination`, `fields`, `groupBy`, `timeRange`, and `options`. Use `pagination.pageSize` up to 200. Filters use `field`, `operator`, and a scalar or array `value`; do not wrap these fields in a raw request object.

## Mutations

Use the resource-specific preview tool and apply only through `operations_apply`. After an ambiguous result, use `operations_verify` and the matching `*_get` tool; `operations_inspect` only reports receipt metadata. There is no raw API tool.
