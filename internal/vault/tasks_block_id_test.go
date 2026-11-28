package vault

import "testing"

// A `^id` marker only names its line while it ends it (ZenNotes/zennotes#601),
// so every writer that grows a task line has to put its token in front of the
// marker. Mirrors packages/shared-domain/src/tasklists-block-id.test.ts; zn has
// no first-tag writer, so the desktop's tag cases have nothing to run against.

func wantLine(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func wantAnchor(t *testing.T, body, id string) {
	t.Helper()
	if _, ok := AnchorLine(body, "Plan^"+id); !ok {
		t.Fatalf("^%s no longer resolves in %q", id, body)
	}
}

func onlyTask(t *testing.T, body string) Task {
	t.Helper()
	tasks := ParseTasks("Plan.md", "Plan", FolderInbox, body, ParseTasksOptions{Dialect: DialectApp})
	if len(tasks) != 1 {
		t.Fatalf("parsed %d tasks from %q", len(tasks), body)
	}
	return tasks[0]
}

func TestBlockIDDueGoesBeforeTheID(t *testing.T) {
	saved := SetTaskDue("- [ ] Call Sam ^call", 0, "2026-12-02")
	wantLine(t, saved, "- [ ] Call Sam due:2026-12-02 ^call")
	wantAnchor(t, saved, "call")
	if got := onlyTask(t, saved).Due; got != "2026-12-02" {
		t.Fatalf("due %q", got)
	}
	wantLine(t, SetTaskDue(saved, 0, "2026-12-05"), "- [ ] Call Sam due:2026-12-05 ^call")
	wantLine(t, SetTaskDue(saved, 0, ""), "- [ ] Call Sam ^call")
}

func TestBlockIDPriorityGoesBeforeTheID(t *testing.T) {
	saved := SetTaskPriority("- [ ] Call Sam ^call", 0, "high")
	wantLine(t, saved, "- [ ] Call Sam !high ^call")
	wantAnchor(t, saved, "call")
	if got := onlyTask(t, saved).Priority; got != "high" {
		t.Fatalf("priority %q", got)
	}
	wantLine(t, SetTaskPriority(saved, 0, ""), "- [ ] Call Sam ^call")
}

func TestBlockIDBothTogetherStillEndWithTheID(t *testing.T) {
	a := SetTaskPriority(SetTaskDue("- [ ] Gym ^gym", 0, "2026-12-02"), 0, "low")
	wantLine(t, a, "- [ ] Gym due:2026-12-02 !low ^gym")
	b := SetTaskField(a, 0, "status", "next")
	wantLine(t, b, "- [ ] Gym due:2026-12-02 !low @status:next ^gym")
}

func TestBlockIDLineWrittenWrongHealsOnTheNextWrite(t *testing.T) {
	wantLine(t, SetTaskDue("- [ ] Gym ^gym due:2026-12-01", 0, "2026-12-02"), "- [ ] Gym due:2026-12-02 ^gym")
}

func TestBlockIDForwardLinkGoesBeforeTheID(t *testing.T) {
	body, _ := ForwardTaskSubtree("- [ ] Call Sam ^call", 0, "[[2026-12-02]]")
	wantLine(t, body, "- [>] Call Sam [[2026-12-02]] ^call")
}

// zn's own writer; the desktop's setTaskWaitingAtIndex still appends after the
// marker.
func TestBlockIDWaitingGoesBeforeTheID(t *testing.T) {
	saved := SetTaskWaiting("- [ ] Call Sam ^call", 0, true)
	wantLine(t, saved, "- [ ] Call Sam @waiting ^call")
	wantAnchor(t, saved, "call")
	wantLine(t, SetTaskWaiting(saved, 0, false), "- [ ] Call Sam ^call")
}

func TestBlockIDLineWithoutAnIDIsUnchanged(t *testing.T) {
	wantLine(t, SetTaskDue("- [ ] Call Sam  ", 0, "2026-12-02"), "- [ ] Call Sam due:2026-12-02")
	wantLine(t, SetTaskPriority("- [ ] Call Sam", 0, "med"), "- [ ] Call Sam !med")
	wantLine(t, SetTaskField("- [ ] Call Sam", 0, "status", "next"), "- [ ] Call Sam @status:next")
	wantLine(t, SetTaskWaiting("- [ ] Call Sam", 0, true), "- [ ] Call Sam @waiting")
}

func TestBlockIDCaretThatIsNotAnIDStaysPut(t *testing.T) {
	wantLine(t, SetTaskDue("- [ ] Solve 2^3", 0, "2026-12-02"), "- [ ] Solve 2^3 due:2026-12-02")
}
