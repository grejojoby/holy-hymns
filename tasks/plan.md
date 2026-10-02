# Approved implementation

Build Holy Hymns using Expo/React Native and a self-hosted Go/PostgreSQL API on Docker. Public online-first lyrics browsing, Malayalam/Manglish search, optional local and synchronized favorites, self-hosted accounts with Google/Apple, server-authorized in-app CMS, protected owner role, draft/publish/version history, durable SSE invalidation, first-party analytics, one-time Grejo Lyrics import, capped Oracle SMTP, encrypted quota-bound off-VM backups.

See ../docs/contract.md for implementation boundaries. No paid services or mandatory hosted software. Production VM, DNS, social login, SMTP and backup credentials are deployment inputs, not included in source.

## Native delivery: iPhone, iPad and Android

Extend the existing Expo application. The iOS target is universal (iPhone and
iPad); Android uses the same reader, accounts and backend. Keep native projects
generated from app configuration, and keep signing credentials outside Git.

1. Add a window-responsive catalogue/reader split at 900 points, with selected
   hymn feedback. Preserve catalogue state and the mounted reader on resize.
   Narrow windows and large accessibility text retain the phone navigation.
2. Add repeatable native prebuild/release commands, explicit build numbers, and
   generated-project checks for iPad orientations and Android links/permissions.
3. Run TypeScript, tests, native exports and available native compilation. Record
   missing SDKs/signing inputs honestly; document local install and store builds.

Verification includes switching hymns, resizing with a hymn open, closing the
reader, switching tabs, and checking phone/tablet layouts. Existing external
release gates remain outstanding until actually verified.
