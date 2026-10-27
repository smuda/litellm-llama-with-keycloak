# litellm-llama-with-keycloak

## Agent skills

### Issue tracker

Issues live in GitHub Issues for smuda/litellm-llama-with-keycloak
(`gh` CLI). Read an issue with
`gh issue view <n> --json title,body,labels,comments`: `--comments`
alone hides the body, and it cannot be combined with `--json`. Other
operations: `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: needs-triage, needs-info, ready-for-agent,
ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root.
See `docs/agents/domain.md`.

### Tooling tests

Tooling self-tests (Makefile targets, hooks) live in
`scripts/*-test.sh` and run from `make test`.

### Playground realm

Editing `keycloak/realm-playground.json` does not update an existing
realm. Run `make realm-reset`; `make e2e` does it before the e2e
script.

### Served config

`key-broker/config/opencode.json` is served to every opencode client.
Do not change it unless asked.

### Live state

Read live playground Keycloak or LiteLLM state with psql on the
compose `postgres` service, with the session defaulting to
read-only so an accidental write fails:

```sh
docker compose exec -T \
  -e PGOPTIONS='-c default_transaction_read_only=on' \
  postgres psql -U postgres -d keycloak   # or -d litellm
```

This needs no LiteLLM master key; keep that key out of inspection.
A read-only error means stop and report; the superuser could lift
the setting, so leaving it on is the agent's job. Select only the
columns you need: Keycloak keeps client secrets, realm keys and
password hashes in plain columns, and query output leaves the
machine.
