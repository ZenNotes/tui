package backend

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/vault"
)

// Remote is a vault behind a self-hosted server, reached over the same HTTP
// API the desktop and web clients use. Where the server has no route for
// something (there is no task-toggle route, and note creation takes no
// body), the operation is composed from the routes that do exist.
type Remote struct {
	client *remote.Client
	target Target
	dbOps  *database.Ops
}

func newRemote(target Target) *Remote {
	return &Remote{client: remote.NewClient(target.BaseURL, target.AuthToken), target: target}
}

// NewRemoteWithClient is a backend on an existing client, for a server that
// reaches its own API without the network (mcpserver).
func NewRemoteWithClient(client *remote.Client, target Target) *Remote {
	return &Remote{client: client, target: target}
}

// Client exposes the HTTP client for callers that need server-only routes.
func (r *Remote) Client() *remote.Client { return r.client }

// Capabilities is what the server says it supports.
func (r *Remote) Capabilities(ctx context.Context) (map[string]any, error) {
	return r.client.Capabilities(ctx)
}

// ErrLinkedFoldersUnknown says a server does not report which system folders are
// links. A current one always sends the list, empty when there are none.
var ErrLinkedFoldersUnknown = errors.New("the server does not report linked system folders")

// LinkedSystemFolders names the system folders the server reports as links.
func (r *Remote) LinkedSystemFolders(ctx context.Context) ([]string, error) {
	info, err := r.client.GetCurrentVault(ctx)
	if err != nil {
		return nil, err
	}
	if info == nil || info.LinkedSystemFolders == nil {
		return nil, ErrLinkedFoldersUnknown
	}
	return info.LinkedSystemFolders, nil
}

func (r *Remote) Kind() Kind { return KindRemote }
func (r *Remote) Label() string {
	if r.target.Name != "" {
		return fmt.Sprintf("%s (%s)", r.target.Name, r.client.BaseURL)
	}
	return r.client.BaseURL
}
func (r *Remote) Root() string { return "" }

// Describe takes two reads: the vault the server is serving and its layout
// settings. A 401 here is the first thing an unauthenticated session hits.
func (r *Remote) Describe(ctx context.Context) (Description, error) {
	info, err := r.client.GetCurrentVault(ctx)
	if err != nil {
		return Description{}, err
	}
	settings, err := r.client.GetVaultSettings(ctx)
	if err != nil {
		return Description{}, err
	}
	d := Description{
		Kind:                 KindRemote,
		BaseURL:              r.client.BaseURL,
		Name:                 r.target.Name,
		PrimaryNotesLocation: vault.PrimaryNotesInbox,
		AuthConfigured:       r.client.AuthToken != "",
	}
	if info != nil {
		d.VaultPath = info.Root
		d.VaultName = info.Name
	}
	if loc, _ := settings["primaryNotesLocation"].(string); loc == "root" {
		d.PrimaryNotesLocation = vault.PrimaryNotesRoot
	}
	return d, nil
}

func (r *Remote) ListNotes(ctx context.Context) ([]vault.NoteMeta, error) {
	return r.client.ListNotes(ctx)
}
func (r *Remote) ListAssets(ctx context.Context) ([]vault.AssetMeta, error) {
	return r.client.ListAssets(ctx)
}

func (r *Remote) ListComments(ctx context.Context, rel string) ([]vault.NoteComment, error) {
	return r.client.ReadComments(ctx, rel)
}

func (r *Remote) WriteComments(ctx context.Context, rel string, comments []vault.NoteComment) ([]vault.NoteComment, error) {
	return r.client.WriteComments(ctx, rel, comments)
}

func (r *Remote) ReadAsset(ctx context.Context, rel string) ([]byte, error) {
	return r.client.ReadAsset(ctx, rel)
}
func (r *Remote) ListFolders(ctx context.Context) ([]vault.FolderEntry, error) {
	return r.client.ListFolders(ctx)
}
func (r *Remote) ReadNote(ctx context.Context, rel string) (vault.NoteContent, error) {
	return r.client.ReadNote(ctx, rel)
}
func (r *Remote) WriteNote(ctx context.Context, rel, body string) (vault.NoteMeta, error) {
	return r.client.WriteNote(ctx, rel, body)
}
func (r *Remote) WriteNoteIfUnchanged(ctx context.Context, rel, body, baseSha256 string) (vault.NoteMeta, error) {
	return r.client.WriteNoteIfUnchanged(ctx, rel, body, baseSha256)
}

// rewrite is RewriteNote for an edit that always writes.
func (r *Remote) rewrite(ctx context.Context, rel string, edit func(body string) string) (vault.NoteMeta, error) {
	meta, _, err := RewriteNote(ctx, r, rel, func(note vault.NoteContent) (string, bool, error) {
		return edit(note.Body), true, nil
	})
	return meta, err
}

// CreateNote takes two round trips: the server's create route takes no body,
// so a note with content is created and then written, on the version the
// create returned when the server names one.
func (r *Remote) CreateNote(ctx context.Context, folder vault.NoteFolder, title, subpath string, body *string) (vault.NoteMeta, error) {
	if folder == vault.FolderTrash {
		return vault.NoteMeta{}, errors.New("Refusing to create a note directly in trash/")
	}
	meta, err := r.client.CreateNote(ctx, folder, title, subpath)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	if body == nil {
		return meta, nil
	}
	return r.client.WriteNoteIfUnchanged(ctx, meta.Path, *body, meta.Sha256)
}

func (r *Remote) AppendToNote(ctx context.Context, rel, text string) (vault.NoteMeta, error) {
	return r.rewrite(ctx, rel, func(body string) string { return vault.AppendToBody(body, text) })
}

func (r *Remote) PrependToNote(ctx context.Context, rel, text string) (vault.NoteMeta, error) {
	return r.rewrite(ctx, rel, func(body string) string { return vault.PrependToBody(body, text) })
}

func (r *Remote) RenameNote(ctx context.Context, rel, nextTitle string) (vault.NoteMeta, error) {
	meta, err := r.client.RenameNote(ctx, rel, nextTitle)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	followRecordPageRename(r.DatabaseOps(), vault.NormalizeRelPath(rel), meta)
	return meta, nil
}

func (r *Remote) MoveNote(ctx context.Context, rel string, folder vault.NoteFolder, subpath string) (vault.NoteMeta, error) {
	return r.client.MoveNote(ctx, rel, folder, subpath)
}
func (r *Remote) ArchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return r.client.ArchiveNote(ctx, rel)
}
func (r *Remote) UnarchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return r.client.UnarchiveNote(ctx, rel)
}
func (r *Remote) MoveToTrash(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return r.client.MoveToTrash(ctx, rel)
}
func (r *Remote) RestoreFromTrash(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return r.client.RestoreFromTrash(ctx, rel)
}
func (r *Remote) DuplicateNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return r.client.DuplicateNote(ctx, rel)
}
func (r *Remote) DeleteNote(ctx context.Context, rel string) error {
	return r.client.DeleteNote(ctx, rel)
}
func (r *Remote) EmptyTrash(ctx context.Context) error { return r.client.EmptyTrash(ctx) }

func (r *Remote) InsertAtLine(ctx context.Context, rel string, lineNumber int, text string) (vault.NoteMeta, error) {
	return r.rewrite(ctx, rel, func(body string) string { return vault.InsertAtLineInBody(body, lineNumber, text) })
}

// ReplaceInNote: no match means no write, so the receipt comes from the
// listing instead of a round trip that would re-save identical bytes.
func (r *Remote) ReplaceInNote(ctx context.Context, rel, find, replace string, all bool) (vault.NoteMeta, int, error) {
	count := 0
	meta, wrote, err := RewriteNote(ctx, r, rel, func(note vault.NoteContent) (string, bool, error) {
		body, n, err := vault.ReplaceInBody(note.Body, find, replace, all)
		count = n
		return body, n > 0, err
	})
	if err != nil {
		return vault.NoteMeta{}, 0, err
	}
	if !wrote {
		path := vault.NormalizeRelPath(rel)
		notes, err := r.client.ListNotes(ctx)
		if err != nil {
			return vault.NoteMeta{}, 0, err
		}
		for _, n := range notes {
			if n.Path == path {
				return n, 0, nil
			}
		}
		return vault.NoteMeta{}, 0, fmt.Errorf("Note not found: %s", rel)
	}
	return meta, count, nil
}

func (r *Remote) CreateFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error {
	return r.client.CreateFolder(ctx, folder, subpath)
}
func (r *Remote) RenameFolder(ctx context.Context, folder vault.NoteFolder, oldSubpath, newSubpath string) (string, error) {
	return r.client.RenameFolder(ctx, folder, oldSubpath, newSubpath)
}
func (r *Remote) DeleteFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error {
	return r.client.DeleteFolder(ctx, folder, subpath)
}

// SearchText: the server's search route has no limit parameter, so the cap
// is applied here. It runs the server's own engine, so ordering can differ
// from a local vault.
func (r *Remote) SearchText(ctx context.Context, query string, limit int) ([]vault.TextSearchMatch, error) {
	// A server stops at its default limit, so a caller that wants more must say so. A
	// scope filters afterwards, and asks for as many as this allows.
	const defaultServerLimit, largestAsk = 200, 50000
	ask := 0
	if limit > defaultServerLimit {
		ask = min(limit, largestAsk)
	}
	matches, err := r.client.SearchTextUpTo(ctx, query, ask)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (r *Remote) Backlinks(ctx context.Context, rel string) ([]vault.NoteMeta, error) {
	notes, err := r.client.ListNotes(ctx)
	if err != nil {
		return nil, err
	}
	return vault.BacklinksIn(notes, vault.NormalizeRelPath(rel)), nil
}

// The server parses with its own grammar; the dialect only shapes the
// toggle transform applied to the body it hands back.
func (r *Remote) ScanTasks(ctx context.Context, opts vault.ParseTasksOptions) ([]vault.Task, error) {
	return r.client.ScanTasks(ctx, opts.IncludeExcluded)
}
func (r *Remote) ScanTasksForPath(ctx context.Context, rel string, opts vault.ParseTasksOptions) ([]vault.Task, error) {
	return r.client.ScanTasksForPath(ctx, rel, opts.IncludeExcluded)
}

// ToggleTask: no task-toggle endpoint exists, so the note is read, the same
// transform a local toggle applies is applied here, and the server re-parses
// the result.
func (r *Remote) ToggleTask(ctx context.Context, taskID string, dialect vault.TaskDialect) (*vault.Task, error) {
	rel, indexStr, err := vault.SplitTaskID(taskID)
	if err != nil {
		return nil, err
	}
	finalID := taskID
	// Redone from the read on each attempt: a refused write means the note
	// moved, and with it maybe the task's checkbox.
	_, found, err := RewriteNote(ctx, r, rel, func(note vault.NoteContent) (string, bool, error) {
		finalID = taskID
		if indexStr == "task" {
			current, err := r.client.ScanTasksForPath(ctx, rel, true)
			if err != nil {
				return "", false, err
			}
			for _, t := range current {
				if t.ID == taskID {
					return vault.ToggleFileTaskInBody(note.Body, t.Checked, time.Now()), true, nil
				}
			}
			return "", false, nil
		}
		index, err := vault.ParseTaskIndex(taskID, indexStr)
		if err != nil {
			return "", false, err
		}
		next, finalIndex, found := vault.ToggleTaskRepeating(note.Body, index, dialect, time.Now())
		finalID = rel + "#" + strconv.Itoa(finalIndex)
		return next, found, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	after, err := r.client.ScanTasksForPath(ctx, rel, true)
	if err != nil {
		return nil, err
	}
	for _, t := range after {
		if t.ID == finalID {
			return &t, nil
		}
	}
	return nil, nil
}

func (r *Remote) ListTemplates(ctx context.Context) ([]vault.CustomTemplateFile, error) {
	return r.client.ListTemplates(ctx)
}
func (r *Remote) WriteTemplate(ctx context.Context, input vault.WriteTemplateInput) (vault.CustomTemplateFile, error) {
	return r.client.WriteTemplate(ctx, input)
}
func (r *Remote) DeleteTemplate(ctx context.Context, sourcePath string) error {
	return r.client.DeleteTemplate(ctx, sourcePath)
}
func (r *Remote) VaultSettings(ctx context.Context) (vault.VaultSettings, error) {
	raw, err := r.client.GetVaultSettings(ctx)
	if err != nil {
		return vault.VaultSettings{}, err
	}
	return vault.ParseSettings(raw), nil
}
func (r *Remote) UpdateVaultSettings(ctx context.Context, patch func(raw map[string]any)) error {
	raw, err := r.client.GetVaultSettings(ctx)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	patch(raw)
	_, err = r.client.SetVaultSettings(ctx, raw)
	return err
}

type remoteFileOps struct {
	r      *Remote
	reader *remote.AbsenceAwareReader
}

func (f remoteFileOps) ReadFileTextOrNull(rel string) (*string, error) {
	return f.reader.ReadFileTextOrNull(context.Background(), rel)
}
func (f remoteFileOps) WriteFile(rel, text string) error {
	_, err := f.r.client.WriteNote(context.Background(), rel, text)
	return err
}
func (f remoteFileOps) CreateFolder(folder vault.NoteFolder, subpath string) error {
	return f.r.client.CreateFolder(context.Background(), folder, subpath)
}
func (f remoteFileOps) RenameFolder(folder vault.NoteFolder, oldSub, newSub string) (string, error) {
	return f.r.client.RenameFolder(context.Background(), folder, oldSub, newSub)
}
func (f remoteFileOps) ListFolders() ([]vault.FolderEntry, error) {
	return f.r.client.ListFolders(context.Background())
}
func (f remoteFileOps) VaultLayout() (database.Layout, error) {
	settings, err := f.r.client.GetVaultSettings(context.Background())
	if err != nil {
		return database.Layout{}, err
	}
	layout := database.Layout{}
	if loc, _ := settings["primaryNotesLocation"].(string); loc == "root" {
		layout.PrimaryNotesAtRoot = true
	}
	if paths, ok := settings["systemFolderPaths"].(map[string]any); ok {
		layout.SystemFolderPaths = map[string]string{}
		for k, v := range paths {
			if s, ok := v.(string); ok {
				layout.SystemFolderPaths[k] = s
			}
		}
	}
	return layout, nil
}

// DatabaseOps reads through /notes/read, which serves any vault file; a 404
// means absent, anything else is a real failure (the absence reader settles
// servers that answer 500 for both).
func (r *Remote) DatabaseOps() *database.Ops {
	if r.dbOps == nil {
		reader := remote.NewAbsenceAwareReader(func(ctx context.Context, rel string) (string, error) {
			note, err := r.client.ReadNote(ctx, rel)
			if err != nil {
				return "", err
			}
			return note.Body, nil
		})
		r.dbOps = database.NewOps(remoteFileOps{r: r, reader: reader})
	}
	return r.dbOps
}
