package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
)

// A `[-]` cancelled task is abandoned, so list_tasks leaves it out of open,
// as the desktop app does (ZenNotes/zennotes#929); status "all" keeps it.
func TestListTasksLeavesCancelledTasksOutOfOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "- [ ] Buy milk\n- [-] Cancelled plan\n- [x] Done thing\n"
	if err := os.WriteFile(filepath.Join(root, "inbox", "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	contents := func(args map[string]any) []string {
		text, isErr := callTool(t, cs, "list_tasks", args)
		if isErr {
			t.Fatalf("list_tasks %v: %s", args, text)
		}
		var tasks []struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(text), &tasks); err != nil {
			t.Fatalf("list_tasks %v: %v\n%s", args, err, text)
		}
		out := []string{}
		for _, task := range tasks {
			out = append(out, task.Content)
		}
		return out
	}
	if got := contents(map[string]any{}); !reflect.DeepEqual(got, []string{"Buy milk"}) {
		t.Fatalf("open tasks: %v", got)
	}
	if got := contents(map[string]any{"status": "all"}); !slices.Contains(got, "Cancelled plan") {
		t.Fatalf("status all: %v", got)
	}
}

// list_tasks reports a task's times and repeat rule, and leaves the keys out
// of tasks that carry none.
func TestListTasksCarriesTimesAndRepeat(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "- [ ] Water plants due:2026-11-02T09:30 repeat:mon,thu\n- [ ] Pay rent due:2026-11-03\n"
	if err := os.WriteFile(filepath.Join(root, "inbox", "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "list_tasks", map[string]any{})
	if isErr {
		t.Fatalf("list_tasks: %s", text)
	}
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(text), &tasks); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	byContent := map[string]map[string]any{}
	for _, task := range tasks {
		byContent[task["content"].(string)] = task
	}
	water, rent := byContent["Water plants"], byContent["Pay rent"]
	if water == nil || rent == nil {
		t.Fatalf("got %s", text)
	}
	if water["repeat"] != "mon,thu" || water["dueTime"] != "09:30" {
		t.Fatalf("water: %+v", water)
	}
	for _, key := range []string{"repeat", "dueTime"} {
		if _, present := rent[key]; present {
			t.Errorf("key %q present on a plain task: %+v", key, rent)
		}
	}
}

// list_tasks reports a task's valid skip items, in their written form.
func TestListTasksCarriesValidSkipItems(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "- [ ] Assignment repeat:1w skip:2027-03-13..2027-03-21,2027-04-02,2027-03-21..2027-03-13\n- [ ] Pay rent\n"
	if err := os.WriteFile(filepath.Join(root, "inbox", "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "list_tasks", map[string]any{})
	if isErr {
		t.Fatalf("list_tasks: %s", text)
	}
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(text), &tasks); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	byContent := map[string]map[string]any{}
	for _, task := range tasks {
		byContent[task["content"].(string)] = task
	}
	assignment, rent := byContent["Assignment"], byContent["Pay rent"]
	if assignment == nil || rent == nil {
		t.Fatalf("got %s", text)
	}
	if got, _ := assignment["skip"].([]any); !reflect.DeepEqual(got, []any{"2027-03-13..2027-03-21", "2027-04-02"}) {
		t.Fatalf("assignment: %+v", assignment)
	}
	if _, present := rent["skip"]; present {
		t.Fatalf("skip key present on a plain task: %+v", rent)
	}
}

// list_tasks gives no scheduled key, for a checkbox token or a task note key,
// and keeps the token in the content.
func TestListTasksOmitsScheduled(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"todo.md": "- [ ] Draft scheduled:2026-11-05T09:30 the report\n",
		"note.md": "---\ntags: [task]\nscheduled: 2026-11-05T09:30\n---\n\n# Class\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, "inbox", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "list_tasks", map[string]any{})
	if isErr {
		t.Fatalf("list_tasks: %s", text)
	}
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(text), &tasks); err != nil || len(tasks) != 2 {
		t.Fatalf("%v\n%s", err, text)
	}
	for _, task := range tasks {
		for _, key := range []string{"scheduled", "scheduledTime"} {
			if _, present := task[key]; present {
				t.Errorf("key %q present: %+v", key, task)
			}
		}
		if task["noteTitle"] == "todo" && task["content"] != "Draft scheduled:2026-11-05T09:30 the report" {
			t.Errorf("content: %+v", task)
		}
	}
}
