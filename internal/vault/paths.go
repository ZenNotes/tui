package vault

import (
	"errors"
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
