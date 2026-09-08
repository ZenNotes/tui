// Package mcp is the ZenNotes MCP server: the vault operations exposed as
// tools, with a note-taker-focused system instruction, over stdio. Tool
// names, argument shapes and result shapes match the server the desktop app
// bundles, so an installed client keeps working when it launches this
// binary instead.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/remote"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

// Backend is what the tools run against.
type Backend = backend.Backend

// Options configure the server.
type Options struct {
	// ResolveBackend is called lazily and once; when it fails, every tool
	// call reports the error and the next call tries again rather than
	// repeating a stale failure.
	ResolveBackend func() (Backend, error)
	// Version is reported to clients.
	Version string
}

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

func sortByUpdated(notes []vault.NoteMeta) {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].UpdatedAt > notes[j].UpdatedAt })
}

func tools() []toolDef {
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
					return nil, err
				}
				isRoot := description.PrimaryNotesLocation == vault.PrimaryNotesRoot
				pathNotes := "IMPORTANT: Always use the `path` returned by other tools verbatim. Never prepend `inbox/` to a path you got back from list_notes / create_note / read_note. "
				if isRoot {
					pathNotes += "This vault uses ROOT mode: notes for the conceptual `inbox` folder live directly at the vault root (e.g. `MyNote.md`), not under `inbox/`. The folders quick/, archive/, trash/ are real subdirectories at the root."
				} else {
					pathNotes += "This vault uses INBOX mode: notes for the conceptual `inbox` folder live under `inbox/` (e.g. `inbox/MyNote.md`)."
				}
				top := []string{"inbox", "quick", "archive", "trash"}
				if description.Kind == backend.KindRemote {
					notes := "This vault lives on a self-hosted ZenNotes server, the workspace the desktop app currently has open. Every tool works on it through the server API; paths are vault-relative POSIX paths exactly as the server reports them. " + pathNotes
					if !description.AuthConfigured {
						notes += " No token is configured for this server in this process. If calls fail with 401, set ZENNOTES_REMOTE_TOKEN in the MCP server's environment to the token the server was started with."
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
						{"notes", notes},
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
					{"notes", pathNotes},
				}, nil
			},
		},
		{
			name:        "list_notes",
			description: "List notes in the vault with metadata (title, folder, tags, wikilinks, excerpt, timestamps). Optional filters narrow by folder, tag, or wikilink target. Use this before editing to pick the right note. Trashed notes are included only when folder='trash' is explicit. The returned `path` field is the canonical reference — pass it back verbatim to read_note / write_note / move_note. In `primaryNotesLocation: root` vaults, inbox notes have a path WITHOUT an `inbox/` prefix (e.g. `MyNote.md`); never add one. Each note also carries `link`, a clickable zennotes:// URL that opens it in the app; use it verbatim when rendering note titles as markdown links for the user.",
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
					if hasTag && !containsFold(n.Tags, strings.ToLower(tag)) {
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
				if int(limit) >= 0 && len(notes) > int(limit) {
					notes = notes[:int(limit)]
				}
				return notes, nil
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
			description: "List files under the vault’s attachments directory (images, PDFs, audio, video, other binaries). Useful when a note references an asset you need to inspect.",
			schema:      `{"type":"object","properties":{}}`,
			handler: func(ctx context.Context, _ args, b Backend) (any, error) {
				return b.ListAssets(ctx)
			},
		},
		{
			name:        "read_note",
			description: "Read the full body and metadata of a single note. Always read before you write. Use the path verbatim from list_notes / search_* / create_note — never invent or prefix it.",
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
			description: "Overwrite the full body of an existing note. Destructive — prefer append_to_note / replace_in_note for incremental edits. Keep frontmatter intact unless the user asked you to change it.",
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
			description: "Create a new note. Always picks a non-conflicting filename by appending a counter if needed. Use the `path` from the response to refer to the note in any follow-up tool calls — DO NOT reconstruct it from `folder + title`. In root-mode vaults, `folder: \"inbox\"` means the note lands directly at the vault root with a path like \"MyNote.md\" (no inbox/ prefix).",
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
			description: "Permanently delete every note in trash/. Confirm with the user before calling — this is irreversible.",
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
				return b.SearchText(ctx, query, int(limit))
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
				if len(matches) > int(limit) {
					matches = matches[:int(limit)]
				}
				return matches, nil
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
				matches := []vault.NoteMeta{}
				for _, n := range all {
					if n.Folder != vault.FolderTrash && containsFold(n.Tags, tag) {
						matches = append(matches, n)
					}
				}
				sortByUpdated(matches)
				if len(matches) > int(limit) {
					matches = matches[:int(limit)]
				}
				return matches, nil
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
				counts := map[string]int{}
				for _, n := range all {
					if n.Folder == vault.FolderTrash {
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
		pathTool("backlinks", "Return every note that links to the given note via [[wikilink]]. Run this before renaming a note — the rename will orphan these links.", func(ctx context.Context, b Backend, rel string) (any, error) {
			return b.Backlinks(ctx, rel)
		}),
		{
			name:        "list_tasks",
			description: "Return every task (markdown checkbox) in live notes with parsed metadata: due date, priority, @waiting, tags. Filter by status, priority, date range, tag, or folder.",
			schema: `{"type":"object","properties":{
"status":{"type":"string","enum":["open","done","waiting","all"],"description":"open = unchecked & not waiting (default). done = checked. waiting = has @waiting. all = everything."},
"priority":{"type":"string","enum":["high","med","low"],"description":"Only tasks at this priority."},
"dueBefore":{"type":"string","description":"YYYY-MM-DD exclusive upper bound."},
"dueAfter":{"type":"string","description":"YYYY-MM-DD inclusive lower bound."},
"tag":{"type":"string"},
"folder":{"type":"string","enum":["inbox","quick","archive"]},
"tags":{"type":"array","items":{"type":"string"},"description":"Alternative to tag: require ALL of these tags."},
"includeExcluded":{"type":"boolean","description":"Also scan notes opted out of Tasks (frontmatter ` + "`tasks: false`" + ` / ` + "`tasks: note`" + `) and folders on the vault's excluded list. Default false: excluded tasks are invisible."}}}`,
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
					if status == "open" && (t.Checked || t.Waiting || t.Forwarded) {
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
				return out, nil
			},
		},
		{
			name:        "toggle_task",
			description: "Flip a task’s checkbox state using its stable id from list_tasks (\"<path>#<index>\"). Returns the updated task, or null if the id no longer matches a task (content drifted).",
			schema:      `{"type":"object","properties":{"id":{"type":"string","description":"Stable task id: sourcePath#taskIndex."}},"required":["id"]}`,
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
	for _, d := range tools() {
		if d.name == name {
			return d.handler(ctx, args(a), b)
		}
	}
	return nil, fmt.Errorf("Unknown tool: %s", name)
}

// DescribeToolError phrases a failure for the agent. A server that turns
// the request away is the one failure an agent cannot fix by retrying: the
// desktop app keeps its token in the OS secret store, so this process only
// has one if the environment supplied it.
func DescribeToolError(err error) string {
	msg := err.Error()
	if status := remote.StatusOf(err); status == 401 || status == 403 {
		return msg + " The MCP server has no valid token for this ZenNotes server. Set ZENNOTES_REMOTE_TOKEN in the MCP server's environment (the desktop app's copy lives in the OS secret store, which zn cannot read), or run zn with --token."
	}
	return msg
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

// Run serves MCP over stdio until the client closes the pipe.
func Run(ctx context.Context, opts Options) error {
	version := opts.Version
	if version == "" {
		version = "0.1.0"
	}
	var mu sync.Mutex
	var cached Backend
	getBackend := func() (Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		if cached != nil {
			return cached, nil
		}
		b, err := opts.ResolveBackend()
		if err != nil {
			return nil, err
		}
		cached = b
		return b, nil
	}

	server := sdk.NewServer(&sdk.Implementation{Name: "zennotes", Version: version}, &sdk.ServerOptions{
		Instructions: ResolveInstructions(),
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	for _, def := range tools() {
		def := def
		server.AddTool(&sdk.Tool{
			Name:        def.name,
			Description: def.description,
			InputSchema: json.RawMessage(def.schema),
		}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			a := map[string]any{}
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &a); err != nil {
					return errorResult("Error: invalid arguments: " + err.Error()), nil
				}
			}
			b, err := getBackend()
			if err != nil {
				return errorResult("Error: " + DescribeToolError(err)), nil
			}
			result, err := def.handler(ctx, args(a), b)
			if err != nil {
				return errorResult("Error: " + DescribeToolError(err)), nil
			}
			payload, err := Payload(result)
			if err != nil {
				return errorResult("Error: " + err.Error()), nil
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: payload}}}, nil
		})
	}
	return server.Run(ctx, &sdk.StdioTransport{})
}

func errorResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: true}
}
