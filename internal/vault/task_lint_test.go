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
		"- [ ] fine due:2026-11-03T14:00 repeat:2w skip:2027-03-13..2027-03-21",
		"- [ ] late due:2026-11-03T25:00",
		"- [ ] soon scheduled:2026-11-03T9:00 #x",
		"- [ ] often repeat:every-day",
		"- [ ] away skip:2027-03-21..2027-03-13,2027-02-30,2027-04-01",
		"```",
		"- [ ] fenced due:2026-11-03T99:99",
		"```",
		"- [ ] not a date due:soon",
	}, "\n")
	got := problemSummary(TaskTokenProblems("n.md", body))
	want := []string{
		"n.md:3 due:2026-11-03T25:00",
		"n.md:4 scheduled:2026-11-03T9:00",
		"n.md:5 repeat:every-day",
		"n.md:6 skip:2027-03-21..2027-03-13",
		"n.md:6 skip:2027-02-30",
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
		"scheduled: \"2026-11-04T08:30\"",
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
		"t.md:5 repeat:nonsense",
		"t.md:8 skip:2027-13-01",
		"t.md:11 due:2026-11-03T99:00",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	plain := "---\ndue: 2026-11-03T25:00\n---\n"
	if got := TaskTokenProblems("p.md", plain); len(got) != 0 {
		t.Fatalf("a note that is not a task note was linted: %+v", got)
	}
}
