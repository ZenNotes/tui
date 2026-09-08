package backend

import (
	"context"
	"os"

	"github.com/ZenNotes/zennotescli/internal/database"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

// Local is a vault on this machine: every method is the vault engine bound
// to one root.
type Local struct {
	vault *vault.Vault
	dbOps *database.Ops
}

func newLocal(root string, opts Options) (*Local, error) {
	v, err := vault.Open(root)
	if err != nil {
		return nil, err
	}
	v.SyncTitleHeading = opts.SyncTitleHeading
	return &Local{vault: v}, nil
}

// Vault exposes the engine for callers that need local-only operations.
func (l *Local) Vault() *vault.Vault { return l.vault }

func (l *Local) Kind() Kind    { return KindLocal }
func (l *Local) Label() string { return l.vault.Root() }
func (l *Local) Root() string  { return l.vault.Root() }

func (l *Local) Describe(context.Context) (Description, error) {
	return Description{
		Kind:                 KindLocal,
		Root:                 l.vault.Root(),
		PrimaryNotesLocation: l.vault.PrimaryNotesLocation(),
	}, nil
}

func (l *Local) ListNotes(context.Context) ([]vault.NoteMeta, error)   { return l.vault.ListNotes() }
func (l *Local) ListAssets(context.Context) ([]vault.AssetMeta, error) { return l.vault.ListAssets() }
func (l *Local) ReadAsset(_ context.Context, rel string) ([]byte, error) {
	abs, err := vault.SafeJoin(l.Root(), rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}
func (l *Local) ListFolders(context.Context) ([]vault.FolderEntry, error) {
	return l.vault.ListFolders()
}
func (l *Local) ReadNote(_ context.Context, rel string) (vault.NoteContent, error) {
	return l.vault.ReadNote(rel)
}
func (l *Local) WriteNote(_ context.Context, rel, body string) (vault.NoteMeta, error) {
	return l.vault.WriteNote(rel, body)
}
func (l *Local) CreateNote(_ context.Context, folder vault.NoteFolder, title, subpath string, body *string) (vault.NoteMeta, error) {
	return l.vault.CreateNote(folder, title, subpath, body)
}
func (l *Local) AppendToNote(_ context.Context, rel, text string) (vault.NoteMeta, error) {
	return l.vault.AppendToNote(rel, text)
}
func (l *Local) PrependToNote(_ context.Context, rel, text string) (vault.NoteMeta, error) {
	return l.vault.PrependToNote(rel, text)
}
func (l *Local) RenameNote(_ context.Context, rel, nextTitle string) (vault.NoteMeta, error) {
	meta, err := l.vault.RenameNote(rel, nextTitle)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	followRecordPageRename(l.DatabaseOps(), vault.NormalizeRelPath(rel), meta)
	return meta, nil
}
func (l *Local) MoveNote(_ context.Context, rel string, folder vault.NoteFolder, subpath string) (vault.NoteMeta, error) {
	return l.vault.MoveNote(rel, folder, subpath)
}
func (l *Local) ArchiveNote(_ context.Context, rel string) (vault.NoteMeta, error) {
	return l.vault.ArchiveNote(rel)
}
func (l *Local) UnarchiveNote(_ context.Context, rel string) (vault.NoteMeta, error) {
	return l.vault.UnarchiveNote(rel)
}
func (l *Local) MoveToTrash(_ context.Context, rel string) (vault.NoteMeta, error) {
	return l.vault.MoveToTrash(rel)
}
func (l *Local) RestoreFromTrash(_ context.Context, rel string) (vault.NoteMeta, error) {
	return l.vault.RestoreFromTrash(rel)
}
func (l *Local) DuplicateNote(_ context.Context, rel string) (vault.NoteMeta, error) {
	return l.vault.DuplicateNote(rel)
}
func (l *Local) DeleteNote(_ context.Context, rel string) error { return l.vault.DeleteNote(rel) }
func (l *Local) EmptyTrash(context.Context) error               { return l.vault.EmptyTrash() }
func (l *Local) InsertAtLine(_ context.Context, rel string, lineNumber int, text string) (vault.NoteMeta, error) {
	return l.vault.InsertAtLine(rel, lineNumber, text)
}
func (l *Local) ReplaceInNote(_ context.Context, rel, find, replace string, all bool) (vault.NoteMeta, int, error) {
	return l.vault.ReplaceInNote(rel, find, replace, all)
}
func (l *Local) CreateFolder(_ context.Context, folder vault.NoteFolder, subpath string) error {
	return l.vault.CreateFolder(folder, subpath)
}
func (l *Local) RenameFolder(_ context.Context, folder vault.NoteFolder, oldSubpath, newSubpath string) (string, error) {
	return l.vault.RenameFolder(folder, oldSubpath, newSubpath)
}
func (l *Local) DeleteFolder(_ context.Context, folder vault.NoteFolder, subpath string) error {
	return l.vault.DeleteFolder(folder, subpath)
}
func (l *Local) SearchText(_ context.Context, query string, limit int) ([]vault.TextSearchMatch, error) {
	return l.vault.SearchText(query, limit)
}
func (l *Local) Backlinks(_ context.Context, rel string) ([]vault.NoteMeta, error) {
	return l.vault.Backlinks(rel)
}
func (l *Local) ScanTasks(_ context.Context, opts vault.ParseTasksOptions) ([]vault.Task, error) {
	return l.vault.ScanTasks(opts)
}
func (l *Local) ScanTasksForPath(_ context.Context, rel string, opts vault.ParseTasksOptions) ([]vault.Task, error) {
	return l.vault.ScanTasksForPath(rel, opts)
}
func (l *Local) ToggleTask(_ context.Context, taskID string, dialect vault.TaskDialect) (*vault.Task, error) {
	return l.vault.ToggleTask(taskID, dialect)
}

func (l *Local) ListTemplates(context.Context) ([]vault.CustomTemplateFile, error) {
	return l.vault.ListTemplates()
}
func (l *Local) WriteTemplate(_ context.Context, input vault.WriteTemplateInput) (vault.CustomTemplateFile, error) {
	return l.vault.WriteTemplate(input)
}
func (l *Local) DeleteTemplate(_ context.Context, sourcePath string) error {
	return l.vault.DeleteTemplate(sourcePath)
}
func (l *Local) VaultSettings(context.Context) (vault.VaultSettings, error) {
	return l.vault.Settings(), nil
}
func (l *Local) UpdateVaultSettings(_ context.Context, patch func(raw map[string]any)) error {
	return l.vault.UpdateSettings(patch)
}

type localFileOps struct{ v *vault.Vault }

func (f localFileOps) ReadFileTextOrNull(rel string) (*string, error) {
	return f.v.ReadFileTextOrNull(rel)
}
func (f localFileOps) WriteFile(rel, text string) error { return f.v.WriteFileText(rel, text) }
func (f localFileOps) CreateFolder(folder vault.NoteFolder, subpath string) error {
	return f.v.CreateFolder(folder, subpath)
}
func (f localFileOps) RenameFolder(folder vault.NoteFolder, oldSub, newSub string) (string, error) {
	return f.v.RenameFolder(folder, oldSub, newSub)
}

// ListFolders hides `.base` dirs on purpose in the engine; database
// discovery needs exactly those, so the composition gets the companion walk.
func (f localFileOps) ListFolders() ([]vault.FolderEntry, error) { return f.v.ListDatabaseDirs() }
func (f localFileOps) ListCSVFiles() ([]string, error)           { return f.v.ListCSVFiles() }
func (f localFileOps) RenameFile(oldRel, newRel string) error    { return f.v.RenameFile(oldRel, newRel) }
func (f localFileOps) VaultLayout() (database.Layout, error) {
	return database.Layout{
		PrimaryNotesAtRoot: f.v.PrimaryAtRoot(),
		SystemFolderPaths:  f.v.Settings().SystemFolderPaths,
	}, nil
}

func (l *Local) DatabaseOps() *database.Ops {
	if l.dbOps == nil {
		l.dbOps = database.NewOps(localFileOps{v: l.vault})
	}
	return l.dbOps
}
