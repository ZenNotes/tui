# Shared compatibility fixtures

The TUI owns its Go implementation and its release. It consumes versioned
behavior fixtures from `ZenNotes/zennotes`, not another repository's source tree.

- `internal/vault/testdata/task-roundtrip.json` covers task identity and
  exact Markdown reads and due-date writes in Los Angeles and Auckland.
- `internal/remote/testdata/self-hosted-http.json` covers authentication, note
  metadata, exact Unicode/whitespace, missing resources and invalid paths at both
  `/` and `/notes`.
- `internal/mcp/testdata/mcp-scope.json` covers scoped MCP access (#21): flag
  validation, the tools each scope lists and their annotations, every tool's
  answer for shared, hidden, symlinked, case- and accent-variant paths in root,
  inbox and remapped vaults, and what is on disk afterwards. The desktop's
  bundled MCP server runs the same cases.
- `mcp-scope.json` has `a-link-to-a-note-is-not-a-note` and its `noteLinks` vault
  from the desktop's fixture, added here by hand and held in both files. The two
  copies differ for now, because the asset cases have not reached the desktop's.
  Do not sync over this file until they match.
- `internal/vault/testdata/task-recurrence.cases.json` is the check-off table for
  recurring tasks, from `packages/shared-domain`, which the desktop runs too.
- Adjacent `.source.json` files record each upstream path and SHA-256. Tests
  verify the checked-in bytes before interpreting the contract.

`internal/mcp/testdata/mcp-scope.json` is ahead of upstream for now. It holds the
cases for the attachment tools of #34 and for `update_task`, `daily_note`,
`note_sections` and `edit_section`, which the desktop's server does not have yet,
so its checksum matches nothing in `ZenNotes/zennotes`. It also expects two things
the desktop's copy does not yet: a rewrite under a path scope writes the full path,
and `daily_note` offers no way to start a note in a hidden folder. When the desktop mirrors them, this file
moves upstream as it is. Until then, `npm run sync:contract-fixtures
-- --tui` from a zennotes checkout overwrites it and `go test ./internal/mcp` fails
on the tool counts: do not sync over it.

`go test ./...` runs all fixtures without a server checkout. To additionally
verify the real server implementation, build the API-only server binary and run:

```sh
ZENNOTES_SERVER_CONTRACT_BINARY=/absolute/path/to/server go test ./internal/remote ./internal/mcp
```

The integration tests start that binary on loopback with disposable vault and
configuration directories, verify persisted note bytes, and clean up the child
process. They do not contact a deployed server or use a real account. CI runs
them against the server's `main` branch (the `server-contract` job).

The server also runs `mcp-scope.json` through its own `/mcp` route, over real HTTP:

```sh
ZENNOTES_SERVER_MCP_CONTRACT_BINARY=/absolute/path/to/server go test ./internal/mcp -run 'ThroughHTTP|TestHTTPMCP'
```

Every call answers as it does in the server tests below, with these differences: a
full-access `vault_info` gives no absolute paths or server address, a stale deny entry is
worded for a server's settings, and a client with a path scope is not offered `rename_note`,
`read_asset`, `duplicate_asset`, `rename_asset` or `move_asset`, which would refuse anyway. The same run covers
what is particular to HTTP: tokens staying on their own side, the route being off by
default, what stops the server from starting, what reaches the log, and vault switches.
This variable is separate because only a server built against this module has the route.
ZenNotes/znserver's CI always sets it, from the `tui` tag its `go.mod` pins. Set it in
this repository's `server-contract` job as well, once the server's `main` has the route.

**This is the first code dependency between the two repositories.** `mcpserver` is this
module's only public package, and the server imports it, so:

- A change to `mcpserver`'s API needs a change in the server to match.
- A tool or scope change here reaches self-hosters only when the server pins a new tag.
- The tag has to exist before a server release can use it, so `tui` releases first.
- Anything `mcpserver` imports must stay free of the terminal UI's libraries, because
  the server's binary carries it.

`internal/mcp` runs `mcp-scope.json` behind the real server, where some calls
answer differently on purpose. `scopeServerDifferences` in
`internal/mcp/scope_server_contract_test.go` names each one and what it answers
instead, and a difference that no longer matches a fixture call fails the run:

- `rename_note`, `rename_asset` and `move_asset` under a path scope are refused,
  because a server rewrites inbound links in every note itself. The fixture's
  later calls on the file that was to be renamed answer that it is not there.
- `read_asset` under a path scope is refused, because a server that does not mark
  links could hand over a hidden file.
- `vault_info` reports `kind: remote`.

Links get more tests there, because the fixture's refusal comes from zn: the
server itself refuses a create or move through a link to a folder, a new file does
not land on a dangling link, and a note or asset the server marks `symlink` in its
listings does not exist for a client with a path scope, and neither does a system
folder that is a link. These cases are not in the public fixture yet. A server that does not
report `supportsSymlinkMarks` shares nothing with such a client, so a path scope
needs a current server and the contract cannot run against an older one.

When updating a contract, copy the upstream fixture and update its provenance
hash together. Review semantic changes as an API compatibility change; do not
regenerate expected results from the TUI implementation. Existing release clients
remain supported until a documented support window permits deprecation.
