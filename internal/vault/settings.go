package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	AssetsDir             = "assets"
	PrimaryAttachmentsDir = "attachements" // intentional legacy spelling, never "fix" it
	InternalVaultDir      = ".zennotes"
	vaultSettingsFile     = "vault.json"

	DefaultDailyNotesDirectory     = "Daily Notes"
	DefaultDailyNoteTitlePattern   = "yyyy-MM-dd"
	DefaultWeeklyNotesDirectory    = "Weekly Notes"
	DefaultWeeklyNoteTitlePattern  = "yyyy-'W'ww"
	DefaultMonthlyNotesDirectory   = "Monthly Notes"
	DefaultMonthlyNoteTitlePattern = "yyyy-MM"
	DefaultDateNoteLocale          = "system"
	DefaultTypstPreambleFolder     = "typst"
)

var legacyAttachmentsDirs = []string{PrimaryAttachmentsDir, "_assets"}

// attachmentsDirs are every directory whose files are assets, canonical first.
var attachmentsDirs = []string{AssetsDir, PrimaryAttachmentsDir, "_assets"}

// reservedNonSystemRootNames stay reserved however the system folders are
// remapped: asset dirs and the internal dir are never user note folders.
var reservedNonSystemRootNames = map[string]struct{}{
	AssetsDir:             {},
	PrimaryAttachmentsDir: {},
	"_assets":             {},
	InternalVaultDir:      {},
}

var reservedFolderPathNames = map[string]struct{}{
	"assets":         {},
	".zennotes":      {},
	"attachements":   {},
	"_assets":        {},
	"deleted-assets": {},
	"comments":       {},
}

// DateNotePattern is one (directory, title pattern, locale) triple a
// periodic-note kind has used; the current one plus any legacy ones.
type DateNotePattern struct {
	Directory    string `json:"directory"`
	TitlePattern string `json:"titlePattern,omitempty"`
	Locale       string `json:"locale,omitempty"`
}

// PeriodicNotes are the daily, weekly or monthly note settings.
type PeriodicNotes struct {
	Enabled        bool
	Directory      string
	TitlePattern   string
	Locale         string
	LegacyPatterns []DateNotePattern
	TemplateID     string
	// Daily only.
	TasksDueOnNoteDate      bool
	RolloverUnfinishedTasks bool
}

// VaultSettings is the parsed, normalized `.zennotes/vault.json`.
type VaultSettings struct {
	// ExplicitPrimary is the file's own primaryNotesLocation, empty when the
	// file says nothing. Only an absent value is inferred from the layout; see
	// (*Vault).PrimaryNotesLocation.
	ExplicitPrimary   PrimaryNotesLocation
	DailyNotes        PeriodicNotes
	WeeklyNotes       PeriodicNotes
	MonthlyNotes      PeriodicNotes
	Favorites         []string
	SystemFolderPaths map[string]string
	// TasksExcludedFolders are vault-relative directory paths whose notes
	// never feed the Tasks surfaces.
	TasksExcludedFolders []string
	// TasksIncludedFolders, the Task folders (#962), narrow Tasks to the
	// notes inside them. Empty means the whole vault.
	TasksIncludedFolders []string
	// TemplateFolder is `templates.folder` (#1024), a vault-relative path as
	// it is on disk; "" when the vault has none. Its notes stay out of Tasks
	// and text search; see template_folder.go.
	TemplateFolder      string
	TypstPreambleFolder string
	// Raw is the decoded file, kept so a targeted write can preserve every
	// key this process does not model.
	Raw map[string]any
}

type cachedSettings struct {
	settings VaultSettings
	modTime  time.Time
	size     int64
}

type settingsCache struct {
	mu     sync.Mutex
	cached *cachedSettings
}

func (v *Vault) settingsPath() string {
	return filepath.Join(v.root, InternalVaultDir, vaultSettingsFile)
}

// Settings reads vault.json, reparsing only when the file changed. A missing
// or unreadable file yields the defaults: every read path must keep working
// for a vault that has never been opened in the app.
func (v *Vault) Settings() VaultSettings {
	file, err := os.Open(v.settingsPath())
	if err != nil {
		return normalizeVaultSettings(nil)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return normalizeVaultSettings(nil)
	}
	v.settingsCache.mu.Lock()
	cached := v.settingsCache.cached
	v.settingsCache.mu.Unlock()
	if cached != nil && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.settings
	}
	var raw map[string]any
	if err := json.NewDecoder(file).Decode(&raw); err != nil {
		return normalizeVaultSettings(nil)
	}
	settings := normalizeVaultSettings(raw)
	v.settingsCache.mu.Lock()
	v.settingsCache.cached = &cachedSettings{settings: settings, modTime: info.ModTime(), size: info.Size()}
	v.settingsCache.mu.Unlock()
	return settings
}

// UpdateSettings applies patch to the raw vault.json object and writes it
// back, preserving keys this process does not model. Used for favorites and
// the tasks exclusion list.
func (v *Vault) UpdateSettings(patch func(raw map[string]any)) error {
	raw := map[string]any{}
	if data, err := os.ReadFile(v.settingsPath()); err == nil {
		_ = json.Unmarshal(data, &raw)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	patch(raw)
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(v.settingsPath()), v.dirMode); err != nil {
		return err
	}
	if err := writeFileAtomic(v.settingsPath(), append(data, '\n'), v.fileMode, v.dirMode); err != nil {
		return err
	}
	v.settingsCache.mu.Lock()
	v.settingsCache.cached = nil
	v.settingsCache.mu.Unlock()
	return nil
}

func normalizeVaultSettings(raw map[string]any) VaultSettings {
	s := VaultSettings{
		Raw:                 raw,
		TypstPreambleFolder: DefaultTypstPreambleFolder,
	}
	if raw == nil {
		s.Raw = map[string]any{}
	}
	switch stringOf(raw["primaryNotesLocation"]) {
	case "root":
		s.ExplicitPrimary = PrimaryNotesRoot
	case "inbox":
		s.ExplicitPrimary = PrimaryNotesInbox
	}
	s.DailyNotes = normalizePeriodic(mapOf(raw["dailyNotes"]), DefaultDailyNotesDirectory, DefaultDailyNoteTitlePattern, true)
	s.WeeklyNotes = normalizePeriodic(mapOf(raw["weeklyNotes"]), DefaultWeeklyNotesDirectory, DefaultWeeklyNoteTitlePattern, false)
	s.MonthlyNotes = normalizePeriodic(mapOf(raw["monthlyNotes"]), DefaultMonthlyNotesDirectory, DefaultMonthlyNoteTitlePattern, false)
	s.Favorites = uniqueStrings(stringsOf(raw["favorites"]))
	s.SystemFolderPaths = NormalizeSystemFolderPaths(stringMapOf(raw["systemFolderPaths"]))
	if tasks := mapOf(raw["tasks"]); tasks != nil {
		s.TasksExcludedFolders = NormalizeTasksExcludedFolders(stringsOf(tasks["excludedFolders"]))
		s.TasksIncludedFolders = NormalizeTasksExcludedFolders(stringsOf(tasks["includedFolders"]))
	}
	s.TemplateFolder = templateFolderOf(raw)
	if typst := mapOf(raw["typstPreambles"]); typst != nil {
		if folder := normalizeTypstPreambleFolder(stringOf(typst["folder"])); folder != "" {
			s.TypstPreambleFolder = folder
		}
	}
	return s
}

func normalizePeriodic(raw map[string]any, defaultDir, defaultTitle string, daily bool) PeriodicNotes {
	p := PeriodicNotes{
		Directory:          normalizeDateNoteDirectory(stringOf(raw["directory"]), defaultDir),
		TitlePattern:       normalizeDateNoteTitlePattern(stringOf(raw["titlePattern"]), defaultTitle),
		Locale:             normalizeDateNoteLocale(stringOf(raw["locale"])),
		TemplateID:         strings.TrimSpace(stringOf(raw["templateId"])),
		TasksDueOnNoteDate: true,
	}
	if raw == nil {
		return p
	}
	p.Enabled = boolOf(raw["enabled"])
	if daily {
		if v, ok := raw["tasksDueOnNoteDate"].(bool); ok {
			p.TasksDueOnNoteDate = v
		}
		p.RolloverUnfinishedTasks = boolOf(raw["rolloverUnfinishedTasks"])
	}
	seen := map[string]struct{}{}
	if legacy, ok := raw["legacyPatterns"].([]any); ok {
		for _, entry := range legacy {
			m := mapOf(entry)
			if m == nil {
				continue
			}
			next := DateNotePattern{
				Directory:    normalizeDateNoteDirectory(stringOf(m["directory"]), defaultDir),
				TitlePattern: normalizeDateNoteTitlePattern(stringOf(m["titlePattern"]), defaultTitle),
				Locale:       normalizeDateNoteLocale(stringOf(m["locale"])),
			}
			key := next.Directory + "\x00" + next.TitlePattern + "\x00" + next.Locale
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			p.LegacyPatterns = append(p.LegacyPatterns, next)
		}
	}
	return p
}

func normalizeDateNoteDirectory(value, fallback string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func normalizeDateNoteTitlePattern(value, fallback string) string {
	trimmed := strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-").Replace(value))
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func normalizeDateNoteLocale(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultDateNoteLocale
	}
	return trimmed
}

// --- system folder paths (mirrors shared-domain/system-folder-paths.ts) ---

var defaultFolderPaths = map[NoteFolder]string{
	FolderInbox:   "inbox",
	FolderQuick:   "quick",
	FolderArchive: "archive",
	FolderTrash:   "trash",
}

func isValidFolderPath(p string) bool {
	if p == "" || len(p) > 128 {
		return false
	}
	if strings.ContainsAny(p, "/\\") {
		return false
	}
	if p == "." || p == ".." || strings.HasPrefix(p, ".") {
		return false
	}
	if strings.ContainsAny(p, ":*?\"<>|#^[]") {
		return false
	}
	if _, reserved := reservedFolderPathNames[strings.ToLower(p)]; reserved {
		return false
	}
	return true
}

// NormalizeSystemFolderPaths validates a raw systemFolderPaths map the way
// every other runtime does: single directory names, no reserved names, no
// folder claiming another folder's default name, no two folders resolving to
// the same directory. Nil when nothing survives.
func NormalizeSystemFolderPaths(raw map[string]string) map[string]string {
	if raw == nil {
		return nil
	}
	next := map[string]string{}
	for _, folder := range AllFolders {
		val := strings.TrimSpace(raw[string(folder)])
		if val == "" || !isValidFolderPath(val) || val == string(folder) {
			continue
		}
		if claimsAnotherDefaultName(folder, val) {
			continue
		}
		next[string(folder)] = val
	}
	changed := true
	for changed {
		changed = false
		for _, folder := range AllFolders {
			val, ok := next[string(folder)]
			if !ok {
				continue
			}
			lower := strings.ToLower(val)
			for _, other := range AllFolders {
				if other == folder {
					continue
				}
				if lower == strings.ToLower(ResolveFolderPath(other, next)) {
					delete(next, string(folder))
					changed = true
					break
				}
			}
		}
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

func claimsAnotherDefaultName(folder NoteFolder, val string) bool {
	lower := strings.ToLower(val)
	for _, other := range AllFolders {
		if other != folder && lower == defaultFolderPaths[other] {
			return true
		}
	}
	return false
}

// ResolveFolderPath is the on-disk directory name of a system folder.
func ResolveFolderPath(folder NoteFolder, paths map[string]string) string {
	if p, ok := paths[string(folder)]; ok && p != "" {
		return p
	}
	return defaultFolderPaths[folder]
}

// SystemFolderForDirName classifies a top-level directory name. Only the
// RESOLVED name of each folder counts, case-insensitively: with inbox
// remapped to `01 - Entry`, a directory literally named `inbox/` is an
// ordinary user folder.
func SystemFolderForDirName(name string, paths map[string]string) (NoteFolder, bool) {
	lower := strings.ToLower(name)
	for _, folder := range AllFolders {
		if strings.ToLower(ResolveFolderPath(folder, paths)) == lower {
			return folder, true
		}
	}
	return "", false
}

// IsReservedRootName reports whether a top-level name is kept out of the
// root-mode note tree: an asset or internal directory, or the resolved
// directory of any system folder. Case-insensitive, unlike the walkers'
// exact skip list, because it guards names a client typed: on a
// case-insensitive volume `Attachements/` is the attachments directory.
func IsReservedRootName(name string, paths map[string]string) bool {
	lower := strings.ToLower(name)
	for reserved := range reservedNonSystemRootNames {
		if strings.ToLower(reserved) == lower {
			return true
		}
	}
	_, system := SystemFolderForDirName(name, paths)
	return system
}

// --- tasks folder lists (mirrors shared-domain/tasks-excluded-folders.ts) ---
// The excluded folders (#458) and the Task folders (#962) share one set of
// validation rules; keep them byte-compatible with the desktop and the server.

func normalizeTasksExcludedFolder(value string) string {
	parts := []string{}
	for _, seg := range strings.Split(strings.ReplaceAll(value, "\\", "/"), "/") {
		s := strings.TrimSpace(seg)
		if s == "" {
			continue
		}
		if s == "." || s == ".." {
			return ""
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, "/")
	if len(joined) > 512 {
		return ""
	}
	return joined
}

// NormalizeTasksExcludedFolders drops invalid entries and duplicates. The
// Task folders list is validated with it too.
func NormalizeTasksExcludedFolders(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, entry := range values {
		cleaned := normalizeTasksExcludedFolder(entry)
		if cleaned == "" {
			continue
		}
		if _, dup := seen[cleaned]; dup {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

// IsPathExcludedFromTasks reports whether a vault-relative POSIX path is kept
// out of Tasks by the folder lists (segment-prefix match, case-sensitive).
// Once included names folders, a path outside all of them is out as well;
// excluded still applies inside them.
func IsPathExcludedFromTasks(relPath string, excluded, included []string) bool {
	if len(included) > 0 && !isInsideAnyFolder(relPath, included) {
		return true
	}
	return isInsideAnyFolder(relPath, excluded)
}

func isInsideAnyFolder(relPath string, folders []string) bool {
	for _, folder := range folders {
		if relPath == folder || strings.HasPrefix(relPath, folder+"/") {
			return true
		}
	}
	return false
}

// rewriteFolderList repoints one list's entries after a folder rename or move:
// an entry naming the folder, or a folder inside it, follows it.
func rewriteFolderList(list []string, oldRel, newRel string) []string {
	out := make([]string, 0, len(list))
	for _, entry := range list {
		if entry == oldRel || strings.HasPrefix(entry, oldRel+"/") {
			entry = newRel + entry[len(oldRel):]
		}
		out = append(out, entry)
	}
	return NormalizeTasksExcludedFolders(out)
}

func listHasFolder(list []string, rel string) bool {
	for _, entry := range list {
		if entry == rel || strings.HasPrefix(entry, rel+"/") {
			return true
		}
	}
	return false
}

// rewriteFolderKeyedMap repoints a `<bucket>:<subpath>` keyed map (the folder
// icons and colors) at a renamed folder and everything inside it. Mirrors
// rewriteFolderIconsForRename in the desktop's main/vault.ts; an entry that
// moves wins over a stale one already at its new key.
func rewriteFolderKeyedMap(m map[string]any, folder NoteFolder, oldSub, newSub string) (map[string]any, bool) {
	exact := string(folder) + ":" + oldSub
	moved := map[string]any{}
	next := make(map[string]any, len(m))
	for key, value := range m {
		if key == exact || strings.HasPrefix(key, exact+"/") {
			moved[string(folder)+":"+newSub+key[len(exact):]] = value
			continue
		}
		next[key] = value
	}
	if len(moved) == 0 {
		return m, false
	}
	for key, value := range moved {
		next[key] = value
	}
	return next, true
}

// rewriteSettingsForFolderRename keeps vault.json's path-keyed settings naming
// the same folders after a rename or move: the Tasks folder lists, which hold
// vault-relative paths (mirrors rewriteTasksFoldersForRename in
// tasks-excluded-folders.ts), the template folder, another such path
// (rewriteTemplateFolderForRename), and the folder icons and colors, keyed by
// bucket and subpath, as desktop main and the server rewrite them. vault.json
// is written only when an entry moves.
func (v *Vault) rewriteSettingsForFolderRename(folder NoteFolder, oldSub, newSub, oldRel, newRel string) error {
	if oldRel == "" || newRel == "" || oldRel == newRel {
		return nil
	}
	s := v.Settings()
	tasksMove := listHasFolder(s.TasksExcludedFolders, oldRel) || listHasFolder(s.TasksIncludedFolders, oldRel)
	templateFolder := rewriteTemplateFolderForRename(s.TemplateFolder, oldRel, newRel)
	_, iconsMove := rewriteFolderKeyedMap(mapOf(s.Raw["folderIcons"]), folder, oldSub, newSub)
	_, colorsMove := rewriteFolderKeyedMap(mapOf(s.Raw["folderColors"]), folder, oldSub, newSub)
	if !tasksMove && templateFolder == s.TemplateFolder && !iconsMove && !colorsMove {
		return nil
	}
	return v.UpdateSettings(func(raw map[string]any) {
		if tasks, _ := raw["tasks"].(map[string]any); tasks != nil {
			for _, key := range []string{"excludedFolders", "includedFolders"} {
				if list := NormalizeTasksExcludedFolders(stringsOf(tasks[key])); len(list) > 0 {
					tasks[key] = rewriteFolderList(list, oldRel, newRel)
				}
			}
		}
		// Only the folder is replaced; anything else under `templates` stays.
		if templates, _ := raw["templates"].(map[string]any); templates != nil && templateFolder != s.TemplateFolder {
			templates["folder"] = templateFolder
		}
		for _, key := range []string{"folderIcons", "folderColors"} {
			if next, changed := rewriteFolderKeyedMap(mapOf(raw[key]), folder, oldSub, newSub); changed {
				raw[key] = next
			}
		}
	})
}

// --- typst preamble folder (mirrors shared-domain/typst-preamble-folder.ts) ---

func normalizeTypstPreambleFolder(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 128 {
		return ""
	}
	if trimmed == "." || trimmed == ".." || strings.HasPrefix(trimmed, ".") {
		return ""
	}
	if strings.ContainsAny(trimmed, `\/:*?"<>|#^[]`) {
		return ""
	}
	return trimmed
}

// IsTypstPreamblePath reports whether a note sits in a directory named like
// the preamble folder, at any depth. Such notes hold Typst source and are
// left out of the tag index.
func IsTypstPreamblePath(relPath, folder string) bool {
	name := strings.ToLower(folder)
	if name == "" {
		return false
	}
	parts := strings.Split(relPath, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if strings.ToLower(strings.TrimSpace(part)) == name {
			return true
		}
	}
	return false
}

// --- small decoding helpers ---

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringsOf(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	}
	return nil
}

func stringMapOf(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, e := range m {
		if s, ok := e.(string); ok {
			out[k] = s
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, entry := range values {
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}
	return out
}

// ParseSettings normalizes a raw vault.json object, for settings that
// arrive over the wire from a server rather than from disk.
func ParseSettings(raw map[string]any) VaultSettings {
	return normalizeVaultSettings(raw)
}
