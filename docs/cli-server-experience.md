# CLI and server experience

Implementation plan approved after the v0.5.0 TUI parity release. The first
deployment target is the machine running `zn`, using a native release binary.

## Outcomes

- An interactive `zn` opens the terminal app and its first-run setup when needed.
- Every command has scoped help, validated arguments, useful errors and shell
  completion. Automation can opt out of prompts and use JSON output.
- `zn status` explains workspace selection; `zn doctor` reports actionable checks.
- `zn config list|get|set|edit` shares the TUI's portable settings implementation.
- `zn server setup <name>` installs a checksummed release, configures a vault and
  token, starts a managed instance, checks the authenticated connection and saves
  a client profile. The individual lifecycle steps are also scriptable.
- `zn server update <name>` supports checks, pinned versions and rollback, with
  health verification before accepting an upgrade.
- `zn update` supports release checks and pinned versions, with installation-owner
  detection for native downloads, Homebrew, Go and desktop-managed installations.
- CLI and TUI server operations use the same implementation. Contributors have
  a reproducible sandbox and a documented validation command.

## Implementation order and verification

1. Command metadata, help and parsing. Prove that help never creates or modifies
   notes, global flags work in either position, unknown flags fail usefully,
   repeated flags and stdin retain their documented meanings.
2. Diagnostics, settings and completion. Verify effective local/remote workspace
   reporting, redacted credentials, persisted settings, and shell syntax.
3. Shared release downloader. Verify exact asset selection, version comparisons,
   checksums, bounded downloads, interrupted transfers and archive extraction.
4. Server manager and CLI. Verify isolated native setup, idempotence, lifecycle,
   health/authentication checks, configuration changes, update and rollback.
   Linux uses systemd user services, macOS uses launchd agents. A foreground
   command supports development; Docker is an additional managed runtime.
5. CLI self-update. Verify owner-specific routes and staged native replacement,
   including failures that retain the original executable.
6. TUI server controls, first-run improvements, sandbox and documentation.
   Exercise the complete local and remote journeys in a real terminal.

## Structure and conventions

The existing Go/Bubble Tea code remains the foundation. Command definitions live
in `internal/cli`; shared configuration stays in `internal/config`. Release
retrieval and verification live in `internal/releases`, and managed server
instances in `internal/server`. Tests live beside the implementation.

Follow the existing explicit Go error handling and format changed Go files with
`gofmt`. Public note commands retain their names and successful JSON shapes.
The server binary and its runtime settings live outside the user's vault.
Downloads use the official repositories and validated release metadata. Tokens
are file-backed, private and excluded from command output. Runtime commands use
argument arrays rather than interpolated shell commands.

## Checks

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/zn
git diff --check
```

Lifecycle integration checks use disposable configuration directories and vaults.
Testing updates targets a copied executable or a disposable server instance.
Release publication and pushes require the maintainer's explicit approval.

## Implementation status

The native-first path is implemented in this branch: CLI foundation, shared
release verification, native managed instances, owner-aware CLI updates, TUI
server menus, onboarding and the contributor sandbox. Docker and SSH deployment
remain separate follow-ups; Windows uses foreground server execution and manual
standalone CLI replacement.

Verification includes the full race-enabled suite, vet, Linux/Windows builds,
local and real-server sandbox smoke checks, a real macOS launchd setup/restart/
configuration/stop journey, a copied-executable self-update to the published
v0.5.0 binary, and TUI server controls at 120×36 and 80×24. Linux server tests and
unit-file validation run in a native arm64 Debian container; a full systemd user
session lifecycle has not been exercised on this macOS host.

See [managed server usage](managed-servers.md) and [contributor setup](../CONTRIBUTING.md).
