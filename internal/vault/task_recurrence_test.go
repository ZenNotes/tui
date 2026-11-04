package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The desktop runs the same table from packages/shared-domain.
func TestRecurringCompletionCases(t *testing.T) {
	data, err := os.ReadFile("testdata/task-recurrence.cases.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/task-recurrence.cases.json.source.json")
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(source, &meta); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != meta.SHA256 {
		t.Fatalf("the cases differ from the recorded source: %s", got)
	}
	var table struct {
		Cases []struct {
			Name        string  `json:"name"`
			Body        string  `json:"body"`
			TaskIndex   int     `json:"taskIndex"`
			Today       string  `json:"today"`
			FallbackDue string  `json:"fallbackDue"`
			Skip        bool    `json:"skip"`
			Expected    *string `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, ok := CompleteRecurringTaskAtIndex(c.Body, c.TaskIndex, DialectApp, RecurringOptions{Today: c.Today, FallbackDue: c.FallbackDue, Skip: c.Skip})
			if c.Expected == nil {
				if ok {
					t.Fatalf("should not repeat: %q", got.Body)
				}
				return
			}
			if !ok || got.Body != *c.Expected {
				t.Fatalf("got %q (%v), want %q", got.Body, ok, *c.Expected)
			}
		})
	}
}

func TestNextRepeatDateAcrossADaylightSavingChange(t *testing.T) {
	rule, ok := parseRepeatRule("daily")
	if !ok {
		t.Fatal("daily")
	}
	// Clocks change in the US on 2026-11-01. Whole days never notice.
	if got := nextRepeatDate(rule, "2026-10-31", "2026-11-01"); got != "2026-11-02" {
		t.Fatalf("%s", got)
	}
}

func TestParseRepeatRuleRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, raw := range []string{"", "fortnightly", "0d", "after-mon", "mon/fortnight", "1000d"} {
		if _, ok := parseRepeatRule(raw); ok {
			t.Errorf("%q should not parse", raw)
		}
	}
	for _, raw := range []string{"daily", "ANNUALLY", "3d", "12w", "after-2m", "after-weekly", "mon/thu", "weekdays", "SAT"} {
		if _, ok := parseRepeatRule(raw); !ok {
			t.Errorf("%q should parse", raw)
		}
	}
}

func TestToggleTaskRepeatingSaysWhereTheTaskWent(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	body := "- [ ] first\n- [ ] weekly @repeat:weekly due:2026-11-01\n  - [ ] sub\n- [ ] last\n"
	next, index, ok := ToggleTaskRepeating(body, 1, DialectCLI, now)
	if !ok || index != 3 {
		t.Fatalf("%q %d %v", next, index, ok)
	}
	if !strings.Contains(next, "- [x] weekly due:2026-11-01\n") {
		t.Fatalf("%q", next)
	}
	tasks := ParseTasks("n.md", "n", FolderInbox, next, ParseTasksOptions{IncludeExcluded: true, Dialect: DialectCLI})
	if !tasks[index].Checked || tasks[index].LineNumber != 3 {
		t.Fatalf("%+v", tasks[index])
	}

	// Unchecking the record is an ordinary toggle, so it never spawns a copy.
	again, index, ok := ToggleTaskRepeating(next, 3, DialectCLI, now)
	if !ok || index != 3 || strings.Count(again, "weekly") != strings.Count(next, "weekly") {
		t.Fatalf("%q %d %v", again, index, ok)
	}
}
