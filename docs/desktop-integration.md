# Desktop CLI compatibility

The `zn` Go CLI and TUI expose desktop integration protocol 1 through
`zn --desktop-integration`. Desktop bundles a pinned release archive, verifies it,
and installs a persistent copy. No Node installation is needed for that copy.

## Workspace selection

Desktop-managed commands set `ZENNOTES_WORKSPACE_SOURCE=app`. They use desktop
vault/profile names, credentials and current selection. A terminal workspace with
the same name cannot redirect an existing desktop script or MCP client.

`zn tui` defaults to the separately saved terminal workspace. Explicit selectors
and `--workspace-source app|terminal` take precedence. MCP retains the first
successfully resolved workspace until its process restarts.

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
