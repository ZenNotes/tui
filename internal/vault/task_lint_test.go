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
		"- [ ] fine due:2026-11-03T14:00 @repeat:2w skip:2027-03-13..2027-03-21",
		"- [ ] late due:2026-11-03T25:00",
		"- [ ] often @repeat:every-day",
		"- [ ] away skip:2027-03-21..2027-03-13,2027-02-30,2027-04-01",
		"```",
		"- [ ] fenced due:2026-11-03T99:99",
		"```",
		"- [ ] not a date due:soon",
	}, "\n")
	got := problemSummary(TaskTokenProblems("n.md", body))
	want := []string{
		"n.md:3 due:2026-11-03T25:00",
		"n.md:4 @repeat:every-day",
		"n.md:5 skip:2027-03-21..2027-03-13",
		"n.md:5 skip:2027-02-30",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
}

func TestTaskTokenProblemsIgnoresReservedSkipNames(t *testing.T) {
	body := "- [ ] away skip:@holidays,@school due:2026-11-03T14:00\n"
	if got := TaskTokenProblems("n.md", body); len(got) != 0 {
		t.Fatalf("reserved skip names reported: %+v", got)
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
	body := "- [ ] a due:2026-11-03T25:00 @repeat:often skip:2027-02-30\n"
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
