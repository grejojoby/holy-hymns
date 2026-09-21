#!/usr/bin/env bash
# Run on the Linux VM: bash RELEASE/scripts/deploy-vm.sh APP_ROOT RELEASE_DIR GHCR_USERNAME
# Supply the short-lived GHCR credential on stdin. APP_ROOT/.env never leaves the VM.
# Manual rollback reruns this helper with a previous release. Migrations are not
# reversed: verify schema compatibility or restore a verified backup before rollback.
set +x
set -Eeuo pipefail
umask 077

fail() {
  printf 'Deployment failed: %s\n' "$1" >&2
  if [[ -n ${deploy_tmp:-} && -f "$deploy_tmp/commands.log" ]]; then
    printf 'Private VM diagnostics: %s/deploy-last-error.log\n' "$app_root" >&2
  fi
  exit 1
}
[[ $# -ge 2 && $# -le 3 ]] || fail 'expected APP_ROOT RELEASE_DIR [GHCR_USERNAME]'
for command in docker python3 flock; do
  command -v "$command" >/dev/null 2>&1 || fail "required command unavailable: $command"
done
[[ "$1" == /* && "$2" == /* ]] || fail 'deployment paths must be absolute'
app_root=$(cd -- "$1" && pwd -P) || fail 'APP_ROOT must already exist'
release_dir=$(cd -- "$2" && pwd -P) || fail 'RELEASE_DIR must already exist'
[[ "$app_root" != / && "${release_dir%/*}" == "$app_root/releases" ]] || fail 'release must be directly inside APP_ROOT/releases'
release_name=${release_dir##*/}
[[ "$release_name" =~ ^[0-9a-f]{40}-[1-9][0-9]*-[1-9][0-9]*$ ]] || fail 'invalid release directory name'
ghcr_username=${3:-${GHCR_USERNAME:-}}
[[ "$ghcr_username" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$ ]] || fail 'a valid GHCR_USERNAME is required'

[[ ! -L "$app_root/.deploy.lock" ]] || fail 'deployment lock cannot be a symbolic link'
[[ ! -e "$app_root/.deploy.lock" || -f "$app_root/.deploy.lock" ]] || fail 'deployment lock must be a regular file'
exec 9>>"$app_root/.deploy.lock"
flock -w 300 9 || fail 'another deployment holds the lock'

deploy_tmp=$(mktemp -d "$app_root/.deploy-tmp.XXXXXXXX")
cleanup() {
  local result=$?
  trap - ERR
  set +e
  if [[ $result -ne 0 && -f "$deploy_tmp/commands.log" ]]; then
    # Keep only the final 64 KiB of the latest failed command log, privately on
    # the VM. Registry-login output is excluded from this retained diagnostic.
    python3 - "$deploy_tmp" "$app_root" <<'PY' >/dev/null 2>&1
import os, pathlib, sys
temporary, root = map(pathlib.Path, sys.argv[1:])
with (temporary / "commands.log").open("rb") as source:
    source.seek(0, os.SEEK_END)
    source.seek(max(0, source.tell() - 65536))
    data = source.read(65536)
destination = temporary / "failure.log"
destination.write_bytes(data)
destination.chmod(0o600)
os.replace(destination, root / "deploy-last-error.log")
PY
  fi
  rm -rf -- "$deploy_tmp"
  return "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'fail "unexpected helper error; inspect VM state before retrying"' ERR
export DOCKER_CONFIG="$deploy_tmp/docker"
mkdir "$DOCKER_CONFIG"
export CADDY_CONFIG_FILE="$release_dir/deploy/Caddyfile"
# Host-shell overrides must not replace the release digests or the VM's profiles.
unset HOLY_HYMNS_IMAGE HOLY_HYMNS_BACKUP_IMAGE COMPOSE_PROFILES COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_ENV_FILES
compose=(docker compose --project-name holy-hymns --project-directory "$app_root" --env-file "$app_root/.env" --env-file "$release_dir/images.env" -f "$release_dir/compose.yaml")

# Read data files, never source them. Error output stays private because Compose
# and migration failures may include interpolated database credentials.
if ! python3 - "$app_root" "$release_dir" <<'PY' >"$deploy_tmp/images.json" 2>"$deploy_tmp/commands.log"
import json, os, pathlib, re, stat, sys
root, release = map(pathlib.Path, sys.argv[1:])
for name in ("compose.yaml", "images.env", "deploy/Caddyfile", "scripts/deploy-vm.sh"):
    path = release / name
    if not path.is_file() or path.is_symlink() or release not in path.resolve().parents:
        raise ValueError("release file missing or outside release directory")
env = root / ".env"
if not env.is_file() or env.is_symlink() or stat.S_IMODE(env.stat().st_mode) & 0o077:
    raise ValueError("APP_ROOT/.env must be a regular private file (chmod 600)")
lines = (release / "images.env").read_text().splitlines()
patterns = {
    "HOLY_HYMNS_IMAGE": r"ghcr\.io/grejojoby/holy-hymns@sha256:[0-9a-f]{64}",
    "HOLY_HYMNS_BACKUP_IMAGE": r"ghcr\.io/grejojoby/holy-hymns-backup@sha256:[0-9a-f]{64}",
}
images = {}
for line in lines:
    key, separator, value = line.partition("=")
    if not separator or key not in patterns or key in images or not re.fullmatch(patterns[key], value):
        raise ValueError("images.env must contain exactly two approved immutable digest references")
    images[key] = value
if set(images) != set(patterns):
    raise ValueError("both release image digests are required")
for name in ("current", "previous"):
    path = root / name
    if path.is_symlink():
        target = path.resolve(strict=True)
        # Historical manual bootstraps can use longer image-ID prefixes. New
        # incoming releases still require the exact 40-character Git SHA above.
        if target.parent != root / "releases" or not target.is_dir() or not re.fullmatch(r"[0-9a-f]{40,64}-[1-9][0-9]*-[1-9][0-9]*", target.name):
            raise ValueError("existing release pointer is invalid")
    elif path.exists():
        raise ValueError("release pointers must be symlinks; existing files will not be overwritten")
json.dump(images, sys.stdout)
PY
then fail 'release files, digest references, private .env, or release pointers are invalid'; fi

run_step() {
  local description=$1
  shift
  local log_file="$deploy_tmp/commands.log"
  if [[ "$1" == docker && "${2:-}" == login ]]; then log_file="$deploy_tmp/registry.log"; fi
  printf '%s\n' "$description"
  if ! "$@" >>"$log_file" 2>&1; then
    printf 'Failed step: %s\n' "$description" >>"$deploy_tmp/commands.log"
    fail "$description; inspect the VM configuration and containers; no automatic database rollback was attempted"
  fi
}

run_step 'Checking Docker Compose' docker compose version
run_step 'Checking Docker access' docker info
if ! "${compose[@]}" config --format json >"$deploy_tmp/compose.json" 2>>"$deploy_tmp/commands.log"; then
  fail 'Compose configuration is invalid; check the VM .env and release files'
fi
if ! python3 - "$deploy_tmp/compose.json" "$deploy_tmp/images.json" "$CADDY_CONFIG_FILE" <<'PY' >"$deploy_tmp/services" 2>>"$deploy_tmp/commands.log"
import json, pathlib, sys
config = json.loads(pathlib.Path(sys.argv[1]).read_text())
images = json.loads(pathlib.Path(sys.argv[2]).read_text())
services = config["services"]
required = {"db", "migrate", "api"}
if not required.issubset(services) or not set(services).issubset(required | {"caddy", "backup"}):
    raise ValueError("unexpected active services")
for name, service in services.items():
    if "build" in service:
        raise ValueError("VM deployments must pull prebuilt images")
    if name in ("api", "migrate") and service.get("image") != images["HOLY_HYMNS_IMAGE"]:
        raise ValueError("API/migration image must match immutable release digest")
    if name == "backup" and service.get("image") != images["HOLY_HYMNS_BACKUP_IMAGE"]:
        raise ValueError("backup image must match immutable release digest")
    if name in ("db", "api"):
        health = service.get("healthcheck", {})
        if health.get("disable") or not health.get("test") or health["test"][0] == "NONE":
            raise ValueError("database and API require health checks")
if "caddy" in services:
    if not any(v.get("type") == "bind" and v.get("source") == sys.argv[3] and v.get("target") == "/etc/caddy/Caddyfile" and v.get("read_only") for v in services["caddy"].get("volumes", [])):
        raise ValueError("Caddy must mount this release's read-only configuration")
for name in ("db", "migrate", "api", "caddy", "backup"):
    if name in services:
        print(name)
PY
then fail 'active Compose services do not match the release contract'; fi
active_services=()
optional_services=()
while IFS= read -r service; do
  active_services+=("$service")
  if [[ "$service" == caddy || "$service" == backup ]]; then optional_services+=("$service"); fi
done <"$deploy_tmp/services"
disabled_services=()
for optional in caddy backup; do
  enabled=false
  if [[ ${#optional_services[@]} -gt 0 ]]; then
    for service in "${optional_services[@]}"; do
      if [[ "$service" == "$optional" ]]; then enabled=true; fi
    done
  fi
  if [[ "$enabled" == false ]]; then disabled_services+=("$optional"); fi
done

run_step 'Authenticating to the image registry' docker login ghcr.io --username "$ghcr_username" --password-stdin
# Pull every active service before touching existing containers; Docker selects
# the VM's architecture from each multi-platform image without --platform.
run_step 'Pulling release images' "${compose[@]}" pull "${active_services[@]}"
run_step 'Waiting for PostgreSQL' "${compose[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 180 db
# Always execute a fresh one-off migration; an old completed migrate container
# must not satisfy this release's migration dependency.
run_step 'Applying database migrations' "${compose[@]}" run --rm --no-deps -T --pull never migrate
run_step 'Waiting for the API' "${compose[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 180 api
if [[ ${#optional_services[@]} -gt 0 ]]; then
  run_step 'Starting enabled optional services' "${compose[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 180 "${optional_services[@]}"
fi
if [[ ${#disabled_services[@]} -gt 0 ]]; then
  # Explicit profiles expose these service definitions for stopping only. This
  # reconciles withdrawn opt-in settings without starting services or removing data.
  run_step 'Stopping disabled optional services' "${compose[@]}" --profile edge --profile backup stop --timeout 30 "${disabled_services[@]}"
fi

# Advance pointers only after every enabled service passed Compose's wait check
# (healthy when a healthcheck exists, otherwise running). Never delete volumes,
# prune the Docker host, or attempt an automatic schema/image rollback on failure.
if ! python3 - "$app_root" "$release_dir" "$deploy_tmp" <<'PY' >>"$deploy_tmp/commands.log" 2>&1
import os, pathlib, sys
root, release, temporary = map(pathlib.Path, sys.argv[1:])
current = root / "current"
old = current.resolve(strict=True) if current.is_symlink() else None
if old != release:
    if old:
        replacement = temporary / "previous"
        replacement.symlink_to(os.path.relpath(old, root))
        os.replace(replacement, root / "previous")
    replacement = temporary / "current"
    replacement.symlink_to(os.path.relpath(release, root))
    os.replace(replacement, current)
PY
then fail 'services started, but release pointers could not be recorded; inspect VM state'; fi
printf 'Deployment complete: %s\n' "$release_name"
