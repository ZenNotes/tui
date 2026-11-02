package vault

import (
	"reflect"
	"testing"
)

// BucketTasksByDueDate feeds Home and the side calendar panel; its result
// must not change when other surfaces learn about other dates.
func TestBucketTasksByDueDateUnchanged(t *testing.T) {
	tasks := []Task{
		{ID: "due-a", Due: "2026-11-18"},
		{ID: "due-b", Due: "2026-11-18", Scheduled: "2026-11-16"},
		{ID: "sched-only", Scheduled: "2026-11-16"},
		{ID: "undated"},
		{ID: "checked", Checked: true, Due: "2026-11-18"},
		{ID: "cancelled", Cancelled: true},
		{ID: "forwarded-dated", Forwarded: true, Due: "2026-11-18"},
		{ID: "forwarded-undated", Forwarded: true},
		{ID: "due-c", Due: "2026-11-19"},
	}
	got := BucketTasksByDueDate(tasks)
	want := map[string][]Task{
		"2026-11-18":  {tasks[0], tasks[1], tasks[6]},
		"2026-11-19":  {tasks[8]},
		"unscheduled": {tasks[2], tasks[3]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BucketTasksByDueDate = %+v, want %+v", got, want)
	}
}
