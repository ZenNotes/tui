package vault

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// rollDay is noon on a date, on the local clock.
func rollDay(t *testing.T, date string) time.Time {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return day.Add(12 * time.Hour)
}

// planRollOf plans the roll of one task in a set of notes. The random source
// has a fixed seed, so wantID gives the id that the plan makes.
func planRollOf(t *testing.T, today, path string, index int, file bool, action RollAction, notes ...sessionNote) (TaskRollPlan, bool) {
	t.Helper()
	tasks, events := sessionVault(notes...)
	bodies := map[string]string{}
	for _, n := range notes {
		bodies[n.path] = n.body
	}
	in := TaskRollInput{
		Path: path, Body: bodies[path], File: file, Index: index,
		Dialect: DialectApp, Action: action, Now: rollDay(t, today), WeekStart: time.Monday,
		Tasks: tasks, Events: events,
		Read: func(p string) (string, bool) { b, ok := bodies[p]; return b, ok },
		IDs:  map[string]bool{"ab12cd": true}, Rand: rand.New(rand.NewSource(7)),
	}
	return PlanTaskRoll(in)
}

// wantID is the id that planRollOf makes.
func wantID() string {
	return NewBlockID(map[string]bool{"ab12cd": true}, rand.New(rand.NewSource(7)))
}

func copyLines(plan TaskRollPlan) []string {
	out := []string{}
	for _, c := range plan.Copies {
		out = append(out, c.Path+":"+c.Line)
	}
	return out
}

func TestPlanRollCopiesSessionsBelowOriginals(t *testing.T) {
	id := wantID()
	work := sessionNote{"Work.md", "Work", "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"}
	plan, ok := planRollOf(t, "2026-11-02", "Work.md", 0, false, RollComplete, work, plans)
	if !ok || !plan.Rolled {
		t.Fatalf("plan: %+v, %v", plan, ok)
	}
	wantBody := "intro\n- [ ] Water plants @repeat:mon/thu ^" + id + "\n- [x] Water plants ^ab12cd\noutro\n"
	if plan.Body != wantBody || plan.Inserted != 1 {
		t.Fatalf("body %q inserted %d, want %q", plan.Body, plan.Inserted, wantBody)
	}
	if len(plan.Copies) != 1 || plan.Copies[0].Path != "Plans.md" || plan.Copies[0].After != 0 ||
		plan.Copies[0].Line != "- Water plants task:[[Work#^"+id+"]] when:2026-11-05T09:00-09:15" {
		t.Fatalf("copies: %+v", plan.Copies)
	}

	// Two sessions on separate lines each get a copy directly below.
	repeat := sessionNote{"Work.md", "Work", "- [ ] T @repeat:1w ^ab12cd\n"}
	two := sessionNote{"Plans.md", "Plans", "- T task:[[Work#^ab12cd]] when:2026-11-02T09:00-10:00\n- T task:[[Work#^ab12cd]] when:2026-11-03T14:00-16:00\n"}
	plan, _ = planRollOf(t, "2026-11-02", "Work.md", 0, false, RollComplete, repeat, two)
	got := InsertSessionCopies(two.body, plan.Copies)
	want := "- T task:[[Work#^ab12cd]] when:2026-11-02T09:00-10:00\n" +
		"- T task:[[Work#^" + id + "]] when:2026-11-09T09:00-10:00\n" +
		"- T task:[[Work#^ab12cd]] when:2026-11-03T14:00-16:00\n" +
		"- T task:[[Work#^" + id + "]] when:2026-11-10T14:00-16:00\n"
	if got != want {
		t.Fatalf("plans:\n%s\nwant:\n%s", got, want)
	}
}

func TestPlanRollCountsFromEarliestSession(t *testing.T) {
	id := wantID()
	for _, tc := range []struct {
		name, today, task, session, wantTask, wantSession string
	}{
		{"due and session", "2026-11-02", "- [ ] T due:2026-11-04T17:00-17:30 @repeat:3d ^ab12cd", "when:2026-11-02T09:30-10:00",
			"- [ ] T due:2026-11-05T17:00-17:30 @repeat:3d ^" + id, "when:2026-11-05T09:30-10:00"},
		{"from completion", "2026-11-04", "- [ ] T due:2026-11-03 @repeat:after-3d ^ab12cd", "when:2026-11-02",
			"- [ ] T due:2026-11-07 @repeat:after-3d ^" + id, "when:2026-11-07"},
		{"skip period", "2027-03-08", "- [ ] Assignment due:2027-03-12T23:59 @repeat:1w @skip:2027-03-13/2027-03-21 ^ab12cd", "when:2027-03-08T09:00-11:00",
			"- [ ] Assignment due:2027-03-22T23:59 @repeat:1w @skip:2027-03-13/2027-03-21 ^" + id, "when:2027-03-22T09:00-11:00"},
		{"across midnight", "2026-11-02", "- [ ] T @repeat:1w ^ab12cd", "when:2026-11-02T22:00/2026-11-03T02:00",
			"- [ ] T @repeat:1w ^" + id, "when:2026-11-09T22:00/2026-11-10T02:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := sessionNote{"Work.md", "Work", tc.task + "\n"}
			plans := sessionNote{"Plans.md", "Plans", "- T task:[[Work#^ab12cd]] " + tc.session + "\n"}
			plan, ok := planRollOf(t, tc.today, "Work.md", 0, false, RollComplete, work, plans)
			if !ok || !plan.Rolled {
				t.Fatalf("plan: %+v, %v", plan, ok)
			}
			if first := strings.Split(plan.Body, "\n")[0]; first != tc.wantTask {
				t.Fatalf("inserted line %q, want %q", first, tc.wantTask)
			}
			want := "Plans.md:- T task:[[Work#^" + id + "]] " + tc.wantSession
			if got := copyLines(plan); len(got) != 1 || got[0] != want {
				t.Fatalf("copies %q, want %q", got, want)
			}
		})
	}

	// A cancelled task rolls the same way, and the original is cancelled.
	work := sessionNote{"Work.md", "Work", "- [ ] A due:2027-03-12 @repeat:1w ^ab12cd\n"}
	plans := sessionNote{"Plans.md", "Plans", "- A task:[[Work#^ab12cd]] when:2027-03-08T09:00\n"}
	plan, _ := planRollOf(t, "2027-03-08", "Work.md", 0, false, RollCancel, work, plans)
	if !strings.Contains(plan.Body, "- [-] A due:2027-03-12 ^ab12cd") || len(plan.Copies) != 1 {
		t.Fatalf("cancel: %+v", plan)
	}
}

func TestPlanRollTaskNoteKeepsItsLink(t *testing.T) {
	report := sessionNote{"Report.md", "Report", "---\npage-type: task\nstatus: open\ndue: 2026-11-04\nrepeat: 1w\n---\nBody\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Report task:[[Report]] when:2026-11-02T09:00\n"}
	plan, ok := planRollOf(t, "2026-11-02", "Report.md", 0, true, RollComplete, report, plans)
	if !ok || !plan.Rolled {
		t.Fatalf("plan: %+v, %v", plan, ok)
	}
	for _, want := range []string{"status: open", "due: 2026-11-09", "lastCompleted: 2026-11-02"} {
		if !strings.Contains(plan.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, plan.Body)
		}
	}
	if got := copyLines(plan); len(got) != 1 || got[0] != "Plans.md:- Report task:[[Report]] when:2026-11-09T09:00" {
		t.Fatalf("copies: %q", got)
	}
}

func TestPlanRollSkipsEventNoteRepeatingAndDanglingSessions(t *testing.T) {
	id := wantID()
	work := sessionNote{"Work.md", "Work", "- [ ] Water plants @repeat:mon/thu ^ab12cd\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Water plants task:[[Work#^ab12cd]] when:2026-11-03T09:00-09:15\n" +
		"- Water plants task:[[Work#^ab12cd]] when:2026-11-02T08:00 @repeat:1w\n" +
		"- Water plants task:[[Work#^zz99zz]] when:2026-11-01T08:00\n"}
	plan, _ := planRollOf(t, "2026-11-02", "Work.md", 0, false, RollComplete, work, plans)
	// The base is the only copied session, 2026-11-03, so the next date is
	// 2026-11-05 and the offset is two days.
	want := "Plans.md:- Water plants task:[[Work#^" + id + "]] when:2026-11-05T09:00-09:15"
	if got := copyLines(plan); len(got) != 1 || got[0] != want {
		t.Fatalf("copies %q, want %q", got, want)
	}
	if plan.InOwnNote != 0 {
		t.Fatalf("own-note count: %d", plan.InOwnNote)
	}

	// An event note that names the task is counted and never copied.
	event := sessionNote{"Meeting.md", "Meeting", "---\npage-type: event\nwhen: 2026-11-02T09:00\ntask: \"[[Work#^ab12cd]]\"\n---\n"}
	due := sessionNote{"Work.md", "Work", "- [ ] T due:2026-11-02 @repeat:1w ^ab12cd\n"}
	plan, _ = planRollOf(t, "2026-11-02", "Work.md", 0, false, RollComplete, due, event)
	if len(plan.Copies) != 0 || plan.InOwnNote != 1 {
		t.Fatalf("event note: %+v", plan)
	}
	if first := strings.Split(plan.Body, "\n")[0]; first != "- [ ] T due:2026-11-09 @repeat:1w" {
		t.Fatalf("inserted line %q: it has no session, so no id", first)
	}
}

func TestPlanRollWithoutDueOrSessionAddsDue(t *testing.T) {
	work := sessionNote{"Work.md", "Work", "- [ ] T @repeat:3d\n"}
	plan, ok := planRollOf(t, "2026-11-02", "Work.md", 0, false, RollComplete, work)
	if !ok || plan.Body != "- [ ] T @repeat:3d due:2026-11-05\n- [x] T\n" {
		t.Fatalf("body %q, %v", plan.Body, ok)
	}
	if len(plan.Copies) != 0 {
		t.Fatalf("copies: %+v", plan.Copies)
	}
}

func TestPlanRollAtUntilCopiesNothing(t *testing.T) {
	work := sessionNote{"Work.md", "Work", "- [ ] Lab @repeat:1w @until:2026-12-14 ^ab12cd\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Lab task:[[Work#^ab12cd]] when:2026-12-14T09:00\n"}
	plan, ok := planRollOf(t, "2026-12-14", "Work.md", 0, false, RollComplete, work, plans)
	if !ok || plan.Rolled || plan.Body != "- [x] Lab @until:2026-12-14 ^ab12cd\n" || len(plan.Copies) != 0 {
		t.Fatalf("plan: %+v, %v", plan, ok)
	}
}

func TestPlanRollSessionCopyCarriesNoUntil(t *testing.T) {
	id := wantID()
	work := sessionNote{"Work.md", "Work", "- [ ] Lab @repeat:1w @until:2026-12-14 ^ab12cd\n"}
	plans := sessionNote{"Plans.md", "Plans", "- Lab task:[[Work#^ab12cd]] when:2026-12-07T09:00 @until:2026-12-20\n"}
	plan, _ := planRollOf(t, "2026-12-07", "Work.md", 0, false, RollComplete, work, plans)
	if first := strings.Split(plan.Body, "\n")[0]; first != "- [ ] Lab @repeat:1w @until:2026-12-14 ^"+id {
		t.Fatalf("inserted line %q", first)
	}
	want := "Plans.md:- Lab task:[[Work#^" + id + "]] when:2026-12-14T09:00"
	if got := copyLines(plan); len(got) != 1 || got[0] != want {
		t.Fatalf("copies %q, want %q", got, want)
	}
}

// A session in the note of its own task goes into the new text of that note.
func TestPlanRollCopiesSessionInTheTaskNote(t *testing.T) {
	id := wantID()
	work := sessionNote{"Work.md", "Work", "- [ ] T @repeat:1w ^ab12cd\n- T task:[[Work#^ab12cd]] when:2026-11-02T09:00\n"}
	plan, _ := planRollOf(t, "2026-11-02", "Work.md", 0, false, RollComplete, work)
	want := "- [ ] T @repeat:1w ^" + id + "\n- [x] T ^ab12cd\n" +
		"- T task:[[Work#^ab12cd]] when:2026-11-02T09:00\n" +
		"- T task:[[Work#^" + id + "]] when:2026-11-09T09:00\n"
	if plan.Body != want || len(plan.Copies) != 0 || plan.Inserted != 1 {
		t.Fatalf("body:\n%s\nwant:\n%s\n%+v", plan.Body, want, plan)
	}
}

func TestPlanRollDueLandsOnRuleDateWhenSessionIsOnAnotherDay(t *testing.T) {
	id := wantID()
	plans := sessionNote{"Plans.md", "Plans", "- Workout when:2026-11-08T18:00-19:00 task:[[Gym]]\n"}
	wantSession := "Plans.md:- Workout when:2026-11-11T18:00-19:00 task:[[Gym]]"

	// A task note: the due date is on a day that the rule allows.
	gym := sessionNote{"Gym.md", "Gym", "---\npage-type: task\nstatus: open\ndue: 2026-11-07T18:00\nrepeat: mon/wed/fri\nskip: [2026-11-09]\n---\n\n# Gym\n"}
	plan, ok := planRollOf(t, "2026-11-08", "Gym.md", 0, true, RollComplete, gym, plans)
	if !ok || !plan.Rolled {
		t.Fatalf("plan: %+v, %v", plan, ok)
	}
	if !strings.Contains(plan.Body, "\ndue: 2026-11-11T18:00\n") {
		t.Fatalf("note: %q", plan.Body)
	}
	if got := copyLines(plan); len(got) != 1 || got[0] != wantSession {
		t.Fatalf("copies %q", got)
	}

	// A checkbox line does the same, and keeps its time and range.
	line := sessionNote{"Work.md", "Gym", "- [ ] Gym due:2026-11-07T18:00-19:00 @repeat:mon/wed/fri @skip:2026-11-09 ^ab12cd\n"}
	link := sessionNote{"Plans.md", "Plans", "- Workout when:2026-11-08T18:00-19:00 task:[[Gym#^ab12cd]]\n"}
	plan, ok = planRollOf(t, "2026-11-08", "Work.md", 0, false, RollComplete, line, link)
	if !ok || !plan.Rolled {
		t.Fatalf("plan: %+v, %v", plan, ok)
	}
	want := "- [ ] Gym due:2026-11-11T18:00-19:00 @repeat:mon/wed/fri @skip:2026-11-09 ^" + id
	if first := strings.Split(plan.Body, "\n")[0]; first != want {
		t.Fatalf("inserted line %q, want %q", first, want)
	}
}

const rollGymNote = "---\npage-type: task\nstatus: open\ndue: 2026-11-11T18:00\nrepeat: mon/wed/fri\nlastCompleted: 2026-11-09\n---\n# Gym\n"

func TestPlanRollWritesNoCopyThatTheNoteHolds(t *testing.T) {
	gym := sessionNote{"Gym.md", "Gym", rollGymNote}
	held := "- Workout when:2026-11-09T18:00-19:00 task:[[Gym]]\n" +
		"- Workout when:2026-11-11T18:00-19:00 task:[[Gym]]\n" +
		"- Workout when:2026-11-13T18:00-19:00 task:[[Gym]]\n"

	// The plans note holds the Friday copy already, so only the Sunday copy is new.
	plan, ok := planRollOf(t, "2026-11-11", "Gym.md", 0, true, RollComplete, gym, sessionNote{"Plans.md", "Plans", held})
	if !ok || !plan.Rolled {
		t.Fatalf("plan: %+v, %v", plan, ok)
	}
	if got := copyLines(plan); len(got) != 1 || got[0] != "Plans.md:- Workout when:2026-11-15T18:00-19:00 task:[[Gym]]" {
		t.Errorf("copies %q, want only the Sunday copy", got)
	}

	// A line for the same day with another link does not count as the copy.
	other := strings.Replace(held, "2026-11-13T18:00-19:00 task:[[Gym]]", "2026-11-13T18:00-19:00 task:[[Run]]", 1)
	plan, _ = planRollOf(t, "2026-11-11", "Gym.md", 0, true, RollComplete, gym, sessionNote{"Plans.md", "Plans", other})
	if got := copyLines(plan); len(got) != 1 || got[0] != "Plans.md:- Workout when:2026-11-13T18:00-19:00 task:[[Gym]]" {
		t.Errorf("copies %q, want the Friday copy", got)
	}

	// The same holds for a session in the note of the task itself.
	own := sessionNote{"Gym.md", "Gym", rollGymNote + held}
	plan, _ = planRollOf(t, "2026-11-11", "Gym.md", 0, true, RollComplete, own)
	if strings.Count(plan.Body, "when:2026-11-13T18:00-19:00 task:[[Gym]]") != 1 {
		t.Errorf("body holds the Friday copy twice:\n%s", plan.Body)
	}
}
