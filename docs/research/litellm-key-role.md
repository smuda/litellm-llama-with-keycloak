# LiteLLM: do virtual keys inherit their owner's role?

Research for issue #40. Question: in LiteLLM v1.106.2 community, does
a virtual key act with its owner's `user_role`, so that a
`proxy_admin` user's opencode key can call admin routes? Does that
include keys minted before the role changed? Which per-key limits
exist, and which are enterprise-only?

## Version looked at

- LiteLLM tag `v1.106.2`, commit
  `49943cd52ea46aacf8db2e5cb2b890d580fe9bb4` (`pyproject.toml`
  version `1.106.2`), the image in `docker-compose.yml`.
- Docs at https://docs.litellm.ai on 2026-10-27. The docs track a
  newer release; where they differ from the v1.106.2 code, the code
  wins and the difference is noted.

`SRC` is shorthand for this URL, and `MGMT` for
`SRC/litellm/proxy/management_endpoints`:

https://github.com/BerriAI/litellm/blob/49943cd52ea46aacf8db2e5cb2b890d580fe9bb4

Enterprise gating in general (`premium_user`, `public_routes`,
`admin_only_routes`, `DISABLE_ADMIN_ENDPOINTS`) is covered in
`docs/research/litellm-community-auth.md` on branch
`research/litellm-community-auth`; not repeated here.

## Answer

- Yes. A virtual key has no role of its own. Each request's role is
  read from the owning user's row (`LiteLLM_UserTable.user_role`,
  looked up by the key's `user_id`). A key owned by a `proxy_admin`
  user is a `proxy_admin` key and skips every non-admin route check.
- Keys minted before the role change pick it up. Nothing about the
  role is stored on the key or in the key cache. The new role takes
  effect once the cached user row expires, within 60 s by default.
- `allowed_routes` (or `key_type: llm_api`, which sets
  `allowed_routes: ["llm_api_routes"]`) is community, and it is
  enforced for every role, including `proxy_admin`. It is the lever
  that stops a `proxy_admin`-owned opencode key from calling
  `/key/*`, `/user/*`, `/team/*`, `/model/*`.
- The `llm_api_routes` bucket is not purely "LLM calls": it also
  lets an admin-owned key write the agent registry (`/v1/agents`),
  and it includes provider pass-through prefixes. See below.
- `permissions` is a guardrail on/off map, not a route limit.
  `allowed_passthrough_routes` and `denied_passthrough_routes` are
  enterprise (`premium_user_check`).

## How the role of a key-authenticated request is derived

1. The key is resolved from cache or DB through `IdentityStore`
   (`SRC/litellm/proxy/auth/resolvers/store.py#L108-L151`). The DB
   query is the `combined_view` join of `LiteLLM_VerificationToken`
   with team, team-membership, budget and organization tables; it
   does not join the user table and selects no `user_role`
   (`SRC/litellm/proxy/utils.py#L5507-L5570`). The key table has no
   role column; it has `permissions`, `allowed_routes` and
   `key_type` (`SRC/litellm/proxy/schema.prisma#L500-L524`). The
   cached copy is that same DB object
   (`SRC/litellm/proxy/auth/auth_checks.py#L3157-L3176`).
2. `validate_resolved_virtual_key` loads the owner with
   `get_user_object(user_id=valid_token.user_id, ...)`
   (`SRC/litellm/proxy/auth/user_api_key_auth.py#L2350-L2367`).
3. `_return_user_api_key_auth_obj` sets
   `user_role = get_user_role(user_obj) or INTERNAL_USER`, and forces
   `PROXY_ADMIN` when `is_user_proxy_admin(user_obj)`
   (`SRC/litellm/proxy/auth/user_api_key_auth.py#L3629-L3674`).
   `get_user_role` maps the row's `user_role`; NULL or an unknown
   value becomes `internal_user`
   (`SRC/litellm/proxy/auth/auth_checks.py#L1382-L1394`).
4. Route authorisation, `_is_api_route_allowed`, runs the
   non-admin route check only when the owner row is not
   `proxy_admin` (`SRC/litellm/proxy/auth/auth_checks.py#L1400-L1435`).
   Handlers then check `user_api_key_dict.user_role` themselves, for
   example key ownership rebinding
   (`MGMT/key_management_endpoints.py#L899-L944`).

The code says so in a comment on `/key/regenerate`: "The `user_id`
on a verification token is what `_return_user_api_key_auth_obj`
resolves against `litellm_usertable` to derive the request's role"
(`key_management_endpoints.py#L906-L909`).

The docs agree. "Virtual Keys > What a key inherits from its owner"
says management-route access "is decided by the role of the owning
user" and "If the owner is `proxy_admin`, every non-admin route
restriction is skipped"
(https://docs.litellm.ai/docs/proxy/virtual_keys).

The only key object cached with a role is the master key's, which
is built with `user_role=PROXY_ADMIN`
(`user_api_key_auth.py#L2165-L2190`); the cache-hit admin
short-cut at `#L2088-L2117` only fires for that object.

## Keys minted before a role change

They follow the user row, so they gain or lose admin with the user.
Delay is the user cache:

- `get_user_object` returns a cached `LiteLLM_UserTable` when present
  and writes the DB row back with `get_management_object_ttl`
  (`SRC/litellm/proxy/auth/auth_checks.py#L2759-L2771`,
  `#L2863-L2869`).
- That TTL is the cache's in-memory default, 60 s
  (`SRC/litellm/proxy/proxy_server.py#L2081`), or
  `general_settings.user_api_key_cache_ttl` if set (`#L6961-L6969`;
  `SRC/litellm/proxy/common_utils/user_api_key_cache.py#L403-L414`).
- `/user/update` evicts the cached user only when budget/limit fields
  or `metadata` change, not on `user_role`
  (`MGMT/internal_user_endpoints.py#L125`,
  `#L1637-L1641`). So a role change via the API lands within one TTL,
  per worker.
- SSO login writes the mapped role (e.g. from
  `GENERIC_ROLE_MAPPINGS_*` / `GENERIC_USER_ROLE_ATTRIBUTE`) onto the
  same user row (`MGMT/ui_sso.py#L1892-L1926`).
  It is the same row the key broker created, so the opencode key
  becomes an admin key after that login.

Our playground config sets no Redis auth cache and no TTL, so the
delay is at most about 60 s.

## Per-key restrictions

| Field                                   | Community?   | Stops admin-owned key on admin routes? |
| --------------------------------------- | ------------ | -------------------------------------- |
| `allowed_routes`                        | Yes          | Yes, enforced for all roles            |
| `key_type: llm_api`                     | Yes          | Yes, sets `["llm_api_routes"]`         |
| `key_type: read_only`                   | Yes          | Yes, sets `["info_routes"]`            |
| `allowed_passthrough_routes`            | No (premium) | No, only adds pass-through grants      |
| `denied_passthrough_routes`             | No (premium) | Pass-through routes only               |
| `permissions`                           | Yes          | No, guardrail toggles                  |
| `models`, budgets, rpm/tpm, `blocked`   | Yes          | No, LLM-call limits                    |
| `object_permission` (MCP, agents, ...)  | Yes          | No, object scopes                      |

### `allowed_routes` and `key_type`

- Stored on the key (`schema.prisma#L515-L516`). `key_type` is
  expanded at generation: `llm_api` -> `["llm_api_routes"]`,
  `management` -> `["management_routes"]`, `read_only` ->
  `["info_routes"]`
  (`MGMT/key_management_endpoints.py#L879-L893`;
  enum at `SRC/litellm/proxy/_types.py#L1387-L1395`).
- Not license-gated: no `premium_user` check on either field.
  Setting raw `allowed_routes` needs a `proxy_admin` caller; non-admins
  may only get the `llm_api_routes` / `info_routes` presets
  (`key_management_endpoints.py#L946-L988`, re-checked after
  `key_type` at `#L1421-L1432`). The broker calls `/key/generate`
  with the master key, so it may set either.
- Enforcement: the auth wrapper calls
  `RouteChecks.should_call_route` for every authenticated request,
  before `common_checks` and regardless of role
  (`SRC/litellm/proxy/auth/user_api_key_auth.py#L3339-L3356`, called
  from `#L3587`). It runs
  `is_virtual_key_allowed_to_call_route`, commented "Applies to all
  Roles", which returns 403 "Virtual key is not allowed to call this
  route" unless the route matches
  (`SRC/litellm/proxy/auth/route_checks.py#L76-L215`). An empty list
  means no restriction (`#L106-L111`).
- Docs: `allowed_routes` "is enforced for every role, including
  admin-owned keys" (https://docs.litellm.ai/docs/proxy/virtual_keys).

What `llm_api_routes` still lets through
(`SRC/litellm/proxy/_types.py#L666-L677`):

- OpenAI, Anthropic, Google, MCP-inference, A2A agent-inference,
  RAG and "lens" routes, and model info.
- Provider pass-through prefixes (`/openai/*`, `/anthropic/*`,
  `/langfuse/*`, ...; `_types.py#L504-L541`). Pass-through routes
  that enforce auth additionally need an `allowed_passthrough_routes`
  grant (`route_checks.py#L124-L137`, `#L792-L837`).
- Agent registry CRUD (`/v1/agents`, `/v1/agents/{id}`, ...;
  `_types.py#L624-L630`) through a backward-compatibility carve-out
  (`route_checks.py#L180-L190`). The handlers allow writes for
  `PROXY_ADMIN`
  (`SRC/litellm/proxy/agent_endpoints/endpoints.py#L219-L222`),
  so an admin-owned `llm_api` key can create and delete agents.
- `GET /v1/mcp/server[/{id}]`; the handler strips URLs, headers and
  credentials for any key with non-empty `allowed_routes`
  (`route_checks.py#L166-L178`,
  `MGMT/mcp_management_endpoints.py#L767-L776`).

It does not include `/key/*`, `/user/*`, `/team/*`, `/model/new`
and the rest of `management_routes` (`_types.py#L744-L800`). A tighter
custom list (for example `["openai_routes", "anthropic_routes"]` or
exact paths) avoids the agent carve-out, since the carve-out only
runs for the `llm_api_routes` group name.

### `allowed_passthrough_routes` / `denied_passthrough_routes`

Both are in `LiteLLM_ManagementEndpoint_MetadataFields_Premium`
(`SRC/litellm/proxy/_types.py#L5161-L5172`). Setting a non-empty value
calls `premium_user_check`
(`MGMT/common_utils.py#L525-L526`,
from `key_management_endpoints.py#L1401-L1407` and `#L2391-L2395`).
They only grant or deny pass-through routes, never admin routes.
The non-admin caller check is at `common_utils.py#L187-L210`.

### `permissions`

A JSON map of guardrail name to on/off, read in
`should_proceed_based_on_api_key`
(`SRC/litellm/proxy/guardrails/guardrail_helpers.py#L70-L92`).
Setting it needs a `proxy_admin` caller
(`key_management_endpoints.py#L1028-L1048`). No license check. Not a
route control.

### Instance-wide switches (not per key)

`DISABLE_ADMIN_ENDPOINTS` blocks all management routes for every
caller and is enterprise: without a license it returns 500
(`SRC/enterprise/litellm_enterprise/proxy/auth/route_checks.py`,
`#L27-L75`).
It would also break the admin UI.

## Docs vs v1.106.2 code

The docs section "Default /key/generate params" says a default
`allowed_routes` list is copied into new keys and replaces `key_type:
llm_api` (https://docs.litellm.ai/docs/proxy/virtual_keys). In
v1.106.2 `default_key_generate_params` only fills `max_budget`,
`user_id`, `team_id`, `max_parallel_requests`, `tpm_limit`,
`rpm_limit`, `budget_duration`, `duration`, `models` and `metadata`
(`key_management_endpoints.py#L1267-L1291`). So in this version the
broker has to send `allowed_routes` or `key_type` itself.

## Consequences for this repo

- Today the broker mints keys with only `user_id` and `duration`
  (`key-broker/internal/broker/litellm.go`), so `allowed_routes` is
  empty. If the same user becomes `proxy_admin` via UI SSO, their
  opencode key becomes a full admin key within about 60 s, including
  keys minted earlier.
- Fix within community: send `key_type: "llm_api"` (or an explicit
  `allowed_routes` list) on `/key/generate`. Existing keys can be
  fixed with `/key/update` from the master key, or left to expire
  (14 d).
- `internal_user_viewer` (issue #21) remains the right default role,
  but it is the user's role, not the key's; it gives no protection
  once the role is raised.

Not verified by running it: the agent-registry write with an
admin-owned `llm_api` key, and the exact delay after an SSO role
change. The live admin API was not exercised for this research.
