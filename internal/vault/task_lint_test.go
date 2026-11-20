package vault

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func problemSummary(problems []TaskTokenProblem) []string {
	out := []string{}
	for _, p := range problems {
		out = append(out, fmt.Sprintf("%s:%d %s", p.Path, p.Line, p.Token))
	}
	return out
}

func TestTaskTokenProblemsFindsBadTimesRulesAndSkipItems(t *testing.T) {
	body := strings.Join([]string{
		"# Plan",
		"- [ ] fine due:2026-11-03T14:00 @repeat:2w @skip:2027-03-13/2027-03-21",
		"- [ ] late due:2026-11-03T25:00",
		"- [ ] often @repeat:every-day",
		"- [ ] away @skip:2027-03-21/2027-03-13 @skip:2027-02-30 @skip:2027-04-01",
		"```",
		"- [ ] fenced due:2026-11-03T99:99",
		"```",
		"- [ ] not a date due:soon",
	}, "\n")
	got := problemSummary(TaskTokenProblems("n.md", body))
	want := []string{
		"n.md:3 due:2026-11-03T25:00",
		"n.md:4 @repeat:every-day",
		"n.md:5 @skip:2027-03-21/2027-03-13",
		"n.md:5 @skip:2027-02-30",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsReadsFrontmatterLines(t *testing.T) {
	body := strings.Join([]string{
		"---",
		"tags: [task]",
		"due: 2026-11-03T25:00",
		"repeat: nonsense",
		"skip:",
		"  - 2027-03-13",
		"  - 2027-13-01",
		"  - \"@holidays\"",
		"---",
		"- [ ] body due:2026-11-03T99:00",
	}, "\n")
	got := problemSummary(TaskTokenProblems("t.md", body))
	want := []string{
		"t.md:3 due:2026-11-03T25:00",
		"t.md:4 repeat:nonsense",
		"t.md:7 skip:2027-13-01",
		"t.md:8 skip:@holidays",
		"t.md:10 due:2026-11-03T99:00",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	plain := "---\ndue: 2026-11-03T25:00\n---\n"
	if got := TaskTokenProblems("p.md", plain); len(got) != 0 {
		t.Fatalf("a note that is not a task note was linted: %+v", got)
	}
}

// A `scheduled` token or key is plain text, so no value of it is a problem.
func TestTaskTokenProblemsIgnoresScheduled(t *testing.T) {
	body := strings.Join([]string{
		"---",
		"tags: [task]",
		"scheduled: 2026-11-04T25:99",
		"---",
		"- [ ] soon scheduled:2026-11-03T9:00 #x",
		"- [ ] later scheduled:2026-11-03T99:99 due:2026-11-04",
	}, "\n")
	if got := TaskTokenProblems("n.md", body); len(got) != 0 {
		t.Fatalf("scheduled values reported: %+v", got)
	}
}

// Each problem says why the token is set aside.
func TestTaskTokenProblemsGiveAReason(t *testing.T) {
	body := "- [ ] a due:2026-11-03T25:00 @repeat:often @skip:2027-02-30\n"
	got := TaskTokenProblems("n.md", body)
	if len(got) != 3 {
		t.Fatalf("problems: %+v", got)
	}
	for _, p := range got {
		if strings.TrimSpace(p.Reason) == "" {
			t.Errorf("%s has no reason", p.Token)
		}
	}
}

func TestTaskTokenProblemsListsBadRepeatFields(t *testing.T) {
	body := strings.Join([]string{
		"- [ ] a @repeat:fortnightly",
		"- [ ] b @repeat:3d/done repeat:often",
		"- [ ] c @repeat:monthly-32 @repeat:yearly-02-30",
		"- [ ] d @repeat:after-mon/thu @repeat:weekly-mon/thu @repeat:after-quarterly",
		"- [ ] e repeat:3d repeat:mon,thu",
		"```",
		"- [ ] f @repeat:nonsense",
		"```",
	}, "\n")
	got := problemSummary(TaskTokenProblems("n.md", body))
	want := []string{
		"n.md:1 @repeat:fortnightly",
		"n.md:2 @repeat:3d/done",
		"n.md:3 @repeat:monthly-32",
		"n.md:3 @repeat:yearly-02-30",
		"n.md:4 @repeat:after-mon/thu",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}

	note := "---\ntags: [task]\nrepeat: 3d/done\n---\n"
	got = problemSummary(TaskTokenProblems("t.md", note))
	if want := []string{"t.md:3 repeat:3d/done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frontmatter problems\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsListsSkipAndUntilFields(t *testing.T) {
	body := strings.Join([]string{
		"- [ ] a @repeat:1w @skip:2027-03-21/2027-03-13 @skip:2027-02-30 @skip:2027-04-01 @skip:2027-03-13/2027-03-21",
		"- [ ] b @repeat:1w @skip:2027-03-13..2027-03-21",
		"- [ ] c @repeat:1w due:2026-11-02 @until:2026-02-30 @until:2026-12-01",
		"- [ ] d due:2026-11-02 @until:2026-12-01",
		"- [ ] e @repeat:1w due:2026-11-09 @until:2026-11-02",
		"- [ ] f @repeat:1w due:2026-11-02 @until:2026-12-01 @until:2026-12-08",
		"- [ ] g skip:2027-02-30 until:soon skip:..",
		"```",
		"- [ ] h @skip:2027-02-30 @until:soon",
		"```",
	}, "\n")
	got := problemSummary(TaskTokenProblems("n.md", body))
	want := []string{
		"n.md:1 @skip:2027-03-21/2027-03-13",
		"n.md:1 @skip:2027-02-30",
		"n.md:2 @skip:2027-03-13..2027-03-21",
		"n.md:3 @until:2026-02-30",
		"n.md:3 @until:2026-12-01",
		"n.md:4 @until:2026-12-01",
		"n.md:5 @until:2026-11-02",
		"n.md:6 @until:2026-12-08",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	for _, p := range TaskTokenProblems("n.md", body) {
		if p.Line == 2 && !strings.Contains(p.Reason, "/") {
			t.Errorf("reason %q does not name the / form", p.Reason)
		}
	}

	note := strings.Join([]string{
		"---",
		"tags: [task]",
		"due: 2026-11-02",
		"repeat: 1w",
		"skip: [2027-03-21/2027-03-13, easter, 2027-04-01]",
		"until: 2026-02-30",
		"---",
	}, "\n")
	got = problemSummary(TaskTokenProblems("t.md", note))
	want = []string{"t.md:5 skip:2027-03-21/2027-03-13", "t.md:6 until:2026-02-30"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frontmatter problems\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsListsUnresolvedSkipNames(t *testing.T) {
	events := noteEvent("Spring Break", FolderInbox, "when: 2027-03-13\n")
	body := strings.Join([]string{
		"- [ ] a @repeat:1w @skip:spring-break @skip:Spring_Break @skip:nope",
		"---",
	}, "\n")
	got := problemSummary(TaskTokenProblemsWithEvents("n.md", body, events))
	want := []string{"n.md:1 @skip:Spring_Break", "n.md:1 @skip:nope"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	if got := TaskTokenProblems("n.md", body); len(got) != 0 {
		t.Fatalf("names are checked without events: %+v", got)
	}

	// Two notes with one title are ambiguous, and the reason names both.
	first := noteEvent("Spring Break", FolderInbox, "when: 2027-03-13\n")[0]
	first.Path = "a/Spring Break.md"
	second := noteEvent("Spring Break", FolderInbox, "when: 2027-03-14\n")[0]
	second.Path = "b/Spring Break.md"
	problems := TaskTokenProblemsWithEvents("n.md", "- [ ] a @repeat:1w @skip:spring-break\n", []Event{first, second})
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "a/Spring Break.md") || !strings.Contains(problems[0].Reason, "b/Spring Break.md") {
		t.Fatalf("ambiguous name: %+v", problems)
	}

	// A note in the trash and an inline event answer no name.
	trashed := noteEvent("Gym", FolderTrash, "when: 2027-03-13\n")[0]
	inline := Event{Kind: EventKindInline, Text: "Camp", StartDate: "2027-03-13"}
	got = problemSummary(TaskTokenProblemsWithEvents("n.md", "- [ ] a @repeat:1w @skip:gym @skip:camp\n", []Event{trashed, inline}))
	if want := []string{"n.md:1 @skip:gym", "n.md:1 @skip:camp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trash and inline\n got %q\nwant %q", got, want)
	}

	// Frontmatter names have no @ and are checked in the same way.
	note := "---\ntags: [task]\nrepeat: 1w\nskip: [spring-break, nope]\n---\n"
	got = problemSummary(TaskTokenProblemsWithEvents("t.md", note, events))
	if want := []string{"t.md:4 skip:nope"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frontmatter names\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsListsSkipNamesThatLeadBack(t *testing.T) {
	a := noteEvent("A", FolderInbox, "when: 2027-03-01\nrepeat: 1w\nskip: [b]\n")
	b := noteEvent("B", FolderInbox, "when: 2027-03-02\nrepeat: 1w\nskip: [a]\n")
	events := append(a, b...)
	body := "---\ntags: [task]\nwhen: 2027-03-01\nrepeat: 1w\nskip: [b]\n---\n"
	got := TaskTokenProblemsWithEvents("A.md", body, events)
	if len(got) != 1 || got[0].Token != "skip:b" || !strings.Contains(got[0].Reason, "leads back") {
		t.Fatalf("cycle: %+v", got)
	}
	// A task line has no event note of its own, so the same name is fine.
	line := "- [ ] t @repeat:1w @skip:b\n"
	if got := TaskTokenProblemsWithEvents("A.md", line, events); len(got) != 0 {
		t.Fatalf("task line: %+v", got)
	}
}

// problemReasons maps each listed token to its reason, for tests that need
// the words of a reason.
func problemReasons(problems []TaskTokenProblem) map[string]string {
	out := map[string]string{}
	for _, p := range problems {
		out[fmt.Sprintf("%d %s", p.Line, p.Token)] = p.Reason
	}
	return out
}

func TestTaskTokenProblemsListsEventProblems(t *testing.T) {
	body := strings.Join([]string{
		"- Dentist when:2026-11-03 when:2026-11-04",
		"- Dentist when:2026-02-30",
		"- Dentist when:2026-11-3",
		"- Ask them when:tomorrow",
		"- Dentist when:2026-11-03T25:00",
		"- E when:2026-11-04/2026-11-03",
		"- E when:2026-11-03T22:00/2026-11-03T21:00",
		"- E when:2026-11-03T22:00/2026-11-04",
		"- E when:2026-11-03/2026-02-30",
		"- Late when:2026-11-03T23:00-01:00",
		"- Fine when:2026-11-03T09:00-10:00 @repeat:weekly @skip:2026-12-21/2027-01-03 @until:2027-06-01",
		"- Gym when:2026-11-02T07:00 @repeat:1w/done @repeat:after-1w",
		"- Gym when:2026-11-02T07:00 @repeat:nonsense @skip:2027-02-30 @skip:2027-03-21/2027-03-13",
		"- Gym when:2026-11-02T07:00 @until:2027-06-01",
		"- Gym when:2026-11-02T07:00 @repeat:1w @until:2026-10-01",
		"- see `when:2026-02-30 @repeat:nonsense`",
		"- Plan when:2026-11-03 @repeat:after-1w",
		"- Dentist !high @waiting when:2026-11-03",
		"```",
		"- Dentist when:2026-02-30",
		"```",
		"- [ ] Call Ana when:2026-11-03T14:00",
	}, "\n")
	got := problemSummary(TaskTokenProblems("Plans.md", body))
	want := []string{
		"Plans.md:1 when:2026-11-03",
		"Plans.md:2 when:2026-02-30",
		"Plans.md:3 when:2026-11-3",
		"Plans.md:5 when:2026-11-03T25:00",
		"Plans.md:6 when:2026-11-04/2026-11-03",
		"Plans.md:7 when:2026-11-03T22:00/2026-11-03T21:00",
		"Plans.md:8 when:2026-11-03T22:00/2026-11-04",
		"Plans.md:9 when:2026-11-03/2026-02-30",
		"Plans.md:10 when:2026-11-03T23:00-01:00",
		"Plans.md:12 @repeat:1w/done",
		"Plans.md:12 @repeat:after-1w",
		"Plans.md:13 @repeat:nonsense",
		"Plans.md:13 @skip:2027-02-30",
		"Plans.md:13 @skip:2027-03-21/2027-03-13",
		"Plans.md:14 @until:2027-06-01",
		"Plans.md:15 @until:2026-10-01",
		"Plans.md:17 @repeat:after-1w",
		"Plans.md:22 when:2026-11-03T14:00",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	reasons := problemReasons(TaskTokenProblems("Plans.md", body))
	if r := reasons["10 when:2026-11-03T23:00-01:00"]; !strings.Contains(r, "2026-11-03T23:00/2026-11-04T01:00") {
		t.Errorf("reason for the end before the start: %q", r)
	}
	for key, r := range reasons {
		if strings.TrimSpace(r) == "" {
			t.Errorf("%s has no reason", key)
		}
	}
}

func TestTaskTokenProblemsListsEventNoteProblems(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"no when", "---\npage-type: event\n---\n", []string{"E.md:2 page-type:event"}},
		{"bad when", "---\npage-type: event\nwhen: 2026-02-30\n---\n", []string{"E.md:3 when:2026-02-30"}},
		{"bad range", "---\npage-type: event\nwhen: 2026-11-04/2026-11-03\n---\n", []string{"E.md:3 when:2026-11-04/2026-11-03"}},
		{"after", "---\npage-type: event\nwhen: 2026-11-03\nrepeat: after-1w\n---\n", []string{"E.md:4 repeat:after-1w"}},
		{"bad rule", "---\npage-type: event\nwhen: 2026-11-03\nrepeat: nonsense\nskip: [2027-02-30, 2027-03-13]\n---\n",
			[]string{"E.md:4 repeat:nonsense", "E.md:5 skip:2027-02-30"}},
		{"until", "---\npage-type: event\nwhen: 2026-11-03\nrepeat: 1w\nuntil: 2026-10-01\n---\n", []string{"E.md:5 until:2026-10-01"}},
		{"until without rule", "---\npage-type: event\nwhen: 2026-11-03\nuntil: 2027-10-01\n---\n", []string{"E.md:4 until:2027-10-01"}},
		{"fine", "---\npage-type: event\nwhen: 2026-11-03T14:00-15:30\nrepeat: weekly\nskip: [2026-11-10]\nuntil: 2027-01-01\n---\n", nil},
		{"event list", "---\npage-type: event-list\nwhen: 2026-11-03\n---\n- A when:2026-11-03\n", nil},
		{"events off", "---\nevents: false\n---\n- A when:2026-02-30\n", nil},
		{"not an event", "---\nwhen: 2026-02-30\nrepeat: nonsense\n---\n", nil},
	}
	for _, tc := range cases {
		got := problemSummary(TaskTokenProblems("E.md", tc.body))
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

func TestTaskTokenProblemsListsEventSkipNames(t *testing.T) {
	a := noteEvent("A", FolderInbox, "when: 2027-03-01\nrepeat: 1w\nskip: [b]\n")
	b := noteEvent("B", FolderInbox, "when: 2027-03-02\nrepeat: 1w\nskip: [a]\n")
	events := append(a, b...)
	body := "---\npage-type: event\nwhen: 2027-03-01\nrepeat: 1w\nskip: [b, nope]\n---\n- Gym when:2027-03-01 @repeat:1w @skip:a @skip:nope\n"
	got := problemSummary(TaskTokenProblemsWithEvents("A.md", body, events))
	want := []string{"A.md:5 skip:b", "A.md:5 skip:nope", "A.md:7 @skip:nope"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsListsCutRepeatValues(t *testing.T) {
	body := strings.Join([]string{
		"- [ ] a @repeat:mon,thu",
		"- Gym when:2026-11-02 @repeat:mon,thu",
		"- [ ] b @repeat:mon/thu",
		"- [ ] c @repeat:daily.",
		"- [ ] d @repeat:weekly,",
	}, "\n")
	problems := TaskTokenProblems("n.md", body)
	if got, want := problemSummary(problems), []string{"n.md:1 @repeat:mon,thu", "n.md:2 @repeat:mon,thu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	for _, p := range problems {
		if !strings.Contains(p.Reason, ",") || !strings.Contains(p.Reason, "mon/thu") {
			t.Errorf("reason lacks the cut character or the slash form: %q", p.Reason)
		}
	}
}

func TestTaskTokenProblemsListsDanglingLinksAndDuplicateIds(t *testing.T) {
	v := newTestVault(t)
	writeFile(t, filepath.Join(v.Root(), "inbox", "Work.md"), "- [ ] Write report ^ab12cd\n- [ ] Plan ^ef34gh\n")
	writeFile(t, filepath.Join(v.Root(), "inbox", "Other.md"), "- [ ] Twin ^ef34gh\n- Bullet ^zz99zz\n- Bullet two ^zz99zz\n")
	writeFile(t, filepath.Join(v.Root(), "inbox", "Plans.md"), strings.Join([]string{
		"- Good task:[[Work#^ab12cd]] when:2026-11-03T14:00",
		"- Missing task:[[Gone#^ab12cd]] when:2026-11-03T15:00",
		"- Nobody task:[[Work#^nothing]] when:2026-11-03T16:00",
		"- Plain task:[[Other#^zz99zz]] when:2026-11-03T17:00",
		"- Whole task:[[Nowhere]] when:2026-11-03T18:00",
		"- Note only task:[[Work#^ab12cd]]",
	}, "\n")+"\n")
	writeFile(t, filepath.Join(v.Root(), "inbox", "Event.md"), "---\npage-type: event\nwhen: 2026-11-03\ntask: \"[[Gone]]\"\n---\n")
	problems, err := v.ScanTaskTokenProblems()
	if err != nil {
		t.Fatal(err)
	}
	got := problemSummary(problems)
	want := []string{
		"inbox/Event.md:4 task:[[Gone]]",
		"inbox/Other.md:1 ^ef34gh",
		"inbox/Other.md:2 ^zz99zz",
		"inbox/Other.md:3 ^zz99zz",
		"inbox/Plans.md:3 task:[[Work#^nothing]]",
		"inbox/Plans.md:4 task:[[Other#^zz99zz]]",
		"inbox/Plans.md:5 task:[[Nowhere]]",
		"inbox/Work.md:2 ^ef34gh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsIgnoresProseBareTokensAndCategoryNotes(t *testing.T) {
	body := strings.Join([]string{
		"---",
		"page-type: event-list",
		"when: 2026-11-03",
		"---",
		"Dentist when:2026-02-30",
		"- Ask them when:tomorrow",
		"- Ask them when: 2026-02-30",
		"- Ask them WHEN:2026-02-30",
		"- dinner:when:2026-02-30",
		"- [ ] A scheduled:2026-11-05 repeat:1w until:2026-02-30 skip:2027-02-30",
		"- [ ] B scheduled:2026-11-03T25:00 when:tomorrow",
	}, "\n")
	if got := TaskTokenProblems("Plans.md", body); len(got) != 0 {
		t.Fatalf("problems: %+v", got)
	}
}
