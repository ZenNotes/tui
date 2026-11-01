package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// A `[-]` cancelled task is abandoned, so `zn task list` leaves it out by
// default and with --unchecked, as the desktop app does
// (ZenNotes/zennotes#929); --all still lists it.
func TestTaskListLeavesCancelledTasksOut(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "- [ ] Buy milk\n- [-] Cancelled plan\n- [x] Done thing\n"
	if err := os.WriteFile(filepath.Join(root, "inbox", "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	listed := func(flags ...string) []string {
		args := append([]string{"task", "list", "--json", "--vault", root}, flags...)
		out := captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("zn %v exited %d", args, code)
			}
		})
		var tasks []struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(out), &tasks); err != nil {
			t.Fatalf("zn %v: %v\n%s", args, err, out)
		}
		contents := []string{}
		for _, task := range tasks {
			contents = append(contents, task.Content)
		}
		return contents
	}
	if got := listed(); !reflect.DeepEqual(got, []string{"Buy milk"}) {
		t.Fatalf("default: %v", got)
	}
	if got := listed("--unchecked"); !reflect.DeepEqual(got, []string{"Buy milk"}) {
		t.Fatalf("--unchecked: %v", got)
	}
	if got := listed("--all"); !slices.Contains(got, "Cancelled plan") {
		t.Fatalf("--all: %v", got)
	}
}

// Finishing a recurring task moves it below its next occurrence, so the id to
// print is the one it has now.
func TestTaskTogglePrintsWhereARecurringTaskWent(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "n.md"), []byte("- [ ] water @repeat:daily due:2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, func() {
		if code := Main([]string{"task", "toggle", "n.md#0", "--vault", root}); code != 0 {
			t.Fatalf("exit %d", code)
		}
	})
	if !strings.Contains(out, "n.md#1") {
		t.Fatalf("%q", out)
	}
	data, err := os.ReadFile(filepath.Join(root, "n.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- [x] water due:2026-01-01") || !strings.Contains(string(data), "- [ ] water @repeat:daily due:") {
		t.Fatalf("%q", data)
	}
}

// A vault that uses none of the scheduling syntax (no times, no `scheduled:`,
// no `repeat:` or `skip:`, no `page-type`) lists the same JSON bytes it always
// did, with and without --all.
func TestTaskListJSONUnchangedForNotesWithoutSchedulingSyntax(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "default-tasks"))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		golden string
		flags  []string
	}{
		{"default-tasks.json", nil},
		{"default-tasks-all.json", []string{"--all"}},
	} {
		want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
		if err != nil {
			t.Fatal(err)
		}
		args := append([]string{"task", "list", "--json", "--vault", root}, tc.flags...)
		out := captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("zn %v exited %d", args, code)
			}
		})
		if out != string(want) {
			t.Errorf("zn %v differs from testdata/%s:\n%s", args, tc.golden, out)
		}
	}
}

// A `scheduled:` token is read as a date, so it leaves the content instead of
// lingering in the task text.
func TestTaskListJSONStripsScheduledFromContent(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	note := "- [ ] Draft scheduled:2026-11-05 the report\n"
	if err := os.WriteFile(filepath.Join(root, "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"task", "list", "--json", "--vault", root}
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	var tasks []struct {
		Content   string `json:"content"`
		Scheduled string `json:"scheduled"`
	}
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(tasks) != 1 || tasks[0].Content != "Draft the report" || tasks[0].Scheduled != "2026-11-05" {
		t.Fatalf("got %+v", tasks)
	}
}
