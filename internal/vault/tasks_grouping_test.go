package vault

import (
	"strings"
	"testing"
	"time"
)

func groupingToday(date string) time.Time {
	d, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		panic(err)
	}
	return d.Add(12 * time.Hour)
}

// groupingTasks scans checkbox lines as one note, so every task carries the
// path and index a real scan gives it.
func groupingTasks(t *testing.T, lines ...string) []Task {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	return ParseTasks("inbox/n.md", "n", FolderInbox, body, ParseTasksOptions{Dialect: DialectApp})
}

func taskNames(tasks []Task) string {
	names := make([]string, len(tasks))
	for i, t := range tasks {
		names[i] = strings.Fields(t.Content)[0]
	}
	return strings.Join(names, " ")
}

func TestGroupTasksJudgesOverdueByDateOnly(t *testing.T) {
	tasks := groupingTasks(t, "- [ ] A due:2026-11-03T14:00")
	g := GroupTasks(tasks, groupingToday("2026-11-04"))
	if len(g.Today) != 1 || g.OverdueCount != 1 {
		t.Fatalf("day after: today=%d overdue=%d", len(g.Today), g.OverdueCount)
	}
	later := groupingToday("2026-11-03").Add(3 * time.Hour)
	g = GroupTasks(tasks, later)
	if len(g.Today) != 1 || g.OverdueCount != 0 {
		t.Fatalf("same day after the time: today=%d overdue=%d", len(g.Today), g.OverdueCount)
	}
}

// A `scheduled:` token places nothing: the task stays undated, or follows its
// due date alone.
func TestGroupTasksIgnoresScheduled(t *testing.T) {
	today := groupingToday("2026-11-04")
	g := GroupTasks(groupingTasks(t, "- [ ] A scheduled:2026-11-04"), today)
	if len(g.Undated) != 1 || len(g.Today) != 0 || g.OverdueCount != 0 {
		t.Fatalf("scheduled today: %+v", g)
	}
	g = GroupTasks(groupingTasks(t, "- [ ] A scheduled:2026-11-02 due:2026-11-20"), today)
	if len(g.Upcoming) != 1 || len(g.Today) != 0 {
		t.Fatalf("scheduled earlier, due later: %+v", g)
	}
	g = GroupTasks(groupingTasks(t, "- [ ] A scheduled:2026-11-06"), today)
	if len(g.Undated) != 1 || len(g.Today) != 0 {
		t.Fatalf("future scheduled: %+v", g)
	}
	g = GroupTasks(groupingTasks(t, "- [ ] A @waiting scheduled:2026-11-02"), today)
	if len(g.Waiting) != 1 || len(g.Today) != 0 {
		t.Fatalf("waiting: %+v", g)
	}
}

func TestBucketTasksByDueDateIgnoresTime(t *testing.T) {
	b := BucketTasksByDueDate(groupingTasks(t, "- [ ] A due:2026-11-03T14:00"))
	if len(b["2026-11-03"]) != 1 {
		t.Fatalf("buckets: %v", b)
	}
}

func TestPriorityOrderSortsTimedBeforeUntimed(t *testing.T) {
	tasks := groupingTasks(t,
		"- [ ] C due:2026-11-10",
		"- [ ] B due:2026-11-10T16:00",
		"- [ ] A due:2026-11-10T09:00",
	)
	g := OrderTaskGroups(GroupTasks(tasks, groupingToday("2026-11-05")), "priority")
	if got := taskNames(g.Upcoming); got != "A B C" {
		t.Fatalf("priority order: %s", got)
	}
	// The grouping's own sort agrees, so the CLI lists the same order.
	if got := taskNames(GroupTasks(tasks, groupingToday("2026-11-05")).Upcoming); got != "A B C" {
		t.Fatalf("grouping order: %s", got)
	}
}

func TestPriorityOrderInTodayUsesDueTime(t *testing.T) {
	today := groupingToday("2026-11-05")
	tasks := groupingTasks(t,
		"- [ ] W !high due:2026-11-05",
		"- [ ] X !high due:2026-11-05T15:00",
		"- [ ] Z !high due:2026-11-05T09:00",
	)
	g := OrderTaskGroups(GroupTasks(tasks, today), "priority")
	if got := taskNames(g.Today); got != "Z X W" {
		t.Fatalf("today order: %s", got)
	}
	if got := taskNames(GroupTasks(tasks, today).Today); got != "Z X W" {
		t.Fatalf("grouping order: %s", got)
	}
	prio := groupingTasks(t,
		"- [ ] P !low due:2026-11-05T08:00",
		"- [ ] Q !high due:2026-11-05T18:00",
	)
	g = OrderTaskGroups(GroupTasks(prio, today), "priority")
	if got := taskNames(g.Today); got != "Q P" {
		t.Fatalf("priority beats time: %s", got)
	}
}

func TestNoteOrderIgnoresTimes(t *testing.T) {
	tasks := groupingTasks(t,
		"- [ ] B due:2026-11-05T16:00",
		"- [ ] C due:2026-11-05",
		"- [ ] A due:2026-11-05T09:00",
	)
	g := OrderTaskGroups(GroupTasks(tasks, groupingToday("2026-11-05")), "note")
	if got := taskNames(g.Today); got != "B C A" {
		t.Fatalf("note order: %s", got)
	}
}

// sessionGrouping groups the tasks of one note that has the given lines, with
// the sessions that sit in a second note.
func sessionGrouping(t *testing.T, today string, lines []string, sessions ...string) TaskGroups {
	t.Helper()
	tasks, events := sessionVault(
		sessionNote{"Work.md", "Work", strings.Join(lines, "\n") + "\n"},
		sessionNote{"Plans.md", "Plans", strings.Join(sessions, "\n") + "\n"},
	)
	day := groupingToday(today)
	return GroupTasks(JoinSessions(tasks, events, day), day)
}

func TestGroupTasksBySessionDate(t *testing.T) {
	const link = "task:[[Work#^ab12cd]] "
	g := sessionGrouping(t, "2026-11-04", []string{"- [ ] X ^ab12cd"}, "- X "+link+"when:2026-11-04")
	if len(g.Today) != 1 || g.OverdueCount != 0 {
		t.Errorf("session today: %+v", g)
	}
	g = sessionGrouping(t, "2026-11-04", []string{"- [ ] X due:2026-11-20 ^ab12cd"}, "- X "+link+"when:2026-11-02")
	if len(g.Today) != 1 || len(g.Upcoming) != 0 || g.OverdueCount != 0 {
		t.Errorf("session earlier, due later: %+v", g)
	}
	g = sessionGrouping(t, "2026-11-04", []string{"- [ ] X due:2026-11-03 ^ab12cd"}, "- X "+link+"when:2026-11-02")
	if len(g.Today) != 1 || g.OverdueCount != 1 {
		t.Errorf("only the due date makes a task overdue: %+v", g)
	}
	g = sessionGrouping(t, "2026-11-04", []string{"- [ ] X @waiting ^ab12cd"}, "- X "+link+"when:2026-11-02")
	if len(g.Waiting) != 1 || len(g.Today) != 0 {
		t.Errorf("waiting: %+v", g)
	}
	g = sessionGrouping(t, "2026-11-04", []string{"- [ ] X ^ab12cd"}, "- X "+link+"when:2026-11-06")
	if len(g.Undated) != 1 || len(g.Today) != 0 {
		t.Errorf("session later: %+v", g)
	}
	g = sessionGrouping(t, "2026-11-04", []string{"- [/] X ^ab12cd"}, "- X "+link+"when:2026-11-02")
	if len(g.Today) != 1 || !g.Today[0].InProgress {
		t.Errorf("in progress: %+v", g)
	}
	g = sessionGrouping(t, "2026-11-04", []string{"- [x] X ^ab12cd", "- [-] Y ^cd34ef"},
		"- X "+link+"when:2026-11-02", "- Y task:[[Work#^cd34ef]] when:2026-11-02")
	if len(g.Done) != 1 || len(g.Cancelled) != 1 || len(g.Today) != 0 {
		t.Errorf("done and cancelled: %+v", g)
	}
	// Without a join, a session changes nothing.
	tasks, _ := sessionVault(sessionNote{"Work.md", "Work", "- [ ] X ^ab12cd\n"})
	if g := GroupTasks(tasks, groupingToday("2026-11-04")); len(g.Undated) != 1 {
		t.Errorf("no join: %+v", g)
	}
}

func TestPriorityOrderInTodayUsesSessionTimeThenDueTime(t *testing.T) {
	g := sessionGrouping(t, "2026-11-05", []string{
		"- [ ] W !high due:2026-11-05",
		"- [ ] X !high due:2026-11-05T15:00",
		"- [ ] Y !high due:2026-11-05T17:00 ^ab12cd",
		"- [ ] Z !high due:2026-11-05T09:00",
	}, "- Y task:[[Work#^ab12cd]] when:2026-11-05T08:00")
	if got := taskNames(g.Today); got != "Y Z X W" {
		t.Fatalf("order: %s", got)
	}
	if got := taskNames(OrderTaskGroups(g, "priority").Today); got != "Y Z X W" {
		t.Fatalf("priority order: %s", got)
	}
	// The session of a task is the first current one that has not ended.
	g = sessionGrouping(t, "2026-11-05", []string{
		"- [ ] X !high due:2026-11-05T15:00",
		"- [ ] Y !high ^ab12cd",
	}, "- Y task:[[Work#^ab12cd]] when:2026-11-05T08:00", "- Y task:[[Work#^ab12cd]] when:2026-11-06T07:00")
	if got := taskNames(g.Today); got != "Y X" {
		t.Fatalf("two sessions: %s", got)
	}
}
