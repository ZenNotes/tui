package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func taskVault(t *testing.T, note string) (Backend, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(root, "inbox", "N.md")
	if err := os.WriteFile(abs, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := New(Target{Kind: KindLocal, Root: root}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return b, abs
}

func TestUpdateTaskEditsOnlyWhatItWasAsked(t *testing.T) {
	ctx := context.Background()
	b, abs := taskVault(t, "- [ ] one due:2026-01-01 !low #tag\n- [x] two\n- [>] sent\n")
	task, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#0", Due: strPtr("2026-02-02")})
	if err != nil || task == nil || task.Due != "2026-02-02" || task.Priority != "low" {
		t.Fatalf("%+v %v", task, err)
	}
	if _, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#0", Priority: strPtr(""), Waiting: boolPtr(true), Status: strPtr("doing")}); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#1", State: strPtr("open")}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(abs)
	if string(got) != "- [ ] one #tag due:2026-02-02 @waiting @status:doing\n- [ ] two\n- [>] sent\n" {
		t.Fatalf("%q", got)
	}
	if _, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#2", State: strPtr("done")}); err == nil {
		t.Fatal("a forwarded record keeps its marker")
	}
	for _, state := range []struct {
		name string
		mark string
	}{{"in-progress", "/"}, {"cancelled", "-"}, {"done", "x"}, {"open", " "}} {
		if _, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#1", State: strPtr(state.name)}); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(abs)
		if want := "- [" + state.mark + "] two\n"; !strings.Contains(string(got), want) {
			t.Fatalf("%s: %q", state.name, got)
		}
	}
	task, err = UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#1", Text: strPtr("renamed !high")})
	if err != nil || task.Priority != "high" {
		t.Fatalf("%+v %v", task, err)
	}
	if task, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#42", Due: strPtr("2026-02-02")}); err != nil || task != nil {
		t.Fatalf("a drifted id is nil: %+v %v", task, err)
	}
	for _, bad := range []UpdateTaskInput{
		{ID: "inbox/N.md#0"},
		{ID: "inbox/N.md#0", Due: strPtr("tomorrow")},
		{ID: "inbox/N.md#0", Priority: strPtr("urgent")},
		{ID: "inbox/N.md#0", State: strPtr("gone")},
		{ID: "inbox/N.md#0", Status: strPtr("two words")},
		{ID: "inbox/N.md#0", Text: strPtr("two\nlines")},
		{ID: "inbox/N.md#0", Text: strPtr("  ")},
	} {
		if _, err := UpdateTask(ctx, b, bad); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
}

// The line setters count tasks with the editor's fence rule and an id comes
// from the CLI dialect's. A four-space fence is a fence to one and not the
// other, so counting again could land on the wrong line; the edit must follow
// the line the scan pointed at.
func TestUpdateTaskFollowsTheScanNotASecondCount(t *testing.T) {
	ctx := context.Background()
	note := "    ```\n- [ ] first\n    ```\n- [ ] second\n"
	b, abs := taskVault(t, note)
	tasks, err := b.ScanTasksForPath(ctx, "inbox/N.md", vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectCLI})
	if err != nil || len(tasks) != 2 {
		t.Fatalf("%+v %v", tasks, err)
	}
	if vault.SetTaskDue(note, 0, "2026-02-02") != "    ```\n- [ ] first\n    ```\n- [ ] second due:2026-02-02\n" {
		t.Skip("the two dialects agree on this note now, so it proves nothing")
	}
	if _, err := UpdateTask(ctx, b, UpdateTaskInput{ID: tasks[0].ID, Due: strPtr("2026-02-02")}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(abs)
	if string(got) != "    ```\n- [ ] first due:2026-02-02\n    ```\n- [ ] second\n" {
		t.Fatalf("%q", got)
	}
}

func TestUpdateTaskOnATaskNote(t *testing.T) {
	ctx := context.Background()
	b, abs := taskVault(t, "---\ntags: [task]\nstatus: open\n---\n# Do it\n")
	task, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#task", Due: strPtr("2026-02-02"), Priority: strPtr("high"), State: strPtr("done")})
	if err != nil || task == nil || task.Due != "2026-02-02" || !task.Checked {
		t.Fatalf("%+v %v", task, err)
	}
	got, _ := os.ReadFile(abs)
	if !strings.Contains(string(got), "due: 2026-02-02") || !strings.Contains(string(got), "status: done") || !strings.Contains(string(got), "priority: high") {
		t.Fatalf("%q", got)
	}
	if _, err := UpdateTask(ctx, b, UpdateTaskInput{ID: "inbox/N.md#task", Text: strPtr("x")}); err == nil {
		t.Fatal("a task note has no line of text")
	}
}
