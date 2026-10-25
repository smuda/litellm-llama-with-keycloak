# litellm-llama-with-keycloak

A playground where each Keycloak user reaches a local llama.cpp model
from opencode through LiteLLM, under their own LiteLLM identity.
Every login mints a per-user LiteLLM virtual key with a 14 day
duration, so LiteLLM tracks usage per person instead of per machine.

## How it works

opencode authenticates against the key broker at
`http://localhost:4000/broker`:

1. `opencode auth login http://localhost:4000/broker` fetches
   `/.well-known/opencode` from the key broker and runs the
   `login-helper` command it names.
2. `login-helper` generates a PKCE verifier, starts a listener on
   `127.0.0.1`, and opens the browser at
   `/login?port=...&challenge=...`.
3. The key broker starts an OIDC authorization code flow with
   Keycloak (realm `playground`). The user logs in as `alice` or `bob`.
4. Keycloak redirects back to the key broker's `/callback`. It
   exchanges the code, verifies the id token, creates the LiteLLM
   user if it does not exist, and mints a 14 day virtual key.
5. The key broker redirects to the helper's local listener with a
   one-time code. The helper proves it owns the PKCE verifier via
   `POST /redeem` and prints the key to stdout, where opencode
   stores it as `LITELLM_API_KEY`.
6. opencode then calls LiteLLM at `http://localhost:4000/v1` with
   the model name `medium`; LiteLLM forwards each request to
   llama-server on the host at `http://host.docker.internal:9931`.
7. On every start opencode fetches the key broker's
   `/remote-config` with the stored key. With fewer than 7 days
   left, or an unknown key, the broker answers with an HTML page and
   opencode refuses to start with a login error; re-running
   `opencode auth login` mints a new key.

## Components

- llama-server (host): serves Qwen3-4B (Qwen3.8-27B in production); see
  [docs/model-and-context.md](docs/model-and-context.md).
- keycloak (Docker, 26.6, dev mode): identity provider, realm
  `playground`, users `alice` and `bob`.
- nginx (Docker, 1.28): the one entry point on port 4000; serves
  LiteLLM at `/` and the key broker at `/broker`.
- litellm (Docker, v1.106.2): maps the model name `medium` to
  llama-server and tracks per-user keys.
- key-broker (Docker, Go): OIDC login, mints virtual keys, serves
  the opencode well-known file.
- login-helper (host, Go): runs the browser login locally and
  redeems the key for opencode.
- postgres (Docker, 17): the `keycloak` and `litellm` databases.

Host ports (all bound to `127.0.0.1`): 4000 nginx (LiteLLM at `/`,
key broker at `/broker`), 8080 Keycloak, 9931 llama-server. Postgres,
LiteLLM and the key broker stay on the compose network.

## Quick start

Full step-by-step instructions with the PATH gotcha are in
[docs/local-testing.md](docs/local-testing.md). In short:

1. Start llama-server on the host (port 9931). Model, quantisation,
   and context size are covered in
   [docs/model-and-context.md](docs/model-and-context.md).
2. `cp .env.example .env` and replace every `change-me`.
3. `docker compose up --build -d`
4. `make build`
5. Link `login-helper/bin/login-helper` into a directory on your
   login `PATH` (opencode's service does not see shell exports).
6. `opencode auth login http://localhost:4000/broker`, log in as `alice`
   with `ALICE_PASSWORD` (or `bob` with `BOB_PASSWORD`) from `.env`.

## Repository layout

- `key-broker/` — Go service: OIDC flow, key minting, `/redeem`,
  `/remote-config` and the served `opencode.json`.
- `login-helper/` — Go program opencode runs on the developer's
  machine.
- `litellm/config.yaml` — LiteLLM `model_list` and settings.
- `keycloak/realm-playground.json` — realm, `alice`, `bob`, and the
  `key-broker` confidential client.
- `nginx/` — nginx config: shared locations, playground and
  production servers, `/metrics` access rules.
- `postgres/initdb.sh` — creates the two databases.
- `docs/` — local testing, model and context choice, spend
  ([docs/spend.md](docs/spend.md)), and agent conventions
  (`docs/agents/`).
- `GLOSSARY.md` — the project's language: virtual key, login
  helper, key broker, spend.
- `go.work` — workspace for the two Go modules.
- `Makefile` — builds both Go modules.
- `docker-compose.yml` — postgres, keycloak, litellm, key-broker and
  nginx.
- `docker-compose.prod.yml` — production override: no Keycloak, nginx
  with TLS on 443, production URLs.
- `.env.example` — the secrets `.env` must define; `.env` itself is
  gitignored.

## Production

Run with the override:

```sh
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

- Replace the `llm.example.internal` and `keycloak.example.internal`
  placeholders in `docker-compose.prod.yml` and
  `key-broker/opencode.production.json`.
- Replace the placeholder scraper IP in
  `nginx/metrics-access.production.conf`.
- Put the certificate and key in a directory as `tls.crt` and
  `tls.key` and point `TLS_DIR` at it.
- Register `https://<host>/broker/callback` as a redirect URI in the
  production Keycloak.
- Set `NODE_USE_SYSTEM_CA=1` so opencode trusts the internal CA. Not
  yet verified against a real host.

`make e2e` checks the playground stack and the production nginx
config.

## Development

- `make build` builds both Go modules, into `key-broker/bin/` and
  `login-helper/bin/`.
- `cd key-broker && go test ./...` and
  `cd login-helper && go test ./...` run the unit tests.
- `docker compose up --build -d` / `docker compose down` manage the
  stack. Keycloak takes a minute to start; the key broker waits for
  it.

## Known limitations

- Login state and one-time codes live in the key broker's memory, so
  a broker restart mid-login fails that login. Each works once and
  expires after 5 minutes.
- Each login mints a new key. Older keys are not revoked; they
  expire.
- The key is minted when the login helper redeems its code. If the
  helper gives up mid-redeem (its 30 s timeout or Ctrl-C) after
  LiteLLM has created the key, nobody receives that key and it stays
  valid until it expires.
