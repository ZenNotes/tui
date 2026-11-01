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
	note := "- [ ] Water plants scheduled:2026-11-02T09:30 repeat:mon,thu\n- [ ] Pay rent due:2026-11-03\n"
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
	if water["repeat"] != "mon,thu" || water["scheduledTime"] != "09:30" {
		t.Fatalf("water: %+v", water)
	}
	for _, key := range []string{"repeat", "dueTime", "scheduledTime"} {
		if _, present := rent[key]; present {
			t.Errorf("key %q present on a plain task: %+v", key, rent)
		}
	}
}
