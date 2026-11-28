package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// The Tasks surfaces place a task by the day it sits on, its planned day
// before its deadline (ZenNotes/zennotes#1119), and the board's date columns
// move the plan of a planned task, as the desktop's do. The board and the list
// read the wall clock, so the dates here are relative to today.

func planDay(days int) string {
	return vault.TodayISO(time.Now().AddDate(0, 0, days))
}

// planApp loads one note and returns the app and its first task.
func planApp(t *testing.T, line string) (*App, *tasksView, string, vault.Task) {
	t.Helper()
	a, tv, root := calVaultApp(t, map[string]string{"work.md": line + "\n"})
	if len(a.idx.tasks) != 1 {
		t.Fatalf("tasks = %+v, want one", a.idx.tasks)
	}
	return a, tv, root, a.idx.tasks[0]
}

func TestStatusBoardMovesThePlanOfAPlannedTask(t *testing.T) {
	for _, c := range []struct {
		name, line, column, want string
	}{
		{"today plans for today", "- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(3), "today",
			"- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(0)},
		{"upcoming moves a missed plan", "- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(-2), "upcoming",
			"- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(1)},
		{"upcoming moves an arrived deadline too", "- [ ] a due:" + planDay(0) + " @scheduled:" + planDay(0), "upcoming",
			"- [ ] a due:" + planDay(1) + " @scheduled:" + planDay(1)},
		{"no due date takes the plan off", "- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(3), "undated",
			"- [ ] a"},
		{"an unplanned task moves by its due date", "- [ ] a due:" + planDay(7), "today",
			"- [ ] a due:" + planDay(0)},
	} {
		a, _, root, task := planApp(t, c.line)
		a.setTaskStatusColumn(task, c.column)
		if got := strings.TrimSpace(calReadFile(t, root, "work.md")); got != c.want {
			t.Errorf("%s: note = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDueBoardMovesThePlanOfAPlannedTask(t *testing.T) {
	for _, c := range []struct {
		name, line, day, want string
	}{
		{"the plan moves, the deadline stays", "- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(3), planDay(1),
			"- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(1)},
		{"an arrived deadline moves with it", "- [ ] a due:" + planDay(0) + " @scheduled:" + planDay(3), planDay(1),
			"- [ ] a due:" + planDay(1) + " @scheduled:" + planDay(1)},
		{"no date clears both", "- [ ] a due:" + planDay(7) + " @scheduled:" + planDay(3), "",
			"- [ ] a"},
	} {
		a, _, root, task := planApp(t, c.line)
		a.moveTaskToDay(task, c.day, time.Now())
		if got := strings.TrimSpace(calReadFile(t, root, "work.md")); got != c.want {
			t.Errorf("%s: note = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBoardColumnsPlaceByThePlannedDay(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": strings.Join([]string{
		"- [ ] planned today due:" + planDay(20) + " @scheduled:" + planDay(0),
		"- [ ] planned this week due:" + planDay(20) + " @scheduled:" + planDay(3),
		"- [ ] missed plan due:" + planDay(20) + " @scheduled:" + planDay(-3),
		"- [ ] late due:" + planDay(-1) + " @scheduled:" + planDay(3),
	}, "\n") + "\n"})
	tv.mode = "kanban"
	column := func(groupBy string) map[string]string {
		tv.groupBy = groupBy
		tv.buildColumns(a)
		out := map[string]string{}
		for _, col := range tv.allColumns {
			for _, card := range col.cards {
				out[card.Content] = col.id
			}
		}
		return out
	}
	if got, want := column("status"), map[string]string{
		"planned today": "today", "planned this week": "upcoming", "missed plan": "today", "late": "today",
	}; !mapsEqual(got, want) {
		t.Errorf("status board = %v, want %v", got, want)
	}
	if got, want := column("due"), map[string]string{
		"planned today": "today", "planned this week": "week", "missed plan": "today", "late": "overdue",
	}; !mapsEqual(got, want) {
		t.Errorf("due board = %v, want %v", got, want)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestTaskRowShowsThePlanBesideTheDeadline(t *testing.T) {
	a, tv, _, task := planApp(t, "- [ ] report due:"+planDay(7)+" @scheduled:"+planDay(3))
	row := stripAnsi(tv.taskLine(a, task, 120, false))
	plan, due := "plan "+planDay(3), "due "+planDay(7)
	if !strings.Contains(row, plan+" "+due) {
		t.Errorf("row = %q, want %q then %q", row, plan, due)
	}
	if strings.Contains(row, "@scheduled") {
		t.Errorf("row = %q, shows the raw field", row)
	}

	done := task
	done.Checked = true
	if row := stripAnsi(tv.taskLine(a, done, 120, false)); strings.Contains(row, "plan ") {
		t.Errorf("done row = %q, want no plan: finished work has none worth showing", row)
	}
}

// A daily note's date gives way to a plan, so the row does not call it due.
func TestTaskRowDropsTheDailyDateAPlanReplaced(t *testing.T) {
	task := vault.Task{ID: "d#0", SourcePath: "Daily/x.md", Content: "a", Due: planDay(-2), DueInferred: true, Fields: map[string]string{"scheduled": planDay(2)}}
	a, tv, _, _ := planApp(t, "- [ ] placeholder")
	row := stripAnsi(tv.taskLine(a, task, 120, false))
	if !strings.Contains(row, "plan "+planDay(2)) || strings.Contains(row, "due ") {
		t.Errorf("row = %q, want the plan and no due date", row)
	}
}

func TestBoardCardShowsTheDeadlineThenThePlan(t *testing.T) {
	a, tv, _, task := planApp(t, "- [ ] report due:"+planDay(7)+" @scheduled:"+planDay(3))
	face := tv.cardFace(a, task, 60)
	if want := planDay(7) + " " + calScheduledMark + planDay(3); !strings.HasPrefix(face.metaPlain, want) {
		t.Errorf("card meta = %q, want it to start with %q", face.metaPlain, want)
	}
}
