# Oracle VM deployment

Deployed and verified on 19 September 2026.

| Setting | Active value |
| --- | --- |
| Public API | `https://holyhymns-backend.grejo.in/v1` |
| Health endpoint | `https://holyhymns-backend.grejo.in/healthz` |
| VM | `ubuntu@140.238.160.97`, Ubuntu 22.04, ARM64 |
| Application account | `holyhymns-deploy` (existing account, Docker group) |
| Deployment directory | `/opt/holy-hymns` |
| Private environment | `/opt/holy-hymns/.env`, owner `holyhymns-deploy`, mode `0600` |
| API listener | `127.0.0.1:18080` → container port `8080` |
| Compose project | `holy-hymns` |
| Containers | `holy-hymns-api-1`, `holy-hymns-db-1` |
| Database volume | `holy-hymns_postgres_data` |
| Host Caddy site file | `/etc/caddy/hosts/holyhymns-backend.grejo.in` |

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

## Bootstrap and GitHub Actions

The first deployment used local ARM64 image `holy-hymns:vm-20260919`, image ID `sha256:bcc50433836ba38d5f2803b31a7bb59d67630288845b8f6b1797cae4ae541a31`, transferred over SSH. `/opt/holy-hymns/current` points at `releases/bcc50433836ba38d5f2803b31a7bb59d6763028884-1789837653-1`. This bootstrap directory uses the image-ID prefix, not a Git commit. Its `bootstrap.json` records provenance.

GitHub Actions still requires GitHub authentication/publication and repository secrets. Use `VM_HOST=140.238.160.97`, `VM_USER=holyhymns-deploy`, and `VM_DEPLOY_PATH=/opt/holy-hymns`; follow [ci-cd.md](ci-cd.md) for the deployment SSH key, verified host key, free-usage limits and activation. Future deployments reuse `.env`, project networks and the database volume, and pull immutable multi-platform GHCR images.

The bootstrap `images.env` contains a local tag. If this release becomes `previous`, the strict GHCR helper cannot redeploy it. For a schema-compatible bootstrap rollback, select its preserved directory as `release_dir` in the operations commands and use the retained local image; do not pass it to `deploy-vm.sh`. Later GitHub releases use the normal digest-based rollback flow.

## Remaining configuration

- Owner account awaits the intended owner's email; no identity was invented.
- The initial catalogue is empty. Upload the [prepared lyrics](../content/lyrics/README.md) as drafts after owner bootstrap.
- SMTP is unset, so registration, recovery and invitations fail closed. Encryption and send limits are configured.
- Google/Apple remain disabled pending their application credentials.
- OCI backups remain disabled pending storage credentials, verified unused free capacity and a restore drill. No storage or paid services were provisioned.
