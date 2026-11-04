# Shared compatibility fixtures

The TUI owns its Go implementation and its release. It consumes versioned
behavior fixtures from `ZenNotes/zennotes`, not another repository's source tree.

- `internal/vault/testdata/task-roundtrip.json` covers stable task identity and
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
- Adjacent `.source.json` files record each upstream path and SHA-256. Tests
  verify the checked-in bytes before interpreting the contract.

`internal/mcp/testdata/mcp-scope.json` is ahead of upstream for now. It holds the
cases for the attachment tools of #34 and for `update_task`, `daily_note`,
`note_sections` and `edit_section`, which the desktop's server does not have yet,
so its checksum matches nothing in `ZenNotes/zennotes`. When the desktop mirrors
them, this file moves upstream as it is. Until then, `npm run sync:contract-fixtures
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
