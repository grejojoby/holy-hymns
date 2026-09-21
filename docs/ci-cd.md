# GitHub CI/CD to the Oracle VM

The repository contains the pipeline; automated activation still requires GitHub access and repository secrets. The Oracle VM has been prepared and manually deployed successfully; see [the live deployment record](oracle-vm.md). Registry publication and the GitHub Actions deployment itself remain unverified. The GitHub CLI is not authenticated on the workstation.

## What runs

Pull requests run `.github/workflows/ci.yml`. A push to `main`, or a manual run of `.github/workflows/release.yml` on `main`, runs the reusable CI checks before publishing both images:

| Image | Platforms |
| --- | --- |
| `ghcr.io/grejojoby/holy-hymns` | `linux/amd64`, `linux/arm64` |
| `ghcr.io/grejojoby/holy-hymns-backup` | `linux/amd64`, `linux/arm64` |

The release identifies images with the Git commit SHA and deploys their exact registry digests. Docker selects the matching architecture from the image manifest: Ampere/AArch64 VMs use `arm64`; x86-64 VMs use `amd64`. Digest references prevent a later tag change from changing the release being pulled. [Docker pull by digest](https://docs.docker.com/reference/cli/docker/image/pull/#pull-an-image-by-digest-immutable-identifier)

CI checks the Go backend against disposable PostgreSQL, typechecks and tests the mobile app, exports its web/native JavaScript bundles, audits production npm dependencies, and validates scripts, the lyrics bundle and Compose configuration. These checks do not replace real-device or native store-build testing.

The final job connects over SSH, pulls the images and applies Docker Compose. Image builds run on GitHub's runners, leaving the small VM for the application and database. Deployment runs only when repository variable `DEPLOY_ENABLED` equals `true`; leaving it unset still permits CI and image publication.

This workflow deploys the backend. iOS/Android store builds and submissions remain separate, using the local native build instructions in [mobile/README.md](../mobile/README.md).

## Keep the pipeline within free usage

As checked on 19 September 2026, standard GitHub-hosted runners are free for public repositories. Private repositories consume the account's included allowance; GitHub Free currently includes 2,000 minutes per month, 500 MB of artifact storage and 10 GB of cache per repository. Larger runners always incur charges. These workflows use standard Linux runners. [GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions)

GHCR container image storage and bandwidth are currently free, including private container images. This is GHCR's current policy, not an unlimited allowance for every GitHub Packages registry. GitHub states it will announce a change at least one month ahead. [GitHub Packages billing](https://docs.github.com/en/billing/concepts/product-billing/github-packages)

Before enabling runs, open the repository owner's **Settings → Billing and licensing → Budgets and alerts**. Set Actions paid usage to **$0** and enable **Stop usage when budget limit is reached**; do the same for any applicable metered storage/package budget. Enable included-usage alerts. A notification-only budget does not block spending. Exhausted free usage must stop runs until the allowance resets; do not enable a paid runner, raise the budget or upgrade a plan automatically. [GitHub budget setup](https://docs.github.com/en/billing/how-tos/set-up-budgets)

The workflow does not change repository or image visibility. GHCR initially creates a package as private; public images are anonymously readable. Keep the package associated with this repository so the workflow's token has access. If a package with the same name existed before this workflow, check its **Package settings → Manage Actions access**. [GHCR permissions and visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)

## Prepare the VM once

1. Connect using existing trusted VM access and inspect `cat /etc/os-release` and `uname -m`. Reuse an existing compatible Docker installation and reverse proxy.
2. Install Docker Engine and the Compose plugin using the instructions for the VM's actual OS. For Ubuntu on Oracle Cloud, follow [Docker's Ubuntu apt-repository instructions](https://docs.docker.com/engine/install/ubuntu/), which support both `amd64` and `arm64`; install `docker-ce`, `docker-ce-cli`, `containerd.io`, `docker-buildx-plugin` and `docker-compose-plugin`. Do not run Ubuntu apt commands on Oracle Linux. See the [Compose plugin installation](https://docs.docker.com/compose/install/linux/) when Engine is already installed.
3. Make sure the deployment account can run `docker info` and `docker compose version` without an interactive password. A dedicated example account is `holyhymns-deploy`. If using the Docker group, treat this account and its SSH key as root-equivalent; Docker socket access grants host-level privileges. [Docker post-installation permissions](https://docs.docker.com/engine/install/linux-postinstall/)
4. Create the deployment directory, owned by that account. It must have enough free disk for the database, the current images and the previous release. Install Bash, Python 3.9 or newer, `flock` (usually from `util-linux`), OpenSSH and `tar` as well. No Go, Node.js or application build toolchain is needed on the VM.

For a **new** deployment account, run these commands through your existing administrator session. Skip creation if the chosen account already exists:

```sh
sudo useradd --create-home --shell /bin/bash holyhymns-deploy
sudo usermod -aG docker holyhymns-deploy
sudo install -d -m 0750 -o holyhymns-deploy -g holyhymns-deploy /opt/holy-hymns
sudo install -d -m 0700 -o holyhymns-deploy -g holyhymns-deploy /home/holyhymns-deploy/.ssh
```

Use a fresh SSH login after changing group membership. Keep SSH key authentication enabled and retain your independent administrator/recovery access.

Create `/opt/holy-hymns/.env` from the repository's [.env.example](../.env.example), with owner `holyhymns-deploy` and mode `0600`. Edit it through trusted VM access. Replace the development password and configure `PUBLIC_URL`, mail credentials, `MAIL_ENCRYPTION_KEY`, provider credentials and proxy settings using [deployment.md](deployment.md). The pipeline must not replace this file, upload its contents to GitHub or put application secrets into an image.

Keep `API_BIND=127.0.0.1`; PostgreSQL has no public port. For an existing HTTPS proxy, keep its configuration and route to the selected `API_PORT` (default `8080`, this VM `18080`) with SSE streaming preserved. To use the bundled Caddy service, set `COMPOSE_PROFILES=edge`, `DOMAIN` and `ACME_EMAIL` in `.env`, with DNS and ports 80/443 ready. Add `backup` to `COMPOSE_PROFILES` only after completing the free-capacity and restore checks in [deployment.md](deployment.md). Example: `COMPOSE_PROFILES=edge,backup`. The next successful deployment stops this project's optional services whose profiles were removed; changing `.env` alone does not stop an existing container immediately.

Backup credentials belong in a persistent VM-only file, for example `/opt/holy-hymns/secrets/rclone.conf`; set `RCLONE_CONFIG_FILE` to that absolute path. Give the backup container's UID 10001 the necessary read access without making it world-readable. Keep the age private decryption key off the VM.

The configured SSH port must be reachable from the runner, and the VM needs outbound HTTPS for registry pulls. Standard GitHub-hosted runner addresses change; an OCI security rule limited to your workstation will not admit them. Reconcile this with your existing SSH network policy before activation. The API host port and PostgreSQL must remain closed publicly. [GitHub runner network addresses](https://docs.github.com/en/actions/reference/runners/github-hosted-runners#ip-addresses)

## Configure deployment authentication

Create a dedicated Ed25519 deployment key on a trusted workstation, outside this repository. The workflow requires a noninteractive key, so this example creates a key without a passphrase:

```sh
ssh-keygen -t ed25519 -N '' -C holy-hymns-actions -f /secure/path/holy-hymns-actions
```

Add the **public** key to the deployment account's `authorized_keys`. Prefix its line with `restrict` to disable forwarding, PTY allocation and user startup commands while permitting noninteractive deployment commands. The result is one line like `restrict ssh-ed25519 PUBLIC_KEY_DATA holy-hymns-actions`. Set `authorized_keys` to mode `0600` and the account's ownership. Never copy the private key to the VM. [OpenSSH authorized-key restrictions](https://man.openbsd.org/sshd.8#AUTHORIZED_KEYS_FILE_FORMAT)

Obtain the server's host public key through the trusted administrator session or OCI serial console:

```sh
sudo cat /etc/ssh/ssh_host_ed25519_key.pub
```

Construct a `known_hosts` entry using exactly the hostname/IP in `VM_HOST` followed by the key type and base64 key from that command. Examples of the required shape:

```text
api-vm.example.com ssh-ed25519 SERVER_HOST_PUBLIC_KEY
[api-vm.example.com]:2222 ssh-ed25519 SERVER_HOST_PUBLIC_KEY
```

Use the first form for port 22 and the bracketed form for a custom port. `VM_KNOWN_HOSTS` contains the complete line, not just a fingerprint. Do not trust an unverified `ssh-keyscan` result or disable host-key checking. A host-key change should stop deployment until you verify the replacement through trusted access.

The automated registry login uses the deployment job's temporary, read-only `GITHUB_TOKEN`, transmitted over SSH to `docker login --password-stdin` with a temporary Docker configuration. No GHCR personal access token is needed, and no permanent registry credential is installed on the VM. The temporary token expires with the job; do not save it for future manual pulls. [GitHub token lifetime](https://docs.github.com/en/actions/concepts/security/github_token), [Docker noninteractive login](https://docs.docker.com/reference/cli/docker/login/)

## Activate in GitHub

Use `grejojoby/holy-hymns` repository settings. Authenticate your workstation first if using the CLI: `gh auth login`; `gh auth status` must then confirm the intended account. The browser settings work equally well and avoid putting secrets into command arguments.

This workflow uses repository-level Actions secrets and variables. It does not require a GitHub deployment environment, keeping private repositories on GitHub Free supported without a plan upgrade. Environment secrets are unavailable to private repositories on that plan. [Environment availability](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)

Under **Settings → Secrets and variables → Actions → Secrets**, add:

| Secret | Value |
| --- | --- |
| `VM_HOST` | VM DNS hostname or IPv4 address, without a scheme or port; IPv6 is not supported by this helper |
| `VM_USER` | Deployment account, for example `holyhymns-deploy` |
| `VM_SSH_KEY` | Entire private deployment key, including BEGIN/END lines |
| `VM_KNOWN_HOSTS` | Independently verified server host-key entry described above |

Under **Settings → Secrets and variables → Actions → Variables**, add the optional repository variables:

| Variable | Default / purpose |
| --- | --- |
| `VM_DEPLOY_PATH` | `/opt/holy-hymns`; absolute path owned by the deployment account; path segments may contain letters, numbers, `_` and `-` only |
| `VM_SSH_PORT` | `22`; must match the host-key entry and firewall |

Set **repository variable** `DEPLOY_ENABLED=true` only after the VM, secrets, budget controls and initial backup/recovery setup are ready. To pause automatic deployments, set it to `false` or delete it. This gate does not disable CI or image publishing.

For the first release, run **Actions → Build and deploy → Run workflow**, selecting `main`, and inspect CI, image publication and deployment results. Subsequent pushes to `main` follow the same pipeline. A successful image build does not prove the API was deployed; inspect the deployment job and confirm the running service before considering activation complete.

## Release history and rollback

The deployment root keeps a persistent `.env` and separate `releases/<commit>-<run>-<attempt>/` directories. Each uploaded bundle contains only `compose.yaml`, `deploy/Caddyfile`, `scripts/deploy-vm.sh` and generated `images.env` references. `current` points to the last successful release; `previous` points to the release it replaced. Pointers advance only after the deployment checks succeed; retrying the same current release preserves `previous`. Each release retains its Compose file, scripts and pinned image references. Keep the referenced images available in GHCR and on the VM until the next release is verified. Never place credentials inside a release directory or replace the deployment root with a fresh checkout.

The VM helper's interface is:

```sh
bash RELEASE_DIR/scripts/deploy-vm.sh APP_ROOT RELEASE_DIR GHCR_USERNAME
```

Supply the temporary registry token on standard input. The automated workflow invokes this helper after transferring the release. It uses `/opt/holy-hymns` as `APP_ROOT` unless `VM_DEPLOY_PATH` changes it. It locks deployments, validates the private regular `.env` file and image references, pulls every active service, waits for PostgreSQL, runs a fresh migration container, then starts the API and enabled optional services. PostgreSQL/API must pass their health checks; optional services without health checks must be running. HTTPS reachability and backup correctness still need the checks below. Temporary registry credentials and configuration files are deleted on exit. On failure, the last 64 KiB of diagnostic output is retained only on the VM as `deploy-last-error.log`, mode `0600`; registry-login output is excluded. Treat this log as sensitive and inspect it through trusted VM access. Workflow output identifies the failed phase without dumping application secrets.

An application rollback does **not** reverse PostgreSQL migrations. Review the migrations between the two releases before proceeding. If the previous binary is incompatible with the current schema, stop writes and use the verified database restore procedure in [deployment.md](deployment.md); do not run an old image against an incompatible database. Deployments are not a substitute for tested backups. A failure can occur after a migration or container replacement: unchanged release pointers do not mean the database or running containers reverted automatically.

For a compatible rollback, inspect `current` and `previous` through trusted VM access, then invoke the preserved helper from the previous release. For private images, obtain a fresh temporary credential that can read the two packages; the old Actions job token is expired. A classic PAT with only `read:packages` and access to those packages can serve this manual operation and should be revoked afterward. Routine automated releases require no PAT. [GHCR read permissions](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#authenticating-with-a-personal-access-token-classic)

Run the following in **Bash** as the deployment account, after the compatibility review. Substitute the configured deployment path if it differs, and the token owner for `grejojoby` if using a different authorized account:

```sh
cd /opt/holy-hymns
readlink current
readlink previous
rollback_release="$(readlink -f previous)"
read -r -s -p 'Temporary GHCR read token: ' rollback_token
printf '\n'
printf '%s\n' "$rollback_token" | bash "$rollback_release/scripts/deploy-vm.sh" /opt/holy-hymns "$rollback_release" grejojoby
unset rollback_token
```

Verify container health and the API after the command returns. Keep the failed release available for diagnosis. The helper deliberately does not restore a database or delete persistent volumes.

## Verify and troubleshoot

After a deployment, check container health, API reachability through HTTPS, sign-in and an authenticated publishing operation. Reconnect a reader and verify live updates. Application settings, users, favorites and lyrics remain in the PostgreSQL volume; a backend release does not import or publish the Blogger collection.

For normal operations, run this in Bash on the VM to select the current release and persistent environment:

```sh
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
```

Use `hh_compose` in place of bare `docker compose` for the account/bootstrap operations in the main README. Keep these operations on the VM; the short-lived registry login has already been removed, so a new manual pull requires authentication. After a failed deployment, inspect both container state and the failed release; `current` still names the last successful release.

Common failures:

- **Deploy skipped:** `DEPLOY_ENABLED` is unset or differs from the exact string `true`.
- **Secrets unavailable:** put the four SSH secrets at repository level. This workflow does not read environment-scoped secrets.
- **SSH timeout:** check hostname, port, OCI security rules and the host firewall against the runner's network path.
- **Host-key verification failed:** verify the server identity through trusted access and update `VM_KNOWN_HOSTS`; do not bypass verification.
- **Permission denied / Docker socket unavailable:** check the deployment public key, account membership, directory ownership and a fresh SSH login.
- **GHCR denied:** ensure both image packages are linked to the repository and grant its Actions access. Do not solve this by making the images public automatically.
- **Free allowance exhausted:** wait for the allowance reset or reduce optional work; leave paid usage blocked.
- **Migration or health check failed:** inspect the failed deployment and preserve the database. Never run `docker compose down -v` in production.
