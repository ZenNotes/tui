package tui

import (
	"reflect"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
)

// entryIDs lists the task ids of a day's entries in order.
func entryIDs(entries []calEntry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.task.ID)
	}
	return out
}

func taskIDs(tasks []vault.Task) []string {
	out := []string{}
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

func TestCalendarEntriesDueOnly(t *testing.T) {
	tasks := []vault.Task{{ID: "a", Content: "a", Due: "2026-11-18"}}
	byDay, undated := bucketCalendarEntries(tasks)
	got := byDay["2026-11-18"]
	if len(got) != 1 || got[0].task.ID != "a" || got[0].scheduled {
		t.Errorf("entries on 2026-11-18 = %+v, want one due entry for a", got)
	}
	if len(undated) != 0 {
		t.Errorf("undated = %v, want none", taskIDs(undated))
	}
	if len(byDay) != 1 {
		t.Errorf("days with entries = %d, want 1", len(byDay))
	}
}

func TestCalendarEntriesInferredDailyDue(t *testing.T) {
	raw := []vault.Task{{ID: "daily#0", SourcePath: "inbox/2026-11-18.md", Content: "from the daily note"}}
	tasks := vault.InferDailyTaskDueDates(raw, map[string]string{"inbox/2026-11-18.md": "2026-11-18"})
	byDay, undated := bucketCalendarEntries(tasks)
	got := byDay["2026-11-18"]
	if len(got) != 1 || got[0].task.ID != "daily#0" || got[0].scheduled {
		t.Errorf("entries on 2026-11-18 = %+v, want one due entry for daily#0", got)
	}
	if len(undated) != 0 {
		t.Errorf("undated = %v, want none", taskIDs(undated))
	}
}

func TestCalendarEntriesDueAndScheduledOnTwoDays(t *testing.T) {
	tasks := []vault.Task{{ID: "a", Content: "a", Due: "2026-11-18", Scheduled: "2026-11-16"}}
	byDay, undated := bucketCalendarEntries(tasks)
	due := byDay["2026-11-18"]
	if len(due) != 1 || due[0].task.ID != "a" || due[0].scheduled {
		t.Errorf("entries on 2026-11-18 = %+v, want one due entry", due)
	}
	sched := byDay["2026-11-16"]
	if len(sched) != 1 || sched[0].task.ID != "a" || !sched[0].scheduled {
		t.Errorf("entries on 2026-11-16 = %+v, want one scheduled entry", sched)
	}
	if len(undated) != 0 {
		t.Errorf("undated = %v, want none", taskIDs(undated))
	}
}

func TestCalendarEntriesSameDayShowsOnce(t *testing.T) {
	tasks := []vault.Task{{ID: "a", Content: "a", Due: "2026-11-18", Scheduled: "2026-11-18"}}
	byDay, _ := bucketCalendarEntries(tasks)
	got := byDay["2026-11-18"]
	if len(got) != 1 || got[0].task.ID != "a" {
		t.Errorf("entries on 2026-11-18 = %+v, want exactly one for a", got)
	}
}

func TestCalendarEntriesScheduledOnly(t *testing.T) {
	tasks := []vault.Task{{ID: "a", Content: "a", Scheduled: "2026-11-16"}}
	byDay, undated := bucketCalendarEntries(tasks)
	got := byDay["2026-11-16"]
	if len(got) != 1 || got[0].task.ID != "a" || !got[0].scheduled {
		t.Errorf("entries on 2026-11-16 = %+v, want one scheduled entry", got)
	}
	if len(undated) != 0 {
		t.Errorf("undated = %v, want none: a scheduled task is placed", taskIDs(undated))
	}
}

func TestCalendarEntriesHideClosedTasks(t *testing.T) {
	tasks := []vault.Task{
		{ID: "checked", Checked: true, Due: "2026-11-18"},
		{ID: "cancelled", Cancelled: true, Due: "2026-11-18"},
		{ID: "forwarded", Forwarded: true, Due: "2026-11-18"},
		{ID: "closed-sched", Checked: true, Scheduled: "2026-11-16"},
		{ID: "closed-undated", Cancelled: true},
	}
	byDay, undated := bucketCalendarEntries(tasks)
	if len(byDay) != 0 {
		t.Errorf("days with entries = %v, want none", byDay)
	}
	if len(undated) != 0 {
		t.Errorf("undated = %v, want none", taskIDs(undated))
	}
}

func TestCalendarEntriesUndatedGoToStrip(t *testing.T) {
	tasks := []vault.Task{
		{ID: "u", Content: "u"},
		{ID: "d", Content: "d", Due: "2026-11-18"},
	}
	byDay, undated := bucketCalendarEntries(tasks)
	if got := taskIDs(undated); !reflect.DeepEqual(got, []string{"u"}) {
		t.Errorf("undated = %v, want [u]", got)
	}
	for day, entries := range byDay {
		for _, e := range entries {
			if e.task.ID == "u" {
				t.Errorf("undated task listed on %s", day)
			}
		}
	}
}

func TestCalendarEntriesDueBeforeScheduled(t *testing.T) {
	// Scan order interleaves scheduled and due entries for one day.
	tasks := []vault.Task{
		{ID: "s1", Scheduled: "2026-11-18"},
		{ID: "d1", Due: "2026-11-18"},
		{ID: "s2", Scheduled: "2026-11-18"},
		{ID: "d2", Due: "2026-11-18"},
	}
	byDay, _ := bucketCalendarEntries(tasks)
	if got, want := entryIDs(byDay["2026-11-18"]), []string{"d1", "d2", "s1", "s2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order on 2026-11-18 = %v, want %v", got, want)
	}
}

func TestCalendarEntriesKeepScanOrderWithinGroup(t *testing.T) {
	// Ids and text are deliberately not in alphabetical order.
	tasks := []vault.Task{
		{ID: "z", Content: "zulu", Due: "2026-11-18"},
		{ID: "m", Content: "mike", Due: "2026-11-18"},
		{ID: "a", Content: "alpha", Due: "2026-11-18"},
		{ID: "y", Content: "yankee", Scheduled: "2026-11-18"},
		{ID: "b", Content: "bravo", Scheduled: "2026-11-18"},
	}
	byDay, _ := bucketCalendarEntries(tasks)
	if got, want := entryIDs(byDay["2026-11-18"]), []string{"z", "m", "a", "y", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order on 2026-11-18 = %v, want %v", got, want)
	}
}

func TestCalendarStripOrderPriorityThenText(t *testing.T) {
	tasks := []vault.Task{
		{ID: "beta", Content: "beta"},
		{ID: "Alpha", Content: "Alpha", Priority: "low"},
		{ID: "gamma", Content: "gamma", Priority: "high"},
		{ID: "delta", Content: "delta", Priority: "high"},
	}
	_, undated := bucketCalendarEntries(tasks)
	if got, want := taskIDs(undated), []string{"delta", "gamma", "Alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("strip order = %v, want %v", got, want)
	}
}

func TestCalendarStripOrderMedBetweenHighAndLow(t *testing.T) {
	tasks := []vault.Task{
		{ID: "low", Content: "a", Priority: "low"},
		{ID: "none", Content: "a"},
		{ID: "med", Content: "z", Priority: "med"},
		{ID: "high", Content: "z", Priority: "high"},
	}
	_, undated := bucketCalendarEntries(tasks)
	if got, want := taskIDs(undated), []string{"high", "med", "low", "none"}; !reflect.DeepEqual(got, want) {
		t.Errorf("strip order = %v, want %v", got, want)
	}
}

func TestCalendarStripOrderTiesKeepScanOrder(t *testing.T) {
	tasks := []vault.Task{
		{ID: "first", Content: "same", Priority: "med"},
		{ID: "second", Content: "SAME", Priority: "med"},
		{ID: "third", Content: "same", Priority: "med"},
	}
	_, undated := bucketCalendarEntries(tasks)
	if got, want := taskIDs(undated), []string{"first", "second", "third"}; !reflect.DeepEqual(got, want) {
		t.Errorf("strip order = %v, want %v", got, want)
	}
}

func TestCalendarEntriesEmptyInput(t *testing.T) {
	byDay, undated := bucketCalendarEntries(nil)
	if len(byDay) != 0 || len(undated) != 0 {
		t.Errorf("empty input gave days=%v undated=%v", byDay, taskIDs(undated))
	}
}
