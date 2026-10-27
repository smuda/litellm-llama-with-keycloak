#!/usr/bin/env bash
# e2e checks for the nginx front door. Run from the repo root against a
# running playground stack; make e2e rebuilds and starts it first.
set -euo pipefail

BASE=http://localhost:4000
REALM=http://localhost:8080/realms/playground
LOGIN_PORT=54321
PROD="-f docker-compose.yml -f docker-compose.prod.yml"
# The production override requires them; any value will do for config checks.
export ACCESS_GROUPS=${ACCESS_GROUPS:-/e2e}
export GROUPS_CLAIM=${GROUPS_CLAIM:-key_broker_groups}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# A missing required variable fails every compose call; stop here with
# compose's own error, which names it, instead of a wall of FAILs.
docker compose $PROD config -q || { echo "compose config failed; check .env" >&2; exit 2; }

envval() { grep -E "^$1=" .env | head -n1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'\$//"; }
KEY=$(envval LITELLM_MASTER_KEY || true)
[ -n "$KEY" ] || { echo "LITELLM_MASTER_KEY not found in .env" >&2; exit 2; }
ALICE_PASSWORD=$(envval ALICE_PASSWORD || true)
EVE_PASSWORD=$(envval EVE_PASSWORD || true)
[ -n "$ALICE_PASSWORD" ] && [ -n "$EVE_PASSWORD" ] ||
  { echo "ALICE_PASSWORD or EVE_PASSWORD not found in .env" >&2; exit 2; }

# The key goes to curl via a config on stdin, so it never shows in argv.
auth() { printf 'header = "Authorization: Bearer %s"\n' "$KEY" | curl -K - "$@"; }

fails=0
check() { # name, command...
  local name=$1; shift
  if "$@" >/dev/null 2>&1; then echo "ok  $name"; else echo "FAIL $name"; fails=$((fails + 1)); fi
}

published_ports_only_4000_8080() {
  local ports
  ports=$(docker compose ps --format json | jq -s -r '[.[] | if type == "array" then .[] else . end
    | .Publishers[]? | select(.PublishedPort > 0) | .PublishedPort] | unique | sort | map(tostring) | join(" ")')
  [ "$ports" = "4000 8080" ]
}

wellknown() {
  curl -fsS "$BASE/broker/.well-known/opencode" | jq -e \
    '.auth.command[1] == "http://localhost:4000/broker"
     and .remote_config.url == "http://localhost:4000/broker/remote-config"'
}

models_lists_medium() {
  [ "$(auth -sS -o "$tmp/models" -w '%{http_code}' "$BASE/v1/models")" = 200 ] &&
    jq -e '.data | map(.id) | index("medium")' "$tmp/models"
}

ui_ok() { [ "$(curl -sSL -o /dev/null -w '%{http_code}' "$BASE/ui/")" = 200 ]; }

metrics_no_cors() {
  curl -sS -D "$tmp/metrics.h" -o /dev/null "$BASE/metrics/"
  head -n1 "$tmp/metrics.h" | grep -q ' 200' &&
    ! grep -qi '^access-control-allow-origin:' "$tmp/metrics.h"
}

# Paths that normalize away from /metrics must not reach LiteLLM's /metrics mount.
metrics_no_traversal() {
  local p
  for p in '/metrics/../v1/models' '/metrics/..%2Fx' '/metrics/%2e%2e/x' '/%6detrics/../x'; do
    ! curl -sS --path-as-is "$BASE$p" | grep -q '^# HELP' || return 1
  done
}

broker_redirect_keeps_port() {
  curl -sS -D "$tmp/broker.h" -o /dev/null "$BASE/broker"
  grep -i '^location:' "$tmp/broker.h" | grep -Eiq '^location: (/broker/|http://localhost:4000/broker/)'
}

login_redirect() {
  curl -sS -D "$tmp/login.h" -o /dev/null "$BASE/broker/login?port=1&challenge=x"
  head -n1 "$tmp/login.h" | grep -q ' 302' &&
    grep -i '^location:' "$tmp/login.h" |
      grep -q 'redirect_uri=http%3A%2F%2Flocalhost%3A4000%2Fbroker%2Fcallback'
}

big_body_not_413() {
  { printf '{"model":"medium","messages":[{"role":"user","content":"'
    head -c 2097152 /dev/zero | tr '\0' a
    printf '"}]}'; } >"$tmp/big.json"
  local code
  code=$(auth -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    --data-binary @"$tmp/big.json" "$BASE/v1/chat/completions")
  [ "$code" != 413 ] && [ "$code" != 000 ]
}

rules_served() {
  curl -sSf "$BASE/opencode/rules.md" | cmp -s - key-broker/config/rules.md
}

# A key minted for an internal_user_viewer must not be able to mint more keys.
viewer_key_cannot_mint() {
  local uid=e2e-viewer-$$ vkey="" rc=0 code
  (
    auth -sS -f -o /dev/null -X POST -H 'Content-Type: application/json' \
      -d "{\"user_id\":\"$uid\",\"auto_create_key\":false,\"user_role\":\"internal_user_viewer\"}" "$BASE/user/new" || exit 1
    vkey=$(auth -sS -f -X POST -H 'Content-Type: application/json' \
      -d "{\"user_id\":\"$uid\",\"duration\":\"5m\"}" "$BASE/key/generate" | jq -er .key) || exit 1
    vauth() { printf 'header = "Authorization: Bearer %s"\n' "$vkey" | curl -K - "$@"; }
    code=$(vauth -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
      -d "{\"user_id\":\"$uid\"}" "$BASE/key/generate")
    [ "$code" = 401 ] || [ "$code" = 403 ] || exit 1
    [ "$(vauth -sS -o /dev/null -w '%{http_code}' "$BASE/v1/models")" = 200 ] || exit 1
  ) || rc=1
  # The subshell's vkey is gone; look the key up by user to delete it.
  local keys
  keys=$(auth -sS "$BASE/user/info?user_id=$uid" | jq -c '[.keys[]?.token]' 2>/dev/null || echo '[]')
  auth -sS -o /dev/null -X POST -H 'Content-Type: application/json' -d "{\"keys\":$keys}" "$BASE/key/delete" || true
  auth -sS -o /dev/null -X POST -H 'Content-Type: application/json' -d "{\"user_ids\":[\"$uid\"]}" "$BASE/user/delete" || true
  return $rc
}

# Logs in as user with password through the Keycloak form, as a browser
# would, and prints the broker callback's status and redirect target.
# The callback's body lands in $tmp/<user>.callback.
broker_login() { # user, password, verifier
  local jar=$tmp/$1.jar challenge action loc
  challenge=$(printf %s "$3" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
  action=$(curl -sSfL -c "$jar" -b "$jar" "$BASE/broker/login?port=$LOGIN_PORT&challenge=$challenge" |
    grep -o 'action="[^"]*"' | head -n1 | sed -e 's/^action="//' -e 's/"$//' -e 's/&amp;/\&/g')
  case $action in "$REALM/"*) ;; *) return 1 ;; esac
  # The password reaches curl on stdin, so it never shows in argv.
  loc=$(printf %s "$2" | curl -sS -c "$jar" -b "$jar" -o /dev/null -w '%{redirect_url}' \
    --data-urlencode "username=$1" --data-urlencode password@- "$action")
  case $loc in "$BASE/broker/callback?"*) ;; *) return 1 ;; esac
  curl -sS -o "$tmp/$1.callback" -w '%{http_code} %{redirect_url}' "$loc"
}

alice_gets_key() {
  local verifier out key rc=0
  verifier=$(openssl rand -hex 32)
  out=$(broker_login alice "$ALICE_PASSWORD" "$verifier") || return 1
  case $out in "302 http://127.0.0.1:$LOGIN_PORT/done?code="*) ;; *) return 1 ;; esac
  # Code and verifier together redeem a key, so they stay out of argv too.
  key=$(printf '{"code":"%s","verifier":"%s"}' "${out#*code=}" "$verifier" |
    curl -sSf -X POST -H 'Content-Type: application/json' --data-binary @- "$BASE/broker/redeem" | jq -er .key) || return 1
  [ "$(printf 'header = "Authorization: Bearer %s"\n' "$key" |
    curl -K - -sS -o "$tmp/alice.models" -w '%{http_code}' "$BASE/v1/models")" = 200 ] &&
    jq -e '.data | map(.id) | index("medium")' "$tmp/alice.models" || rc=1
  # stdin carries auth's config, so the key goes in a file instead.
  printf '{"keys":["%s"]}' "$key" >"$tmp/alice.delete"
  auth -sSf -o /dev/null -X POST -H 'Content-Type: application/json' --data-binary @"$tmp/alice.delete" "$BASE/key/delete" || rc=1
  return $rc
}

eve_gets_403() {
  local out
  out=$(broker_login eve "$EVE_PASSWORD" "$(openssl rand -hex 32)") || return 1
  [ "${out%% *}" = 403 ] && grep -q 'not in an access group' "$tmp/eve.callback"
}

nginx_t_playground() { docker compose exec -T nginx nginx -t; }

nginx_t_production() {
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=llm.example.internal \
    -keyout "$tmp/tls.key" -out "$tmp/tls.crt"
  TLS_DIR=$tmp docker compose $PROD run --rm --no-deps nginx nginx -t
}

prod_has_no_keycloak() {
  ! docker compose $PROD config --services | grep -qx keycloak
}

# Dropping any one variable from .env.example must fail compose config
# and name it, rather than defaulting to a blank string.
required_vars_fail_when_unset() {
  local v vars
  vars=$(grep -oE '^[A-Z_]+=' .env.example | tr -d =)
  [ -n "$vars" ] || return 1
  for v in $vars; do
    grep -v "^$v=" .env.example >"$tmp/env.missing"
    ! env -u "$v" docker compose $PROD --env-file "$tmp/env.missing" config -q 2>"$tmp/env.err" &&
      grep -qw "$v" "$tmp/env.err" || return 1
  done
}

# Keycloak takes about a minute to start, and the login checks need it.
deadline=$((SECONDS + 180))
until curl -fs -m 5 -o /dev/null "$REALM"; do
  [ "$SECONDS" -lt "$deadline" ] || { echo "Keycloak playground realm not ready after 180s" >&2; exit 1; }
  sleep 2
done

check "published ports are only 4000 and 8080" published_ports_only_4000_8080
check "broker /.well-known/opencode points at /broker" wellknown
check "/v1/models with master key lists medium" models_lists_medium
check "/ui/ returns 200" ui_ok
check "/metrics/ is 200 without CORS header" metrics_no_cors
check "/metrics traversal paths don't reach metrics" metrics_no_traversal
check "/broker redirect keeps the port" broker_redirect_keeps_port
check "/broker/login redirects with /broker/callback" login_redirect
check "alice logs in and gets a virtual key" alice_gets_key
check "eve gets 403 from the access group gate" eve_gets_403
check "2 MB request body is not 413" big_body_not_413
check "/opencode/rules.md is served" rules_served
check "viewer key cannot mint keys but can list models" viewer_key_cannot_mint
check "nginx -t playground" nginx_t_playground
check "nginx -t production" nginx_t_production
check "production override has no keycloak" prod_has_no_keycloak
check "each required variable fails compose config when unset" required_vars_fail_when_unset

[ "$fails" -eq 0 ] || { echo "$fails check(s) failed" >&2; exit 1; }
