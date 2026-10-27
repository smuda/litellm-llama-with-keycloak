# LiteLLM v1.106.2 admin UI SSO with Keycloak (community)

Research for issue #39. Question: how the LiteLLM v1.106.2 admin UI
behaves with generic OIDC SSO against Keycloak, without a license.
Goal behind it: Keycloak group `llm-admins` gets `proxy_admin` in the
UI, everyone else is refused, `UI_USERNAME`/`UI_PASSWORD` stays as
break-glass.

## Version looked at

- LiteLLM tag `v1.106.2`, commit
  `49943cd52ea46aacf8db2e5cb2b890d580fe9bb4`, `pyproject.toml`
  version `1.106.2` (the image this repo runs).
- Docs at https://docs.litellm.ai/docs/proxy/admin_ui_sso on
  2026-10-27. The docs track `main`, not the tag; code wins where they
  differ.
- Builds on `docs/research/litellm-community-auth.md` on branch
  `research/litellm-community-auth` (main @ e159005, v1.108.0). The
  5-user cap behaves differently in v1.106.2; see below.

`SRC` is shorthand for this URL:
https://github.com/BerriAI/litellm/blob/49943cd52ea46aacf8db2e5cb2b890d580fe9bb4
`UI_SSO` is `SRC/litellm/proxy/management_endpoints/ui_sso.py`.

Not verified by running it: everything here is from reading code. The
worktree had no `.env`, so live playground state was not inspected.

## Summary

| Question                         | Answer in v1.106.2                               |
| -------------------------------- | ------------------------------------------------ |
| Generic OIDC UI SSO in community | Yes, until the user table has more than 5 rows   |
| `user_id` = `preferred_username` | Yes, it is the default                           |
| Group -> `proxy_admin`           | Yes, `GENERIC_ROLE_MAPPINGS_*`, no license check |
| Refuse non-members at UI login   | Yes, `ui_access_mode`, no license check          |
| `UI_USERNAME`/`UI_PASSWORD`      | Still works next to SSO unless disabled          |
| 5-user cap                       | Login-time gate on total users; blocks everyone  |
| Session lifetime                 | 24h JWT + 24h key, not tied to Keycloak session  |
| Paths                            | `/sso/key/generate`, `/sso/callback`, `/ui/`     |

The blocker is the cap: with the key broker creating one LiteLLM user
per Keycloak user, a deployment with more than 5 users cannot use UI
SSO at all without `LITELLM_LICENSE`.

## License gates (`premium_user`) on this path

`premium_user` is `True` only with a valid `LITELLM_LICENSE` (see the
earlier research). On the UI SSO path in v1.106.2 it is checked in
exactly these places:

- `_raise_if_sso_exceeds_free_user_limit`, the 5-user cap
  (`UI_SSO#L986-L1004`), called from the SSO login route
  (`UI_SSO#L1040-L1047`) and the SAML callback (`UI_SSO#L2212`).
- `/sso/debug/login` (`UI_SSO#L4714-L4737`). Debug only.

Not license-checked: the generic OIDC login and callback, role
mapping from groups, `GENERIC_USER_ROLE_ATTRIBUTE`, `ui_access_mode`
(both forms), `PROXY_ADMIN_ID`, `general_settings.custom_sso`, and the
`UI_USERNAME`/`UI_PASSWORD` login.

Grey: `general_settings.custom_ui_sso_sign_in_handler` has no
`premium_user` check but imports `litellm_enterprise`
(`UI_SSO#L1070-L1083`), which is enterprise-licensed code. It runs
after the cap check, so it does not help with the cap either.

The docs page opens with "SSO requires a LiteLLM Enterprise license"
and "From v1.76.0, SSO is free for up to 5 users. Beyond that, an
enterprise license is required." It does not say role mapping or
`ui_access_mode` need a license, and the code agrees.

## Env vars and Keycloak client

LiteLLM side (read in `UI_SSO#L1261-L1317` and `UI_SSO#L1141-L1159`):

| Env var                              | Value for this repo                                                       |
| ------------------------------------ | ------------------------------------------------------------------------- |
| `GENERIC_CLIENT_ID`                  | e.g. `litellm-ui`                                                         |
| `GENERIC_CLIENT_SECRET`              | from `.env`                                                               |
| `GENERIC_AUTHORIZATION_ENDPOINT`     | `http://localhost:8080/realms/playground/protocol/openid-connect/auth`    |
| `GENERIC_TOKEN_ENDPOINT`             | `http://keycloak:8080/realms/playground/protocol/openid-connect/token`    |
| `GENERIC_USERINFO_ENDPOINT`          | `http://keycloak:8080/realms/playground/protocol/openid-connect/userinfo` |
| `GENERIC_SCOPE`                      | default `openid email profile`                                            |
| `GENERIC_USER_ID_ATTRIBUTE`          | default `preferred_username`                                              |
| `GENERIC_ROLE_MAPPINGS_ROLES`        | `{"proxy_admin": ["/llm-admins"]}`                                        |
| `GENERIC_ROLE_MAPPINGS_GROUP_CLAIM`  | the groups claim name, e.g. `groups`                                      |
| `GENERIC_ROLE_MAPPINGS_DEFAULT_ROLE` | `internal_user_viewer`                                                    |
| `GENERIC_CLIENT_USE_PKCE`            | optional, `true` adds PKCE                                                |
| `PROXY_BASE_URL`                     | public origin, e.g. `http://localhost:4000`                               |

All five of `GENERIC_CLIENT_ID`, `_SECRET`, `_AUTHORIZATION_`,
`_TOKEN_` and `_USERINFO_ENDPOINT` are required; a missing one is a
500 (`UI_SSO#L1272-L1300`). Discovery is not used; the three endpoints
are given separately (`UI_SSO#L1509-L1513`). So the authorization
endpoint can be the browser-facing `localhost:8080` host while token
and userinfo use the in-network `keycloak:8080` host. The playground
Keycloak sets `KC_HOSTNAME=http://localhost:8080` with
`KC_HOSTNAME_BACKCHANNEL_DYNAMIC=true` (`docker-compose.yml`), which
is what makes that split work. The provider is built with
`allow_insecure_http=True` (`UI_SSO#L1554-L1560`), so plain HTTP in
the playground is fine.

Keycloak side:

- A confidential OIDC client (client authentication on), standard
  flow on.
- Valid redirect URI `<PROXY_BASE_URL>/sso/callback`.
- A "Group Membership" mapper whose claim lands in the userinfo
  response ("Add to userinfo" on). Role mapping reads the claim from
  the userinfo response (`UI_SSO#L1519-L1547`, `#L1193-L1225`); the
  access token is only consulted as a fallback when no role was found
  (`UI_SSO#L863-L957`), and the existing `key_broker_groups` mapper
  has `userinfo.token.claim: false`. With "Full group path" on, the
  value is `/llm-admins`, which is what the mapping must list.
  `GENERIC_INCLUDE_TOKEN_CLAIMS=true` merges ID and access token
  claims into the userinfo claims instead (`UI_SSO#L1517`,
  `#L1531-L1539`).

## `user_id` = `preferred_username`

Yes, by default. `GENERIC_USER_ID_ATTRIBUTE` defaults to
`preferred_username` (`UI_SSO#L1148`) and becomes the OpenID `id`
(`UI_SSO#L1248-L1249`), then the LiteLLM `user_id`
(`UI_SSO#L3519-L3523`).

On login, LiteLLM looks up an existing user by that id (as `user_id`
and as `sso_user_id`) and by email (`UI_SSO#L1764-L1826`). A user the
key broker already created with the same `user_id` is reused: its
email and, if the SSO gave a valid role, its `user_role` are updated
(`UI_SSO#L3295-L3315`, `#L1892-L1927`). Only a user not found is
inserted, with `sso_user_id` set (`UI_SSO#L2609-L2677`).

## Group -> `proxy_admin`

Works without a license. Three routes, all community code:

1. `GENERIC_ROLE_MAPPINGS_ROLES` (a Python-literal dict, parsed with
   `ast.literal_eval`), `GENERIC_ROLE_MAPPINGS_GROUP_CLAIM`,
   `GENERIC_ROLE_MAPPINGS_DEFAULT_ROLE` (`UI_SSO#L1385-L1411`). The
   highest matching role wins (`UI_SSO#L817-L860`). The same mapping
   can be stored in the DB SSO settings (`UI_SSO#L1364-L1376`); the
   env vars override it. Not mentioned on the docs page.
2. `GENERIC_USER_ROLE_ATTRIBUTE`: a claim whose value is a LiteLLM
   role name (`UI_SSO#L1227-L1238`). Needs a Keycloak mapper that
   emits the literal string `proxy_admin`.
3. `PROXY_ADMIN_ID`: one user id forced to `proxy_admin` on every SSO
   login (`UI_SSO#L2011-L2029`). One user only.

`general_settings.custom_sso` (a Python hook returning
`SSOUserDefinedValues`, loaded without a license check at
`SRC/litellm/proxy/proxy_server.py#L7060-L7062`, called at
`UI_SSO#L3594-L3599`) is a fourth, do-it-yourself route.

The SSO role is authoritative: when the SSO yields a valid role it is
written to the DB user row on every login (`UI_SSO#L1987-L1994`,
`#L1916-L1925`). When it yields none, the DB role is kept
(`UI_SSO#L1995-L2002`).

Two consequences to design around:

- Set `GENERIC_ROLE_MAPPINGS_DEFAULT_ROLE`. Without it, a user with
  no matching group gets no SSO role, so the DB role is kept. A
  removed admin would then stay `proxy_admin` forever. With
  `internal_user_viewer` as default, the next SSO login demotes them.
  Demotion only happens at a login; the DB row keeps `proxy_admin`
  until then.
- The role is per user, not per session. Any virtual key whose owner
  has DB role `proxy_admin` authenticates as `PROXY_ADMIN`
  (`SRC/litellm/proxy/auth/user_api_key_auth.py#L3668-L3672`,
  `SRC/litellm/proxy/auth/auth_checks.py#L1427-L1434`). Because the
  broker's `user_id` equals the SSO `user_id`, an `llm-admins`
  member's opencode key becomes a full admin key after their first UI
  login. Avoiding that needs separate identities, e.g. a different
  `GENERIC_USER_ID_ATTRIBUTE` for the UI, which breaks the "matches
  the key broker" goal.

## Refusing users outside a group

Works without a license, via `general_settings.ui_access_mode`
(loaded at `SRC/litellm/proxy/proxy_server.py#L7083`). Two forms, and
they do not combine (`check_is_admin_only_access` returns `False` for
the dict form,
`SRC/litellm/proxy/management_endpoints/sso_helper_utils.py#L9-L14`):

- `ui_access_mode: admin_only`. After login, a user whose final role
  is not `proxy_admin` or `proxy_admin_viewer` gets HTTP 401
  (`UI_SSO#L3680-L3690`, `sso_helper_utils.py#L17-L27`). With the role
  mapping above this is the `llm-admins` gate. Note the order: the
  user row is created or updated (role written) before the 401
  (`UI_SSO#L3617-L3625`), and a 24h UI key is generated before the
  check too (`UI_SSO#L3652-L3668`), though its cookie is never set.
- `ui_access_mode: {type: restricted_sso_group, restricted_sso_group:
  /llm-admins, sso_group_jwt_field: groups}`. The claim is read into
  `team_ids` (`UI_SSO#L2064-L2075`, `#L1167-L1170`) and a non-member
  gets HTTP 403 before any DB write (`UI_SSO#L3610-L3615`,
  `#L3338-L3371`). The 403 message echoes the whole userinfo response.
  It does not set roles, so role mapping is still needed, and the
  groups also feed team membership for existing teams.

The restricted-group form refuses earlier and writes nothing for
non-members; `admin_only` is simpler and ties refusal to the role.

## `UI_USERNAME`/`UI_PASSWORD` alongside SSO

Still works. The UI login page shows the username/password form plus
a "Login with SSO" button and does not auto-redirect unless
`AUTO_REDIRECT_UI_LOGIN_TO_SSO=true`
(`SRC/ui/litellm-dashboard/src/app/login/LoginPage.tsx#L34-L47`,
`#L130-L133`,
`SRC/litellm/proxy/discovery_endpoints/ui_discovery_endpoints.py#L24`).
The form posts to `/login` or `/v2/login`
(`SRC/litellm/proxy/proxy_server.py#L17623-L17716`). The docs also name
`/fallback/login` (`proxy_server.py#L17597-L17620`).

The env credentials log in as `proxy_admin` with user id
`PROXY_ADMIN_ID` or `default_user_id`
(`SRC/litellm/proxy/auth/login_utils.py#L317-L353`). That user row
counts toward the 5-user cap.

Only two settings turn it off: `disable_env_credential_login: true`,
or `disable_password_login_when_sso_enabled: true` when SSO is fully
configured (`login_utils.py#L170-L183`, `#L260-L269`). Leave both
unset to keep break-glass. Failed attempts are throttled
(`login_utils.py#L279-L280`).

## The 5-user cap

This differs from main/v1.108.0. In v1.106.2 the cap is a gate at the
start of every SSO login, not a limit on creating users:

- `GET /sso/key/generate` calls `_raise_if_sso_exceeds_free_user_limit`
  whenever a Google, Microsoft, generic or SAML client is configured
  (`UI_SSO#L1040-L1047`).
- It counts all billable users and raises HTTP 403 "You must be a
  LiteLLM Enterprise user to use SSO for more than 5 users" when the
  count is greater than 5 (`UI_SSO#L986-L1004`).
- Billable means every row in the user table except rows with
  `metadata.scim_active == false`
  (`SRC/litellm/repositories/user_repository.py#L132-L144`).
- The generic `/sso/callback` itself has no cap check
  (`UI_SSO#L2032-L2158`), but it is only reached through the login
  route.

Consequences:

- Up to 5 user rows: SSO works for everyone. At 5 rows a sixth person
  can still log in and is created; from then on nobody can start SSO.
- Pre-created users do not help. Users made by `/user/new` (the key
  broker) and the `UI_USERNAME` admin row all count. With the broker
  provisioning everyone, the cap is hit as soon as the sixth person
  gets a key, and UI SSO stops for admins too.
- The earlier research's lever (pre-create users to dodge the cap)
  applies to main, where only creation inside SSO is capped. It does
  not apply to v1.106.2.
- `/user/new` itself is not capped without a license
  (`internal_user_endpoints.py#L574-L580` in
  `SRC/litellm/proxy/management_endpoints/`).

So for more than 5 LiteLLM users in total, UI SSO on v1.106.2 needs
`LITELLM_LICENSE`, or an upgrade to a version with the narrower
creation-only cap (verify the target tag before relying on it).

## Session lifetime

- A successful SSO login generates a virtual key with
  `duration=LITELLM_UI_SESSION_DURATION`, team `litellm-dashboard`
  (`UI_SSO#L3652-L3668`). Default `24h`
  (`SRC/litellm/constants.py#L1790`).
- The key is wrapped in an HS256 JWT signed with the master key, with
  `exp` set to the same duration
  (`SRC/litellm/proxy/auth/login_utils.py#L478-L498`).
- It is stored in a `token` cookie: not HttpOnly (the dashboard reads
  it), `SameSite=Lax`, `Secure` only when the request is seen as
  HTTPS, no `Max-Age` (`UI_SSO#L2862-L2877`).
- It is not bound to the Keycloak session. No refresh token is kept
  for the UI, and Keycloak logout or group removal does not end it.
  It lives until the JWT or key expires, the key is deleted, or the
  master key rotates. Role changes from Keycloak apply at the next SSO
  login only.
- `PROXY_LOGOUT_URL` is passed to the UI as the logout target
  (`UI_SSO#L2689-L2703`), e.g. Keycloak's end-session endpoint.

## Paths behind nginx

| Path                                                     | Purpose                          |
| -------------------------------------------------------- | -------------------------------- |
| `/ui/`, `/ui/login`                                      | Dashboard and its login page     |
| `/sso/key/generate`                                      | SSO start, redirects to Keycloak |
| `/sso/callback`                                          | OIDC redirect URI                |
| `/login`, `/v2/login`, `/fallback/login`                 | Username/password login          |
| `/sso/get/ui_settings`, `/.well-known/litellm-ui-config` | UI config                        |

- The redirect URI is `get_custom_url(request.base_url) +
  "sso/callback"` (`UI_SSO#L3259-L3279`). `get_custom_url` uses
  `PROXY_BASE_URL` if set, else the request base URL, plus
  `SERVER_ROOT_PATH` (`SRC/litellm/proxy/utils.py#L8826-L8872`). The
  post-login redirect goes to `<base>/ui/?login=success`
  (`UI_SSO#L3703`, `#L3752-L3757`).
- This repo's nginx sends everything not under `/broker/`,
  `/opencode/` or `/metrics` to LiteLLM with `Host $http_host`
  (`nginx/locations.conf`), so all paths above already reach LiteLLM
  and no nginx change is needed.
- Set `PROXY_BASE_URL` anyway. Behind TLS-terminating nginx the
  request scheme seen by LiteLLM may be `http`, which would give an
  `http://` redirect URI and a non-`Secure` cookie.
- The UI builds the SSO start URL from its proxy base URL
  (`LoginPage.tsx#L133`), so the dashboard and `/sso/*` must share the
  same origin as served here.

## Levers for the wider effort

1. Generic OIDC env vars + `GENERIC_ROLE_MAPPINGS_ROLES={"proxy_admin":
   ["/llm-admins"]}` + `GENERIC_ROLE_MAPPINGS_DEFAULT_ROLE=
   internal_user_viewer` + `ui_access_mode: admin_only`. All community.
2. Keep `UI_USERNAME`/`UI_PASSWORD`; do not set
   `disable_password_login_when_sso_enabled`.
3. Decide on the cap before building: more than 5 user rows (broker
   users included) disables UI SSO on v1.106.2.
4. Decide whether admins' opencode keys may carry `proxy_admin`; with
   a shared `user_id` they will.
