# Sentry error monitoring

Organization: [grejo](https://grejo.sentry.io/). Projects:

- [holy-hymns-backend](https://grejo.sentry.io/projects/holy-hymns-backend/) — Go API, identity/catalogue failures, request panics, mail and retention workers, command failures.
- [holy-hymns-mobile](https://grejo.sentry.io/projects/holy-hymns-mobile/) — release-build JavaScript errors, React rendering failures, native crashes and handled API responses with status 500 or higher.

These reports answer: which operation failed, which release/environment is affected, and which mobile/server failures belong to the same request? Expected validation errors, unauthorized requests and ordinary offline/device cancellations do not produce explicit error reports. Existing local backend error logs remain available as JSON.

## Backend configuration

Set these values in the VM's private `/opt/holy-hymns/.env`; Compose passes them to the API:

```dotenv
SENTRY_DSN=https://1316973edbc8d368af4076b0a8e91fbc@o4507351643389952.ingest.us.sentry.io/4512186331693056
SENTRY_ENVIRONMENT=production
```

The release workflow embeds `holy-hymns-backend@<git-sha>` in the image. `SENTRY_RELEASE` is an optional runtime override; normally leave it blank. Blank `SENTRY_DSN` disables reporting. No Sentry administrative token belongs on the VM.

Every request receives a generated `X-Request-ID`. It is attached to error logs and Sentry events, with the matched route template and entry point. Mobile API failures include the same identifier. The SDK sends asynchronously and flushes for up to three seconds on command exit. SDK initialization rejects an invalid configured DSN; routine health probe processes do not initialize Sentry.

## Mobile builds and source maps

Copy the public settings from `mobile/.env.example`. `EXPO_PUBLIC_SENTRY_DSN` enables release-build reporting; development builds are disabled. The native SDK derives releases from the app version/build unless `EXPO_PUBLIC_SENTRY_RELEASE` overrides it. Rebuild the native app after adding the SDK; JavaScript reloads cannot install its native modules.

The Expo config plugin and Metro configuration produce debug IDs and upload native release source maps/debug symbols during native builds. The organization build token `holy-hymns-sourcemap-upload` has `org:ci` scope. Keep `SENTRY_AUTH_TOKEN` in ignored, mode-0600 `mobile/.env.local` or in the build service's secret store. Never prefix it with `EXPO_PUBLIC_`, place it in app config, or commit `sentry.properties`.

For local builds, the ignored file also contains `SENTRY_ORG=grejo`, `SENTRY_PROJECT=holy-hymns-mobile`, and `SENTRY_URL=https://sentry.io/`. GitHub's current workflow checks JavaScript exports only; it does not build or publish the native app. A future EAS/CI native builder must receive the upload token as a build secret.

For an exported bundle, explicitly upload its source maps (the export alone does not upload):

```sh
cd mobile
npm run export:native -- --source-maps
node --env-file=.env.local node_modules/@sentry/cli/bin/sentry-cli sourcemaps upload dist
```

Keep the exact source maps for the binary being distributed. Native crash symbolication requires that build's native debug symbols as well; JavaScript exports do not validate native crash handling.

## Privacy and scope

Tracing, profiling, replay, screenshots, view hierarchy attachments, Sentry Logs and session tracking are disabled. Backend reports use operation names, error types, stack frames and PostgreSQL SQLSTATE instead of raw driver/provider messages. They do not attach requests, bodies, headers, user profiles or breadcrumbs. Full diagnostic details remain in operator-only local logs.

JavaScript reports remove requests, user data, extra fields, breadcrumbs and frame variables; message filtering removes URLs, common credential forms and email addresses. Native crash reports bypass JavaScript filtering and rely on native SDK privacy settings and Sentry's project-side data scrubbing. Default data scrubbing and IP scrubbing are enabled on both projects, verified on 2 October 2026. Do not add account identity, lyric text, search terms or credentials to Sentry context. Review the deployed diagnostics and retention in the privacy policy/store disclosures before a mobile release; see `mobile/PRIVACY.md`.

## Verification and troubleshooting

The backend is live as of 2 October 2026. An on-VM probe was received as event `f3b52a30bd7243e581f8fa7ea65d3c1b`, release `holy-hymns-backend@e62daed`, environment `verification`; see the [deployment record and rollback instructions](oracle-vm.md#sentry-backend-deployment--2-october-2026). Both projects have verified IP scrubbing. The Expo dependency audit blocker now has a [tested local mitigation](mobile-security.md), with a narrowly scoped, expiring audit exception that requires the patched source. No mobile or website release was distributed.

On 2 October 2026, an isolated iPhone 17 Pro simulator running iOS 26.5 completed a Release-build test without an attached debugger. The temporary app used bundle ID `org.holyhymns.sentryverification`, release `holy-hymns-mobile@028a615-verification`, and environment `verification`. A temporary alternate JavaScript entry reported one labeled exception and called `Sentry.nativeCrash()` once, using a persisted marker to prevent a crash loop. The normal app entry has no crash trigger. The build uploaded matching Hermes source maps and 30 native debug files; restarting the app sent the saved native crash.

| Check | Received evidence |
| --- | --- |
| JavaScript error | [Issue 7768052829](https://grejo.sentry.io/issues/7768052829/), event `3e6ce05421bb4db0bd34d1e764a9e7f3`; `verifySentryMobileRelease` resolves to the original TypeScript entry, line 12 |
| Native crash | [Issue 7768054882](https://grejo.sentry.io/issues/7768054882/), event `9b6a2ea9c6454224a825c76399ab3f27`; fatal `EXC_BAD_ACCESS`, `+[SentrySDKInternal crash]` symbol present, no symbolication errors |
| Privacy | Neither received event contains a user IP address |

The JavaScript test's application frames resolve correctly, but five runtime/helper frames still report `js_invalid_sourcemap_location`; complete mapping of those frames is not verified. This was a simulator build, not a store-signed or physical-device test. Android runtime crash delivery and signed-device verification remain release checks; Android export and native configuration tests pass.

The CLI-only probe sends a labeled synthetic event without affecting HTTP routes:

```sh
hh_compose exec -e SENTRY_ENVIRONMENT=verification api /app/holyhymns sentry-test
```

Use the `hh_compose` helper in [VM operations](oracle-vm.md). Find the printed event ID in the backend project, environment `verification`; a successful flush alone does not prove ingestion. There is no public crash/test endpoint.

For a real failure, filter Sentry by release/environment and `request_id`. Find its `sentry_event_id` in `hh_compose logs --tail 100 api` for operator-only error details. Investigate the route/operation, SQLSTATE and recent deployments. If events disappear, check the DSN and environment filters, project quota/rate limits, VM outbound HTTPS, and Sentry ingestion status. For unreadable mobile stacks, check that the exact build's source maps and native symbols uploaded successfully.

Sources: [Sentry Go SDK](https://github.com/getsentry/sentry-go), [Sentry Expo setup](https://docs.sentry.io/platforms/react-native/guides/expo/).
