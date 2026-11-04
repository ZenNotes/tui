// Package mcp is the ZenNotes MCP server: the vault operations exposed as
// tools, with a note-taker-focused system instruction, over stdio (or HTTP,
// through the mcpserver package). Tool names, argument shapes and result shapes match the server the desktop app
// bundles, so an installed client keeps working when it launches this
// binary instead. The tools in assets.go and worktools.go are this server's
// alone until the desktop's mirrors them; see docs/shared-contracts.md.
package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/vault"
)

// Backend is what the tools run against.
type Backend = backend.Backend

// Options configure the server.
type Options struct {
	// ResolveTarget picks the vault. It runs before every tool call (see
	// session); a failure is that call's error, and the next call tries
	// again rather than repeating a stale failure.
	ResolveTarget func() (backend.Target, error)
	// OpenBackend binds a resolved target.
	OpenBackend func(backend.Target) (Backend, error)
	// Scope limits what clients see and change (ZenNotes/tui#21); nil is
	// full access. It comes from the server's own launch flags, never from a
	// tool call.
	Scope *Scope
	// Version is reported to clients.
	Version string
	// Transport defaults to stdio; tests pass an in-memory one.
	Transport sdk.Transport
	// Hosted is the mode for a ZenNotes server serving these tools from inside
	// itself over HTTP (mcpserver), where the client is on another machine:
	//   - The server pins the vault itself, so nothing asks which one it serves.
	//   - A full-access client is wrapped in the scope too, so it gets no absolute
	//     paths, and vault_info does not report the server's internal address.
	//   - No zn config file is read; they belong to whoever runs zn, not the server.
	//   - A hidden tool answers as a tool error, because the SDK's HTTP 400 for an
	//     unknown tool under protocol 2026-07-28 makes its Go client hang up.
	//   - A refusal never advises `zn connect`, and names no internal address.
	//   - A tool that would always refuse under a path scope is not offered.
	//   - A panic in a tool answers that call instead of stopping the server.
	Hosted bool
	// HostedBaseURL is the internal address Hosted keeps out of what clients read.
	HostedBaseURL string
	// LogCall, when set, is told each tool call's name, outcome (ok, error or panic)
	// and duration, and never its arguments or result. Hosted only.
	LogCall func(tool, outcome string, took time.Duration)
	// Logf, when set, gets what would otherwise go to stderr. Hosted only.
	Logf func(format string, args ...any)
	// MaxResponseBytes, when set, is the largest answer to a tool call, as it goes on the
	// wire. An answer waits in memory until its client reads it, and JSON encoded twice
	// can take four times the note it came from. Hosted only.
	MaxResponseBytes int64
	// ToolSlots, when set, is how many tool calls run at once, shared with other
	// servers built on the same channel. Hosted only.
	ToolSlots chan struct{}
	// WriteTurn, when set, is the turn writes take, shared with other servers built on
	// the same channel, since several credentials can write one vault. Hosted only.
	WriteTurn chan struct{}
	// CommentAuthor signs every comment and reply. A client's own `author` argument
	// is ignored, so one credential cannot sign as someone else. Hosted only.
	CommentAuthor string
}

// previousVaultKey carries, into vault_info, the vault a session left when
// that call moved it to the one the app has open now.
type previousVaultKey struct{}

type args map[string]any

func (a args) requireString(key string) (string, error) {
	v, ok := a[key].(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("Missing required string argument: %s", key)
	}
	return v, nil
}

func (a args) optionalString(key string) (string, bool, error) {
	v, present := a[key]
	if !present || v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	return s, true, nil
}

func (a args) requireFolder(key string) (vault.NoteFolder, error) {
	v, err := a.requireString(key)
	if err != nil {
		return "", err
	}
	switch v {
	case "inbox", "quick", "archive", "trash":
		return vault.NoteFolder(v), nil
	}
	return "", fmt.Errorf("%s must be one of inbox, quick, archive, trash", key)
}

func (a args) optionalNumber(key string) (float64, bool, error) {
	v, present := a[key]
	if !present || v == nil {
		return 0, false, nil
	}
	n, ok := v.(float64)
	if !ok {
		return 0, false, fmt.Errorf("%s must be a number", key)
	}
	return n, true, nil
}

func (a args) optionalStringArray(key string) ([]string, bool, error) {
	v, present := a[key]
	if !present || v == nil {
		return nil, false, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, false, fmt.Errorf("%s must be an array of strings", key)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, false, fmt.Errorf("%s must be an array of strings", key)
		}
		out = append(out, s)
	}
	return out, true, nil
}

func (a args) boolIs(key string) bool {
	b, _ := a[key].(bool)
	return b
}

type toolDef struct {
	name        string
	description string
	schema      string
	handler     func(ctx context.Context, a args, b Backend) (any, error)
}

func lower(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = strings.ToLower(s)
	}
	return out
}

func containsFold(list []string, needle string) bool {
	for _, s := range list {
		if strings.ToLower(s) == needle {
			return true
		}
	}
	return false
}

// capped applies a client's limit. A negative one, or one too large for an int, is
// no limit at all, so that it can never become a bad slice bound.
func capped[T any](items []T, limit float64) []T {
	if limit < 0 || limit >= float64(len(items)) {
		return items
	}
	return items[:int(limit)]
}

// clampedLimit is a client's limit as an int. Converting a float too large for one
// is undefined, and comes out negative on amd64.
func clampedLimit(limit float64) int {
	if limit < 0 {
		return 0
	}
	if limit > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(limit)
}

func sortByUpdated(notes []vault.NoteMeta) {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].UpdatedAt > notes[j].UpdatedAt })
}

func tools() []toolDef {
	out := coreTools()
	out = append(out, assetTools()...)
	return append(out, workTools()...)
}

func coreTools() []toolDef {
	return []toolDef{
		{
			name:        "vault_info",
			description: "Return where the currently configured ZenNotes vault lives (a folder on this machine, or a self-hosted server the desktop app is connected to) and its top-level layout. Call this once at the start of a session to confirm you are pointing at the right vault, and to learn whether the user runs in `inbox` or `root` primary mode (the answer changes how every other tool behaves).",
			schema:      `{"type":"object","properties":{}}`,
			handler: func(ctx context.Context, _ args, b Backend) (any, error) {
				description, err := b.Describe(ctx)
				if err != nil {
					return nil, err
				}
				folders, err := b.ListFolders(ctx)
				if err != nil {
					// A scope that shares nothing still answers vault_info, with
					// the reason under access.blocked.
					if _, scoped := b.(*scopedBackend); !scoped {
						return nil, err
					}
					folders = []vault.FolderEntry{}
				}
				isRoot := description.PrimaryNotesLocation == vault.PrimaryNotesRoot
				scoped, isScoped := b.(*scopedBackend)
				pathNotes := "IMPORTANT: Always use the `path` returned by other tools verbatim. Never prepend `inbox/` to a path you got back from list_notes / create_note / read_note. "
				switchNote := ""
				if previous, ok := ctx.Value(previousVaultKey{}).(backend.Target); ok {
					switchNote = "The vault changed: until this call the session worked in " + describeTarget(previous, isScoped) + ". Every tool runs here now, and paths or content you got from the previous vault do not apply to this one. "
				}
				if isRoot {
					pathNotes += "This vault uses ROOT mode: notes for the conceptual `inbox` folder live directly at the vault root (e.g. `MyNote.md`), not under `inbox/`. The folders quick/, archive/, trash/ are real subdirectories at the root."
				} else {
					pathNotes += "This vault uses INBOX mode: notes for the conceptual `inbox` folder live under `inbox/` (e.g. `inbox/MyNote.md`)."
				}
				if isScoped && scoped.scope.Restricted() {
					pathNotes += " You see part of this vault: `access.allow` lists what was shared (empty means everything except some hidden paths). Anything else does not exist for you."
				}
				top := []string{"inbox", "quick", "archive", "trash"}
				if isScoped {
					// An assistant with scoped access gets no absolute paths: it
					// has no use for them, and they name the user's home folder.
					result := orderedResult{{"kind", string(description.Kind)}}
					if description.Kind == backend.KindRemote && !scoped.hosted {
						var serverName *string
						if description.Name != "" {
							serverName = &description.Name
						}
						result = append(result, orderedResult{{"server", description.BaseURL}, {"serverName", serverName}}...)
					}
					result = append(result, orderedResult{
						{"primaryNotesLocation", description.PrimaryNotesLocation},
						{"topFolders", top},
						{"subfolders", folders},
						{"access", scoped.accessInfo(ctx)},
						{"notes", switchNote + pathNotes},
					}...)
					return result, nil
				}
				if description.Kind == backend.KindRemote {
					notes := "This vault lives on a self-hosted ZenNotes server, the workspace the desktop app currently has open. Every tool works on it through the server API; paths are vault-relative POSIX paths exactly as the server reports them. " + pathNotes
					if !description.AuthConfigured {
						notes += " No token is configured for this server in this process. If calls fail with 401, " + tokenHint(description.BaseURL)
					}
					var serverName, vaultPath, vaultName *string
					if description.Name != "" {
						serverName = &description.Name
					}
					if description.VaultPath != "" {
						vaultPath = &description.VaultPath
					}
					if description.VaultName != "" {
						vaultName = &description.VaultName
					}
					return orderedResult{
						{"kind", "remote"},
						{"server", description.BaseURL},
						{"serverName", serverName},
						{"vaultPath", vaultPath},
						{"vaultName", vaultName},
						{"primaryNotesLocation", description.PrimaryNotesLocation},
						{"topFolders", top},
						{"subfolders", folders},
						{"authConfigured", description.AuthConfigured},
						{"notes", switchNote + notes},
					}, nil
				}
				inboxAbs := description.Root + "/inbox"
				if isRoot {
					inboxAbs = description.Root
				}
				return orderedResult{
					{"kind", "local"},
					{"vaultRoot", description.Root},
					{"primaryNotesLocation", description.PrimaryNotesLocation},
					{"inboxAbsolutePath", inboxAbs},
					{"topFolders", top},
					{"subfolders", folders},
					{"notes", switchNote + pathNotes},
				}, nil
			},
		},
		{
			name:        "list_notes",
			description: "List notes in the vault with metadata (title, folder, tags, wikilinks, excerpt, timestamps). Optional filters narrow by folder, tag, or wikilink target. Use this before editing to pick the right note. Trashed notes are included only when folder='trash' is explicit. The returned `path` field is the canonical reference: pass it back verbatim to read_note / write_note / move_note. In `primaryNotesLocation: root` vaults, inbox notes have a path WITHOUT an `inbox/` prefix (e.g. `MyNote.md`); never add one. Each note also carries `link`, a clickable zennotes:// URL that opens it in the app; use it verbatim when rendering note titles as markdown links for the user.",
			schema: `{"type":"object","properties":{
"folder":{"type":"string","enum":["inbox","quick","archive","trash"],"description":"Only return notes in this top-level folder."},
"subpath":{"type":"string","description":"POSIX subpath under the folder (e.g. \"Work/Meetings\"). Requires folder to be set."},
"tag":{"type":"string","description":"Only return notes that contain this #tag."},
"wikilinkTo":{"type":"string","description":"Only return notes that link to a note with this title."},
"updatedSinceMs":{"type":"number","description":"Only return notes updated at or after this epoch millisecond timestamp."},
"limit":{"type":"number","description":"Cap the number of notes returned. Default: 200."}}}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				var folder vault.NoteFolder
				if a["folder"] != nil {
					f, err := a.requireFolder("folder")
					if err != nil {
						return nil, err
					}
					folder = f
				}
				sub, hasSub, err := a.optionalString("subpath")
				if err != nil {
					return nil, err
				}
				tag, hasTag, err := a.optionalString("tag")
				if err != nil {
					return nil, err
				}
				wikilinkTo, hasWiki, err := a.optionalString("wikilinkTo")
				if err != nil {
					return nil, err
				}
				since, hasSince, err := a.optionalNumber("updatedSinceMs")
				if err != nil {
					return nil, err
				}
				limit, hasLimit, err := a.optionalNumber("limit")
				if err != nil {
					return nil, err
				}
				if !hasLimit {
					limit = 200
				}
				all, err := b.ListNotes(ctx)
				if err != nil {
					return nil, err
				}
				// A tag filter is a tag search, which skips the template
				// folder (ZenNotes/zennotes#1024).
				templateFolder := ""
				if hasTag {
					templateFolder = backend.TemplateFolder(ctx, b)
				}
				notes := []vault.NoteMeta{}
				prefix := ""
				if hasSub && sub != "" {
					prefix = string(folder) + "/" + strings.Trim(sub, "/") + "/"
				}
				for _, n := range all {
					if folder != "" {
						if n.Folder != folder {
							continue
						}
					} else if n.Folder == vault.FolderTrash {
						continue
					}
					if prefix != "" && !strings.HasPrefix(n.Path, prefix) {
						continue
					}
					if hasTag && (!containsFold(n.Tags, strings.ToLower(tag)) || vault.IsPathInTemplateFolder(n.Path, templateFolder)) {
						continue
					}
					if hasWiki && !containsFold(n.Wikilinks, strings.ToLower(wikilinkTo)) {
						continue
					}
					if hasSince && float64(n.UpdatedAt) < since {
						continue
					}
					notes = append(notes, n)
				}
				sortByUpdated(notes)
				return capped(notes, limit), nil
			},
		},
		{
			name:        "list_folders",
			description: "List every subfolder in the vault grouped by top-level folder.",
			schema:      `{"type":"object","properties":{}}`,
			handler: func(ctx context.Context, _ args, b Backend) (any, error) {
				return b.ListFolders(ctx)
			},
		},
		{
			name:        "list_assets",
			description: "List the vault’s attachments: every file that is not a note, wherever it is (images, PDFs, audio, video, other binaries). Useful when a note references an asset you need to inspect. With deleted: true, lists the attachments in the app's Trash instead, with the undoToken restore_asset takes.",
			schema:      `{"type":"object","properties":{"deleted":{"type":"boolean","description":"List deleted attachments instead."}}}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				if a.boolIs("deleted") {
					m, err := assetManager(b)
					if err != nil {
						return nil, err
					}
					return m.ListDeletedAssets(ctx)
				}
				all, err := b.ListAssets(ctx)
				if err != nil {
					return nil, err
				}
				return shownAssets(all), nil
			},
		},
		{
			name:        "read_note",
			description: "Read the full body and metadata of a single note. Always read before you write. Use the path verbatim from list_notes / search_* / create_note. Never invent or prefix it.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative POSIX path exactly as another tool returned it. In ` + "`primaryNotesLocation: root`" + ` vaults this looks like \"MyNote.md\"; in ` + "`inbox`" + ` mode it looks like \"inbox/MyNote.md\". Pass it through unchanged."}},"required":["path"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				return b.ReadNote(ctx, rel)
			},
		},
		{
			name:        "write_note",
			description: "Overwrite the full body of an existing note. Destructive, so prefer append_to_note / replace_in_note for incremental edits. Keep frontmatter intact unless the user asked you to change it.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative POSIX path."},"body":{"type":"string","description":"New full markdown body."}},"required":["path","body"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				body, ok := a["body"].(string)
				if !ok {
					return nil, errors.New("body must be a string")
				}
				return b.WriteNote(ctx, rel, body)
			},
		},
		{
			name:        "create_note",
			description: "Create a new note. Always picks a non-conflicting filename by appending a counter if needed. Use the `path` from the response to refer to the note in any follow-up tool calls. DO NOT reconstruct it from `folder + title`. In root-mode vaults, `folder: \"inbox\"` means the note lands directly at the vault root with a path like \"MyNote.md\" (no inbox/ prefix).",
			schema: `{"type":"object","properties":{
"folder":{"type":"string","enum":["inbox","quick","archive"],"description":"Conceptual folder. Refuses \"trash\". In ` + "`primaryNotesLocation: inbox`" + ` vaults, \"inbox\" maps to <root>/inbox/; in ` + "`primaryNotesLocation: root`" + ` vaults, \"inbox\" maps to the vault root itself. quick/ and archive/ are always real subdirectories. Default if omitted: \"inbox\"."},
"title":{"type":"string","description":"Human-readable title. Becomes the filename (sanitized). Optional; defaults to \"Untitled\"."},
"subpath":{"type":"string","description":"POSIX subpath under the folder. Example: \"Work/Research\". Missing parents are created."},
"body":{"type":"string","description":"Initial markdown body. Defaults to \"# <title>\\n\\n\"."}}}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				folder := vault.FolderInbox
				if a["folder"] != nil {
					f, err := a.requireFolder("folder")
					if err != nil {
						return nil, err
					}
					folder = f
				}
				if folder == vault.FolderTrash {
					return nil, errors.New("Refusing to create a note directly in trash/")
				}
				title, _, err := a.optionalString("title")
				if err != nil {
					return nil, err
				}
				subpath, _, err := a.optionalString("subpath")
				if err != nil {
					return nil, err
				}
				body, hasBody, err := a.optionalString("body")
				if err != nil {
					return nil, err
				}
				var bodyPtr *string
				if hasBody {
					bodyPtr = &body
				}
				return b.CreateNote(ctx, folder, title, subpath, bodyPtr)
			},
		},
		{
			name:        "rename_note",
			description: "Rename a note in place (filename only, same folder). Check backlinks() first so you can warn the user about dangling wikilinks.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"newTitle":{"type":"string"}},"required":["path","newTitle"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				title, err := a.requireString("newTitle")
				if err != nil {
					return nil, err
				}
				return b.RenameNote(ctx, rel, title)
			},
		},
		{
			name:        "move_note",
			description: "Move a note to a new folder or subfolder. Use archive_note/unarchive_note/move_to_trash for the special destinations rather than this tool.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"targetFolder":{"type":"string","enum":["inbox","quick","archive","trash"]},"targetSubpath":{"type":"string","description":"Optional POSIX subpath under the folder."}},"required":["path","targetFolder"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				folder, err := a.requireFolder("targetFolder")
				if err != nil {
					return nil, err
				}
				sub, _, err := a.optionalString("targetSubpath")
				if err != nil {
					return nil, err
				}
				return b.MoveNote(ctx, rel, folder, sub)
			},
		},
		pathTool("duplicate_note", "Duplicate a note next to itself with a \" copy\" suffix.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.DuplicateNote(ctx, rel)
		}),
		pathTool("move_to_trash", "Soft-delete: move the note into trash/. Reversible via restore_from_trash.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.MoveToTrash(ctx, rel)
		}),
		pathTool("restore_from_trash", "Restore a trashed note back to inbox/.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.RestoreFromTrash(ctx, rel)
		}),
		{
			name:        "empty_trash",
			description: "Permanently delete every note in trash/. Confirm with the user before calling: this is irreversible.",
			schema:      `{"type":"object","properties":{"confirm":{"type":"boolean","description":"Must be true. Prevents accidental triggers."}},"required":["confirm"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				if !a.boolIs("confirm") {
					return nil, errors.New("empty_trash requires confirm=true. Ask the user first.")
				}
				if err := b.EmptyTrash(ctx); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true}, nil
			},
		},
		{
			name:        "delete_note",
			description: "Permanently delete a single note file. Prefer move_to_trash unless the user explicitly asked for a hard delete.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"confirm":{"type":"boolean"}},"required":["path","confirm"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				if !a.boolIs("confirm") {
					return nil, errors.New("delete_note requires confirm=true.")
				}
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				if err := b.DeleteNote(ctx, rel); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true}, nil
			},
		},
		{
			name:        "list_comments",
			description: "The comment threads on a note: each top-level comment with the text it is anchored to, the line that text sits on now, who wrote it (author is null for the vault owner), and its replies in order. Read this before reviewing or answering a discussion; unresolved threads only unless include_resolved is true.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative note path."},"include_resolved":{"type":"boolean","description":"Also list resolved threads. Default false."}},"required":["path"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				return backend.ListCommentThreads(ctx, b, rel, a.boolIs("include_resolved"))
			},
		},
		{
			name:        "add_comment",
			description: "Start a new comment thread on a note, attributed to you. Pass anchor_text, a passage copied exactly from the note, to attach the comment to it (the app highlights it and jumps there); omit it for a note-level comment. Markdown is fine in the body. Returns the new thread.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative note path."},"body":{"type":"string","description":"The comment, in Markdown."},"anchor_text":{"type":"string","description":"Text from the note the comment is about, verbatim. Omit for a note-level comment."},"author":{"type":"string","description":"Display name to sign with, such as the assistant's name. Omit to write as the vault owner."}},"required":["path","body"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				body, err := a.requireString("body")
				if err != nil {
					return nil, err
				}
				anchor, _, _ := a.optionalString("anchor_text")
				author, _, _ := a.optionalString("author")
				return backend.AddComment(ctx, b, backend.AddCommentInput{Path: rel, Body: body, AnchorText: anchor, Author: author})
			},
		},
		{
			name:        "reply_to_comment",
			description: "Answer a comment in its thread, attributed to you. id is a thread id (or any reply id in it) from list_comments; the reply keeps the thread's anchor. Use this to respond to the user's comments the way you would on a pull request, instead of editing the note body. Returns the updated thread.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative note path."},"id":{"type":"string","description":"A comment id from list_comments."},"body":{"type":"string","description":"The reply, in Markdown."},"author":{"type":"string","description":"Display name to sign with, such as the assistant's name. Omit to write as the vault owner."}},"required":["path","id","body"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				id, err := a.requireString("id")
				if err != nil {
					return nil, err
				}
				body, err := a.requireString("body")
				if err != nil {
					return nil, err
				}
				author, _, _ := a.optionalString("author")
				return backend.ReplyToComment(ctx, b, backend.ReplyInput{Path: rel, ID: id, Body: body, Author: author})
			},
		},
		{
			name:        "resolve_comment",
			description: "Mark a comment thread resolved (or reopen it with resolved: false). Resolve only when the discussion is settled or the user asks; the thread stays in the note's history and moves to the Resolved section of the app's Comments panel.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative note path."},"id":{"type":"string","description":"A comment id from list_comments."},"resolved":{"type":"boolean","description":"false reopens the thread. Default true."}},"required":["path","id"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				id, err := a.requireString("id")
				if err != nil {
					return nil, err
				}
				resolved := true
				if v, ok := a["resolved"].(bool); ok {
					resolved = v
				}
				return backend.ResolveComment(ctx, b, rel, id, resolved)
			},
		},
		pathTool("archive_note", "Move a note into archive/.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.ArchiveNote(ctx, rel)
		}),
		pathTool("unarchive_note", "Move an archived note back to inbox/.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.UnarchiveNote(ctx, rel)
		}),
		{
			name:        "create_folder",
			description: "Create a subfolder under inbox/, quick/, or archive/.",
			schema:      `{"type":"object","properties":{"folder":{"type":"string","enum":["inbox","quick","archive"]},"subpath":{"type":"string"}},"required":["folder","subpath"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				folder, err := a.requireFolder("folder")
				if err != nil {
					return nil, err
				}
				if folder == vault.FolderTrash {
					return nil, errors.New("Refusing to create subfolders inside trash/")
				}
				sub, err := a.requireString("subpath")
				if err != nil {
					return nil, err
				}
				if err := b.CreateFolder(ctx, folder, sub); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true}, nil
			},
		},
		{
			name:        "rename_folder",
			description: "Rename or move a subfolder in place.",
			schema:      `{"type":"object","properties":{"folder":{"type":"string","enum":["inbox","quick","archive"]},"oldSubpath":{"type":"string"},"newSubpath":{"type":"string"}},"required":["folder","oldSubpath","newSubpath"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				folder, err := a.requireFolder("folder")
				if err != nil {
					return nil, err
				}
				if folder == vault.FolderTrash {
					return nil, errors.New("Cannot rename inside trash/")
				}
				oldSub, err := a.requireString("oldSubpath")
				if err != nil {
					return nil, err
				}
				newSub, err := a.requireString("newSubpath")
				if err != nil {
					return nil, err
				}
				return b.RenameFolder(ctx, folder, oldSub, newSub)
			},
		},
		{
			name:        "delete_folder",
			description: "Delete a subfolder and everything inside it. Destructive. Confirm with the user first.",
			schema:      `{"type":"object","properties":{"folder":{"type":"string","enum":["inbox","quick","archive"]},"subpath":{"type":"string"},"confirm":{"type":"boolean"}},"required":["folder","subpath","confirm"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				if !a.boolIs("confirm") {
					return nil, errors.New("delete_folder requires confirm=true.")
				}
				folder, err := a.requireFolder("folder")
				if err != nil {
					return nil, err
				}
				if folder == vault.FolderTrash {
					return nil, errors.New("Cannot delete inside trash/")
				}
				sub, err := a.requireString("subpath")
				if err != nil {
					return nil, err
				}
				if err := b.DeleteFolder(ctx, folder, sub); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true}, nil
			},
		},
		{
			name:        "search_text",
			description: "Full-text search across live notes (inbox/quick/archive). Returns line-level matches with path, line number, and preview. Cheap first step when the user says \"find\" or \"where did I write\".",
			schema:      `{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"number"}},"required":["query"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				query, err := a.requireString("query")
				if err != nil {
					return nil, err
				}
				limit, has, err := a.optionalNumber("limit")
				if err != nil {
					return nil, err
				}
				if !has {
					limit = 80
				}
				return b.SearchText(ctx, query, clampedLimit(limit))
			},
		},
		{
			name:        "search_by_title",
			description: "Fuzzy-match notes by title (case-insensitive substring). Good for \"open my note about …\".",
			schema:      `{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"number"}},"required":["query"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				query, err := a.requireString("query")
				if err != nil {
					return nil, err
				}
				limit, has, err := a.optionalNumber("limit")
				if err != nil {
					return nil, err
				}
				if !has {
					limit = 20
				}
				needle := strings.ToLower(query)
				all, err := b.ListNotes(ctx)
				if err != nil {
					return nil, err
				}
				matches := []vault.NoteMeta{}
				for _, n := range all {
					if n.Folder != vault.FolderTrash && strings.Contains(strings.ToLower(n.Title), needle) {
						matches = append(matches, n)
					}
				}
				sortByUpdated(matches)
				return capped(matches, limit), nil
			},
		},
		{
			name:        "search_by_tag",
			description: "Find notes carrying a specific inline #tag. Tags are case-insensitive; pass the name with or without the leading #.",
			schema:      `{"type":"object","properties":{"tag":{"type":"string"},"limit":{"type":"number"}},"required":["tag"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				raw, err := a.requireString("tag")
				if err != nil {
					return nil, err
				}
				limit, has, err := a.optionalNumber("limit")
				if err != nil {
					return nil, err
				}
				if !has {
					limit = 200
				}
				tag := strings.ToLower(strings.TrimPrefix(raw, "#"))
				all, err := b.ListNotes(ctx)
				if err != nil {
					return nil, err
				}
				templateFolder := backend.TemplateFolder(ctx, b)
				matches := []vault.NoteMeta{}
				for _, n := range all {
					if n.Folder != vault.FolderTrash && containsFold(n.Tags, tag) && !vault.IsPathInTemplateFolder(n.Path, templateFolder) {
						matches = append(matches, n)
					}
				}
				sortByUpdated(matches)
				return capped(matches, limit), nil
			},
		},
		{
			name:        "list_tags",
			description: "Enumerate every #tag used in the vault (excluding trash) with the count of notes carrying it.",
			schema:      `{"type":"object","properties":{}}`,
			handler: func(ctx context.Context, _ args, b Backend) (any, error) {
				all, err := b.ListNotes(ctx)
				if err != nil {
					return nil, err
				}
				// A note in the template folder contributes no tags (#1024).
				templateFolder := backend.TemplateFolder(ctx, b)
				counts := map[string]int{}
				for _, n := range all {
					if n.Folder == vault.FolderTrash || vault.IsPathInTemplateFolder(n.Path, templateFolder) {
						continue
					}
					for _, t := range n.Tags {
						counts[strings.ToLower(t)]++
					}
				}
				type tagCount struct {
					Tag   string `json:"tag"`
					Count int    `json:"count"`
				}
				out := make([]tagCount, 0, len(counts))
				for tag, count := range counts {
					out = append(out, tagCount{tag, count})
				}
				sort.SliceStable(out, func(i, j int) bool {
					if out[i].Count != out[j].Count {
						return out[i].Count > out[j].Count
					}
					return out[i].Tag < out[j].Tag
				})
				return out, nil
			},
		},
		pathTool("backlinks", "Return every note that links to the given note via [[wikilink]]. Run this before renaming a note: the rename will orphan these links.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.Backlinks(ctx, rel)
		}),
		{
			name:        "list_tasks",
			description: "Return every task (markdown checkbox) in live notes with parsed metadata: due date, priority, @waiting, tags, and status, the column a Kanban board grouped by status shows it in (a task note’s `status:`, a line’s `@status:`, or the frontmatter `status:` of its note). Filter by state, priority, date range, tag, or folder.",
			schema: `{"type":"object","properties":{
"status":{"type":"string","enum":["open","done","waiting","all"],"description":"The task’s state, not its status column. open = not done, cancelled or waiting (default). done = checked. waiting = has @waiting. all = everything."},
"priority":{"type":"string","enum":["high","med","low"],"description":"Only tasks at this priority."},
"dueBefore":{"type":"string","description":"YYYY-MM-DD exclusive upper bound."},
"dueAfter":{"type":"string","description":"YYYY-MM-DD inclusive lower bound."},
"tag":{"type":"string"},
"folder":{"type":"string","enum":["inbox","quick","archive"]},
"tags":{"type":"array","items":{"type":"string"},"description":"Alternative to tag: require ALL of these tags."},
"limit":{"type":"number","description":"Cap the number of tasks returned. Default: all."},
"includeExcluded":{"type":"boolean","description":"Also scan notes opted out of Tasks (frontmatter ` + "`tasks: false`" + ` / ` + "`tasks: note`" + `), folders on the vault's excluded list, its template folder, and notes outside its Task folders. Default false: excluded tasks are invisible."}}}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				status, has, err := a.optionalString("status")
				if err != nil {
					return nil, err
				}
				if !has {
					status = "open"
				}
				priority, _, err := a.optionalString("priority")
				if err != nil {
					return nil, err
				}
				dueBefore, _, err := a.optionalString("dueBefore")
				if err != nil {
					return nil, err
				}
				dueAfter, _, err := a.optionalString("dueAfter")
				if err != nil {
					return nil, err
				}
				tag, hasTag, err := a.optionalString("tag")
				if err != nil {
					return nil, err
				}
				tags, hasTags, err := a.optionalStringArray("tags")
				if err != nil {
					return nil, err
				}
				var folder vault.NoteFolder
				if a["folder"] != nil {
					f, err := a.requireFolder("folder")
					if err != nil {
						return nil, err
					}
					folder = f
				}
				all, err := b.ScanTasks(ctx, vault.ParseTasksOptions{IncludeExcluded: a.boolIs("includeExcluded"), Dialect: vault.DialectCLI})
				if err != nil {
					return nil, err
				}
				out := []vault.Task{}
				for _, t := range all {
					if folder != "" && t.NoteFolder != folder {
						continue
					}
					// A `[-]` cancelled task is abandoned, so it is not open either,
					// as in the desktop app (ZenNotes/zennotes#929).
					if status == "open" && (t.Checked || t.Waiting || t.Forwarded || t.Cancelled) {
						continue
					}
					if status == "done" && !t.Checked {
						continue
					}
					if status == "waiting" && !t.Waiting {
						continue
					}
					if priority != "" && t.Priority != priority {
						continue
					}
					if dueBefore != "" && (t.Due == "" || t.Due >= dueBefore) {
						continue
					}
					if dueAfter != "" && (t.Due == "" || t.Due < dueAfter) {
						continue
					}
					if hasTag && !containsFold(t.Tags, strings.ToLower(tag)) {
						continue
					}
					if hasTags {
						missing := false
						for _, need := range lower(tags) {
							if !containsFold(t.Tags, need) {
								missing = true
								break
							}
						}
						if missing {
							continue
						}
					}
					out = append(out, t)
				}
				if limit, has, err := a.optionalNumber("limit"); err != nil {
					return nil, err
				} else if has {
					out = capped(out, limit)
				}
				return out, nil
			},
		},
		{
			name:        "toggle_task",
			description: "Flip a task’s checkbox state using its id from list_tasks (\"<path>#<index>\"). An id counts the task’s position in its note, so list_tasks again after that note changes. Finishing a task with `@repeat:` writes its next occurrence above it and returns the finished one under its new id. Returns the updated task, or null if the id no longer matches a task.",
			schema:      `{"type":"object","properties":{"id":{"type":"string","description":"Task id: sourcePath#taskIndex."}},"required":["id"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				id, err := a.requireString("id")
				if err != nil {
					return nil, err
				}
				task, err := b.ToggleTask(ctx, id, vault.DialectCLI)
				if err != nil {
					return nil, err
				}
				if task == nil {
					return nil, nil
				}
				return task, nil
			},
		},
		{
			name:        "append_to_note",
			description: "Append markdown to the end of a note (inserts a blank line separator if needed). Preferred for adding entries to daily logs, running lists, meeting notes.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"text":{"type":"string","description":"Markdown to append (no leading newline needed)."}},"required":["path","text"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				text, err := a.requireString("text")
				if err != nil {
					return nil, err
				}
				return b.AppendToNote(ctx, rel, text)
			},
		},
		{
			name:        "prepend_to_note",
			description: "Insert markdown at the top of a note, after any frontmatter block. Good for \"pin this to the top\" or adding a banner.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"text":{"type":"string"}},"required":["path","text"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				text, err := a.requireString("text")
				if err != nil {
					return nil, err
				}
				return b.PrependToNote(ctx, rel, text)
			},
		},
		{
			name:        "insert_at_line",
			description: "Insert text before a given zero-based line number. Line numbers come from read_note or list_tasks (which reports lineNumber).",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"lineNumber":{"type":"number","description":"Zero-based line number."},"text":{"type":"string"}},"required":["path","lineNumber","text"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				line, has, err := a.optionalNumber("lineNumber")
				if err != nil {
					return nil, err
				}
				if !has {
					return nil, errors.New("lineNumber is required")
				}
				text, err := a.requireString("text")
				if err != nil {
					return nil, err
				}
				return b.InsertAtLine(ctx, rel, int(line), text)
			},
		},
		{
			name:        "replace_in_note",
			description: "Literal (non-regex) find-and-replace inside a single note. Returns the number of replacements made. Default scope: first occurrence.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"find":{"type":"string"},"replace":{"type":"string"},"occurrence":{"type":"string","enum":["first","all"],"description":"Default: \"first\"."}},"required":["path","find","replace"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				find, err := a.requireString("find")
				if err != nil {
					return nil, err
				}
				replace, ok := a["replace"].(string)
				if !ok {
					return nil, errors.New("replace must be a string")
				}
				occurrence, _, err := a.optionalString("occurrence")
				if err != nil {
					return nil, err
				}
				meta, count, err := b.ReplaceInNote(ctx, rel, find, replace, occurrence == "all")
				if err != nil {
					return nil, err
				}
				return orderedResult{{"meta", meta}, {"replacements", count}}, nil
			},
		},
	}
}

func pathTool(name, description string, run func(ctx context.Context, b Backend, rel string) (any, error)) toolDef {
	return toolDef{
		name:        name,
		description: description,
		schema:      `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		handler: func(ctx context.Context, a args, b Backend) (any, error) {
			rel, err := a.requireString("path")
			if err != nil {
				return nil, err
			}
			return run(ctx, b, rel)
		},
	}
}

// orderedResult marshals as an object with keys in the given order, the way
// the desktop server's object literals serialize.
type orderedResult []struct {
	key   string
	value any
}

func (r orderedResult) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range r {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(kv.key)
		buf.Write(k)
		buf.WriteByte(':')
		v, err := marshalNoEscape(kv.value)
		if err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimRight(v, "\n"))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToolNames lists every tool, in registration order.
func ToolNames() []string {
	defs := tools()
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.name
	}
	return out
}

// CallTool runs one tool by name: the seam tests use; the stdio server goes
// through the same handlers.
func CallTool(ctx context.Context, name string, a map[string]any, b Backend) (any, error) {
	return CallToolScoped(ctx, name, a, b, nil)
}

// CallToolScoped is CallTool under a scope, as the stdio server runs it: a
// tool the scope does not offer is unknown, and the rest see the vault
// through the scope.
func CallToolScoped(ctx context.Context, name string, a map[string]any, b Backend, scope *Scope) (any, error) {
	if !toolAllowed(name, scope) {
		return nil, fmt.Errorf("Unknown tool: %s", name)
	}
	for _, d := range tools() {
		if d.name == name {
			return d.handler(ctx, args(a), scopeBackend(b, scope))
		}
	}
	return nil, fmt.Errorf("Unknown tool: %s", name)
}

// scopeBackend wraps a call's backend in its scope; full access runs bare.
func scopeBackend(b Backend, scope *Scope) Backend {
	if scope == nil || !scope.Active() {
		return b
	}
	return newScopedBackend(b, *scope)
}

// hostedScopeBackend always wraps, whatever the scope (Options.Hosted).
func hostedScopeBackend(b Backend, scope *Scope, logf func(string, ...any)) Backend {
	s := Scope{Access: AccessWrite}
	if scope != nil {
		s = *scope
	}
	scoped := newScopedBackend(b, s)
	scoped.hosted = true
	scoped.logf = logf
	return scoped
}

// hostedAlwaysRefused are tools a hosted server would list and then refuse for any
// client with a path scope, whatever it asks. Listing them only wastes the model's
// attention. See errAssetBytesOnAServer, errAssetLinksEverywhere and RenameNote.
var hostedAlwaysRefused = map[string]bool{
	"rename_note":     true,
	"read_asset":      true,
	"duplicate_asset": true,
	"rename_asset":    true,
	"move_asset":      true,
}

func hostedToolAllowed(name string, scope *Scope) bool {
	if scope != nil && scope.Restricted() && hostedAlwaysRefused[name] {
		return false
	}
	return toolAllowed(name, scope)
}

// hostedCommentTools write a comment, which Options.CommentAuthor signs.
var hostedCommentTools = map[string]bool{"add_comment": true, "reply_to_comment": true, "resolve_comment": true}

// hostedToolError phrases a failure for a client of a hosted server. The server
// calls itself with its own credentials, so a refusal is nothing the client can
// fix, and the zn advice DescribeToolError gives does not apply.
func hostedToolError(err error, b Backend, baseURL string) string {
	msg := err.Error()
	if b != nil {
		msg = scrubPaths(msg, b)
	}
	status := remote.StatusOf(err)
	if status == 401 || status == 403 {
		return "The ZenNotes server refused this request."
	}
	if status >= 500 {
		return "The ZenNotes server could not do that."
	}
	if baseURL != "" {
		msg = strings.ReplaceAll(msg, baseURL, "the ZenNotes server")
	}
	msg = hostedRequestPrefix.ReplaceAllString(msg, "The ZenNotes server refused this ($1)$2")
	return hostedFileOperation.ReplaceAllString(msg, "")
}

var (
	// hostedRequestPrefix is how the remote client starts an error, naming an internal route.
	hostedRequestPrefix = regexp.MustCompile(`^Remote server request failed \((\d+) [^)]*\) for [^:]*(: )?`)
	// hostedFileOperation is how Go starts a file error, naming a path on the server.
	hostedFileOperation = regexp.MustCompile(`\b(?:open|stat|lstat|mkdir|rename|remove|unlinkat|read|write) [^\s:]*/[^\s:]*: `)
)

// MaxHTTPRequestBytes is the largest MCP request a hosted server should accept:
// the largest note write the server takes, or the largest upload_asset, whichever
// is larger, plus room for the JSON around it. The server still applies its own
// note limit to what the tool sends it.
func MaxHTTPRequestBytes(maxNoteBytes int64) int64 {
	const envelope = 1 << 20
	return max(maxNoteBytes, int64(base64.StdEncoding.EncodedLen(maxUploadAssetBytes))) + envelope
}

// DescribeToolError phrases a failure for the agent. A server that turns
// the request away is the one failure an agent cannot fix by retrying: the
// desktop app keeps its token in the OS secret store, where zn cannot read
// it, so say how to give this process one. b is the backend the call ran
// against, nil when none resolved.
func DescribeToolError(err error, b Backend) string {
	msg := err.Error()
	if b != nil {
		if _, scoped := b.(*scopedBackend); scoped {
			msg = scrubPaths(msg, b)
		}
		b = unwrapBackend(b)
	}
	if status := remote.StatusOf(err); status == 401 || status == 403 {
		serverURL := ""
		if r, ok := b.(*backend.Remote); ok {
			serverURL = r.Client().BaseURL
			// The environment outranks a saved token, so a stale one there
			// keeps failing however often `zn connect` runs.
			if fromEnv := strings.TrimSpace(os.Getenv(backend.RemoteTokenEnv)); fromEnv != "" && r.Client().AuthToken == fromEnv {
				return msg + " The rejected token is ZENNOTES_REMOTE_TOKEN from the MCP server's environment, which wins over one saved with `zn connect`: update it there, or remove it and run `zn connect " + serverURL + " --no-default` once in a terminal."
			}
		}
		return msg + " The MCP server has no valid token for this ZenNotes server; the desktop app keeps its copy in the OS secret store, which zn cannot read. To fix it, " + tokenHint(serverURL)
	}
	return msg
}

// tokenHint says how to hand zn a server token. Since 2.69 the desktop app
// does it itself: Settings > MCP > Server token writes the entry `zn connect`
// would (ZenNotes/zennotes#830), so that comes first, with the version for a
// zn that is newer than the app. On the terminal way `--no-default` matters:
// a plain `zn connect` also makes the server zn's own default, and an MCP
// server in terminal mode would then stop following the app.
func tokenHint(serverURL string) string {
	if serverURL == "" {
		serverURL = "<server-url>"
	}
	return "in the ZenNotes desktop app (2.69 or later), choose Let assistants use it under Settings > MCP > Server token, or run `zn connect " + serverURL + " --no-default` once in a terminal. Either saves the token where zn and this MCP server look, and the next call uses it. Setting ZENNOTES_REMOTE_TOKEN in the MCP server's environment works too."
}

// Payload renders a tool result the way the desktop server does: a string
// as-is, anything else as two-space-indented JSON.
func Payload(result any) (string, error) {
	if s, ok := result.(string); ok {
		return s, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// Run serves MCP (over stdio unless Options names a transport) until the
// client closes the connection.
func Run(ctx context.Context, opts Options) error {
	server := BuildServer(opts)
	transport := opts.Transport
	if transport == nil {
		transport = &sdk.StdioTransport{}
	}
	err := server.Run(ctx, transport)
	if err == nil || errors.Is(err, context.Canceled) || isClientDisconnect(err) {
		return nil
	}
	return err
}

// BuildServer is the MCP server with every tool the scope offers, ready for a
// transport.
func BuildServer(opts Options) *sdk.Server {
	version := opts.Version
	if version == "" {
		version = "0.1.0"
	}
	vaults := &session{
		resolve: opts.ResolveTarget,
		open:    opts.OpenBackend,
		served:  backend.Served,
		scoped:  opts.Scope != nil && opts.Scope.Active(),
	}

	// Tool calls are served concurrently, and most writes read a note first: an
	// append, a task edit, the link rewrite after a rename, a server's whole round
	// trip. Whatever landed between the read and the write would be lost, so writes
	// take turns. One turn rather than one per note, because a rename rewrites any
	// number of notes, and on a server it does so where no lock here can reach.
	writeTurn := opts.WriteTurn
	if writeTurn == nil {
		writeTurn = make(chan struct{}, 1)
	}

	instructions := DefaultInstructions
	if opts.Hosted {
		vaults.served = nil
		vaults.scoped = true
	} else {
		instructions = ResolveInstructions()
	}
	if opts.Scope != nil && opts.Scope.Active() {
		instructions += scopeInstructions(*opts.Scope)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "zennotes", Version: version}, &sdk.ServerOptions{
		Instructions: instructions,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	describeError := func(err error, b Backend) string {
		if opts.Hosted {
			return hostedToolError(err, b, opts.HostedBaseURL)
		}
		return DescribeToolError(err, b)
	}
	if opts.Hosted {
		server.AddReceivingMiddleware(hostedCalls(opts))
	}
	for _, def := range tools() {
		def := def
		// A tool the scope does not offer is not listed, so the model never
		// plans around it, and an SDK call to it is an unknown tool.
		if !toolAllowed(def.name, opts.Scope) || (opts.Hosted && !hostedToolAllowed(def.name, opts.Scope)) {
			continue
		}
		server.AddTool(&sdk.Tool{
			Name:        def.name,
			Description: def.description,
			InputSchema: json.RawMessage(def.schema),
			Annotations: toolAnnotations(def.name),
		}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			a := map[string]any{}
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &a); err != nil {
					return errorResult("Error: invalid arguments: " + err.Error()), nil
				}
				// `null` decodes to no map at all.
				if a == nil {
					a = map[string]any{}
				}
			}
			b, move, err := vaults.backendFor(ctx, def.name == "vault_info")
			if err != nil {
				// A failed open names the vault's location, which a scoped client
				// is not told.
				var pathErr *fs.PathError
				if opts.Scope != nil && errors.As(err, &pathErr) {
					return errorResult("Error: The vault cannot be opened."), nil
				}
				return errorResult("Error: " + describeError(err, b)), nil
			}
			if move != nil {
				ctx = context.WithValue(ctx, previousVaultKey{}, move.from)
			}
			if opts.Hosted {
				b = hostedScopeBackend(b, opts.Scope, opts.Logf)
				if hostedCommentTools[def.name] && opts.CommentAuthor != "" {
					a["author"] = opts.CommentAuthor
				}
			} else {
				b = scopeBackend(b, opts.Scope)
			}
			// An edit to a note is recorded so the user can review it in the app.
			agent := "Assistant"
			if req.Session != nil {
				if params := req.Session.InitializeParams(); params != nil && params.ClientInfo != nil {
					agent = agentForClient(params.ClientInfo.Name)
				}
			}
			if classOf(def.name) != classRead {
				select {
				case writeTurn <- struct{}{}:
					defer func() { <-writeTurn }()
				case <-ctx.Done():
					return errorResult("Error: " + ctx.Err().Error()), nil
				}
			}
			result, err := runRecording(ctx, def.name, args(a), b, agent, func() (any, error) {
				return def.handler(ctx, args(a), b)
			})
			if err != nil {
				return errorResult("Error: " + describeError(err, b)), nil
			}
			// A tool that answers with more than text (read_asset) builds its own.
			if built, ok := result.(*sdk.CallToolResult); ok {
				return built, nil
			}
			payload, err := Payload(result)
			if err != nil {
				return errorResult("Error: " + err.Error()), nil
			}
			// Only now can the assistant be told, so only now does the session move.
			if move != nil {
				vaults.finishMove(move)
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: payload}}}, nil
		})
	}
	return server
}

const (
	// hostedToolSlotWait is how long a call waits for a turn. A client that has hung up
	// is not noticed, since the SDK detaches calls from their requests, so this is finite.
	hostedToolSlotWait = 30 * time.Second
	// hostedCallTimeout bounds one tool call.
	hostedCallTimeout = 90 * time.Second
	// HostedCallLimit is the longest a tool call can take, waiting included. A response's
	// write deadline must be longer, or the answer to a slow call could not be sent.
	HostedCallLimit = hostedToolSlotWait + hostedCallTimeout
)

// hostedCalls answers a call to a tool the scope hides as a tool error rather
// than the protocol error the SDK sends (Options.Hosted), and reports each call.
func hostedCalls(opts Options) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (result sdk.Result, err error) {
			params, ok := req.GetParams().(*sdk.CallToolParamsRaw)
			if method != "tools/call" || !ok {
				return next(ctx, method, req)
			}
			if !hostedToolAllowed(params.Name, opts.Scope) || !knownTool(params.Name) {
				return errorResult("Unknown tool: " + params.Name), nil
			}
			if opts.ToolSlots != nil {
				wait := time.NewTimer(hostedToolSlotWait)
				defer wait.Stop()
				select {
				case opts.ToolSlots <- struct{}{}:
					defer func() { <-opts.ToolSlots }()
				case <-wait.C:
					return errorResult("Error: The ZenNotes server is busy. Try again shortly."), nil
				case <-ctx.Done():
					return errorResult("Error: The call was cancelled."), nil
				}
			}
			ctx, cancel := context.WithTimeout(ctx, hostedCallTimeout)
			defer cancel()
			started := time.Now()
			report := func(outcome string) {
				if opts.LogCall != nil {
					opts.LogCall(params.Name, outcome, time.Since(started))
				}
			}
			// The SDK runs a call on its own goroutine, where a panic ends the whole
			// server. The stack names functions, and the value is left out of the log
			// because it could hold a client's text.
			defer func() {
				if failure := recover(); failure != nil {
					report("panic")
					if opts.Logf != nil {
						opts.Logf("mcp: tool=%s panicked (%T)\n%s", params.Name, failure, debug.Stack())
					}
					result, err = errorResult("Error: The tool failed."), nil
				}
			}()
			result, err = next(ctx, method, req)
			if called, isResult := result.(*sdk.CallToolResult); err == nil && isResult && opts.MaxResponseBytes > 0 {
				if size := encodedResultSize(called); size > opts.MaxResponseBytes {
					result = errorResult(fmt.Sprintf("Error: This answer is too large to send, at %d MiB. Ask for less, such as one section of a note with note_sections, or a lower limit.", size>>20))
				}
			}
			outcome := "ok"
			if called, isResult := result.(*sdk.CallToolResult); err != nil || (isResult && called.IsError) {
				outcome = "error"
			}
			report(outcome)
			return result, err
		}
	}
}

// encodedResultSize is about how many bytes a result takes as JSON, without building it.
func encodedResultSize(result *sdk.CallToolResult) int64 {
	var size int64
	for _, content := range result.Content {
		switch c := content.(type) {
		case *sdk.TextContent:
			size += encodedStringSize(c.Text)
		case *sdk.ImageContent:
			size += int64(base64.StdEncoding.EncodedLen(len(c.Data)))
		case *sdk.AudioContent:
			size += int64(base64.StdEncoding.EncodedLen(len(c.Data)))
		default:
			if raw, err := json.Marshal(content); err == nil {
				size += int64(len(raw))
			}
		}
	}
	return size
}

// encodedStringSize is how many bytes a string takes as JSON, with HTML characters
// escaped, as the SDK writes them.
func encodedStringSize(text string) int64 {
	size := int64(len(text))
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t':
				size++
			case c == '<' || c == '>' || c == '&' || c < 0x20:
				size += 5
			}
			i++
			continue
		}
		r, width := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && width == 1 {
			size += 5
		} else if r == '\u2028' || r == '\u2029' {
			size += 3
		}
		i += width
	}
	return size
}

func knownTool(name string) bool {
	for _, known := range ToolNames() {
		if known == name {
			return true
		}
	}
	return false
}

// isClientDisconnect recognises the SDK's report that the client closed
// the pipe, which is how MCP clients end a session: not an error.
func isClientDisconnect(err error) bool {
	// The SDK sometimes formats EOF with %v rather than wrapping it. Match
	// that exact fallback; the same prefix can accompany real I/O failures.
	return errors.Is(err, io.EOF) || err.Error() == "server is closing: EOF"
}

func errorResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: true}
}
