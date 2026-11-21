package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
)

// list_tasks reports the end of a ranged due time, and only when there is one.
func TestListTasksCarriesDueEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "- [ ] Call Ana due:2026-11-03T14:00-15:30\n- [ ] Pay rent due:2026-11-03T14:00\n"
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
	if len(tasks) != 2 {
		t.Fatalf("got %s", text)
	}
	for _, task := range tasks {
		switch task["content"] {
		case "Call Ana":
			if task["dueEnd"] != "15:30" {
				t.Errorf("ranged: %+v", task)
			}
		case "Pay rent":
			if _, present := task["dueEnd"]; present {
				t.Errorf("dueEnd present on a task with no range: %+v", task)
			}
		}
	}
}
