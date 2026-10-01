# Desktop CLI compatibility

The `zn` Go CLI and TUI expose desktop integration protocol 1 through
`zn --desktop-integration`. Desktop bundles a pinned release archive, verifies it,
and installs a persistent copy. No Node installation is needed for that copy.

## Desktop-managed updates

The bundled pin is where a desktop install starts, not where it stays. ZenNotes
desktop checks once a day (and on demand from Settings > CLI > Check for updates)
for two assets of the latest ZenNotes/tui release:

```text
https://github.com/ZenNotes/tui/releases/latest/download/terminal-release.json
https://github.com/ZenNotes/tui/releases/latest/download/terminal-release.json.sig
```

`terminal-release.json` has the same schema as the desktop's bundled pin
(`apps/desktop/terminal-release.json` in the desktop repository): the repository,
the integration protocol, the version, the source commit, and a URL and SHA-256
for each of `darwin-arm64`, `darwin-x64`, `linux-arm64` and `linux-x64`. It is
2-space indented JSON ending in one newline, byte for byte what
`JSON.stringify(value, null, 2) + "\n"` writes, so a desktop release can adopt a
verified manifest as its new pin unchanged.

The release workflow writes both files with `scripts/releasemanifest` after
GoReleaser. It checks every archive against `checksums.txt` and takes `protocol`
from the released Linux x64 binary's own `zn --desktop-integration` output, which
must also report the tagged version. The signature file is:

```json
{
  "keyId": "zn-release-1",
  "algorithm": "ed25519",
  "signature": "<base64 of the 64-byte Ed25519 signature>"
}
```

The signature covers the exact bytes of `terminal-release.json`. Desktop ships
the public key for every key id it trusts; this repository keeps the matching copy
at `packaging/release-signing/zn-release-1.pub` (base64 of the raw 32-byte key),
which the workflow verifies against before uploading. A manifest with a missing,
unknown or invalid signature is ignored, and so is a release without these assets.

Desktop installs a release only when its version is newer than the managed CLI
and its `protocol` is one that desktop build supports, after the platform archive
matches the manifest's SHA-256. The new version is installed next to the current
one, and the previous version is kept so desktop can roll back to it. Desktop
never downgrades and never touches a Homebrew, Go or manually installed `zn`.
For a desktop-managed executable, `zn update` refuses to replace it and points to
Settings > CLI > Check for updates; `zn update --check` still reports the latest
release.

A protocol change still needs a desktop release. Desktop builds that do not
support the new protocol keep their current CLI, so ship the desktop update that
speaks it before relying on the new CLI. Rotating the signing key works the same
way: the new public key must reach users in a desktop release before any CLI
release is signed with it.

## Workspace selection

Desktop-managed commands set `ZENNOTES_WORKSPACE_SOURCE=app`. They use desktop
vault/profile names, credentials and current selection. A terminal workspace with
the same name cannot redirect an existing desktop script or MCP client.

The desktop keeps server tokens in the OS secret store, which zn cannot read. When
neither `--token`, `ZENNOTES_REMOTE_TOKEN` nor the desktop profile supplies one, app
mode uses the token `zn connect` saved for the same server URL. The lookup is keyed
by the URL the desktop selected, so it authenticates that server and never selects
a different one.

`zn tui` defaults to the separately saved terminal workspace. Explicit selectors
and `--workspace-source app|terminal` take precedence. MCP resolves the workspace
again before every tool call and follows a desktop switch once `vault_info`
confirms it. Until then every other tool stops with a message naming the old and
new workspace, so a switch never redirects in-flight edits.

## Creation dates

Linux reads filesystem birth time using `statx` where supported; ctime is only a
fallback. Before replacing an existing note atomically, the CLI stores its original
creation date in `.zennotes/note-metadata/<note-path>.metadata.json`:

```json
{"version":1,"createdAt":1600000000123}
```

`createdAt` is a positive integer Unix timestamp in milliseconds, at most
8640000000000000. Updated desktop builds and the CLI prefer this value over native
filesystem time. The file contains no note text, and Markdown bytes remain exact.

The sidecar is committed before note replacement. An unreadable, corrupt or
unsupported sidecar stops the save before changing the note. Read-only views fall
back to filesystem dates so damaged metadata cannot hide Markdown. Renaming or
moving a note or folder also moves its metadata; an occupied destination blocks
the move. Permanent deletion removes it. New notes and duplicates get new dates.

Keep the `.zennotes` directory with the vault when copying or backing it up.
Creation metadata is supported by Go 0.2.0 and the corresponding desktop update.
Older clients still use filesystem dates and do not maintain these sidecars when
moving or deleting files. Moves outside supported ZenNotes clients must also move
the matching metadata if the logical date should be retained. Cloud support needs
the matching client and server path-allowlist update before the desktop release.

This does not make simultaneous content edits mergeable. Atomic replacement
prevents truncated reads; competing complete saves still use the last writer.

## Rollback

Desktop retains its Node CLI during the first migration release. Set
`ZENNOTES_CLI_ENGINE=legacy` before invocation for an explicit rollback. Failed Go
commands are never retried by a second engine. The legacy path requires the app's
original Electron/resources location to remain available; AppImage extraction
paths disappear when the app exits. The persistent Go command does not have that
lifetime restriction.
