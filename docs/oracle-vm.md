# Oracle VM deployment

Initially deployed and verified on 19 September 2026. The public domain was updated and verified on 2 October 2026.

| Setting | Active value |
| --- | --- |
| Lyrics website | `https://holyhymns.in` |
| Public API | `https://backend.holyhymns.in/v1` |
| Health endpoint | `https://backend.holyhymns.in/healthz` |
| Legacy API alias | `https://holyhymns-backend.grejo.in/v1` |
| VM | `ubuntu@140.238.160.97`, Ubuntu 22.04, ARM64 |
| Application account | `holyhymns-deploy` (existing account, Docker group) |
| Deployment directory | `/opt/holy-hymns` |
| Private environment | `/opt/holy-hymns/.env`, owner `holyhymns-deploy`, mode `0600` |
| API listener | `127.0.0.1:18080` → container port `8080` |
| Compose project | `holy-hymns` |
| Containers | `holy-hymns-api-1`, `holy-hymns-db-1` |
| Database volume | `holy-hymns_postgres_data` |
| Host Caddy site file | `/etc/caddy/hosts/holyhymns-backend.grejo.in` |
| Website files | `/var/www/holy-hymns/current` → `releases/20261002-93e0ff754838` |
| Backend release | `/opt/holy-hymns/current` → `releases/e62daed2d81663b8320f9a720781faccc164762d-20261002114300-1` |

The database password and 32-byte mail encryption key were generated on the VM and never copied into the repository or chat. `PUBLIC_URL` and `ALLOWED_ORIGIN` use the HTTPS hostname. `COMPOSE_PROFILES` is empty: the existing host Caddy handles HTTPS, and remote backups remain disabled.

Port 8080 already belongs to another application. Keep `API_PORT=18080` on this VM. The API's observed host-proxy peer is `172.30.41.1`, so `TRUSTED_PROXY_CIDRS=172.30.41.1/32`. PostgreSQL has no published port and uses its own internal Docker network.

## HTTPS and verification

The DNS name uses Cloudflare's proxy. [The site configuration](../deploy/holy-hymns.caddy) accepts `CF-Connecting-IP` only from Cloudflare's published address ranges; direct connections use their peer address. Keep those ranges current from the source URLs in the file. This configuration is scoped to Holy Hymns.

Caddy obtained a Let's Encrypt certificate valid through 18 December 2026 and manages renewal. Both public Cloudflare HTTPS and direct-origin HTTPS passed certificate verification and returned `{"status":"ok"}`. The public SSE endpoint immediately returned a content revision; the bounded five-second test intentionally closed the continuing stream. Invalid sign-in and unauthenticated administrative requests returned `401`. Forged forwarding headers did not create rate-limit buckets for the supplied test addresses.

All 35 pre-existing containers retained their IDs, start times and running states. Caddy retained its PID and start time, and all 39 pre-existing Caddy configuration files retained their hashes. Private verification records are under `/opt/holy-hymns/ops`, accessible through `sudo`.

## Operations

Connect as `ubuntu`, then use the application account for Compose operations:

```sh
sudo -iu holyhymns-deploy
app_root=/opt/holy-hymns
release_dir="$(readlink -f "$app_root/current")"
export CADDY_CONFIG_FILE="$release_dir/deploy/Caddyfile"
hh_compose() {
  docker compose --project-name holy-hymns --project-directory "$app_root" \
    --env-file "$app_root/.env" --env-file "$release_dir/images.env" \
    -f "$release_dir/compose.yaml" "$@"
}
hh_compose ps
hh_compose logs --tail 100 api
curl --fail http://127.0.0.1:18080/healthz
```

For a reviewed environment change, recreate only the API with `hh_compose up -d --no-deps --no-build --wait api`. Review database changes separately. Do not run a global Docker prune, restart Docker, or delete project volumes during an application deployment.

For Caddy changes, preserve the import tree, validate the complete `/etc/caddy/Caddyfile`, and reload the service. Do not enable the Compose `edge` profile here: the existing Caddy owns ports 80/443.

## Website and backend domains — 2 October 2026

`holyhymns.in` serves the existing Expo lyrics app as static files. Its production JavaScript calls `https://backend.holyhymns.in/v1`; Caddy forwards that hostname to `127.0.0.1:18080`. Both Cloudflare A records point to `140.238.160.97`, with proxying enabled, TTL Auto and SSL/TLS mode Full (strict). The nameservers are `dean.ns.cloudflare.com` and `yolanda.ns.cloudflare.com`. Caddy manages both origin certificates and HTTP-to-HTTPS redirects. Cloudflare's Always Use HTTPS setting remains off; no `www` record was added.

`PUBLIC_URL=https://holyhymns.in` intentionally remains the email-link origin, and `ALLOWED_ORIGIN=https://holyhymns.in` allows the website to call the backend subdomain. These are not the client's API base URL. Caddy preserves `/auth/*`, `/v1/*` and `/healthz` on the root domain for account landing pages and existing clients. The original `holyhymns-backend.grejo.in` API also remains available. Visiting `/auth/*` on either backend hostname redirects to the website's allowed browser origin. `DOMAIN=holyhymns.in` remains unused by the disabled Compose edge profile.

The web export is at `/var/www/holy-hymns/releases/20261002-93e0ff754838`, with `current` pointing to it. Only exported assets are public; the application source and private VM environment are outside the web root. Hashed JavaScript/fonts use immutable caching, HTML uses `no-cache`, unknown app paths fall back to `index.html`, and missing static assets return 404. The website deployment does not rebuild or recreate backend containers.

Verification passed for the production build and TypeScript check, public HTML/assets, backend HTTPS, website-origin CORS/preflight, SSE, protected admin endpoints and legacy URLs. A real browser loaded 288 hymns, searched the catalogue and opened lyrics; requests used the backend subdomain. A 390px-wide frame also rendered the catalogue and lyrics without horizontal overflow. This was a browser-width check, not a physical phone test. The Holy Hymns API/database containers and Caddy's process were preserved; only the Holy Hymns Caddy site file changed. This deployment performed no container start, stop or recreation operations.

### Publish a new website release

1. In `mobile/`, run `npm run typecheck` and `npm run export:web:production`. The production script fixes the backend URL, skips local dotenv files and clears Metro's transform cache. Confirm the exported JavaScript contains `https://backend.holyhymns.in/v1`.
2. Transfer only `mobile/dist/` into a fresh `/var/www/holy-hymns/releases/<release>` directory on the VM, with directories mode `0755` and files mode `0644`. Confirm the `caddy` user can read `index.html` and its assets. Do not build on the VM or copy environment files into the web root.
3. Retain the previous release and atomically replace the `current` symlink with one pointing to the new release. A static-file release needs no Caddy reload. For route changes, validate the complete `/etc/caddy/Caddyfile` before gracefully reloading it.
4. Verify root HTML, exported assets, catalogue/search/lyrics in a real browser, backend requests with `Origin: https://holyhymns.in`, live events and account landing pages. The backend GitHub Actions workflow currently deploys only the backend; website publication is separate.

To roll back a later web build, atomically point `current` to the retained previous release. The configuration before the website switch is saved at `/opt/holy-hymns/ops/website-activate-20261002T111434Z/holy-hymns.caddy`. Restoring that file, validating the complete Caddyfile and reloading Caddy returns the root to API-only service while preserving the new backend alias. The earlier `/opt/holy-hymns/ops/website-split-20261002T110939Z` backup also includes container state and Caddy file hashes. No database or image rollback is needed for this website change.

## Sentry backend deployment — 2 October 2026

The owner approved a backend-only deployment while mobile remained blocked by the existing Expo tooling dependency audit. The tested ARM64 image `holy-hymns:sentry-e62daed` was transferred over SSH and activated at 11:49 UTC. Its image ID is `sha256:48dd440a9c0dbb3bf3c750aef771b7d5feefc850fa29422e4433f2ed58a3363a`, built from commit `e62daed2d81663b8320f9a720781faccc164762d`. The later merge from main changed no backend source. Sentry identifies this image as `holy-hymns-backend@e62daed`; its private VM configuration uses environment `production`.

The deployment held the normal deployment lock and recreated only `api`, using `--no-deps --no-build --pull never --wait`. Local and public health checks passed, and both API hostnames returned generated `X-Request-ID` headers. The website origin can read that header through CORS. The database and all other containers retained their IDs and start times; the website release stayed unchanged. The CLI probe in the live API container produced Sentry event `f3b52a30bd7243e581f8fa7ea65d3c1b`, confirmed received in environment `verification` with the expected release and Linux runtime. The API performed its normal startup migration check, with no new migrations in this change. No separate migration container or mobile/website release was deployed.

The release directory contains private `sentry-deployment.json`, deployment diagnostics, and before/after container records. `previous` points to `releases/4850dffaee603ca90a8f4427ca10263a0b1c75c6-35614551362-1`. For an API rollback, acquire `/opt/holy-hymns/.deploy.lock`, select that retained directory as `release_dir` in the operations helper above, and run `hh_compose up -d --no-deps --no-build --pull never --wait api`. Verify health before atomically restoring the `current` symlink. Keep database and website state unchanged. This manual Sentry release uses a local image tag; the strict GHCR deployment helper cannot redeploy it. Preserve the local image until a normal digest-based release replaces it.

## Bootstrap and GitHub Actions

The first deployment used local ARM64 image `holy-hymns:vm-20260919`, image ID `sha256:bcc50433836ba38d5f2803b31a7bb59d67630288845b8f6b1797cae4ae541a31`, transferred over SSH. Its release directory was `releases/bcc50433836ba38d5f2803b31a7bb59d6763028884-1789837653-1`. This bootstrap directory uses the image-ID prefix, not a Git commit. Its `bootstrap.json` records provenance.

GitHub Actions still requires GitHub authentication/publication and repository secrets. Use `VM_HOST=140.238.160.97`, `VM_USER=holyhymns-deploy`, and `VM_DEPLOY_PATH=/opt/holy-hymns`; follow [ci-cd.md](ci-cd.md) for the deployment SSH key, verified host key, free-usage limits and activation. Future deployments reuse `.env`, project networks and the database volume, and pull immutable multi-platform GHCR images.

The bootstrap `images.env` contains a local tag. If this release becomes `previous`, the strict GHCR helper cannot redeploy it. For a schema-compatible bootstrap rollback, select its preserved directory as `release_dir` in the operations commands and use the retained local image; do not pass it to `deploy-vm.sh`. Later GitHub releases use the normal digest-based rollback flow.

## Remaining configuration

- Owner account awaits the intended owner's email; no identity was invented.
- The initial catalogue is empty. Upload the [prepared lyrics](../content/lyrics/README.md) as drafts after owner bootstrap.
- SMTP is unset, so registration, recovery and invitations fail closed. Encryption and send limits are configured.
- Google/Apple remain disabled pending their application credentials.
- OCI backups remain disabled pending storage credentials, verified unused free capacity and a restore drill. No storage or paid services were provisioned.
