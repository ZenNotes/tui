package tui

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/charmbracelet/lipgloss"
)

func TestCalMoreLabelFallsBackWithoutCuttingWords(t *testing.T) {
	cases := []struct {
		n, w int
		want string
	}{
		{4, 20, " +4 more"},
		{4, 8, " +4 more"},
		{4, 7, " +4"},
		{4, 3, " +4"},
		{12, 3, " +…"},
		{4, 0, ""},
	}
	for _, c := range cases {
		got := calMoreLabel(c.n, c.w)
		if got != c.want {
			t.Errorf("calMoreLabel(%d, %d) = %q, want %q", c.n, c.w, got, c.want)
		}
		if lipgloss.Width(got) > c.w {
			t.Errorf("calMoreLabel(%d, %d) = %q is wider than %d", c.n, c.w, got, c.w)
		}
	}
}

// A month box too narrow for "+N more" must read "+N", never a cut word.
func TestMonthOverflowLineNeverCutMidWord(t *testing.T) {
	var tasks []vault.Task
	for i := 0; i < 50; i++ {
		tasks = append(tasks, vault.Task{ID: fmt.Sprintf("n#%d", i), SourcePath: "n.md", Content: fmt.Sprintf("zq%02d", i), Due: "2026-11-10"})
	}
	cut := regexp.MustCompile(`\+\d+ mo?r?(\s|$)`)
	short := regexp.MustCompile(`\+\d+(\s|$)`)
	for _, sz := range [][2]int{{120, 40}, {72, 19}} {
		a, tv := calTestApp(t, tasks)
		lines := calScreen(a, tv, sz[0], sz[1])
		text := calScreenText(lines)
		for _, l := range lines {
			if lipgloss.Width(l) > sz[0] {
				t.Errorf("%dx%d: line wider than the body: %q", sz[0], sz[1], l)
			}
			if m := cut.FindString(l); m != "" {
				t.Errorf("%dx%d: overflow line cut mid-word: %q\n%s", sz[0], sz[1], m, text)
			}
		}
		if !short.MatchString(text) {
			t.Errorf("%dx%d: no overflow count shown:\n%s", sz[0], sz[1], text)
		}
	}
}
