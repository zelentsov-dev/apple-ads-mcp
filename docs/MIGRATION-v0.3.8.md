# v0.3.8 migration notes

This release fixes campaign versus ad-group `bidStrategy.bid` validation and wire serialization for Apple Ads Platform API v1, and normalizes decimal money comparisons in `operations_verify`. Tool names, default read-only mode, credential handling, and explicit preview/apply gates remain unchanged.

## Campaign and ad-group bidStrategy.bid rules

In Apple Ads Platform API v1, `bidStrategy.bid` has opposite requirements on campaigns (`TargetCpa`) and on ad groups (default Max CPT):

| Level | `MANUAL_CPT` + `TAP` | `MAX_CONVERSIONS` + `INSTALL` |
| --- | --- | --- |
| Campaign (`POST /v1/campaigns`, `PUT /v1/campaigns/{id}`) | `bid` must be omitted or `null` (`CAMPAIGN_TARGET_CPA_MUST_BE_NULL` if any non-null `bid` is sent). | `bid` (`TargetCpa`) is required and must be positive (`> 0`). |
| Ad Group (`POST /v1/adgroups`, `PUT /v1/adgroups/{id}`) | `bid` (default Max CPT) is required on creation and must be positive (`> 0`). | Manual `bid > 0` is forbidden on updates (omitted `bid` or a `0` echo is allowed). |

MCP input schemas (`campaign_create_preview`, `campaign_update_preview`, `campaign_bid_strategy_preview`, and `ad_group_create_preview`) and runtime validators (`validateCampaignPayload`, `validateAdGroupPayload`, `validateBidStrategyForOperation`, and `effectiveBidStrategy`) now enforce these resource-specific rules:

- Creating or updating a campaign with `MANUAL_CPT` + `TAP` must not include a `bid` object.
- Creating or switching a campaign to `MAX_CONVERSIONS` + `INSTALL` requires a positive `bid` (`TargetCpa`).
- Creating an ad group with `MANUAL_CPT` + `TAP` requires a positive `bid` (default Max CPT).

## Clearing TargetCpa when switching a campaign to MANUAL_CPT

When updating an existing campaign from `MAX_CONVERSIONS` to `MANUAL_CPT` via `campaign_bid_strategy_preview`, `campaign_update_preview`, or `optimization_plan_preview`, the server now explicitly includes `"bid": null` inside `bidStrategy` in `PUT /v1/campaigns/{id}` so Apple clears the previous `TargetCpa` instead of retaining it via partial object merge.

## Decimal money normalization in operations_verify

Apple Ads normalizes trailing zeros on stored monetary amounts (for example, saving `"amount": "4.00"` and returning `"amount": "4"` on readback). `operations_verify` now compares `amount` fields using exact rational (`big.Rat`) equivalence and treats `"bid": null` expectations as satisfied when Apple omits `"bid"` or returns `null` on readback.

## Build baseline

Source builds now require Go 1.26.10 or newer, and the pinned Docker builder uses `golang:1.26.10-bookworm` to incorporate standard-library security fixes (`GO-2026-6809`).
