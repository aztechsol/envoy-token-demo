#!/bin/sh
set -eu

config_file=${TOKEN_CONFIG:-config/tokens.yaml}
base_url=${ENVOY_URL:-http://localhost:8080}
backup_file=$(mktemp)
stage_file=$(mktemp)
cp "$config_file" "$backup_file"

restore() {
  cp "$backup_file" "$config_file"
  rm -f "$backup_file" "$stage_file"
}
trap restore EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if ! grep -Eq '^[[:space:]]*spine-prod:[[:space:]]*239043-3-423-34-423[[:space:]]*$' "$config_file"; then
	echo "$config_file must contain the spine-prod token for this demo" >&2
  exit 1
fi

echo "Stage 1: the spine-prod token is current"
curl -fsS -H 'Authorization: Bearer 239043-3-423-34-423' "$base_url/test"
echo

echo "Stage 2: adding rotated-token so both tokens overlap"
printf '\n  spine-prod-rotated: rotated-token\n' >>"$config_file"
sleep 2
curl -fsS -H 'Authorization: Bearer 239043-3-423-34-423' "$base_url/test"
echo
curl -fsS -H 'Authorization: Bearer rotated-token' "$base_url/test"
echo

echo "Stage 3: removing the old token; rotated-token remains"
sed '/^[[:space:]]*spine-prod:[[:space:]]*239043-3-423-34-423[[:space:]]*$/d' "$config_file" >"$stage_file"
cp "$stage_file" "$config_file"
sleep 2
old_status=$(curl -sS -o /dev/null -w '%{http_code}' \
  -H 'Authorization: Bearer 239043-3-423-34-423' "$base_url/test")
new_status=$(curl -sS -o /dev/null -w '%{http_code}' \
  -H 'Authorization: Bearer rotated-token' "$base_url/test")

if [ "$old_status" != 403 ] || [ "$new_status" != 200 ]; then
  echo "unexpected result: old=$old_status new=$new_status" >&2
  exit 1
fi

echo "rotation passed (old=403, new=200); original config will now be restored"
