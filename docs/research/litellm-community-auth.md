# LiteLLM community edition: which auth features work?

Research for issue #2. Question: which LiteLLM auth features work in
the open-source (community) edition and which are enterprise-only.

## Version looked at

- LiteLLM `main` at commit `e159005545fc3e92574d47ca96225be5f75d96fb`
  (2026-10-24), `pyproject.toml` version `1.108.0`.
- Docs at https://docs.litellm.ai on 2026-10-24.

Code links below are pinned to that commit. `SRC` is shorthand for
`https://github.com/BerriAI/litellm/blob/e159005545fc3e92574d47ca96225be5f75d96fb`.

## How gating works

There is no separate community build. One codebase checks a global
`premium_user`, which is `True` only when a valid `LITELLM_LICENSE`
env var is set (`SRC/litellm/proxy/auth/litellm_license.py#L100-L125`,
`SRC/litellm/proxy/proxy_server.py#L1482`). "Enterprise-only" in this
document means "the code raises or no-ops when `premium_user` is not
`True`".

The `litellm-enterprise` package is a hard dependency of `litellm`
(`SRC/pyproject.toml#L83`), so its code is installed in the community
image too. Code under `enterprise/` is under the BerriAI Enterprise
License, which needs a license for production use but explicitly
allows copying and modifying "for development and testing purposes"
(`SRC/enterprise/LICENSE.md`). That matters for one item below
(`custom_auth_settings`).

## Summary table

| Feature                                    | Community? | Gate                                                   |
| ------------------------------------------ | ---------- | ------------------------------------------------------ |
| UI SSO, generic OIDC (Keycloak)            | Yes        | Max 5 users created by SSO, then 403                   |
| SSO login for users pre-created by API     | Yes        | Not counted against the SSO cap at login               |
| SSO groups claim -> existing team members  | Yes        | None found                                             |
| JWT auth on API calls (`enable_jwt_auth`)  | No         | `premium_user` check, 403                              |
| OAuth2 token introspection auth            | No         | `premium_user` check, 403                              |
| OAuth2-proxy header auth                   | Yes        | Needs trusted proxy CIDRs, identity fields only        |
| `custom_auth` hook                         | Yes        | None                                                   |
| `custom_auth_settings.mode: auto`          | Runs       | No code gate; enterprise-licensed code                 |
| `custom_ui_sso_sign_in_handler`            | Runs       | No `premium_user` gate; enterprise-licensed code       |
| `/user/new`, `/key/generate`, `/team/new`  | Yes        | Seat limit only applies when a license sets one        |
| `/team/member_add` with role `admin`       | No         | `premium_user` check                                   |
| Key `tags`                                 | No         | `premium_user` check                                   |
| SCIM v2 provisioning                       | No         | `premium_user_check` dependency                        |
| `public_routes`, `admin_only_routes`       | No         | `premium_user` check                                   |

## UI SSO and the 5-user cap

Docs: "From v1.76.0, SSO is free for up to 5 users. Beyond that, an
enterprise license is required."
(https://docs.litellm.ai/docs/proxy/admin_ui_sso)

Code, which is narrower than the docs suggest:

- The SSO callback creates a new user inside
  `free_sso_user_creation(..., enabled=premium_user is not True)`
  (`SRC/litellm/proxy/management_endpoints/ui_sso.py#L2795-L2810`).
- Inside that scope, `UserRepository.upsert_user` takes an advisory
  lock and returns `None` if the user does not exist yet and the
  table already has 5 or more billable users
  (`SRC/litellm/repositories/user_repository.py#L171-L196`).
  Billable means every user row except SCIM-deactivated ones
  (`#L161-L169`), so users created via `/user/new` also count toward
  the 5.
- `None` turns into HTTP 403 "SSO for more than 5 users requires a
  LiteLLM Enterprise license. Existing users can still sign in."
  (`SRC/litellm/proxy/utils.py#L6034-L6040`).
- Only the create path is capped. Before creating, the callback looks
  up an existing user by SSO id / user id and by email
  (`SRC/litellm/proxy/management_endpoints/ui_sso.py#L1880-L1925`); if
  found, it just updates email/alias and logs in
  (`#L3519-L3537`). Outside the SSO scope, `upsert_user` has no cap
  (`user_repository.py#L174-L181`).

Consequence: users pre-created through `/user/new` (whose `user_id`
equals the SSO user id, or whose `user_email` matches) can sign in via
SSO without limit. This is the main dirty lever for us.

Not verified by running it: exact matching behaviour of
`get_user_object` on email when `user_id` differs. Worth a quick test
in the playground.

## Generic OIDC SSO (Keycloak)

Works in community. The `GENERIC_*` env vars
(`GENERIC_CLIENT_ID`, `GENERIC_CLIENT_SECRET`,
`GENERIC_AUTHORIZATION_ENDPOINT`, `GENERIC_TOKEN_ENDPOINT`,
`GENERIC_USERINFO_ENDPOINT`, `GENERIC_SCOPE`, PKCE via
`GENERIC_CLIENT_USE_PKCE`) are read in
`SRC/litellm/proxy/management_endpoints/ui_sso.py#L1324-L1380` with no
license check on the login or callback path. The only
`premium_user` gate on generic SSO is the `/sso/debug/login` route
(`#L4986-L5000`).

Claim mapping env vars (`ui_sso.py#L1211-L1222`):
`GENERIC_USER_ID_ATTRIBUTE` (default `preferred_username`),
`GENERIC_USER_EMAIL_ATTRIBUTE`, `GENERIC_USER_ROLE_ATTRIBUTE`,
`GENERIC_USER_EXTRA_ATTRIBUTES`. Role mapping from groups:
`GENERIC_ROLE_MAPPINGS_ROLES`, `GENERIC_ROLE_MAPPINGS_GROUP_CLAIM`,
`GENERIC_ROLE_MAPPINGS_DEFAULT_ROLE` (`#L1448-L1462`).

New SSO users get role `internal_user_viewer` unless a role is
mapped or `default_internal_user_params` is set in config
(`ui_sso.py#L2753-L2778`). For self-service keys we will want
`default_internal_user_params: {user_role: internal_user}`.

UI SSO is for the admin UI only. It does not authenticate API calls
from opencode; those still need a virtual key (or JWT, which is
enterprise).

## JWT auth for API calls

Enterprise-only. Docs: "JWT-based Auth requires a LiteLLM Enterprise
license." (https://docs.litellm.ai/docs/proxy/token_auth)

Code: when `enable_jwt_auth` is set and the bearer token is a JWT,
`user_api_key_auth` raises 403 "JWT Auth is an enterprise only
feature" unless `premium_user`
(`SRC/litellm/proxy/auth/user_api_key_auth.py#L2250-L2262`). This
covers everything behind it: `team_ids_jwt_field`,
`user_id_upsert`, JWT-to-virtual-key mapping.

OAuth2 token introspection (`enable_oauth2_auth`) is gated the same
way (`user_api_key_auth.py#L2233-L2243`,
`SRC/litellm/proxy/auth/oauth2_check.py#L129-L134`).

Not gated: `enable_oauth2_proxy_auth`, header-based identity from a
trusted reverse proxy such as oauth2-proxy
(`SRC/litellm/proxy/auth/oauth2_proxy_hook.py`). It only accepts the
fields `user_id`, `user_email`, `team_id`, `team_alias`, `org_id`,
`models` from headers and requires the caller to be in
`trusted_proxy_cidrs`. It does not give per-user virtual keys and does
not help a CLI client like opencode that sends a bearer token.

## `custom_auth` hook

Works in community. `general_settings.custom_auth` is loaded with no
license check (`SRC/litellm/proxy/proxy_server.py#L7062-L7068`) and
called before any built-in auth
(`SRC/litellm/proxy/auth/user_api_key_auth.py#L2159-L2192`).
Signature: `async def fn(request: Request, api_key: str) ->
UserAPIKeyAuth`. With `custom_auth_run_common_checks: true` or
`litellm_settings.enable_post_custom_auth_checks: true`, budgets and
model allowlists from the DB are still applied.

So a community proxy can validate Keycloak JWTs itself: the hook
verifies the token against Keycloak's JWKS and returns a
`UserAPIKeyAuth(user_id=..., team_id=...)`. This is a do-it-yourself
replacement for enterprise JWT auth.

Caveat on the mode switch. Docs: "Using Custom Auth with LiteLLM
Virtual Keys requires a LiteLLM Enterprise license."
(https://docs.litellm.ai/docs/proxy/custom_auth). In code that is
`custom_auth_settings.mode` (`on` / `off` / `auto`) and the
"return a string to use it as a virtual key" behaviour, implemented in
`litellm_enterprise` (`SRC/enterprise/litellm_enterprise/proxy/auth/user_api_key_auth.py`,
`SRC/enterprise/litellm_enterprise/proxy/proxy_server.py`). There is
no `premium_user` check; it runs whenever the package imports, which
it does by default. It is enterprise-licensed code, so it is fine for
a playground (dev/testing clause) but not a production answer. Without
`auto`, a custom_auth hook replaces virtual-key auth entirely, so the
hook would have to handle `sk-...` keys itself.

`custom_ui_sso_sign_in_handler` (header-based UI login) is the same
story: imported from `litellm_enterprise`, no `premium_user` check
(`SRC/litellm/proxy/management_endpoints/ui_sso.py#L1133-L1145`).

## User, key and team management APIs

Work in community.

- `/user/new` checks duplicates, then
  `_license_check.is_over_limit(total_users=...)`
  (`SRC/litellm/proxy/management_endpoints/internal_user_endpoints.py#L550-L559`).
  `is_over_limit` is `False` when no license sets a seat limit
  (`SRC/litellm/proxy/auth/litellm_license.py#L139-L144`). No cap
  without a license.
- `/team/new` has the same shape with `is_team_count_over_limit`,
  which is `False` without an airgapped license
  (`SRC/litellm/proxy/management_endpoints/team_endpoints.py#L1663`,
  `litellm_license.py#L146-L155`).
- `/key/generate` is not gated. Enterprise-only bits inside it: key
  `tags` (`SRC/litellm/proxy/management_endpoints/key_management_endpoints.py#L1650-L1653`),
  `enforced_params`, and model access groups on wildcard models
  (`#L4972-L4990`).
- `/team/member_add` works, except adding a member with role `admin`
  (`team_endpoints.py#L2903-L2917`).
- Personal key creation by non-admins can be restricted with
  `key_generation_settings` (`key_management_endpoints.py#L774-L800`);
  not license-gated.
- SCIM v2 is enterprise (`SRC/litellm/proxy/management_endpoints/scim/scim_v2.py#L292`).

So an admin script or sidecar holding the master key can create one
LiteLLM user and one virtual key per Keycloak user, with no license.

## Groups -> teams

- On every UI SSO login, LiteLLM reads team ids from a claim and adds
  the user as a `user`-role member to each listed team that already
  exists (`SRC/litellm/proxy/management_endpoints/ui_sso.py#L1230-L1250`,
  `#L1801-L1830`, `#L3562-L3576`). No `premium_user` check on this
  path.
- The claim is configured via `team_ids_jwt_field` in the DB SSO
  settings `team_mappings` (`#L1383-L1416`) or
  `general_settings.ui_access_mode.sso_group_jwt_field`
  (`#L2203-L2213`); the latter also restricts UI login to one group.
- Teams are not auto-created; a group with no matching `team_id` is
  skipped (docs: admin_ui_sso). So team ids must equal the claim
  values. Keycloak's group mapper emits full paths like `/eng` unless
  "Full group path" is turned off.
- Making SSO users team admins is enterprise (`team_member_add` with
  role `admin`).
- Team sync for API calls via JWT is enterprise (see JWT above).
  A `custom_auth` hook can set `team_id` itself instead.

## Levers for our goal (least end-user friction)

1. Pre-provision users with `/user/new` from Keycloak (script or
   sidecar), so the 5-user SSO cap never triggers. Optionally create a
   key per user with `/key/generate`.
2. Use generic OIDC UI SSO so users can log in to the LiteLLM UI and
   see or create their own key, with
   `default_internal_user_params.user_role: internal_user`.
3. For zero-copy-paste API auth, a `custom_auth` hook that verifies
   Keycloak tokens is the community path; JWT auth itself is
   enterprise.
4. Groups -> teams on UI login works in community if team ids match
   the Keycloak group claim and teams are pre-created.
