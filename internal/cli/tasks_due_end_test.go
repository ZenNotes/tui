package cli

import "testing"

// The JSON reports the end of a ranged due time, and only when there is one.
func TestTaskListJSONCarriesDueEnd(t *testing.T) {
	tasks := listTaskJSON(t, "- [ ] Call Ana due:2026-11-03T14:00-15:30\n- [ ] Pay rent due:2026-11-03T14:00\n")
	if len(tasks) != 2 {
		t.Fatalf("got %+v", tasks)
	}
	for _, task := range tasks {
		switch task["content"] {
		case "Call Ana":
			if task["dueTime"] != "14:00" || task["dueEnd"] != "15:30" {
				t.Errorf("ranged: %+v", task)
			}
		case "Pay rent":
			if _, present := task["dueEnd"]; present {
				t.Errorf("dueEnd present on a task with no range: %+v", task)
			}
		}
	}
}
