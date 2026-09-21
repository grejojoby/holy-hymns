# VM deployment and operations

The current live VM, service paths and verification results are recorded in [oracle-vm.md](oracle-vm.md).

For automatic ARM64/AMD64 image builds and Compose deployment from GitHub Actions, follow [CI/CD setup](ci-cd.md). The workstation build below remains available for manual deployment. With CI/CD, Compose files are versioned under `releases/` and `current` points at the successful release; use the operations commands in that guide to include the correct image digests and VM environment.

## Deployment inputs

Prepare the VM's CPU architecture (`uname -m`), Docker/Compose, API domain and DNS, owner email, Google/Apple app identifiers, SMTP credentials and a private existing OCI backup bucket. Store real secrets only in an untracked `.env` or a protected secret manager. File permissions should be `0600`. Use a random hex PostgreSQL password so it is safe in the database URL.

The stack limits API/PostgreSQL/Caddy/backup container memory to 256/256/96/128 MiB. The Go runtime targets 160 MiB, PostgreSQL permits 20 connections, and logs rotate at three 5 MiB files per service. These limits leave space on a small VM, but capacity must be measured with your actual reader/login load before launch. Size database disk separately and monitor free disk space. Do not run image builds on a 1 GB production VM.

## Build on the workstation

Select `linux/arm64` for an AArch64 VM or `linux/amd64` for x86-64. Use an immutable tag such as a Git commit identifier and retain the previous image for rollback.

```sh
docker buildx build --platform linux/arm64 --load -f backend/Dockerfile -t holy-hymns:release .
docker buildx build --platform linux/arm64 --load -f deploy/backup.Dockerfile -t holy-hymns-backup:release .
docker save -o holy-hymns-images.tar holy-hymns:release holy-hymns-backup:release
```

Transfer the archive and deployment files securely to the VM; `docker load -i holy-hymns-images.tar` installs the built images. Set `HOLY_HYMNS_IMAGE=holy-hymns:release` and `HOLY_HYMNS_BACKUP_IMAGE=holy-hymns-backup:release`. The backend Dockerfile pins Go 1.25.5; runtime image minor tags should be refreshed through a tested release rather than unattended production pulls.

Set `PUBLIC_URL=https://your-api-domain`, exact `ALLOWED_ORIGIN` if you host a web client, and production secrets. Keep `API_BIND=127.0.0.1`. Set the API domain in the mobile build too.

If the VM already has a reverse proxy, forward HTTPS to `127.0.0.1:${API_PORT}` (default `8080`, deployed VM `18080`) and preserve SSE streaming. Set `API_PORT` to an unused host port; the container continues listening on port `8080`. Start the default stack:

```sh
docker compose up -d
docker compose ps
docker compose logs --tail 100 api
```

For a VM without a proxy, set `DOMAIN` and `ACME_EMAIL`, point DNS at the VM, allow inbound TCP 80/443 (and optionally UDP 443), then run `docker compose --profile edge up -d`. Caddy handles certificates and live-event flushing. The proxy network defaults to `172.30.41.0/24`, with Caddy at `172.30.41.10`; adjust both `PROXY_NETWORK_SUBNET` and `CADDY_INTERNAL_IP` if this overlaps existing networks. `TRUSTED_PROXY_CIDRS` defaults to the exact Caddy `/32`, so untrusted clients cannot forge rate-limit identity with forwarded headers. For an existing host proxy, have it overwrite `X-Real-IP` and add only its observed Docker gateway/source `/32` to `TRUSTED_PROXY_CIDRS`; never trust all addresses. Keep the API loopback-bound.

The database is on a private internal Docker network and has no published production port. Restrict SSH and keep the selected API host port private at the VM firewall.

The `migrate` service applies migrations before `api` starts. Before upgrades, take and verify a backup. Database migrations may prevent simply reverting an image; use a verified database restore for an incompatible rollback. Never run `docker compose down -v` against production because it deletes persistent data.

## Accounts and email

Bootstrap the first owner using the README command. `recover-owner --email EMAIL` uses the same `HOLY_HYMNS_OWNER_PASSWORD` environment variable and is reserved for owner recovery through trusted VM access. Both operations are audited by the backend. An existing owner can promote another active verified account to owner in the app; the last active verified owner cannot be removed, suspended or demoted. Do not grant database or VM access to regular editors.

Use Oracle Email Delivery's SMTP submission endpoint with TLS, an approved sender and the required DNS authentication records. Generate `MAIL_ENCRYPTION_KEY` with `openssl rand -base64 32`; it encrypts queued messages at rest. Preserve it across deployments and backups. Configure `SMTP_HOST`, `SMTP_PORT` (normally 587), `SMTP_USER`, `SMTP_PASSWORD` and `SMTP_FROM`. Keep `ALLOW_INSECURE_SMTP=false` in production.

Review your actual tenancy allowance before enabling email. Defaults cap sends at 80 per rolling day and 2,000 per calendar month; lower these values when needed. There is no paid provider fallback. Monitor the admin mail alerts, delivery failures and OCI usage. Other workloads sharing the tenancy also consume quota. Limits reduce risk but do not constitute a provider billing guarantee.

Set exact allowed Google and Apple ID-token audiences in `GOOGLE_CLIENT_IDS` and `APPLE_CLIENT_IDS`. Use the mobile app's documented client IDs and bundle identifiers. Register redirect/return URLs and native signing identifiers in each provider console as required. Keep unused providers blank. Do not add broad, unrelated audiences.

Apple sign-in additionally requires `APPLE_TEAM_ID`, `APPLE_KEY_ID`, and `APPLE_PRIVATE_KEY` from your Sign in with Apple p8 key. The key value may contain literal `\n` line separators, which the backend converts to PEM newlines. Keep this signing key only on the backend. Use a PKCS8 P-256 EC key; Apple appears as an enabled provider only when its audiences, team/key identifiers, signing key and 32-byte `MAIL_ENCRYPTION_KEY` are complete and valid. The backend exchanges Apple's one-time authorization code, retains the refresh grant encrypted with `MAIL_ENCRYPTION_KEY`, and revokes that grant before account deletion. Preserve the encryption key securely; it protects both queued mail and Apple grants. Apple revocation failures leave the account intact for a later retry. For a historical Apple account with no retained grant, local deletion still succeeds and the app provides a link for the user to remove the remaining Apple authorization.

## Encrypted backups without resource provisioning

The optional backup image contains open-source `pg_dump`, `age`, `rclone` and Python. It runs nightly at 02:00 UTC by default. It writes a compressed custom-format dump to temporary memory-backed storage, validates its archive structure, encrypts it to an age **public** recipient, uploads it and downloads it again to verify its SHA-256. The private decryption identity must remain off the VM.

Configure an existing **private** OCI bucket with a separate `holy-hymns` prefix. Disable bucket versioning and automatic multipart retention that could leave billed historical objects; verify any existing lifecycle rules. Configure rclone's S3-compatible provider with the OCI endpoint, existing bucket namespace and customer secret credentials, using `deploy/rclone.conf.example` as a template and the [OCI S3 compatibility instructions](https://docs.oracle.com/en-us/iaas/Content/Object/Tasks/s3compatibleapi.htm) with the [rclone S3 backend](https://rclone.org/s3/). Restrict credentials to the intended bucket where your OCI policy permits. The scripts never create buckets. Keep the rclone file outside the repository or in ignored `deploy/rclone.conf`; ensure UID 10001 inside the container can read it without making it world-readable.

On a trusted workstation, generate the key:

```sh
age-keygen -o holy-hymns-backup.agekey
```

Keep `holy-hymns-backup.agekey` securely offline. Put only its printed public recipient into `BACKUP_AGE_RECIPIENT`. Configure:

- `BACKUP_REMOTE=oci:existing-private-bucket/holy-hymns` and `RCLONE_CONFIG_FILE` to the absolute path of your rclone configuration.
- `OCI_FREE_QUOTA_VERIFIED=true` only after inspecting current tenancy usage and confirming the bucket is within the free allowance.
- `OCI_VERIFIED_FREE_BYTES` to the **unused capacity reserved for this application**, accounting for all other workloads.
- `BACKUP_STORAGE_CAP_BYTES` no larger than that reservation; default 100 MiB.
- `BACKUP_MAX_ARCHIVE_BYTES` no larger than one eighth of the cap; default 12 MiB, maximum supported 32 MiB.

The job refuses to run without these confirmations, refuses to exceed its prefix cap, and makes no automatic quota increases. It counts every object in the prefix when measuring usage. A full cap stops new uploads and requires manual investigation; it does not delete the last good backup to make room. Object storage request/egress allowances must also be verified, including verification downloads and other tenant usage.

```sh
docker compose --profile backup run --rm backup once
docker compose --profile backup up -d backup
docker compose logs --tail 100 backup
```

After a verified upload, only matching Holy Hymns archives older than seven days are removed; unrelated files are never deleted. At least eight maximum-sized archives fit the configured cap, allowing the new backup before retention. Monitor `BACKUP FAILED` logs and verify fresh objects daily; the container does not send an external notification. Recheck free allowances when tenancy usage changes. Do not enable additional paid services to handle a failure automatically.

## Restore drill and recovery

Perform this drill on a trusted workstation before launch and periodically afterward. Download one exact archive with rclone using your configured remote, decrypt it with the offline private identity, and verify the archive listing:

```sh
rclone copyto oci:existing-private-bucket/holy-hymns/EXACT_ARCHIVE_NAME.dump.age ./verified.dump.age
age --decrypt --identity /secure/path/holy-hymns-backup.agekey --output ./verified.dump ./verified.dump.age
pg_restore --list ./verified.dump
```

Restore into a disposable isolated PostgreSQL 17 container; no host port is exposed. This deliberately uses local trust only inside the isolated test container:

```sh
docker run -d --name holy-hymns-restore --network none -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17-alpine
docker exec holy-hymns-restore pg_isready -U postgres
# Continue only after pg_isready reports accepting connections.
docker exec -i holy-hymns-restore pg_restore --username postgres --dbname postgres --exit-on-error --no-owner --no-acl < verified.dump
docker exec holy-hymns-restore psql -U postgres -d postgres -c 'SELECT count(*) FROM songs;'
```

Check expected source/song/account counts and revisions, then run the API integration checks against a disposable restored database. Delete the drill container and decrypted files when finished. A successful `pg_restore --list` alone is not a completed restore drill.

For real recovery, stop API writes, retain the damaged volume for investigation, restore into a new empty production database, restore the matching mail encryption key, apply required migrations, validate content/accounts and then switch the API database URL. Rotate compromised credentials and revoke sessions if the incident involved account exposure. Resume traffic only after testing sign-in and publishing. Record the restore date, archive name, result and measured recovery time.

For the locally executed fixture and bounded reader/login load check, see [verification.md](verification.md).

## Operational checks

Check API/container health, remaining disk and RAM, database pool use, API errors, failed logins, mail quotas and backup freshness. Confirm drafts are inaccessible without admin authorization, updates reach two connected devices within five seconds under the agreed test load, withdrawn songs disappear on reconnect, and both Google/Apple token validation work with production identifiers. A device build, verified restore and measured VM load test remain release gates, not assumptions inferred from unit tests.

References: [Caddy reverse proxy and SSE](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy), [age encryption](https://github.com/FiloSottile/age), [PostgreSQL backup/restore](https://www.postgresql.org/docs/17/backup-dump.html).

The API admits at most 32 ordinary requests at once (20-second processing deadline), up to 250 SSE streams and one password-hash computation at a time. Busy requests receive a retryable503. These bounds complement the container memory limits; repeat the smoke test on the actual VM.
