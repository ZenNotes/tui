package vault

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseRepeatRuleAcceptsEveryForm(t *testing.T) {
	for _, rule := range []string{
		"1d", "3d", "999d", "007d",
		"daily", "weekly", "monthly", "quarterly", "yearly", "annually",
		"weekdays",
		"mon", "mon/thu", "sat/sun", "thu/mon",
		"1w", "2w", "2w-mon/thu", "1w-sun", "weekly-mon/thu",
		"1m", "3m", "3m-15", "monthly-15", "monthly-1", "monthly-31", "monthly-start", "monthly-end",
		"monthly-2-tue", "monthly-1-mon", "monthly-4-sun", "monthly-last-fri",
		"1q", "2q", "quarterly-start", "quarterly-end", "2q-end",
		"1y", "2y", "yearly-03-15", "yearly-02-29", "yearly-12-31", "2y-03-14",
		"after-3d", "after-daily", "after-weekly", "after-2w", "after-3m", "after-yearly",
		"after-quarterly", "after-2q",
		"3D", "WEEKDAYS", "Mon/THU", "MONTHLY-LAST-FRI", "After-3D", "Yearly-03-15",
	} {
		if _, ok := ParseRepeatRule(rule); !ok {
			t.Errorf("ParseRepeatRule(%q) rejected a valid rule", rule)
		}
	}
}

func TestParseRepeatRuleDescribesTheRule(t *testing.T) {
	rule, ok := ParseRepeatRule("2W-Mon/Thu")
	if !ok || rule.Unit != RepeatWeeks || rule.Every != 2 || rule.FromCompletion {
		t.Fatalf("got %+v ok=%v", rule, ok)
	}
	if len(rule.Weekdays) != 2 || rule.Weekdays[0] != 1 || rule.Weekdays[1] != 4 {
		t.Fatalf("weekdays: %v", rule.Weekdays)
	}
	after, ok := ParseRepeatRule("After-Weekly")
	if !ok || after.Unit != RepeatWeeks || after.Every != 1 || !after.FromCompletion || after.Anchor != AnchorNone {
		t.Fatalf("got %+v ok=%v", after, ok)
	}
	list, ok := ParseRepeatRule("mon/thu/mon")
	if !ok || list.Unit != RepeatWeeks || list.Every != 1 || list.Anchor != AnchorWeekdays || len(list.Weekdays) != 2 {
		t.Fatalf("got %+v ok=%v", list, ok)
	}
	monthly, ok := ParseRepeatRule("monthly-last-fri")
	if !ok || monthly.Unit != RepeatMonths || monthly.Every != 1 || monthly.Anchor != AnchorWeekdayOfMonth || monthly.Ordinal != -1 || monthly.Weekday != 5 {
		t.Fatalf("got %+v ok=%v", monthly, ok)
	}
	yearly, ok := ParseRepeatRule("2y-03-15")
	if !ok || yearly.Unit != RepeatYears || yearly.Every != 2 || yearly.Anchor != AnchorMonthDay || yearly.Month != 3 || yearly.Day != 15 {
		t.Fatalf("got %+v ok=%v", yearly, ok)
	}
}

func TestParseRepeatRuleRejectsOutOfRangeRules(t *testing.T) {
	for _, rule := range []string{
		"0d", "000d", "1000d", "-1d", "d", "1.5d", "0q",
		"monthly-5-tue", "monthly-0-tue", "monthly-32", "monthly-0", "monthly-mid", "monthly-2-xyz",
		"yearly-02-30", "yearly-13-01", "yearly-00-10", "yearly-04-31", "yearly-3-15", "yearly-start",
		"quarterly-mid", "quarterly-15",
		"daily-5", "3d-mon", "weekly-", "weekly-mon/xyz", "weekly-mon//thu", "weekly-tue,fri",
		"monthly-tue", "weekly-15", "yearly-mon", "monthly-03-14",
		"fortnightly", "sometimes", "every-day", "", "after-", "after-after-3d", "-3d",
		"after-weekdays", "after-mon/thu", "after-2w-mon", "after-monthly-15", "after-quarterly-end",
		"weekdays-mon", "mon,thu", "mon/", "/mon", "3x", "3 d", "3d/done", "3d:5", "1m:15", "2w:mon,thu",
	} {
		if _, ok := ParseRepeatRule(rule); ok {
			t.Errorf("ParseRepeatRule(%q) accepted an invalid rule", rule)
		}
	}
}

func TestParseTasksReadsRepeatRule(t *testing.T) {
	got := scanOne(t, "- [ ] Water plants due:2026-11-02 @repeat:mon/thu", DialectApp)
	if got.Repeat != "mon/thu" {
		t.Fatalf("Repeat: %q", got.Repeat)
	}
	cli := scanOne(t, "- [ ] Water plants @repeat:after-3d", DialectCLI)
	if cli.Repeat != "after-3d" {
		t.Fatalf("CLI Repeat: %q", cli.Repeat)
	}
	upper := scanOne(t, "- [ ] T due:2026-11-02 @REPEAT:3D", DialectApp)
	if upper.Repeat != "3d" {
		t.Fatalf("upper-case Repeat: %q", upper.Repeat)
	}
	first := scanOne(t, "- [ ] T @repeat:fortnightly @repeat:1w", DialectApp)
	if first.Repeat != "" {
		t.Fatalf("a later field set the rule: %q", first.Repeat)
	}
}

func TestParseTaskFileReadsRepeat(t *testing.T) {
	body := "---\ntags: [task]\ndue: 2026-11-02\nrepeat: 3d\n---\n\n# Report\n"
	tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Repeat != "3d" {
		t.Fatalf("got %+v", tasks)
	}
	for value, want := range map[string]string{"after-3d": "after-3d", "Mon/Thu": "mon/thu", "fortnightly": "", "mon,thu": "", "3d/done": ""} {
		note := "---\ntags: [task]\nrepeat: " + value + "\n---\n\n# Report\n"
		tasks = ParseTasks("inbox/r.md", "Report", FolderInbox, note, ParseTasksOptions{})
		if len(tasks) != 1 || tasks[0].Repeat != want {
			t.Errorf("frontmatter repeat %q: %+v, want Repeat %q", value, tasks, want)
		}
	}
}

func TestParseTasksIgnoresInvalidRepeatField(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Water plants @repeat:fortnightly", dialect)
		if got.Repeat != "" {
			t.Errorf("dialect %v: Repeat %q, want empty", dialect, got.Repeat)
		}
	}
	for _, rule := range []string{"0d", "1000d", "monthly-5-tue", "monthly-32", "yearly-02-30", "quarterly-mid", "3d/done", "mon//thu"} {
		if got := scanOne(t, "- [ ] T due:2026-11-02 @repeat:"+rule, DialectApp); got.Repeat != "" {
			t.Errorf("@repeat:%s read as %q", rule, got.Repeat)
		}
	}
}

func TestParseTasksTreatsRepeatAsAField(t *testing.T) {
	// The app grammar takes the field out of the text and the CLI grammar
	// leaves it there, as for any other field.
	app := scanOne(t, "- [ ] Water @REPEAT:mon/thu the plants", DialectApp)
	if app.Content != "Water the plants" || app.Repeat != "mon/thu" || app.Fields["repeat"] != "mon/thu" {
		t.Errorf("app: %+v", app)
	}
	cli := scanOne(t, "- [ ] Water @REPEAT:mon/thu the plants", DialectCLI)
	if cli.Content != "Water @REPEAT:mon/thu the plants" || cli.Repeat != "mon/thu" || cli.Fields != nil {
		t.Errorf("cli: %+v", cli)
	}
}

func TestBareRepeatTokenIsPlainText(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		for _, rule := range []string{"3d", "weekly", "mon/thu", "mon,thu", "2w:mon,thu", "fortnightly"} {
			line := "- [ ] Water repeat:" + rule + " plants due:2026-11-02"
			got := scanOne(t, line, dialect)
			if got.Repeat != "" || got.Content != "Water repeat:"+rule+" plants" {
				t.Errorf("dialect %v, %q: Repeat %q, content %q", dialect, line, got.Repeat, got.Content)
			}
		}
	}
	// A bare token is no rule, so a tick does not roll the task.
	body := "- [ ] Water repeat:weekly due:2026-11-02\n"
	next, inserted, ok := RollTaskInBody(body, 0, DialectApp, RollComplete, time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), time.Monday)
	if !ok || inserted != 0 || next != "- [x] Water repeat:weekly due:2026-11-02\n" {
		t.Errorf("roll of a bare token: %q, %d, %v", next, inserted, ok)
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
		{"mon/thu from monday", "mon/thu", "2026-11-02", "2026-11-05", time.Monday},
		{"mon/thu from thursday", "mon/thu", "2026-11-05", "2026-11-09", time.Monday},
		{"every two weeks", "2w", "2026-11-02", "2026-11-16", time.Monday},
		{"2w days from monday", "2w-mon/thu", "2026-11-02", "2026-11-05", time.Monday},
		{"2w days from thursday", "2w-mon/thu", "2026-11-05", "2026-11-16", time.Monday},
		{"2w days monday week start", "2w-sun/wed", "2026-11-04", "2026-11-08", time.Monday},
		{"2w days sunday week start", "2w-sun/wed", "2026-11-04", "2026-11-15", time.Sunday},
		{"day 15 after the 15th", "monthly-15", "2026-11-15", "2026-12-15", time.Monday},
		{"day 15 before the 15th", "monthly-15", "2026-11-01", "2026-11-15", time.Monday},
		{"month start", "monthly-start", "2026-11-01", "2026-12-01", time.Monday},
		{"month end", "monthly-end", "2026-11-30", "2026-12-31", time.Monday},
		{"second tuesday", "monthly-2-tue", "2026-11-10", "2026-12-08", time.Monday},
		{"last friday", "monthly-last-fri", "2026-11-27", "2026-12-25", time.Monday},
		{"every three months", "3m", "2026-11-15", "2027-02-15", time.Monday},
		{"quarter start", "quarterly-start", "2026-10-01", "2027-01-01", time.Monday},
		{"quarter end", "quarterly-end", "2026-12-31", "2027-03-31", time.Monday},
		{"quarter on the base day", "quarterly", "2026-11-15", "2027-02-15", time.Monday},
		{"every two months on the 31st", "2m-31", "2026-11-30", "2027-01-31", time.Monday},
		{"every other quarter end", "2q-end", "2026-03-31", "2026-09-30", time.Monday},
		{"year on a date", "yearly-03-15", "2026-03-15", "2027-03-15", time.Monday},
		{"year on the base day", "yearly", "2026-03-15", "2027-03-15", time.Monday},
		{"clamp day 31 to february", "monthly-31", "2027-01-31", "2027-02-28", time.Monday},
		{"clamp monthly to february", "monthly", "2027-01-31", "2027-02-28", time.Monday},
		{"clamp day 31 to a leap february", "monthly-31", "2028-01-31", "2028-02-29", time.Monday},
		{"clamp leap day", "yearly-02-29", "2028-02-29", "2029-02-28", time.Monday},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nextFrom(t, tc.rule, tc.base, tc.base, tc.weekStart)
			if !ok || got != tc.want {
				t.Errorf("@repeat:%s from %s = %q, %v; want %q", tc.rule, tc.base, got, ok, tc.want)
			}
		})
	}
}

func TestNextOccurrenceCountsMonthsFromTheBaseDate(t *testing.T) {
	// A month that clamps must not drag later months with it: the steps are
	// counted from the base date, not from the previous occurrence.
	got, ok := nextFrom(t, "monthly", "2027-01-31", "2027-03-01", time.Monday)
	if !ok || got != "2027-03-31" {
		t.Errorf("got %q, %v; want 2027-03-31", got, ok)
	}
}

func TestNextOccurrenceSkipsMissedDates(t *testing.T) {
	got, ok := nextFrom(t, "1d", "2026-11-02", "2026-11-12", time.Monday)
	if !ok || got != "2026-11-13" {
		t.Errorf("daily rule: got %q, %v; want 2026-11-13", got, ok)
	}
	got, ok = nextFrom(t, "mon/thu", "2026-11-02", "2026-11-12", time.Monday)
	if !ok || got != "2026-11-16" {
		t.Errorf("weekday rule: got %q, %v; want 2026-11-16", got, ok)
	}
}

func TestNextOccurrenceFromCompletionDate(t *testing.T) {
	// The caller passes today as the base for a rule counted from completion.
	got, ok := nextFrom(t, "after-3d", "2026-11-04", "2026-11-04", time.Monday)
	if !ok || got != "2026-11-07" {
		t.Errorf("got %q, %v; want 2026-11-07", got, ok)
	}
	got, ok = nextFrom(t, "after-3d", "2027-03-12", "2027-03-12", time.Monday, "2027-03-14..2027-03-20")
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
		{"anchored rule", "mon/thu", "2027-03-08", "2027-03-08", "2027-03-22", []string{"2027-03-09..2027-03-21"}},
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
	for _, rule := range []string{"1d", "mon", "monthly-15", "yearly-03-15", "monthly-last-fri"} {
		got, ok := nextFrom(t, rule, "2027-03-08", "2027-03-08", time.Monday, "2027-03-09..2999-12-31")
		if ok {
			t.Errorf("@repeat:%s found %q inside a skip range of centuries", rule, got)
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
			t.Errorf("@repeat:%s from %s = %q, %v; want %q", tc.rule, tc.base, got, ok, tc.want)
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

// upstreamRepeatValues lists values for `@repeat:`: every form that
// parseRepeatRule accepts, and near misses that it rejects.
func upstreamRepeatValues() []string {
	values := []string{
		"daily", "weekly", "monthly", "yearly", "annually", "weekdays",
		"quarterly", "fortnightly", "", "0d", "1000d", "007d", "00w",
		"mon", "sun", "sat", "mon/thu", "thu/mon", "tue/wed/fri", "sat/sun",
		"sun/mon/tue/wed/thu/fri/sat", "mon/mon", "mon/", "mon,thu", "mon/xyz",
		"weekdays/done", "3d/done", "3d:5", "2w:mon,thu", "1m:15",
		"after-", "after-after-3d", "after-weekdays", "after-mon/thu", "after-2w-mon",
		"MONTHLY", "After-3D", "WEEKDAYS", "Mon/THU",
	}
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7", "10", "12", "13", "24", "30", "52", "99", "100", "365", "999"} {
		for _, unit := range []string{"d", "w", "m", "y"} {
			values = append(values, n+unit, "after-"+n+unit)
		}
	}
	for _, name := range []string{"daily", "weekly", "monthly", "yearly", "annually"} {
		values = append(values, "after-"+name)
	}
	return values
}

// TestNextOccurrenceAgreesWithUpstreamForUpstreamRules runs the rules that
// parseRepeatRule accepts through both calculators, for many base dates and
// days, and for both week starts. The two must give the same date.
func TestNextOccurrenceAgreesWithUpstreamForUpstreamRules(t *testing.T) {
	bases := []string{
		"2024-02-29", "2023-01-31", "2024-01-31", "2024-08-30", "2025-02-28",
		"2026-05-31", "2026-11-08", "2026-12-31", "2027-03-31", "2028-02-29",
		"2000-01-01", "2026-01-01",
	}
	offsets := []int{-400, -40, -8, -1, 0, 1, 2, 3, 6, 7, 8, 13, 14, 15, 29, 30, 31, 32, 59, 60, 90, 91, 364, 365, 366, 400, 1000}
	accepted := 0
	for _, value := range upstreamRepeatValues() {
		up, upOK := parseRepeatRule(value)
		ours, ok := ParseRepeatRule(value)
		if upOK && !ok {
			t.Errorf("%q: upstream accepts the value and ParseRepeatRule does not", value)
			continue
		}
		if !upOK {
			continue
		}
		accepted++
		for _, base := range bases {
			baseDay, _ := repeatParseISO(base)
			for _, offset := range offsets {
				today := repeatFormatISO(baseDay.AddDate(0, 0, offset))
				want := nextRepeatDate(up, base, today)
				for _, weekStart := range []time.Weekday{time.Monday, time.Sunday} {
					got, found := NextOccurrence(ours, base, today, weekStart, nil, "")
					if !found || got != want {
						t.Fatalf("@repeat:%s, base %s, today %s, week start %v: got %q (%v), upstream %q",
							value, base, today, weekStart, got, found, want)
					}
				}
			}
		}
	}
	if accepted < 100 {
		t.Fatalf("only %d upstream values were compared", accepted)
	}
}

// TestParseRepeatRuleExtendsUpstreamOnly checks that a value which upstream
// rejects is read only when it uses an extension, so no value means one thing
// here and another upstream.
func TestParseRepeatRuleExtendsUpstreamOnly(t *testing.T) {
	for _, value := range upstreamRepeatValues() {
		if _, upOK := parseRepeatRule(value); upOK {
			continue
		}
		if _, ok := ParseRepeatRule(value); !ok {
			continue
		}
		lower := strings.ToLower(value)
		inner := strings.TrimPrefix(lower, "after-")
		extension := strings.Contains(lower, "quarterly") || strings.HasSuffix(inner, "q") ||
			// A leading zero is the one value that upstream's interval check
			// reads and its own range check refuses.
			strings.Contains(inner, "-")
		if !extension {
			t.Errorf("%q is read here and not upstream, and is no extension", value)
		}
	}
	for _, value := range []string{"quarterly", "3q", "weekly-mon/thu", "monthly-15", "yearly-03-14", "after-2q"} {
		if _, upOK := parseRepeatRule(value); upOK {
			t.Errorf("upstream reads the extension %q", value)
		}
		if _, ok := ParseRepeatRule(value); !ok {
			t.Errorf("ParseRepeatRule rejects the extension %q", value)
		}
	}
	_ = fmt.Sprint
}
