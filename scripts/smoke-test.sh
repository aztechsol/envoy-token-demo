#!/bin/sh
set -eu

base_url=${ENVOY_URL:-http://localhost:8080}
response_file=$(mktemp)
trap 'rm -f "$response_file"' EXIT HUP INT TERM

request() {
	label=$1
	expected_status=$2
	shift 2
	actual_status=$(curl -sS -o "$response_file" -w '%{http_code}' "$@")
	if [ "$actual_status" != "$expected_status" ]; then
		echo "$label: expected HTTP $expected_status, got $actual_status" >&2
		sed -n '1,20p' "$response_file" >&2
		exit 1
	fi
	echo "  $label: HTTP $actual_status"
}

echo "[1/3] Known-token mapping: 123456-3290902-2-2323 -> mytest--dev"
request "known token" 200 -H 'Authorization: Bearer 123456-3290902-2-2323' "$base_url/test"
if ! grep -Eq '"tenant"[[:space:]]*:[[:space:]]*"mytest--dev"' "$response_file"; then
	echo "backend did not receive mytest--dev" >&2
	exit 1
fi
if ! grep -Eq '"authorization_present"[[:space:]]*:[[:space:]]*false' "$response_file"; then
	echo "Authorization header was forwarded unexpectedly" >&2
	exit 1
fi
echo "  backend response: $(tr '\n' ' ' <"$response_file")"
echo "  PASS: Envoy set X-Scope-OrgID=mytest--dev and removed Authorization."

echo "[2/3] Unknown-token rejection"
request "unknown token" 403 -H 'Authorization: Bearer bad-token' "$base_url/test"
echo "  PASS: unknown bearer token was rejected before reaching the backend."

echo "[3/3] Missing-authorization rejection"
request "missing authorization" 401 "$base_url/test"
echo "  PASS: request without Authorization was rejected before reaching the backend."

echo "Smoke test passed."
