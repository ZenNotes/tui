# Managed servers and CLI updates

These commands are available in **zn v0.6.0 and later**. Upgrade older copies
through their original installation method; v0.5.0 does not have `zn update`.

## Install a server on this machine

```sh
zn server setup home --vault ~/Notes
zn server status home
zn list --server home
```

Setup downloads the official `ZenNotes/znserver` release for this platform,
verifies its SHA-256 digest, generates a private token, installs a login service,
and checks health, version, authentication and the exact vault path. Only then
does it save the terminal client profile and make it the default. Repeating
setup preserves the installed version and token. A conflicting configuration
requires an explicit `server config` command.

The default listener is `127.0.0.1:7878`. To pick a different port or URL prefix:

```sh
zn server setup work --vault ~/WorkNotes --bind 127.0.0.1:7879 --base-path /notes
```

Use `--version 2.56.0` to select a stable release, `--no-default` to retain your
current terminal default, or `--no-start` to install without starting/registering.
`zn server install` is the separate install-only step. Values are explicit, so
`--no-input --json` is suitable for automation.

macOS uses a launchd user agent; Linux uses a systemd user service. These run in
your user session without root privileges. A Linux server that should survive
logout requires user lingering, configured separately by the machine's owner.
The first adapter is native/local: SSH deployment, Docker management and Windows
background services are not implemented. Windows amd64 can use `server install`
and `server run`; a release must exist for the selected platform.

## Lifecycle and configuration

```sh
zn server list --json
zn server start home
zn server stop home
zn server restart home
zn server logs home
zn server config home --json
zn server config home --bind 127.0.0.1:7879
zn server config home --base-path /notes
```

Stop disables the login service as well as stopping the process. Start installs
it again. Logs prints up to the last 64 KiB. Configure accepts `--vault`, `--bind`
and `--base-path`; it checks the new configuration and restores the old one if
the check fails. A stopped server is briefly started for verification and then
stopped again. Changing its URL updates saved terminal profiles for the old URL.

For development, stop the service and use `zn server run home`. Ctrl-C stops the
foreground process. The instance is locked against concurrent lifecycle changes
while this command runs.

Inside the TUI, use `:servers` or **Manage local servers…** in the command palette.
It offers setup, status, connect, start/stop/restart, logs, update checks, version
selection, rollback, and listen-address changes. Long operations run in the
background; their status panel can be dismissed while you continue editing.
`:server` continues to mean connecting to an existing remote vault.

## Update and roll back a server

```sh
zn server update home --check
zn server update home
zn server update home --version 2.56.0
zn server update home --rollback
```

A release is downloaded and verified before the running service is stopped.
The candidate must report the expected version, accept its token, reject an
unauthenticated vault request, and serve the configured vault. A failed check
restores the previous configuration and service. Successful updates retain the
previous executable and its digest; explicit rollback checks that digest again.
Updates preserve notes, tokens and whether the service was running.

`--check` never changes the installation. The default update selects the latest
stable release and never downgrades; an explicit `--version` can select an older
release. `--rollback` cannot be combined with `--version` or `--check`.

Runtime files live under `servers/<name>` in the ZenNotes user-data directory
(or `$ZENNOTES_CONFIG_DIR/servers` when overridden): `instance.json`, `server.json`,
`token`, `server.log`, and `versions/<version>/zennotes-server`. Tokens are mode
0600 and never included in status/config output. This directory and the client
credential file must stay outside the served vault.

If a process is forcibly terminated while holding an instance `.lock`, first
ensure no setup/update/foreground command is still running, then remove that
specific lock and retry. Version switching is verified before the manifest is
accepted; `server restart` reapplies the saved manifest after an interrupted
operation. Automatic rollback covers ordinary failures and cancellation, not
power-loss recovery or vault-format migrations performed by a server release.

## Update the CLI

```sh
zn update --check --json
zn update
zn update --version 0.6.0
```

`zn update` checks the running executable's installation owner:

| Owner | Update path |
| --- | --- |
| Homebrew | Runs `brew upgrade zn`. Homebrew chooses the packaged version; pinning through `--version` is rejected. |
| Go-installed module | Runs `go install github.com/ZenNotes/tui/cmd/zn@v<version>` into the existing executable directory. |
| Desktop-managed | Directs you to update ZenNotes desktop, which owns the CLI runtime. |
| Standalone macOS/Linux release | Verifies the archive, extracts only the expected executable, probes version/protocol, retains `zn.previous`, then atomically replaces `zn`. |
| Windows standalone / other package managers | Provides the release or owner-specific update instruction. |

Native self-update requires a writable installation directory. Development builds
should be rebuilt from source. Homebrew/Go command output goes to stderr so JSON
results stay usable by scripts. A successful managed update also verifies the
installed version; a newer GitHub release may not yet be packaged in Homebrew.

Downloads are limited in size and use the official repositories. Verification
uses GitHub's SHA-256 asset digest, falling back to the release's exact checksum
entry when a digest is unavailable. `GH_TOKEN` can authenticate GitHub metadata
requests when the anonymous rate limit is exhausted.
