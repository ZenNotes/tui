package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func occurrenceDays(t *testing.T, body, from, to string) []string {
	t.Helper()
	e := oneEvent(t, body)
	days := []string{}
	for _, o := range EventOccurrences(e, from, to, time.Monday, nil) {
		days = append(days, o.StartDate)
	}
	return days
}

func TestRepeatingEventSkipsItsSkipDates(t *testing.T) {
	got := occurrenceDays(t, "- Lecture when:2026-11-02T09:00-10:30 @repeat:1w skip:2026-11-16", "2026-11-01", "2026-11-30")
	want := []string{"2026-11-02", "2026-11-09", "2026-11-23", "2026-11-30"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEventRuleIgnoresAfterPrefix(t *testing.T) {
	got := occurrenceDays(t, "- Gym when:2026-11-02T07:00 @repeat:after-1w", "2026-11-01", "2026-11-30")
	want := []string{"2026-11-02", "2026-11-09", "2026-11-16", "2026-11-23", "2026-11-30"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMultiDayEventRepeatsWithItsLength(t *testing.T) {
	e := oneEvent(t, "- Conference when:2027-03-13..2027-03-14 @repeat:1y")
	got := EventOccurrences(e, "2028-03-01", "2028-03-31", time.Monday, nil)
	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want 1: %+v", len(got), got)
	}
	if o := got[0]; o.StartDate != "2028-03-13" || o.EndDate != "2028-03-14" {
		t.Fatalf("got %+v, want 2028-03-13 to 2028-03-14", o)
	}
	// An occurrence that starts before the window and ends inside it counts.
	got = EventOccurrences(e, "2028-03-14", "2028-03-31", time.Monday, nil)
	if len(got) != 1 || got[0].StartDate != "2028-03-13" {
		t.Fatalf("got %+v, want the occurrence that starts on 2028-03-13", got)
	}
	// Times and the length across midnight are kept.
	n := oneEvent(t, "- Night when:2026-11-03T22:00..2026-11-04T02:00 @repeat:1w")
	got = EventOccurrences(n, "2026-11-10", "2026-11-10", time.Monday, nil)
	want := EventOccurrence{StartDate: "2026-11-10", StartTime: "22:00", EndDate: "2026-11-11", EndTime: "02:00"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEventStopsAtUntil(t *testing.T) {
	got := occurrenceDays(t, "- Lecture when:2026-11-02T09:00-10:30 @repeat:1w until:2026-11-23", "2026-11-01", "2026-11-30")
	want := []string{"2026-11-02", "2026-11-09", "2026-11-16", "2026-11-23"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got = occurrenceDays(t, "- Lecture when:2026-11-02T09:00-10:30 @repeat:1w until:2026-11-23 skip:2026-11-16", "2026-11-01", "2026-11-30")
	want = []string{"2026-11-02", "2026-11-09", "2026-11-23"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with skip: got %v, want %v", got, want)
	}
	body := "---\npage-type: event\nwhen: 2026-11-02\nrepeat: 1w\nuntil: 2026-11-15\n---\n"
	got = occurrenceDays(t, body, "2026-11-01", "2026-11-30")
	want = []string{"2026-11-02", "2026-11-09"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("note: got %v, want %v", got, want)
	}
	e := oneEvent(t, "- Trip when:2026-11-06..2026-11-08 @repeat:1w until:2026-11-13")
	var starts, ends []string
	for _, o := range EventOccurrences(e, "2026-11-01", "2026-12-31", time.Monday, nil) {
		starts, ends = append(starts, o.StartDate), append(ends, o.EndDate)
	}
	if !reflect.DeepEqual(starts, []string{"2026-11-06", "2026-11-13"}) ||
		!reflect.DeepEqual(ends, []string{"2026-11-08", "2026-11-15"}) {
		t.Fatalf("got %v to %v, want the trip whole on 11-06 and on 11-13", starts, ends)
	}
}

func TestEventUntilWithoutRepeatShowsOnlyItsDate(t *testing.T) {
	got := occurrenceDays(t, "- E when:2026-11-03 until:2026-12-01", "2026-11-01", "2026-12-31")
	if want := []string{"2026-11-03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEventUntilBeforeStartShowsOnlyFirstDate(t *testing.T) {
	got := occurrenceDays(t, "- E when:2026-11-10 @repeat:1w until:2026-11-01", "2026-11-01", "2026-12-31")
	if want := []string{"2026-11-10"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDailyEventSince2020IsCheapForOneMonth(t *testing.T) {
	rules := []string{"1d", "1w", "weekdays", "1m", "monthly-15", "monthly-last-fri", "quarterly-end", "yearly-02-29"}
	base, _ := time.Parse(isoDay, "2020-01-01")
	target, _ := time.Parse(isoDay, "2026-11-01")
	for _, raw := range rules {
		r, ok := ParseRepeatRule(raw)
		if !ok {
			t.Fatalf("rule %q does not parse", raw)
		}
		calls := 0
		eachRuleDate(r, base, time.Monday, periodNear(r, base, target), func(date string) bool {
			calls++
			return date <= "2026-11-30" && calls < 100
		})
		if calls >= 100 {
			t.Fatalf("%s: the walk made %d calls, want a number close to the window", raw, calls)
		}
	}
	e := oneEvent(t, "- E when:2020-01-01 @repeat:1d")
	if got := EventOccurrences(e, "2026-11-01", "2026-11-30", time.Monday, nil); len(got) != 30 {
		t.Fatalf("got %d daily occurrences, want 30", len(got))
	}
}

func TestJumpedWalkMatchesWalkFromStart(t *testing.T) {
	rules := []string{"3d", "2w", "1w:mon,fri", "weekdays", "1m", "2m:31", "1m:start", "1m:end", "1m:2-tue", "1m:last-sun", "1q", "1q:start", "1y", "1y:02-29"}
	for _, raw := range rules {
		e := Event{StartDate: "2024-01-31", Repeat: raw}
		var want, got []string
		for _, o := range EventOccurrences(e, "2024-01-01", "2026-12-31", time.Sunday, nil) {
			if o.StartDate >= "2026-06-01" {
				want = append(want, o.StartDate)
			}
		}
		for _, o := range EventOccurrences(e, "2026-06-01", "2026-12-31", time.Sunday, nil) {
			got = append(got, o.StartDate)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: windowed %v differs from full walk %v", raw, got, want)
		}
	}
}

func TestOccurrencesNeverWriteTheNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Plans.md")
	body := "- Standup when:2026-11-02T09:00-09:15 @repeat:weekdays\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	e := oneEvent(t, body)
	for day := 2; day <= 10; day++ {
		d := time.Date(2026, 11, day, 12, 0, 0, 0, time.UTC).Format(isoDay)
		EventOccurrences(e, d, d, time.Monday, nil)
	}
	after, _ := os.Stat(path)
	got, _ := os.ReadFile(path)
	if string(got) != body || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the note changed: %q", got)
	}
}
