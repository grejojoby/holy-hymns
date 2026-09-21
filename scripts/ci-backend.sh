#!/usr/bin/env bash
# Uses a fresh local database; never connects to a caller's DATABASE_URL.
set +x
set -Eeuo pipefail

ci_project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
ci_go_binary=${GO_BINARY:-/Users/grejo.j/.gvm/gos/go1.25.5/bin/go}
ci_container_name="holy-hymns-ci-$(openssl rand -hex 8)"
ci_database_password=$(openssl rand -hex 32)

cleanup() {
  docker rm --force --volumes "$ci_container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ ${GITHUB_ACTIONS:-} == true ]]; then
  printf '::add-mask::%s\n' "$ci_database_password"
fi

"$ci_go_binary" version
docker info >/dev/null
POSTGRES_PASSWORD="$ci_database_password" docker run --detach \
  --name "$ci_container_name" \
  --publish 127.0.0.1::5432 \
  --env POSTGRES_DB=holyhymns_ci \
  --env POSTGRES_USER=holyhymns_ci \
  --env POSTGRES_PASSWORD \
  --tmpfs /var/lib/postgresql/data:rw,nosuid,size=512m \
  --memory 512m \
  --shm-size 64m \
  postgres:17-alpine >/dev/null

ci_database_ready=false
for ((ci_attempt = 0; ci_attempt < 60; ci_attempt++)); do
  # The temporary initialization server does not listen on TCP.
  if docker exec "$ci_container_name" pg_isready -h 127.0.0.1 -U holyhymns_ci -d holyhymns_ci >/dev/null 2>&1; then
    ci_database_ready=true
    break
  fi
  sleep 1
done
if [[ $ci_database_ready != true ]]; then
  printf 'PostgreSQL did not become ready within 60 seconds.\n' >&2
  docker logs --tail 50 "$ci_container_name" >&2
  exit 1
fi

ci_port_mapping=$(docker port "$ci_container_name" 5432/tcp)
if [[ ! $ci_port_mapping =~ ^127\.0\.0\.1:([0-9]+)$ ]]; then
  printf 'Expected a loopback-only PostgreSQL port mapping.\n' >&2
  exit 1
fi
export TEST_DATABASE_URL="postgres://holyhymns_ci:${ci_database_password}@127.0.0.1:${BASH_REMATCH[1]}/holyhymns_ci?sslmode=disable"
if [[ ${GITHUB_ACTIONS:-} == true ]]; then
  printf '::add-mask::%s\n' "$TEST_DATABASE_URL"
fi

cd "$ci_project_root/backend"
"$ci_go_binary" vet ./...
"$ci_go_binary" test -race -count=1 ./...
"$ci_go_binary" build ./cmd/holyhymns
