package vault

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// These cases are the desktop's task-planning.test.ts at v2.94.0, so zn places
// a planned task on the day the app does. The desktop's tasksOnDay, its editor
// edit setPlannedDayField and its reminder case have no zn counterpart and are
// not ported.

// A Friday. The 25th is the Wednesday before it, the 30th the Monday after.
const planToday = "2026-11-27"

var planNow = time.Date(2026, 11, 27, 12, 0, 0, 0, time.Local)

func planLines(body string) []Task {
	return ParseTasks("inbox/t.md", "t", FolderInbox, body, ParseTasksOptions{Dialect: DialectApp})
}

func planParse(t *testing.T, line string) Task {
	t.Helper()
	tasks := planLines(line)
	if len(tasks) != 1 {
		t.Fatalf("%q parsed as %d tasks, want 1", line, len(tasks))
	}
	return tasks[0]
}

func planInDailyNote(t *testing.T, line, noteISO string) Task {
	t.Helper()
	path := "Daily/2026-11-25.md"
	tasks := ParseTasks(path, "2026-11-25", FolderInbox, line, ParseTasksOptions{Dialect: DialectApp})
	if len(tasks) != 1 {
		t.Fatalf("%q parsed as %d tasks, want 1", line, len(tasks))
	}
	return InferDailyTaskDueDates(tasks, map[string]string{path: noteISO})[0]
}

func planFileTask(t *testing.T, frontmatter string) Task {
	t.Helper()
	tasks := ParseTasks("inbox/Plan.md", "Plan", FolderInbox, "---\ntags: [task]\n"+frontmatter+"\n---\n", ParseTasksOptions{Dialect: DialectApp})
	if len(tasks) != 1 || tasks[0].Kind != "file" {
		t.Fatalf("frontmatter %q gave %+v, want one task note", frontmatter, tasks)
	}
	return tasks[0]
}

func planContents(tasks []Task) []string {
	out := []string{}
	for _, task := range tasks {
		out = append(out, task.Content)
	}
	return out
}

func TestPlannedDayReadsTheFieldATaskCarries(t *testing.T) {
	task := planParse(t, "- [ ] report due:2026-12-04 @scheduled:2026-12-02")
	if got := PlannedDay(task); got != "2026-12-02" {
		t.Errorf("PlannedDay = %q, want 2026-12-02", got)
	}
	if task.Due != "2026-12-04" || task.Content != "report" {
		t.Errorf("due = %q, content = %q, want 2026-12-04 and report", task.Due, task.Content)
	}

	note := planFileTask(t, "due: 2026-12-04\nscheduled: 2026-12-02")
	if got := PlannedDay(note); got != "2026-12-02" || note.Due != "2026-12-04" {
		t.Errorf("task note: PlannedDay = %q, due = %q, want 2026-12-02 and 2026-12-04", got, note.Due)
	}
}

func TestPlannedDayTreatsAnythingButADateAsInert(t *testing.T) {
	for _, value := range []string{"friday", "2026-12", "2026-12-02-14h00", "2026-12-02/afternoon", "20261202"} {
		task := planParse(t, "- [ ] report due:2026-12-04 @scheduled:"+value)
		if got := PlannedDay(task); got != "" {
			t.Errorf("%s: PlannedDay = %q, want none", value, got)
		}
		if got := TaskDay(task, planToday); got != "2026-12-04" {
			t.Errorf("%s: TaskDay = %q, want the due date", value, got)
		}
	}
	if ParsePlannedDay("") != "" || ParsePlannedDay("  ") != "" {
		t.Error("an empty value plans a day")
	}
}

func TestPlannedDayKeepsNoteAndLineApart(t *testing.T) {
	if got := PlannedDay(Task{Kind: "file", Fields: map[string]string{"scheduled": "2026-12-02"}}); got != "" {
		t.Errorf("a task note's field planned %q, want none", got)
	}
	if got := PlannedDay(Task{Scheduled: "2026-12-02", Fields: map[string]string{}}); got != "" {
		t.Errorf("a checkbox task's frontmatter planned %q, want none", got)
	}
}

// The CLI grammar reports no fields, so the plan comes off the line.
func TestPlannedDayUnderTheCLIGrammar(t *testing.T) {
	tasks := ParseTasks("inbox/t.md", "t", FolderInbox, "- [ ] report due:2026-12-04 @Scheduled:2026-12-02", ParseTasksOptions{Dialect: DialectCLI})
	if len(tasks) != 1 || tasks[0].Fields != nil {
		t.Fatalf("CLI scan = %+v, want one task without fields", tasks)
	}
	if got := PlannedDay(tasks[0]); got != "2026-12-02" {
		t.Errorf("PlannedDay = %q, want 2026-12-02", got)
	}
}

func TestTaskDayIsTheDeadlineWithoutAPlan(t *testing.T) {
	for line, want := range map[string]string{
		"- [ ] a due:2026-12-04": "2026-12-04",
		"- [ ] a due:2026-11-20": "2026-11-20",
		"- [ ] a":                "",
	} {
		if got := TaskDay(planParse(t, line), planToday); got != want {
			t.Errorf("%s: TaskDay = %q, want %q", line, got, want)
		}
	}
}

func TestTaskDayIsThePlanBeforeTheDeadline(t *testing.T) {
	task := planParse(t, "- [ ] a due:2026-12-04 @scheduled:2026-12-02")
	if got := TaskDay(task, planToday); got != "2026-12-02" {
		t.Errorf("TaskDay = %q, want 2026-12-02", got)
	}
	if got := DeadlineApartFromDay(task, planToday); got != "2026-12-04" {
		t.Errorf("DeadlineApartFromDay = %q, want 2026-12-04", got)
	}
	if IsOverdue(task, planNow) {
		t.Error("overdue, want not")
	}
}

func TestTaskDayIsThePlanWithoutADeadline(t *testing.T) {
	task := planParse(t, "- [ ] a @scheduled:2026-12-02")
	if got := TaskDay(task, planToday); got != "2026-12-02" {
		t.Errorf("TaskDay = %q, want 2026-12-02", got)
	}
	if got := DeadlineApartFromDay(task, planToday); got != "" {
		t.Errorf("DeadlineApartFromDay = %q, want none", got)
	}
}

func TestTaskDayStaysOnALatePlanUntilTheDeadlineArrives(t *testing.T) {
	task := planParse(t, "- [ ] a due:2026-12-01 @scheduled:2026-12-05")
	if got := TaskDay(task, planToday); got != "2026-12-05" {
		t.Errorf("TaskDay = %q, want 2026-12-05", got)
	}
	if got := DeadlineApartFromDay(task, planToday); got != "2026-12-01" {
		t.Errorf("DeadlineApartFromDay = %q, want 2026-12-01", got)
	}
	// The day it is due, the deadline takes over, and after it the task is late.
	if got := TaskDay(task, "2026-12-01"); got != "2026-12-01" {
		t.Errorf("on the deadline: TaskDay = %q, want 2026-12-01", got)
	}
	if got := TaskDay(task, "2026-12-03"); got != "2026-12-01" {
		t.Errorf("after the deadline: TaskDay = %q, want 2026-12-01", got)
	}
	if !IsOverdue(task, time.Date(2026, 12, 3, 12, 0, 0, 0, time.Local)) {
		t.Error("not overdue after the deadline")
	}
}

func TestTaskDayShowsATaskDueTodayAsDueToday(t *testing.T) {
	for _, line := range []string{"- [ ] a due:2026-11-27 @scheduled:2026-11-30", "- [ ] a due:2026-11-27 @scheduled:2026-11-25"} {
		if got := TaskDay(planParse(t, line), planToday); got != planToday {
			t.Errorf("%s: TaskDay = %q, want today", line, got)
		}
	}
}

func TestTaskDayShowsAnOverdueTaskOnItsDeadline(t *testing.T) {
	for _, planned := range []string{"2026-11-18", "2026-11-27", "2026-11-30"} {
		task := planParse(t, "- [ ] a due:2026-11-20 @scheduled:"+planned)
		if got := TaskDay(task, planToday); got != "2026-11-20" {
			t.Errorf("planned %s: TaskDay = %q, want 2026-11-20", planned, got)
		}
		if !IsOverdue(task, planNow) {
			t.Errorf("planned %s: not overdue", planned)
		}
		if got := DeadlineApartFromDay(task, planToday); got != "" {
			t.Errorf("planned %s: DeadlineApartFromDay = %q, want none", planned, got)
		}
	}
}

func TestTaskDayMovesAMissedPlanToTodayWithoutCallingItLate(t *testing.T) {
	future := planParse(t, "- [ ] a due:2026-12-04 @scheduled:2026-11-25")
	if got := TaskDay(future, planToday); got != planToday {
		t.Errorf("TaskDay = %q, want today", got)
	}
	if IsOverdue(future, planNow) {
		t.Error("a missed plan is overdue")
	}
	if got := DeadlineApartFromDay(future, planToday); got != "2026-12-04" {
		t.Errorf("DeadlineApartFromDay = %q, want 2026-12-04", got)
	}
	noDeadline := planParse(t, "- [ ] a @scheduled:2026-11-25")
	if got := TaskDay(noDeadline, planToday); got != planToday || IsOverdue(noDeadline, planNow) {
		t.Errorf("no deadline: TaskDay = %q, overdue = %v, want today and not late", got, IsOverdue(noDeadline, planNow))
	}
}

func TestTaskDayKeepsARecordOnTheDayItNames(t *testing.T) {
	for _, box := range []string{"x", "-", ">"} {
		if got := TaskDay(planParse(t, "- ["+box+"] a @scheduled:2026-11-25"), planToday); got != "2026-11-25" {
			t.Errorf("[%s]: TaskDay = %q, want 2026-11-25", box, got)
		}
	}
	// Started and waiting work is still live, so it follows to today.
	for _, line := range []string{"- [/] a @scheduled:2026-11-25", "- [ ] a @waiting @scheduled:2026-11-25"} {
		if got := TaskDay(planParse(t, line), planToday); got != planToday {
			t.Errorf("%s: TaskDay = %q, want today", line, got)
		}
	}
}

func TestTaskDayPlacesATaskNoteByItsFrontmatter(t *testing.T) {
	for fm, want := range map[string]string{
		"due: 2026-12-04\nscheduled: 2026-12-02": "2026-12-02",
		"scheduled: 2026-11-20":                  planToday,
		"due: 2026-11-20\nscheduled: 2026-12-02": "2026-11-20",
	} {
		if got := TaskDay(planFileTask(t, fm), planToday); got != want {
			t.Errorf("%q: TaskDay = %q, want %q", fm, got, want)
		}
	}
}

func TestDailyNoteTaskWithoutAPlanSitsOnTheNotesDate(t *testing.T) {
	task := planInDailyNote(t, "- [ ] a", "2026-11-25")
	if TaskDay(task, planToday) != "2026-11-25" || TaskDeadline(task) != "2026-11-25" || !IsOverdue(task, planNow) {
		t.Errorf("day %q, deadline %q, overdue %v, want the note's date and late", TaskDay(task, planToday), TaskDeadline(task), IsOverdue(task, planNow))
	}
}

func TestDailyNotePlanReplacesTheNotesDate(t *testing.T) {
	task := planInDailyNote(t, "- [ ] a @scheduled:2026-11-30", "2026-11-25")
	if got := TaskDeadline(task); got != "" {
		t.Errorf("TaskDeadline = %q, want none", got)
	}
	if got := TaskDay(task, planToday); got != "2026-11-30" {
		t.Errorf("TaskDay = %q, want 2026-11-30", got)
	}
	if IsOverdue(task, planNow) {
		t.Error("late by the note's date it was planned away from")
	}
	if got := DeadlineApartFromDay(task, planToday); got != "" {
		t.Errorf("DeadlineApartFromDay = %q, want none", got)
	}

	inToday := planInDailyNote(t, "- [ ] a @scheduled:2026-11-30", planToday)
	if got := TaskDay(inToday, planToday); got != "2026-11-30" {
		t.Errorf("today's note: TaskDay = %q, want 2026-11-30", got)
	}

	missed := planInDailyNote(t, "- [ ] a @scheduled:2026-11-26", "2026-11-25")
	if got := TaskDay(missed, planToday); got != planToday || IsOverdue(missed, planNow) {
		t.Errorf("missed: TaskDay = %q, overdue = %v, want today and not late", got, IsOverdue(missed, planNow))
	}
}

func TestDailyNoteKeepsAWrittenDueAsTheDeadline(t *testing.T) {
	task := planInDailyNote(t, "- [ ] a due:2026-11-26 @scheduled:2026-11-30", "2026-11-25")
	if task.DueInferred {
		t.Error("a written due: was marked inferred")
	}
	if TaskDay(task, planToday) != "2026-11-26" || !IsOverdue(task, planNow) {
		t.Errorf("TaskDay = %q, overdue = %v, want 2026-11-26 and late", TaskDay(task, planToday), IsOverdue(task, planNow))
	}
}

func TestGroupTasksPlacesByThePlannedDay(t *testing.T) {
	g := GroupTasks(planLines(strings.Join([]string{
		"- [ ] planned today due:2026-12-04 @scheduled:2026-11-27",
		"- [ ] planned monday due:2026-12-04 @scheduled:2026-11-30",
		"- [ ] due friday due:2026-12-04",
		"- [ ] planned only @scheduled:2026-12-01",
		"- [ ] nothing",
	}, "\n")), planNow)
	if got, want := planContents(g.Today), []string{"planned today"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Today = %v, want %v", got, want)
	}
	if got, want := planContents(g.Upcoming), []string{"planned monday", "planned only", "due friday"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Upcoming = %v, want %v", got, want)
	}
	if got, want := planContents(g.Undated), []string{"nothing"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Undated = %v, want %v", got, want)
	}
	if g.OverdueCount != 0 {
		t.Errorf("OverdueCount = %d, want 0", g.OverdueCount)
	}
}

func TestGroupTasksCountsOnlyAPassedDeadlineAsOverdue(t *testing.T) {
	g := GroupTasks(planLines(strings.Join([]string{
		"- [ ] missed plan due:2026-12-04 @scheduled:2026-11-25",
		"- [ ] late due:2026-11-20 @scheduled:2026-11-30",
		"- [ ] plainly late due:2026-11-26",
	}, "\n")), planNow)
	if got, want := planContents(g.Today), []string{"late", "plainly late", "missed plan"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Today = %v, want %v", got, want)
	}
	if g.OverdueCount != 2 || len(g.Upcoming) != 0 {
		t.Errorf("OverdueCount = %d, Upcoming = %v, want 2 and none", g.OverdueCount, planContents(g.Upcoming))
	}
}

func TestGroupTasksLeavesRecordsAndWaitingInTheirGroups(t *testing.T) {
	g := GroupTasks(planLines(strings.Join([]string{
		"- [x] done @scheduled:2026-11-27",
		"- [-] cancelled @scheduled:2026-11-27",
		"- [>] forwarded @scheduled:2026-11-27",
		"- [ ] waiting @waiting @scheduled:2026-11-27",
	}, "\n")), planNow)
	for name, got := range map[string][]Task{"done": g.Done, "cancelled": g.Cancelled, "forwarded": g.Forwarded, "waiting": g.Waiting} {
		if want := []string{name}; !reflect.DeepEqual(planContents(got), want) {
			t.Errorf("%s group = %v, want %v", name, planContents(got), want)
		}
	}
	if len(g.Today) != 0 || len(g.Upcoming) != 0 {
		t.Errorf("Today = %v, Upcoming = %v, want both empty", planContents(g.Today), planContents(g.Upcoming))
	}
}

func TestBucketTasksByDayPlacesEveryOpenTaskOnItsDay(t *testing.T) {
	tasks := planLines(strings.Join([]string{
		"- [ ] planned due:2026-12-04 @scheduled:2026-12-02",
		"- [ ] due only due:2026-12-04",
		"- [ ] missed @scheduled:2026-11-20",
		"- [ ] late due:2026-11-20 @scheduled:2026-12-02",
		"- [x] done @scheduled:2026-12-02",
		"- [-] cancelled @scheduled:2026-12-02",
		"- [ ] waiting @waiting @scheduled:2026-12-02",
		"- [>] forwarded @scheduled:2026-12-02",
		"- [>] forwarded bare",
		"- [ ] nothing",
	}, "\n"))
	got := map[string][]string{}
	for day, list := range BucketTasksByDay(tasks, planToday) {
		got[day] = planContents(list)
	}
	want := map[string][]string{
		"2026-12-02":  {"planned", "waiting", "forwarded"},
		"2026-12-04":  {"due only"},
		planToday:     {"missed"},
		"2026-11-20":  {"late"},
		"unscheduled": {"nothing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BucketTasksByDay = %v, want %v", got, want)
	}
}

func TestSetTaskFieldWritesReplacesAndClearsThePlan(t *testing.T) {
	planned := SetTaskField("- [ ] a due:2026-12-04", 0, PlannedDayField, "2026-12-02")
	if planned != "- [ ] a due:2026-12-04 @scheduled:2026-12-02" {
		t.Errorf("planned = %q", planned)
	}
	if got := planParse(t, planned).Due; got != "2026-12-04" {
		t.Errorf("due after planning = %q, want 2026-12-04", got)
	}
	if got := SetTaskField(planned, 0, PlannedDayField, "2026-12-03"); got != "- [ ] a due:2026-12-04 @scheduled:2026-12-03" {
		t.Errorf("replanned = %q", got)
	}
	if got := SetTaskField(planned, 0, PlannedDayField, ""); got != "- [ ] a due:2026-12-04" {
		t.Errorf("cleared = %q", got)
	}
}

func TestSetTaskFieldKeepsABlockIDAtTheEndOfItsLine(t *testing.T) {
	planned := SetTaskField("- [ ] Gym ^gym", 0, PlannedDayField, "2026-12-02")
	if planned != "- [ ] Gym @scheduled:2026-12-02 ^gym" {
		t.Errorf("planned = %q", planned)
	}
	if got := PlannedDay(planParse(t, planned)); got != "2026-12-02" {
		t.Errorf("PlannedDay = %q, want 2026-12-02", got)
	}
	if got := SetTaskField(planned, 0, PlannedDayField, ""); got != "- [ ] Gym ^gym" {
		t.Errorf("cleared = %q", got)
	}
}

func TestSetTaskFileFieldPlansATaskNoteAndTakesItOff(t *testing.T) {
	note := "---\ntags: [task]\ndue: 2026-12-04\n---\nBody\n"
	planned := SetTaskFileField(note, PlannedDayField, "2026-12-02")
	tasks := ParseTasks("inbox/Plan.md", "Plan", FolderInbox, planned, ParseTasksOptions{Dialect: DialectApp})
	if len(tasks) == 0 || PlannedDay(tasks[0]) != "2026-12-02" || tasks[0].Due != "2026-12-04" {
		t.Fatalf("planned note gave %+v, want planned 2026-12-02, due 2026-12-04", tasks)
	}
	if got := SetTaskFileField(planned, PlannedDayField, ""); got != note {
		t.Errorf("cleared note = %q, want %q", got, note)
	}
}

func TestNudgedDueDateCountsFromTheDeadline(t *testing.T) {
	for _, c := range []struct {
		line, selected string
		days           int
		want           string
	}{
		// From the due date, not from the planned day the task sits on.
		{"- [ ] Report due:2026-12-04 @scheduled:2026-12-01", "2026-12-01", 1, "2026-12-05"},
		{"- [ ] Report due:2026-12-04 @scheduled:2026-12-01", "2026-12-01", -1, "2026-12-03"},
		// A plan that has passed does not make < set the task overdue.
		{"- [ ] Report due:2026-12-04 @scheduled:2026-11-25", "2026-11-28", -1, "2026-12-03"},
		// A task with no due date counts from the selected day.
		{"- [ ] Gym @scheduled:2026-12-02", "2026-12-02", 1, "2026-12-03"},
		{"- [ ] Gym", "2026-12-02", -1, "2026-12-01"},
		// A task with only a due date moves as before.
		{"- [ ] Pay rent due:2026-12-31", "2026-12-31", 1, "2027-01-01"},
	} {
		if got := NudgedDueDate(planParse(t, c.line), c.selected, c.days); got != c.want {
			t.Errorf("%s %+d from %s = %s, want %s", c.line, c.days, c.selected, got, c.want)
		}
	}
}
