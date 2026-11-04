# MCP over HTTP in the self-hosted server (ZenNotes/znserver#56, ZenNotes/zennotes#965)

> Status: built, not released, on zn `4157c3c` and znserver `f261fb1`. It targets server 2.83.0 and the
> next `tui` tag, assumed to be 0.17.0, because 0.16.0 has no `mcpserver`.
> Adib chose a built-in `/mcp` over a sidecar on 2026-11-04. This describes what the code does,
> after two independent reviews on 2026-11-04 and their fixes. Open items are in sections 10 and 11.

## Summary

znserver serves `/mcp` itself, off by default, with the same tools and scope rules as `zn mcp`:

- **One implementation of the tools and scopes.** A small public package in `tui` (`mcpserver`)
  exposes one handler, and the tool and scope code stays in `internal/`. Go allows this, because
  the rule applies to the importer's path.
- **One set of vault rules.** The MCP tools reach the vault by calling the server's own API
  in-process, without the network. File modes, size limits and link rewriting are the server's,
  and every future server rule applies automatically.
- **Separate credentials.** An MCP token cannot use the REST API, and the server's token and
  browser login cannot use `/mcp`. Each MCP token carries its own scope, which is what lewxdev's
  second option asks for.

**Cost:** the server binary grows from 8.4 to 11.2 MiB (3.4 to 4.4 MiB compressed), and znserver
pins a `tui` version, so an MCP fix needs a server release.

**Limits in this version:** a path-scoped client is not offered note renames or attachment reads,
renames, moves or copies, and MCP edits are not offered for agent edit review. All of that already
applies to `zn mcp` against a server, and can be added to the server later (section 10).

## 1. The ask

- [znserver#56](https://github.com/ZenNotes/znserver/issues/56) (lewxdev) runs ZenNotes in Docker
  with OpenAI's tunnel-client and maintains a custom image to run `zn mcp`. They want an optional
  Streamable HTTP endpoint so the tunnel can stay stock and the custom build and separate CLI
  version go away. Their follow-up asks for the existing scope semantics and contract tests, a
  dedicated credential instead of the server's unrestricted token, and offers two policy shapes.
- [zennotes#965](https://github.com/ZenNotes/zennotes/issues/965) (lawtj) wants agents on several
  machines to use a self-hosted vault without installing `zn` on each, at the same time, with a
  way to turn it off.

## 2. The shape

```
ChatGPT ─▶ OpenAI ◀─poll── tunnel-client ─┐
                                          ├─ POST /mcp + MCP token ─▶ znserver
Claude Code / Codex ─▶ your TLS proxy ────┘                             │
                                                     Origin and token checks
                                                     credential picks the scope
                                                     zn's tools and scopedBackend
                                                                        │ in-process, no network
                                                     the server's own /api handlers ─▶ vault
```

**In `ZenNotes/tui`:**

- `mcpserver/mcpserver.go`, the only public package. `NewHandler(Options) (http.Handler, error)`,
  `ValidateScope(Credential)`, and `HashToken(string)`. It holds tokens only as hashes.
- `internal/mcp`: `Run` splits into `BuildServer` and running a transport, so stdio is unchanged.
  `Options.Hosted` holds what differs when the client is on another machine (section 4).
- `remote.NewClientWithTransport` and `backend.NewRemoteWithClient`, so a client can use a
  transport instead of the network.

**In `ZenNotes/znserver`:**

- `internal/config/mcp.go`: reads and refuses settings.
- `internal/httpserver/mcp.go`: `EnableMCP`, a route registered only when enabled, and the
  in-process transport, which is the only place the MCP path touches the server's token.

**How the tools reach the vault.** Three ways were weighed:

| Backend | Result |
|---------|--------|
| **Calling the server's handlers in-process (chosen)** | One implementation owns the vault. New notes get the server's `0600`, folders `0700`, `MaxNoteBytes` applies, works under a base path. |
| zn's `Local` next to the server's vault code | Renames would work, but zn writes `0644` and `0755` where the server's default is `0600` and `0700`, so notes made through MCP would be readable by more people than the owner chose. It also skips the server's limits. Two implementations would be writing one vault from one process. |
| An adapter from the 40-method `Backend` onto the server's vault | Fastest at runtime and the most code, with the most room for scope differences. |

**The in-process transport** (`mcpTransport`):

- Clears chi's route state from the context. Otherwise the server routes the call back to `/mcp` and
  answers 401, which looks like a token problem. A test covers it.
- Reads the server's token fresh on each call, so rotation just works.
- Allows only the routes the tools call, by method and path. Anything else is a 403, so a bug in a
  tool cannot switch the vault, change the token or browse the filesystem, and a route added to the
  server later stays out until it is listed.
- Holds each response in memory, up to the larger of 64 MiB and twice `MaxNoteBytes`, then answers
  413. `read_note` checks the file's size first, for a note larger than that.

## 3. Configuration

| Variable | Meaning |
|----------|---------|
| `ZENNOTES_MCP_ENABLED=1` | Turn it on. When off, `/mcp` is not routed and nothing below is read. |
| `ZENNOTES_MCP_TOKEN`, `ZENNOTES_MCP_TOKEN_FILE` | The credential for one agent. Not both. |
| `ZENNOTES_MCP_ACCESS` | `read`, `comment` or `write`. Required. |
| `ZENNOTES_MCP_ALLOW_PATHS`, `ZENNOTES_MCP_DENY_PATHS` | JSON arrays: `["Projects","Areas/Health"]`. Same rules as `--allow-path` and `--deny-path`. Blank, `[]` or `null` is refused for either: leave it out. |
| `ZENNOTES_MCP_CREDENTIALS_FILE` | Several agents, each with their own scope (section 6). Refused together with the token, access and path variables. |
| `ZENNOTES_MCP_ALLOWED_ORIGINS` | `Origin` values to accept. None by default, and `*` is refused. |

The path lists are JSON because the server's usual list format splits on commas. A folder called
`My, Folder` would become an allow entry for `My`, which could share the wrong folder.

**`ZENNOTES_MCP_ALLOWED_ORIGINS` alone does not make `/mcp` usable from a web page.** It adds no CORS
headers. A page whose origin is also in `ZENNOTES_ALLOWED_ORIGINS` gets those from the server's
middleware, and can call `/mcp` if it holds a token. Otherwise the option is for a non-browser client
that sends an `Origin`.

**The server refuses to start** with MCP enabled, like its existing refusal to run without a token
off loopback, if:

- There is no token, or it has fewer than 32 characters.
- There is no `ZENNOTES_AUTH_TOKEN`. On loopback the server otherwise runs with an open REST API,
  where a scope restricts nothing. This is new since the design, and I chose failing closed.
- There is no `ZENNOTES_VAULT_PATH`. Without it, a token could end up on another vault after a switch
  or, because the host config remembers the switch, after a restart. With it, the server refuses a
  switch at all, so nothing else has to handle one. This replaced an earlier check for a switch.
- An MCP token equals `ZENNOTES_AUTH_TOKEN`. `/session/rotate-token` refuses this too.
- Access is not stated. Full access has to be written down as `ZENNOTES_MCP_ACCESS=write`.
- A scope entry is invalid. Validation is zn's `NewScope`, with the same rules and the flag names
  replaced by the operator's words.
- The credentials file has an unknown `version`, invalid JSON, trailing data, an unknown field, a
  bad or repeated name, a bad hash, or two credentials with one token. An unknown field is an error
  because a misspelled `allow` would otherwise share the whole vault.

Errors name variables and files, never a token.

## 4. Security

| Topic | Decision | Evidence or reason |
|-------|----------|-----|
| Separate credentials | MCP token gets 401 on `/api` and the root routes, and is not a login. Server token and browser session cookie get 401 on `/mcp`. | Tested on a real server. This is lewxdev's requirement. |
| Token format | Any secret of 32+ characters. The docs say `openssl rand -hex 32`. | No format to maintain. The length cannot be checked on a credentials-file hash. |
| Storage | Environment or file. The credentials file stores `tokenSha256`, never the token. | The plain token only needs to exist with the client. |
| Comparison | SHA-256, then `subtle.ConstantTimeCompare`, checked against every credential. Bearer header only. | Hashing removes the length signal. |
| Failure | 401 with `WWW-Authenticate: Bearer`, the same answer for every wrong credential. The log counts them, at most once a minute. | Tested. No lockout or rate limit, which behind a proxy or tunnel would let anyone block everyone. A 32-character token cannot be guessed, but the server cannot check the length of a token it only has the hash of, and the README says so. The body of a request without a valid token is never read. |
| Origin | Any request with an `Origin` is 403 unless listed, even a same-origin one. | Tested. The MCP spec requires validating Origin. A browser always sends it on a cross-site POST or a rebound one, so this also covers DNS rebinding. Go's `http.CrossOriginProtection` would not: after a rebind the browser calls it same-origin. |
| Host | No allowlist. `DisableLocalhostProtection` is set. | Tested: a server bound to loopback behind a proxy passing its public Host gets 200. The SDK's default answers 403 there, which would break a common setup. Origin and the token do this job. |
| Binding | Unchanged: `ZENNOTES_BIND`, with TLS at a proxy. | Enabling MCP adds a route, not a listener. |
| Sessions | `Stateless: true`, `JSONResponse: true`. The scope is chosen on every request. | With stateful sessions, a `Projects`-only credential could read `Journal` by presenting another credential's session id. Stateless has no session to steal, and restarts lose nothing. zn makes no server-to-client requests, so nothing needs sessions. `GET` and `DELETE` get 405. |
| A hidden tool | Answered as a tool error, as the desktop engine does. | Under protocol 2026-07-28 the SDK sends HTTP 400 for an unknown tool, and its Go client then closes the connection. Tested: a stray call does not break the next one. |
| Malformed bodies | Anything but valid JSON is a 400 before the SDK sees it. So is a JSON-RPC batch, and JSON nested deeper than 64 levels. | Tested. The SDK's decoder recurses and takes time quadratic in depth. A batch ran every call in it under one slot. MCP dropped batches in 2025-06-18. |
| Panics | A panic in a tool answers that call, and is logged with its stack but not its value. | Tested. The SDK runs each call on its own goroutine, where a panic would end the server. |
| Body limit | Write credentials: the larger of `MaxNoteBytes` and the largest `upload_asset`, plus 1 MiB, so 23.4 MiB by default. Read and comment credentials: 1 MiB. | Tested: a 10 MiB note and a 16 MiB upload accepted, past the limit 413. The SDK's 4 MiB default would refuse notes the server allows. The server's own limit applies to the JSON it receives, so a note of quotes or newlines, which JSON escapes, hits it at about half the size. |
| Memory | Tool calls take turns across credentials: 4 at once, 2 under a 2 GiB limit, 1 under 800 MiB, and a call waits up to 30 s. An answer waits in memory until its client reads it, so open requests follow memory too: 32, 16 up to 2 GiB, 8 up to 800 MiB, shared between credentials. So does the largest answer: a share of the limit, at least 16 MiB, at most the larger of 64 MiB and twice the note limit, both for what the API hands a tool and for what the tool sends the client, counted as it goes on the wire, since JSON inside JSON can take four times a note of quotes. One search returns at most 1,000 matches. Each credential has a share of 64 MiB of request bodies, counting only bytes that have arrived. In a container, a soft limit of 70% of its memory is set at startup, and `GOMEMLIMIT` wins. | Proven on the real image at 512 MiB and 1 GiB, including clients that never read their answers. See section 12 for what was run. The cost: 16 concurrent reads of a 9 MiB note at 512 MiB gets some clear 503s. Reading a 400 MiB note peaks at 1.5 GB, because the listing a scoped call needs still reads it. |
| Time limits | 15 s for a read or comment credential to send its body, 60 s for a write one, then 130 s for the call and its answer, which is 10 s more than the longest a call can take, per request with `ResponseController`, since the server sets none for its websockets. | Tested: a client that stops sending or stops reading frees its slot. `net/http` clears both when they stop applying, and a slow tool is not cut off by the first. These limit the client's connection only: the SDK detaches tool calls from the request, so a tool keeps running after a client leaves, with 60 s per internal call. |
| Load | A limited number of requests open (see Memory), shared equally between credentials. A request waits 2 s for a place, then gets 503, and its body is read and dropped first, up to 32 MiB, so that the client gets the answer rather than a reset. | Tested. A wrong token never takes a place. |
| Concurrent edits | Writes take one turn, across credentials, as `zn mcp` does for one client: any tool that is not read-only, with comments, moves, trash and folder deletes among them. | Tested: 40 parallel appends from two credentials and 20 parallel comments all land, and neither a trashed note nor a deleted folder comes back. A tool reads a note and writes it back through the API, so this holds only among MCP clients: a browser or `zn` writing at the same moment can still be overwritten. |
| Links | A note or system folder that is a link is left out for every hosted client, at full access too. | Tested. A link can lead out of the vault, to a file on the server. |
| Files as pages | `/assets/raw` sends a `sandbox` CSP on everything but PDFs (#121), and scripts and wasm as plain text. | Tested. A path-scoped write token can upload a file, and the owner's browser session is what views it. In a real browser: the web client draws PNG and SVG embeds, an HTML file in a frame is cut off from the origin, and a `<script>` pointing at an uploaded `.js` is refused. |
| Logging | One line per call: credential name, tool, outcome, duration. Never headers, arguments, paths or results. The SDK logger stays nil. | Tested: after a full run, the log holds no token, note text or note path. The server's other log lines can hold paths: a failed vault operation, a deny entry that matches nothing, and the startup banner's vault. |
| Comment authors | Every comment is signed with the credential's name, and a client's `author` is ignored. | Tested. A credential cannot sign as the vault owner or as another credential. |
| Errors to clients | A 401 or 403 from inside becomes "The ZenNotes server refused this request", and a 5xx "could not do that". No internal address, API route or path on the server appears. `vault_info` has no `server`, `serverName` or `vaultPath`. | Tested with each failure. The zn advice about `zn connect` is for someone running zn. |
| Absolute paths | Always wrap in `scopedBackend`, even at full access. | Tested. `vault_info` answers the same way it does for a scoped client. |
| Instructions | zn's default text. No zn config file is read by the server process. | They belong to whoever runs zn. |
| Blast radius | The honest cost of this choice: a bug in the MCP HTTP code runs in the process holding the vault and logins. | Reduced by a separate route with separate auth, and by reaching the vault only through existing handlers, with the refused routes above. |

**What the tunnel changes.** `MCP_EXTRA_HEADERS` adds the token on the last hop only, but a header
forwarded by a connector overrides it, which here means a 401 rather than wider access. Per
OpenAI's docs, MCP payloads and results pass through OpenAI. The scope limits how much of the
vault can make that trip.

## 5. Does the scope fail closed on every tool?

**The tool layer does.** Every tool registers through one loop that checks `toolAllowed`. An
unclassified tool counts as `classUnscopedWrite`, which no path-scoped client gets, and a test
fails until it is classified. Every handler receives `scopedBackend`, whose inner backend is a
named field, so a new `Backend` method fails to compile until it is implemented.

**The contract fixture runs through the real `/mcp` route: all 25 cases pass.** The answers match
`zn mcp --server`, which `scopeServerDifferences` lists, plus one difference: a full-access
`vault_info` gives no absolute paths. The tools a path-scoped client is not offered answer as unknown, and a
difference that stops matching a fixture call fails the run. znserver's CI runs this from the `tui` tag it pins, and each scope case starts its own server
process from environment variables, so the variables are tested as well as the rules.

**Two changes to the scope rules came out of the review**, and they apply to `zn mcp` as well as
`/mcp`. The desktop's engine does not have them yet, so the shared fixture does not cover them, and
`internal/mcp/scope_test.go` does:

- **A deny entry follows a note between system folders.** A note keeps its folders when archived or
  trashed, so `deny: ["Journal"]` also hides `archive/Journal/x.md`, and an entry written with a system
  folder, in any case, hides that folder's name in the others: `INBOX/journal`, or `archive/AJ` for
  `trash/AJ/a.md`.
- **An allow entry with accents does not match a folder without them.** `cafe` still finds `Café`, as
  the fixture says, but `Café` no longer finds `Cafe`.

**One thing cannot be closed:** `create_note` with the name of a hidden note gets `name 2`, which says
something is there. Every answer other than the plain name says the same, and the plain name would
overwrite it.

**A vault switch cannot happen.** `ZENNOTES_VAULT_PATH` is required with MCP on, and with it set the
server refuses a switch. A token therefore cannot end up on another vault, after a switch or a restart.

**A scoped search can miss something.** It asks the server for at most 50,000 matches and then filters,
so a shared note can be missed when over 50,000 matching lines come before it in folders the client
cannot see.

**Single-note deny entries are weaker than folder ones,** and the README says so. One that names a
note goes by that name: when the note moves or is renamed in the vault's folders, the entry matches
nothing and the credential shares nothing, and a note given a new name on a collision, like
`secret 2.md`, is not covered.

## 6. lewxdev's two options

Both are supported.

**Option 1, one configured scope:** the environment variables in section 3.

**Option 2, named scopes bound to separate credentials:** `ZENNOTES_MCP_CREDENTIALS_FILE`.

```json
{
  "version": 1,
  "credentials": [
    { "name": "chatgpt", "tokenSha256": "…", "access": "read", "allow": ["Projects"] },
    { "name": "reviewer", "tokenSha256": "…", "access": "comment", "allow": ["Areas"], "deny": ["Areas/Health"] }
  ]
}
```

- **The credential decides the scope.** No tool argument or header can name or widen one. Tested
  with two tokens on one server.
- **Fails closed.** See section 3.
- **Separate from profiles for now.** When `.zennotes/mcp.json` ships, an entry can say
  `"profile": "chatgpt"` in place of its lists, and the server reads the file directly. Profile
  and entry restrictions then combine by intersection, as the scoped-access design already says.
- **No hot reload.** Changing or revoking a credential means a restart. A Settings screen can
  replace the file later.

## 7. Deploying it

The issue's Compose file, with its idea intact:

```yaml
services:
  zennotes:
    image: adibhanna/zennotes:2.83
    environment:
      - ZENNOTES_AUTH_TOKEN=${ZENNOTES_AUTH_TOKEN:?}
      - ZENNOTES_VAULT_PATH=/workspace   # required; a switch is refused
      - ZENNOTES_MCP_ENABLED=1
      - ZENNOTES_MCP_TOKEN=${ZENNOTES_MCP_TOKEN:?}
      - ZENNOTES_MCP_ACCESS=read
      - 'ZENNOTES_MCP_ALLOW_PATHS=["Projects"]'
    volumes:
      - /data/zennotes/vault:/workspace
      - /data/zennotes/data:/data

  zennotes-mcp:
    image: ghcr.io/openai/tunnel-client:v0.0.19
    environment:
      - CONTROL_PLANE_API_KEY=${CONTROL_PLANE_API_KEY:?}
      - CONTROL_PLANE_TUNNEL_ID=${CONTROL_PLANE_TUNNEL_ID:?}
      - MCP_SERVER_URL=http://zennotes:7878/mcp
      - "MCP_EXTRA_HEADERS=Authorization: Bearer ${ZENNOTES_MCP_TOKEN:?}"
```

Two small differences from the issue: the tunnel uses the MCP token rather than the server's, and
the `MCP_EXTRA_HEADERS` line is quoted, because without quotes the `: ` makes it invalid YAML (the
same applies to the issue's example). A tunnel on the server's network can reach the REST API, but
its token is refused there, so no separate network is needed.

**For #965:** `claude mcp add --transport http zennotes https://host/mcp --header "Authorization: Bearer <token>"`.
Under `ZENNOTES_BASE_PATH=/notes` the route is `/notes/mcp`.

**Publishing:** nothing new. The existing server image and channels carry it. Nix needs its
`vendorHash` refreshed, and its toolchain needs Go 1.26, which has to be checked (section 13).

**Versioning and release order:** znserver's `go.mod` pins a `tui` tag, like the browser artifact it
already pins. Each server release has exactly one set of MCP tools, so nothing can drift for users,
which removes lewxdev's second complaint. The price is that a zn MCP fix reaches self-hosters only
with a server release. The server's release notes should name the `tui` version.

**The order this requires:**

1. Release `tui` with `mcpserver`, and tag it (0.17.0 is assumed; 0.16.0 does not have it). The tag has
   to exist first, or the server cannot build.
2. In znserver, replace the local `replace` with that tag (and change the requirement if the version
   differs), refresh `go.sum` and `vendorHash`, and let
   its CI run the contract through `/mcp`.
3. Release server 2.83.0.
4. In `tui`, set `ZENNOTES_SERVER_MCP_CONTRACT_BINARY` in the `server-contract` job, now that the
   server's `main` has the route.

**What the dependency adds:** `go.mod` gains one direct requirement and goes from 1 to 11 indirect
ones, with no terminal UI libraries. Only ten `tui` packages end up in the binary. The `go` directive
moves from 1.25 to 1.26.0, which the Dockerfile already uses. `docs/shared-contracts.md` records this
as the first code dependency between the two repositories.

**Load:** 9,600 requests from 8 clients as fast as they could go, half of them scoped `read_note` calls
and half `tools/list`, on 1,000 notes, with tool calls taking turns, ran at about 115 requests a
second on 2 cores. Memory settled at about 160 MiB and did not grow between rounds.

## 8. What this does not solve

Claude and ChatGPT connectors differ. As of the vendors' docs on 2026-11-04:

| Client | Public HTTPS | Auth it can use | Reached by this design? |
|--------|--------------|-----------------|---------------------------|
| ChatGPT, Codex, Responses API through Secure MCP Tunnel | No, the tunnel polls outward | Static header added by tunnel-client | **Yes**, lewxdev's case |
| Claude Code, Codex, other header-capable clients | No, any URL they can reach | Bearer header | **Yes**, #965 |
| ChatGPT custom connector, direct | Yes | OAuth 2.1; OpenAI says it cannot present custom API keys | No |
| Claude custom connector | Yes, from Anthropic's IP ranges (the docs also mention MCP tunnels) | OAuth, or request headers, in beta for a limited set of organizations with names reviewed by Anthropic | Only in that beta |

For someone on a phone with an ordinary account, the parked connector still needs OAuth and public
HTTPS, and **this version adds neither.**

**This choice helps that later, and it is the main reason a built-in has the longer future.** The
server already has a login, sessions and a public URL, so an authorization server has a natural
home. The SDK's `auth` package already provides the resource-server half. `handler.serverFor` is the
one function that turns a request into a scope, so an OAuth token becomes another way to get one.
Static tokens remain valid for tunnels and CLIs. The parked plan's first choice, a hosted Cloud
connector, is untouched.

## 9. What was given up by not using a sidecar

Recorded so the trade is not lost:

- **Isolation.** A sidecar with a `:ro` mount had the operating system enforcing read-only, and no
  server token at all. Here the MCP code shares a process with everything else.
- **Independent releases.** A sidecar could ship MCP fixes on zn's schedule.
- **Scoped renames and agent edit review on day one**, which a mounted sidecar had. It would also
  have written `0644` files into a `0600` vault.

The HTTP handler stays usable outside the server. `zn mcp --listen` could wrap the same package
later if someone wants a local vault over HTTP. Nothing here needs it.

## 10. What is left

| Step | Work | Repos |
|------|------|-------|
| Before release | Independent security review | |
| Before release | The steps under "The order this requires" in section 7 | tui, znserver |
| Before release | Docs on the website and in the in-app manual; answer both issues | website, zennotes |
| Later | Scoped renames: a way for the server to limit its link rewrite to visible notes | znserver, tui |
| Later | Agent edit review in the server | znserver |
| Later | Settings screen for credentials; `profile` entries; OAuth | all |

The desktop's TypeScript engine needs no change.

## 11. Decisions made while building, for Adib to overrule

1. **Both of lewxdev's options ship together.** Said in the approved reply.
2. **The server's own token is refused on `/mcp`.** A full-access user adds one more secret, and a
   leaked MCP token can never reach the REST API. Said in the approved reply.
3. **A bad MCP configuration stops the whole server**, matching its existing refusals. These errors
   appear when someone is deploying and watching.
4. **MCP needs `ZENNOTES_VAULT_PATH`.** New since the reply, whose example already sets it. A server
   whose vault is picked in the browser cannot use MCP.
5. **MCP needs `ZENNOTES_AUTH_TOKEN`, even on loopback.** New since the design and the reply: a
   loopback server with an open REST API otherwise has scopes that restrict nothing.
6. **`ZENNOTES_MCP_ALLOWED_ORIGINS` is kept.** It could be removed.
7. **No rate limit on 401s.** Behind a proxy or tunnel every client shares an address, so a limit would
   let anyone block everyone. The log counts them instead.
8. **Tool calls take turns for everyone**, not only the heavy ones. Nearly every scoped call reads the
   whole listing, so all of them hold memory. This is simpler than a list, and it costs little: 32
   `list_notes` at once on 5,000 notes finished in about a second in a 512 MiB container, with one call
   running at a time.
9. **A long title is shortened rather than refused**, to 200 bytes on the server and in zn. This
   applies to the REST API too.

## 12. What was run on the real image

At 2 CPUs, on 5,000 notes, with a read token, against clients that send a request and never read the
answer. None of these got the container killed:

| Container | Clients | Request | Peak |
|-----------|---------|---------|------|
| 512 MiB | 24 | `search_text` with a limit of 100,000,000 | 280 MiB |
| 512 MiB | 32 | `read_note` of a 20 MiB note | 464 MiB |
| 512 MiB | 32 | `read_note` of a 10 MiB note | 428 MiB |
| 1 GiB | 32 | `search_text` with a limit of 50,000 and long lines | 647 MiB |
| 1 GiB | 32 | `search_text` with a limit of 100,000,000 | 306 MiB |
| 1 GiB | 32 | `read_note` of a 40 MiB note, the largest case tried under the 44 MiB limit at this size | 846 MiB |

A 60 MiB note at 1 GiB is refused with a 413. `/api/notes` answered in under 0.1 s throughout. At
512 MiB, 16 clients that do read, all reading a 9 MiB note at once, got 10 or 11 answers and 5 or 6 503s.

## 13. Not verified

- **The real tunnel-client**, which needs an OpenAI key and tunnel. I cannot test that hop. Its docs
  describe what this relies on. Asking lewxdev to try a build is the cheapest proof.
- **Real assistants calling tools.** The SDK's Go client and raw JSON-RPC called the tools. Claude
  Code 2.1.317's `claude mcp list` connected with an MCP token and reported a 401 with the server's
  token, which proves its handshake only. ChatGPT was not tried.
- **Windows, and Linux beyond the Docker image.** Tests ran on macOS. The image was built locally for
  one architecture, with `go.mod` pointed at a copy of `tui`, since no tag exists yet: off by default,
  the scoped calls, both token refusals, the healthcheck, a startup refusal, a 12 MiB upload and the log
  all held there.
- **Nix.** The `vendorHash` cannot be set until the `tui` tag exists, and whether the pinned nixpkgs
  has Go 1.26 has not been checked.
- **Connector facts** come from the vendors' docs on 2026-11-04 and may change.

## Sources

- [OpenAI: Secure MCP Tunnel](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels) and [openai/tunnel-client](https://github.com/openai/tunnel-client) (`docs/connectors.md`, `docs/configuration.md`, `docs/architecture.md`, `docs/deployment/docker.md`)
- [OpenAI: Apps SDK authentication](https://developers.openai.com/apps-sdk/build/auth)
- [Claude: add a connector that isn't in the directory](https://claude.com/docs/connectors/custom/remote-mcp) and [custom connectors using remote MCP](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp)
- MCP specification 2025-11-25: [transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports), [security best practices](https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices)
- go-sdk v1.7.0: `mcp/streamable.go`, `auth/auth.go`
- [Scoped access design](mcp-scoped-access.md)
