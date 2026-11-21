package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

const (
	skipDailySettings = `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true}}`
	skipDailyOff      = `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"tasksDueOnNoteDate":false}}`
	skipDailyPath     = "Daily Notes/2026-11-18.md"
)

// skipApp opens a vault with the given files and a fixed clock of 2026-11-25.
func skipApp(t *testing.T, files map[string]string) (*App, string) {
	t.Helper()
	calFixToday(t, calDate(2026, time.November, 25))
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newSessionParityApp(t, root)
	loaded := a.loadTasksCmd()().(tasksLoadedMsg)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	a.idx.tasks = loaded.tasks
	a.width, a.height, a.ready = 120, 40, true
	a.layout()
	return a, root
}

func skipTaskOf(t *testing.T, a *App, path string) vault.Task {
	t.Helper()
	for _, task := range a.displayTasks() {
		if task.SourcePath == path && task.TaskIndex == 0 {
			return task
		}
	}
	t.Fatalf("no task in %s", path)
	return vault.Task{}
}

func skipItems(a *App, task vault.Task) []menuItem {
	a.overlay = nil
	a.taskMenu(task)
	return a.overlay.(*menu).items
}

func skipItem(items []menuItem) (menuItem, bool) {
	for _, it := range items {
		if it.label == "Skip this time" {
			return it, true
		}
	}
	return menuItem{}, false
}

func skipRead(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTaskMenuOffersSkipThisTimeOnlyForAnOpenRepeatingInlineTask(t *testing.T) {
	a, _ := skipApp(t, map[string]string{"n.md": "- [ ] water @repeat:monthly due:2026-11-01\n"})
	open := skipTaskOf(t, a, "n.md")
	for _, tc := range []struct {
		name string
		edit func(*vault.Task)
		want bool
	}{
		{"open repeating", func(*vault.Task) {}, true},
		{"no repeat", func(k *vault.Task) { k.RawText = "- [ ] water due:2026-11-01" }, false},
		{"rule that does not parse", func(k *vault.Task) { k.RawText = "- [ ] water @repeat:sometimes" }, false},
		{"task note", func(k *vault.Task) { k.Kind = "file" }, false},
		{"checked", func(k *vault.Task) { k.Checked = true }, false},
		{"cancelled", func(k *vault.Task) { k.Cancelled = true }, false},
		{"forwarded", func(k *vault.Task) { k.Forwarded = true }, false},
	} {
		task := open
		tc.edit(&task)
		if _, got := skipItem(skipItems(a, task)); got != tc.want {
			t.Errorf("%s: Skip this time shown = %v, want %v", tc.name, got, tc.want)
		}
	}
	keys := map[string]bool{}
	for _, it := range skipItems(a, open) {
		if it.sep {
			continue
		}
		if keys[it.key] {
			t.Errorf("menu key %q is used twice", it.key)
		}
		keys[it.key] = true
		if it.key == "j" || it.key == "k" || it.key == "q" {
			t.Errorf("menu key %q moves the menu or closes it", it.key)
		}
	}
}

func TestSkipThisTimeWritesACancelledRecordAndNamesTheNextDay(t *testing.T) {
	a, root := skipApp(t, map[string]string{"n.md": "- [ ] water @repeat:monthly due:2026-11-01\n"})
	item, ok := skipItem(skipItems(a, skipTaskOf(t, a, "n.md")))
	if !ok {
		t.Fatal("the task menu has no Skip this time")
	}
	item.run(a)
	want := "- [ ] water @repeat:monthly due:2026-12-01\n- [-] water due:2026-11-01\n"
	if got := skipRead(t, root, "n.md"); got != want {
		t.Fatalf("note after skip:\n%s\nwant:\n%s", got, want)
	}
	if a.message != "Skipped. Next: 2026-12-01" {
		t.Fatalf("notice = %q", a.message)
	}
}

func TestCheckOffFromTheTasksViewCountsFromTheDailyNoteDate(t *testing.T) {
	const task = "- [ ] water @repeat:monthly\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
		path  string
		want  string
		note  string
	}{
		{
			"daily note, setting on",
			map[string]string{".zennotes/vault.json": skipDailySettings, skipDailyPath: task},
			skipDailyPath,
			"- [ ] water @repeat:monthly due:2026-12-18\n- [x] water\n",
			"Next: 2026-12-18",
		},
		{
			"daily note, setting off",
			map[string]string{".zennotes/vault.json": skipDailyOff, skipDailyPath: task},
			skipDailyPath,
			"- [ ] water @repeat:monthly due:2026-12-25\n- [x] water\n",
			"Next: 2026-12-25",
		},
		{
			"plain note",
			map[string]string{".zennotes/vault.json": skipDailySettings, "n.md": task},
			"n.md",
			"- [ ] water @repeat:monthly due:2026-12-25\n- [x] water\n",
			"Next: 2026-12-25",
		},
		{
			"line due wins",
			map[string]string{".zennotes/vault.json": skipDailySettings, skipDailyPath: "- [ ] water @repeat:monthly due:2026-11-04\n"},
			skipDailyPath,
			"- [ ] water @repeat:monthly due:2026-12-04\n- [x] water due:2026-11-04\n",
			"Next: 2026-12-04",
		},
		{
			"frontmatter due wins",
			map[string]string{".zennotes/vault.json": skipDailySettings, skipDailyPath: "---\ndue: 2026-11-04\n---\n" + task},
			skipDailyPath,
			"---\ndue: 2026-11-04\n---\n- [ ] water @repeat:monthly due:2026-12-04\n- [x] water\n",
			"Next: 2026-12-04",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, root := skipApp(t, tc.files)
			a.toggleTask(skipTaskOf(t, a, tc.path))
			if got := skipRead(t, root, tc.path); got != tc.want {
				t.Fatalf("note after check-off:\n%s\nwant:\n%s", got, tc.want)
			}
			if a.message != tc.note {
				t.Fatalf("notice = %q, want %q", a.message, tc.note)
			}
		})
	}
}

func TestCheckOffOfATaskWithAnInferredDuePassesThatDue(t *testing.T) {
	a, root := skipApp(t, map[string]string{"n.md": "- [ ] water @repeat:monthly\n"})
	task := skipTaskOf(t, a, "n.md")
	task.Due, task.DueInferred = "2026-11-10", true
	a.toggleTask(task)
	want := "- [ ] water @repeat:monthly due:2026-12-10\n- [x] water\n"
	if got := skipRead(t, root, "n.md"); got != want {
		t.Fatalf("note after check-off:\n%s\nwant:\n%s", got, want)
	}
}

func TestBoardMoveToDoneAndMenuToggleCountFromTheDailyNoteDate(t *testing.T) {
	files := map[string]string{".zennotes/vault.json": skipDailySettings, skipDailyPath: "- [ ] water @repeat:monthly\n"}
	want := "- [ ] water @repeat:monthly due:2026-12-18\n- [x] water\n"
	a, root := skipApp(t, files)
	a.setTaskStatusColumn(skipTaskOf(t, a, skipDailyPath), "done")
	if got := skipRead(t, root, skipDailyPath); got != want {
		t.Fatalf("after a board move to Done:\n%s", got)
	}
	a, root = skipApp(t, files)
	for _, it := range skipItems(a, skipTaskOf(t, a, skipDailyPath)) {
		if it.label == "Toggle done" {
			it.run(a)
		}
	}
	if got := skipRead(t, root, skipDailyPath); got != want {
		t.Fatalf("after the menu Toggle done:\n%s", got)
	}
}

func TestOrdinaryTaskStillTogglesWithoutANotice(t *testing.T) {
	a, root := skipApp(t, map[string]string{"n.md": "- [ ] water\n"})
	a.toggleTask(skipTaskOf(t, a, "n.md"))
	if got := skipRead(t, root, "n.md"); got != "- [x] water\n" {
		t.Fatalf("note = %q", got)
	}
	if a.message != "" {
		t.Fatalf("notice = %q", a.message)
	}
}

// remoteNoteServer is a server with one note that serves reads and writes.
func remoteNoteServer(t *testing.T, path, body string) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	current := body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/notes/read":
			_ = json.NewEncoder(w).Encode(vault.NoteContent{NoteMeta: vault.NoteMeta{Path: path, Title: "n"}, Body: current})
		case "/api/notes/write":
			var in struct{ Path, Body string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			current = in.Body
			_ = json.NewEncoder(w).Encode(vault.NoteMeta{Path: path, Title: "n"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() string { mu.Lock(); defer mu.Unlock(); return current }
}

func TestSkipAndCheckOffWorkOnARemoteVault(t *testing.T) {
	for _, skip := range []bool{true, false} {
		a, _ := skipApp(t, map[string]string{"n.md": "- [ ] x\n"})
		srv, stored := remoteNoteServer(t, "n.md", "- [ ] water @repeat:monthly\n")
		target := backend.Target{Kind: backend.KindRemote, BaseURL: srv.URL}
		b, err := backend.New(target, backend.Options{})
		if err != nil {
			t.Fatal(err)
		}
		a.backend, a.opts.Backend, a.opts.Target = b, b, target
		task := vault.Task{SourcePath: "n.md", TaskIndex: 0, Content: "water", RawText: "- [ ] water @repeat:monthly", Due: "2026-11-18", DueInferred: true}
		want, notice := "- [ ] water @repeat:monthly due:2026-12-18\n- [x] water\n", "Next: 2026-12-18"
		if skip {
			for _, it := range skipItems(a, task) {
				if it.label == "Skip this time" {
					it.run(a)
				}
			}
			want, notice = strings.Replace(want, "[x]", "[-]", 1), "Skipped. "+notice
		} else {
			a.toggleTask(task)
		}
		if got := stored(); got != want {
			t.Errorf("skip=%v: remote note:\n%s\nwant:\n%s", skip, got, want)
		}
		if a.message != notice {
			t.Errorf("skip=%v: notice = %q, want %q", skip, a.message, notice)
		}
	}
}

// A roll writes lines above the task, so a cursor on or below the task moves
// down with the text, and a cursor above it stays.
func TestRollKeepsTheCursorOnItsLineInAnOpenNote(t *testing.T) {
	const note = "intro\n- [ ] water @repeat:monthly due:2026-11-01\nafter\n"
	for _, tc := range []struct {
		name       string
		line, want int
	}{
		{"above the task", 0, 0},
		{"on the task", 1, 2},
		{"below the task", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := skipApp(t, map[string]string{"n.md": note})
			a.openNote("n.md", true)
			buf := a.buffers["n.md"]
			buf.ed.SetCursor(vim.Pos{Line: tc.line})
			a.rollRecurringTask(skipTaskOf(t, a, "n.md"), false)
			if got := buf.ed.Cursor().Line; got != tc.want {
				t.Fatalf("cursor line = %d, want %d\n%s", got, tc.want, buf.ed.Text())
			}
		})
	}
}
