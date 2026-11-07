package vault

import (
	"testing"
	"time"
)

func TestParseRepeatRuleAcceptsEveryForm(t *testing.T) {
	for _, rule := range []string{
		"1d", "3d", "999d",
		"weekdays",
		"mon", "mon,thu", "sat,sun",
		"1w", "2w", "2w:mon,thu", "1w:sun",
		"1m", "3m", "1m:15", "1m:1", "1m:31", "1m:start", "1m:end",
		"1m:2-tue", "1m:1-mon", "1m:4-sun", "1m:last-fri",
		"1q", "2q", "1q:start", "1q:end",
		"1y", "2y", "1y:03-15", "1y:02-29", "1y:12-31",
		"3d/done", "weekdays/done", "mon,thu/done", "2w:mon,thu/done",
		"1m:last-fri/done", "1q:end/done", "1y:03-15/done",
		"3D", "WEEKDAYS", "Mon,THU", "1M:LAST-FRI", "3D/DONE", "1Y:03-15/Done",
	} {
		if _, ok := ParseRepeatRule(rule); !ok {
			t.Errorf("ParseRepeatRule(%q) rejected a valid rule", rule)
		}
	}
}

func TestParseRepeatRuleDescribesTheRule(t *testing.T) {
	rule, ok := ParseRepeatRule("2W:Mon,Thu/Done")
	if !ok || rule.Unit != RepeatWeeks || rule.Every != 2 || !rule.FromCompletion {
		t.Fatalf("got %+v ok=%v", rule, ok)
	}
	if len(rule.Weekdays) != 2 || rule.Weekdays[0] != 1 || rule.Weekdays[1] != 4 {
		t.Fatalf("weekdays: %v", rule.Weekdays)
	}
	monthly, ok := ParseRepeatRule("1m:last-fri")
	if !ok || monthly.Unit != RepeatMonths || monthly.Anchor != AnchorWeekdayOfMonth || monthly.Ordinal != -1 || monthly.Weekday != 5 {
		t.Fatalf("got %+v ok=%v", monthly, ok)
	}
	yearly, ok := ParseRepeatRule("1y:03-15")
	if !ok || yearly.Unit != RepeatYears || yearly.Anchor != AnchorMonthDay || yearly.Month != 3 || yearly.Day != 15 {
		t.Fatalf("got %+v ok=%v", yearly, ok)
	}
}

func TestParseRepeatRuleRejectsOutOfRangeRules(t *testing.T) {
	for _, rule := range []string{
		"0d", "1000d", "-1d", "d", "1.5d",
		"1m:5-tue", "1m:0-tue", "1m:32", "1m:0", "1m:mid", "1m:2-xyz",
		"1y:02-30", "1y:13-01", "1y:00-10", "1y:04-31", "1y:3-15", "1y:start",
		"1q:mid", "1q:15",
		"1d:5", "1w:", "1w:mon,xyz", "1w:mon,,thu",
		"fortnightly", "weekly", "", "/done", "3d/", "3d/done/done", "3d/later",
		"weekdays:mon", "mon,thu:fri", "3x", "3 d",
	} {
		if _, ok := ParseRepeatRule(rule); ok {
			t.Errorf("ParseRepeatRule(%q) accepted an invalid rule", rule)
		}
	}
}

func TestParseTasksReadsRepeatRule(t *testing.T) {
	got := scanOne(t, "- [ ] Water plants due:2026-11-02 repeat:mon,thu", DialectApp)
	if got.Repeat != "mon,thu" {
		t.Fatalf("Repeat: %q", got.Repeat)
	}
	cli := scanOne(t, "- [ ] Water plants repeat:3d/done", DialectCLI)
	if cli.Repeat != "3d/done" {
		t.Fatalf("CLI Repeat: %q", cli.Repeat)
	}
	upper := scanOne(t, "- [ ] T due:2026-11-02 REPEAT:3D", DialectApp)
	if upper.Repeat != "3d" {
		t.Fatalf("upper-case Repeat: %q", upper.Repeat)
	}
}

func TestParseTaskFileReadsRepeat(t *testing.T) {
	body := "---\ntags: [task]\ndue: 2026-11-02\nrepeat: 3d\n---\n\n# Report\n"
	tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Repeat != "3d" {
		t.Fatalf("got %+v", tasks)
	}
	bad := "---\ntags: [task]\nrepeat: fortnightly\n---\n\n# Report\n"
	tasks = ParseTasks("inbox/r.md", "Report", FolderInbox, bad, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Repeat != "" {
		t.Fatalf("invalid frontmatter rule: %+v", tasks)
	}
}

func TestParseTasksIgnoresInvalidRepeatButStripsIt(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Water plants repeat:fortnightly", dialect)
		if got.Repeat != "" {
			t.Errorf("dialect %v: Repeat %q, want empty", dialect, got.Repeat)
		}
		if got.Content != "Water plants" {
			t.Errorf("dialect %v: content %q", dialect, got.Content)
		}
	}
	for _, rule := range []string{"0d", "1000d", "1m:5-tue", "1m:32", "1y:02-30", "1q:mid"} {
		if got := scanOne(t, "- [ ] T due:2026-11-02 repeat:"+rule, DialectApp); got.Repeat != "" {
			t.Errorf("repeat:%s read as %q", rule, got.Repeat)
		}
	}
}

func TestParseTasksStripsRepeatFromContent(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Water plants repeat:mon,thu", dialect)
		if got.Content != "Water plants" {
			t.Errorf("dialect %v: content %q", dialect, got.Content)
		}
		mid := scanOne(t, "- [ ] Water REPEAT:2w:mon,thu/done the plants", dialect)
		if mid.Content != "Water the plants" || mid.Repeat != "2w:mon,thu/done" {
			t.Errorf("dialect %v: %+v", dialect, mid)
		}
	}
}

// nextFrom parses rule and returns its next occurrence for a task whose base
// date is base, completed on today, with the given week start and skip items.
func nextFrom(t *testing.T, rule, base, today string, weekStart time.Weekday, skip ...string) (string, bool) {
	t.Helper()
	parsed, ok := ParseRepeatRule(rule)
	if !ok {
		t.Fatalf("ParseRepeatRule(%q) rejected the rule", rule)
	}
	var periods []SkipPeriod
	for _, item := range skip {
		_, p, _ := ParseSkipItems(item)
		periods = append(periods, p...)
	}
	return NextOccurrence(parsed, base, today, weekStart, periods, "")
}

func TestNextOccurrenceOfEachRule(t *testing.T) {
	for _, tc := range []struct {
		name, rule, base, want string
		weekStart              time.Weekday
	}{
		{"every day", "1d", "2026-11-02", "2026-11-03", time.Monday},
		{"every three days", "3d", "2026-11-02", "2026-11-05", time.Monday},
		{"weekdays from monday", "weekdays", "2026-11-02", "2026-11-03", time.Monday},
		{"weekdays from friday", "weekdays", "2026-11-06", "2026-11-09", time.Monday},
		{"mon,thu from monday", "mon,thu", "2026-11-02", "2026-11-05", time.Monday},
		{"mon,thu from thursday", "mon,thu", "2026-11-05", "2026-11-09", time.Monday},
		{"every two weeks", "2w", "2026-11-02", "2026-11-16", time.Monday},
		{"2w days from monday", "2w:mon,thu", "2026-11-02", "2026-11-05", time.Monday},
		{"2w days from thursday", "2w:mon,thu", "2026-11-05", "2026-11-16", time.Monday},
		{"2w days monday week start", "2w:sun,wed", "2026-11-04", "2026-11-08", time.Monday},
		{"2w days sunday week start", "2w:sun,wed", "2026-11-04", "2026-11-15", time.Sunday},
		{"day 15 after the 15th", "1m:15", "2026-11-15", "2026-12-15", time.Monday},
		{"day 15 before the 15th", "1m:15", "2026-11-01", "2026-11-15", time.Monday},
		{"month start", "1m:start", "2026-11-01", "2026-12-01", time.Monday},
		{"month end", "1m:end", "2026-11-30", "2026-12-31", time.Monday},
		{"second tuesday", "1m:2-tue", "2026-11-10", "2026-12-08", time.Monday},
		{"last friday", "1m:last-fri", "2026-11-27", "2026-12-25", time.Monday},
		{"every three months", "3m", "2026-11-15", "2027-02-15", time.Monday},
		{"quarter start", "1q:start", "2026-10-01", "2027-01-01", time.Monday},
		{"quarter end", "1q:end", "2026-12-31", "2027-03-31", time.Monday},
		{"quarter on the base day", "1q", "2026-11-15", "2027-02-15", time.Monday},
		{"every two months on the 31st", "2m:31", "2026-11-30", "2027-01-31", time.Monday},
		{"every other quarter end", "2q:end", "2026-03-31", "2026-09-30", time.Monday},
		{"year on a date", "1y:03-15", "2026-03-15", "2027-03-15", time.Monday},
		{"year on the base day", "1y", "2026-03-15", "2027-03-15", time.Monday},
		{"clamp day 31 to february", "1m:31", "2027-01-31", "2027-02-28", time.Monday},
		{"clamp monthly to february", "1m", "2027-01-31", "2027-02-28", time.Monday},
		{"clamp day 31 to a leap february", "1m:31", "2028-01-31", "2028-02-29", time.Monday},
		{"clamp leap day", "1y:02-29", "2028-02-29", "2029-02-28", time.Monday},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nextFrom(t, tc.rule, tc.base, tc.base, tc.weekStart)
			if !ok || got != tc.want {
				t.Errorf("repeat:%s from %s = %q, %v; want %q", tc.rule, tc.base, got, ok, tc.want)
			}
		})
	}
}

func TestNextOccurrenceCountsMonthsFromTheBaseDate(t *testing.T) {
	// A month that clamps must not drag later months with it: the steps are
	// counted from the base date, not from the previous occurrence.
	got, ok := nextFrom(t, "1m", "2027-01-31", "2027-03-01", time.Monday)
	if !ok || got != "2027-03-31" {
		t.Errorf("got %q, %v; want 2027-03-31", got, ok)
	}
}

func TestNextOccurrenceSkipsMissedDates(t *testing.T) {
	got, ok := nextFrom(t, "1d", "2026-11-02", "2026-11-12", time.Monday)
	if !ok || got != "2026-11-13" {
		t.Errorf("daily rule: got %q, %v; want 2026-11-13", got, ok)
	}
	got, ok = nextFrom(t, "mon,thu", "2026-11-02", "2026-11-12", time.Monday)
	if !ok || got != "2026-11-16" {
		t.Errorf("weekday rule: got %q, %v; want 2026-11-16", got, ok)
	}
}

func TestNextOccurrenceFromCompletionDate(t *testing.T) {
	// The caller passes today as the base for a rule counted from completion.
	got, ok := nextFrom(t, "3d/done", "2026-11-04", "2026-11-04", time.Monday)
	if !ok || got != "2026-11-07" {
		t.Errorf("got %q, %v; want 2026-11-07", got, ok)
	}
	got, ok = nextFrom(t, "3d/done", "2027-03-12", "2027-03-12", time.Monday, "2027-03-14..2027-03-20")
	if !ok || got != "2027-03-21" {
		t.Errorf("with a break: got %q, %v; want 2027-03-21", got, ok)
	}
}

func TestNextOccurrencePassesOverSkipPeriods(t *testing.T) {
	for _, tc := range []struct {
		name, rule, base, today, want string
		skip                          []string
	}{
		{"range", "1w", "2027-03-08", "2027-03-08", "2027-03-22", []string{"2027-03-13..2027-03-21"}},
		{"back to back ranges", "1w", "2027-03-08", "2027-03-08", "2027-03-29",
			[]string{"2027-03-13..2027-03-21", "2027-03-22..2027-03-28"}},
		{"range over several cycles", "1w", "2027-03-08", "2027-03-08", "2027-04-05", []string{"2027-03-13..2027-04-04"}},
		{"single day", "1d", "2027-03-12", "2027-03-12", "2027-03-14", []string{"2027-03-13"}},
		{"day and range", "1d", "2027-03-12", "2027-03-12", "2027-03-17",
			[]string{"2027-03-13", "2027-03-14..2027-03-16"}},
		{"ignored reversed range", "1w", "2027-03-08", "2027-03-08", "2027-03-15", []string{"2027-03-21..2027-03-13"}},
		{"ignored unreal day", "1w", "2027-03-08", "2027-03-08", "2027-03-15", []string{"2027-02-30"}},
		{"reversed range beside a valid day", "1w", "2027-03-08", "2027-03-08", "2027-03-22",
			[]string{"2027-03-21..2027-03-13", "2027-03-15"}},
		{"today inside the break", "1w", "2027-03-08", "2027-03-16", "2027-03-22", []string{"2027-03-13..2027-03-21"}},
		{"anchored rule", "mon,thu", "2027-03-08", "2027-03-08", "2027-03-22", []string{"2027-03-09..2027-03-21"}},
		{"reserved name", "1w", "2027-03-08", "2027-03-08", "2027-03-15", []string{"@spring-break"}},
		{"reserved name beside a day", "1w", "2027-03-08", "2027-03-08", "2027-03-22",
			[]string{"@spring-break,2027-03-15"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nextFrom(t, tc.rule, tc.base, tc.today, time.Monday, tc.skip...)
			if !ok || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}

func TestNextOccurrenceIgnoresPastSkipPeriods(t *testing.T) {
	got, ok := nextFrom(t, "1w", "2027-03-08", "2027-03-08", time.Monday, "2026-12-21..2027-01-03")
	if !ok || got != "2027-03-15" {
		t.Errorf("got %q, %v; want 2027-03-15", got, ok)
	}
}

func TestNextOccurrenceStopsAtSearchCap(t *testing.T) {
	for _, rule := range []string{"1d", "mon", "1m:15", "1y:03-15", "1m:last-fri"} {
		got, ok := nextFrom(t, rule, "2027-03-08", "2027-03-08", time.Monday, "2027-03-09..2999-12-31")
		if ok {
			t.Errorf("repeat:%s found %q inside a skip range of centuries", rule, got)
		}
	}
}

func TestNextOccurrenceIgnoresDaylightSavingChange(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no time zone data: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })

	for _, tc := range []struct{ rule, base, want string }{
		{"1d", "2027-03-13", "2027-03-14"},
		{"1d", "2026-10-31", "2026-11-01"},
		{"1d", "2026-11-01", "2026-11-02"},
		{"1w", "2027-03-08", "2027-03-15"},
		{"7d", "2026-10-28", "2026-11-04"},
		{"mon", "2027-03-08", "2027-03-15"},
	} {
		got, ok := nextFrom(t, tc.rule, tc.base, tc.base, time.Sunday)
		if !ok || got != tc.want {
			t.Errorf("repeat:%s from %s = %q, %v; want %q", tc.rule, tc.base, got, ok, tc.want)
		}
	}
}

func TestNextOccurrenceRejectsBadDates(t *testing.T) {
	rule, _ := ParseRepeatRule("1d")
	for _, tc := range [][2]string{{"", "2026-11-02"}, {"2026-11-02", ""}, {"2026-02-30", "2026-11-02"}, {"2026-11-02T09:00", "2026-11-02"}} {
		if got, ok := NextOccurrence(rule, tc[0], tc[1], time.Monday, nil, ""); ok {
			t.Errorf("NextOccurrence(%q, %q) = %q, want no occurrence", tc[0], tc[1], got)
		}
	}
}

// nextUntil is nextFrom for a rule that ends on the until date.
func nextUntil(t *testing.T, rule, base, today, until string, skip ...string) (string, bool) {
	t.Helper()
	parsed, ok := ParseRepeatRule(rule)
	if !ok {
		t.Fatalf("ParseRepeatRule(%q) rejected the rule", rule)
	}
	var periods []SkipPeriod
	for _, item := range skip {
		_, p, _ := ParseSkipItems(item)
		periods = append(periods, p...)
	}
	return NextOccurrence(parsed, base, today, time.Monday, periods, until)
}

func TestNextOccurrenceStopsAtUntil(t *testing.T) {
	// The until day itself still counts.
	if got, ok := nextUntil(t, "1d", "2026-11-02", "2026-11-02", "2026-11-03"); !ok || got != "2026-11-03" {
		t.Errorf("until on the next day = %q, %v; want 2026-11-03", got, ok)
	}
	// The day after the until day does not.
	if got, ok := nextUntil(t, "1d", "2026-11-03", "2026-11-03", "2026-11-03"); ok {
		t.Errorf("past until = %q, want no occurrence", got)
	}
	// A skipped date comes first, and the date after it lies past until.
	if got, ok := nextUntil(t, "1d", "2026-11-02", "2026-11-02", "2026-11-04", "2026-11-03..2026-11-05"); ok {
		t.Errorf("skip up to past until = %q, want no occurrence", got)
	}
	// A skip period before until leaves the later date inside until.
	if got, ok := nextUntil(t, "1d", "2026-11-02", "2026-11-02", "2026-11-06", "2026-11-03..2026-11-04"); !ok || got != "2026-11-05" {
		t.Errorf("skip then until = %q, %v; want 2026-11-05", got, ok)
	}
	// An until date before the base date leaves no occurrence.
	if got, ok := nextUntil(t, "1d", "2026-11-10", "2026-11-02", "2026-11-05"); ok {
		t.Errorf("until before base = %q, want no occurrence", got)
	}
	// An until date before today leaves none for an earlier base.
	if got, ok := nextUntil(t, "1w", "2026-11-02", "2026-11-20", "2026-11-15"); ok {
		t.Errorf("until before today = %q, want no occurrence", got)
	}
	// A far until date does not slow an infinite rule.
	if got, ok := nextUntil(t, "1d", "2026-11-02", "2026-11-02", "2099-12-31"); !ok || got != "2026-11-03" {
		t.Errorf("far until = %q, %v; want 2026-11-03", got, ok)
	}
	// A skip that covers everything up to a far until date ends the search.
	start := time.Now()
	if got, ok := nextUntil(t, "1d", "2026-11-02", "2026-11-02", "2099-12-31", "2026-11-03..2099-12-31"); ok {
		t.Errorf("skip to until = %q, want no occurrence", got)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("search took %v", time.Since(start))
	}
}
