package vault

import (
	"fmt"
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
