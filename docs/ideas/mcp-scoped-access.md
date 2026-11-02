# Scoped access for the MCP servers (ZenNotes/tui#21)

> Status: design accepted 2026-10-29 (section 12). zn 0.13.0 (2026-10-30) ships Phase 0
> (ZenNotes/tui#23) and Phase 1a (Go, flags only) with the link fix and the shared contract
> test; section 13 records where 1a differs from this design. Phase 1b (the desktop's bundled
> server, the shared fixture) is 7be9e993 in ZenNotes/zennotes, landing in desktop 2.80.0;
> section 14 records it and a fix it made to both engines. Phases 2 to 4 are not started.
> Written 2026-10-29 for
> [ZenNotes/tui#21](https://github.com/ZenNotes/tui/issues/21) (lewxdev). Covers both MCP
> engines: the Go `zn mcp` in this repo and the TypeScript server the desktop app bundles
> (`apps/desktop/src/mcp` in ZenNotes/zennotes). Decisions are at the end.

## Summary

- **One idea, both engines.** A *scope* is an access level (`read`, `comment`, `write`) plus
  folder lists (`allow`, `deny`). It is enforced in the server, in a wrapper around the
  vault backend that every tool already goes through, so no tool can skip it.
- **Default-deny.** A non-empty `allow` list shows only what it names; everything else does
  not exist for the client. `deny` wins inside allowed folders. Errors for hidden paths are
  byte-identical to errors for missing ones.
- **How a client gets one.** Ad hoc flags (`zn mcp --allow-path Projects --read-only`, the
  issue's shape) or a named profile (`zn mcp --profile chatgpt`) defined per vault in
  `.zennotes/mcp.json`. Settings → MCP writes `--profile` into each client's launch config.
  Flags and a profile combine by intersection; nothing can widen what the other allows.
- **Fail closed everywhere.** Unknown profile, unreadable file, a hidden folder that no
  longer exists, a server too old to serve profiles: the client sees nothing, with a message
  that says how to fix it.
- **Fix the floor first (Phase 0).** Probing both engines found that today's tools can move
  `.zennotes/vault.json` into a note folder and read it, plant notes inside `.zennotes/`,
  delete the Archive or attachments folder through an inbox subpath, and (desktop engine
  only) follow a symlink out of the vault. A scope built on top of that is not a boundary.
  These are worth fixing on their own, before #21.

## 1. The ask

From the issue: `zn mcp` exposes the whole selected vault. The reporter wants path-based
restrictions (`--allow-path /projects`), a read-only mode with no create/modify/delete tools,
default-deny once restrictions are set, the same restrictions on every tool (search, listing,
note references) so excluded content cannot be discovered indirectly, and enforcement in the
server rather than in client permissions.

Users reach two servers with the same 34 tools: the Go engine (`zn mcp`, this repo,
`internal/mcp`) and the desktop's bundled engine (`out/main/mcp.js` run with
`ELECTRON_RUN_AS_NODE`, or the desktop's own TS `zn mcp`). Settings → MCP → Install picks one
per client (`getMcpServerRuntime` in `apps/desktop/src/main/mcp-integrations.ts`). A design
that only covers one engine leaves the other as an unscoped side door, so this covers both
with one config shape.

## 2. What the servers do today

### 2.1 Call path

Both engines have the same shape:

```
client ── tools/call ──▶ dispatcher ──▶ VaultSession.backendFor()  (re-resolves the vault every call,
                                         │                           blocks after a switch until vault_info)
                                         ▼
                              handler(args, backend)  ──▶ Backend (Local | Remote)
                                         │
                              runRecording (agent edit review) for text edits
```

- Go: `internal/mcp/server.go` (`tools()`, `Run`), `session.go`, `agentedits.go`;
  `internal/backend` (`Local`, `Remote`); `internal/vault` (path safety in `paths.go`).
- TS: `apps/desktop/src/mcp/server.ts` (`TOOLS`, `VaultSession`, `runMcpServer`),
  `vault-ops.ts`, `apps/desktop/src/cli/backend.ts` (`VaultBackend`, local and remote).

Every handler receives a backend and does its own filtering on top of `ListNotes`,
`SearchText`, `ScanTasks` and friends. That is the seam: a scoped backend decorator that
implements the same interface is the one place enforcement can live without touching 34
handlers, plus a gate in the dispatcher for the access level.

### 2.2 The 34 tools

Reads: `vault_info`, `list_notes`, `list_folders`, `list_assets`, `read_note`, `search_text`,
`search_by_title`, `search_by_tag`, `list_tags`, `backlinks`, `list_tasks`, `list_comments`.
Comments: `add_comment`, `reply_to_comment`, `resolve_comment`.
Note writes: `write_note`, `append_to_note`, `prepend_to_note`, `insert_at_line`,
`replace_in_note`, `toggle_task`, `create_note`, `rename_note`, `move_note`, `duplicate_note`,
`archive_note`, `unarchive_note`, `move_to_trash`, `restore_from_trash`, `delete_note`.
Folder writes: `create_folder`, `rename_folder`, `delete_folder`, `empty_trash`.

Neither engine sets MCP tool annotations (`readOnlyHint`, `destructiveHint`) today.

### 2.3 Verified gaps (probed 2026-10-29)

Both engines were driven over stdio JSON-RPC against scratch vaults in root mode: the Go
binary built from this repo's `main` (4c4237b) and the desktop's built
`apps/desktop/out/main/mcp.js` (Oct 26 build; `vault-ops.ts` last changed Oct 26). Scripts and
transcripts are in the shared notes folder (`mcp-scoped-access/`).

| # | Call | Go `zn mcp` | Desktop engine |
|---|------|-------------|----------------|
| G1 | `move_note {path: ".zennotes/vault.json", targetFolder: "inbox", targetSubpath: "Projects"}` then `read_note` | moved and readable | moved and readable |
| G2 | `create_note {folder: "inbox", subpath: ".zennotes", ...}` | `.zennotes/planted.md` written | same |
| G3 | `delete_folder {folder: "inbox", subpath: "attachements"}` / `subpath: "archive"` | both deleted | both deleted |
| G4 | `rename_folder {folder: "inbox", oldSubpath: ".zennotes", newSubpath: "Projects/internals"}` | internal dir moved into notes | same |
| G5 | `read_note` through `Projects/link -> <outside the vault>` | refused (`SafeJoin` resolves links) | **file outside the vault returned** (`resolveSafe` is lexical) |
| G6 | `read_note` through `Projects/inner -> ../Journal` | Journal note returned | Journal note returned |
| G7 | `read_note {path: "journal/DIARY.md"}` on APFS | `Journal/Diary.md` returned | (same FS behavior) |
| G8 | `read_note` of a missing note | error carries the absolute path | same |

Causes: `MoveNote`/`moveNote` never classify the *source*; subpaths are only checked against
escaping the bucket root, and in root mode the inbox bucket root is the vault root, so every
hidden and system directory is a "subpath of inbox"; the TS `resolveSafe` has no realpath step.
Also in passing: the desktop engine's `renameFolder` does not rewrite vault.json's
path-keyed lists (`tasks.excludedFolders`, folder icons and colors), which desktop main,
app-core, znserver and Go zn all do; and `read_note` reads any file in a note folder, not
only `.md`.

G1 to G4 matter for #21 directly: if profiles live in the vault, a write-capable client must
not be able to reach the file that defines them. G5 to G7 are the classic scope bypasses (the
reference filesystem MCP server's CVE-2025-53109 was this class) and dictate how the scope
check canonicalizes paths. G8 is the existence oracle a scope must close.

## 3. Threat model

Protects against:
- An MCP-only assistant (Claude Desktop chat, ChatGPT through a bridge, any client without a
  shell) reading, listing, searching or inferring notes outside what the user shared.
- Such an assistant writing outside its scope, or writing at all when read-only.
- Prompt injection that steers a scoped assistant toward hidden content: the content is not
  reachable, whatever the model is told.
- A scope silently applying to the wrong vault after the app switches vaults.

Does not protect against:
- Clients that also have a shell or filesystem tools (Claude Code, Codex, OpenCode). A scope
  governs MCP tools only; such a client can read the files directly. The docs must say so
  plainly, and Settings should say it on those client rows.
- Anything running as the user (other processes, a full-access MCP client, sync tools) or
  hard links the user created.
- Timing side channels.

## 4. Design

### 4.1 Scope model

A **scope** is:

| Field | Meaning |
|-------|---------|
| `access` | `read` (read tools), `comment` (read + the three comment tools), `write` (everything the paths allow). A profile that omits it gets `read`. |
| `allow` | Vault-relative folder or note paths. Empty means the whole vault. |
| `deny` | Vault-relative folder or note paths, hidden even inside `allow`. |

A path is **visible** when all of these hold:

1. It is a note (`.md`) or folder inside a note bucket, classified through the vault's
   `systemFolderPaths` (`FolderForRelativePath` / `folderOf`). `.zennotes`, dot entries, the
   attachments directory and `.base` internals are never visible, whatever the lists say.
2. `allow` is empty, or the path equals an `allow` entry or lies under one (segment prefix:
   `Projects` covers `Projects/x.md`, never `Projects2/x.md`). This is the rule
   `isPathExcludedFromTasks` already uses for the Tasks folder lists (#458, #962).
3. It is not equal to or under a `deny` entry. Deny additionally matches after NFC and case
   folding, on purpose: on a case-insensitive volume a near-miss must over-hide, never leak.
4. Every component on the way is a real directory, not a symlink (see 4.4).

**Structural ancestors.** The folders above an `allow` entry (`Areas` for `Areas/Health`)
are listed by name so the tree makes sense, but nothing directly inside them is visible and
nothing can be created there. Their names are part of the entry the user wrote, so they leak
nothing.

**Paths are physical,** exactly as the tools return them (`inbox/Projects` in inbox mode,
`Projects` in root mode, `99 Archive/Projects` when Archive is remapped). This is the
tasks-lists rule ("vault-relative directory paths exactly as they exist on disk, so remapped
system folders need no translation"). The scope evaluator never joins a literal folder name;
classification goes through `systemFolderPaths` like everything else.

**System buckets.** Archive and Trash keep the note's subpath (`inbox/Projects/X.md` archives
to `archive/Projects/X.md`), so a user who wants archived project notes visible adds
`archive/Projects`, and `trash/Projects` for trashed ones. Nothing is implied.

**No globs in v1.** Folder and note entries cover the issue, match the app's model, stay
rename-rewritable, and keep Go and TS matching trivially identical. Globs are an open question.

### 4.2 Where scopes come from

```
                  ┌───────────── client launch config (args/env), written by Settings → MCP ─────────────┐
                  │  zn mcp --profile chatgpt  [--allow-path P]... [--deny-path P]... [--read-only|--access A] │
                  └──────────────────────────────────────────────────────────────────────────────────────┘
                                 │ profile name                         │ inline restrictions
                                 ▼                                      ▼
        <vault>/.zennotes/mcp.json ── profiles.chatgpt ──▶  effective scope = profile ∧ flags
        (per vault; remote: served by znserver)
```

- **No `--profile` and no flags:** unscoped, exactly today's behavior. Nothing changes for
  anyone who does nothing.
- **Flags only** (`--allow-path`, `--deny-path`, `--read-only`, `--access`): an inline scope,
  the issue's exact ask, works against any vault and any server version with no stored config.
  A leading slash is accepted and stripped (`/projects` → `projects`).
- **`--profile NAME`** (or `ZENNOTES_MCP_PROFILE`, for the legacy `mcp.js` entry and clients
  that only pass env): the profile of that name in the *current* vault's `.zennotes/mcp.json`.
- **Both:** a path is visible only if every scope says so, and access is the lowest of them.
  Restrictions compose by intersection; no input can widen another. That is also how
  `--read-only` on top of a write profile behaves.
- **Never from tool arguments.** No tool takes a scope or profile; `list_notes`' `folder` and
  `subpath` only narrow inside it. Same rule as the bridge trust model: the authority comes
  from the process's own state, never from the caller.

**Why a file in the vault.** Entries are vault-relative, so they belong to a vault, not to a
user: the MCP follows the app across vaults (#688, #830), and a `Projects` allowlist meant
for the personal vault must not quietly apply to the work vault. Per-vault profiles make the
switch safe by construction: the session already stops after a switch until `vault_info`
confirms (`session.go` / `VaultSession`), and the new vault either defines `chatgpt` or the
client sees nothing. They also travel with the vault to other machines and to a self-hosted
server, which a future server-side `/mcp` can enforce natively.

**Why its own file, not vault.json.** znserver's `VaultSettings` is a typed struct, and the
round-trip rule (#446, #379) says any key not modeled there is dropped when a web client
saves settings. Desktop main, the phones' vault-core and older versions of all of them write
vault.json too. A `deny` list that an older client drops fails *open*. `.zennotes/mcp.json`
is written only by MCP-aware code (Settings → MCP, `zn mcp profile`), so no settings save
elsewhere can lose it, and no phone app needs a change.

**Why not config.toml.** config.toml is the user's portable prefs and stays that
(`[mcp] review_agent_edits` already lives there). Vault-relative path lists in a user-level
file would need per-vault keys that do not survive moving a vault between machines, and no
host could rewrite them on a folder rename (znserver cannot touch it). Ad hoc flags already
cover "a scope not tied to a vault".

**Rejected:** selecting a profile from the client's `initialize` name (client-asserted,
spoofable, and unrelated to what the user configured); an implicit `default` profile for
clients that pass none (a lost or unsynced `mcp.json` would then silently widen every
client); MCP roots as the mechanism (see 6).

### 4.3 Config shape

`.zennotes/mcp.json` (pretty-printed, written atomically like vault.json):

```json
{
  "version": 1,
  "profiles": {
    "chatgpt": {
      "label": "ChatGPT",
      "access": "read",
      "allow": ["Projects", "Areas/Health/Plan.md"],
      "deny": ["Projects/Clients"]
    },
    "reviewer": {
      "label": "Claude (review only)",
      "access": "comment",
      "deny": ["Journal"]
    }
  }
}
```

Rules, normative in the shared module (5.1):
- Profile names: `[a-z0-9][a-z0-9_-]{0,31}`, compared lowercase.
- Entries validated by `normalizeTasksExcludedFolder` (backslashes to slashes, trimmed
  segments, no `.`/`..`, at most 512 characters), deduplicated, order kept. An entry with a
  dot-prefixed segment (`.zennotes`, any hidden folder) is invalid. The attachments directory
  may be named; it only ever matches assets.
- An unknown `version`, unparseable JSON, or a profile that fails validation: that profile
  denies everything. A typo never becomes "whole vault".
- `access` other than the three values: `read`.

Flags on both `zn mcp` engines (and argv for `mcp.js`):

```bash
zn mcp --profile chatgpt
zn mcp --allow-path Projects --allow-path Areas/Health --deny-path Projects/Clients --read-only
zn mcp --profile reviewer --access read     # narrows the profile further
```

`--read-only` is an alias for `--access read`. Repeated flags accumulate.

### 4.4 Canonical paths (traversal, symlinks, case)

Every path a tool receives is turned into a canonical vault path *before* the scope check,
and the check runs on that:

1. Reject NUL, drive letters, and any `.` or `..` segment outright (no resolving); turn `\`
   into `/`; strip leading `/` and `./`.
2. Look the path up in the backend's own listings (`ListNotes`, `ListFolders`), local and
   remote alike: exact path first, else the single listed path equal under case and accent
   folding, so the vault's spelling is what the scope checks and what the backend opens. The
   listings walk real directories and regular files only (`walkNotes` and `listNotes` read
   `Dirent` types; the server's walker does the same), so a path through a symlink, a case
   variant of a hidden folder, a hidden directory or an unexpanded Windows 8.3 name simply
   has no match, and G5 to G7 close. A path the client was never shown does not exist for it.
   (The design first had a per-segment `Lstat` walk for local vaults; one rule for both
   backends is simpler and needs no platform cases. The cost is a listing per call, which
   most tools already pay.)
3. Creates (`create_note`, `create_folder`, move and restore destinations): existing folders
   keep their listed spelling, new segments are created as given; a segment ending in `.base`
   (a database's insides), and in root mode a first segment that names a system, asset or
   internal directory under any case (`vault.IsReservedRootName`), are refused; the result
   must still classify into the bucket asked for and lie inside an `allow` entry.
4. Match the canonical path against the entries, which are themselves spelled the vault's way
   once per call (so `--allow-path /projects` finds `Projects/`).

### 4.5 Fail-closed rules

| Situation | Behavior |
|-----------|----------|
| `--profile X` and the vault has no `mcp.json`, or no `X` | Every tool but `vault_info` errors: "This vault has no MCP profile named X, so nothing is shared. Add it in ZenNotes Settings > MCP > Assistant access." |
| `mcp.json` unreadable or invalid | Same, naming the file problem, never its contents. |
| A `deny` entry no longer exists on disk | The profile denies everything: "This profile hides a folder that no longer exists (renamed or deleted outside ZenNotes). Open Settings > MCP to update it." The folder is not named. Renames through ZenNotes rewrite entries (5.3), so this only fires after an outside rename or an old client's rename, which is exactly when a deny list would otherwise fail open. |
| An `allow` entry no longer exists | It matches nothing (already closed); `vault_info` says one entry is missing. |
| Remote server without the profiles endpoint | `--profile` clients see nothing: "This ZenNotes server does not serve MCP profiles (needs server X.Y)." Flags-only scopes still work. |
| Vault switch | Unchanged session rule, then the new vault's profile (or nothing). |

## 5. Behavior of every tool under a scope

"Not found" means the uniform error `Note not found: <path as given>` (or
`Folder not found: ...`), the same bytes whether the path is hidden or does not exist, with no
absolute path (closes G8). A tool above the access level is not listed and, if called anyway,
answers "This assistant can only read this vault (profile chatgpt)." without touching a path.

| Tool | Needs | Under a scope |
|------|-------|---------------|
| `vault_info` | read | Adds `access: {profile, level, allow, partial, missingEntries}`; `subfolders` filtered to visible folders plus structural ancestors; `deny` entries are never named; omits `vaultRoot`, `inboxAbsolutePath` and the remote `vaultPath` (they name the user's home directory and add nothing for an MCP-only client). |
| `list_notes` | read | Visible notes only; `folder`, `subpath`, `tag`, `wikilinkTo`, `updatedSinceMs` narrow inside the scope; `limit` applies after it. `folder: "trash"` returns visible trashed notes only. |
| `list_folders` | read | Visible folders plus structural ancestors; denied subtrees absent. |
| `list_assets` | read | Assets inside visible folders. Phase 3: plus assets a visible note embeds or links (via the asset-link parsers), never the rest of the shared attachments folder. |
| `read_note` | read | Visible `.md` note, else not found. |
| `search_text` | read | Hidden subtrees are skipped during the walk; `limit` counts visible matches only (today the walk stops at `limit`, and the remote backend truncates before returning, so filtering after would leak by short results). Snippets come only from visible notes. |
| `search_by_title`, `search_by_tag` | read | Over visible notes. |
| `list_tags` | read | Counted over visible notes; a tag used only in hidden notes does not appear. |
| `backlinks` | read | Target must be visible; results filtered to visible notes. |
| `list_tasks` | read | Tasks of visible notes; `includeExcluded` widens the Tasks exclusions, never the scope. |
| `list_comments` | read | Visible note only. |
| `add_comment`, `reply_to_comment`, `resolve_comment` | comment | Visible note only. |
| `toggle_task` | write | The note in the id (`path#index`) must be visible. |
| `write_note`, `append_to_note`, `prepend_to_note`, `insert_at_line`, `replace_in_note` | write | Visible note only; agent edit review unchanged. |
| `create_note`, `create_folder` | write | Destination inside an `allow` entry (not only an ancestor), not denied, never a hidden or system directory reached through a subpath (closes G2 for scoped clients; Phase 0 closes it for all). An error at an ancestor names where creating is possible: "You can create notes only in: Projects, Areas/Health." |
| `rename_note` | write | Source visible, new path visible. Inbound links are rewritten in visible notes only (decision 6). A server rewrites links in every note itself, so on a remote vault a path-restricted client cannot rename. |
| `move_note` | write | Source and destination visible. |
| `duplicate_note` | write | Source visible; the copy lands beside it. |
| `archive_note`, `move_to_trash` | write | Source visible. The destination may be hidden (a lifecycle exit: the client loses sight of the note, the user can undo in the app). Decision 5. |
| `unarchive_note`, `restore_from_trash` | write | Source visible (needs `archive/...` or `trash/...` allowed) and destination visible. |
| `delete_note` | write | Visible note; `confirm` as today. |
| `rename_folder`, `delete_folder` | write | Source and destination inside `allow` entries and no `deny` entry inside the subtree, else refused with one generic message. Decision 7. |
| `empty_trash` | write, unscoped only | Not listed under any `allow` or `deny`: it acts on every trashed note, visible or not. |

Every tool also gets MCP annotations from the same table (`readOnlyHint` for reads,
`destructiveHint` for `delete_*`, `empty_trash`, `write_note`), so clients that confirm
destructive calls can, scoped or not.

When a scope is active, the server instructions gain one paragraph: "This server shows part
of the user's vault. Paths and notes you are not shown do not exist for you; do not guess at
them, and treat links that do not resolve as pointing outside what you can see. Call
`vault_info` to see what you can reach."

## 6. Leak analysis

| Channel | Today | Under a scope | What remains |
|---------|-------|---------------|--------------|
| Listings (`list_notes`, `list_folders`, `vault_info.subfolders`) | whole vault | visible + structural ancestors | Ancestor names, which the entries contain anyway. |
| `search_text` results, counts, snippets | whole vault; stops at `limit` | filtered during the walk, limit after | None. |
| `search_by_title` / `search_by_tag` / `list_tags` counts | whole vault | visible only | None. |
| `list_tasks`, task ids | whole vault | visible only | None. |
| Backlinks into a visible note | from any note | visible linkers only | None. |
| Outbound links of a visible note (`wikilinks`, body) | n/a | unchanged | The *text* of a link to a hidden note, which is the visible note's own content. Its target never resolves for the client. |
| Wikilink resolution (`search_by_title`, `list_notes(wikilinkTo)`, `link` deep links) | whole vault | visible only | Hidden targets look unresolved. |
| Assets (`list_assets`) | every non-note file under the root | visible folders (later: referenced by visible notes) | Names and sizes of referenced assets, which the note already names. |
| Comments | any note | visible notes | Text a comment quotes, which is from the visible note. |
| Templates, databases, settings | no MCP tool | unchanged | A future tool must take the scope like the rest. |
| Error text (G8) | absolute path, distinct "missing" vs "not in a folder" | one uniform message, no absolute path | None. |
| Name collisions (`uniqueTitle` picks "Plan 2"; rename conflict error) | n/a | unchanged | One bit: a hidden note with that exact name exists in the same visible folder. Only possible with a `deny` note entry inside an allowed folder, only for a guessed name. Documented. |
| Folder-op refusals (`rename_folder`, `delete_folder`) | n/a | one generic refusal | One bit: something hidden lies inside. Documented; decision 7. |
| Hidden areas (`.zennotes`, attachments) via move/create/folder ops (G1 to G4) | reachable | never visible | None once Phase 0 lands for unscoped clients too. |
| Symlinks (G5, G6), case variants (G7), Unicode normalization, 8.3 names | followed | canonical walk refuses | None known. |
| Vault switch (#688 follow-app) | scope-less | per-vault profile or nothing | None. |
| Absolute paths in `vault_info` | shown | omitted | None. |
| Custom MCP instructions (user-written file) | sent | sent | Whatever the user wrote in it. |
| Link rewrite on rename | Go zn and znserver rewrite every note; the desktop engine none | decision 6 | If kept: the new title is written into hidden notes; nothing flows back to the client. |
| Remote token | in the MCP process env or credentials.toml | unchanged | The client never sees it; it cannot call the server API around the scope. |

## 7. Sharing between the desktop engine and zn

### 7.1 What is synced today

From the zennotes CLAUDE.md and the code:

| Logic | Copies |
|-------|--------|
| System-folder path rules | `packages/shared-domain/src/system-folder-paths.ts` (normative), a copy in `apps/desktop/src/mcp/vault-ops.ts`, znserver `internal/vault/types.go`, and Go zn `internal/vault/settings.go` |
| `stripCodeContent` | 5 in the monorepo (desktop main, MCP, app-core tags, app-core wikilinks, Go server) plus the iPhone vault-core |
| Task-line parser | 4 (editor, CLI, MCP, Go) |
| Tasks folder lists and their rename rewrite | `shared-domain/tasks-excluded-folders.ts` (imported by desktop main, the MCP, the iPhone bridge), znserver `internal/vault/tasks_exclude.go`, Go zn `internal/vault/settings.go` |
| MCP tool table, schemas and descriptions | hand-kept parity between `server.ts` and `server.go` |
| MCP instructions, client display names, token hint, agent-edit recorder | verbatim mirrors (`instructions.go` `DefaultInstructions`, `agentedits.go`) |

### 7.2 What this adds, and where

- **Normative module:** `packages/shared-domain/src/mcp-scope.ts`, pure TS: parse and
  validate `mcp.json` and flags, `effectiveScope(...)`, `isVisible(canonicalPath, kind)`,
  `isStructuralAncestor`, `rewriteMcpScopeForRename(oldRel, newRel)`, and `MCP_TOOL_ACCESS`
  (tool → `read | comment | write | unscopedWrite` plus annotations). The desktop MCP may import
  `@shared` (bundled into `out/main/chunks`), so it imports this instead of copying, as it
  already does for the tasks lists. Settings, desktop main (rename rewrite) and web import it.
- **Go mirrors (one new synced copy family, three places):** Go zn `internal/mcp/scope.go`
  (evaluator, canonical walk, decorator) and the rename rewrite next to the tasks-lists one in
  `internal/vault`; znserver `internal/vault` (rename rewrite now, native enforcement later).
- **Decorator, one per engine:** TS `apps/desktop/src/mcp/scoped-backend.ts` implements
  `VaultBackend`; Go `internal/mcp/scoped.go` implements `backend.Backend`. The dispatcher in
  each wraps the session's backend when a scope is active and checks `MCP_TOOL_ACCESS` before
  the handler runs. Handlers that compute over `ListNotes` (`list_tags`, `search_by_*`) become
  correct for free; `SearchText` gets a "no limit, filter, then cut" path because Go's
  `SearchText` treats `limit <= 0` as 80 and the remote one truncates.
- **Contract fixture:** `mcp-scope.json` exported from zennotes (vault trees including symlinks,
  case variants, remapped system folders, root and inbox mode; scopes; tool calls; expected
  results and error bytes), consumed in this repo under `internal/mcp/testdata/` with a
  `.source.json` hash, per `docs/shared-contracts.md`. Neither implementation regenerates
  expectations from itself.
- **Tool table parity check:** a test in each repo that the tool list in `MCP_TOOL_ACCESS`
  (and its Go mirror) equals the registered tools, so a new tool cannot ship unclassified.
  An unclassified tool is treated as `unscopedWrite` (not listed under any scope).

### 7.3 Rename rewriting

Folder renames through ZenNotes must repoint `allow` and `deny` entries, like the tasks lists:
desktop main (`vault.ts` rename path), app-core store, znserver `RenameFolder`, Go zn
`RenameFolder`, and both MCP engines' `rename_folder` (the desktop engine skips the tasks lists
today; fix both together). Folder deletes through ZenNotes drop entries under the deleted
folder. Renames outside ZenNotes are caught by the stale-deny rule (4.5).

## 8. Remote and self-hosted vaults

- The decorator wraps the `Remote` backend the same way; canonical paths come from the
  server's listings (4.4 step 3). Flags-only scopes need no server change.
- Profiles for a remote vault live in that vault's `.zennotes/mcp.json` on the server. The
  server needs `GET /api/vault/mcp-profiles` (and `PUT` for Settings), returning the validated
  file; `.zennotes` stays unreachable through the note APIs. An older server answers 404 and
  `--profile` clients see nothing (4.5).
- Token handling (#830, Settings → MCP → Server token) is unchanged: the MCP process holds a
  full token, which the client never sees.
- Later, with the parked remote connector (znserver `/mcp`, OAuth): the server enforces the
  same profiles natively, and a scoped server token could bind a profile to the token so the
  HTTP API itself refuses out-of-scope paths. Not needed for #21.

## 9. UI and docs

### Desktop Settings → MCP

- New **Assistant access** section: the vault's profiles as rows (label, access, "Projects,
  Areas/Health" summary, "shares 213 of 1,402 notes"). New / Edit / Delete.
- Profile editor: access segmented control (Read / Comment / Edit); "Can see": Whole vault or
  Only these folders, built like `TasksFolderListRow` (the Tasks folders editor, #458, #962);
  "Always hidden" with the same editor; a live preview list of what the profile shows,
  computed by the shared evaluator, so the user checks it before an assistant does.
- Each client row (`McpClientRow`) gains a **Profile** select (Full access, or a profile).
  Install writes `--profile <name>` into the client's args for both runtimes
  (`['mcp', '--profile', name]` for a managed zn, `[entry, '--profile', name]` for
  Electron + `mcp.js`); the existing `upToDate` comparison already covers args. Rows for
  clients with a shell (Claude Code, Codex, OpenCode) carry one line: "This assistant can also
  read files directly; a profile limits its ZenNotes tools only."
- Stale entries show inline ("Journal no longer exists") with Remove / Pick folder.
- Settings search items for each new control (`mcp-assistant-access`, `mcp-client-profile`).

### Keyboard

- Profile rows and folder lists navigate with j/k and Enter when Vim is on, arrows otherwise
  (the Vim-off rule: no single-key list shortcuts).
- Sidebar folder context menu: "Share with assistant ▸ <profile>" and "Hide from assistant ▸
  <profile>". This is how the Tasks folder lists already work: the folder menu is the
  primary way in, the Settings list is where entries are reviewed and removed.
- Palette command `mcp.assistantAccess` ("Assistant access…") opens the section.
- Vim binding: proposed under note actions, `Space l h`, "share or hide this note's folder for
  an assistant profile" (opens a small profile picker). Final key is Adib's call.

### Terminal (this repo)

- `zn mcp profile list | show <name> | explain <name> <path>`: `explain` prints visible or
  hidden and which rule decided, the fastest way to test a scope. `zn mcp profile add/edit`
  writes `mcp.json` with the same validator.
- `zn help` lists the new `zn mcp` flags; `:mcp` in `zn tui` shows the vault's profiles read-only.

### Docs, both surfaces in the same change (zennotes CLAUDE.md rule)

- In-app manual: `packages/app-core/src/lib/help.ts`, MCP card: profiles, flags, what read /
  comment / edit mean, the shell-access caveat, fail-closed messages.
- Website: `resources/views/docs.blade.php` and `docs-sections.json` in the Laravel repo.
- This repo: README MCP section and `docs/desktop-integration.md`.
- The issue thread, when it ships (not before).

## 10. Prior art

- **Reference filesystem server** (`@modelcontextprotocol/server-filesystem`): allowed
  directories as launch arguments; CVE-2025-53109 / GHSA-q66q-fx2p-7w4m was a path validation
  bypass through prefix matching and symlinks in allowed directories, fixed in 2025.7.1.
  Lessons taken: segment-boundary matching, canonical paths, symlinks as the first thing to
  test. ([Wiz](https://www.wiz.io/vulnerability-database/cve/cve-2025-53109),
  [GitLab advisory](https://advisories.gitlab.com/pkg/npm/@modelcontextprotocol/server-filesystem/CVE-2025-53109/))
- **MCP roots:** client-provided `file://` roots; servers "SHOULD respect root boundaries", and
  the docs call roots a coordination mechanism, not a security boundary. So roots can only
  ever *narrow* a ZenNotes scope (a possible Phase 3 extra), never define one.
  ([client concepts](https://modelcontextprotocol.io/docs/2025-03-26/learn/client-concepts),
  [spec 2025-06-18](https://modelcontextprotocol.io/specification/2025-06-18/client))
- **GitHub MCP server:** `--read-only` drops write tools at registration, `--toolsets` loads
  groups, options compose; lockdown mode is a best-effort content filter, not an authorization
  boundary. Lessons: do not list what a client cannot use; restrictions compose.
  ([configuration guide](https://glama.ai/mcp/servers/@github/github-mcp-server/blob/bf6467855c13d3b8f5cf72f8dfd9c86f8336b20b/docs/server-configuration.md),
  [NeuralTrust](https://neuraltrust.ai/blog/how-to-secure-github-mcp))
- **Obsidian MCP servers:** `obsidian-mcp-server` has `OBSIDIAN_READ_PATHS`,
  `OBSIDIAN_WRITE_PATHS`, `OBSIDIAN_READ_ONLY` gating every path-taking tool, with write tools
  disabled at registration; a Daily Notes server has per-vault `allowedRoots` /
  `excludedRoots`, read-only by default. Separate read and write path sets are an idea worth
  keeping for later (open question 9). (from their changelogs and listings, not verified
  against current READMEs:
  [changelog](https://unpkg.com/obsidian-mcp-server@3.2.4/changelog/3.1.x/3.1.2.md),
  [Daily Notes MCP](https://glama.ai/mcp/servers/nmt8vee4zb),
  [Obsidian Scoped-write MCP](https://glama.ai/mcp/servers/pa06gualox))
- **Notion integrations:** default-deny by page sharing (children inherit), with separate
  capabilities for read / update / insert content and read / insert comments. The model for
  the `comment` tier. ([capabilities](https://developers.notion.com/reference/capabilities))
- **Cursor `.cursorignore`, Gemini `.aiexclude`:** ignore files in the content tree; Cursor
  documents that MCP and terminal tools are not blocked and that it is best-effort. The
  lesson for #21 is the reporter's: enforcement has to be in the server.
  ([Cursor](https://cursor.com/docs/context/ignore-files),
  [Gemini Code Assist](https://docs.cloud.google.com/gemini/docs/codeassist/create-aiexclude-file))

## 11. Phased plan

Estimates are focused engineer-days for someone who knows both codebases, including tests.

| Phase | Scope | Repos | Effort |
|-------|-------|-------|--------|
| 0. Hardening (ship first, on its own) | Move/rename/delete sources and destinations must classify into a note bucket; subpaths cannot reach hidden or system directories in root mode (G1 to G4); TS realpath step like Go `SafeJoin` (G5); `read_note` only `.md`; desktop engine `rename_folder` rewrites vault.json lists; tests from the probe cases | tui, zennotes | 2 |
| 1a. Scope core, flags only, Go | `--allow-path` / `--deny-path` / `--read-only` / `--access`; evaluator, canonical walk, decorator, dispatcher gate, uniform errors, `vault_info.access`, instructions paragraph, annotations, tool parity test; ships in a zn release and reaches the desktop through the zn pin | tui | 4 |
| 1b. Scope core, desktop engine | `shared-domain/mcp-scope.ts` (normative) + fixture export, TS decorator, flags on the TS `zn mcp` and argv on `mcp.js`; Go consumes the fixture | zennotes, tui | 4 |
| 2. Profiles | `.zennotes/mcp.json` read/write and validation, `--profile` / `ZENNOTES_MCP_PROFILE`, fail-closed rules, rename rewrite in all hosts, znserver profiles endpoint, Settings → MCP Assistant access + client Profile select, sidebar menu, palette, Vim binding, `zn mcp profile`, docs on both surfaces | zennotes, tui, znserver, website | 7 |
| 3. Extras (each optional) | Referenced assets; per-note frontmatter opt-out (`mcp: false`, rename-proof, like `tasks: false`); MCP roots as extra narrowing; per-folder access levels; globs | as needed | 1 to 2 each |
| 4. Server-side (with the parked remote connector) | znserver `/mcp` enforcing profiles; scoped server tokens | znserver | separate design |

Phases 0 and 1a alone answer the issue as filed. Phase 2 is what makes it usable from the
app without hand-editing client configs.

## 12. Decisions

Adib, 2026-10-29: "follow your recommendations". Settled:

1. **Profiles live in `.zennotes/mcp.json`** per vault (4.2), not vault.json or config.toml.
2. **No implicit default profile.** A client without `--profile` keeps full access; restricting
   every assistant means giving each client a profile.
3. **A stale `deny` entry denies everything** until it is fixed. This applies to
   `--deny-path` too: a flag naming a folder the current vault does not have shares nothing.
4. **Folders and notes only in v1**, no globs.
5. **Lifecycle exits are allowed:** a scoped write client may archive or trash a visible note
   into an archive or trash it cannot see.
6. **Under a scope, rename rewrites `[[links]]` in visible notes only.** The engines' existing
   difference (Go zn and znserver rewrite every note, the desktop engine none) is settled
   separately.
7. **`rename_folder` / `delete_folder` stay available** when source and destination lie
   inside `allow` entries and no `deny` entry is inside the subtree; the one-bit leak is
   documented.
8. **Three access tiers:** `read`, `comment`, `write`.
9. **Per-folder access levels** are a Phase 3 option, not v1.
10. **Phase 0 ships first, on its own** (started 2026-10-29 in its own session; merged on its
    own as ZenNotes/tui#23, then released with Phase 1a in zn 0.13.0 and desktop 2.80.0).
11. **`Space l h`** for the folder share/hide action.

Still open: whether Phase 0's release notes carry an advisory line (zn 0.13.0's notes list
it as a fix, with no advisory), and when to answer lewxdev on #21 (and whether to invite
their review of this design).

## 13. Phase 1a as built

Shipped in zn 0.13.0 (2026-10-30), with Phase 0 under it:

- `internal/mcp/scope.go`: `Scope`, `NewScope` (flag validation), segment matching,
  `toolClasses` and `toolAllowed`, tool annotations, the instructions paragraph, the
  `vault_info.access` block.
- `internal/mcp/scoped.go`: `scopedBackend`, one per tool call, resolving every path
  against the listings (4.4) and implementing every `Backend` method; the ones MCP never
  reaches (templates, settings writes, asset bytes, databases) refuse.
- `internal/mcp/server.go`: `Options.Scope`; unlisted tools are not registered; every call
  runs on the scoped backend; `vault_info` drops absolute paths and adds `access`;
  `DescribeToolError` scrubs the vault root from a scoped client's errors;
  `CallToolScoped` is the test seam.
- `internal/vault`: `RenameNoteRewritingLinksIn` (the rename with a filter on which notes
  get their links rewritten) and `IsReservedRootName`; `internal/backend/local.go` exposes the
  rename. `internal/cli`: the four flags (`--read-only` is a switch) and the help row.
- Tests: `internal/mcp/scope_test.go`, through the in-memory MCP transport. A mutation that
  disables deny matching fails five of them.
- Verified with the built binary over stdio: `zn mcp --allow-path /projects --read-only`
  lists 12 tools, shares only `Projects/`, answers `Note not found` for a hidden note and for
  the symlinked path to it, and treats `move_note` / `write_note` as unknown tools;
  `--allow-path .zennotes` stops the server before it serves.

Differences from the design above: path resolution through the listings for local vaults
too (4.4); the stale-deny rule applies to `--deny-path` as well as to profiles (decision 3);
remote renames are refused under a path restriction rather than rewriting hidden notes.

Not in Phase 1a: the desktop engine (1b), profiles and the Settings UI (2), the shared
fixture (lands with 1b, which owns the normative TS module).

## 14. Phase 1b as built

In ZenNotes/zennotes as 7be9e993 (on top of Phase 0, landing in desktop 2.80.0), and here
as the link fix and the contract test in zn 0.13.0:

- `packages/shared-domain/src/mcp-scope.ts`, the normative module: flag parsing and entry
  validation with zn's refusal bytes (Go's `%q` included), `foldMcpScopePath` (`search.Fold`,
  lowered one code point at a time so JavaScript's final sigma does not apply), segment
  matching, `MCP_TOOL_ACCESS` and the annotations, the access refusals, the instructions
  paragraph, the `vault_info.access` block and code-point sorting.
- `apps/desktop/src/mcp/scoped-backend.ts`: `ScopedBackend`, the `VaultBackend` decorator,
  one per call, ported from `scoped.go`. `VaultBackend` gained `vaultLayout()` (system folder
  paths and the primary location, local and remote), the classification input Go reads from
  `VaultSettings`.
- `server.ts`: tools a scope does not offer are not listed, every tool carries annotations
  (`readOnlyHint` and `idempotentHint` spelled out even when false, as the Go SDK sends them),
  the scoped `vault_info`, scrubbed errors, the instructions paragraph;
  `callTool(name, args, backend, scope)` is the test seam.
- Flags on the desktop's TS `zn mcp` (`read-only` joined `VALUELESS_FLAGS`, and the comment on
  `valuelessFlags` here no longer calls it zn's own) and argv on the legacy `out/main/mcp.js`.
  A bad entry stops either before it serves, with one line on stderr.
- The contract fixture `packages/bridge-contract/fixtures/mcp-scope.json`: flag cases and tool
  cases on three vaults (root mode with a link to a hidden folder and one out of the vault,
  accented names, inbox mode with Inbox and Archive remapped), each checking the tools listed,
  every call's result or exact error and the disk afterwards. The desktop runs it in
  `apps/desktop/src/mcp/scope-contract.test.ts`, this repo in
  `internal/mcp/scope_contract_test.go` from a copy with its `.source.json`;
  `npm run sync:contract-fixtures -- --tui <checkout>` refreshes the copy.

Found while building it, and fixed in both engines: a destination segment the listings do
not know was taken as new. The listings skip symbolic links, so under `--allow-path Projects`,
`create_note`, `create_folder`, `move_note` and `rename_folder` into `Projects/inner` (a link
to the hidden `Journal`) wrote into `Journal`. On a vault on this machine, the first segment
of a destination the listings do not know must now have nothing on disk (anything but "does
not exist" refuses, with the usual refusal); fixture case `nothing-is-created-through-a-link`.
A server's vault is left to the server's own walker.

Engine differences that remain, left out of the fixture on purpose:

- Renames: the desktop engine never rewrites inbound links, scoped or not, so decision 6
  holds trivially there; zn rewrites them in the notes the client can see.
- `list_assets`: zn lists every non-note file, the desktop engine the attachments folders
  only. Both filter by visibility.
- A tool the scope does not offer: zn answers with a JSON-RPC error (the SDK's unknown tool),
  the desktop engine with an `isError` result `Unknown tool: X`, as each already did for any
  unknown name.
- A bad flag: zn and the desktop's `zn mcp` print `zn: <message>`, the legacy `mcp.js`
  `[zennotes-mcp] <message>`.

Pre-existing and unscoped, seen in the same probe: the wording of a missing file's error,
`add_comment`'s author for a client neither engine knows (zn null, the desktop the client's
name capitalised), and the desktop engine creating an archive or trash folder before it finds
that the note to move is gone.

Verified: both engines pass the fixture; the stdio parity probe (`probe_scoped.py`, in the
session notes) gives identical transcripts for every scoped flag set on Go (fbaf94c plus the
fix, with and without Phase 0 merged in) and on both TS entries, apart from the differences
above. Not in 1b: profiles and the Settings UI (Phase 2).
