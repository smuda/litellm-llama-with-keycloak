# Local testing

How to run the playground on the Mac and log in from opencode as
alice.

## Start the stack

1. Start llama-server on the host as described in
   [model-and-context.md](model-and-context.md#commands-and-config).
2. Create `.env` from `.env.example` and replace every `change-me`:

   ```sh
   cp .env.example .env
   ```

3. Start the services:

   ```sh
   docker compose up --build -d
   ```

Keycloak takes a minute to start. The key broker waits for it.

## Build the login helper

```sh
make build
```

This puts `login-helper` in `login-helper/bin/` and `key-broker` in
`key-broker/bin/`.

## Put the login helper on the PATH

opencode runs `login-helper` by name, as set in the served
`opencode.json`. It does not run it from your shell but from its
background service (`opencode serve --service`), which keeps the
`PATH` it was started with. An `export PATH=...` in your shell
therefore does not reach it once the service is running, and the
login fails with `NotFound: ChildProcess.spawn`.

Link the helper into a directory that is already on your login
`PATH`, such as `~/.local/bin`. From the repo root:

```sh
ln -s "$PWD/login-helper/bin/login-helper" ~/.local/bin/login-helper
```

The link follows rebuilds with `make build`.

## Log in

```sh
opencode auth login http://localhost:8090
```

The browser opens Keycloak. Log in as `alice` with `ALICE_PASSWORD`
from `.env`. The tab then says it can be closed, and opencode stores
the virtual key.
