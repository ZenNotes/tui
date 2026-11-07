package vault

import (
	"reflect"
	"testing"
	"time"
)

// noteEvent reads an event note titled title from its frontmatter lines.
func noteEvent(title string, folder NoteFolder, front string) []Event {
	return ParseEvents(title+".md", title, folder, "---\npage-type: event\n"+front+"---\n")
}

func resolverFor(events ...[]Event) SkipResolver {
	all := []Event{}
	for _, group := range events {
		all = append(all, group...)
	}
	return NamedSkips(all, time.Monday, "2026-01-01", "2030-12-31")
}

func TestNamedSkipNormalisesTitles(t *testing.T) {
	want := []SkipPeriod{{Start: "2027-03-13", End: "2027-03-21"}}
	for _, title := range []string{"Spring Break", "spring-break", "SPRING  BREAK", " Spring - Break "} {
		names := resolverFor(noteEvent(title, FolderInbox, "when: 2027-03-13..2027-03-21\n"))
		for _, name := range []string{"spring-break", "SPRING-BREAK", "spring break", "-spring-break-"} {
			if got := names(Event{}, name); !reflect.DeepEqual(got, want) {
				t.Errorf("title %q, name %q: got %v, want %v", title, name, got, want)
			}
		}
	}
	names := resolverFor(noteEvent("Spring Break", FolderInbox, "when: 2027-03-13..2027-03-21\n"))
	for _, name := range []string{"SpringBreak", "Spring_Break", "spring", ""} {
		if got := names(Event{}, name); len(got) != 0 {
			t.Errorf("name %q: got %v, want no period", name, got)
		}
	}
}

func TestNamedSkipCoversEveryOccurrence(t *testing.T) {
	names := resolverFor(noteEvent("Reading Week", FolderInbox, "when: 2026-11-07..2026-11-15\nrepeat: 1y\n"))
	got := names(Event{}, "reading-week")
	want := []SkipPeriod{
		{Start: "2026-11-07", End: "2026-11-15"},
		{Start: "2027-11-07", End: "2027-11-15"},
		{Start: "2028-11-07", End: "2028-11-15"},
		{Start: "2029-11-07", End: "2029-11-15"},
		{Start: "2030-11-07", End: "2030-11-15"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNamedSkipIgnoresInlineEventsTrashAndAmbiguity(t *testing.T) {
	inline := ParseEvents("Plans.md", "Plans", FolderInbox, "- Spring break when:2027-03-13..2027-03-21\n")
	if len(inline) != 1 {
		t.Fatalf("inline events: %+v", inline)
	}
	if got := resolverFor(inline)(Event{}, "spring-break"); len(got) != 0 {
		t.Errorf("inline event: got %v", got)
	}
	trashed := noteEvent("Spring Break", FolderTrash, "when: 2027-03-13..2027-03-21\n")
	if got := resolverFor(trashed)(Event{}, "spring-break"); len(got) != 0 {
		t.Errorf("trashed note: got %v", got)
	}
	a := ParseEvents("a/Spring Break.md", "Spring Break", FolderInbox, "---\npage-type: event\nwhen: 2027-03-13\n---\n")
	b := ParseEvents("b/Spring Break.md", "Spring Break", FolderInbox, "---\npage-type: event\nwhen: 2027-04-13\n---\n")
	if got := resolverFor(a, b)(Event{}, "spring-break"); len(got) != 0 {
		t.Errorf("ambiguous name: got %v", got)
	}
	if got := resolverFor(a)(Event{}, "spring-break"); len(got) != 1 {
		t.Errorf("single note: got %v", got)
	}
	if got := resolverFor(a, trashed)(Event{}, "spring-break"); len(got) != 1 {
		t.Errorf("a trashed note must not make the name ambiguous: got %v", got)
	}
}

func TestNamedSkipCycleIsIgnored(t *testing.T) {
	a := noteEvent("A", FolderInbox, "when: 2027-03-01\nrepeat: 1w\nskip: [@b]\n")
	b := noteEvent("B", FolderInbox, "when: 2027-03-02\nrepeat: 1w\nskip: [@a]\n")
	names := resolverFor(a, b)
	days := []string{}
	for _, o := range EventOccurrences(a[0], "2027-03-01", "2027-03-31", time.Monday, names) {
		days = append(days, o.StartDate)
	}
	if want := []string{"2027-03-01", "2027-03-08", "2027-03-15", "2027-03-22", "2027-03-29"}; !reflect.DeepEqual(days, want) {
		t.Errorf("A: got %v, want %v", days, want)
	}
	if got := len(EventOccurrences(b[0], "2027-03-01", "2027-03-31", time.Monday, names)); got != 5 {
		t.Errorf("B: got %d occurrences, want 5", got)
	}
	self := noteEvent("S", FolderInbox, "when: 2027-03-01\nrepeat: 1w\nskip: [@s]\n")
	if got := len(EventOccurrences(self[0], "2027-03-01", "2027-03-31", time.Monday, resolverFor(self))); got != 5 {
		t.Errorf("self: got %d occurrences, want 5", got)
	}
}

func TestNamedSkipRespectsEventUntil(t *testing.T) {
	names := resolverFor(noteEvent("Reading Week", FolderInbox, "when: 2026-11-07..2026-11-15\nrepeat: 1y\nuntil: 2027-12-31\n"))
	got := names(Event{}, "reading-week")
	want := []SkipPeriod{
		{Start: "2026-11-07", End: "2026-11-15"},
		{Start: "2027-11-07", End: "2027-11-15"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNamedSkipUsesTheEventsOwnSkips(t *testing.T) {
	names := resolverFor(noteEvent("Gym", FolderInbox, "when: 2027-03-01\nrepeat: 1w\nskip: 2027-03-08\n"))
	got := names(Event{}, "gym")
	if len(got) == 0 {
		t.Fatal("no periods")
	}
	for _, p := range got {
		if p.Start <= "2027-03-08" && "2027-03-08" <= p.End {
			t.Fatalf("2027-03-08 is skipped by the event and must not be covered: %v", got)
		}
	}
}

func TestEventOccurrencesHonourNamedSkips(t *testing.T) {
	spring := noteEvent("Spring Break", FolderInbox, "when: 2027-03-13..2027-03-21\n")
	lab := ParseEvents("Plans.md", "Plans", FolderInbox, "- Lab when:2027-03-01T14:00-16:00 repeat:1w skip:@spring-break\n")
	if len(lab) != 1 {
		t.Fatalf("events: %+v", lab)
	}
	got := []string{}
	for _, o := range EventOccurrences(lab[0], "2027-03-01", "2027-03-31", time.Monday, resolverFor(spring)) {
		got = append(got, o.StartDate)
	}
	if want := []string{"2027-03-01", "2027-03-08", "2027-03-22", "2027-03-29"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSkipNamesStayOutOfSkipField(t *testing.T) {
	items, periods, names := ParseSkipItems("@Spring-Break,2027-04-02, @x ,@")
	if want := []string{"2027-04-02"}; !reflect.DeepEqual(items, want) {
		t.Errorf("items: %v", items)
	}
	if len(periods) != 1 {
		t.Errorf("periods: %v", periods)
	}
	if want := []string{"Spring-Break", "x"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names: %v, want %v", names, want)
	}
	task := scanOne(t, "- [ ] A due:2027-03-08 repeat:1w skip:@spring-break,2027-04-02", DialectApp)
	if want := []string{"2027-04-02"}; !reflect.DeepEqual(task.Skip, want) {
		t.Errorf("task skip: %v", task.Skip)
	}
}
