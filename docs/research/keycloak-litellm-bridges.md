# Keycloak to LiteLLM bridge options

Research for issue #4: what can sit between opencode and LiteLLM (or
beside LiteLLM) to validate a Keycloak token and map it to a per-user
LiteLLM identity or virtual key. Deciding criterion: least friction
for the end user. Researched 2026-10-24.

## Versions checked

| Component            | Version        | Source                       |
| -------------------- | -------------- | ---------------------------- |
| LiteLLM              | v1.106.2       | GitHub release, source       |
| oauth2-proxy         | v7.15.5        | GitHub release, 7.15.x docs  |
| Keycloak             | 26.8.1         | GitHub release, docs         |
| Envoy                | v1.40.1        | GitHub release, latest docs  |
| Traefik              | v3.7.16        | GitHub release, v3.7 docs    |
| Caddy                | v2.11.7        | GitHub release, docs         |
| lua-resty-openidc    | 1.9.2          | GitHub README / release      |

## The two shapes

Every option lands in one of two shapes. Which one wins depends
mostly on the opencode client-auth ticket, not on this one.

1. Per-request token. opencode sends a Keycloak access token on every
   call; something validates it and turns it into a LiteLLM identity
   or key on the fly. Seamless only if opencode can obtain and refresh
   a Keycloak token itself. Keycloak access tokens are short-lived by
   default, so a static token pasted into config goes stale.
2. One-time key issuance. The user logs in with Keycloak once, gets a
   long-lived per-user LiteLLM virtual key, and pastes it into
   opencode config (or an env var). opencode then talks to LiteLLM
   with a plain API key, which it already supports. Friction is one
   login plus one paste, then nothing.

## LiteLLM facts that constrain the bridges

- LiteLLM has a built-in header-trust mode for "oauth2-proxy,
  Authelia" style upstreams: `general_settings.enable_oauth2_proxy_auth`
  plus `oauth2_config_mappings` (header name per field). In v1.106.2
  source this branch has no `premium_user` check, unlike JWT auth and
  `enable_oauth2_auth`, which both raise "enterprise only" errors.
  Mappable fields are restricted to an identity allowlist: `user_id`,
  `user_email`, `team_id`, `team_alias`, `org_id`, `models`. It fails
  closed unless `general_settings.trusted_proxy_ranges` contains the
  direct peer IP. It is not listed on the docs site; found only in
  source.
  Sources: `litellm/proxy/auth/oauth2_proxy_hook.py`,
  `litellm/proxy/auth/trusted_proxy_utils.py`,
  `litellm/proxy/auth/user_api_key_auth.py` (tag v1.106.2).
- In that source, the header-trust branch runs before the master key
  check, so when enabled it appears to apply to every request,
  including admin calls. Not tested; a playground may need a separate
  LiteLLM route or instance for admin/UI. Unverified.
- Custom auth (`general_settings.custom_auth: module.fn` returning
  `UserAPIKeyAuth` with `user_id`/`team_id`) is not labelled
  enterprise. Running it together with virtual keys
  (`custom_auth_settings.mode: auto`) is enterprise.
  Source: https://docs.litellm.ai/docs/proxy/custom_auth
- JWT auth, including the beta "JWT to virtual key mapping"
  (`virtual_key_claim_field`, `/jwt/key/mapping/*`), requires an
  Enterprise license.
  Source: https://docs.litellm.ai/docs/proxy/token_auth
- OAuth2 token introspection (`enable_oauth2_auth`) is enterprise.
  Source: https://docs.litellm.ai/docs/proxy/oauth2
- Key management is open: `/user/new`, `/key/generate` (with
  `user_id`, `team_id`, `models`, `duration`, `metadata`, budgets),
  `/key/info`, `/user/info`, `/team/new`. Only key rotation is marked
  enterprise on that page. Keys are stored hashed (`hash_token` in
  auth source), so a bridge cannot read a user's existing raw key
  back; it must store it at creation or mint a new one.
  Source: https://docs.litellm.ai/docs/proxy/virtual_keys
- Admin UI SSO with a generic OIDC provider (Keycloak) is free "for
  up to 5 users" from v1.76.0; beyond that it needs a license.
  `internal_user` role can create, view and delete their own keys.
  SSO users can be auto-added to existing teams via
  `team_ids_jwt_field` (teams are not auto-created). Redirect URI is
  `<PROXY_BASE_URL>/sso/callback`.
  Source: https://docs.litellm.ai/docs/proxy/admin_ui_sso

## Options

### A. LiteLLM admin UI with Keycloak SSO (beside LiteLLM, no code)

- Mapping: Keycloak `sub` (via `GENERIC_USER_ID_ATTRIBUTE`) becomes
  the LiteLLM user; the user creates their own virtual key in the UI.
- End user: log in to the LiteLLM UI, click "create key", paste into
  opencode. The UI exposes more than "get key", which may confuse.
- Effort: config only (env vars + a Keycloak client).
- Groups: yes, `team_ids_jwt_field` maps a claim to existing teams.
- Catch: hard ceiling of 5 SSO users without a license. Fine for a
  playground, a dead end beyond it.

### B. Self-service "log in, get your key" page (beside LiteLLM)

- Mapping: a tiny web app does the OIDC login (or sits behind
  oauth2-proxy and reads `X-Forwarded-User`/`-Email`/`-Groups`), then
  with the master key calls `/user/new` (idempotent on `user_id` =
  `sub`) and `/key/generate` with `user_id`, `key_alias`, optional
  `team_id`. Shows the key once, or regenerates on each visit.
- End user: one browser login, copy one key, paste into opencode.
  Could print a ready-to-paste `opencode.json` snippet or export line.
- Effort: about 50-100 lines plus a Keycloak client. Putting it
  behind oauth2-proxy removes the OIDC code entirely; the page then
  only reads headers and calls two LiteLLM endpoints.
- Groups: yes, read `groups` claim, call `/team/new` and
  `/team/member_add`, or pass `team_id` on key creation.
- Catch: the key is long-lived and decoupled from Keycloak; disabling
  a user in Keycloak does not revoke it unless the page or a cron
  job does so.

### C. oauth2-proxy in front of LiteLLM + LiteLLM header trust

- Mapping: oauth2-proxy with `provider = keycloak-oidc`,
  `skip_jwt_bearer_tokens = true` validates the bearer token from
  opencode (signature, issuer, `aud` = client ID or
  `oidc_extra_audience`) and turns it into a session; with
  `pass_user_headers` (default on) it sends `X-Forwarded-User`,
  `-Email`, `-Groups`, `-Preferred-Username` upstream. LiteLLM's
  `enable_oauth2_proxy_auth` maps those headers to `user_id` /
  `user_email` / `team_id`. No virtual key exists per user; LiteLLM
  sees an identity, not a key.
- End user: zero LiteLLM keys, but opencode must send a fresh
  Keycloak access token on every request (depends on issue for
  opencode client auth). Browser cookie sessions do not help a CLI.
- Effort: config only. Keycloak client needs an audience mapper so
  `aud` contains the client ID (oauth2-proxy keycloak-oidc docs).
- Groups: `X-Forwarded-Groups` exists, but LiteLLM maps one header to
  one `team_id` string; Keycloak groups are paths like `/team-a` and
  multi-valued. Would need a single-group claim or a mapper.
- Catch: header-trust path is undocumented on the docs site and
  appears to bypass per-key budgets and the master key path. Also
  `allowed_groups` / `allowed_role` can gate access at the proxy.
- Sources:
  https://oauth2-proxy.github.io/oauth2-proxy/configuration/overview,
  https://oauth2-proxy.github.io/oauth2-proxy/configuration/providers/keycloak_oidc,
  `pkg/middleware/jwt_session.go` (v7.15.5).

### D. Custom shim (token in, key out) in front of LiteLLM

- Mapping: small HTTP proxy validates the Keycloak JWT against the
  realm JWKS, looks up `sub` in its own table (SQLite, or a table in
  the existing Postgres), on miss calls `/user/new` +
  `/key/generate` and stores the raw key, then forwards the request
  to LiteLLM with `Authorization: Bearer <that key>`. LiteLLM sees a
  normal per-user virtual key, so budgets, spend and limits all
  work in the community edition.
- End user: same as C; opencode must send a Keycloak token. No
  LiteLLM key ever shown to the user.
- Effort: about 100-200 lines; must stream responses (SSE) through.
  Can be split: oauth2-proxy (or Envoy/Caddy/Traefik) validates the
  token, and the shim is only a forward-auth endpoint that returns
  the key in a header. Caddy `forward_auth` supports
  `copy_headers X-Key>Authorization`; Traefik `forwardAuth`
  `authResponseHeaders` replaces conflicting headers.
- Groups: yes, read `groups` claim, set `team_id` at key creation or
  add the user to teams.
- Catch: the shim holds raw keys; protect that table like the master
  key.

### E. JWT-validating reverse proxies (Envoy, OpenResty, Traefik, Caddy)

- Envoy: `jwt_authn` filter with `remote_jwks` validates the token;
  `claim_to_headers` copies claims to headers. Mapping to a LiteLLM
  key still needs ext_authz or a Lua filter calling a shim (D), or
  header trust (C). Source:
  https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/jwt_authn_filter
- nginx/OpenResty: `lua-resty-openidc` `bearer_jwt_verify` validates
  against discovery/JWKS; Lua in `access_by_lua` could do the
  `sub` -> key lookup and set `Authorization`, i.e. D written in Lua.
  Source: https://github.com/zmartzone/lua-resty-openidc
- Traefik Proxy OSS: no built-in JWT/OIDC middleware; the reference
  marks JWT and OIDC middlewares as Traefik Hub. Use `forwardAuth` to
  oauth2-proxy or a shim. Source:
  https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/forwardauth/
- Caddy: `forward_auth` to oauth2-proxy or a shim, with header
  renaming. Source:
  https://caddyserver.com/docs/caddyfile/directives/forward_auth
- Verdict: none of these maps a token to a LiteLLM key on its own.
  They only replace the "validate token" half of C or D. Not worth
  a new component when oauth2-proxy already does that half.

### F. Keycloak token exchange

- Standard token exchange V2 is enabled by default in current
  Keycloak, but it only exchanges Keycloak tokens for other Keycloak
  tokens in the same realm (audience downscoping, extra scopes).
  Internal-to-external is not supported; it cannot mint a LiteLLM
  virtual key. Useful at most to get a token with the right `aud`
  for C or D.
  Source: https://www.keycloak.org/securing-apps/token-exchange

## Summary

| Option                  | User experience          | Effort    | Groups      |
| ----------------------- | ------------------------ | --------- | ----------- |
| A. LiteLLM UI SSO       | login once, paste key    | config    | yes         |
| B. key page             | login once, paste key    | small app | yes         |
| C. oauth2-proxy + trust | token per request        | config    | limited     |
| D. token-to-key shim    | token per request        | small app | yes         |
| E. JWT reverse proxies  | as C or D                | config+   | as C or D   |
| F. token exchange       | not a bridge             | n/a       | n/a         |

Shortlist:

- If opencode cannot fetch and refresh Keycloak tokens by itself, B
  (or A while under 5 users) is the least-friction path: one login,
  one paste, then opencode works with a plain API key.
- If opencode can, D gives the smoothest experience with full
  per-user key semantics; C is the zero-code variant with weaker
  accounting and an undocumented LiteLLM path.
- B and D share the same core (sub -> `/user/new` + `/key/generate`),
  so B can grow into D later.

## Open questions

- Does opencode support an OAuth/OIDC flow or a token helper command
  for a custom OpenAI-compatible provider? (sibling ticket)
- Does `enable_oauth2_proxy_auth` record spend per `user_id` and
  coexist with the admin UI and master key? Needs a quick test.
