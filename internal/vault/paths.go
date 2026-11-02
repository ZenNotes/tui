package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathEscape is returned for a vault-relative path that resolves outside
// the vault root, through `..` or a symbolic link.
var ErrPathEscape = errors.New("path escapes vault")

// SafeJoin cleans a user-supplied relative POSIX path and joins it onto root,
// refusing anything that resolves outside root. Existing symlinked components
// must still resolve inside root; components that do not exist yet are left
// alone (they cannot be links until they are created).
func SafeJoin(root, rel string) (string, error) {
	if root == "" {
		return "", errors.New("root is empty")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/"))
	joined := filepath.Join(rootAbs, filepath.FromSlash(cleaned))

	relBack, err := filepath.Rel(rootAbs, joined)
	if err != nil {
		return "", err
	}
	if relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) {
		return "", ErrPathEscape
	}
	rootCanonical, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return joined, nil
	}
	if relBack == "." {
		return joined, nil
	}
	walk := rootAbs
	for _, part := range strings.Split(relBack, string(filepath.Separator)) {
		walk = filepath.Join(walk, part)
		info, err := os.Lstat(walk)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return joined, nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(walk)
			if err != nil {
				return "", err
			}
			relTarget, err := filepath.Rel(rootCanonical, target)
			if err != nil {
				return "", err
			}
			if relTarget == ".." || strings.HasPrefix(relTarget, ".."+string(filepath.Separator)) {
				return "", ErrPathEscape
			}
			walk = target
		}
	}
	return joined, nil
}

// ToPosix converts an OS path to forward slashes.
func ToPosix(p string) string {
	return filepath.ToSlash(p)
}

// NormalizeRelPath turns `./inbox/Note.md` or `\inbox\Note.md` into
// `inbox/Note.md`, the form every listing reports.
func NormalizeRelPath(rel string) string {
	p := strings.ReplaceAll(rel, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return strings.TrimLeft(p, "/")
}

// hiddenPrimaryRootNames are the directory names skipped while walking the
// vault root in root primary mode: asset dirs, the internal dir, and the
// RESOLVED directory of every other system folder. A default name whose
// folder was remapped away (`quick/` once quick lives in `Fast/`) is an
// ordinary user folder and must not be hidden.
func hiddenPrimaryRootNames(paths map[string]string) map[string]struct{} {
	names := map[string]struct{}{}
	for name := range reservedNonSystemRootNames {
		names[name] = struct{}{}
	}
	for _, folder := range []NoteFolder{FolderQuick, FolderArchive, FolderTrash} {
		names[ResolveFolderPath(folder, paths)] = struct{}{}
	}
	return names
}

// isFormDirName reports whether a directory is a `<Name>.base` database.
func isFormDirName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".base")
}

// FolderForRelativePath classifies a vault-relative path by its first
// segment. Root-level files belong to inbox (the root primary layout); hidden
// names and dotfiles are not notes.
func FolderForRelativePath(rel string, paths map[string]string) (NoteFolder, bool) {
	normalized := ToPosix(rel)
	top := strings.SplitN(normalized, "/", 2)[0]
	if top == "" || strings.HasPrefix(top, ".") {
		return "", false
	}
	if folder, ok := SystemFolderForDirName(top, paths); ok {
		return folder, true
	}
	if _, hidden := hiddenPrimaryRootNames(paths)[top]; hidden {
		return "", false
	}
	return FolderInbox, true
}

// isHiddenTopName reports whether a top-level name is one the root walk
// hides: the internal dir, an attachments dir or another bucket's directory.
// A different spelling still is one when it names the same directory on
// disk, as a case variant does on macOS and Windows.
func (v *Vault) isHiddenTopName(name string, paths map[string]string) bool {
	hidden := hiddenPrimaryRootNames(paths)
	if _, ok := hidden[name]; ok {
		return true
	}
	info, err := os.Lstat(filepath.Join(v.root, name))
	if err != nil {
		return false
	}
	for h := range hidden {
		if !strings.EqualFold(h, name) {
			continue
		}
		if hInfo, err := os.Lstat(filepath.Join(v.root, h)); err == nil && os.SameFile(info, hInfo) {
			return true
		}
	}
	return false
}

// classifyPath is the bucket a vault-relative note (isDir false) or folder
// (isDir true) belongs to as the listings see it. They never descend into a
// dot directory at any depth, nor show the internal dir, an attachments dir
// or another bucket's directory as part of the inbox, so neither does this.
func (v *Vault) classifyPath(rel string, isDir bool) (NoteFolder, bool) {
	rel = NormalizeRelPath(ToPosix(rel))
	if rel == "" || rel == "." {
		return "", false
	}
	segments := strings.Split(rel, "/")
	dirs := segments
	if !isDir {
		dirs = segments[:len(segments)-1]
	}
	for _, segment := range dirs {
		if strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	paths := v.Settings().SystemFolderPaths
	folder, ok := FolderForRelativePath(rel, paths)
	if !ok {
		return "", false
	}
	if folder == FolderInbox && v.isHiddenTopName(segments[0], paths) {
		return "", false
	}
	return folder, true
}

// notePath resolves a note a tool reads, edits, moves or deletes, refusing
// one outside the note folders before anything touches it.
func (v *Vault) notePath(rel string) (string, NoteFolder, error) {
	abs, err := v.AbsPath(rel)
	if err != nil {
		return "", "", err
	}
	folder, ok := v.classifyPath(v.relPosix(abs), false)
	if !ok {
		return "", "", errNotInFolder(rel)
	}
	return abs, folder, nil
}

// bucketDir resolves the directory a create, move, rename or delete names by
// bucket and subpath. It must be a folder the listings show in that bucket:
// a subpath through a dot directory, or one that reaches the internal dir, an
// attachments dir or another bucket (in root primary mode the inbox IS the
// vault root, so all of them sit one level down) is refused. A subpath that
// cleans to nothing, `x/..` included, is the bucket root itself, which
// callers that need a subfolder refuse.
func (v *Vault) bucketDir(folder NoteFolder, subpath string) (string, error) {
	base := v.FolderRoot(folder)
	clean := cleanSubpath(subpath)
	if clean == "" {
		return base, nil
	}
	abs, err := SafeJoin(base, clean)
	if err != nil {
		return "", fmt.Errorf("Path escapes vault: %s", clean)
	}
	if abs == base {
		return base, nil
	}
	if got, ok := v.classifyPath(v.relPosix(abs), true); !ok || got != folder {
		return "", fmt.Errorf("Not a note folder: %s", clean)
	}
	return abs, nil
}

// FolderSubpathOf is the note's directory relative to its bucket root, or ""
// at the bucket root. `inbox/Work/Note.md` gives `Work`; in root primary mode
// `Work/Note.md` gives `Work`.
func FolderSubpathOf(rel string, folder NoteFolder, primaryAtRoot bool, paths map[string]string) string {
	dir := ToPosix(filepath.Dir(rel))
	if dir == "." {
		return ""
	}
	if folder == FolderInbox && primaryAtRoot {
		return dir
	}
	top := ResolveFolderPath(folder, paths)
	if strings.EqualFold(dir, top) {
		return ""
	}
	if len(dir) > len(top)+1 && strings.EqualFold(dir[:len(top)], top) && dir[len(top)] == '/' {
		return dir[len(top)+1:]
	}
	return ""
}
