# Verification record and repeatable checks

These checks were performed locally on 19 September 2026. They do not establish the capacity of the production Oracle VM or replace native-device/provider testing.

## Recorded results

- Go importer tests and the race detector passed, including the full 331-post source snapshot, Malayalam preservation, script/stanza splitting, duplicate title flags and media-only exclusion.
- Seven backup policy tests passed, including cap refusal, unrelated-object retention and refusal to delete previous archives after a corrupted upload.
- Docker Compose production/development configuration validated with `.env.example`.
- The complete Go backend image built successfully for local Linux ARM64 with Go 1.25.5. The separate backup image also built.
- An isolated PostgreSQL 17 fixture was dumped, age-encrypted, decrypted, byte-compared and restored into a second database. The restored Malayalam line matched the original. No OCI resources or production data were used.
- The local API served 25 concurrent SSE connections, 100 search requests and three complete sign-in/sign-out cycles without failures in 8.06 seconds. A temporary container limited to one CPU and 256 MiB also passed; its sampled memory usage was 134.7 MiB, and search median/p95 latency was 7.0/16.3 ms. These are observed local smoke measurements, not a promised capacity or a measured worst-case memory peak.

The API load result covers the implementation at the time of the test. Repeat the checks after final authentication or schema changes and on the actual VM before launch. Real Google/Apple configuration, SMTP delivery, OCI upload/download, a restore of production-sized data, App Review and real iPhone/Android behavior require their respective deployment inputs.

## Application and security verification

- The complete Go suite passed with the race detector against disposable PostgreSQL schemas. `go vet ./...` passed. Coverage includes reader denial across all administrative API families, draft isolation, exact/partial/approximate and mixed-script search, stale song/category/settings conflicts, revision restore, import metadata and rerun safety, staff analytics exclusion, and bounded request/client state.
- The live publication test observed changed revisions within five seconds, then verified withdrawal and durable state through a new server instance. A browser smoke check also confirmed a private draft did not alter the open reader until publication; the published text then updated without manual refresh.
- Identity tests use real signed JWT fixtures, a local SMTP server and mock Apple endpoints. They cover invalid issuer/audience/signature/nonce/expiry, password and account lifecycle, session revocation, invitations and owner transfer, last-owner protection, SMTP quotas across restart/concurrency, Apple encrypted grants and revocation failure/retry. Production Google/Apple/SMTP behavior is still a release gate.
- A fresh database imported 331 source posts as 328 draft candidates, excluding 3 media-only entries. No source lyrics were published automatically. Two clearly labelled original reading-layout fixtures remain only in the disposable local preview while the UI is being reviewed; they are not included in the source import or deployment seed.
- Mobile TypeScript checking, 11 focused logic tests, Expo dependency compatibility and web/iOS/Android JavaScript exports passed. The actual native iOS simulator build succeeded with no errors and launched on an iPhone 17 Pro simulator running iOS 26.5. Android SDK tooling is absent on this machine, so a native Android build and device smoke test remain outstanding.
- Browser verification covered guest/account/admin visibility, owner login, create/save/publish, Malayalam rendering, script switching, favorites, privacy information and live updates. Native device accessibility, keyboard behavior and real iPhone testing are still required.

## Readability redesign

The mobile UI was revised using a plain hymn-list reference from Dribbble, then reduced slightly after the user reviewed the native screenshot. System interface text, Noto Sans Malayalam lyrics, restrained colors, compact rows, expandable filters and 48-pixel touch targets replace the earlier decorative layout. Enabled theme text contrast is at least 5.91:1; essential input boundaries exceed 3:1. See [design.md](design.md). TypeScript and all 11 logic tests pass. Browser checks cover Malayalam/script switching, retained search after reading and 320/390-pixel layout geometry; native accessibility scaling and screen-reader checks remain release gates.

## Extracted collection and deferred upload

- A fresh paginated feed fetch returned 331 posts; all 331 source hashes matched the saved Atom snapshot. The reviewed parser exports 288 draft candidates: 46 Malayalam files, 279 Manglish files and 37 bilingual candidates. Forty-three posts are excluded from automatic upload, with original HTML retained for all 331 and 41 separate review text files. This supersedes the initial broad 328-candidate classification described above.
- Parser regression checks cover Malayalam titles after Latin titles, unsupported-language separation, collections/index/image posts, credited metadata and chord markup that incorrectly wraps real lyric words. Unicode and source stanza text are retained without generated transliteration.
- The exporter passes database-free CLI and no-overwrite checks. Its length-prefixed UTF-8 content fingerprint matches an independently asserted Python test vector. Local validation of the final folder finds all 288 candidates unchanged from extraction.
- Fifteen Python uploader tests pass, including unsafe paths/symlinks, missing scripts, edited-content fingerprints, canonical URLs, redirects, token handling, bounded retries, dry runs and session cleanup. The complete server race suite passes with draft privacy, source deduplication, concurrent changed-source imports, access control, validation and atomic audit rollback coverage.
- A real CLI upload test used a temporary local PostgreSQL database and API: dry run reported 288 would-create records and wrote no songs; first upload created 288 drafts; the second reported 288 unchanged; a changed-content request produced one conflict and preserved the stored text. Zero songs became public. All 577 non-dry import operations had audit records. The disposable test database was removed afterward.

The upload script is ready for the deployed server; production credentials and infrastructure were not needed for these checks. Use [the collection instructions](../content/lyrics/README.md) when the production backend is available.

## CI/CD verification

- The API and backup Dockerfiles each built a combined OCI image for `linux/amd64` and `linux/arm64`. Both exported manifests and image configurations contain the expected architectures and run as UID/GID `10001:10001`. The temporary validation builder was removed afterward; no images were published.
- The CI backend helper passed `go vet`, the complete race suite and a binary build against its own disposable PostgreSQL 17 container, then removed that container. Local Go commands used the configured Go 1.25.5 binary.
- Mobile typechecking, all 11 logic tests, web/iOS/Android JavaScript exports and a production dependency audit passed. The audit used the public npm registry and reported zero vulnerabilities.
- All 41 Python script tests passed, including SSH host-key enforcement, secret-free release bundles, migration/health failures, private diagnostic logs, profile removal and release-pointer behavior. Deployment protocol tests simulate Docker and SSH; they do not establish a successful remote deployment.
- Checksum-verified actionlint 1.7.12 accepted both workflows. Production/development Compose configurations validate with every optional profile, and deployment shell syntax passes.

GitHub publication and the automated Actions deployment remain unverified. SSH connectivity, the manual ARM64 deployment, HTTPS/SSE and preservation of existing VM services subsequently passed; see [oracle-vm.md](oracle-vm.md). Activation steps and free-usage controls are in [ci-cd.md](ci-cd.md).

## Bounded API smoke test

The script accepts only localhost/loopback URLs, holds 25 SSE connections, sends at most 100 small searches, and optionally performs three sign-in/sign-out cycles. It changes no catalogue content. Optional authentication creates audit/session records and logs out its own sessions. It prints aggregate counts and timings, never passwords or tokens.

```sh
python3 scripts/load-smoke.py --base-url http://127.0.0.1:8080/v1 --duration 8
```

To include authentication, use a disposable verified development account; enter the password interactively rather than saving it in a file:

```sh
export HOLY_HYMNS_SMOKE_EMAIL=your-disposable-development-account@example.com
read -r -s HOLY_HYMNS_SMOKE_PASSWORD
export HOLY_HYMNS_SMOKE_PASSWORD
python3 scripts/load-smoke.py --duration 8
unset HOLY_HYMNS_SMOKE_PASSWORD HOLY_HYMNS_SMOKE_EMAIL
```

Do not repeatedly loop sign-in smoke tests: normal per-account/IP abuse limits still apply. A run lasts at most 23 seconds (duration up to 15 seconds plus a bounded cleanup deadline), and failures produce a nonzero exit status. For container measurements, observe `docker stats` alongside the test; monitor database and host resources too.

## Repeat the isolated encrypted restore fixture

This command creates an ephemeral, network-isolated PostgreSQL instance inside the backup image, generates a disposable age key there, verifies a restore, and removes the container and temporary data afterward. It does not exercise OCI; follow the real remote restore drill in [deployment.md](deployment.md) separately.

```sh
docker build -f deploy/backup.Dockerfile -t holy-hymns-backup:validation .
docker run --rm --network none --user postgres --entrypoint sh -i holy-hymns-backup:validation <<'SH'
set -eu
umask 077
export PGHOST=/tmp PGUSER=postgres
initdb -D /tmp/pgdata -A trust >/tmp/init.log
pg_ctl -D /tmp/pgdata -o '-k /tmp -c listen_addresses=' -w start >/tmp/start.log
createdb source
psql -d source -v ON_ERROR_STOP=1 -c "CREATE TABLE songs (id integer PRIMARY KEY, lyrics text); INSERT INTO songs VALUES (1, 'കർത്താവേ കനിയണമേ');" >/tmp/seed.log
pg_dump --format=custom --compress=6 --no-owner --no-acl --file /tmp/database.dump source
age-keygen -o /tmp/private.agekey 2>/tmp/keygen.log
recipient=$(age-keygen -y /tmp/private.agekey)
age --recipient "$recipient" --output /tmp/database.dump.age /tmp/database.dump
age --decrypt --identity /tmp/private.agekey --output /tmp/restored.dump /tmp/database.dump.age
cmp /tmp/database.dump /tmp/restored.dump
createdb restored
pg_restore --dbname restored --exit-on-error --no-owner --no-acl /tmp/restored.dump
restored_lyric=$(psql -d restored -At -c 'SELECT lyrics FROM songs WHERE id=1')
[ "$restored_lyric" = 'കർത്താവേ കനിയണമേ' ]
pg_ctl -D /tmp/pgdata -m fast -w stop >/tmp/stop.log
printf 'Encrypted PostgreSQL restore verified.\n'
SH
```
