package backend

import (
	"context"
	"strings"

	"github.com/ZenNotes/zennotescli/internal/database"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

// Description is where a vault lives and how it is laid out.
type Description struct {
	Kind Kind
	// Root is the local vault directory.
	Root string
	// BaseURL and Name describe a server; VaultPath and VaultName are the
	// vault it serves, as it reports them.
	BaseURL              string
	Name                 string
	VaultPath            string
	VaultName            string
	PrimaryNotesLocation vault.PrimaryNotesLocation
	// AuthConfigured says whether this process holds a token at all.
	AuthConfigured bool
}

// Backend is the set of operations every command is written against.
type Backend interface {
	Kind() Kind
	// Label names the vault in output and errors.
	Label() string
	// Root is the vault directory on disk; empty for a server.
	Root() string

	Describe(ctx context.Context) (Description, error)
	ListNotes(ctx context.Context) ([]vault.NoteMeta, error)
	ListAssets(ctx context.Context) ([]vault.AssetMeta, error)
	// ReadAsset returns an attachment's bytes by vault-relative path.
	ReadAsset(ctx context.Context, rel string) ([]byte, error)
	ListFolders(ctx context.Context) ([]vault.FolderEntry, error)
	ReadNote(ctx context.Context, rel string) (vault.NoteContent, error)
	WriteNote(ctx context.Context, rel, body string) (vault.NoteMeta, error)
	CreateNote(ctx context.Context, folder vault.NoteFolder, title, subpath string, body *string) (vault.NoteMeta, error)
	AppendToNote(ctx context.Context, rel, text string) (vault.NoteMeta, error)
	PrependToNote(ctx context.Context, rel, text string) (vault.NoteMeta, error)
	RenameNote(ctx context.Context, rel, nextTitle string) (vault.NoteMeta, error)
	MoveNote(ctx context.Context, rel string, folder vault.NoteFolder, subpath string) (vault.NoteMeta, error)
	ArchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error)
	UnarchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error)
	MoveToTrash(ctx context.Context, rel string) (vault.NoteMeta, error)
	RestoreFromTrash(ctx context.Context, rel string) (vault.NoteMeta, error)
	DuplicateNote(ctx context.Context, rel string) (vault.NoteMeta, error)
	DeleteNote(ctx context.Context, rel string) error
	EmptyTrash(ctx context.Context) error
	InsertAtLine(ctx context.Context, rel string, lineNumber int, text string) (vault.NoteMeta, error)
	ReplaceInNote(ctx context.Context, rel, find, replace string, all bool) (vault.NoteMeta, int, error)
	CreateFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error
	RenameFolder(ctx context.Context, folder vault.NoteFolder, oldSubpath, newSubpath string) (string, error)
	DeleteFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error
	SearchText(ctx context.Context, query string, limit int) ([]vault.TextSearchMatch, error)
	Backlinks(ctx context.Context, rel string) ([]vault.NoteMeta, error)
	ScanTasks(ctx context.Context, opts vault.ParseTasksOptions) ([]vault.Task, error)
	ScanTasksForPath(ctx context.Context, rel string, opts vault.ParseTasksOptions) ([]vault.Task, error)
	ToggleTask(ctx context.Context, taskID string, dialect vault.TaskDialect) (*vault.Task, error)
	// DatabaseOps composes the CSV database operations over this backend's
	// file IO, so `zn base` writes the identical on-disk format everywhere.
	DatabaseOps() *database.Ops

	ListTemplates(ctx context.Context) ([]vault.CustomTemplateFile, error)
	WriteTemplate(ctx context.Context, input vault.WriteTemplateInput) (vault.CustomTemplateFile, error)
	DeleteTemplate(ctx context.Context, sourcePath string) error
	VaultSettings(ctx context.Context) (vault.VaultSettings, error)
	UpdateVaultSettings(ctx context.Context, patch func(raw map[string]any)) error
}

// Options tune a backend.
type Options struct {
	// SyncTitleHeading is the desktop's "Sync title heading on rename"
	// preference, applied by the local backend.
	SyncTitleHeading bool
}

// New binds a target to a backend.
func New(target Target, opts Options) (Backend, error) {
	if target.Kind == KindRemote {
		return newRemote(target), nil
	}
	return newLocal(target.Root, opts)
}

// followRecordPageRename keeps a database's sidecar pointing at a record
// page after the page's file was renamed: the pages map and the title cell
// both move with it. Best effort: a database that cannot be read leaves the
// rename standing, since the note itself moved correctly.
func followRecordPageRename(ops *database.Ops, oldRel string, meta vault.NoteMeta) {
	formDir := database.FormDirContaining(oldRel)
	if formDir == "" || meta.Path == oldRel {
		return
	}
	csvPath := database.CSVPathForFormDir(formDir)
	doc, err := ops.OpenDatabase(csvPath)
	if err != nil {
		return
	}
	oldKey := vault.NormalizeRelPath(oldRel)
	rowID := ""
	for id, pagePath := range doc.Pages {
		if vault.NormalizeRelPath(pagePath) == oldKey {
			rowID = id
			break
		}
	}
	if rowID == "" {
		return
	}
	if doc.Pages == nil {
		doc.Pages = map[string]string{}
	}
	doc.Pages[rowID] = meta.Path
	if titleField := doc.TitleFieldID(); titleField != "" {
		doc.SetCell(rowID, titleField, meta.Title)
	}
	_, _ = ops.WriteSchema(csvPath, doc)
}

func normalizeFolderSub(subpath string) string {
	return strings.Trim(strings.ReplaceAll(subpath, "\\", "/"), "/")
}
