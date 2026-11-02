package tui

import (
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// Timed tasks sort earliest first, ahead of untimed ones, only under the
// priority order; the note order keeps line order whatever the times.
func TestTasksListRendersTimedTasksInOrder(t *testing.T) {
	day := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	tasks := []vault.Task{
		{ID: "W", Content: "W", SourcePath: "a.md", TaskIndex: 0, Priority: "high", Due: "2026-11-05"},
		{ID: "X", Content: "X", SourcePath: "a.md", TaskIndex: 1, Priority: "high", Due: "2026-11-05", DueTime: "15:00"},
		{ID: "Y", Content: "Y", SourcePath: "a.md", TaskIndex: 2, Priority: "high", Due: "2026-11-05", DueTime: "17:00", Scheduled: "2026-11-05", ScheduledTime: "08:00"},
		{ID: "Z", Content: "Z", SourcePath: "a.md", TaskIndex: 3, Priority: "high", Due: "2026-11-05", DueTime: "09:00"},
	}
	for _, tc := range []struct {
		order string
		want  string
	}{
		{"priority", "YZXW"},
		{"note", "WXYZ"},
	} {
		a := &App{idx: &index{tasks: tasks}, prefs: prefsView{Prefs: config.Prefs{TasksListOrder: tc.order}}}
		a.taskNow = func() time.Time { return day }
		v := &tasksView{mode: "list"}
		v.refresh(a)
		got := ""
		for _, row := range v.rows {
			if row.task != nil {
				got += row.task.ID
			}
		}
		if got != tc.want {
			t.Fatalf("%s: rows = %s, want %s", tc.order, got, tc.want)
		}
	}
}

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
