package vault

import (
	"reflect"
	"testing"
)

func parseNote(body string) []Task {
	return ParseTasks("inbox/n.md", "n", FolderInbox, body, ParseTasksOptions{})
}

func wholeNoteTasks(tasks []Task) []Task {
	var out []Task
	for _, task := range tasks {
		if task.Kind == "file" {
			out = append(out, task)
		}
	}
	return out
}

// `page-type: task` marks a note as a task, as the `task` tag does, in
// plain, quoted and list form.
func TestPageTypeTaskMarksTaskNote(t *testing.T) {
	for name, value := range map[string]string{
		"plain":  "page-type: task",
		"double": `page-type: "task"`,
		"single": `page-type: 'task'`,
		"inline": "page-type: [task]",
		"block":  "page-type:\n  - task",
	} {
		t.Run(name, func(t *testing.T) {
			got := wholeNoteTasks(parseNote("---\n" + value + "\nstatus: open\ndue: 2026-11-03\n---\n\n# Pay rent\n"))
			if len(got) != 1 {
				t.Fatalf("got %d whole-note tasks, want 1", len(got))
			}
			if got[0].Due != "2026-11-03" || got[0].Status != "open" || got[0].ID != "inbox/n.md#task" {
				t.Fatalf("got %+v", got[0])
			}
		})
	}
}

// A note carrying both markers still yields a single task.
func TestPageTypeAndTaskTagYieldOneTask(t *testing.T) {
	got := wholeNoteTasks(parseNote("---\npage-type: task\ntags: [task]\n---\n\n# Both\n"))
	if len(got) != 1 {
		t.Fatalf("got %d whole-note tasks, want 1", len(got))
	}
}

// Only the exact lower-case key and value mark a task note. The frontmatter
// reader lower-cases keys, so the key is checked as written.
func TestPageTypeMatchIsCaseSensitive(t *testing.T) {
	for name, fm := range map[string]string{
		"value capital": "page-type: Task",
		"value upper":   "page-type: TASK",
		"key capital":   "Page-Type: task",
		"key upper":     "PAGE-TYPE: task",
		"other value":   "page-type: project",
		"list of two":   "page-type: [task, project]",
		"list of other": "page-type: [project]",
		"substring":     "page-type: tasks",
	} {
		t.Run(name, func(t *testing.T) {
			if got := wholeNoteTasks(parseNote("---\n" + fm + "\n---\n\n# N\n")); len(got) != 0 {
				t.Fatalf("got %d whole-note tasks, want 0", len(got))
			}
		})
	}
}

// A page-type task note keeps every tag it has; only `task` is dropped.
func TestPageTypeTaskNoteKeepsOtherTags(t *testing.T) {
	got := wholeNoteTasks(parseNote("---\npage-type: task\ntags: [home, chores]\n---\n\n# N\n"))
	if len(got) != 1 {
		t.Fatalf("got %d whole-note tasks, want 1", len(got))
	}
	if want := []string{"home", "chores"}; !reflect.DeepEqual(got[0].Tags, want) {
		t.Fatalf("tags = %v, want %v", got[0].Tags, want)
	}
}

// `tasks: false` still silences a page-type task note.
func TestPageTypeTaskNoteHonoursTasksFalse(t *testing.T) {
	if got := parseNote("---\npage-type: task\ntasks: false\n---\n\n# N\n- [ ] x\n"); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}
