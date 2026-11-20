package vault

import (
	"strings"
	"testing"
)

// Each setter that adds a token to a task line keeps the block id at the end
// of the line, so the task keeps its id.
func TestTaskSettersKeepTrailingBlockID(t *testing.T) {
	const line = "- [ ] T ^ab12cd"
	cases := map[string]func(string) string{
		"due":      func(b string) string { return SetTaskDue(b, 0, "2099-01-01") },
		"due time": func(b string) string { return SetTaskDue(b, 0, "2099-01-01T09:00-10:00") },
		"field":    func(b string) string { return SetTaskField(b, 0, "area", "home") },
		"priority": func(b string) string { return SetTaskPriority(b, 0, "high") },
		"waiting":  func(b string) string { return SetTaskWaiting(b, 0, true) },
	}
	for name, set := range cases {
		got := set(line + "\n")
		if !strings.HasSuffix(strings.TrimRight(got, "\n"), " ^ab12cd") {
			t.Errorf("%s: block id is not last: %q", name, got)
		}
		if strings.Count(got, "^ab12cd") != 1 {
			t.Errorf("%s: block id count in %q", name, got)
		}
		tasks := ParseTasks("n.md", "n", FolderInbox, got, ParseTasksOptions{Dialect: DialectApp})
		if len(tasks) != 1 || tasks[0].BlockID != "ab12cd" {
			t.Errorf("%s: parsed tasks = %+v", name, tasks)
		}
	}
	if got := SetTaskDue(line+"\n", 0, "2099-01-01"); got != "- [ ] T due:2099-01-01 ^ab12cd\n" {
		t.Errorf("due line = %q", got)
	}
}

// A change of an existing token and a removal keep the block id too.
func TestTaskSettersReplaceAndRemoveKeepBlockID(t *testing.T) {
	line := "- [ ] T due:2099-01-01 !high @waiting @area:home ^ab12cd\n"
	for name, got := range map[string]string{
		"due changed":      SetTaskDue(line, 0, "2099-02-02"),
		"due removed":      SetTaskDue(line, 0, ""),
		"priority changed": SetTaskPriority(line, 0, "low"),
		"priority removed": SetTaskPriority(line, 0, ""),
		"waiting removed":  SetTaskWaiting(line, 0, false),
		"field changed":    SetTaskField(line, 0, "area", "work"),
		"field removed":    SetTaskField(line, 0, "area", ""),
	} {
		if !strings.HasSuffix(strings.TrimRight(got, "\n"), " ^ab12cd") {
			t.Errorf("%s: block id is not last: %q", name, got)
		}
	}
}

func TestWithDueKeepsTrailingBlockID(t *testing.T) {
	if got := withDue("- [ ] T ^ab12cd", "2099-01-01"); got != "- [ ] T due:2099-01-01 ^ab12cd" {
		t.Errorf("withDue = %q", got)
	}
	if got := withDue("- [ ] T", "2099-01-01"); got != "- [ ] T due:2099-01-01" {
		t.Errorf("withDue without id = %q", got)
	}
}
