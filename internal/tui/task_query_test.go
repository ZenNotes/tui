package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// Saved task queries are portable desktop queries. Their default semantics
// come from app-core/src/lib/tasks-filter.ts, including metadata token spelling.
func TestFilterTaskQueryMatchesDesktopMetadataSubstrings(t *testing.T) {
	tasks := []vault.Task{
		{ID: "launch", Content: "Draft the Launch email", NoteTitle: "Plans", Priority: "high", Tags: []string{"Project-Alpha"}, Fields: map[string]string{"project": "alpha"}},
		{ID: "journal", Content: "Water the plants", NoteTitle: "Journal", Tags: []string{"Infra"}, Fields: map[string]string{"project": "beta", "sprint": "24"}},
		{ID: "unrelated", Content: "Call the plumber", NoteTitle: "Home"},
	}
	tests := []struct {
		query string
		want  []string
	}{
		{"", []string{"launch", "journal", "unrelated"}},
		{"  \t ", []string{"launch", "journal", "unrelated"}},
		{"  LAUNCH  ", []string{"launch"}},
		{"JOURNAL", []string{"journal"}},
		{"!high", []string{"launch"}},
		{"high", []string{"launch"}},
		{"#project-alpha", []string{"launch"}},
		{"#PROJ", []string{"launch"}},
		{"infra", []string{"journal"}},
		{"@project:alpha", []string{"launch"}},
		{"project:beta", []string{"journal"}},
		{"@project", []string{"launch", "journal"}},
		{"alpha", []string{"launch"}},
		{"@sprint:24", []string{"journal"}},
		{"@project:zeta", nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			assertTaskQueryIDs(t, filterTaskQuery(tasks, tt.query), tt.want)
		})
	}
}

func TestFilterTaskQueryTreatsWordsAndPunctuationLiterally(t *testing.T) {
	tasks := []vault.Task{
		{ID: "phrase", Content: "Prepare alpha launch notes"},
		{ID: "split-content", Content: "Prepare alpha for launch"},
		{ID: "split-fields", Content: "Prepare alpha", NoteTitle: "Launch"},
		{ID: "literal-priority", Content: "Document priority:high syntax"},
		{ID: "high-priority", Content: "Ship it", Priority: "high"},
		{ID: "literal-minus", Content: "Explain -draft flags"},
	}
	assertTaskQueryIDs(t, filterTaskQuery(tasks, "alpha launch"), []string{"phrase"})
	assertTaskQueryIDs(t, filterTaskQuery(tasks, "priority:high"), []string{"literal-priority"})
	assertTaskQueryIDs(t, filterTaskQuery(tasks, "-draft"), []string{"literal-minus"})
}

func TestFilterTaskQueryWherePrefixKeepsAdvancedGrammar(t *testing.T) {
	tasks := []vault.Task{
		{ID: "open-high", Content: "Prepare release", Priority: "high", Fields: map[string]string{"project": "alpha"}},
		{ID: "done-high", Content: "Last release", Priority: "high", Checked: true},
		{ID: "low", Content: "Document priority:high syntax", Priority: "low"},
	}
	assertTaskQueryIDs(t, filterTaskQuery(tasks, "where: priority:high"), []string{"open-high", "done-high"})
	assertTaskQueryIDs(t, filterTaskQuery(tasks, "where: priority:high -is:done project:alpha"), []string{"open-high"})
}

func TestTaskViewsApplyPortableSavedQuery(t *testing.T) {
	a := &App{idx: &index{tasks: []vault.Task{
		{ID: "alpha", Content: "Write proposal", Fields: map[string]string{"project": "alpha"}},
		{ID: "beta", Content: "Write proposal", Fields: map[string]string{"project": "beta"}},
	}}}
	v := &tasksView{groupBy: "status", filter: "@project:alpha"}
	v.refresh(a)
	assertTaskQueryIDs(t, v.tasks, []string{"alpha"})
	assertKanbanCards(t, v, "undated", []string{"alpha"})
}

func assertTaskQueryIDs(t *testing.T, tasks []vault.Task, want []string) {
	t.Helper()
	var got []string
	for _, task := range tasks {
		got = append(got, task.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("matched tasks = %v, want %v", got, want)
	}
}

func queryDay(t *testing.T, date string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return d.Add(12 * time.Hour)
}

func TestStructuredFilterDueTodayMatchesTimedTask(t *testing.T) {
	tasks := []vault.Task{
		{ID: "timed", Content: "timed", Due: "2026-11-03", DueTime: "14:00"},
		{ID: "other", Content: "other", Due: "2026-11-04"},
	}
	got := filterTasksOn(tasks, "due:today", queryDay(t, "2026-11-03"))
	assertTaskQueryIDs(t, got, []string{"timed"})
}

func TestStructuredFilterScheduledValues(t *testing.T) {
	today := queryDay(t, "2026-11-05")
	t.Run("today", func(t *testing.T) {
		tasks := []vault.Task{
			{ID: "A", Content: "A", Scheduled: "2026-11-05", ScheduledTime: "09:30"},
			{ID: "B", Content: "B", Scheduled: "2026-11-06"},
		}
		assertTaskQueryIDs(t, filterTasksOn(tasks, "scheduled:today", today), []string{"A"})
	})
	t.Run("none and any", func(t *testing.T) {
		tasks := []vault.Task{
			{ID: "A", Content: "A", Scheduled: "2026-11-05"},
			{ID: "B", Content: "B", Due: "2026-11-05"},
		}
		assertTaskQueryIDs(t, filterTasksOn(tasks, "scheduled:none", today), []string{"B"})
		assertTaskQueryIDs(t, filterTasksOn(tasks, "scheduled:any", today), []string{"A"})
	})
	t.Run("date prefix", func(t *testing.T) {
		tasks := []vault.Task{
			{ID: "A", Content: "A", Scheduled: "2026-11-05"},
			{ID: "B", Content: "B", Scheduled: "2026-12-01"},
		}
		assertTaskQueryIDs(t, filterTasksOn(tasks, "scheduled:2026-11", today), []string{"A"})
	})
	t.Run("overdue", func(t *testing.T) {
		tasks := []vault.Task{
			{ID: "A", Content: "A", Scheduled: "2026-11-04"},
			{ID: "B", Content: "B", Scheduled: "2026-11-04", Checked: true},
			{ID: "C", Content: "C", Scheduled: "2026-11-05"},
			{ID: "D", Content: "D", Due: "2026-11-01"},
		}
		assertTaskQueryIDs(t, filterTasksOn(tasks, "scheduled:overdue", today), []string{"A"})
	})
	t.Run("through the where prefix", func(t *testing.T) {
		tasks := []vault.Task{
			{ID: "A", Content: "A", Scheduled: "2026-11-05"},
			{ID: "B", Content: "B", Scheduled: "2026-11-06"},
		}
		assertTaskQueryIDs(t, filterTaskQueryOn(tasks, "where: scheduled:today", today), []string{"A"})
		// Without the prefix the words stay a plain substring search.
		assertTaskQueryIDs(t, filterTaskQueryOn(tasks, "scheduled:today", today), nil)
	})
}
