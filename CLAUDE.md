# litellm-llama-with-keycloak

## Agent skills

### Issue tracker

Issues live in GitHub Issues for smuda/litellm-llama-with-keycloak
(`gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: needs-triage, needs-info, ready-for-agent,
ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root.
See `docs/agents/domain.md`.

### Tooling tests

Tooling self-tests (Makefile targets, hooks) live in
`scripts/*-test.sh` and run from `make test`.

### Served config

`key-broker/config/opencode.json` is served to every opencode client.
Do not change it unless asked.
