package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// These cases mirror the desktop TasksKanban statusColumns/dropMutationsFor
// contract: checkbox state and scheduling are separate from the @status field.
func TestKanbanStatusColumnsMatchDesktop(t *testing.T) {
	now := time.Now()
	today := vault.TodayISO(now)
	future := vault.TodayISO(now.AddDate(0, 0, 10))
	v := &tasksView{groupBy: "status", tasks: []vault.Task{
		{ID: "undated"},
		{ID: "due-today", Due: today},
		{ID: "overdue", Due: vault.TodayISO(now.AddDate(0, 0, -2))},
		{ID: "future", Due: future},
		{ID: "started-undated", InProgress: true},
		{ID: "started-future", InProgress: true, Due: future},
		{ID: "waiting-started", InProgress: true, Waiting: true, Due: future},
		{ID: "waiting-overdue", Waiting: true, Due: "2000-01-01"},
		{ID: "done-waiting", Checked: true, Waiting: true},
		{ID: "custom-status", Fields: map[string]string{"status": "review"}},
		{ID: "custom-started", Fields: map[string]string{"status": "in-progress"}},
		{ID: "cancelled", Cancelled: true},
		{ID: "forwarded", Forwarded: true},
	}}
	a := &App{prefs: prefsView{Prefs: config.Prefs{KanbanStatuses: []string{"review", "blocked"}}}}
	v.buildColumns(a)
	assertKanbanColumnIDs(t, v, []string{"today", "upcoming", "in-progress", "waiting", "done"})
	wantTitles := []string{"Today", "Upcoming", "In progress", "Waiting", "Done"}
	for i, col := range v.columns {
		if col.title != wantTitles[i] {
			t.Errorf("column %q title = %q, want %q", col.id, col.title, wantTitles[i])
		}
	}
	want := map[string]string{
		"undated": "today", "due-today": "today", "overdue": "today",
		"future": "upcoming", "started-undated": "in-progress", "started-future": "in-progress",
		"waiting-started": "waiting", "waiting-overdue": "waiting", "done-waiting": "done",
		"custom-status": "today", "custom-started": "today",
	}
	got := map[string]string{}
	for _, col := range v.columns {
		for _, card := range col.cards {
			if _, exists := got[card.ID]; exists {
				t.Errorf("task %q appears in multiple columns", card.ID)
			}
			got[card.ID] = col.id
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("task placement = %v, want %v", got, want)
	}
}

func TestKanbanCustomStatusUsesConfiguredThenDiscoveredColumns(t *testing.T) {
	v := &tasksView{groupBy: "field:status", tasks: []vault.Task{
		{ID: "late-review", Due: "2030-02-02", Fields: map[string]string{"status": "review"}},
		{ID: "early-review", Due: "2030-01-01", Fields: map[string]string{"status": "review"}},
		{ID: "discovered", Fields: map[string]string{"status": "triage"}},
		{ID: "no-status"},
		{ID: "finished", Checked: true, Fields: map[string]string{"status": "finished-only"}},
		{ID: "cancelled", Cancelled: true, Fields: map[string]string{"status": "cancelled-only"}},
		{ID: "forwarded", Forwarded: true, Fields: map[string]string{"status": "forwarded-only"}},
	}}
	a := &App{prefs: prefsView{Prefs: config.Prefs{KanbanStatuses: []string{"review", "backlog", "review"}}}}
	v.buildColumns(a)
	assertKanbanColumnIDs(t, v, []string{"review", "backlog", "triage", "__none__"})
	assertKanbanCards(t, v, "review", []string{"early-review", "late-review"})
	assertKanbanCards(t, v, "backlog", nil)
	assertKanbanCards(t, v, "triage", []string{"discovered"})
	assertKanbanCards(t, v, "__none__", []string{"no-status"})
	if got := v.columns[len(v.columns)-1].title; got != "No status" {
		t.Errorf("no-value title = %q, want No status", got)
	}
}

func TestKanbanProjectFieldSeparatesLiteralNoneFromMissingValue(t *testing.T) {
	v := &tasksView{groupBy: "field:project", tasks: []vault.Task{
		{ID: "literal-none", Fields: map[string]string{"project": "none"}},
		{ID: "alpha", Fields: map[string]string{"project": "alpha"}},
		{ID: "missing"},
		{ID: "completed", Checked: true, Fields: map[string]string{"project": "hidden"}},
	}}
	v.buildColumns(&App{})
	assertKanbanColumnIDs(t, v, []string{"alpha", "none", "__none__"})
	assertKanbanCards(t, v, "alpha", []string{"alpha"})
	assertKanbanCards(t, v, "none", []string{"literal-none"})
	assertKanbanCards(t, v, "__none__", []string{"missing"})
}

func TestKanbanFolderColumnsUseSourceFolders(t *testing.T) {
	v := &tasksView{groupBy: "folder", tasks: []vault.Task{
		{ID: "quick", SourcePath: "quick/capture.md", NoteFolder: vault.FolderQuick},
		{ID: "project", SourcePath: "inbox/Projects/alpha.md", NoteFolder: vault.FolderInbox},
		{ID: "inbox", SourcePath: "inbox/note.md", NoteFolder: vault.FolderInbox},
		{ID: "done", SourcePath: "inbox/Done/old.md", NoteFolder: vault.FolderInbox, Checked: true},
	}}
	v.buildColumns(&App{})
	assertKanbanColumnIDs(t, v, []string{"inbox", "inbox/Projects", "quick"})
	assertKanbanCards(t, v, "inbox", []string{"inbox"})
	assertKanbanCards(t, v, "inbox/Projects", []string{"project"})
	assertKanbanCards(t, v, "quick", []string{"quick"})
}

func TestKanbanFolderRootUsesRemappedSystemFolderPaths(t *testing.T) {
	a := &App{
		prefs: prefsView{Prefs: config.Prefs{KanbanFolderRoot: "projects/"}},
		idx: &index{primary: vault.PrimaryNotesInbox, settings: vault.VaultSettings{
			SystemFolderPaths: map[string]string{"inbox": "01 - Entry", "quick": "Capture"},
		}},
	}
	v := &tasksView{groupBy: "folder", tasks: []vault.Task{
		{ID: "beta", SourcePath: "01 - Entry/Projects/beta/work.md", NoteFolder: vault.FolderInbox},
		{ID: "alpha", SourcePath: "01 - Entry/Projects/alpha/work.md", NoteFolder: vault.FolderInbox},
		{ID: "deep", SourcePath: "01 - Entry/Projects/alpha/deep/work.md", NoteFolder: vault.FolderInbox},
		{ID: "overview", SourcePath: "01 - Entry/Projects/overview.md", NoteFolder: vault.FolderInbox},
		{ID: "areas", SourcePath: "01 - Entry/Areas/home.md", NoteFolder: vault.FolderInbox},
		{ID: "quick", SourcePath: "Capture/Projects/note.md", NoteFolder: vault.FolderQuick},
		{ID: "literal-inbox-folder", SourcePath: "inbox/Projects/note.md", NoteFolder: vault.FolderInbox},
	}}
	v.buildColumns(a)
	assertKanbanColumnIDs(t, v, []string{"01 - Entry/Projects", "01 - Entry/Projects/alpha", "01 - Entry/Projects/beta", "__none__"})
	assertKanbanCards(t, v, "01 - Entry/Projects", []string{"overview"})
	assertKanbanCards(t, v, "01 - Entry/Projects/alpha", []string{"deep", "alpha"})
	assertKanbanCards(t, v, "01 - Entry/Projects/beta", []string{"beta"})
	assertKanbanCards(t, v, "__none__", []string{"areas", "quick", "literal-inbox-folder"})
	for i, title := range []string{"Projects", "alpha", "beta", "Other folders"} {
		if v.columns[i].title != title {
			t.Errorf("column %q title = %q, want %q", v.columns[i].id, v.columns[i].title, title)
		}
	}
}

func TestKanbanFolderRootSupportsPrimaryNotesAtVaultRoot(t *testing.T) {
	a := &App{
		prefs: prefsView{Prefs: config.Prefs{KanbanFolderRoot: " projects\\ "}},
		idx: &index{primary: vault.PrimaryNotesRoot, settings: vault.VaultSettings{
			SystemFolderPaths: map[string]string{"quick": "Capture"},
		}},
	}
	v := &tasksView{groupBy: "folder", tasks: []vault.Task{
		{ID: "root-note", SourcePath: "plan.md", NoteFolder: vault.FolderInbox},
		{ID: "overview", SourcePath: "Projects/overview.md", NoteFolder: vault.FolderInbox},
		{ID: "alpha", SourcePath: "Projects/alpha/work.md", NoteFolder: vault.FolderInbox},
		{ID: "deep", SourcePath: "Projects/alpha/deep/work.md", NoteFolder: vault.FolderInbox},
		{ID: "quick", SourcePath: "Capture/Projects/note.md", NoteFolder: vault.FolderQuick},
	}}
	v.buildColumns(a)
	assertKanbanColumnIDs(t, v, []string{"Projects", "Projects/alpha", "__none__"})
	assertKanbanCards(t, v, "Projects", []string{"overview"})
	assertKanbanCards(t, v, "Projects/alpha", []string{"deep", "alpha"})
	assertKanbanCards(t, v, "__none__", []string{"quick", "root-note"})
	for i, title := range []string{"Projects", "alpha", "Other folders"} {
		if v.columns[i].title != title {
			t.Errorf("column %q title = %q, want %q", v.columns[i].id, v.columns[i].title, title)
		}
	}
}

func TestKanbanStatusMovesMatchDesktopMutations(t *testing.T) {
	now := time.Now()
	today := vault.TodayISO(now)
	tomorrow := vault.TodayISO(now.AddDate(0, 0, 1))
	future := vault.TodayISO(now.AddDate(0, 0, 10))
	tests := []struct {
		name, line, target, due    string
		checked, waiting, progress bool
	}{
		{name: "today clears waiting and progress", line: "- [/] Work @waiting due:" + future, target: "today", due: today},
		{name: "today reopens done", line: "- [x] Work", target: "today", due: today},
		{name: "upcoming schedules tomorrow for overdue", line: "- [/] Work @waiting due:2000-01-01", target: "upcoming", due: tomorrow},
		{name: "upcoming preserves future due date", line: "- [x] Work @waiting due:" + future, target: "upcoming", due: future},
		{name: "progress clears waiting and preserves due", line: "- [ ] Work @waiting due:" + future, target: "in-progress", due: future, progress: true},
		{name: "waiting preserves progress and due", line: "- [/] Work due:" + future, target: "waiting", due: future, waiting: true, progress: true},
		{name: "done preserves waiting and due", line: "- [/] Work @waiting due:" + future, target: "done", due: future, waiting: true, checked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, path, task := newKanbanMutationTestApp(t, tt.line+" @status:review @project:alpha\n")
			v := &tasksView{groupBy: "status", columns: []kanbanColumn{{id: tt.target}}}
			v.moveCard(a, task, 0)
			got := readKanbanMutationTask(t, path)
			if got.Checked != tt.checked || got.Waiting != tt.waiting || got.InProgress != tt.progress || got.Due != tt.due {
				t.Errorf("moved task: checked=%v waiting=%v progress=%v due=%q; want %v/%v/%v/%q; body=%q",
					got.Checked, got.Waiting, got.InProgress, got.Due, tt.checked, tt.waiting, tt.progress, tt.due, got.RawText)
			}
			if got.Fields["status"] != "review" || got.Fields["project"] != "alpha" {
				t.Errorf("status move changed custom fields: %v", got.Fields)
			}
		})
	}
}

func TestKanbanFieldMovesWriteUnprefixedField(t *testing.T) {
	tests := []struct{ group, target, key, want string }{
		{"field:project", "beta", "project", "beta"},
		{"field:project", "none", "project", "none"},
		{"field:project", "__none__", "project", ""},
		{"field:status", "review", "status", "review"},
	}
	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.target, func(t *testing.T) {
			a, path, task := newKanbanMutationTestApp(t, "- [/] Work @waiting @project:alpha @status:backlog\n")
			v := &tasksView{groupBy: tt.group, columns: []kanbanColumn{{id: tt.target}}}
			v.moveCard(a, task, 0)
			got := readKanbanMutationTask(t, path)
			if got.Fields[tt.key] != tt.want {
				t.Errorf("field %q = %q, want %q; body=%q", tt.key, got.Fields[tt.key], tt.want, got.RawText)
			}
			if !got.InProgress || !got.Waiting || got.Checked {
				t.Errorf("custom field move changed task state: %+v", got)
			}
			if strings.Contains(got.RawText, "@field:") {
				t.Errorf("group-by prefix leaked into note: %q", got.RawText)
			}
		})
	}
}

func TestKanbanFolderMovesDoNotRewriteTask(t *testing.T) {
	const body = "- [/] Work @project:alpha\n"
	a, path, task := newKanbanMutationTestApp(t, body)
	v := &tasksView{groupBy: "folder", columns: []kanbanColumn{{id: "quick"}}}
	v.moveCard(a, task, 0)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("folder board must be read-only; body=%q, want %q", got, body)
	}
}

func assertKanbanColumnIDs(t *testing.T, v *tasksView, want []string) {
	t.Helper()
	got := make([]string, len(v.columns))
	for i, col := range v.columns {
		got[i] = col.id
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("column IDs = %v, want %v", got, want)
	}
}

func assertKanbanCards(t *testing.T, v *tasksView, column string, want []string) {
	t.Helper()
	for _, col := range v.columns {
		if col.id != column {
			continue
		}
		var got []string
		for _, card := range col.cards {
			got = append(got, card.ID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("column %q tasks = %v, want %v", column, got, want)
		}
		return
	}
	t.Errorf("missing column %q", column)
}

func newKanbanMutationTestApp(t *testing.T, body string) (*App, string, vault.Task) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "Work.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{backend: b, ignoredPaths: map[string]time.Time{}}
	return a, path, readKanbanMutationTask(t, path)
}

func readKanbanMutationTask(t *testing.T, path string) vault.Task {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tasks := vault.ParseTasks("inbox/Work.md", "Work", vault.FolderInbox, string(body), vault.ParseTasksOptions{Dialect: vault.DialectApp})
	if len(tasks) != 1 {
		t.Fatalf("parsed %d tasks, want one; body=%q", len(tasks), body)
	}
	return tasks[0]
}
