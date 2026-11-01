package vault

import "testing"

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
	got := scanOne(t, "- [ ] Water plants scheduled:2026-11-02 repeat:mon,thu", DialectApp)
	if got.Repeat != "mon,thu" {
		t.Fatalf("Repeat: %q", got.Repeat)
	}
	cli := scanOne(t, "- [ ] Water plants repeat:3d/done", DialectCLI)
	if cli.Repeat != "3d/done" {
		t.Fatalf("CLI Repeat: %q", cli.Repeat)
	}
	upper := scanOne(t, "- [ ] T scheduled:2026-11-02 REPEAT:3D", DialectApp)
	if upper.Repeat != "3d" {
		t.Fatalf("upper-case Repeat: %q", upper.Repeat)
	}
}

func TestParseTaskFileReadsRepeat(t *testing.T) {
	body := "---\ntags: [task]\nscheduled: 2026-11-02\nrepeat: 3d\n---\n\n# Report\n"
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
		if got := scanOne(t, "- [ ] T scheduled:2026-11-02 repeat:"+rule, DialectApp); got.Repeat != "" {
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
