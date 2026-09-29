package tui

import (
	"reflect"
	"testing"

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
	assertKanbanCards(t, v, "today", []string{"alpha"})
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
