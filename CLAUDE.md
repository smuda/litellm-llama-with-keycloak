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
