# How opencode authenticates to an OpenAI-compatible provider

Research for issue #3. Question: how can opencode authenticate to a
custom OpenAI-compatible provider (LiteLLM), and what does each option
cost the end user in re-logins and hand-copied values?

Sources: opencode source and docs at tag `v1.19.4` (released
2026-10-22) of `anomalyco/opencode` (the repo `sst/opencode` now
redirects there). Paths below are relative to that tag. Base URL:
https://github.com/anomalyco/opencode/tree/v1.19.4

## Short answer

- opencode has no generic OIDC / OAuth support for custom providers and
  no key-helper command (nothing like `apiKeyHelper` or `{cmd:...}`).
  Config only knows `{env:...}` and `{file:...}`, resolved once when
  config loads.
- The least-friction built-in path is the well-known login:
  `opencode auth login https://<litellm-host>`. The server publishes
  `/.well-known/opencode`, which names a command opencode runs locally
  to obtain a token, plus a config blob that defines the provider. The
  user types one command once and copies nothing. The token is stored
  as-is with no refresh, so it must be long-lived (a per-user LiteLLM
  virtual key, not a Keycloak access token).
- Real OAuth with refresh is only available through a plugin `auth`
  hook. The built-in xAI plugin is a ready template for an RFC 8628
  device flow with refresh and a per-request `fetch` override. The
  user runs `/connect` once and re-logs only when the Keycloak refresh
  token / offline session dies.

## Where things are resolved in code

| Concern                       | File                                                   |
| ----------------------------- | ------------------------------------------------------ |
| `{env:}` / `{file:}`          | `packages/opencode/src/config/variable.ts`             |
| Credential store              | `packages/opencode/src/auth/index.ts`                  |
| `auth login` / `/connect`     | `packages/opencode/src/cli/cmd/providers.ts`           |
| Well-known remote config      | `packages/opencode/src/config/config.ts`               |
| Provider options, key, loader | `packages/opencode/src/provider/provider.ts`           |
| Plugin hook types             | `packages/plugin/src/index.ts`                         |
| `chat.headers` hook call      | `packages/opencode/src/session/llm/request.ts`         |
| Device-flow plugin example    | `packages/opencode/src/plugin/xai.ts`                  |
| Docs                          | `packages/web/src/content/docs/{providers,config,plugins}.mdx` |

## Options

### 1. Static API key in config

Custom provider block with `npm: "@ai-sdk/openai-compatible"`,
`options.baseURL` and `options.apiKey` (docs `providers.mdx`, section
"Custom provider"). In `resolveSDK` the key goes to the AI SDK as
`apiKey`, which sends `Authorization: Bearer <key>`
(`provider.ts`, `if (options["apiKey"] === undefined && provider.key)
options["apiKey"] = provider.key`).

User experience: paste a LiteLLM virtual key into `opencode.json` once.
Never re-login while the key is valid. The key sits in plain text in a
file that may be committed.

### 2. Environment variables and `{env:}` / `{file:}` substitution

`ConfigVariable.substitute` replaces `{env:VAR}` with the env value
(empty string if unset) and `{file:path}` with the trimmed file
contents (`~/` and config-relative paths supported, missing file is a
config error). Both are textual substitutions on the config text at
load time, not per request.

Also, a provider config may list `env: ["VAR"]`
(`packages/core/src/v1/config/provider.ts`); `provider.ts` picks the
first set variable as the key.

User experience: same as option 1 (one long-lived key), but the key
lives in the shell profile or a secrets file. Rotating the value only
takes effect on the next opencode start. A short-lived token here means
the user refreshes it by hand and restarts opencode.

There is no command substitution or key-helper. A dirty workaround is a
wrapper script that fetches a token, exports it, then `exec opencode`;
that still yields one token per process lifetime.

### 3. Custom headers

`options.headers` on the provider and `headers` on each model are sent
with every request (docs `providers.mdx`; merged in `provider.ts` and
`request.ts`). Values support `{env:}` / `{file:}` like any config
string, so they are equally static per process. Useful if LiteLLM
should get the identity in a non-`Authorization` header.

User experience: identical to options 1 and 2.

### 4. `opencode auth login` / `/connect` with an API key

`opencode auth login` (TUI: `/connect`), choose "Other", enter a
provider id, paste a key. Stored as `{ type: "api", key }` in
`~/.local/share/opencode/auth.json` (mode 0600, `auth/index.ts`). The
CLI warns that the user must still define the provider in
`opencode.json` with the same id. `provider.ts` merges the stored key
into that provider.

`OPENCODE_AUTH_CONTENT` (env var holding the whole auth JSON) overrides
the file, which is handy for scripted setups.

User experience: copy the key once from wherever it is issued (for
example a LiteLLM UI), paste at a password prompt. Plus the provider
block in config, which can come from a shared project file. Re-login
only when the key is revoked or expires.

### 5. Well-known login: `opencode auth login <url>`

This is the closest thing to an organisational SSO flow, and it is
generic (not tied to a vendor). Flow from `providers.ts` and
`config.ts`:

1. `opencode auth login https://host` fetches
   `https://host/.well-known/opencode` and expects
   `{ "auth": { "command": [...], "env": "VAR" }, "config": {...},
   "remote_config": { "url": "...", "headers": {...} } }`.
2. It spawns `auth.command` locally (argv array, stderr shown to the
   user, so interactive prompts and printed URLs work), reads stdout
   as the token, and stores
   `{ type: "wellknown", key: "VAR", token }` under the URL.
3. On every opencode start, `config.ts` sets `VAR=<token>` in an
   auth env map, re-fetches `/.well-known/opencode`, optionally fetches
   `remote_config.url` with headers that may use `{env:VAR}`, and loads
   the result as the lowest-precedence config layer. The same auth env
   is available to `{env:VAR}` in the global and project configs too.
4. If the remote config endpoint answers with an HTML login page, it
   raises `RemoteAuthError` telling the user to re-auth.

So the server can ship the whole LiteLLM provider block, including
`"apiKey": "{env:VAR}"`, models, and even a `plugin` list (remote config
is merged like any other config layer, including plugins). Docs:
`config.mdx` section "Remote".

The token is stored verbatim and never refreshed. opencode does not
know or care whether it expires.

User experience: run one command once, e.g.
`opencode auth login https://litellm.example`. The command that runs is
whatever the server says, so it could open a Keycloak device or browser
login and then mint or fetch a per-user LiteLLM virtual key and print
it. Nothing is copied by hand. If the printed token is a long-lived
virtual key, the user never logs in again. If it is a Keycloak access
token (minutes), the user would re-run login constantly, so do not do
that. Caveat: the command must already exist on the user's machine
(a dirty answer is `["sh", "-c", "curl -fsS https://host/login.sh |
sh"]`, which the user should be told about).

### 6. Plugin `auth` hook: OAuth / device flow with refresh

The plugin API (`packages/plugin/src/index.ts`, `AuthHook`) lets a
plugin register auth methods for a provider id:

- `methods`: `type: "oauth"` with `authorize()` returning a URL,
  instructions and either `method: "auto"` (plugin polls, used for
  device flow) or `method: "code"` (user pastes a code). Success
  returns `{ refresh, access, expires }` or `{ key }`. Also
  `type: "api"` with custom prompts.
- `loader(getAuth, provider)`: called at provider setup when a
  credential for that id is stored (`provider.ts`, skipped if none is
  stored). It returns provider options that are merged in. Returning
  `{ apiKey: "dummy", fetch }` gives a fetch override that wraps every
  model request (`timeoutFetch` in `provider.ts` uses
  `options["fetch"]`).

`packages/opencode/src/plugin/xai.ts` does exactly the Keycloak-shaped
thing for xAI: RFC 8628 device authorization grant against an OIDC
issuer, `offline_access` scope, a `fetch` override that refreshes the
access token 120 s before expiry with single-flight protection, writes
rotated tokens back via `input.client.auth.set`, and sets
`Authorization: Bearer <access>`. Swapping the endpoints and client id
for a Keycloak public client with device flow enabled is most of the
work.

Plugins load from `~/.config/opencode/plugins/`,
`.opencode/plugins/`, or npm packages listed under `plugin` in config
(docs `plugins.mdx`). The provider still needs a config block with a
matching id.

User experience: install the plugin (or receive it through a shared or
remote config), run `/connect`, pick the provider and "Login with
Keycloak", open the shown URL, enter the user code. Nothing to copy
except the short user code shown on screen. Re-login only when the
Keycloak refresh token expires (SSO session idle / max, or the offline
session if `offline_access` is used, which can be made effectively
unlimited). Note that this sends the Keycloak access token to LiteLLM,
so LiteLLM must accept and map that JWT to a user; that is a separate
question for the LiteLLM side.

### 7. Plugin `chat.headers` hook

`Hooks["chat.headers"]` is an async hook triggered for every LLM call
(`session/llm/request.ts`); its output headers are spread after
`model.headers`. A plugin could compute or refresh a token from its own
cache and set `Authorization` per request, without using the auth store
or `/connect`. Overriding the SDK's own `Authorization` relies on the
AI SDK merging call-level headers last; that is how the AI SDK works but
it was not tested here.

User experience: depends on how the plugin gets its first token (could
run a device flow on first use). Same re-login profile as option 6.

## Friction summary

| Option                         | First-time user action             | Re-login            | Copied by hand        |
| ------------------------------ | ---------------------------------- | ------------------- | --------------------- |
| 1 Static key in config         | Edit `opencode.json`               | When key expires    | Key                   |
| 2 Env var / `{file:}`          | Set env var or write file          | When key expires    | Key                   |
| 3 Custom headers               | Edit config                        | When value expires  | Header value          |
| 4 `auth login`, API key        | Paste key at prompt + config block | When key expires    | Key                   |
| 5 Well-known login             | `opencode auth login <url>`        | Never if long-lived | Nothing               |
| 6 Plugin OAuth device flow     | Install plugin, `/connect`, code   | Refresh token death | User code only        |
| 7 Plugin `chat.headers`        | Install plugin, first-use login    | Refresh token death | Depends on plugin     |

Lowest friction for the opencode user: option 5, with the well-known
command performing Keycloak login and returning a per-user long-lived
LiteLLM virtual key. If the identity must stay tied to live Keycloak
sessions, option 6 is next best.

## Not verified

- No end-to-end run against a live LiteLLM or Keycloak; all claims are
  from reading source and docs at `v1.19.4`.
- Whether a plugin delivered through well-known remote config loads
  before provider setup in the same start was not traced.
