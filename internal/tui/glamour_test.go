package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/search"
)

func TestSplitBlocks(t *testing.T) {
	src := "---\ntitle: x\n---\n# Heading\n\npara one\npara two\n\n- [ ] a\n- [x] b\n    - child\n\n```go\ncode\n```\n> quote\n| a | b |\n| - | - |\n"
	blocks := splitBlocks(strings.Split(src, "\n"), false)
	kinds := []string{}
	for _, b := range blocks {
		kinds = append(kinds, b.kind)
	}
	want := "front heading blank para blank list blank fence quote table blank"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds %q, want %q", got, want)
	}
	if blocks[5].start != 8 || blocks[5].end != 11 {
		t.Fatalf("list block lines %d..%d", blocks[5].start, blocks[5].end)
	}
}

func TestActiveSGR(t *testing.T) {
	if got := activeSGR("\x1b[38;5;252mtext"); got != "\x1b[38;5;252m" {
		t.Fatalf("256 color: %q", got)
	}
	if got := activeSGR("\x1b[1m\x1b[38;2;1;2;3mx\x1b[0m"); got != "" {
		t.Fatalf("reset should clear: %q", got)
	}
	if got := activeSGR("\x1b[31;1mx"); got != "\x1b[31;1m" {
		t.Fatalf("basic color + bold: %q", got)
	}
}

func TestPrepareMarkdown(t *testing.T) {
	out := prepareMarkdown("see [[Note|alias]] and [[Other]] ![[pic.png]]\n> [!tip] Title\n> body\n- [/] going\n- [-] dropped\n- [>] moved")
	for _, want := range []string{"⟦alias⟧", "⟦Other⟧", "⟦" + literalTokenOpen + "image: pic.png" + literalTokenClose + "⟧", "> **TIP** Title", "- ◩ going", "- ✕ ~~dropped~~", "- → moved"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestResolveGlamourStyle(t *testing.T) {
	for _, name := range []string{"auto", "dark", "light", "dracula", "tokyo-night", "pink", "ascii", "notty"} {
		cfg, err := resolveGlamourStyle(name, buildTheme(true))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cfg.Document.Margin == nil || *cfg.Document.Margin != 0 {
			t.Fatalf("%s: margin should be zeroed", name)
		}
	}
	if _, err := resolveGlamourStyle("/no/such/style.json", buildTheme(true)); err == nil {
		t.Fatal("missing file should error")
	}
}

func TestGlamourRendersBlocksWithSourceLines(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	a.prefs.PreviewStyle = "dark"
	text := "# Title\n\nHello [[World]] #tag\n\n- [ ] first\n- [x] second\n"
	lines, err := a.renderGlamourLines(text, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 5 {
		t.Fatalf("too few rows: %d", len(lines))
	}
	seen := map[int]bool{}
	for _, l := range lines {
		seen[l.srcLine] = true
	}
	for _, want := range []int{0, 2, 4, 5} {
		if !seen[want] {
			t.Fatalf("no row maps to source line %d: %+v", want, lines)
		}
	}
	taskRows := 0
	for _, l := range lines {
		if l.task {
			taskRows++
		}
	}
	if taskRows != 2 {
		t.Fatalf("task rows %d", taskRows)
	}
}

func TestFoldAndMatchPositions(t *testing.T) {
	if search.Fold("Résumé Ünïcode") != "resume unicode" {
		t.Fatalf("fold: %q", search.Fold("Résumé Ünïcode"))
	}
	pos := search.MatchPositions("rsm", "Résumé")
	if len(pos) != 3 || pos[0] != 0 || pos[1] != 2 || pos[2] != 4 {
		t.Fatalf("positions %v", pos)
	}
	if search.MatchPositions("zz", "Résumé") != nil {
		t.Fatal("no match should be nil")
	}
}

func TestKeysHintFollowsRebinding(t *testing.T) {
	a := &App{keymap: keymaps.NewResolver(map[string]string{"nav.moveDown": "n", "nav.contextMenu": ""})}
	got := a.keysHint([]hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"nav.contextMenu", "menu"}, {"key:Esc", "back"}})
	if got != "n/k move · Esc back" {
		t.Fatalf("hint %q", got)
	}
	if bindingLabel("g g") != "gg" || bindingLabel("Ctrl+D") != "Ctrl+D" {
		t.Fatal("binding labels")
	}
	if fitHint("a b · c d · e f", 9) != "a b …" {
		t.Fatalf("fit: %q", fitHint("a b · c d · e f", 9))
	}
}

// Glamour styles each word, so a multi-word wikilink token carries escape
// codes; styling the token must not split them into visible text that
// widens the row and makes the terminal wrap it.
func TestGlamourMultiWordWikilinkKeepsRowWidth(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	a.prefs.PreviewStyle = "dark"
	lines, err := a.renderGlamourLines("- jump between notes with [[double brackets]]\n", 60)
	if err != nil {
		t.Fatal(err)
	}
	row := lines[0].text
	if w := ansi.StringWidth(row); w > 60 {
		t.Fatalf("row is %d cells wide, want <= 60: %q", w, row)
	}
	if plain := ansi.Strip(row); !strings.Contains(plain, "double brackets") || strings.Contains(plain, "[0m") {
		t.Fatalf("link text mangled: %q", plain)
	}
}

// An image in running text shows as its placeholder, brackets included, in both
// styles, and the table it sits in keeps its columns.
func TestGlamourImagePlaceholderKeepsItsBrackets(t *testing.T) {
	for name, md := range map[string]string{
		"wikilink": "see ![[pic.png]] here\n",
		"markdown": "see ![pic.png](b.png) here\n",
		"list":     "- see ![[pic.png]] here\n",
		"quote":    "> see ![[pic.png]] here\n",
		"table":    "| see ![[pic.png]] here | x |\n|---|---|\n| 1 | 2 |\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := plainRows(glamourTestLines(t, md, 60))
			if !strings.Contains(got, "⟦image: pic.png⟧") {
				t.Errorf("no placeholder:\n%s", got)
			}
		})
	}
	t.Run("table columns", func(t *testing.T) {
		rows := glamourTestLines(t, "| ![[pic.png]] | x |\n|---|---|\n| 1 | 2 |\n", 60)
		var rules []int
		for _, l := range rows {
			text := stripAnsi(l.text)
			for i, r := range []rune(text) {
				if r == '│' || r == '┼' {
					rules = append(rules, i)
					break
				}
			}
		}
		for _, r := range rules {
			if r != rules[0] {
				t.Errorf("the column rule moved:\n%s", plainRows(rows))
				break
			}
		}
	})
	t.Run("wrapped", func(t *testing.T) {
		long := strings.Repeat("a", 30) + " " + strings.Repeat("b", 30) + ".png"
		got := plainRows(glamourTestLines(t, "x ![["+long+"]] y\n", 40))
		if !strings.Contains(got, "⟦image:") || !strings.Contains(got, ".png⟧") {
			t.Errorf("a wrapped placeholder lost its brackets:\n%s", got)
		}
	})
	t.Run("link token", func(t *testing.T) {
		if got := plainRows(glamourTestLines(t, "[[Note|image: x]] and [[Other]]\n", 60)); strings.ContainsAny(got, "⟦⟧") {
			t.Errorf("a link shows brackets:\n%s", got)
		}
	})
	t.Run("code", func(t *testing.T) {
		if got := plainRows(glamourTestLines(t, "`![[pic.png]]`\n", 60)); !strings.Contains(got, "![[pic.png]]") || strings.Contains(got, "⟦") {
			t.Errorf("code changed:\n%s", got)
		}
	})
}

// A task row's timed due: token and its @-fields carry the task-metadata
// colour as whole tokens. A bare repeat:, skip: or until: is plain text.
func TestPreviewStylesFieldsAndTimedDue(t *testing.T) {
	a := previewMetaApp()
	row := previewMetaRow(t, a, "- [ ] Call due:2026-11-03T14:00 @repeat:2w-mon/thu @skip:2027-03-13/2027-03-21 @until:2027-12-01 end\n")
	for _, token := range []string{"due:2026-11-03T14:00", "@repeat:2w-mon/thu", "@skip:2027-03-13/2027-03-21", "@until:2027-12-01"} {
		if !strings.Contains(row, previewMetaStyle(a, token)) {
			t.Errorf("%q is not styled as one task-metadata token in %q", token, row)
		}
	}
	row = previewMetaRow(t, a, "- [ ] Call repeat:weekly skip:2027-03-13 until:2027-12-01 end\n")
	for _, token := range []string{"repeat:weekly", "skip:2027-03-13", "until:2027-12-01"} {
		if strings.Contains(row, previewMetaStyle(a, token)) {
			t.Errorf("%q is styled in %q", token, row)
		}
	}
}

func previewMetaApp() *App {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	a.prefs.PreviewStyle = "dark"
	return a
}

func previewMetaStyle(a *App, token string) string {
	return lipgloss.NewStyle().Foreground(a.theme.Purple).Render(token)
}

// previewMetaRow renders markdown with a true-colour profile and gives the
// first row.
func previewMetaRow(t *testing.T, a *App, markdown string) string {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lines, err := a.renderGlamourLines(markdown, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("no rows rendered")
	}
	return lines[0].text
}

func TestPreviewStylesEventTokens(t *testing.T) {
	a := previewMetaApp()
	row := previewMetaRow(t, a, "- Lecture when:2026-11-03T14:00-15:30 @repeat:weekly @until:2026-12-14 end\n")
	for _, token := range []string{"when:2026-11-03T14:00-15:30", "@repeat:weekly", "@until:2026-12-14"} {
		if !strings.Contains(row, previewMetaStyle(a, token)) {
			t.Errorf("%q is not styled in %q", token, row)
		}
	}
	row = previewMetaRow(t, a, "- Range when:2027-03-13/2027-03-21 @skip:Spring-Break end\n")
	for _, token := range []string{"when:2027-03-13/2027-03-21", "@skip:Spring-Break"} {
		if !strings.Contains(row, previewMetaStyle(a, token)) {
			t.Errorf("%q is not styled in %q", token, row)
		}
	}
	row = previewMetaRow(t, a, "- Work when:2026-11-03T09:00 task:[[Work#^ab12cd]] end\n")
	if !strings.Contains(row, previewMetaStyle(a, "when:2026-11-03T09:00")) {
		t.Errorf("when: is not styled in %q", row)
	}
	if !strings.Contains(row, previewMetaStyle(a, "task:Work")) {
		t.Errorf("the task link is not styled in %q", row)
	}
	row = previewMetaRow(t, a, "- ask them when: tomorrow or when:soon or when:2026-11-03x end\n")
	for _, token := range []string{"when:", "when:soon", "when:2026-11-03x"} {
		if strings.Contains(row, previewMetaStyle(a, token)) {
			t.Errorf("%q is styled in %q", token, row)
		}
	}
}

// A checkbox line with scheduled: keeps the style that v0.17.1 gives it.
func TestPreviewKeepsScheduledStyled(t *testing.T) {
	a := previewMetaApp()
	row := previewMetaRow(t, a, "- [ ] Draft scheduled:2026-11-05T09:30 the report\n")
	if !strings.Contains(row, previewMetaStyle(a, "scheduled:2026-11-05T09:30")) {
		t.Errorf("scheduled: is not styled in %q", row)
	}
}
