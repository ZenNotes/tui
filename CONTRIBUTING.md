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

## Releases and signing

A `v*` tag push runs [the release workflow](.github/workflows/release.yml).
GoReleaser publishes the archives and `checksums.txt`; then
`go run ./scripts/releasemanifest build` writes `terminal-release.json`, signs it
into `terminal-release.json.sig`, verifies the pair against
`packaging/release-signing/zn-release-1.pub` and uploads both to the release.
ZenNotes desktop installs CLI updates only from a manifest signed by a key it
ships with ([desktop integration](docs/desktop-integration.md#desktop-managed-updates)).

The workflow needs the `ZN_RELEASE_SIGNING_KEY` repository secret, the base64
Ed25519 seed that `keygen` writes. Generate it outside the checkout:

```sh
go run ./scripts/releasemanifest keygen -out ~/zn-release-1.key
```

The file is created with mode 0600 and never overwritten. `keygen` prints only the
key id and the public key: store the file's single line as the secret, save the
printed public key as `packaging/release-signing/zn-release-1.pub`, and ship the
same public key in ZenNotes desktop.

Back the seed up somewhere safe and offline. If it is lost, desktop builds cannot
verify new CLI releases (they keep their current CLI) until a desktop release
ships a new public key. A leaked seed needs the same rotation: create
`zn-release-2` with `-key-id`, ship its public key in a desktop release first,
then replace the secret and point the workflow's `-key-id` and
`-public-key-file` at the new key.

To rehearse locally, build a snapshot and run the manifest step without a key;
it writes an unsigned manifest and warns:

```sh
goreleaser release --snapshot --clean
go run ./scripts/releasemanifest build -dist dist -tag v<snapshot version> -commit "$(git rev-parse HEAD)"
```

## Shared contracts

The desktop protocol remains `1`. Preserve note command names, successful JSON
shapes and stable task IDs. Read [desktop integration](docs/desktop-integration.md)
and [shared contracts](docs/shared-contracts.md) before changing workspace selection.
Desktop runtime configuration is read-only; terminal profiles and credentials
have separate files. CLI and TUI server management share `internal/server`, and
both updaters share `internal/releases`.
