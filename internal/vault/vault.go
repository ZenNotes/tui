package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Vault is one vault root on disk. Mutating operations serialize through a
// mutex; reads run concurrently.
type Vault struct {
	root     string
	fileMode fs.FileMode
	dirMode  fs.FileMode
	mu       sync.Mutex

	settingsCache settingsCache

	primaryMu      sync.Mutex
	primaryCached  PrimaryNotesLocation
	primaryCheckAt time.Time

	// SyncTitleHeading rewrites a note's leading `# Heading` on rename, the
	// desktop's "Sync title heading on rename" preference. On by default.
	SyncTitleHeading bool

	// RecordPages treats the pages in a database folder as notes everywhere, as the
	// desktop's renderer and a server do. `zn mcp` sets it. The CLI's listings and
	// the TUI draw a database themselves and leave it off.
	RecordPages bool

	// WeekStart is the first day of the week for repeat rules counted in
	// weeks. Monday by default.
	WeekStart time.Weekday
	// Now is the clock the task toggles read; nil means time.Now.
	Now func() time.Time
	// TaskWarning receives a warning when a roll cannot do all of its work.
	// Nil drops the warning.
	TaskWarning func(string)
	// Rand is the source of new block ids; nil uses the global source.
	Rand *rand.Rand
}

// Open binds a Vault to an existing directory.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", abs)
	}
	return &Vault{root: abs, fileMode: 0o644, dirMode: 0o755, SyncTitleHeading: true, WeekStart: time.Monday}, nil
}

// now is the current time on the vault's clock.
func (v *Vault) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Root is the absolute vault directory.
func (v *Vault) Root() string { return v.root }

// Info names the vault the way a server reports it.
func (v *Vault) Info() VaultInfo {
	return VaultInfo{Root: v.root, Name: filepath.Base(v.root)}
}

// AbsPath resolves a vault-relative path, refusing escapes.
func (v *Vault) AbsPath(rel string) (string, error) {
	abs, err := SafeJoin(v.root, rel)
	if err != nil {
		if errors.Is(err, ErrPathEscape) {
			return "", fmt.Errorf("Path escapes vault: %s", rel)
		}
		return "", err
	}
	return abs, nil
}

func (v *Vault) relPosix(abs string) string {
	rel, err := filepath.Rel(v.root, abs)
	if err != nil {
		return ToPosix(abs)
	}
	return ToPosix(rel)
}

// --- layout ---

func countLooseRootContent(root string, paths map[string]string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	// Inference ignores the default system names even after a remap, matching
	// the desktop. Walking an explicitly root-mode vault still treats a
	// vacated default name as an ordinary folder; that is a separate rule.
	hidden := hiddenPrimaryRootNames(nil)
	hidden[defaultFolderPaths[FolderInbox]] = struct{}{}
	custom := map[string]struct{}{}
	for _, name := range paths {
		custom[strings.ToLower(name)] = struct{}{}
	}
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if _, skip := hidden[name]; skip {
			continue
		}
		if _, skip := custom[strings.ToLower(name)]; skip {
			continue
		}
		if entry.IsDir() || (entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(name), ".md")) {
			count++
		}
	}
	return count
}

// PrimaryNotesLocation decides whether the inbox lives under `inbox/` or at
// the vault root. An explicit vault.json setting wins, as it does in the
// desktop app. Only an absent setting needs disk inference. Cached briefly:
// every operation consults it and a root directory scan is not free.
func (v *Vault) PrimaryNotesLocation() PrimaryNotesLocation {
	v.primaryMu.Lock()
	defer v.primaryMu.Unlock()
	if v.primaryCached != "" && time.Since(v.primaryCheckAt) < 2*time.Second {
		return v.primaryCached
	}
	settings := v.Settings()
	paths := settings.SystemFolderPaths
	var result PrimaryNotesLocation
	switch {
	case settings.ExplicitPrimary != "":
		result = settings.ExplicitPrimary
	case countLooseRootContent(v.root, paths) >= 1:
		result = PrimaryNotesRoot
	default:
		result = PrimaryNotesInbox
	}
	v.primaryCached = result
	v.primaryCheckAt = time.Now()
	return result
}

func (v *Vault) invalidateLayout() {
	v.primaryMu.Lock()
	v.primaryCached = ""
	v.primaryMu.Unlock()
}

// PrimaryAtRoot is true in the Obsidian-style layout.
func (v *Vault) PrimaryAtRoot() bool {
	return v.PrimaryNotesLocation() == PrimaryNotesRoot
}

// FolderRoot is the absolute directory holding a bucket's notes.
func (v *Vault) FolderRoot(folder NoteFolder) string {
	paths := v.Settings().SystemFolderPaths
	if folder == FolderInbox && v.PrimaryAtRoot() {
		return v.root
	}
	return filepath.Join(v.root, ResolveFolderPath(folder, paths))
}

// LinkedSystemFolders lists the system folders with a symbolic link on the way
// to them, as vault-relative paths. The walks start at a system folder whatever
// it is, so one that is a link shows another folder's notes under its own name.
// Anything but a real directory or nothing at all counts, so a stat that fails
// reports the folder rather than hiding it.
func (v *Vault) LinkedSystemFolders() []string {
	var linked []string
	for _, folder := range AllFolders {
		rel, err := filepath.Rel(v.root, v.FolderRoot(folder))
		if err != nil || rel == "." {
			continue
		}
		walk := v.root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			walk = filepath.Join(walk, part)
			info, err := os.Lstat(walk)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil || !info.IsDir() {
				linked = append(linked, filepath.ToSlash(rel))
				break
			}
		}
	}
	return linked
}

// FolderDirName is a bucket's on-disk directory name; "" for the inbox in
// root primary mode.
func (v *Vault) FolderDirName(folder NoteFolder) string {
	if folder == FolderInbox && v.PrimaryAtRoot() {
		return ""
	}
	return ResolveFolderPath(folder, v.Settings().SystemFolderPaths)
}

// FolderOf classifies a vault-relative path.
func (v *Vault) FolderOf(rel string) (NoteFolder, bool) {
	return FolderForRelativePath(rel, v.Settings().SystemFolderPaths)
}

func (v *Vault) folderOfAbs(abs string) (NoteFolder, bool) {
	rel, err := filepath.Rel(v.root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return v.FolderOf(rel)
}

// SubpathOf is a note's directory relative to its bucket root.
func (v *Vault) SubpathOf(rel string) string {
	folder, ok := v.FolderOf(rel)
	if !ok {
		return ""
	}
	return FolderSubpathOf(NormalizeRelPath(rel), folder, v.PrimaryAtRoot(), v.Settings().SystemFolderPaths)
}

// RelDirFor is the vault-relative directory for a (folder, subpath).
func (v *Vault) RelDirFor(folder NoteFolder, subpath string) string {
	sub := strings.Trim(strings.ReplaceAll(subpath, "\\", "/"), "/")
	top := v.FolderDirName(folder)
	switch {
	case top == "":
		return sub
	case sub == "":
		return top
	default:
		return top + "/" + sub
	}
}

func errNotInFolder(rel string) error {
	return fmt.Errorf("Note not in a known folder: %s", rel)
}

// --- meta ---

func (v *Vault) readMeta(abs string, folder NoteFolder, preambleFolder string) (NoteMeta, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return NoteMeta{}, err
	}
	body, _ := os.ReadFile(abs)
	rel := v.relPosix(abs)
	created, updated := fileTimes(abs, info)
	created = v.noteCreatedAt(rel, created)
	bodyStr := string(body)
	meta := NoteMeta{
		Path:      rel,
		Link:      BuildOpenNoteDeepLink(rel),
		Title:     strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs)),
		Folder:    folder,
		CreatedAt: created,
		UpdatedAt: updated,
		Size:      info.Size(),
		Tags:      []string{},
		Wikilinks: ExtractWikilinks(bodyStr),
		Excerpt:   BuildExcerpt(bodyStr),
	}
	if !IsTypstPreamblePath(rel, preambleFolder) {
		meta.Tags = ExtractTags(bodyStr)
	}
	return meta, nil
}

// MetaFor re-reads one note's metadata.
func (v *Vault) MetaFor(rel string) (NoteMeta, error) {
	abs, err := v.AbsPath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	folder, ok := v.folderOfAbs(abs)
	if !ok {
		return NoteMeta{}, errNotInFolder(rel)
	}
	return v.readMeta(abs, folder, v.Settings().TypstPreambleFolder)
}

type noteFile struct {
	folder NoteFolder
	abs    string
}

// walkNotes visits every note file of every bucket, in the order the
// desktop CLI does: bucket by bucket, directories in name order, database
// folders and dot directories skipped, the other buckets' directories skipped
// at the root when the root is the inbox.
func (v *Vault) walkNotes(folders []NoteFolder, includeFormDirs bool, visit func(noteFile)) {
	hidden := hiddenPrimaryRootNames(v.Settings().SystemFolderPaths)
	for _, folder := range folders {
		top := v.FolderRoot(folder)
		isPrimaryRoot := folder == FolderInbox && filepath.Clean(top) == filepath.Clean(v.root)
		var walk func(dir string)
		walk = func(dir string) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				name := entry.Name()
				full := filepath.Join(dir, name)
				if entry.IsDir() {
					if strings.HasPrefix(name, ".") {
						continue
					}
					if !includeFormDirs && !v.RecordPages && isFormDirName(name) {
						continue
					}
					if isPrimaryRoot && dir == top {
						if _, skip := hidden[name]; skip {
							continue
						}
					}
					walk(full)
					continue
				}
				if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(name), ".md") {
					visit(noteFile{folder: folder, abs: full})
				}
			}
		}
		walk(top)
	}
}

// ListNotes returns metadata for every note in every bucket.
func (v *Vault) ListNotes() ([]NoteMeta, error) {
	files := []noteFile{}
	v.walkNotes(AllFolders, false, func(f noteFile) { files = append(files, f) })
	preamble := v.Settings().TypstPreambleFolder
	results := make([]NoteMeta, len(files))
	ok := make([]bool, len(files))
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func(i int, f noteFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			meta, err := v.readMeta(f.abs, f.folder, preamble)
			if err != nil {
				return
			}
			results[i] = meta
			ok[i] = true
		}(i, f)
	}
	wg.Wait()
	out := make([]NoteMeta, 0, len(files))
	for i := range results {
		if ok[i] {
			out = append(out, results[i])
		}
	}
	return out, nil
}

func (v *Vault) walkFolders(collectFormDirs bool) []FolderEntry {
	hidden := hiddenPrimaryRootNames(v.Settings().SystemFolderPaths)
	out := []FolderEntry{}
	for _, folder := range AllFolders {
		top := v.FolderRoot(folder)
		isPrimaryRoot := folder == FolderInbox && filepath.Clean(top) == filepath.Clean(v.root)
		var walk func(dir, subpath string)
		walk = func(dir, subpath string) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				name := entry.Name()
				if !entry.IsDir() || strings.HasPrefix(name, ".") {
					continue
				}
				if isPrimaryRoot && dir == top {
					if _, skip := hidden[name]; skip {
						continue
					}
				}
				next := name
				if subpath != "" {
					next = subpath + "/" + name
				}
				if isFormDirName(name) {
					if collectFormDirs {
						out = append(out, FolderEntry{Folder: folder, Subpath: next})
					}
					continue
				}
				if !collectFormDirs {
					out = append(out, FolderEntry{Folder: folder, Subpath: next})
				}
				walk(filepath.Join(dir, name), next)
			}
		}
		walk(top, "")
	}
	return out
}

// ListFolders enumerates every subfolder under each bucket, database
// folders excluded.
func (v *Vault) ListFolders() ([]FolderEntry, error) {
	return v.walkFolders(false), nil
}

// ListDatabaseDirs enumerates every `.base` database folder.
func (v *Vault) ListDatabaseDirs() ([]FolderEntry, error) {
	return v.walkFolders(true), nil
}

// ListAssets includes loose files throughout the vault, like the server.
// Notes, database internals, hidden files and symlinks are not attachments.
func (v *Vault) ListAssets() ([]AssetMeta, error) {
	out := []AssetMeta{}
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || IsAtomicWriteTempPath(name) {
				continue
			}
			full := filepath.Join(dir, name)
			if entry.IsDir() {
				if isFormDirName(name) {
					continue
				}
				if err := walk(full); err != nil {
					return err
				}
				continue
			}
			if !entry.Type().IsRegular() || strings.EqualFold(filepath.Ext(name), ".md") || strings.HasSuffix(strings.ToLower(name), ".excalidraw") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			out = append(out, AssetMeta{
				Path:      v.relPosix(full),
				Name:      name,
				Size:      info.Size(),
				UpdatedAt: info.ModTime().UnixMilli(),
			})
		}
		return nil
	}
	if err := walk(v.root); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// --- read / write ---

// ReadNote returns a note with its body.
func (v *Vault) ReadNote(rel string) (NoteContent, error) {
	abs, folder, err := v.notePath(rel)
	if err != nil {
		return NoteContent{}, err
	}
	// A note is a Markdown file. Anything else in a note folder (a root-mode
	// vault that is also a code checkout holds plenty) is not one to read.
	if !strings.EqualFold(filepath.Ext(abs), ".md") {
		return NoteContent{}, fmt.Errorf("Not a Markdown note: %s", rel)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return NoteContent{}, err
	}
	meta, err := v.readMeta(abs, folder, v.Settings().TypstPreambleFolder)
	if err != nil {
		return NoteContent{}, err
	}
	return NoteContent{NoteMeta: meta, Body: string(body)}, nil
}

// WriteNote replaces a note's body, creating parent directories.
func (v *Vault) WriteNote(rel, body string) (NoteMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Refuse anything outside the note folders BEFORE a byte lands. This used
	// to write first and check after, so write_note on `.zennotes/...` created
	// the file and only then failed: an assistant could plant comments,
	// creation dates or the agent edit records that say what a note was.
	abs, folder, err := v.notePath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	if err := v.writeNoteFileAtomic(abs, []byte(body)); err != nil {
		return NoteMeta{}, err
	}
	v.invalidateLayout()
	return v.readMeta(abs, folder, v.Settings().TypstPreambleFolder)
}

// ReadFileTextOrNull is the raw text of any vault file, database internals
// included; absent is (nil, "", nil) and every other failure is an error.
func (v *Vault) ReadFileTextOrNull(rel string) (*string, error) {
	abs, err := v.AbsPath(rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
				return nil, nil
			}
		}
		return nil, err
	}
	s := string(data)
	return &s, nil
}

// WriteFileText writes any vault file's text, creating parents.
func (v *Vault) WriteFileText(rel, text string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	abs, err := v.AbsPath(rel)
	if err != nil {
		return err
	}
	return v.writeNoteFileAtomic(abs, []byte(text))
}

var titleBadCharsRe = regexp.MustCompile(`[\\/:\x00-\x1f*?"<>|]`)

// SanitizeTitle makes a title safe as a filename on every platform.
func SanitizeTitle(raw string) string {
	t := titleBadCharsRe.ReplaceAllString(raw, "-")
	t = strings.TrimSpace(wsCollapseRe.ReplaceAllString(t, " "))
	runes := []rune(t)
	if len(runes) > 200 {
		t = string(runes[:200])
	}
	// 200 characters of Chinese or emoji is past the 255 bytes most filesystems allow.
	for len(t) > maxTitleBytes {
		_, size := utf8.DecodeLastRuneInString(t)
		t = t[:len(t)-size]
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return "Untitled"
	}
	return t
}

// maxTitleBytes leaves room in a file name for a copy number and an extension.
const maxTitleBytes = 200

// maxUniqueAttempts bounds the search for a free name.
const maxUniqueAttempts = 10000

// uniqueTitle finds a title no note in dir uses. Any answer but "exists" or "does not
// exist" (a name too long, a folder it cannot read) stops the search: the next number
// would get the same answer.
func uniqueTitle(dir, base string) (string, error) {
	candidate := base
	for n := 2; n <= maxUniqueAttempts; n++ {
		if _, err := os.Lstat(filepath.Join(dir, candidate+".md")); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s %d", base, n)
	}
	return "", fmt.Errorf("no free name for %q after %d tries", base, maxUniqueAttempts)
}

// CreateNote creates a note in a bucket (and subpath), picking a
// non-colliding filename. body defaults to `# <title>` plus a blank line.
func (v *Vault) CreateNote(folder NoteFolder, title, subpath string, body *string) (NoteMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if folder == FolderTrash {
		return NoteMeta{}, errors.New("Refusing to create a note directly in trash/")
	}
	if !IsValidFolder(folder) {
		return NoteMeta{}, fmt.Errorf("invalid folder: %s", folder)
	}
	if title == "" {
		title = "Untitled"
	}
	base := SanitizeTitle(title)
	dir, err := v.bucketDir(folder, subpath)
	if err != nil {
		return NoteMeta{}, err
	}
	if err := os.MkdirAll(dir, v.dirMode); err != nil {
		return NoteMeta{}, err
	}
	finalTitle, err := uniqueTitle(dir, base)
	if err != nil {
		return NoteMeta{}, err
	}
	abs := filepath.Join(dir, finalTitle+".md")
	content := "# " + finalTitle + "\n\n"
	if body != nil {
		content = *body
	}
	if err := v.writeNoteFileAtomic(abs, []byte(content)); err != nil {
		return NoteMeta{}, err
	}
	v.invalidateLayout()
	return v.readMeta(abs, folder, v.Settings().TypstPreambleFolder)
}

// RenameNote renames a note in place (same directory). The leading heading
// follows when SyncTitleHeading is on, and every `[[wikilink]]` in the vault
// that resolved to the note is rewritten to the new name.
func (v *Vault) RenameNote(rel, nextTitle string) (NoteMeta, error) {
	return v.RenameNoteRewritingLinksIn(rel, nextTitle, nil)
}

// RenameNoteRewritingLinksIn is RenameNote with the inbound link rewrite
// limited to the notes rewrite accepts; nil accepts every note. A scoped MCP
// client renames this way so its rename never writes into notes it cannot
// see (ZenNotes/tui#21).
func (v *Vault) RenameNoteRewritingLinksIn(rel, nextTitle string, rewrite func(path string) bool) (NoteMeta, error) {
	oldRel := NormalizeRelPath(rel)
	notesBefore, _ := v.ListNotes()
	meta, err := v.renameNoteFile(rel, nextTitle)
	if err != nil {
		return NoteMeta{}, err
	}
	if meta.Path != oldRel {
		v.rewriteInboundWikilinks(notesBefore, oldRel, meta.Title, rewrite)
	}
	return meta, nil
}

func (v *Vault) renameNoteFile(rel, nextTitle string) (NoteMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	abs, folder, err := v.notePath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	dir := filepath.Dir(abs)
	trimmed := SanitizeTitle(nextTitle)
	target := filepath.Join(dir, trimmed+".md")
	if target != abs {
		if dstInfo, statErr := os.Stat(target); statErr == nil {
			srcInfo, err := os.Stat(abs)
			if err != nil {
				return NoteMeta{}, err
			}
			if !os.SameFile(srcInfo, dstInfo) {
				return NoteMeta{}, fmt.Errorf("A note named %q already exists in %s", trimmed, folder)
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return NoteMeta{}, statErr
		}
		if strings.EqualFold(abs, target) {
			tmp := fmt.Sprintf("%s_rename_tmp_%d", abs, time.Now().UnixMilli())
			if err := v.relocateWithMetadata(abs, tmp, false); err != nil {
				return NoteMeta{}, err
			}
			if err := v.relocateWithMetadata(tmp, target, false); err != nil {
				return NoteMeta{}, err
			}
		} else if err := v.relocateWithMetadata(abs, target, false); err != nil {
			return NoteMeta{}, err
		}
		_ = v.moveNoteComments(v.relPosix(abs), v.relPosix(target))
		_ = v.moveAgentEdit(v.relPosix(abs), v.relPosix(target))
	}
	v.syncTitleHeading(abs, target, trimmed)
	return v.readMeta(target, folder, v.Settings().TypstPreambleFolder)
}

// syncTitleHeading rewrites the note's leading `# Heading` to match its new
// filename. Never an Obsidian drawing, and a failure never undoes the rename.
func (v *Vault) syncTitleHeading(sourceAbs, targetAbs, title string) {
	if !v.SyncTitleHeading {
		return
	}
	if IsObsidianExcalidrawPath(sourceAbs) || IsObsidianExcalidrawPath(targetAbs) {
		return
	}
	body, err := os.ReadFile(targetAbs)
	if err != nil || IsObsidianExcalidrawMarkdown(string(body)) {
		return
	}
	next := RetitleLeadingHeading(string(body), title)
	if next != string(body) {
		_ = v.writeNoteFileAtomic(targetAbs, []byte(next))
	}
}

func (v *Vault) rewriteInboundWikilinks(notesBefore []NoteMeta, oldPath, newTitle string, rewrite func(path string) bool) {
	for _, n := range notesBefore {
		if n.Path == oldPath || n.Folder == FolderTrash {
			continue
		}
		if rewrite != nil && !rewrite(n.Path) {
			continue
		}
		linksToIt := false
		for _, t := range n.Wikilinks {
			if r, ok := ResolveWikilink(notesBefore, t); ok && r.Path == oldPath {
				linksToIt = true
				break
			}
		}
		if !linksToIt {
			continue
		}
		content, err := v.ReadNote(n.Path)
		if err != nil {
			continue
		}
		body, changed := RewriteWikilinksForRename(content.Body, notesBefore, oldPath, newTitle)
		if changed > 0 {
			_, _ = v.WriteNote(n.Path, body)
		}
	}
}

func (v *Vault) moveBetweenFolders(rel string, target NoteFolder) (NoteMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// The source is classified first: unchecked, archive_note on
	// `.zennotes/vault.json` carried the settings off to archive/vault.md.
	abs, _, err := v.notePath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	if _, err := os.Stat(abs); err != nil {
		return NoteMeta{}, err
	}
	destDir, err := v.bucketDir(target, v.SubpathOf(v.relPosix(abs)))
	if err != nil {
		return NoteMeta{}, err
	}
	if err := os.MkdirAll(destDir, v.dirMode); err != nil {
		return NoteMeta{}, err
	}
	baseTitle := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	finalTitle, err := uniqueTitle(destDir, baseTitle)
	if err != nil {
		return NoteMeta{}, err
	}
	destAbs := filepath.Join(destDir, finalTitle+".md")
	if err := v.relocateWithMetadata(abs, destAbs, false); err != nil {
		return NoteMeta{}, err
	}
	v.invalidateLayout()
	_ = v.moveNoteComments(v.relPosix(abs), v.relPosix(destAbs))
	_ = v.moveAgentEdit(v.relPosix(abs), v.relPosix(destAbs))
	return v.readMeta(destAbs, target, v.Settings().TypstPreambleFolder)
}

// MoveToTrash soft-deletes a note, keeping its subfolder for a later restore.
func (v *Vault) MoveToTrash(rel string) (NoteMeta, error) {
	return v.moveBetweenFolders(rel, FolderTrash)
}

// RestoreFromTrash moves a trashed note back to the inbox.
func (v *Vault) RestoreFromTrash(rel string) (NoteMeta, error) {
	return v.moveBetweenFolders(rel, FolderInbox)
}

// ArchiveNote moves a note into the archive.
func (v *Vault) ArchiveNote(rel string) (NoteMeta, error) {
	return v.moveBetweenFolders(rel, FolderArchive)
}

// UnarchiveNote moves an archived note back to the inbox.
func (v *Vault) UnarchiveNote(rel string) (NoteMeta, error) {
	return v.moveBetweenFolders(rel, FolderInbox)
}

// MoveNote moves a note to a bucket and subpath.
func (v *Vault) MoveNote(rel string, target NoteFolder, targetSubpath string) (NoteMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !IsValidFolder(target) {
		return NoteMeta{}, fmt.Errorf("invalid folder: %s", target)
	}
	// Both ends must be note folders: an unchecked source let move_note
	// carry `.zennotes/vault.json` into a note folder, where read_note read it.
	oldAbs, folder, err := v.notePath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	destDir, err := v.bucketDir(target, targetSubpath)
	if err != nil {
		return NoteMeta{}, err
	}
	if filepath.Clean(filepath.Dir(oldAbs)) == filepath.Clean(destDir) {
		return v.readMeta(oldAbs, folder, v.Settings().TypstPreambleFolder)
	}
	if err := os.MkdirAll(destDir, v.dirMode); err != nil {
		return NoteMeta{}, err
	}
	ext := filepath.Ext(oldAbs)
	baseTitle := strings.TrimSuffix(filepath.Base(oldAbs), ext)
	finalTitle, err := uniqueTitle(destDir, baseTitle)
	if err != nil {
		return NoteMeta{}, err
	}
	destAbs := filepath.Join(destDir, finalTitle+ext)
	if err := v.relocateWithMetadata(oldAbs, destAbs, false); err != nil {
		return NoteMeta{}, err
	}
	v.invalidateLayout()
	_ = v.moveNoteComments(v.relPosix(oldAbs), v.relPosix(destAbs))
	_ = v.moveAgentEdit(v.relPosix(oldAbs), v.relPosix(destAbs))
	return v.readMeta(destAbs, target, v.Settings().TypstPreambleFolder)
}

// DuplicateNote copies a note next to itself with a " copy" suffix.
func (v *Vault) DuplicateNote(rel string) (NoteMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	abs, folder, err := v.notePath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	dir := filepath.Dir(abs)
	ext := filepath.Ext(abs)
	baseTitle := strings.TrimSuffix(filepath.Base(abs), ext)
	copyTitle, err := uniqueTitle(dir, baseTitle+" copy")
	if err != nil {
		return NoteMeta{}, err
	}
	destAbs := filepath.Join(dir, copyTitle+ext)
	body, err := os.ReadFile(abs)
	if err != nil {
		return NoteMeta{}, err
	}
	if err := v.writeNoteFileAtomic(destAbs, body); err != nil {
		return NoteMeta{}, err
	}
	_ = v.copyNoteComments(v.relPosix(abs), v.relPosix(destAbs))
	return v.readMeta(destAbs, folder, v.Settings().TypstPreambleFolder)
}

// DeleteNote removes a note permanently; a missing file is not an error.
func (v *Vault) DeleteNote(rel string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	abs, _, err := v.notePath(rel)
	if err != nil {
		return err
	}
	// A server answers 404 here; saying "Deleted" for a note that was never
	// there hid typos in scripts, so the local vault reports it too.
	if err := os.Remove(abs); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Note not found: %s", v.relPosix(abs))
	} else if err != nil {
		return err
	}
	v.invalidateLayout()
	if err := v.removeMetadata(v.relPosix(abs), false); err != nil {
		return err
	}
	_ = v.removeAgentEdit(v.relPosix(abs))
	return v.removeNoteComments(v.relPosix(abs))
}

// EmptyTrash permanently deletes everything in the trash bucket.
func (v *Vault) EmptyTrash() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	trashDir := v.FolderRoot(FolderTrash)
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return nil
	}
	trashRel := v.relPosix(trashDir)
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(trashDir, e.Name())); err != nil {
			return err
		}
		if err := v.removeMetadata(trashRel+"/"+e.Name(), e.IsDir()); err != nil {
			return err
		}
		_ = v.removeNoteComments(trashRel + "/" + e.Name())
		_ = v.removeAgentEditTree(trashRel + "/" + e.Name())
	}
	return nil
}

// --- folders ---

func cleanSubpath(subpath string) string {
	return strings.Trim(strings.ReplaceAll(subpath, "\\", "/"), "/")
}

// CreateFolder creates a subfolder under a bucket.
func (v *Vault) CreateFolder(folder NoteFolder, subpath string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !IsValidFolder(folder) {
		return fmt.Errorf("invalid folder: %s", folder)
	}
	abs, err := v.bucketDir(folder, subpath)
	if err != nil {
		return err
	}
	if abs == v.FolderRoot(folder) {
		return errors.New("Folder name is required")
	}
	if err := os.MkdirAll(abs, v.dirMode); err != nil {
		return err
	}
	v.invalidateLayout()
	return nil
}

// RenameFolder renames or moves a subfolder within its bucket and returns
// the new subpath.
func (v *Vault) RenameFolder(folder NoteFolder, oldSubpath, newSubpath string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	oldClean, newClean := cleanSubpath(oldSubpath), cleanSubpath(newSubpath)
	if oldClean == "" || newClean == "" {
		return "", errors.New("Both old and new folder paths are required")
	}
	// Both ends are note folders of this bucket and neither is its root: the
	// internal dir, an attachments dir or another bucket must not be moved
	// into notes, nor a note folder into them.
	base := v.FolderRoot(folder)
	oldAbs, err := v.bucketDir(folder, oldClean)
	if err != nil {
		return "", err
	}
	newAbs, err := v.bucketDir(folder, newClean)
	if err != nil {
		return "", err
	}
	if oldAbs == base || newAbs == base {
		return "", errors.New("Both old and new folder paths are required")
	}
	if newAbs == oldAbs {
		return newClean, nil
	}
	if strings.HasPrefix(newAbs+string(filepath.Separator), oldAbs+string(filepath.Separator)) {
		return "", errors.New("Cannot move a folder into itself")
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), v.dirMode); err != nil {
		return "", err
	}
	if err := v.relocateWithMetadata(oldAbs, newAbs, true); err != nil {
		return "", err
	}
	v.invalidateLayout()
	// The Tasks folder lists name on-disk paths, so they follow the folder: a
	// renamed Task folder left behind would empty Tasks (#962). So does the
	// template folder (ZenNotes/zennotes#1024), and its icon and color.
	oldSub, _ := filepath.Rel(base, oldAbs)
	newSub, _ := filepath.Rel(base, newAbs)
	if err := v.rewriteSettingsForFolderRename(folder, ToPosix(oldSub), ToPosix(newSub), v.relPosix(oldAbs), v.relPosix(newAbs)); err != nil {
		return "", err
	}
	return newClean, nil
}

// DeleteFolder removes a subfolder and everything inside it.
func (v *Vault) DeleteFolder(folder NoteFolder, subpath string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	// `x/..` cleans to the bucket root, which in root primary mode is the
	// whole vault: checked on the resolved directory, not the string.
	abs, err := v.bucketDir(folder, subpath)
	if err != nil {
		return err
	}
	if abs == v.FolderRoot(folder) {
		return errors.New("Cannot delete the top-level folder")
	}
	if err := os.RemoveAll(abs); err != nil {
		return err
	}
	if err := v.removeMetadata(v.relPosix(abs), true); err != nil {
		return err
	}
	v.invalidateLayout()
	return nil
}

// --- search ---

// SearchText is a case-insensitive substring search over every line of
// every live note, capped at limit matches. The template folder's notes are
// placeholders nobody is searching for (ZenNotes/zennotes#1024), so they are
// skipped before the cap, as the app and the server skip them, and the limit
// is spent on real notes.
func (v *Vault) SearchText(query string, limit int) ([]TextSearchMatch, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []TextSearchMatch{}, nil
	}
	if limit <= 0 {
		limit = 80
	}
	needle := strings.ToLower(trimmed)
	templateFolder := v.Settings().TemplateFolder
	out := []TextSearchMatch{}
	v.walkNotes(LiveFolders, true, func(f noteFile) {
		if len(out) >= limit {
			return
		}
		rel := v.relPosix(f.abs)
		if IsPathInTemplateFolder(rel, templateFolder) {
			return
		}
		body, err := os.ReadFile(f.abs)
		if err != nil {
			return
		}
		title := strings.TrimSuffix(filepath.Base(f.abs), filepath.Ext(f.abs))
		for i, line := range strings.Split(string(body), "\n") {
			if len(out) >= limit {
				return
			}
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			text := strings.TrimSpace(wsCollapseRe.ReplaceAllString(line, " "))
			if r := []rune(text); len(r) > 220 {
				text = string(r[:220])
			}
			out = append(out, TextSearchMatch{
				Path:       rel,
				Link:       BuildOpenNoteDeepLink(rel),
				Title:      title,
				Folder:     f.folder,
				LineNumber: i + 1,
				LineText:   text,
			})
		}
	})
	return out, nil
}

// --- tasks ---

// ScanTasks parses every task in every live note, honoring the vault's
// excluded folders, its Task folders (#962), its template folder, whose
// checkboxes are placeholders (ZenNotes/zennotes#1024), and each note's
// `tasks:` opt-out unless asked not to.
func (v *Vault) ScanTasks(opts ParseTasksOptions) ([]Task, error) {
	excluded, included := v.Settings().TasksFolderLists()
	if opts.IncludeExcluded {
		excluded = nil
		included = nil
	}
	files := []noteFile{}
	v.walkNotes(LiveFolders, false, func(f noteFile) { files = append(files, f) })
	groups := make([][]Task, len(files))
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for i, f := range files {
		rel := v.relPosix(f.abs)
		if IsPathExcludedFromTasks(rel, excluded, included) {
			continue
		}
		wg.Add(1)
		go func(i int, f noteFile, rel string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := os.ReadFile(f.abs)
			if err != nil {
				return
			}
			title := strings.TrimSuffix(filepath.Base(f.abs), filepath.Ext(f.abs))
			groups[i] = ParseTasks(rel, title, f.folder, string(body), opts)
		}(i, f, rel)
	}
	wg.Wait()
	out := []Task{}
	for _, g := range groups {
		out = append(out, g...)
	}
	return out, nil
}

// ScanTasksForPath parses one note's tasks. A trashed or unclassifiable note
// contributes nothing, and neither does one the full scan's folder lists
// leave out (an excluded folder, outside the Task folders, or the template
// folder) unless opts asks for it.
func (v *Vault) ScanTasksForPath(rel string, opts ParseTasksOptions) ([]Task, error) {
	abs, err := v.AbsPath(rel)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	folder, ok := v.folderOfAbs(abs)
	if !ok || folder == FolderTrash {
		return []Task{}, nil
	}
	relPosix := v.relPosix(abs)
	if !opts.IncludeExcluded {
		if excluded, included := v.Settings().TasksFolderLists(); IsPathExcludedFromTasks(relPosix, excluded, included) {
			return []Task{}, nil
		}
	}
	title := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	return ParseTasks(relPosix, title, folder, string(body), opts), nil
}

// ToggleOptions tunes ToggleTaskWith.
type ToggleOptions struct {
	// CanWrite says whether the caller may change the note at a vault path.
	// A roll does not write a session copy into a note that it refuses. The
	// roll counts such notes and does not name them. Nil allows every note.
	CanWrite func(rel string) bool
	// Around runs before the roll writes a session copy into a note. The
	// function that it returns runs after the write. Nil does nothing.
	Around func(rel string) func()
}

// ToggleTask flips the task named by an id, counted in the given grammar.
// Exclusion-blind on purpose: an explicit id is an explicit ask. Finishing a
// recurring task writes its next occurrence above it and returns the finished
// one, whose id moved down past it. The remote backend toggles plainly instead.
// Returns (nil, nil) when the id no longer matches a task.
//
// A roll copies the work sessions of the task. The note of the task is
// written first, then each note that holds a session. A note that cannot be
// written gives a warning to TaskWarning, and the roll stands.
func (v *Vault) ToggleTask(taskID string, dialect TaskDialect) (*Task, error) {
	return v.ToggleTaskWith(taskID, dialect, ToggleOptions{})
}

// ToggleTaskWith is ToggleTask with options.
func (v *Vault) ToggleTaskWith(taskID string, dialect TaskDialect, toggle ToggleOptions) (*Task, error) {
	rel, indexStr, err := SplitTaskID(taskID)
	if err != nil {
		return nil, err
	}
	abs, folder, err := v.notePath(rel)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	relPosix := v.relPosix(abs)
	title := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	opts := ParseTasksOptions{IncludeExcluded: true, Dialect: dialect}
	now := v.now()
	input := TaskRollInput{
		Path: relPosix, Body: string(body), Dialect: dialect, Action: RollComplete,
		Now: now, WeekStart: v.WeekStart, Names: v.skipResolver(now), Rand: v.Rand,
	}
	targetIndex := 0
	hasRule := false
	if indexStr == "task" {
		task, found := parseTaskFile(relPosix, title, folder, strings.ReplaceAll(string(body), "\r\n", "\n"), dialect)
		if !found {
			return nil, nil
		}
		input.File = true
		if task.Repeat != "" {
			v.loadRollContext(&input, opts)
		}
	} else {
		if id, byBlockID := strings.CutPrefix(indexStr, "^"); byBlockID {
			// The id names a line by the block id that ends it. No such line is
			// the same as an index that no task has.
			targetIndex = -1
			for _, t := range ParseTasks(relPosix, title, folder, string(body), opts) {
				if t.Kind == "" && t.BlockID == id && id != "" {
					targetIndex = t.TaskIndex
					break
				}
			}
			if targetIndex < 0 {
				return nil, nil
			}
		} else if targetIndex, err = ParseTaskIndex(taskID, indexStr); err != nil {
			return nil, err
		}
		input.Index = targetIndex
		for _, t := range ParseTasks(relPosix, title, folder, string(body), opts) {
			if t.Kind == "" && t.TaskIndex == targetIndex && t.Repeat != "" {
				v.loadRollContext(&input, opts)
				hasRule = true
				break
			}
		}
	}

	plan, found := PlanTaskRoll(input)
	finalIndex := targetIndex + plan.Inserted
	if found && !input.File && plan.Inserted == 0 && !hasRule {
		// A task whose rule the roll above did not read keeps the way the
		// desktop app rolls it. A task with a rule and no date left to land
		// on is only ticked.
		plan.Body, finalIndex, found = ToggleTaskRepeating(string(body), targetIndex, dialect, now)
	}
	if !found {
		return nil, nil
	}
	if _, err := v.WriteNote(relPosix, plan.Body); err != nil {
		return nil, err
	}
	v.writeSessionCopies(plan.Copies, toggle.CanWrite, toggle.Around)

	for _, t := range ParseTasks(relPosix, title, folder, plan.Body, opts) {
		if input.File && t.ID == taskID {
			return &t, nil
		}
		if !input.File && t.Kind == "" && t.TaskIndex == finalIndex {
			return &t, nil
		}
	}
	return nil, nil
}

// loadRollContext fills the parts of a roll input that the sessions need: the
// tasks, the events and the block ids of the vault, and a way to read a note.
func (v *Vault) loadRollContext(in *TaskRollInput, opts ParseTasksOptions) {
	in.Tasks, _ = v.ScanTasks(opts)
	in.Events, _ = v.ScanEvents()
	in.IDs, _ = v.VaultBlockIDs()
	in.Read = func(rel string) (string, bool) {
		abs, err := v.AbsPath(rel)
		if err != nil {
			return "", false
		}
		data, err := os.ReadFile(abs)
		return string(data), err == nil
	}
}

// LoadRollContext fills the parts of a roll input that the work sessions
// need: the tasks, the events, the block ids and a reader for notes. A caller
// that plans a roll itself uses it, as ToggleTask does.
func (v *Vault) LoadRollContext(in *TaskRollInput) {
	v.loadRollContext(in, ParseTasksOptions{IncludeExcluded: true, Dialect: in.Dialect})
}

// SkipResolverAt gives the resolver for the names in `@skip:` fields,
// for a roll at the time now. Events are read at the first name only.
func (v *Vault) SkipResolverAt(now time.Time) SkipResolver { return v.skipResolver(now) }

// writeSessionCopies writes the session copies of a roll, one note at a time.
// A note that the caller may not write is counted in one warning without its
// path. A note that fails to write gets a warning that names it.
func (v *Vault) writeSessionCopies(copies []SessionCopy, canWrite func(string) bool, around func(string) func()) {
	var paths []string
	byPath := map[string][]SessionCopy{}
	for _, c := range copies {
		if _, seen := byPath[c.Path]; !seen {
			paths = append(paths, c.Path)
		}
		byPath[c.Path] = append(byPath[c.Path], c)
	}
	refused := 0
	for _, path := range paths {
		if canWrite != nil && !canWrite(path) {
			refused += len(byPath[path])
			continue
		}
		done := func() {}
		if around != nil {
			done = around(path)
		}
		abs, err := v.AbsPath(path)
		var data []byte
		if err == nil {
			data, err = os.ReadFile(abs)
		}
		if err == nil {
			_, err = v.WriteNote(path, InsertSessionCopies(string(data), byPath[path]))
		}
		done()
		if err != nil {
			v.warnTask(fmt.Sprintf("Could not copy a work session into %s: %v", path, err))
		}
	}
	switch {
	case refused == 1:
		v.warnTask("1 work session in a note that you cannot change was not copied.")
	case refused > 1:
		v.warnTask(fmt.Sprintf("%d work sessions in notes that you cannot change were not copied.", refused))
	}
}

// warnTask gives a warning about a roll to TaskWarning.
func (v *Vault) warnTask(message string) {
	if v.TaskWarning != nil {
		v.TaskWarning(message)
	}
}

// --- convenience edits ---

func (v *Vault) editNote(rel string, transform func(body string) (string, bool, error)) (NoteMeta, error) {
	abs, folder, err := v.notePath(rel)
	if err != nil {
		return NoteMeta{}, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return NoteMeta{}, err
	}
	next, write, err := transform(string(body))
	if err != nil {
		return NoteMeta{}, err
	}
	if write {
		if _, err := v.WriteNote(v.relPosix(abs), next); err != nil {
			return NoteMeta{}, err
		}
	}
	return v.readMeta(abs, folder, v.Settings().TypstPreambleFolder)
}

// AppendToNote adds text to the end of a note after a blank line.
func (v *Vault) AppendToNote(rel, text string) (NoteMeta, error) {
	return v.editNote(rel, func(body string) (string, bool, error) { return AppendToBody(body, text), true, nil })
}

// PrependToNote inserts text at the top, below any frontmatter.
func (v *Vault) PrependToNote(rel, text string) (NoteMeta, error) {
	return v.editNote(rel, func(body string) (string, bool, error) { return PrependToBody(body, text), true, nil })
}

// ReplaceInNote is a literal find-and-replace; no match means no write.
func (v *Vault) ReplaceInNote(rel, find, replace string, all bool) (NoteMeta, int, error) {
	count := 0
	meta, err := v.editNote(rel, func(body string) (string, bool, error) {
		next, n, err := ReplaceInBody(body, find, replace, all)
		count = n
		return next, n > 0, err
	})
	return meta, count, err
}

// InsertAtLine inserts text before a zero-based line number.
func (v *Vault) InsertAtLine(rel string, lineNumber int, text string) (NoteMeta, error) {
	return v.editNote(rel, func(body string) (string, bool, error) {
		return InsertAtLineInBody(body, lineNumber, text), true, nil
	})
}

// Backlinks lists every note that wikilinks to rel.
func (v *Vault) Backlinks(rel string) ([]NoteMeta, error) {
	abs, err := v.AbsPath(rel)
	if err != nil {
		return nil, err
	}
	all, err := v.ListNotes()
	if err != nil {
		return nil, err
	}
	return BacklinksIn(all, v.relPosix(abs)), nil
}

// --- comment sidecars (.zennotes/comments/<path>.comments.json) ---

const noteCommentsSuffix = ".comments.json"

func (v *Vault) commentsPath(rel string) (string, error) {
	return SafeJoin(filepath.Join(v.root, InternalVaultDir, "comments"), ToPosix(rel)+noteCommentsSuffix)
}

func (v *Vault) removeNoteComments(rel string) error {
	abs, err := v.commentsPath(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// moveNoteComments carries a note's comment sidecar along with the note.
// A sidecar already at the destination absorbs the moving comments.
func (v *Vault) moveNoteComments(oldRel, nextRel string) error {
	oldAbs, err := v.commentsPath(oldRel)
	if err != nil {
		return err
	}
	nextAbs, err := v.commentsPath(nextRel)
	if err != nil {
		return err
	}
	if oldAbs == nextAbs {
		return nil
	}
	if _, err := os.Stat(oldAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(nextAbs), v.dirMode); err != nil {
		return err
	}
	if _, err := os.Stat(nextAbs); err == nil {
		existing := readCommentList(nextAbs)
		moving := readCommentList(oldAbs)
		for i := range moving {
			moving[i]["notePath"] = ToPosix(nextRel)
		}
		merged := append(existing, moving...)
		data, err := json.MarshalIndent(map[string]any{"version": 1, "comments": merged}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(nextAbs, data, v.fileMode); err != nil {
			return err
		}
		return os.Remove(oldAbs)
	}
	moving := readCommentList(oldAbs)
	for i := range moving {
		moving[i]["notePath"] = ToPosix(nextRel)
	}
	data, err := json.MarshalIndent(map[string]any{"version": 1, "comments": moving}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(nextAbs, data, v.fileMode); err != nil {
		return err
	}
	return os.Remove(oldAbs)
}

func (v *Vault) copyNoteComments(sourceRel, nextRel string) error {
	sourceAbs, err := v.commentsPath(sourceRel)
	if err != nil {
		return err
	}
	source := readCommentList(sourceAbs)
	if len(source) == 0 {
		return nil
	}
	nextAbs, err := v.commentsPath(nextRel)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for i := range source {
		source[i]["id"] = fmt.Sprintf("comment-%d-%d", now, i)
		source[i]["notePath"] = ToPosix(nextRel)
		source[i]["createdAt"] = now
		source[i]["updatedAt"] = now
	}
	if err := os.MkdirAll(filepath.Dir(nextAbs), v.dirMode); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"version": 1, "comments": source}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(nextAbs, data, v.fileMode)
}

func readCommentList(abs string) []map[string]any {
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	var envelope struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Comments != nil {
		return envelope.Comments
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	return nil
}

// ListCSVFiles finds every database CSV in the vault at any depth: loose
// `.csv` files, and the data.csv of `.base` folders wherever they sit,
// including places the bucket walk does not cover. Hidden directories
// are skipped and `.base` folders are not descended into.
func (v *Vault) ListCSVFiles() ([]string, error) {
	out := []string{}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || IsAtomicWriteTempPath(name) {
				continue
			}
			full := filepath.Join(dir, name)
			if entry.IsDir() {
				if isFormDirName(name) {
					if info, err := os.Stat(filepath.Join(full, "data.csv")); err == nil && info.Mode().IsRegular() {
						out = append(out, v.relPosix(filepath.Join(full, "data.csv")))
					}
					continue
				}
				walk(full)
				continue
			}
			lower := strings.ToLower(name)
			if strings.HasSuffix(lower, ".csv") {
				out = append(out, v.relPosix(full))
			}
		}
	}
	walk(v.root)
	sort.Strings(out)
	return out, nil
}

// RenameFile moves one vault file to a new vault-relative path.
func (v *Vault) RenameFile(oldRel, newRel string) error {
	from, err := SafeJoin(v.root, oldRel)
	if err != nil {
		return err
	}
	to, err := SafeJoin(v.root, newRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	return v.relocateWithMetadata(from, to, info.IsDir())
}
