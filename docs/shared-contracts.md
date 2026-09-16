# Shared compatibility fixtures

The TUI owns its Go implementation and its release. It consumes versioned
behavior fixtures from `ZenNotes/zennotes`, not another repository's source tree.

- `internal/vault/testdata/task-roundtrip.json` covers stable task identity and
  exact Markdown reads and due-date writes in Los Angeles and Auckland.
- `internal/remote/testdata/self-hosted-http.json` covers authentication, note
  metadata, exact Unicode/whitespace, missing resources and invalid paths at both
  `/` and `/notes`.
- Adjacent `.source.json` files record each upstream path and SHA-256. Tests
  verify the checked-in bytes before interpreting the contract.

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
