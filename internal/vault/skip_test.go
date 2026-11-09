package vault

import (
	"reflect"
	"testing"
)

func TestParseSkipItemsKeepsValidDaysAndRanges(t *testing.T) {
	items, periods, _ := ParseSkipItems("2027-03-13/2027-03-21,2027-04-02")
	if want := []string{"2027-03-13/2027-03-21", "2027-04-02"}; !reflect.DeepEqual(items, want) {
		t.Fatalf("items: %v, want %v", items, want)
	}
	want := []SkipPeriod{{Start: "2027-03-13", End: "2027-03-21"}, {Start: "2027-04-02", End: "2027-04-02"}}
	if !reflect.DeepEqual(periods, want) {
		t.Fatalf("periods: %v, want %v", periods, want)
	}
	// A range of one day is valid, and so is a leap day.
	items, _, _ = ParseSkipItems("2028-02-29/2028-02-29")
	if len(items) != 1 {
		t.Fatalf("one-day range: %v", items)
	}
}

func TestParseSkipItemsIgnoresReversedAndUnrealDates(t *testing.T) {
	items, periods, _ := ParseSkipItems("2027-03-21/2027-03-13,2027-02-30,2027-03-15,!nonsense,2027-03-01/2027-02-30,,2027-3-4")
	if want := []string{"2027-03-15"}; !reflect.DeepEqual(items, want) {
		t.Fatalf("items: %v, want %v", items, want)
	}
	if len(periods) != 1 || periods[0].Start != "2027-03-15" {
		t.Fatalf("periods: %v", periods)
	}
	if items, periods, _ := ParseSkipItems(""); len(items) != 0 || len(periods) != 0 {
		t.Fatalf("empty value: %v %v", items, periods)
	}
}

func TestParseTasksReadsSkipKeyInAnyCase(t *testing.T) {
	got := scanOne(t, "- [ ] A due:2027-03-08 @repeat:1w @SKIP:2027-03-13/2027-03-21", DialectApp)
	if want := []string{"2027-03-13/2027-03-21"}; !reflect.DeepEqual(got.Skip, want) || got.Content != "A" {
		t.Fatalf("got %+v", got)
	}
	if mixed := scanOne(t, "- [ ] A @Skip:2027-03-13", DialectCLI); !reflect.DeepEqual(mixed.Skip, []string{"2027-03-13"}) {
		t.Fatalf("mixed case: %v", mixed.Skip)
	}
}

func TestParseTaskFileReadsSkipScalarAndList(t *testing.T) {
	parse := func(frontmatter string) []string {
		t.Helper()
		body := "---\ntags: [task]\nrepeat: 1w\n" + frontmatter + "---\n\n# Report\n"
		tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
		if len(tasks) != 1 {
			t.Fatalf("got %+v", tasks)
		}
		return tasks[0].Skip
	}
	if got, want := parse("skip: 2027-04-02\n"), []string{"2027-04-02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scalar: %v, want %v", got, want)
	}
	if got, want := parse("skip: [2027-03-13/2027-03-21, easter, 2027-04-02]\n"), []string{"2027-03-13/2027-03-21", "2027-04-02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("inline list: %v, want %v", got, want)
	}
	if got, want := parse("skip:\n  - 2027-03-13/2027-03-21\n  - 2027-02-30\n  - easter\n  - 2027-04-02\n"), []string{"2027-03-13/2027-03-21", "2027-04-02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("block list: %v, want %v", got, want)
	}
	if got := parse("SKIP: 2027-04-02\n"); !reflect.DeepEqual(got, []string{"2027-04-02"}) {
		t.Errorf("upper-case key: %v", got)
	}
	if got := parse(""); got != nil {
		t.Errorf("no skip key: %v", got)
	}
}

func TestSkipFieldStopsAtDots(t *testing.T) {
	const line = "- [ ] A due:2027-03-08 @repeat:1w @skip:2027-03-13..2027-03-21"
	app := scanOne(t, line, DialectApp)
	if !reflect.DeepEqual(app.Skip, []string{"2027-03-13"}) {
		t.Errorf("skip: %v, want the day before the dots", app.Skip)
	}
	if app.Content != "A ..2027-03-21" {
		t.Errorf("app content: %q, want the rest of the value to stay as text", app.Content)
	}
	cli := scanOne(t, line, DialectCLI)
	if want := "A @repeat:1w @skip:2027-03-13..2027-03-21"; cli.Content != want {
		t.Errorf("cli content: %q, want %q", cli.Content, want)
	}
	// The slash form is one range.
	slash := scanOne(t, "- [ ] A @repeat:1w @skip:2027-03-13/2027-03-21", DialectApp)
	if !reflect.DeepEqual(slash.Skip, []string{"2027-03-13/2027-03-21"}) || slash.Content != "A" {
		t.Errorf("slash range: %+v", slash)
	}
}

func TestBareSkipAndUntilArePlainText(t *testing.T) {
	const line = "- [ ] A due:2027-03-08 @repeat:1w skip:2027-03-15 until:2027-03-10"
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, line, dialect)
		if got.Skip != nil || got.Until != "" {
			t.Errorf("dialect %v: bare tokens were read: skip %v until %q", dialect, got.Skip, got.Until)
		}
		if want := "A skip:2027-03-15 until:2027-03-10"; dialect == DialectApp && got.Content != want {
			t.Errorf("content %q, want %q", got.Content, want)
		}
	}
	// A bare token neither skips a day nor ends the series.
	lines, inserted := rollLines(t, []string{line}, 0, RollComplete, "2027-03-08")
	if inserted != 1 || lines[0] != "- [ ] A due:2027-03-15 @repeat:1w skip:2027-03-15 until:2027-03-10" {
		t.Errorf("roll: %q", lines)
	}
	e := oneEvent(t, "- Lecture when:2026-11-02 @repeat:1w skip:2026-11-09 until:2026-11-10\n")
	if e.Skip != nil || e.Until != "" || e.Text != "Lecture skip:2026-11-09 until:2026-11-10" {
		t.Errorf("event: %+v", e)
	}
}

func TestSkipValueThatStartsWithALetterIsAName(t *testing.T) {
	items, periods, names := ParseSkipItems("spring-break,2027-03-15,Spring_Break2,@old,9lives")
	if want := []string{"2027-03-15"}; !reflect.DeepEqual(items, want) {
		t.Errorf("items: %v, want %v", items, want)
	}
	if len(periods) != 1 {
		t.Errorf("periods: %v", periods)
	}
	if want := []string{"spring-break", "Spring_Break2"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names: %v, want %v", names, want)
	}
	// A name is no date item. It does not show in the task's skip list, and
	// the task keeps it apart.
	got := scanOne(t, "- [ ] A due:2027-03-08 @repeat:1w @skip:spring-break", DialectApp)
	if got.Content != "A" || len(got.Skip) != 0 {
		t.Fatalf("got content %q skip %v", got.Content, got.Skip)
	}
	mixed := scanOne(t, "- [ ] A due:2027-03-08 @repeat:1w @skip:spring-break @skip:2027-03-15", DialectApp)
	if !reflect.DeepEqual(mixed.Skip, []string{"2027-03-15"}) {
		t.Fatalf("mixed skip: %v", mixed.Skip)
	}
}

func TestParseTasksCollectsSkipFields(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Assignment @repeat:1w @skip:2027-03-13/2027-03-21 @skip:2027-02-30 @skip:2027-04-02", dialect)
		// The app grammar strips every field. The CLI grammar keeps them.
		wantContent := "Assignment"
		if dialect == DialectCLI {
			wantContent = "Assignment @repeat:1w @skip:2027-03-13/2027-03-21 @skip:2027-02-30 @skip:2027-04-02"
		}
		if got.Content != wantContent {
			t.Errorf("dialect %v: content %q, want %q", dialect, got.Content, wantContent)
		}
		if want := []string{"2027-03-13/2027-03-21", "2027-04-02"}; !reflect.DeepEqual(got.Skip, want) {
			t.Errorf("dialect %v: skip %v, want %v", dialect, got.Skip, want)
		}
		bad := scanOne(t, "- [ ] Hand in @skip:2027-02-30", dialect)
		if bad.Skip != nil {
			t.Errorf("dialect %v: invalid-only field: %+v", dialect, bad)
		}
	}
	mid := scanOne(t, "- [ ] Hand in @skip:2027-03-13 the essay", DialectApp)
	if mid.Content != "Hand in the essay" {
		t.Errorf("mid content %q", mid.Content)
	}
	// The app grammar lists the fields, and the first one of a key wins.
	fields := scanOne(t, "- [ ] A @repeat:1w @until:2027-04-01 @skip:2027-03-13 @skip:2027-04-02", DialectApp).Fields
	if fields["skip"] != "2027-03-13" || fields["until"] != "2027-04-01" || fields["repeat"] != "1w" {
		t.Errorf("fields: %v", fields)
	}
}
