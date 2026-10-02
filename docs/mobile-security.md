# Mobile dependency security

## Temporary node-forge mitigation

On 2 October 2026, the release audit failed on [GHSA-86w9-cpqp-85rv](https://github.com/advisories/GHSA-86w9-cpqp-85rv), a high-severity RSA PKCS#1 v1.5 signature-verification flaw in `node-forge <=1.4.0`. Expo CLI and `@expo/code-signing-certificates` pull it into the dependency tree. Certificate and code-signature verification call the affected code, so merely excluding development tools from the audit is insufficient. npm's six high-severity findings are this advisory and its transitive effects, not six independent flaws. The suggested forced Sentry downgrade is not an appropriate fix.

The npm registry still has no patched release, including through the latest Expo CLI checked (`57.0.27`). We keep the original locked `node-forge@1.4.0` version and apply a small, reviewable local patch during `npm ci`/`npm install`:

- Backport the nested `DigestAlgorithm` element-count check from [upstream PR #1152](https://github.com/digitalbazaar/forge/pull/1152), commit `ceba34402e329f0365134f23fe19898756527d65`. This is an **unmerged proposal**, not a maintainer-approved release.
- Additionally reject nonempty NULL parameters, noncanonical OID encodings, and noncanonical DER. The proposal alone still accepted malformed values containing extra bytes. [RFC 8017 §9.2](https://www.rfc-editor.org/rfc/rfc8017.html#section-9.2) specifies DER-encoded `DigestInfo`; legacy BER tolerance is intentionally removed. Ordinary SHA256 signatures, including absent or empty NULL parameters, remain supported.
- Verify the full RSA source file's SHA-256 before and after patching, for every installed copy listed in the lockfile. Unknown source, changed versions, missing files and unexpected paths fail closed. No replacement package, invented package version or new dependency is used.

The implementation and source hashes are in `mobile/scripts/node-forge-patch.mjs`. A clean installation with scripts disabled must explicitly run `npm run postinstall` before any Expo signing/build operation. Normal `npm ci` runs it automatically.

## Audit gate

Run from `mobile/`:

```sh
npm run audit:security
```

This runs the native `npm audit --omit=dev --audit-level=high --json` against the official npm registry. npm still reports the advisory because its database sees the unchanged version number, not the patched source. The wrapper accepts **only this exact advisory and transitive findings caused exclusively by it**, after verifying the installed mitigation. Any other high/critical advisory, unpatched copy, malformed report, network failure or inconsistent result still fails CI. The raw audit is not claimed to be clean.

**Review deadline: 16 October 2026 (00:00 UTC).** The gate fails at that deadline until this mitigation is reviewed. Once a maintained upstream release fixes the issue, update the dependency/lockfile, rerun the parser and Expo compatibility tests, remove the local patch and scoped exception, and restore the direct npm audit command. Do not merely extend the deadline or broaden the exception to silence a failure.

## Verification

The regression reproduces malformed signature acceptance on pristine 1.4.0, and rejects extra children, NULL content, padded OIDs, nonminimal DER lengths and indefinite lengths after patching. Removing the NULL guard makes the behavior test fail. Expo's own certificate validation, CSR verification and SHA256 signing round trip pass. The audit tests cover unrelated advisories, missing patch paths, invalid reports, cyclic dependency causes and expiration. TypeScript, app tests, and web/iOS/Android exports also pass.

Sentry runtime verification is recorded separately in [Sentry monitoring](sentry.md). This change does not publish a mobile app or website release.
