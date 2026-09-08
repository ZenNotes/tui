// Package vault is the filesystem engine behind every zn command and the
// terminal UI: it reads and writes a ZenNotes vault exactly the way the
// desktop app, the bundled `zn`, and the self-hosted server do.
//
// SYNCED SEMANTICS. The parsers in this package (tags, wikilinks, excerpts,
// task lines, frontmatter, system folder paths) are deliberate copies of the
// ones in the ZenNotes repo: `apps/desktop/src/mcp/vault-ops.ts`,
// `packages/shared-domain/src/*.ts` and `apps/server/internal/vault/*.go`.
// A note must read the same everywhere, so a change to one of those has to
// land here too, byte for byte where the rules are byte rules.
package vault

import "encoding/json"

// NoteFolder is one of the four conceptual top-level buckets. Their on-disk
// directory names can be remapped per vault (`systemFolderPaths` in
// vault.json), so a NoteFolder is never joined onto a path directly; see
// (*Vault).folderRoot.
type NoteFolder string

const (
	FolderInbox   NoteFolder = "inbox"
	FolderQuick   NoteFolder = "quick"
	FolderArchive NoteFolder = "archive"
	FolderTrash   NoteFolder = "trash"
)

// AllFolders lists the buckets in the order every listing walks them.
var AllFolders = []NoteFolder{FolderInbox, FolderQuick, FolderArchive, FolderTrash}

// LiveFolders are the buckets searches and task scans cover: everything but
// the trash.
var LiveFolders = []NoteFolder{FolderInbox, FolderQuick, FolderArchive}

// IsValidFolder reports whether f names one of the four buckets.
func IsValidFolder(f NoteFolder) bool {
	switch f {
	case FolderInbox, FolderQuick, FolderArchive, FolderTrash:
		return true
	}
	return false
}

// PrimaryNotesLocation says where the inbox bucket lives: under `inbox/`
// (the classic layout) or directly at the vault root (Obsidian-style).
type PrimaryNotesLocation string

const (
	PrimaryNotesInbox PrimaryNotesLocation = "inbox"
	PrimaryNotesRoot  PrimaryNotesLocation = "root"
)

// NoteMeta is what `zn list --json` prints for a note. Field names and order
// match the desktop CLI so scripts reading its JSON keep working.
type NoteMeta struct {
	// Path is vault-relative and always POSIX-separated.
	Path string `json:"path"`
	// Link is the `zennotes://open?path=…` URL that focuses the app on the
	// note. Presented to humans; tools are handed Path.
	Link      string     `json:"link"`
	Title     string     `json:"title"`
	Folder    NoteFolder `json:"folder"`
	CreatedAt int64      `json:"createdAt"`
	UpdatedAt int64      `json:"updatedAt"`
	Size      int64      `json:"size"`
	Tags      []string   `json:"tags"`
	Wikilinks []string   `json:"wikilinks"`
	Excerpt   string     `json:"excerpt"`
}

// NoteContent is a note with its body.
type NoteContent struct {
	NoteMeta
	Body string `json:"body"`
}

// FolderEntry is one subfolder under a bucket, as `zn folder list` reports it.
type FolderEntry struct {
	Folder  NoteFolder `json:"folder"`
	Subpath string     `json:"subpath"`
}

// AssetMeta describes a file under the vault's attachment directories.
type AssetMeta struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updatedAt"`
}

// TextSearchMatch is one matching line from a full-text search.
type TextSearchMatch struct {
	Path       string     `json:"path"`
	Link       string     `json:"link"`
	Title      string     `json:"title"`
	Folder     NoteFolder `json:"folder"`
	LineNumber int        `json:"lineNumber"`
	LineText   string     `json:"lineText"`
}

// Task is a checkbox line or a whole-note task file, with its inline
// metadata parsed out. Mirrors the desktop's VaultTask.
type Task struct {
	// ID is `<path>#<taskIndex>` for a checkbox line and `<path>#task` for a
	// task file. Stable across plain edits, so it is what toggles take.
	ID         string     `json:"id"`
	SourcePath string     `json:"sourcePath"`
	Link       string     `json:"link"`
	NoteTitle  string     `json:"noteTitle"`
	NoteFolder NoteFolder `json:"noteFolder"`
	// LineNumber is zero-based over the whole file, frontmatter included.
	LineNumber int    `json:"lineNumber"`
	TaskIndex  int    `json:"taskIndex"`
	RawText    string `json:"rawText"`
	Content    string `json:"content"`
	Checked    bool   `json:"checked"`
	// Cancelled is a `[-]` line: intentionally abandoned.
	Cancelled bool `json:"cancelled"`
	// InProgress is a `[/]` line: started, not finished. Still open work.
	InProgress bool `json:"inProgress"`
	// Forwarded is a `[>]` record: the task moved to another note.
	Forwarded bool   `json:"forwarded"`
	Due       string `json:"due,omitempty"`
	Priority  string `json:"priority,omitempty"`
	Waiting   bool   `json:"waiting"`
	// Fields holds inline `@key:value` tokens (lower-cased), the note's
	// frontmatter `status:` folded in. Any key can drive a Kanban board.
	Fields map[string]string `json:"fields,omitempty"`
	Status string            `json:"status,omitempty"`
	Tags   []string          `json:"tags"`
	// Kind is "file" for a whole-note task; empty means an inline checkbox.
	Kind          string `json:"kind,omitempty"`
	Scheduled     string `json:"scheduled,omitempty"`
	CompletedDate string `json:"completedDate,omitempty"`
	// DueInferred marks a due date derived from the containing daily note
	// rather than written on the line. Set by the TUI, never by a scan.
	DueInferred bool `json:"dueInferred,omitempty"`
}

// TaskGroups is the Tasks list's bucketing: Today (undated, due today, and
// overdue), Upcoming, Waiting, Done, Forwarded, Cancelled.
type TaskGroups struct {
	Today        []Task
	Upcoming     []Task
	Waiting      []Task
	Done         []Task
	Forwarded    []Task
	Cancelled    []Task
	OverdueCount int
}

// TaskDialect picks which task-line grammar a scan uses. The desktop app
// and its CLI disagree on the edges (numbered-list and blockquoted checkboxes
// exist for the app only, and the CLI leaves `@key:value` fields in the
// content), and a task id is only stable within one grammar, so each surface
// keeps the grammar it always had.
type TaskDialect int

const (
	// DialectApp is the grammar of the app's Tasks views, which the terminal
	// UI shares so a toggle there edits the line the app would.
	DialectApp TaskDialect = iota
	// DialectCLI is the grammar of the desktop's `zn task` commands and MCP
	// tools, so scripts keep getting the ids they got before.
	DialectCLI
)

// ParseTasksOptions controls scanning past the exclusions and the grammar.
type ParseTasksOptions struct {
	// IncludeExcluded scans past the vault's excluded-folders list and the
	// note-level frontmatter `tasks:` opt-out. The `--include-excluded`
	// escape hatch; listings never set it on their own.
	IncludeExcluded bool
	Dialect         TaskDialect
}

// VaultInfo names the vault a server (or this process) is serving.
type VaultInfo struct {
	Root string `json:"root"`
	Name string `json:"name"`
}

// CustomTemplateFile is a raw custom template as it lives on disk.
type CustomTemplateFile struct {
	SourcePath string `json:"sourcePath"`
	Raw        string `json:"raw"`
}

// WriteTemplateInput saves a custom template under a slug.
type WriteTemplateInput struct {
	Slug               string `json:"slug"`
	Raw                string `json:"raw"`
	PreviousSourcePath string `json:"previousSourcePath,omitempty"`
}

// OutlineItem is one heading of a note.
type OutlineItem struct {
	Level int
	Text  string
	// Line is 1-based.
	Line int
}

// MarshalJSON keeps the desktop CLI's exact key set: a checkbox task carries
// `forwarded` (true or false), a task file never does.
func (t Task) MarshalJSON() ([]byte, error) {
	type wire struct {
		ID            string            `json:"id"`
		SourcePath    string            `json:"sourcePath"`
		Link          string            `json:"link"`
		NoteTitle     string            `json:"noteTitle"`
		NoteFolder    NoteFolder        `json:"noteFolder"`
		LineNumber    int               `json:"lineNumber"`
		TaskIndex     int               `json:"taskIndex"`
		RawText       string            `json:"rawText"`
		Content       string            `json:"content"`
		Checked       bool              `json:"checked"`
		Cancelled     bool              `json:"cancelled"`
		InProgress    bool              `json:"inProgress"`
		Forwarded     *bool             `json:"forwarded,omitempty"`
		Due           string            `json:"due,omitempty"`
		Priority      string            `json:"priority,omitempty"`
		Waiting       bool              `json:"waiting"`
		Fields        map[string]string `json:"fields,omitempty"`
		Status        string            `json:"status,omitempty"`
		Tags          []string          `json:"tags"`
		Kind          string            `json:"kind,omitempty"`
		Scheduled     string            `json:"scheduled,omitempty"`
		CompletedDate string            `json:"completedDate,omitempty"`
		DueInferred   bool              `json:"dueInferred,omitempty"`
	}
	w := wire{
		ID: t.ID, SourcePath: t.SourcePath, Link: t.Link, NoteTitle: t.NoteTitle, NoteFolder: t.NoteFolder,
		LineNumber: t.LineNumber, TaskIndex: t.TaskIndex, RawText: t.RawText, Content: t.Content,
		Checked: t.Checked, Cancelled: t.Cancelled, InProgress: t.InProgress, Due: t.Due, Priority: t.Priority,
		Waiting: t.Waiting, Fields: t.Fields, Status: t.Status, Tags: t.Tags, Kind: t.Kind,
		Scheduled: t.Scheduled, CompletedDate: t.CompletedDate, DueInferred: t.DueInferred,
	}
	if t.Kind != "file" {
		forwarded := t.Forwarded
		w.Forwarded = &forwarded
	}
	if w.Tags == nil {
		w.Tags = []string{}
	}
	return json.Marshal(w)
}
