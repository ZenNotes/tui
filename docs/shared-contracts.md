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
ZENNOTES_SERVER_CONTRACT_BINARY=/absolute/path/to/server go test ./internal/remote
```

The integration test starts that binary on loopback with disposable vault and
configuration directories, verifies persisted note bytes, and cleans up its
child process. It does not contact a deployed server or use a real account.

When updating a contract, copy the upstream fixture and update its provenance
hash together. Review semantic changes as an API compatibility change; do not
regenerate expected results from the TUI implementation. Existing release clients
remain supported until a documented support window permits deprecation.
