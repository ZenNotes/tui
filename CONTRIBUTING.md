# Contributing

Use Go 1.26 or newer. Start with an isolated workspace:

```sh
bash scripts/sandbox.sh
bash scripts/sandbox.sh --smoke
```

The sandbox builds this checkout, creates sample notes and tasks in a temporary
vault, isolates the configuration and credentials, and removes everything on
exit. It does not use your desktop workspace or normal notes. Exit the TUI with
`:qa`.

To exercise the same commands against a real server:

```sh
bash scripts/sandbox.sh --server --smoke
bash scripts/sandbox.sh --server
```

This downloads a verified native server release and runs it in the foreground
under the sandbox. It creates no login service. Set `ZN_SANDBOX_BIND` to a free
loopback address/port if `127.0.0.1:17878` is occupied. The remote sandbox requires
network access; ordinary unit tests use local fixtures.

## Validate a change

```sh
go test -race ./...
go vet ./...
go build ./cmd/zn
git diff --check
bash scripts/sandbox.sh --smoke
```

For server/update work, also run the server sandbox. Unit tests cover failed
checksums, archive extraction, authentication and vault identity, retained
versions, failed-upgrade rollback, installation ownership and failed CLI probes.
Actual launchd/systemd behavior needs a disposable instance on the corresponding
OS. Cross-compilation alone does not exercise the service manager.

Use copied executables for self-update tests. Releases and Homebrew installation
files must not be replaced as a side effect of ordinary test commands.

## Shared contracts

The desktop protocol remains `1`. Preserve note command names, successful JSON
shapes and stable task IDs. Read [desktop integration](docs/desktop-integration.md)
and [shared contracts](docs/shared-contracts.md) before changing workspace selection.
Desktop runtime configuration is read-only; terminal profiles and credentials
have separate files. CLI and TUI server management share `internal/server`, and
both updaters share `internal/releases`.
