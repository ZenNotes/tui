package tui

import (
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// The list follows tasks_list_order, the desktop's Order menu (#989).
func TestTasksListFollowsTheListOrder(t *testing.T) {
	today := vault.TodayISO(time.Now())
	tasks := []vault.Task{
		{ID: "bank", Content: "bank", SourcePath: "a.md", TaskIndex: 0, Priority: "med", Due: today},
		{ID: "passport", Content: "passport", SourcePath: "a.md", TaskIndex: 1, Priority: "high", Due: today},
		{ID: "plants", Content: "plants", SourcePath: "b.md", TaskIndex: 0, Due: today},
	}
	for _, tc := range []struct {
		order string
		want  []string
	}{
		{"note", []string{"bank", "passport", "plants"}},
		{"priority", []string{"passport", "bank", "plants"}},
	} {
		a := &App{idx: &index{tasks: tasks}, prefs: prefsView{Prefs: config.Prefs{TasksListOrder: tc.order}}}
		v := &tasksView{mode: "list"}
		v.refresh(a)
		var got []string
		for _, row := range v.rows {
			if row.task != nil {
				got = append(got, row.task.ID)
			}
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s: rows = %v, want %v", tc.order, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: rows = %v, want %v", tc.order, got, tc.want)
			}
		}
	}
}
