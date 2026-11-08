package vault

import (
	"reflect"
	"testing"
)

func TestParseSkipItemsKeepsValidDaysAndRanges(t *testing.T) {
	items, periods, _ := ParseSkipItems("2027-03-13..2027-03-21,2027-04-02")
	if want := []string{"2027-03-13..2027-03-21", "2027-04-02"}; !reflect.DeepEqual(items, want) {
		t.Fatalf("items: %v, want %v", items, want)
	}
	want := []SkipPeriod{{Start: "2027-03-13", End: "2027-03-21"}, {Start: "2027-04-02", End: "2027-04-02"}}
	if !reflect.DeepEqual(periods, want) {
		t.Fatalf("periods: %v, want %v", periods, want)
	}
	// A range of one day is valid, and so is a leap day.
	items, _, _ = ParseSkipItems("2028-02-29..2028-02-29")
	if len(items) != 1 {
		t.Fatalf("one-day range: %v", items)
	}
}

func TestParseSkipItemsIgnoresReversedAndUnrealDates(t *testing.T) {
	items, periods, _ := ParseSkipItems("2027-03-21..2027-03-13,2027-02-30,2027-03-15,nonsense,2027-03-01..2027-02-30,,2027-3-4")
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

func TestParseSkipItemsIgnoresReservedNames(t *testing.T) {
	items, periods, _ := ParseSkipItems("@spring-break,2027-03-15")
	if want := []string{"2027-03-15"}; !reflect.DeepEqual(items, want) {
		t.Fatalf("items: %v, want %v", items, want)
	}
	if len(periods) != 1 {
		t.Fatalf("periods: %v", periods)
	}
	got := scanOne(t, "- [ ] A due:2027-03-08 @repeat:1w skip:@spring-break", DialectApp)
	if got.Content != "A" || len(got.Skip) != 0 {
		t.Fatalf("got content %q skip %v", got.Content, got.Skip)
	}
	mixed := scanOne(t, "- [ ] A due:2027-03-08 @repeat:1w skip:@spring-break,2027-03-15", DialectApp)
	if !reflect.DeepEqual(mixed.Skip, []string{"2027-03-15"}) {
		t.Fatalf("mixed skip: %v", mixed.Skip)
	}
}

func TestParseTasksCollectsSkipAcrossTokensAndStripsThem(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Assignment @repeat:1w skip:2027-03-13..2027-03-21 skip:2027-02-30 skip:2027-04-02", dialect)
		wantContent := "Assignment"
		if dialect == DialectCLI {
			wantContent = "Assignment @repeat:1w"
		}
		if got.Content != wantContent {
			t.Errorf("dialect %v: content %q", dialect, got.Content)
		}
		if want := []string{"2027-03-13..2027-03-21", "2027-04-02"}; !reflect.DeepEqual(got.Skip, want) {
			t.Errorf("dialect %v: skip %v, want %v", dialect, got.Skip, want)
		}
		mid := scanOne(t, "- [ ] Hand in skip:2027-03-13 the essay", dialect)
		if mid.Content != "Hand in the essay" {
			t.Errorf("dialect %v: mid content %q", dialect, mid.Content)
		}
		bad := scanOne(t, "- [ ] Hand in skip:2027-02-30", dialect)
		if bad.Content != "Hand in" || bad.Skip != nil {
			t.Errorf("dialect %v: invalid-only token: %+v", dialect, bad)
		}
	}
}

func TestParseTasksReadsSkipKeyInAnyCase(t *testing.T) {
	got := scanOne(t, "- [ ] A due:2027-03-08 @repeat:1w SKIP:2027-03-13..2027-03-21", DialectApp)
	if want := []string{"2027-03-13..2027-03-21"}; !reflect.DeepEqual(got.Skip, want) || got.Content != "A" {
		t.Fatalf("got %+v", got)
	}
	if mixed := scanOne(t, "- [ ] A Skip:2027-03-13", DialectCLI); !reflect.DeepEqual(mixed.Skip, []string{"2027-03-13"}) {
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
	if got, want := parse("skip: [2027-03-13..2027-03-21, 2027-04-02]\n"), []string{"2027-03-13..2027-03-21", "2027-04-02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("inline list: %v, want %v", got, want)
	}
	if got, want := parse("skip:\n  - 2027-03-13..2027-03-21\n  - 2027-02-30\n  - \"@spring-break\"\n  - 2027-04-02\n"), []string{"2027-03-13..2027-03-21", "2027-04-02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("block list: %v, want %v", got, want)
	}
	if got := parse("SKIP: 2027-04-02\n"); !reflect.DeepEqual(got, []string{"2027-04-02"}) {
		t.Errorf("upper-case key: %v", got)
	}
	if got := parse(""); got != nil {
		t.Errorf("no skip key: %v", got)
	}
}
