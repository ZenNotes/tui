package vault

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func readEvents(body string) []Event {
	return ParseEvents("Plans.md", "Plans", FolderInbox, body)
}

func oneEvent(t *testing.T, body string) Event {
	t.Helper()
	got := readEvents(body)
	if len(got) != 1 {
		t.Fatalf("got %d events from %q, want 1: %+v", len(got), body, got)
	}
	return got[0]
}

func TestInlineEventsAcrossDaysAndTimes(t *testing.T) {
	type span struct{ sd, st, ed, et string }
	cases := map[string]struct {
		line string
		want span
	}{
		"date range":          {"- Spring break when:2027-03-13/2027-03-21", span{"2027-03-13", "", "2027-03-21", ""}},
		"across midnight":     {"- Night shift when:2026-11-03T22:00/2026-11-04T02:00", span{"2026-11-03", "22:00", "2026-11-04", "02:00"}},
		"same day long form":  {"- Talk when:2026-11-03T14:00/2026-11-03T15:30", span{"2026-11-03", "14:00", "2026-11-03", "15:30"}},
		"same day short form": {"- Talk when:2026-11-03T14:00-15:30", span{"2026-11-03", "14:00", "2026-11-03", "15:30"}},
		"end date first":      {"- E when:2026-11-04/2026-11-03", span{"2026-11-04", "", "", ""}},
		"end time first":      {"- E when:2026-11-03T22:00/2026-11-03T21:00", span{"2026-11-03", "22:00", "", ""}},
		"time then date":      {"- E when:2026-11-03T22:00/2026-11-04", span{"2026-11-03", "22:00", "", ""}},
		"date then time":      {"- E when:2026-11-03/2026-11-04T02:00", span{"2026-11-03", "", "", ""}},
		"end not a date":      {"- E when:2026-11-03/2026-02-30", span{"2026-11-03", "", "", ""}},
		"end wraps":           {"- Late when:2026-11-03T23:00-01:00", span{"2026-11-03", "23:00", "", ""}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := oneEvent(t, c.line+"\n")
			got := span{e.StartDate, e.StartTime, e.EndDate, e.EndTime}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestEventNoteIsReadFromFrontmatter(t *testing.T) {
	body := "---\npage-type: event\nwhen: 2026-11-03T14:00-15:30\n---\n\nBring the card.\n"
	got := ParseEvents("Dentist.md", "Dentist", FolderInbox, body)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	e := got[0]
	if e.Kind != EventKindNote || e.Text != "Dentist" || e.Path != "Dentist.md" || e.Line != 0 {
		t.Errorf("identity wrong: %+v", e)
	}
	if e.StartDate != "2026-11-03" || e.StartTime != "14:00" || e.EndDate != "2026-11-03" || e.EndTime != "15:30" {
		t.Errorf("span wrong: %+v", e)
	}
	if tasks := ParseTasks("Dentist.md", "Dentist", FolderInbox, body, ParseTasksOptions{}); len(tasks) != 0 {
		t.Errorf("an event note must not be a task: %+v", tasks)
	}
}

func TestEventNoteKeepsRuleSkipAndLink(t *testing.T) {
	body := "---\npage-type: event\nwhen: 2026-11-02T09:00-10:30\nrepeat: after-1W\nuntil: 2027-01-01\nskip: [2026-11-16, easter]\ntask: \"[[Work#^ab12cd]]\"\n---\n"
	e := oneEvent(t, body)
	if e.Repeat != "1w" || e.Until != "2027-01-01" || e.TaskLink != "[[Work#^ab12cd]]" {
		t.Errorf("got %+v", e)
	}
	if !reflect.DeepEqual(e.Skip, []string{"2026-11-16"}) || !reflect.DeepEqual(e.SkipNames, []string{"easter"}) {
		t.Errorf("skip wrong: %+v", e)
	}
	if e.When != "2026-11-02T09:00-10:30" {
		t.Errorf("When = %q", e.When)
	}
}

func TestEventNoteMarkerMatchesLikeTaskMarker(t *testing.T) {
	yes := []string{"page-type: event", `page-type: "event"`, "page-type: 'event'", "page-type: [event]", "page-type:\n  - event"}
	no := []string{"page-type: Event", "page-type: [event, project]", "Page-Type: event", "page-type: events", "page-type: task"}
	for _, fm := range yes {
		if got := readEvents("---\n" + fm + "\nwhen: 2026-11-03\n---\n"); len(got) != 1 {
			t.Errorf("%q: got %d events, want 1", fm, len(got))
		}
	}
	for _, fm := range no {
		if got := readEvents("---\n" + fm + "\nwhen: 2026-11-03\n---\n"); len(got) != 0 {
			t.Errorf("%q: got %d events, want 0", fm, len(got))
		}
	}
}

func TestEventNoteWithoutValidWhenYieldsNoEvent(t *testing.T) {
	for name, fm := range map[string]string{
		"no key":    "page-type: event",
		"bad date":  "page-type: event\nwhen: 2026-02-30",
		"not a day": "page-type: event\nwhen: soon",
		"empty":     "page-type: event\nwhen:",
	} {
		if got := readEvents("---\n" + fm + "\n---\n"); len(got) != 0 {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}

func TestEventNoteBodyKeepsTasksAndInlineEvents(t *testing.T) {
	body := "---\npage-type: event\nwhen: 2026-11-03T14:00\n---\n\n- [ ] Bring card\n- Parking when:2026-11-03T13:45\n"
	events := ParseEvents("Dentist.md", "Dentist", FolderInbox, body)
	if len(events) != 2 || events[0].Kind != EventKindNote || events[1].Kind != EventKindInline || events[1].Text != "Parking" {
		t.Fatalf("events: %+v", events)
	}
	tasks := ParseTasks("Dentist.md", "Dentist", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Content != "Bring card" {
		t.Fatalf("tasks: %+v", tasks)
	}
}

func TestInlineEventTextAndTokens(t *testing.T) {
	e := oneEvent(t, "- Lecture #uni when:2026-11-03T09:00-10:30 @repeat:1w @skip:2026-12-21/2027-01-03 @until:2027-06-01 task:[[Work#^ab12cd]] ^lec1\n")
	if e.Text != "Lecture #uni" || e.Repeat != "1w" || e.Until != "2027-06-01" || e.BlockID != "lec1" {
		t.Errorf("got %+v", e)
	}
	if !reflect.DeepEqual(e.Skip, []string{"2026-12-21/2027-01-03"}) || e.TaskLink != "[[Work#^ab12cd]]" {
		t.Errorf("got %+v", e)
	}
	if e.Kind != EventKindInline || e.Path != "Plans.md" || e.NoteTitle != "Plans" {
		t.Errorf("identity: %+v", e)
	}

	plain := oneEvent(t, "- Dentist !high @waiting when:2026-11-03\n")
	if plain.Text != "Dentist !high @waiting" {
		t.Errorf("text = %q", plain.Text)
	}
	if got := oneEvent(t, "- Gym when:2026-11-02T07:00 @repeat:after-1w\n"); got.Repeat != "1w" {
		t.Errorf("repeat = %q, want the after- prefix dropped", got.Repeat)
	}
	if got := oneEvent(t, "- Gym when:2026-11-02 @until:2027-01-01\n"); got.Until != "" {
		t.Errorf("until = %q, want none without a rule", got.Until)
	}
	if got := oneEvent(t, "- Gym  when:2026-11-02   `a  b`\n"); got.Text != "Gym `a b`" {
		t.Errorf("text = %q", got.Text)
	}
}

func TestInlineEventLineNumber(t *testing.T) {
	e := oneEvent(t, "---\nx: y\n---\n\n- Dentist when:2026-11-03\n")
	if e.Line != 4 {
		t.Errorf("Line = %d, want the zero-based line 4", e.Line)
	}
}

func TestListLinesThatAreNotEvents(t *testing.T) {
	cases := map[string]string{
		"task":             "- [ ] Dentist when:2026-11-03T14:00\n",
		"odd checkbox":     "- [?] Dentist when:2026-11-03\n",
		"not a list":       "Dentist when:2026-11-03\n",
		"space after":      "- Ask them when: 2026-11-03\n",
		"word":             "- Ask them when:tomorrow\n",
		"upper case":       "- Ask them WHEN:2026-11-03\n",
		"glued":            "- dinner:when:2026-11-03\n",
		"inline code":      "- see `when:2026-11-03`\n",
		"fenced":           "```\n- Dentist when:2026-11-03\n```\n",
		"plain note front": "---\nwhen: 2026-11-03\n---\n",
		"opted out":        "---\nevents: false\n---\n- Dentist when:2026-11-03\n",
		"opted out off":    "---\nevents: off\n---\n- Dentist when:2026-11-03\n",
	}
	for name, body := range cases {
		if got := readEvents(body); len(got) != 0 {
			t.Errorf("%s: got %+v", name, got)
		}
	}
	tasks := ParseTasks("Plans.md", "Plans", FolderInbox, "- [ ] Dentist when:2026-11-03T14:00\n", ParseTasksOptions{})
	if len(tasks) != 1 || !strings.Contains(tasks[0].Content, "when:2026-11-03T14:00") {
		t.Errorf("the task must keep the token in its content: %+v", tasks)
	}
}

func TestListMarkersThatAreEvents(t *testing.T) {
	for _, line := range []string{
		"  - Dentist when:2026-11-03", "* Dentist when:2026-11-03", "+ Dentist when:2026-11-03",
		"1. Dentist when:2026-11-03", "2) Dentist when:2026-11-03", "> - Dentist when:2026-11-03",
		"- when:2026-11-03 Dentist",
	} {
		e := oneEvent(t, line+"\n")
		if e.Text != "Dentist" || e.StartDate != "2026-11-03" {
			t.Errorf("%q: got %+v", line, e)
		}
	}
}

func TestSeveralWhenTokensMakeNoEvent(t *testing.T) {
	if got := readEvents("- Dentist when:2026-11-03 when:2026-11-04\n"); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestWhenMustStartWithARealDate(t *testing.T) {
	for _, line := range []string{
		"- Dentist when:2026-02-30", "- Dentist when:2026-11-3", "- Ask them when:tomorrow", "- Dentist when:2026-11-03x",
	} {
		if got := readEvents(line + "\n"); len(got) != 0 {
			t.Errorf("%q: got %+v", line, got)
		}
	}
}

func TestInvalidTimeKeepsEventAllDay(t *testing.T) {
	e := oneEvent(t, "- Dentist when:2026-11-03T25:00\n")
	if e.StartDate != "2026-11-03" || e.StartTime != "" || e.EndDate != "" {
		t.Errorf("got %+v", e)
	}
	if e.Text != "Dentist" {
		t.Errorf("text = %q", e.Text)
	}
}

func TestTaskLinkWithoutWhenIsNoEvent(t *testing.T) {
	if got := readEvents("- Notes task:[[Work#^ab12cd]]\n"); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestEventListMarker(t *testing.T) {
	yes := []string{"page-type: event-list", `page-type: "event-list"`, "page-type: [event-list]"}
	no := []string{"page-type: events", "page-type: Event-List", "page-type: [event-list, uni]", "Page-Type: event-list", "page-type: event"}
	for _, fm := range yes {
		if !IsEventListNote("---\n" + fm + "\n---\n") {
			t.Errorf("%q should mark a category note", fm)
		}
	}
	for _, fm := range no {
		if IsEventListNote("---\n" + fm + "\n---\n") {
			t.Errorf("%q should not mark a category note", fm)
		}
	}
	if IsEventListNote("page-type: event-list\n") {
		t.Error("a marker outside frontmatter must not count")
	}
	got := readEvents("---\npage-type: event-list\nwhen: 2026-11-03\n---\n- Dentist when:2026-11-04\n")
	if len(got) != 1 || got[0].Kind != EventKindInline {
		t.Errorf("a category note makes no event of its own: %+v", got)
	}
}

func TestDueAcrossDaysIsNotAnEvent(t *testing.T) {
	body := "- [ ] Essay due:2026-11-03/2026-11-05\n"
	if got := readEvents(body); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
	tasks := ParseTasks("Plans.md", "Plans", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Due != "2026-11-03" || tasks[0].DueTime != "" || tasks[0].DueEnd != "" {
		t.Errorf("tasks: %+v", tasks)
	}
}

func TestScanEventsReadsLiveNotesOnly(t *testing.T) {
	v := newTestVault(t)
	writeFile(t, filepath.Join(v.root, "inbox", "Plans.md"), "- Dentist when:2026-11-03\n")
	writeFile(t, filepath.Join(v.root, "inbox", "Lecture.md"), "---\npage-type: event\nwhen: 2026-11-04\n---\n")
	writeFile(t, filepath.Join(v.root, "trash", "Old.md"), "- Gone when:2026-11-05\n")
	got, err := v.ScanEvents()
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	for _, e := range got {
		texts = append(texts, e.Text)
	}
	sort.Strings(texts)
	if !reflect.DeepEqual(texts, []string{"Dentist", "Lecture"}) {
		t.Errorf("got %v", texts)
	}
}

func TestEventLineKeepsABareRepeatTokenInItsText(t *testing.T) {
	e := oneEvent(t, "- Gym when:2026-11-02T07:00 repeat:1w\n")
	if e.Repeat != "" || e.Text != "Gym repeat:1w" {
		t.Errorf("bare token: repeat %q, text %q", e.Repeat, e.Text)
	}
	e = oneEvent(t, "- Gym @repeat:1w when:2026-11-02T07:00 repeat:2w\n")
	if e.Repeat != "1w" || e.Text != "Gym repeat:2w" {
		t.Errorf("field and bare token: repeat %q, text %q", e.Repeat, e.Text)
	}
}

func TestEventLineKeepsBareTokensAndAtTaskAsText(t *testing.T) {
	e := oneEvent(t, "- Lecture when:2026-11-02T09:00 repeat:1w until:2026-11-23 skip:2026-11-16")
	if e.Text != "Lecture repeat:1w until:2026-11-23 skip:2026-11-16" || e.Repeat != "" || e.Until != "" || len(e.Skip) != 0 {
		t.Errorf("bare tokens: %+v", e)
	}
	e = oneEvent(t, "- Write report @task:[[Work#^ab12cd]] when:2026-11-03T14:00")
	if e.Text != "Write report @task:[[Work#^ab12cd]]" || e.TaskLink != "" {
		t.Errorf("@task: %+v", e)
	}
	e = oneEvent(t, "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00")
	if e.Text != "Write report" || e.TaskLink != "[[Work#^ab12cd]]" {
		t.Errorf("task: %+v", e)
	}
}

func TestEventFieldsLeaveTheText(t *testing.T) {
	e := oneEvent(t, "- Lecture when:2026-11-02T09:00-10:30 @repeat:weekly @until:2026-11-23 @skip:2026-11-16")
	if e.Text != "Lecture" || e.Repeat == "" || e.Until != "2026-11-23" || !reflect.DeepEqual(e.Skip, []string{"2026-11-16"}) {
		t.Errorf("got %+v", e)
	}
}
