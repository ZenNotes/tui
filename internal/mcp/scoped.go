package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/search"
	"github.com/ZenNotes/tui/internal/vault"
)

// scopedBackend is a Backend that shows a client only what its scope allows.
// Every tool runs against one, so no handler can skip the check, and a new
// handler written against Backend gets it for free.
//
// A path is resolved against the vault's own listings before the scope
// check, never trusted as given: the listings walk real directories and
// regular files only, so a path through a symlink, a case variant of a hidden
// folder on a case-insensitive volume, or a hidden directory simply has no
// match. Whatever the client is never shown does not exist for it, and the
// error for a hidden path is the same bytes as for a missing one.
//
// One is made per tool call; the listings it reads are cached for that call.
type scopedBackend struct {
	inner Backend
	scope Scope

	once sync.Once
	view *scopeView
	err  error
}

func newScopedBackend(inner Backend, scope Scope) *scopedBackend {
	return &scopedBackend{inner: inner, scope: scope}
}

// unwrapBackend returns the backend a scoped one wraps, so error reporting
// can still recognise a server.
func unwrapBackend(b Backend) Backend {
	if sb, ok := b.(*scopedBackend); ok {
		return sb.inner
	}
	return b
}

// scopeView is the vault as one call sees it.
type scopeView struct {
	paths         map[string]string
	primaryAtRoot bool

	// notes are the visible notes; byPath indexes every listed note.
	notes  []vault.NoteMeta
	byPath map[string]vault.NoteMeta
	// folders indexes every listed folder by its vault-relative directory;
	// allFolders keeps the listing's order.
	folders    map[string]vault.FolderEntry
	allFolders []vault.FolderEntry
	// dirs are the folder paths plus the bucket directories, for spelling
	// lookups.
	dirs []string

	allow        []string
	deny         []string
	missingAllow []string
	ancestors    map[string]bool

	// root is the vault on this machine, where destinations are checked on
	// disk; "" for a server's, whose walker is its own.
	root string
}

func notFound(rel string) error     { return fmt.Errorf("Note not found: %s", rel) }
func folderNotFound(p string) error { return fmt.Errorf("Folder not found: %s", p) }

var errNotOffered = errors.New("That is not available to an assistant with scoped access.")

// staleDenyLogged keeps the stderr hint about a missing deny entry to one
// line per entry for the life of the process.
var staleDenyLogged sync.Map

func (sb *scopedBackend) load(ctx context.Context) (*scopeView, error) {
	sb.once.Do(func() { sb.view, sb.err = sb.build(ctx) })
	return sb.view, sb.err
}

func (sb *scopedBackend) build(ctx context.Context) (*scopeView, error) {
	settings, err := sb.inner.VaultSettings(ctx)
	if err != nil {
		return nil, err
	}
	desc, err := sb.inner.Describe(ctx)
	if err != nil {
		return nil, err
	}
	notes, err := sb.inner.ListNotes(ctx)
	if err != nil {
		return nil, err
	}
	folders, err := sb.inner.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	v := &scopeView{
		paths:         settings.SystemFolderPaths,
		primaryAtRoot: desc.PrimaryNotesLocation == vault.PrimaryNotesRoot,
		byPath:        map[string]vault.NoteMeta{},
		folders:       map[string]vault.FolderEntry{},
		allFolders:    folders,
		ancestors:     map[string]bool{},
	}
	if sb.inner.Kind() == backend.KindLocal {
		v.root = sb.inner.Root()
	}
	for _, f := range vault.AllFolders {
		if dir := v.bucketDir(f); dir != "" {
			v.dirs = append(v.dirs, dir)
		}
	}
	for _, f := range folders {
		rel := v.folderRel(f)
		v.folders[rel] = f
		v.dirs = append(v.dirs, rel)
	}
	known := make([]string, 0, len(notes)+len(v.dirs))
	for _, n := range notes {
		v.byPath[n.Path] = n
		known = append(known, n.Path)
	}
	known = append(known, v.dirs...)

	// Entries take the vault's own spelling, so `--allow-path /projects`
	// finds `Projects/`.
	for _, entry := range sb.scope.Allow {
		canonical, ok := spell(entry, known)
		if !ok {
			v.missingAllow = append(v.missingAllow, entry)
			canonical = entry
		}
		v.allow = append(v.allow, canonical)
		for p := path.Dir(canonical); p != "." && p != "/"; p = path.Dir(p) {
			v.ancestors[p] = true
		}
	}
	// A deny entry that matches nothing was renamed or deleted outside
	// ZenNotes, or belongs to another vault: what it hid may now sit under a
	// new name, so the scope shares nothing until it is fixed. Only stderr,
	// which lands in the client's logs and not in front of the model, names
	// the entry.
	for _, entry := range sb.scope.Deny {
		matched := false
		for _, p := range known {
			if search.Fold(p) == search.Fold(entry) {
				matched = true
				break
			}
		}
		if !matched {
			if _, seen := staleDenyLogged.LoadOrStore(entry, true); !seen {
				fmt.Fprintf(os.Stderr, "zn mcp: --deny-path %s matches nothing in this vault, so the MCP server shares nothing until it is fixed\n", entry)
			}
			return nil, errors.New("This assistant's scope hides a folder or note that does not exist in this vault (renamed or deleted outside ZenNotes, or it belongs to another vault), so nothing is shared until the MCP client's --deny-path entries are fixed.")
		}
		v.deny = append(v.deny, entry)
	}
	for _, n := range notes {
		if v.visible(n.Path) {
			v.notes = append(v.notes, n)
		}
	}
	return v, nil
}

// spell finds the vault's spelling of p among known paths: exact, else the
// one path equal to it after case and accent folding.
func spell(p string, known []string) (string, bool) {
	folded := search.Fold(p)
	match := ""
	count := 0
	for _, k := range known {
		if k == p {
			return k, true
		}
		if search.Fold(k) == folded {
			match = k
			count++
		}
	}
	if count == 1 {
		return match, true
	}
	return "", false
}

func (v *scopeView) bucketDir(f vault.NoteFolder) string {
	if f == vault.FolderInbox && v.primaryAtRoot {
		return ""
	}
	return vault.ResolveFolderPath(f, v.paths)
}

func joinRel(dir, sub string) string {
	switch {
	case dir == "":
		return sub
	case sub == "":
		return dir
	}
	return dir + "/" + sub
}

func (v *scopeView) folderRel(f vault.FolderEntry) string {
	return joinRel(v.bucketDir(f.Folder), f.Subpath)
}

func (v *scopeView) covers(p string) bool {
	if len(v.allow) == 0 {
		return true
	}
	for _, e := range v.allow {
		if inside(p, e) {
			return true
		}
	}
	return false
}

func (v *scopeView) denied(p string) bool {
	for _, e := range v.deny {
		if insideFolded(p, e) {
			return true
		}
	}
	return false
}

func (v *scopeView) visible(p string) bool { return v.covers(p) && !v.denied(p) }

// denyInside says whether a deny entry lies at or below p.
func (v *scopeView) denyInside(p string) bool {
	for _, e := range v.deny {
		if insideFolded(e, p) {
			return true
		}
	}
	return false
}

// cleanRequest turns a client's path into vault-relative POSIX form, or
// refuses it: `.` and `..` segments and hidden names are never resolved.
func cleanRequest(raw string) (string, bool) {
	p := strings.ReplaceAll(raw, "\\", "/")
	parts := []string{}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		if seg == "." || seg == ".." || strings.HasPrefix(seg, ".") || strings.ContainsRune(seg, 0) {
			return "", false
		}
		parts = append(parts, seg)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "/"), true
}

// note resolves a client's note path to a visible listed note.
func (v *scopeView) note(raw string) (vault.NoteMeta, bool) {
	clean, ok := cleanRequest(raw)
	if !ok {
		return vault.NoteMeta{}, false
	}
	meta, ok := v.byPath[clean]
	if !ok {
		folded := search.Fold(clean)
		count := 0
		for p, m := range v.byPath {
			if search.Fold(p) == folded {
				meta = m
				count++
			}
		}
		if count != 1 {
			return vault.NoteMeta{}, false
		}
	}
	if !v.visible(meta.Path) {
		return vault.NoteMeta{}, false
	}
	return meta, true
}

// destination resolves where a note or folder would go, as the folder plus
// its subpath in the vault's spelling, or refuses. The directory must sit in
// the bucket asked for (a subpath never reaches another system folder or a
// hidden one) and inside an allow entry, not merely above one.
func (v *scopeView) destination(folder vault.NoteFolder, rawSub string) (string, error) {
	sub := strings.Trim(strings.ReplaceAll(rawSub, "\\", "/"), "/")
	if sub != "" {
		clean, ok := cleanRequest(sub)
		if !ok {
			return "", v.createRefusal()
		}
		sub = clean
	}
	for _, seg := range strings.Split(sub, "/") {
		// A database folder's insides are its record pages, not notes.
		if strings.HasSuffix(strings.ToLower(seg), ".base") {
			return "", v.createRefusal()
		}
	}
	bucket := v.bucketDir(folder)
	if bucket == "" && sub != "" && vault.IsReservedRootName(strings.SplitN(sub, "/", 2)[0], v.paths) {
		return "", v.createRefusal()
	}
	rel := joinRel(bucket, sub)
	if rel != "" {
		if got, ok := vault.FolderForRelativePath(rel+"/x.md", v.paths); !ok || got != folder {
			return "", v.createRefusal()
		}
	}
	// Existing folders keep their spelling; new segments are created as given.
	spelled := bucket
	firstNew := ""
	if sub != "" {
		for _, seg := range strings.Split(sub, "/") {
			next := joinRel(spelled, seg)
			if s, ok := spell(next, v.dirs); ok {
				next = s
			} else if firstNew == "" {
				firstNew = next
			}
			spelled = next
		}
	}
	if !v.visible(spelled) {
		return "", v.createRefusal()
	}
	// A segment the listings do not know is new only when nothing is there.
	// The walks skip symbolic links, so `Projects/inner -> ../Journal` reads
	// as missing, and creating through it would write into the hidden folder.
	if firstNew != "" && v.root != "" {
		if _, err := os.Lstat(filepath.Join(v.root, filepath.FromSlash(firstNew))); !errors.Is(err, fs.ErrNotExist) {
			return "", v.createRefusal()
		}
	}
	if bucket == "" {
		return spelled, nil
	}
	return strings.TrimPrefix(strings.TrimPrefix(spelled, bucket), "/"), nil
}

// createRefusal is the one answer for every destination a client may not
// use, hidden or not, so it confirms nothing about hidden folders.
func (v *scopeView) createRefusal() error {
	if len(v.allow) > 0 {
		return fmt.Errorf("This assistant can create and move notes only inside: %s.", strings.Join(v.allow, ", "))
	}
	return errors.New("This assistant cannot put notes there.")
}

// folder resolves a client's (bucket, subpath) to a visible listed folder.
func (v *scopeView) folder(folder vault.NoteFolder, rawSub string) (string, vault.FolderEntry, bool) {
	clean, ok := cleanRequest(rawSub)
	if !ok {
		return "", vault.FolderEntry{}, false
	}
	rel, ok := spell(joinRel(v.bucketDir(folder), clean), v.dirs)
	if !ok {
		return "", vault.FolderEntry{}, false
	}
	entry, listed := v.folders[rel]
	if !listed || entry.Folder != folder || !v.visible(rel) {
		return "", vault.FolderEntry{}, false
	}
	return rel, entry, true
}

func (sb *scopedBackend) need(a Access) error {
	if sb.scope.Access < a {
		return accessError(sb.scope)
	}
	return nil
}

// noteFor loads the view and resolves a note, the first step of every
// per-note operation.
func (sb *scopedBackend) noteFor(ctx context.Context, rel string, a Access) (vault.NoteMeta, error) {
	if err := sb.need(a); err != nil {
		return vault.NoteMeta{}, err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	meta, ok := v.note(rel)
	if !ok {
		return vault.NoteMeta{}, notFound(rel)
	}
	return meta, nil
}

// --- Backend ---

func (sb *scopedBackend) Kind() backend.Kind { return sb.inner.Kind() }
func (sb *scopedBackend) Label() string      { return sb.inner.Label() }
func (sb *scopedBackend) Root() string       { return sb.inner.Root() }

func (sb *scopedBackend) Describe(ctx context.Context) (backend.Description, error) {
	return sb.inner.Describe(ctx)
}

func (sb *scopedBackend) ListNotes(ctx context.Context) ([]vault.NoteMeta, error) {
	v, err := sb.load(ctx)
	if err != nil {
		return nil, err
	}
	return append([]vault.NoteMeta{}, v.notes...), nil
}

// ListFolders is the visible folders plus the folders above an allow entry,
// which are listed by name (the entry names them already) so the tree makes
// sense, while nothing directly inside them is visible.
func (sb *scopedBackend) ListFolders(ctx context.Context) ([]vault.FolderEntry, error) {
	v, err := sb.load(ctx)
	if err != nil {
		return nil, err
	}
	out := []vault.FolderEntry{}
	for _, f := range v.allFolders {
		rel := v.folderRel(f)
		if v.visible(rel) || (v.ancestors[rel] && !v.denied(rel)) {
			out = append(out, f)
		}
	}
	return out, nil
}

func (sb *scopedBackend) ListAssets(ctx context.Context) ([]vault.AssetMeta, error) {
	v, err := sb.load(ctx)
	if err != nil {
		return nil, err
	}
	all, err := sb.inner.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := []vault.AssetMeta{}
	for _, a := range all {
		if v.visible(a.Path) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (sb *scopedBackend) ReadAsset(context.Context, string) ([]byte, error) {
	return nil, errNotOffered
}

func (sb *scopedBackend) ReadNote(ctx context.Context, rel string) (vault.NoteContent, error) {
	meta, err := sb.noteFor(ctx, rel, AccessRead)
	if err != nil {
		return vault.NoteContent{}, err
	}
	return sb.inner.ReadNote(ctx, meta.Path)
}

func (sb *scopedBackend) WriteNote(ctx context.Context, rel, body string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.WriteNote(ctx, meta.Path, body)
}

func (sb *scopedBackend) CreateNote(ctx context.Context, folder vault.NoteFolder, title, subpath string, body *string) (vault.NoteMeta, error) {
	if err := sb.need(AccessWrite); err != nil {
		return vault.NoteMeta{}, err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	sub, err := v.destination(folder, subpath)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.CreateNote(ctx, folder, title, sub, body)
}

func (sb *scopedBackend) AppendToNote(ctx context.Context, rel, text string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.AppendToNote(ctx, meta.Path, text)
}

func (sb *scopedBackend) PrependToNote(ctx context.Context, rel, text string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.PrependToNote(ctx, meta.Path, text)
}

// linkScopedRenamer is a backend that can limit a rename's link rewrite to
// some notes. A server rewrites inbound links in every note itself, so a
// rename there would write into notes the client cannot see.
type linkScopedRenamer interface {
	RenameNoteRewritingLinksIn(ctx context.Context, rel, nextTitle string, rewrite func(path string) bool) (vault.NoteMeta, error)
}

func (sb *scopedBackend) RenameNote(ctx context.Context, rel, nextTitle string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	v, _ := sb.load(ctx)
	next := path.Join(path.Dir(meta.Path), vault.SanitizeTitle(nextTitle)+".md")
	if !v.visible(next) {
		return vault.NoteMeta{}, errors.New("This assistant cannot give the note that name.")
	}
	if !sb.scope.Restricted() {
		return sb.inner.RenameNote(ctx, meta.Path, nextTitle)
	}
	renamer, ok := sb.inner.(linkScopedRenamer)
	if !ok {
		return vault.NoteMeta{}, errors.New("Renaming notes on a ZenNotes server is not available to an assistant that sees only part of the vault: the server rewrites links in every note, including ones this assistant cannot see.")
	}
	return renamer.RenameNoteRewritingLinksIn(ctx, meta.Path, nextTitle, v.visible)
}

func (sb *scopedBackend) MoveNote(ctx context.Context, rel string, folder vault.NoteFolder, subpath string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	v, _ := sb.load(ctx)
	sub, err := v.destination(folder, subpath)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.MoveNote(ctx, meta.Path, folder, sub)
}

// Archiving and trashing are lifecycle exits: the note may leave what the
// client sees, and the user can undo either in the app.
func (sb *scopedBackend) ArchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.ArchiveNote(ctx, meta.Path)
}

func (sb *scopedBackend) MoveToTrash(ctx context.Context, rel string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.MoveToTrash(ctx, meta.Path)
}

// restoreTarget checks that a note brought back to the inbox lands where the
// client can see it: the same subpath under the inbox bucket.
func (sb *scopedBackend) restoreTarget(ctx context.Context, rel string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	v, _ := sb.load(ctx)
	sub := vault.FolderSubpathOf(meta.Path, meta.Folder, v.primaryAtRoot, v.paths)
	if _, err := v.destination(vault.FolderInbox, sub); err != nil {
		return vault.NoteMeta{}, err
	}
	return meta, nil
}

func (sb *scopedBackend) UnarchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	meta, err := sb.restoreTarget(ctx, rel)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.UnarchiveNote(ctx, meta.Path)
}

func (sb *scopedBackend) RestoreFromTrash(ctx context.Context, rel string) (vault.NoteMeta, error) {
	meta, err := sb.restoreTarget(ctx, rel)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.RestoreFromTrash(ctx, meta.Path)
}

func (sb *scopedBackend) DuplicateNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.DuplicateNote(ctx, meta.Path)
}

func (sb *scopedBackend) DeleteNote(ctx context.Context, rel string) error {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return err
	}
	return sb.inner.DeleteNote(ctx, meta.Path)
}

func (sb *scopedBackend) EmptyTrash(ctx context.Context) error {
	if err := sb.need(AccessWrite); err != nil {
		return err
	}
	if sb.scope.Restricted() {
		return errNotOffered
	}
	return sb.inner.EmptyTrash(ctx)
}

func (sb *scopedBackend) InsertAtLine(ctx context.Context, rel string, lineNumber int, text string) (vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, err
	}
	return sb.inner.InsertAtLine(ctx, meta.Path, lineNumber, text)
}

func (sb *scopedBackend) ReplaceInNote(ctx context.Context, rel, find, replace string, all bool) (vault.NoteMeta, int, error) {
	meta, err := sb.noteFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.NoteMeta{}, 0, err
	}
	return sb.inner.ReplaceInNote(ctx, meta.Path, find, replace, all)
}

func (sb *scopedBackend) CreateFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error {
	if err := sb.need(AccessWrite); err != nil {
		return err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return err
	}
	sub, err := v.destination(folder, subpath)
	if err != nil {
		return err
	}
	if sub == "" {
		return errors.New("Folder name is required")
	}
	return sb.inner.CreateFolder(ctx, folder, sub)
}

// wholeFolder resolves a folder a client may rename or delete as a whole: it
// must be visible and hold nothing hidden, or the operation would carry or
// destroy what the client cannot see. The refusal for a folder that holds
// something hidden is the one bit this tells a client, documented in the
// design.
func (sb *scopedBackend) wholeFolder(ctx context.Context, folder vault.NoteFolder, subpath string, verb string) (*scopeView, string, error) {
	if err := sb.need(AccessWrite); err != nil {
		return nil, "", err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return nil, "", err
	}
	rel, entry, ok := v.folder(folder, subpath)
	if !ok {
		return nil, "", folderNotFound(subpath)
	}
	if v.denyInside(rel) {
		return nil, "", fmt.Errorf("This assistant cannot %s that folder.", verb)
	}
	return v, entry.Subpath, nil
}

func (sb *scopedBackend) RenameFolder(ctx context.Context, folder vault.NoteFolder, oldSubpath, newSubpath string) (string, error) {
	v, oldSub, err := sb.wholeFolder(ctx, folder, oldSubpath, "rename")
	if err != nil {
		return "", err
	}
	newSub, err := v.destination(folder, newSubpath)
	if err != nil {
		return "", err
	}
	if newSub == "" {
		return "", errors.New("Both old and new folder paths are required")
	}
	return sb.inner.RenameFolder(ctx, folder, oldSub, newSub)
}

func (sb *scopedBackend) DeleteFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error {
	_, sub, err := sb.wholeFolder(ctx, folder, subpath, "delete")
	if err != nil {
		return err
	}
	return sb.inner.DeleteFolder(ctx, folder, sub)
}

// SearchText filters before it limits: the inner walk stops at its limit, so
// filtering its first page would let hidden matches crowd out visible ones,
// and a short page would say how many hidden notes match.
func (sb *scopedBackend) SearchText(ctx context.Context, query string, limit int) ([]vault.TextSearchMatch, error) {
	v, err := sb.load(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 80
	}
	all, err := sb.inner.SearchText(ctx, query, int(^uint(0)>>1))
	if err != nil {
		return nil, err
	}
	out := []vault.TextSearchMatch{}
	for _, m := range all {
		if len(out) >= limit {
			break
		}
		if n, ok := v.byPath[m.Path]; ok && v.visible(n.Path) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (sb *scopedBackend) Backlinks(ctx context.Context, rel string) ([]vault.NoteMeta, error) {
	meta, err := sb.noteFor(ctx, rel, AccessRead)
	if err != nil {
		return nil, err
	}
	v, _ := sb.load(ctx)
	all, err := sb.inner.Backlinks(ctx, meta.Path)
	if err != nil {
		return nil, err
	}
	out := []vault.NoteMeta{}
	for _, n := range all {
		if v.visible(n.Path) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (sb *scopedBackend) filterTasks(v *scopeView, all []vault.Task) []vault.Task {
	out := []vault.Task{}
	for _, t := range all {
		if _, listed := v.byPath[t.SourcePath]; listed && v.visible(t.SourcePath) {
			out = append(out, t)
		}
	}
	return out
}

func (sb *scopedBackend) ScanTasks(ctx context.Context, opts vault.ParseTasksOptions) ([]vault.Task, error) {
	v, err := sb.load(ctx)
	if err != nil {
		return nil, err
	}
	all, err := sb.inner.ScanTasks(ctx, opts)
	if err != nil {
		return nil, err
	}
	return sb.filterTasks(v, all), nil
}

func (sb *scopedBackend) ScanTasksForPath(ctx context.Context, rel string, opts vault.ParseTasksOptions) ([]vault.Task, error) {
	meta, err := sb.noteFor(ctx, rel, AccessRead)
	if err != nil {
		return nil, err
	}
	v, _ := sb.load(ctx)
	all, err := sb.inner.ScanTasksForPath(ctx, meta.Path, opts)
	if err != nil {
		return nil, err
	}
	return sb.filterTasks(v, all), nil
}

func (sb *scopedBackend) ToggleTask(ctx context.Context, taskID string, dialect vault.TaskDialect) (*vault.Task, error) {
	hash := strings.LastIndex(taskID, "#")
	if hash <= 0 {
		return nil, fmt.Errorf("Task not found: %s", taskID)
	}
	meta, err := sb.noteFor(ctx, taskID[:hash], AccessWrite)
	if err != nil {
		return nil, err
	}
	return sb.inner.ToggleTask(ctx, meta.Path+taskID[hash:], dialect)
}

// DatabaseOps is never reached through MCP; there is no database tool.
func (sb *scopedBackend) DatabaseOps() *database.Ops { return nil }

func (sb *scopedBackend) ListComments(ctx context.Context, rel string) ([]vault.NoteComment, error) {
	meta, err := sb.noteFor(ctx, rel, AccessRead)
	if err != nil {
		return nil, err
	}
	return sb.inner.ListComments(ctx, meta.Path)
}

func (sb *scopedBackend) WriteComments(ctx context.Context, rel string, comments []vault.NoteComment) ([]vault.NoteComment, error) {
	meta, err := sb.noteFor(ctx, rel, AccessComment)
	if err != nil {
		return nil, err
	}
	return sb.inner.WriteComments(ctx, meta.Path, comments)
}

func (sb *scopedBackend) ListTemplates(context.Context) ([]vault.CustomTemplateFile, error) {
	return nil, errNotOffered
}

func (sb *scopedBackend) WriteTemplate(context.Context, vault.WriteTemplateInput) (vault.CustomTemplateFile, error) {
	return vault.CustomTemplateFile{}, errNotOffered
}

func (sb *scopedBackend) DeleteTemplate(context.Context, string) error { return errNotOffered }

func (sb *scopedBackend) VaultSettings(ctx context.Context) (vault.VaultSettings, error) {
	return sb.inner.VaultSettings(ctx)
}

func (sb *scopedBackend) UpdateVaultSettings(context.Context, func(raw map[string]any)) error {
	return errNotOffered
}

// accessInfo is the scope as vault_info reports it, with entries in the
// vault's spelling.
func (sb *scopedBackend) accessInfo(ctx context.Context) orderedResult {
	v, err := sb.load(ctx)
	if err != nil {
		return describeScope(sb.scope, sb.scope.Allow, nil, err)
	}
	return describeScope(sb.scope, v.allow, v.missingAllow, nil)
}

// scrubPaths keeps the vault's absolute location out of a scoped client's
// errors: an MCP-only client has no use for it, and it names the user's home
// directory.
func scrubPaths(msg string, b Backend) string {
	root := b.Root()
	if root == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, root+string(os.PathSeparator), "")
	return strings.ReplaceAll(msg, root, "the vault")
}
