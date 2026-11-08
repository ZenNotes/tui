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

// A `scheduled:` token is plain text, so it stays in the content and sets no
// date.
func TestTaskListJSONKeepsScheduledTokenInContent(t *testing.T) {
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
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(tasks) != 1 || tasks[0]["content"] != "Draft scheduled:2026-11-05 the report" {
		t.Fatalf("got %+v", tasks)
	}
	for _, key := range []string{"scheduled", "scheduledTime", "due"} {
		if _, present := tasks[0][key]; present {
			t.Errorf("key %q present: %+v", key, tasks[0])
		}
	}
}

// A vault with checkbox `scheduled:` tokens, prose that mentions `when:` and
// no new syntax lists the same bytes as released zn v0.17.1. The files were
// made by running `zn task list --json`, `zn task list --json --all` and
// `zn task list` with zn v0.17.1 over testdata/released-tasks.
func TestTaskListMatchesReleasedOutputForNotesWithoutNewSyntax(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "released-tasks"))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		golden string
		flags  []string
	}{
		{"released-tasks.json", []string{"--json"}},
		{"released-tasks-all.json", []string{"--json", "--all"}},
		{"released-tasks.txt", nil},
	} {
		want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
		if err != nil {
			t.Fatal(err)
		}
		args := append([]string{"task", "list", "--vault", root}, tc.flags...)
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

// A vault with only the `@repeat:` values of released zn (daily, weekly,
// weekdays, `after-` and `@remind:`) lists the same text as released zn
// v0.17.1. The file was made by running `zn task list` with zn v0.17.1 over
// testdata/released-repeat.
func TestTaskListPlainMatchesReleasedForUpstreamRepeat(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "released-repeat"))); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "released-repeat.txt"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"task", "list", "--vault", root}
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	if out != string(want) {
		t.Errorf("zn %v differs from testdata/released-repeat.txt:\n%s", args, out)
	}
}

// A task note's `scheduled` key gives no JSON key and does not move on a roll:
// the date counts from `due`.
func TestTaskNoteScheduledKeyIsNotRead(t *testing.T) {
	for _, value := range []string{"2026-11-05", "2026-11-05T09:30"} {
		fixClock(t, "2026-11-10")
		note := "---\ntags: [task]\nstatus: open\nscheduled: " + value + "\ndue: 2026-11-10\nrepeat: 1w\n---\n\n# Class\n"
		root := isolatedCLI(t)
		if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "inbox", "note.md"), []byte(note), 0o644); err != nil {
			t.Fatal(err)
		}
		args := []string{"task", "list", "--json", "--vault", root}
		out := captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("zn %v exited %d", args, code)
			}
		})
		var tasks []map[string]any
		if err := json.Unmarshal([]byte(out), &tasks); err != nil || len(tasks) != 1 {
			t.Fatalf("%v\n%s", err, out)
		}
		for _, key := range []string{"scheduled", "scheduledTime"} {
			if _, present := tasks[0][key]; present {
				t.Errorf("%s: key %q present: %+v", value, key, tasks[0])
			}
		}
		got, _ := toggleNoteIn(t, root, note, "task")
		for _, want := range []string{"due: 2026-11-17", "lastCompleted: 2026-11-10", "scheduled: " + value} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in:\n%s", value, want, got)
			}
		}
	}
}

// A vault with event notes and work sessions lists only checkbox lines and
// task notes. No entry stands for an event or a session, and no entry has a
// `scheduled`, `scheduledTime` or `scheduledEnd` key.
func TestTaskListShowsNoEventsOrSessions(t *testing.T) {
	isolatedCLI(t)
	fixClock(t, "2026-11-02")
	root := writeVaultNotes(t, map[string]string{
		"inbox/Meeting.md": "---\npage-type: event\nwhen: 2026-11-09T14:00-15:00\nrepeat: 1w\n---\n\n# Team meeting\n",
		"inbox/Work.md":    "- [ ] Water plants repeat:mon,thu skip:@Holiday until:2026-12-31 ^ab12cd\n",
		"inbox/Essay.md":   "---\npage-type: task\nstatus: open\ndue: 2026-11-10\n---\n\n# Essay\n",
		"quick/Plans.md": "- Water plants task:[[Work#^ab12cd]] when:2026-11-09T09:00-09:15\n" +
			"- Essay task:[[Essay]] when:2026-11-08T10:00\n- Lunch when:2026-11-09T12:00\n",
	})
	for _, flags := range [][]string{nil, {"--all"}} {
		args := append([]string{"task", "list", "--json", "--vault", root}, flags...)
		out := captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("zn %v exited %d", args, code)
			}
		})
		var tasks []map[string]any
		if err := json.Unmarshal([]byte(out), &tasks); err != nil {
			t.Fatalf("zn %v: %v\n%s", args, err, out)
		}
		ids := []string{}
		for _, task := range tasks {
			ids = append(ids, task["id"].(string))
			for _, key := range []string{"scheduled", "scheduledTime", "scheduledEnd"} {
				if _, present := task[key]; present {
					t.Errorf("zn %v: key %q present: %+v", args, key, task)
				}
			}
		}
		slices.Sort(ids)
		want := []string{"inbox/Essay.md#task", "inbox/Work.md#0"}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("zn %v: ids = %v, want %v", args, ids, want)
		}
	}
}

func listTaskJSON(t *testing.T, note string) []map[string]any {
	t.Helper()
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"task", "list", "--json", "--vault", root}
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return tasks
}

// The JSON reports a task's time of day and repeat rule next to its dates.
func TestTaskListJSONCarriesTimesAndRepeat(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] Water plants due:2026-11-03T14:00 repeat:Mon,Thu\n")
	if len(tasks) != 1 {
		t.Fatalf("got %+v", tasks)
	}
	got := tasks[0]
	if got["due"] != "2026-11-03" || got["dueTime"] != "14:00" ||
		got["repeat"] != "mon,thu" || got["content"] != "Water plants" {
		t.Fatalf("got %+v", got)
	}
}

// A task that uses none of the scheduling syntax gains no new JSON keys.
func TestTaskListJSONOmitsSchedulingKeysWhenUnset(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] Pay rent due:2026-11-03\n")
	if len(tasks) != 1 {
		t.Fatalf("got %+v", tasks)
	}
	for _, key := range []string{"dueTime", "repeat"} {
		if _, present := tasks[0][key]; present {
			t.Errorf("key %q present: %+v", key, tasks[0])
		}
	}
}

// The JSON lists a task's valid skip items in their written form and drops
// the reversed ones.
func TestTaskListJSONCarriesValidSkipItems(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] A repeat:1w skip:2027-03-13..2027-03-21,2027-04-02,2027-03-21..2027-03-13\n")
	if len(tasks) != 1 {
		t.Fatalf("got %+v", tasks)
	}
	got, _ := tasks[0]["skip"].([]any)
	if !reflect.DeepEqual(got, []any{"2027-03-13..2027-03-21", "2027-04-02"}) {
		t.Fatalf("skip: %+v", tasks[0])
	}
	if tasks[0]["content"] != "A" {
		t.Fatalf("content: %+v", tasks[0])
	}
}

// A task without a skip token has no skip key in its JSON.
func TestTaskListJSONOmitsSkipWhenUnset(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] A repeat:1w\n- [ ] B skip:@spring-break\n")
	if len(tasks) != 2 {
		t.Fatalf("got %+v", tasks)
	}
	for _, task := range tasks {
		if _, present := task["skip"]; present {
			t.Errorf("skip key present: %+v", task)
		}
	}
}

// A note marked `page-type: task` is toggled like a tagged task note: the
// whole note becomes done.
func TestTaskToggleCompletesPageTypeTaskNote(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "rent.md")
	note := "---\npage-type: task\nstatus: open\n---\n\n# Pay rent\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"task", "toggle", "inbox/rent.md#task", "--vault", root}
	captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "status: done") {
		t.Fatalf("note not completed:\n%s", got)
	}
}

// The JSON lists the until date of a repeating task, and leaves it out when
// the task has none or the date does not count.
func TestTaskListJSONCarriesUntil(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] A repeat:1w until:2027-03-21\n- [ ] B repeat:1w\n- [ ] C until:2027-03-21\n")
	if len(tasks) != 3 {
		t.Fatalf("got %+v", tasks)
	}
	if tasks[0]["until"] != "2027-03-21" || tasks[0]["content"] != "A" {
		t.Fatalf("A: %+v", tasks[0])
	}
	for _, task := range tasks[1:] {
		if _, present := task["until"]; present {
			t.Errorf("until key present: %+v", task)
		}
	}
	if tasks[2]["content"] != "C" {
		t.Errorf("C: %+v", tasks[2])
	}
}

// The JSON gives a task's block id as `blockId` and leaves the key out for a
// task without one.
func TestTaskListJSONCarriesBlockId(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] A ^ab12cd\n- [ ] B\n")
	if len(tasks) != 2 {
		t.Fatalf("got %+v", tasks)
	}
	if tasks[0]["blockId"] != "ab12cd" || tasks[0]["content"] != "A" {
		t.Fatalf("A: %+v", tasks[0])
	}
	if _, present := tasks[1]["blockId"]; present {
		t.Errorf("blockId key present: %+v", tasks[1])
	}
}

// Plain-text `zn task list` writes a due time or range as in the note, in
// 24-hour form, before the priority. Its text keeps a `scheduled:` token.
func TestTaskListPlainWritesDueAsInNote(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	note := "- [ ] Call Ana due:2026-11-03T14:00-15:30 scheduled:2026-11-02T09:00 !high\n" +
		"- [ ] Plain due:2026-11-04 !low\n- [ ] Timed due:2026-11-05T08:15\n"
	if err := os.WriteFile(filepath.Join(root, "todo.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"task", "list", "--vault", root}
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %q", out)
	}
	if !strings.HasSuffix(lines[0], "  due:2026-11-03T14:00-15:30  !high") || !strings.Contains(lines[0], "scheduled:2026-11-02T09:00") {
		t.Errorf("range line %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "  due:2026-11-04  !low") {
		t.Errorf("date line %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], "  due:2026-11-05T08:15") {
		t.Errorf("time line %q", lines[2])
	}
}
