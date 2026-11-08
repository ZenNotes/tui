package vault

import (
	"strings"
	"testing"
)

// sessionNote is a note of a test vault.
type sessionNote struct {
	path, title, body string
}

// sessionVault reads the tasks and the events of some notes, as a scan does.
func sessionVault(notes ...sessionNote) ([]Task, []Event) {
	tasks := []Task{}
	events := []Event{}
	for _, n := range notes {
		tasks = append(tasks, ParseTasks(n.path, n.title, FolderInbox, n.body, ParseTasksOptions{Dialect: DialectApp})...)
		events = append(events, ParseEvents(n.path, n.title, FolderInbox, n.body)...)
	}
	return tasks, events
}

func sessionsOf(events []Event) []Event {
	out := []Event{}
	for _, e := range events {
		if e.TaskLink != "" {
			out = append(out, e)
		}
	}
	return out
}

func TestSessionResolvesByNoteThenUniqueId(t *testing.T) {
	work := sessionNote{"Work.md", "Work", "- [ ] Write report due:2026-11-06 ^ab12cd\n- [ ] Other ^zz99zz\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00\n"}
	tasks, events := sessionVault(work, plans)
	sessions := sessionsOf(events)
	if len(sessions) != 1 {
		t.Fatalf("sessions: %+v", events)
	}
	got, ok := ResolveSession(tasks, sessions[0])
	if !ok || !strings.HasPrefix(got.Content, "Write report") {
		t.Fatalf("resolved %+v, %v", got, ok)
	}

	// The task line moved to another note by hand: the unique id still finds it.
	archive := sessionNote{"Archive.md", "Archive", "- [ ] Write report due:2026-11-06 ^ab12cd\n"}
	moved := sessionNote{"Work.md", "Work", "- [ ] Other ^zz99zz\n"}
	tasks, events = sessionVault(moved, archive, plans)
	got, ok = ResolveSession(tasks, sessionsOf(events)[0])
	if !ok || got.SourcePath != "Archive.md" {
		t.Fatalf("moved line: %+v, %v", got, ok)
	}

	// The named note wins over a line with the same id in another note.
	both := sessionNote{"Work.md", "Work", "- [ ] Right ^ab12cd\n"}
	tasks, events = sessionVault(both, archive, plans)
	got, ok = ResolveSession(tasks, sessionsOf(events)[0])
	if !ok || got.SourcePath != "Work.md" {
		t.Fatalf("named note: %+v, %v", got, ok)
	}

	// A link without a block id names a task note.
	report := sessionNote{"Report.md", "Report", "---\npage-type: task\nstatus: open\n---\n"}
	link := sessionNote{"Plans.md", "Plans", "- Draft task:[[Report]] when:2026-11-05T09:00\n"}
	tasks, events = sessionVault(report, link)
	got, ok = ResolveSession(tasks, sessionsOf(events)[0])
	if !ok || got.Kind != "file" || got.SourcePath != "Report.md" {
		t.Fatalf("task note: %+v, %v", got, ok)
	}
}

func TestDanglingSessions(t *testing.T) {
	plans := sessionNote{"Plans.md", "Plans", "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00\n"}
	cases := map[string]sessionNote{
		"no note":      {"Other.md", "Other", "- [ ] T ^zz99zz\n"},
		"no line":      {"Work.md", "Work", "- [ ] T ^zz99zz\n"},
		"plain bullet": {"Work.md", "Work", "- plain bullet ^ab12cd\n"},
		"two lines":    {"Archive.md", "Archive", "- [ ] T ^ab12cd\n- [ ] U ^ab12cd\n"},
		"two in note":  {"Work.md", "Work", "- [ ] T ^ab12cd\n- [ ] U ^ab12cd\n"},
	}
	for name, note := range cases {
		tasks, events := sessionVault(note, plans)
		if got, ok := ResolveSession(tasks, sessionsOf(events)[0]); ok {
			t.Errorf("%s: resolved to %+v", name, got)
		}
	}
	// A link that is not a wiki link, and a note link to a missing note.
	for _, link := range []string{"Work#^ab12cd", "[[Missing]]", "[[]]"} {
		tasks, _ := sessionVault(sessionNote{"Work.md", "Work", "- [ ] T ^ab12cd\n"})
		if got, ok := ResolveSession(tasks, Event{TaskLink: link}); ok {
			t.Errorf("link %q: resolved to %+v", link, got)
		}
	}
	// An event without a link is not a session.
	if _, ok := ResolveSession(nil, Event{}); ok {
		t.Error("an event without a link resolved")
	}
}

func joinedTask(t *testing.T, today string, notes ...sessionNote) Task {
	t.Helper()
	tasks, events := sessionVault(notes...)
	joined := JoinSessions(tasks, events, groupingToday(today))
	if len(joined) != len(tasks) {
		t.Fatalf("joined %d tasks from %d", len(joined), len(tasks))
	}
	return joined[0]
}

func TestSessionDateIsFirstCurrentSessionEndingTodayOrLater(t *testing.T) {
	work := sessionNote{"Work.md", "Work", "- [ ] T ^ab12cd\n"}
	plans := func(lines ...string) sessionNote {
		return sessionNote{"Plans.md", "Plans", strings.Join(lines, "\n") + "\n"}
	}
	cases := []struct {
		name       string
		lines      []string
		date, time string
		end        string
	}{
		{"past then future", []string{
			"- T task:[[Work#^ab12cd]] when:2026-11-01T08:00",
			"- T task:[[Work#^ab12cd]] when:2026-11-06T09:30-10:30",
		}, "2026-11-06", "09:30", "10:30"},
		{"past then today", []string{
			"- T task:[[Work#^ab12cd]] when:2026-11-01",
			"- T task:[[Work#^ab12cd]] when:2026-11-04T07:00",
		}, "2026-11-04", "07:00", ""},
		{"range that ends today", []string{
			"- T task:[[Work#^ab12cd]] when:2026-11-03T22:00..2026-11-04T02:00",
		}, "2026-11-03", "22:00", "02:00"},
		{"all ended: the last one", []string{
			"- T task:[[Work#^ab12cd]] when:2026-11-01",
			"- T task:[[Work#^ab12cd]] when:2026-11-02T10:00",
		}, "2026-11-02", "10:00", ""},
		{"order by start, not by line", []string{
			"- T task:[[Work#^ab12cd]] when:2026-11-09",
			"- T task:[[Work#^ab12cd]] when:2026-11-07",
		}, "2026-11-07", "", ""},
		{"a repeating session is not current", []string{
			"- T task:[[Work#^ab12cd]] when:2026-11-05 @repeat:1w",
			"- T task:[[Work#^ab12cd]] when:2026-11-08",
		}, "2026-11-08", "", ""},
	}
	for _, c := range cases {
		got := joinedTask(t, "2026-11-04", work, plans(c.lines...))
		if got.SessionDate != c.date || got.SessionTime != c.time || got.SessionEnd != c.end {
			t.Errorf("%s: got %q %q %q, want %q %q %q", c.name,
				got.SessionDate, got.SessionTime, got.SessionEnd, c.date, c.time, c.end)
		}
	}
	// An event note is a session, but it is not a current one.
	note := sessionNote{"Sprint.md", "Sprint", "---\npage-type: event\nwhen: 2026-11-04T09:00\ntask: \"[[Work#^ab12cd]]\"\n---\n"}
	if got := joinedTask(t, "2026-11-04", work, note); got.SessionDate != "" {
		t.Errorf("event note session: %q", got.SessionDate)
	}
	// JoinSessions leaves the tasks it was given as they were.
	tasks, events := sessionVault(work, plans("- T task:[[Work#^ab12cd]] when:2026-11-06"))
	JoinSessions(tasks, events, groupingToday("2026-11-04"))
	if tasks[0].SessionDate != "" {
		t.Error("JoinSessions changed its input")
	}
}

func TestUnresolvedSessionDoesNotGroup(t *testing.T) {
	work := sessionNote{"Work.md", "Work", "- [ ] T\n"}
	plans := sessionNote{"Plans.md", "Plans", "- T task:[[Work#^zz99zz]] when:2026-11-04T09:00\n"}
	tasks, events := sessionVault(work, plans)
	day := groupingToday("2026-11-04")
	g := GroupTasks(JoinSessions(tasks, events, day), day)
	if len(g.Undated) != 1 || len(g.Today) != 0 {
		t.Fatalf("groups: %+v", g)
	}
}

func TestSessionLinkWithSpacedNoteName(t *testing.T) {
	uni := sessionNote{"Uni events.md", "Uni events", "- [ ] Study ^ab12cd\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Study when:2026-11-04T10:00-12:00 task:[[Uni events#^ab12cd]] later\n"}
	tasks, events := sessionVault(uni, plans)
	sessions := sessionsOf(events)
	if len(sessions) != 1 || sessions[0].TaskLink != "[[Uni events#^ab12cd]]" || sessions[0].Text != "Study later" {
		t.Fatalf("sessions: %+v", sessions)
	}
	got, ok := ResolveSession(tasks, sessions[0])
	if !ok || got.SourcePath != "Uni events.md" {
		t.Fatalf("resolved %+v, %v", got, ok)
	}
	for _, link := range []string{`"[[Uni events#^ab12cd]]"`, `[[Uni events#^ab12cd]]`} {
		note := sessionNote{"Sprint.md", "Sprint", "---\npage-type: event\nwhen: 2026-11-04T09:00\ntask: " + link + "\n---\n"}
		_, noteEvents := sessionVault(note)
		if len(noteEvents) != 1 || noteEvents[0].TaskLink != "[[Uni events#^ab12cd]]" {
			t.Errorf("event note %s: %+v", link, noteEvents)
			continue
		}
		if _, ok := ResolveSession(tasks, noteEvents[0]); !ok {
			t.Errorf("event note %s did not resolve", link)
		}
	}
}

func TestTaskNoteSessionsBeforeLastCompletedAreNotCurrent(t *testing.T) {
	report := sessionNote{"Report.md", "Report", "---\npage-type: task\nstatus: open\nrepeat: 1w\nlastCompleted: 2026-11-02\n---\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Report task:[[Report]] when:2026-11-02T09:00\n- Report task:[[Report]] when:2026-11-05T09:00\n"}
	tasks, events := sessionVault(report, plans)
	today := groupingToday("2026-11-04")
	joined := JoinSessions(tasks, events, today)
	if joined[0].SessionDate != "2026-11-05" {
		t.Fatalf("session date %q", joined[0].SessionDate)
	}
	g := GroupTasks(joined, today)
	if len(g.Undated) != 1 || len(g.Today) != 0 {
		t.Fatalf("groups: %+v", g)
	}
	// Without lastCompleted the earlier session counts, and it has ended.
	open := sessionNote{"Report.md", "Report", "---\npage-type: task\nstatus: open\n---\n"}
	tasks, events = sessionVault(open, plans)
	if got := JoinSessions(tasks, events, today)[0].SessionDate; got != "2026-11-05" {
		t.Fatalf("without lastCompleted: %q", got)
	}
}
