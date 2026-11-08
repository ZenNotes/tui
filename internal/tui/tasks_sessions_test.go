package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// sessionDay is the fixed day that the session tests treat as today.
var sessionDay = time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)

// sessionApp reads the tasks and the events of some note bodies and gives an
// app that holds them with a fixed clock.
func sessionApp(order string, notes map[string]string) *App {
	tasks := []vault.Task{}
	events := []vault.Event{}
	for _, name := range []string{"Work", "Plans"} {
		body, ok := notes[name]
		if !ok {
			continue
		}
		tasks = append(tasks, vault.ParseTasks(name+".md", name, vault.FolderInbox, body, vault.ParseTasksOptions{Dialect: vault.DialectApp})...)
		events = append(events, vault.ParseEvents(name+".md", name, vault.FolderInbox, body)...)
	}
	a := &App{idx: &index{tasks: tasks, events: events}, prefs: prefsView{Prefs: config.Prefs{TasksListOrder: order}}}
	a.taskNow = func() time.Time { return sessionDay }
	return a
}

// listContents gives the content of each task row, in order.
func listContents(v *tasksView) []string {
	var out []string
	for _, row := range v.rows {
		if row.task != nil {
			out = append(out, row.task.Content)
		}
	}
	return out
}

func listHeaders(v *tasksView) []string {
	var out []string
	for _, row := range v.rows {
		if row.header != "" {
			out = append(out, row.header)
		}
	}
	return out
}

const sessionWork = `- [ ] Draft report ^aaaaaa
- [ ] Review budget ^bbbbbb
- [ ] Plan trip ^cccccc
- [ ] No plan at all
`

func TestTasksViewGroupsBySessionDate(t *testing.T) {
	a := sessionApp("note", map[string]string{
		"Work": sessionWork,
		"Plans": `- Draft task:[[Work#^aaaaaa]] when:2026-11-05T09:30
- Review task:[[Work#^bbbbbb]] when:2026-11-03
- Trip task:[[Work#^cccccc]] when:2026-11-20
`,
	})
	v := &tasksView{mode: "list"}
	v.refresh(a)
	if got := strings.Join(listHeaders(v), "|"); got != "Today (2)|No due date (2)" {
		t.Fatalf("headers = %s", got)
	}
	if got := strings.Join(listContents(v), "|"); got != "Draft report|Review budget|Plan trip|No plan at all" {
		t.Fatalf("rows = %s", got)
	}
}

func TestTasksViewSortsTodayBySessionTime(t *testing.T) {
	a := sessionApp("priority", map[string]string{
		"Work": sessionWork,
		"Plans": `- Draft task:[[Work#^aaaaaa]] when:2026-11-05T15:00
- Review task:[[Work#^bbbbbb]] when:2026-11-05T09:30
- Trip task:[[Work#^cccccc]] when:2026-11-05
`,
	})
	v := &tasksView{mode: "list"}
	v.refresh(a)
	got := strings.Join(listContents(v), "|")
	if !strings.HasPrefix(got, "Review budget|Draft report|Plan trip") {
		t.Fatalf("order = %s", got)
	}
}

func TestWhereSessionFilter(t *testing.T) {
	a := sessionApp("note", map[string]string{
		"Work": sessionWork,
		"Plans": `- Draft task:[[Work#^aaaaaa]] when:2026-11-05T09:30
- Review task:[[Work#^bbbbbb]] when:2026-11-03
- Trip task:[[Work#^cccccc]] when:2026-12-01
`,
	})
	for _, tc := range []struct {
		query string
		want  string
	}{
		{"where: session:today", "Draft report"},
		{"where: session:overdue", "Review budget"},
		{"where: session:none", "No plan at all"},
		{"where: session:any", "Draft report|Review budget|Plan trip"},
		{"where: session:2026-12", "Plan trip"},
	} {
		v := &tasksView{mode: "list", filter: tc.query}
		v.refresh(a)
		if got := strings.Join(listContents(v), "|"); got != tc.want {
			t.Errorf("%s: rows = %s, want %s", tc.query, got, tc.want)
		}
	}
}

// The filter reads the session date only. A task that has a `@session:`
// field and no session does not match.
func TestWhereSessionIgnoresSessionField(t *testing.T) {
	tasks := []vault.Task{
		{ID: "field", Content: "field", Fields: map[string]string{"session": "2026-11-05"}},
		{ID: "joined", Content: "joined", SessionDate: "2026-11-05"},
	}
	for _, query := range []string{"session:today", "session:2026-11", "session:any"} {
		got := filterTasksOn(tasks, query, sessionDay)
		if len(got) != 1 || got[0].ID != "joined" {
			t.Errorf("%s: matches = %+v", query, got)
		}
	}
	got := filterTasksOn(tasks, "session:none", sessionDay)
	if len(got) != 1 || got[0].ID != "field" {
		t.Errorf("session:none matches = %+v", got)
	}
}

func TestTasksListRowShowsSession(t *testing.T) {
	a := sessionApp("note", map[string]string{
		"Work":  "- [ ] Draft report ^aaaaaa\n",
		"Plans": "- Draft task:[[Work#^aaaaaa]] when:2026-11-05T09:30\n",
	})
	v := &tasksView{mode: "list"}
	v.refresh(a)
	var line string
	for _, row := range v.rows {
		if row.task != nil {
			line = stripAnsi(v.taskLine(a, *row.task, 120, false))
		}
	}
	if !strings.Contains(line, "session 2026-11-05 09:30") {
		t.Fatalf("row = %q", line)
	}
	if strings.Contains(line, "scheduled") {
		t.Fatalf("row names scheduled: %q", line)
	}
}

func TestEventsNeverListAsTasks(t *testing.T) {
	a := sessionApp("note", map[string]string{
		"Work": "- [ ] Draft report ^aaaaaa\n",
		"Plans": `- Draft task:[[Work#^aaaaaa]] when:2026-11-05T09:30
- Standup when:2026-11-05T10:00 repeat:weekly
- Holiday when:2026-11-06
`,
	})
	if len(a.idx.events) != 3 {
		t.Fatalf("events = %d", len(a.idx.events))
	}
	if got := a.displayTasks(); len(got) != 1 {
		t.Fatalf("display tasks = %+v", got)
	}
	for _, group := range []string{"status", "priority", "due", "folder"} {
		v := &tasksView{mode: "list", groupBy: group}
		v.refresh(a)
		if got := listContents(v); len(got) != 1 || got[0] != "Draft report" {
			t.Fatalf("%s: list rows = %v", group, got)
		}
		for _, col := range v.columns {
			for _, c := range col.cards {
				if c.Content != "Draft report" {
					t.Fatalf("%s: card %q", group, c.Content)
				}
			}
		}
	}
	v := &tasksView{mode: "calendar"}
	v.refresh(a)
	if len(v.tasks) != 1 {
		t.Fatalf("calendar tasks = %+v", v.tasks)
	}
}

// stubBackend is a backend that is not a local vault. A call to any of its
// methods panics.
type stubBackend struct{ backend.Backend }

func TestRemoteVaultLoadsNoEvents(t *testing.T) {
	if got := loadEvents(stubBackend{}); got != nil {
		t.Fatalf("events = %+v", got)
	}
}

func TestLocalVaultLoadsEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Plans.md"), []byte("- Holiday when:2026-11-06\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := loadEvents(b); len(got) != 1 {
		t.Fatalf("events = %+v", got)
	}
}
