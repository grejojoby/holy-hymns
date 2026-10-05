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

This runs the native `npm audit --omit=dev --audit-level=high --json` against the official npm registry. npm still reports the advisory because its database sees the unchanged version number, not the patched source. The wrapper accepts **only the two exact advisories documented here and transitive findings caused exclusively by them**, after verifying every installed mitigation. Metro cycles are traversed without recursion; every reachable finding must lead to a verified advisory root. Cycles without a verified root still fail. Any other high/critical advisory, unpatched copy, malformed report, network failure or inconsistent result still fails CI. The raw audit is not claimed to be clean.

**Review deadline: 16 October 2026 (00:00 UTC).** The gate fails at that deadline until this mitigation is reviewed. Once a maintained upstream release fixes the issue, update the dependency/lockfile, rerun the parser and Expo compatibility tests, remove the local patch and scoped exception, and restore the direct npm audit command. Do not merely extend the deadline or broaden the exception to silence a failure.

## Verification

The regression reproduces malformed signature acceptance on pristine 1.4.0, and rejects extra children, NULL content, padded OIDs, nonminimal DER lengths and indefinite lengths after patching. Removing the NULL guard makes the behavior test fail. Expo's own certificate validation, CSR verification and SHA256 signing round trip pass. The audit tests cover unrelated advisories, missing patch paths, invalid reports, cyclic dependency causes and expiration. TypeScript, app tests, and web/iOS/Android exports also pass.

Sentry runtime verification is recorded separately in [Sentry monitoring](sentry.md). This change does not publish a mobile app or website release.

## Temporary braces mitigation — 4 October 2026

The category release was blocked by [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm): deeply nested brace/parenthesis patterns can exhaust the recursive AST walkers in `braces <=3.0.3`. Metro and Expo inherit the finding. The npm registry still reports 3.0.3 as latest, with no patched release.

`mobile/scripts/braces-patch.mjs` applies the five source-file changes from [upstream PR #72](https://github.com/micromatch/braces/pull/72), pinned to `28d440b5dd449dbf1fe6f3506cf94ecca4d02660`. This remains an **unmerged proposal**, not an official fixed release. Exact original and patched SHA-256 hashes, substitutions, provenance, and the upstream MIT license are recorded in `braces-patch.json`. Every installed copy from the lockfile is verified; unexpected versions, modified source, and paths outside the installed package fail closed. Installation is offline once npm has fetched dependencies.

The patch limits parser nesting and caller-provided AST depth to 100, accepts stricter limits, and rejects cyclic parent chains during expansion. This intentionally rejects unusually deep patterns. Regression tests first reproduced stack exhaustion without the patch, then verified bounded rejection, brace/parenthesis boundaries, custom ASTs, fractional limits, and ordinary Metro glob/range matching. The audit still blocks unknown advisories, additional unpatched copies, missing causes, and rootless cycles. Both mitigations retain the **16 October 2026** review deadline; no deadline was extended to pass the release.
