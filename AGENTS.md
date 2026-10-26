# AGENTS.md

## Layout

- Go 1.27. Two modules, `key-broker/` and `login-helper/`, joined by
  `go.work` at the repo root.
- `key-broker/opencode.json` is served to every opencode client. Do
  not change it unless asked.

## Build and test

Run in the module directory you changed, after every edit:

```sh
go build ./... && go vet ./... && go test ./...
```

Fix a failing build before making the next change. `make build`
builds both modules into `<module>/bin/`.

## Check facts, don't guess

Before you rely on a package, import path or function signature,
check it with the toolchain:

- `go list std | grep <name>` finds a standard library package and
  its import path, e.g. `log/slog`, not `slog`.
- `go doc <pkg>` and `go doc <pkg>.<Symbol>` show the real API.

If a build error says a package does not exist, check the import
path with the commands above before concluding the package is
unavailable.

## Editing

- Change call sites one at a time with edits. Never search-and-replace
  an identifier across files: replacing `log` also hits `login` and
  `logout`.
- Keep each change small and build after it. Do not revert working
  changes because a later step fails; fix the later step.
- Do not add scratch files to a module directory. A second
  `func main()` in `package main` breaks the build.
- Do not commit unless asked.
