package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixClock replaces the CLI's task clock with noon on the given date for the
// rest of the test.
func fixClock(t *testing.T, date string) {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	prev := taskClock
	taskClock = func() time.Time { return day.Add(12 * time.Hour) }
	t.Cleanup(func() { taskClock = prev })
}

// toggleNote writes body as inbox/note.md in a fresh vault, runs
// `zn task toggle <id>` and returns the note afterwards and the command's
// output.
func toggleNote(t *testing.T, body, id string, extra ...string) (string, string) {
	t.Helper()
	return toggleNoteIn(t, isolatedCLI(t), body, id, extra...)
}

func toggleNoteIn(t *testing.T, root, body, id string, extra ...string) (string, string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "note.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"task", "toggle", "inbox/note.md#" + id, "--vault", root}, extra...)
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got), out
}

func TestTaskToggleInsertsNextOccurrence(t *testing.T) {
	fixClock(t, "2026-11-02")
	in := "intro\n- [ ] Water plants due:2026-11-02 repeat:mon,thu\noutro\n"
	want := "intro\n- [ ] Water plants due:2026-11-05 repeat:mon,thu\n" +
		"- [x] Water plants due:2026-11-02 repeat:mon,thu\noutro\n"
	got, _ := toggleNote(t, in, "0")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskToggleNextOccurrenceOfEachRule(t *testing.T) {
	for _, tc := range []struct {
		name, rule, base, next string
		sunday                 bool
	}{
		{"every day", "1d", "2026-11-02", "2026-11-03", false},
		{"every third day", "3d", "2026-11-02", "2026-11-05", false},
		{"weekdays from monday", "weekdays", "2026-11-02", "2026-11-03", false},
		{"weekdays from friday", "weekdays", "2026-11-06", "2026-11-09", false},
		{"named days", "mon,thu", "2026-11-02", "2026-11-05", false},
		{"named days wrap", "mon,thu", "2026-11-05", "2026-11-09", false},
		{"every two weeks", "2w", "2026-11-02", "2026-11-16", false},
		{"fortnight named days", "2w:mon,thu", "2026-11-02", "2026-11-05", false},
		{"fortnight skips a week", "2w:mon,thu", "2026-11-05", "2026-11-16", false},
		{"fortnight, week starting monday", "2w:mon,sun", "2026-11-04", "2026-11-08", false},
		{"fortnight, week starting sunday", "2w:mon,sun", "2026-11-04", "2026-11-15", true},
		{"day of month", "1m:15", "2026-11-15", "2026-12-15", false},
		{"day of month ahead", "1m:15", "2026-11-01", "2026-11-15", false},
		{"first of month", "1m:1", "2026-11-01", "2026-12-01", false},
		{"clamped to month end", "1m:31", "2026-11-30", "2026-12-31", false},
		{"nth weekday", "1m:2-tue", "2026-11-10", "2026-12-08", false},
		{"last weekday", "1m:last-fri", "2026-11-27", "2026-12-25", false},
		{"every three months", "3m:15", "2026-11-15", "2027-02-15", false},
		{"quarter start", "3m:1", "2026-10-01", "2027-01-01", false},
		{"quarter end", "3m:31", "2026-12-31", "2027-03-31", false},
		{"yearly", "1y:03-15", "2026-03-15", "2027-03-15", false},
		{"short month clamp", "1m:31", "2027-01-31", "2027-02-28", false},
		{"leap month clamp", "1m:31", "2028-01-31", "2028-02-29", false},
		{"leap day yearly", "1y:02-29", "2028-02-29", "2029-02-28", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixClock(t, tc.base)
			root := isolatedCLI(t)
			if tc.sunday {
				cfg := filepath.Join(os.Getenv("ZENNOTES_CONFIG_DIR"), "config.toml")
				if err := os.WriteFile(cfg, []byte("[view]\ncalendar_week_start = \"sunday\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			in := "- [ ] T due:" + tc.base + " repeat:" + tc.rule + "\n"
			got, _ := toggleNoteIn(t, root, in, "0")
			want := "- [ ] T due:" + tc.next + " repeat:" + tc.rule + "\n" +
				"- [x] T due:" + tc.base + " repeat:" + tc.rule + "\n"
			if got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestTaskToggleBaseDateAndMissedOccurrences(t *testing.T) {
	for _, tc := range []struct{ name, today, line, open string }{
		{"done counts from completion", "2026-11-04", "- [ ] T due:2026-11-02 repeat:3d/done", "- [ ] T due:2026-11-07 repeat:3d/done"},
		{"missed days are passed over", "2026-11-12", "- [ ] T due:2026-11-02 repeat:1d", "- [ ] T due:2026-11-13 repeat:1d"},
		{"missed named days", "2026-11-12", "- [ ] T due:2026-11-02 repeat:mon,thu", "- [ ] T due:2026-11-16 repeat:mon,thu"},
		{"scheduled token stays", "2026-11-02", "- [ ] T scheduled:2026-11-01 due:2026-11-04 repeat:3d", "- [ ] T scheduled:2026-11-01 due:2026-11-07 repeat:3d"},
		{"counted from completion", "2026-11-04", "- [ ] T due:2026-11-03 repeat:3d/done", "- [ ] T due:2026-11-07 repeat:3d/done"},
		{"due only", "2026-11-02", "- [ ] T due:2026-11-02 repeat:3d", "- [ ] T due:2026-11-05 repeat:3d"},
		{"times are kept", "2026-11-02", "- [ ] T due:2026-11-02T17:00 repeat:1d", "- [ ] T due:2026-11-03T17:00 repeat:1d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixClock(t, tc.today)
			got, _ := toggleNote(t, tc.line+"\n", "0")
			done := strings.Replace(tc.line, "[ ]", "[x]", 1)
			if want := tc.open + "\n" + done + "\n"; got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestTaskToggleRepeatKeyInAnyCase(t *testing.T) {
	fixClock(t, "2026-11-02")
	got, _ := toggleNote(t, "- [ ] T due:2026-11-02 REPEAT:3D\n", "0")
	want := "- [ ] T due:2026-11-05 REPEAT:3D\n- [x] T due:2026-11-02 REPEAT:3D\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	fixClock(t, "2027-03-08")
	got, _ = toggleNote(t, "- [ ] A due:2027-03-08 repeat:1w SKIP:2027-03-13..2027-03-21\n", "0")
	want = "- [ ] A due:2027-03-22 repeat:1w SKIP:2027-03-13..2027-03-21\n- [x] A due:2027-03-08 repeat:1w SKIP:2027-03-13..2027-03-21\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskToggleInvalidRuleOnlyTicks(t *testing.T) {
	fixClock(t, "2026-11-02")
	for _, rule := range []string{"fortnightly", "0d", "1000d", "1m:5-tue", "1m:32", "1y:02-30"} {
		t.Run(rule, func(t *testing.T) {
			got, _ := toggleNote(t, "- [ ] T due:2026-11-02 repeat:"+rule+"\n", "0")
			if want := "- [x] T due:2026-11-02 repeat:" + rule + "\n"; got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestTaskToggleUntickLeavesOccurrence(t *testing.T) {
	fixClock(t, "2026-11-02")
	in := "- [ ] Water plants due:2026-11-05 repeat:mon,thu\n- [x] Water plants due:2026-11-02 repeat:mon,thu\n"
	got, _ := toggleNote(t, in, "1")
	want := "- [ ] Water plants due:2026-11-05 repeat:mon,thu\n- [ ] Water plants due:2026-11-02 repeat:mon,thu\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskToggleWithoutRepeatOnlyTicks(t *testing.T) {
	fixClock(t, "2026-11-02")
	for _, line := range []string{
		"- [ ] Pay rent due:2026-11-03",
		"- [ ] A due:2027-03-15 skip:2027-03-13..2027-03-21",
	} {
		got, _ := toggleNote(t, line+"\n", "0")
		if want := strings.Replace(line, "[ ]", "[x]", 1) + "\n"; got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	}
}

func TestTaskToggleWithoutDatesAddsDue(t *testing.T) {
	fixClock(t, "2026-11-02")
	got, _ := toggleNote(t, "- [ ] T repeat:3d\n", "0")
	if want := "- [ ] T repeat:3d due:2026-11-05\n- [x] T repeat:3d\n"; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskToggleHonoursSkipPeriods(t *testing.T) {
	for _, tc := range []struct{ name, today, line, open string }{
		{"break is passed over", "2027-03-08",
			"- [ ] Assignment due:2027-03-12T23:59 repeat:1w skip:2027-03-13..2027-03-21",
			"- [ ] Assignment due:2027-03-26T23:59 repeat:1w skip:2027-03-13..2027-03-21"},
		{"back to back ranges", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:2027-03-13..2027-03-21,2027-03-22..2027-03-28",
			"- [ ] A due:2027-03-29 repeat:1w skip:2027-03-13..2027-03-21,2027-03-22..2027-03-28"},
		{"range over several cycles", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:2027-03-13..2027-04-04",
			"- [ ] A due:2027-04-05 repeat:1w skip:2027-03-13..2027-04-04"},
		{"counted from completion", "2027-03-12",
			"- [ ] A due:2027-03-10 repeat:3d/done skip:2027-03-14..2027-03-20",
			"- [ ] A due:2027-03-21 repeat:3d/done skip:2027-03-14..2027-03-20"},
		{"single date", "2027-03-12",
			"- [ ] A due:2027-03-12 repeat:1d skip:2027-03-13",
			"- [ ] A due:2027-03-14 repeat:1d skip:2027-03-13"},
		{"date and range in one token", "2027-03-12",
			"- [ ] A due:2027-03-12 repeat:1d skip:2027-03-13,2027-03-14..2027-03-16",
			"- [ ] A due:2027-03-17 repeat:1d skip:2027-03-13,2027-03-14..2027-03-16"},
		{"reversed range is ignored", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:2027-03-21..2027-03-13",
			"- [ ] A due:2027-03-15 repeat:1w skip:2027-03-21..2027-03-13"},
		{"impossible date is ignored", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:2027-02-30",
			"- [ ] A due:2027-03-15 repeat:1w skip:2027-02-30"},
		{"bad item beside a good one", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:2027-03-21..2027-03-13,2027-03-15",
			"- [ ] A due:2027-03-22 repeat:1w skip:2027-03-21..2027-03-13,2027-03-15"},
		{"past period stays on the line", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:2026-12-21..2027-01-03",
			"- [ ] A due:2027-03-15 repeat:1w skip:2026-12-21..2027-01-03"},
		{"today inside a break", "2027-03-16",
			"- [ ] A due:2027-03-08 repeat:1w skip:2027-03-13..2027-03-21",
			"- [ ] A due:2027-03-22 repeat:1w skip:2027-03-13..2027-03-21"},
		{"named period is ignored", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:@spring-break",
			"- [ ] A due:2027-03-15 repeat:1w skip:@spring-break"},
		{"named period beside a date", "2027-03-08",
			"- [ ] A due:2027-03-08 repeat:1w skip:@spring-break,2027-03-15",
			"- [ ] A due:2027-03-22 repeat:1w skip:@spring-break,2027-03-15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixClock(t, tc.today)
			got, _ := toggleNote(t, tc.line+"\n", "0")
			done := strings.Replace(tc.line, "[ ]", "[x]", 1)
			if want := tc.open + "\n" + done + "\n"; got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// A task already inside its skip period is left where it is: listing never
// rewrites the note.
func TestTaskListNeverRewritesTaskInsideSkipPeriod(t *testing.T) {
	fixClock(t, "2027-03-15")
	root := isolatedCLI(t)
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "note.md")
	note := "- [ ] A due:2027-03-15 repeat:1w skip:2027-03-13..2027-03-21\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, func() {
		if code := Main([]string{"task", "list", "--json", "--vault", root}); code != 0 {
			t.Errorf("exit %d", code)
		}
	})
	var tasks []struct {
		Due string `json:"due"`
	}
	if err := json.Unmarshal([]byte(out), &tasks); err != nil || len(tasks) != 1 || tasks[0].Due != "2027-03-15" {
		t.Fatalf("listed %s (%v)", out, err)
	}
	if got, _ := os.ReadFile(path); string(got) != note {
		t.Fatalf("note rewritten:\n%s", got)
	}
}

func TestTaskToggleAdvancesRepeatingTaskNote(t *testing.T) {
	t.Run("plain date", func(t *testing.T) {
		fixClock(t, "2027-03-08")
		in := "---\ntags: [task]\nstatus: open\ndue: 2027-03-08T09:00\nrepeat: 1w\nskip: [2027-03-13..2027-03-21, 2027-04-02]\n---\n\n# Class\n"
		got, _ := toggleNote(t, in, "task")
		for _, want := range []string{"due: 2027-03-22T09:00", "status: open", "lastCompleted: 2027-03-08", "skip: [2027-03-13..2027-03-21, 2027-04-02]"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
		if strings.Contains(got, "status: done") {
			t.Errorf("note was finished:\n%s", got)
		}
	})
	t.Run("single skip date", func(t *testing.T) {
		fixClock(t, "2027-03-12")
		in := "---\ntags: [task]\nstatus: open\ndue: 2027-03-12\nrepeat: 1d\nskip: 2027-03-13\n---\n"
		got, _ := toggleNote(t, in, "task")
		if !strings.Contains(got, "due: 2027-03-14") || !strings.Contains(got, "status: open") {
			t.Fatalf("got:\n%s", got)
		}
	})
	t.Run("scheduled key stays", func(t *testing.T) {
		fixClock(t, "2026-11-02")
		in := "---\ntags: [task]\nstatus: open\nscheduled: 2026-11-02\ndue: 2026-11-04\nrepeat: 3d\n---\n"
		got, _ := toggleNote(t, in, "task")
		for _, want := range []string{"scheduled: 2026-11-02", "due: 2026-11-07", "lastCompleted: 2026-11-02"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
	})
}

func TestTaskToggleCompletesTaskNoteWithoutRepeat(t *testing.T) {
	fixClock(t, "2026-11-04")
	in := "---\ntags: [task]\nstatus: open\n---\n\n# Pay rent\n"
	got, _ := toggleNote(t, in, "task")
	if !strings.Contains(got, "status: done") || !strings.Contains(got, "completedDate: 2026-11-04") {
		t.Fatalf("got:\n%s", got)
	}
}

// --json reports the task that was ticked, at its new id, not the copy that
// was inserted above it.
func TestTaskToggleJSONReportsTheTickedTask(t *testing.T) {
	fixClock(t, "2026-11-02")
	in := "intro\n- [ ] Water plants due:2026-11-02 repeat:mon,thu\n- [ ] Other\n"
	_, out := toggleNote(t, in, "0", "--json")
	var task struct {
		ID      string `json:"id"`
		Checked bool   `json:"checked"`
		Due     string `json:"due"`
	}
	if err := json.Unmarshal([]byte(out), &task); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !task.Checked || task.Due != "2026-11-02" || task.ID != "inbox/note.md#1" {
		t.Fatalf("reported %+v", task)
	}
}

// A repeating task whose next date lies past its until date is only ticked,
// and a task note is finished for good.
func TestTaskToggleStopsAtUntil(t *testing.T) {
	t.Run("copy before the end", func(t *testing.T) {
		fixClock(t, "2026-11-02")
		got, _ := toggleNote(t, "- [ ] T due:2026-11-02 repeat:1d until:2026-11-03\n", "0")
		want := "- [ ] T due:2026-11-03 repeat:1d until:2026-11-03\n- [x] T due:2026-11-02 repeat:1d until:2026-11-03\n"
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("last date", func(t *testing.T) {
		fixClock(t, "2026-11-03")
		got, _ := toggleNote(t, "- [ ] T due:2026-11-03 repeat:1d until:2026-11-03\n", "0")
		if want := "- [x] T due:2026-11-03 repeat:1d until:2026-11-03\n"; got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("skip reaches past the end", func(t *testing.T) {
		fixClock(t, "2026-11-02")
		in := "- [ ] T due:2026-11-02 repeat:1d skip:2026-11-03 until:2026-11-03\n"
		got, _ := toggleNote(t, in, "0")
		if want := "- [x] T due:2026-11-02 repeat:1d skip:2026-11-03 until:2026-11-03\n"; got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("json reports the ticked task", func(t *testing.T) {
		fixClock(t, "2026-11-03")
		_, out := toggleNote(t, "- [ ] T due:2026-11-03 repeat:1d until:2026-11-03\n", "0", "--json")
		var task struct {
			ID      string `json:"id"`
			Checked bool   `json:"checked"`
			Until   string `json:"until"`
		}
		if err := json.Unmarshal([]byte(out), &task); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if task.ID != "inbox/note.md#0" || !task.Checked || task.Until != "2026-11-03" {
			t.Fatalf("got %+v", task)
		}
	})
	t.Run("task note", func(t *testing.T) {
		fixClock(t, "2026-11-03")
		in := "---\ntags: [task]\nstatus: open\ndue: 2026-11-03\nrepeat: 1d\nuntil: 2026-11-03\n---\n"
		got, _ := toggleNote(t, in, "task")
		for _, want := range []string{"status: done", "completedDate: 2026-11-03", "due: 2026-11-03", "until: 2026-11-03"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
		if strings.Contains(got, "lastCompleted") {
			t.Errorf("lastCompleted written:\n%s", got)
		}
	})
	t.Run("task note before the end", func(t *testing.T) {
		fixClock(t, "2026-11-02")
		in := "---\ntags: [task]\nstatus: open\ndue: 2026-11-02\nrepeat: 1d\nuntil: 2026-11-03\n---\n"
		got, _ := toggleNote(t, in, "task")
		if !strings.Contains(got, "due: 2026-11-03") || !strings.Contains(got, "status: open") {
			t.Fatalf("got:\n%s", got)
		}
	})
}

// A task id can end in the block id of the line instead of an index.
func TestTaskToggleByBlockId(t *testing.T) {
	fixClock(t, "2026-11-02")
	in := "- [ ] One\n- [ ] Two\n- [ ] Write report due:2026-11-06 ^ab12cd\n"
	got, _ := toggleNote(t, in, "^ab12cd")
	if want := "- [ ] One\n- [ ] Two\n- [x] Write report due:2026-11-06 ^ab12cd\n"; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
