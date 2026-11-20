package vault

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// recurrenceCase is one row of the table that the desktop app and upstream's
// Go roll share.
type recurrenceCase struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	TaskIndex   int     `json:"taskIndex"`
	Today       string  `json:"today"`
	FallbackDue string  `json:"fallbackDue"`
	Skip        bool    `json:"skip"`
	Expected    *string `json:"expected"`
}

// readRecurrenceCases reads the shared table without a change.
func readRecurrenceCases(t *testing.T) []recurrenceCase {
	t.Helper()
	data, err := os.ReadFile("testdata/task-recurrence.cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []recurrenceCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("no cases")
	}
	return table.Cases
}

// The local roll gives the same text as the shared table for every case. A
// case with `skip` is a cancel, and `fallbackDue` is the date that a daily note
// gives. A case that expects null is a task that does not roll.
func TestZnRollPassesUpstreamRecurrenceCases(t *testing.T) {
	for _, c := range readRecurrenceCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			today, err := time.ParseInLocation(isoDay, c.Today, time.Local)
			if err != nil {
				t.Fatal(err)
			}
			action := RollComplete
			if c.Skip {
				action = RollCancel
			}
			plan, ok := PlanTaskRoll(TaskRollInput{
				Path: "Work.md", Body: c.Body, Index: c.TaskIndex, Dialect: DialectApp,
				Action: action, Now: today.Add(12 * time.Hour), WeekStart: time.Monday,
				FallbackDue: c.FallbackDue,
			})
			if c.Expected == nil {
				if plan.Rolled || plan.Ended {
					t.Fatalf("should not roll: %q", plan.Body)
				}
				return
			}
			if !ok || !plan.Rolled || plan.Body != *c.Expected {
				t.Fatalf("got %q (rolled %v), want %q", plan.Body, plan.Rolled, *c.Expected)
			}
		})
	}
}
