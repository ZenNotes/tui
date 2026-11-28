package vault

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type lintWant struct {
	check TaskValueCheck
	path  string
	lines []int
	value string
}

const lintSettings = `{"primaryNotesLocation":"root","templates":{"folder":"Templates"},"tasks":{"excludedFolders":["Old"]}}`

func lintProblems(t *testing.T, files map[string]string) []TaskValueProblem {
	t.Helper()
	v := openTemplateFolderVault(t, lintSettings, files)
	problems, err := v.LintTaskValues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return problems
}

func lintVault(t *testing.T, files map[string]string) []lintWant {
	t.Helper()
	problems := lintProblems(t, files)
	got := []lintWant{}
	for _, p := range problems {
		if strings.TrimSpace(p.Reason) == "" {
			t.Errorf("problem without a reason: %+v", p)
		}
		got = append(got, lintWant{p.Check, p.Path, p.Lines, p.Value})
	}
	return got
}

func TestLintFindsRepeatValueNoAppReads(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "# A\n\n- [ ] good @repeat:weekly\n- [ ] bad @repeat:fortnightly\n- [x] odd @repeat:every-3-days\n- [ ] plain\n",
	})
	want := []lintWant{
		{TaskValueRepeat, "Notes/A.md", []int{4}, "@repeat:fortnightly"},
		{TaskValueRepeat, "Notes/A.md", []int{5}, "@repeat:every-3-days"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLintFindsDueValueNoAppReadsAsDate(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "- [ ] ok due:2025-03-04\n- [ ] day due:2025-02-30\n- [ ] words due: tomorrow\n- [ ] short due:2025-3-4\n",
	})
	want := []lintWant{
		{TaskValueDue, "Notes/A.md", []int{2}, "due:2025-02-30"},
		{TaskValueDue, "Notes/A.md", []int{3}, "due:tomorrow"},
		{TaskValueDue, "Notes/A.md", []int{4}, "due:2025-3-4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLintChecksDueInFrontmatter(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "---\ntitle: A\ndue: next week\n---\n\n- [ ] x\n",
		"Notes/B.md": "---\ndue: \"2025-04-05\"\n---\n- [ ] x\n",
		"Notes/C.md": "---\ndue: 2025-13-01\ntags: [task]\ntasks: note\n---\n",
		"Notes/D.md": "---\ndue: soon\ntasks: note\n---\n",
		"Notes/E.md": "---\ndue: soon\ntasks: false\n---\n- [ ] x\n",
		"Notes/F.md": "---\ndue: soon\n---\n- [ ] x due:2025-01-02\n",
		"Notes/G.md": "---\ndue: soon\n---\n- [ ] x due:bad\n",
	})
	want := []lintWant{
		{TaskValueDue, "Notes/A.md", []int{3}, "due:next week"},
		{TaskValueDue, "Notes/C.md", []int{2}, "due:2025-13-01"},
		{TaskValueDue, "Notes/G.md", []int{2}, "due:soon"},
		{TaskValueDue, "Notes/G.md", []int{4}, "due:bad"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLintFrontmatterDueOfANoteWithNoTaskGivesNothing(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "---\ndue: Q3\n---\n\nJust prose.\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLintReadsFrontmatterAsTheTaskParserDoes(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/List.md":  "---\ntags: [task]\ndue: [2025-01-01]\n---\n",
		"Notes/Space.md": "--- \ndue: soon\n---\n- [ ] x\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLintGivesADayThatDoesNotExistItsOwnReason(t *testing.T) {
	got := lintProblems(t, map[string]string{
		"Notes/A.md": "- [ ] a due:2025-02-30\n- [ ] b due:2025-04-31\n- [ ] c due:2025-02-29\n- [ ] d due:2025-13-01\n- [ ] e due:2025-02-32\n- [ ] f due:2025-00-30\n",
		"Notes/B.md": "---\ndue: 2025-06-31\n---\n- [ ] x\n",
	})
	reasons := map[string]string{}
	for _, p := range got {
		reasons[p.Path+":"+p.Value] = p.Reason
	}
	for _, key := range []string{"Notes/A.md:due:2025-02-30", "Notes/A.md:due:2025-04-31", "Notes/A.md:due:2025-02-29", "Notes/B.md:due:2025-06-31"} {
		if reasons[key] != dueNoSuchDayReason {
			t.Errorf("%s reason %q", key, reasons[key])
		}
	}
	for _, key := range []string{"Notes/A.md:due:2025-13-01", "Notes/A.md:due:2025-02-32", "Notes/A.md:due:2025-00-30"} {
		if reasons[key] != dueUnreadReason {
			t.Errorf("%s reason %q", key, reasons[key])
		}
	}
	if len(got) != 7 {
		t.Errorf("got %d problems: %+v", len(got), got)
	}
}

func TestLintStopsWhenItsContextEnds(t *testing.T) {
	v := openTemplateFolderVault(t, lintSettings, map[string]string{"Notes/A.md": "- [ ] a due:soon\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.LintTaskValues(ctx); err == nil {
		t.Fatal("a canceled context did not stop the lint")
	}
}

func TestLintFindsBlockIDOnMoreThanOneLine(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "- [ ] one ^abc\ntext\n- [ ] two ^ABC\n- [ ] three ^x1\n\n^abc\n",
	})
	want := []lintWant{{TaskValueBlockID, "Notes/A.md", []int{1, 3, 6}, "^abc"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLintGivesNothingForValidValues(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "---\ndue: 2025-01-02\n---\n- [ ] a @repeat:after-2w due:2025-01-02 ^one\n- [ ] b @repeat:weekdays ^two\n- [ ] c @repeat:mon/wed ^three\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLintSkipsFencedCodeAndFrontmatter(t *testing.T) {
	bad := "- [ ] x @repeat:nope due:nope ^dup\n- [ ] y ^dup\n"
	got := lintVault(t, map[string]string{
		"Notes/Fence.md": "```\n" + bad + "```\n~~~\n" + bad + "~~~\n",
		"Notes/Front.md": "---\nnote: - [ ] x @repeat:nope due:nope ^dup\nother: ^dup\n---\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// A block id is a link target, so notes that the task scan leaves out count.
func TestLintChecksBlockIDsOfNotesThatTasksLeaveOut(t *testing.T) {
	bad := "- [ ] x @repeat:nope due:nope ^dup\n- [ ] y ^dup\n"
	got := lintVault(t, map[string]string{
		"Notes/Opt.md":     "---\ntasks: false\n---\n" + bad,
		"Templates/Tpl.md": bad,
		"Old/Old.md":       bad,
	})
	want := []lintWant{
		{TaskValueBlockID, "Notes/Opt.md", []int{4, 5}, "^dup"},
		{TaskValueBlockID, "Old/Old.md", []int{1, 2}, "^dup"},
		{TaskValueBlockID, "Templates/Tpl.md", []int{1, 2}, "^dup"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLintCountsLinesOfCRLFNotes(t *testing.T) {
	got := lintVault(t, map[string]string{
		"Notes/A.md": "---\r\ntitle: A\r\n---\r\n\r\n- [ ] a @repeat:nope\r\n- [ ] b due:nope ^id\r\n- [ ] c ^id\r\n",
	})
	want := []lintWant{
		{TaskValueRepeat, "Notes/A.md", []int{5}, "@repeat:nope"},
		{TaskValueDue, "Notes/A.md", []int{6}, "due:nope"},
		{TaskValueBlockID, "Notes/A.md", []int{6, 7}, "^id"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLintGivesNothingForALeapDay(t *testing.T) {
	got := lintProblems(t, map[string]string{
		"Notes/A.md": "- [ ] g due:2024-02-29\n",
		"Notes/B.md": "---\ndue: 2024-02-29\n---\n- [ ] x\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLintGivesTheLineOfTheDueKeyThatATaskNoteReads(t *testing.T) {
	got := lintProblems(t, map[string]string{
		"Notes/C.md": "---\ntags: [task]\ntasks: note\ndue: 2025-01-01\ndue: soon\n---\n",
	})
	if len(got) != 1 || got[0].Value != "due:soon" || len(got[0].Lines) != 1 || got[0].Lines[0] != 5 {
		t.Fatalf("got %+v, want due:soon on line 5", got)
	}
}
