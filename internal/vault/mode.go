package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SwitchPrimaryMode moves a vault between the classic layout, where notes
// live under the inbox directory, and root mode, where they sit at the
// vault root with the other system folders beside them. The layout drives
// mode detection, so the files move and `primaryNotesLocation` is written
// to match. Favorites that name moved notes are rewritten. It refuses to
// run when a move would overwrite something.
func (v *Vault) SwitchPrimaryMode(target PrimaryNotesLocation) ([]string, error) {
	current := v.PrimaryNotesLocation()
	if target != PrimaryNotesRoot && target != PrimaryNotesInbox {
		return nil, fmt.Errorf("mode must be root or inbox")
	}
	if current == target {
		return nil, nil
	}
	settings := v.Settings()
	paths := settings.SystemFolderPaths
	inboxDir := ResolveFolderPath(FolderInbox, paths)
	inboxAbs := filepath.Join(v.root, filepath.FromSlash(inboxDir))
	hidden := hiddenPrimaryRootNames(paths)
	moved := []string{}

	var plan [][2]string // from, to (absolute)
	if target == PrimaryNotesRoot {
		entries, err := os.ReadDir(inboxAbs)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, e := range entries {
			from := filepath.Join(inboxAbs, e.Name())
			to := filepath.Join(v.root, e.Name())
			if _, err := os.Lstat(to); err == nil {
				return nil, fmt.Errorf("%s already exists at the vault root; move or rename it first", e.Name())
			}
			if _, reserved := hidden[e.Name()]; reserved {
				return nil, fmt.Errorf("%s inside the inbox clashes with a system folder name at the root", e.Name())
			}
			plan = append(plan, [2]string{from, to})
		}
	} else {
		entries, err := os.ReadDir(v.root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if _, reserved := hidden[name]; reserved {
				continue
			}
			if name == inboxDir || strings.HasPrefix(inboxDir, name+"/") {
				continue
			}
			if !e.IsDir() && !strings.EqualFold(filepath.Ext(name), ".md") {
				continue
			}
			from := filepath.Join(v.root, name)
			to := filepath.Join(inboxAbs, name)
			if _, err := os.Lstat(to); err == nil {
				return nil, fmt.Errorf("%s already exists inside the inbox; move or rename it first", name)
			}
			plan = append(plan, [2]string{from, to})
		}
		if err := os.MkdirAll(inboxAbs, 0o755); err != nil {
			return nil, err
		}
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i][0] < plan[j][0] })
	for _, p := range plan {
		if err := os.Rename(p[0], p[1]); err != nil {
			return moved, fmt.Errorf("moving %s: %w", filepath.Base(p[0]), err)
		}
		moved = append(moved, filepath.Base(p[0]))
	}
	if target == PrimaryNotesRoot {
		// An emptied inbox directory would be read as an empty inbox layout.
		_ = os.Remove(inboxAbs)
	}
	prefix := ToPosix(inboxDir) + "/"
	err := v.UpdateSettings(func(raw map[string]any) {
		raw["primaryNotesLocation"] = string(target)
		list, _ := raw["favorites"].([]any)
		next := make([]any, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				continue
			}
			switch {
			case strings.Contains(s, ":"):
				// Folder keys name the bucket, not a path: unchanged.
			case target == PrimaryNotesRoot && strings.HasPrefix(s, prefix):
				s = strings.TrimPrefix(s, prefix)
			case target == PrimaryNotesInbox && !isSystemRelPath(s, paths):
				s = prefix + s
			}
			next = append(next, s)
		}
		raw["favorites"] = next
	})
	if err != nil {
		return moved, err
	}
	v.primaryMu.Lock()
	v.primaryCached = ""
	v.primaryMu.Unlock()
	return moved, nil
}

// isSystemRelPath reports whether a vault-relative path sits under quick,
// archive or trash.
func isSystemRelPath(rel string, paths map[string]string) bool {
	for _, folder := range []NoteFolder{FolderQuick, FolderArchive, FolderTrash} {
		dir := ResolveFolderPath(folder, paths)
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}
