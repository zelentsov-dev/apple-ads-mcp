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
2. Run `ad_accounts_list` and record the correct `adAccountId` and role.
3. Run `org_get` for the organization returned by Apple.
4. Run `apps_search` with owned apps enabled.
5. Run `apps_eligibility` for the target storefronts.

App Store Connect access and Apple Ads API access are separate. Initial App Store Connect account linking remains a web administration task when Apple does not expose an API operation for it.

Never ask for private-key contents, access tokens, or client secrets in chat. A path and non-secret identifiers are sufficient.
