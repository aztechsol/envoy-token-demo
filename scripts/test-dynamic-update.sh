#!/bin/sh
set -eu

base_url=${ENVOY_URL:-http://localhost:8080}
config_file=${TOKEN_CONFIG:-config/tokens.yaml}
backup_file=$(mktemp)
response_file=$(mktemp)
cp "$config_file" "$backup_file"

restore() {
  echo
  echo "[cleanup] Restoring the original token configuration."
  cp "$backup_file" "$config_file"
  rm -f "$backup_file" "$response_file"
}
trap restore EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

initial_id=$(docker compose ps -q envoy)
if [ -z "$initial_id" ]; then
  echo "Envoy is not running; run 'make up' first" >&2
  exit 1
fi

echo "[1/6] Checking the current mapping through Envoy..."
ready_attempt=0
while [ "$ready_attempt" -lt 30 ]; do
  status=$(curl -sS -o "$response_file" -w '%{http_code}' \
    -H 'Authorization: Bearer 239043-3-423-34-423' "$base_url/test" || true)
  if [ "$status" = 200 ] && grep -Eq '"tenant"[[:space:]]*:[[:space:]]*"spine-prod"' "$response_file"; then
    break
  fi
  ready_attempt=$((ready_attempt + 1))
  sleep 1
done
if [ "$ready_attempt" -ge 30 ]; then
  echo "initial spine-prod token request did not become ready; inspect 'docker compose logs xds envoy'" >&2
  exit 1
fi
echo "      OK: the spine-prod token is mapped; Envoy container is ${initial_id}."

if grep -Eq '^[[:space:]]*dynamic-test-tenant:' "$config_file"; then
  echo "dynamic-test-token already exists in $config_file" >&2
  exit 1
fi

log_mark=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
echo "[2/6] Generating a new token mapping (token value is intentionally redacted)..."
printf '\n  dynamic-test-tenant: dynamic-test-token\n' >>"$config_file"
echo "      Saved config/tokens.yaml with one additional mapping."
echo "[3/6] Pushing new Lua configuration through xDS/ECDS..."
echo "      Waiting for xDS publication, Envoy ACK, and the new mapping to become active."

attempt=0
published=0
acked=0
active=0
while [ "$attempt" -lt 30 ]; do
  xds_logs=$(docker compose logs --since "$log_mark" xds 2>&1 || true)
  if printf '%s\n' "$xds_logs" | grep -q '"msg":"configuration published"'; then
    published=1
  fi
  if printf '%s\n' "$xds_logs" | grep -q '"event":"ACK"'; then
    acked=1
  fi
  status=$(curl -sS -o "$response_file" -w '%{http_code}' \
    -H 'Authorization: Bearer dynamic-test-token' "$base_url/test" || true)
  if [ "$status" = 200 ] && grep -Eq '"tenant"[[:space:]]*:[[:space:]]*"dynamic-test-tenant"' "$response_file"; then
    active=1
  fi
  if [ "$published" = 1 ] && [ "$acked" = 1 ] && [ "$active" = 1 ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

if [ "$published" != 1 ] || [ "$acked" != 1 ] || [ "$active" != 1 ]; then
  echo "timed out waiting for the ECDS update (published=$published acked=$acked active=$active)" >&2
  docker compose logs --since "$log_mark" xds >&2
  exit 1
fi

echo "      xDS change detected and published."
printf '%s\n' "$xds_logs" | grep -E '"msg":"(config change detected|configuration published|xDS response)"' | tail -n 5 || true
echo "[4/6] Envoy ACKed the new tenant-auth ECDS version."
echo "[5/6] New mapping is live: dynamic-test-token maps to dynamic-test-tenant."

final_id=$(docker compose ps -q envoy)
if [ "$initial_id" != "$final_id" ]; then
  echo "Envoy container changed during the update" >&2
  exit 1
fi

echo "[6/6] Zero-downtime check passed: Envoy container ID is unchanged (${final_id})."
echo "Dynamic ECDS update completed successfully."
