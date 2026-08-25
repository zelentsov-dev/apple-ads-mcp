# Account onboarding

Use this reference for initial setup, authentication failures, missing apps, or missing ad accounts.

## Required Apple setup

1. An Apple Ads Account Admin grants an API role appropriate to the intended work.
2. The API user creates a public/private key pair and registers the public key in Apple Ads.
3. Store the private key outside the repository and restrict it to the current user.
4. Add only profile metadata and the absolute key path to `accounts.json`.

Start with a read-only Apple role. Enable write permissions only after read tools and account selection have been verified.

## Validation order

1. Run `auth_check` for the explicit profile.
2. Run `ad_accounts_list`, then `ad_account_get`, and record the correct `adAccountId`, role, currency, time zone, payment model, and product features.
3. Run `org_get` for the organization returned by Apple.
4. Run `advertiser_resources_list` when the account may advertise apps delegated by a content provider. Ignore non-App Store delegation types.
5. Run `apps_search` with owned apps enabled and confirm the chosen app through `apps_get`.
6. Run `apps_eligibility` for every target storefront and placement.
7. Run `account_health` for the explicit app when a full readiness decision is needed.

Do not treat App Store availability alone as placement eligibility. Missing ACL, `APPSTORE_APP_MANUAL`, content-provider delegation, ownership, currency, or placement eligibility is a readiness failure that must be reported before preview.

App Store Connect access and Apple Ads API access are separate. Initial App Store Connect account linking and Apple Ads API-user administration remain web administration tasks when Apple does not expose an API operation for them.

Never ask for private-key contents, access tokens, or client secrets in chat. A path and non-secret identifiers are sufficient.
