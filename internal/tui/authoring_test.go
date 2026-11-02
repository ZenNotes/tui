package tui

import (
	"github.com/ZenNotes/tui/internal/vim"
	"strings"
	"testing"
)

func TestTableHelpersPreserveEscapedPipesAndOutsideText(t *testing.T) {
	body := "# Note\n\n| Name | Value |\n| --- | --- |\n| a\\|b | 10 |\n\nAfter\n"
	got, err := editMarkdownTable(body, 4, 3, "column-right")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "| a\\|b |  | 10 |") || !strings.HasSuffix(got, "\n\nAfter\n") {
		t.Fatal(got)
	}
	got, err = editMarkdownTable(got, 4, 3, "align-center")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "| :---: |") {
		t.Fatal(got)
	}
	if _, err = editMarkdownTable("```md\n"+body+"```", 5, 3, "row-below"); err == nil {
		t.Fatal("table inside fence was edited")
	}
}
func TestSnippetRespectsPreferenceAndExistingPairs(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.openNote("note.md", true)
	buf := a.activeBuffer()
	buf.ed.ReplaceText("**")
	buf.ed.HandleKey(vim.R('A'))
	a.handleKey(vim.R(' '))
	if buf.ed.Text() != "****" {
		t.Fatalf("snippet: %q", buf.ed.Text())
	}
	a.prefs.MarkdownSnippets = false
	buf.ed.ReplaceText("**")
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 2})
	a.handleKey(vim.R(' '))
	if buf.ed.Text() != "** " {
		t.Fatalf("disabled snippet: %q", buf.ed.Text())
	}
}

func TestFrontmatterTagCompletionPreservesSequenceAndUndo(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.openNote("note.md", true)
	a.idx.tags = []tagCount{{tag: "bar"}, {tag: "baz"}}
	buf := a.activeBuffer()
	body := "---\ntags: [foo, ba]\n---\nText"
	buf.ed.ReplaceText(body)
	buf.ed.SetCursor(vim.Pos{Line: 1, Col: 14})
	a.markdownCompletion()
	p, ok := a.overlay.(*palette)
	if !ok {
		t.Fatal("no tag completion")
	}
	p.onSelect(a, paletteItem{id: "bar"})
	if buf.ed.Line(1) != "tags: [foo, bar]" {
		t.Fatalf("frontmatter damaged: %s", buf.ed.Text())
	}
	buf.ed.HandleKey(vim.R('u'))
	if buf.ed.Text() != body {
		t.Fatalf("completion undo did not restore note: %s", buf.ed.Text())
	}
}

func TestHelpersRespectLongCodeFences(t *testing.T) {
	lines := []string{"````markdown", "```", "**", "````"}
	if !inMarkdownFence(lines, 2) {
		t.Fatal("short delimiter closed the longer code fence")
	}
	if inMarkdownFence(lines, 4) {
		t.Fatal("closing fence left helper disabled")
	}
}

func TestTypedBacktickFenceClosesOnEnter(t *testing.T) {
	a, buf := newTagTestApp(t, vimPrefs(), "# Draft")
	feed(a, vim.R('o'))
	typeKeys(a, "```go")
	if got := buf.ed.Line(1); got != "```go" {
		t.Fatalf("typed fence %q", got)
	}
	feed(a, vim.KeyEnter)
	if got := buf.ed.Text(); got != "# Draft\n```go\n\n```" {
		t.Fatalf("Enter after the fence: %q", got)
	}
	if got := buf.ed.Cursor(); got != (vim.Pos{Line: 2, Col: 0}) {
		t.Fatalf("cursor should sit inside the block: %v", got)
	}
}

// typeLineAndEnter puts line in the note, moves to its end in insert mode
// and presses Enter through the App, as a user typing it would.
func typeLineAndEnter(t *testing.T, math, line string) *noteBuffer {
	t.Helper()
	a, _ := newFeatureTestApp(t)
	a.prefs.TerminalMath = math
	a.openNote("note.md", true)
	buf := a.activeBuffer()
	buf.ed.ReplaceText(line)
	buf.ed.HandleKey(vim.R('A'))
	a.handleKey(vim.Key{Name: "enter"})
	return buf
}

func assertSnippet(t *testing.T, buf *noteBuffer, want string) {
	t.Helper()
	if got := buf.ed.Text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if cur := buf.ed.Cursor(); cur.Line != 1 || cur.Col != 0 {
		t.Fatalf("cursor = %+v, want line 1 col 0 (between opener and closer)", cur)
	}
}

func TestSnippetBracketClosesUnderLatex(t *testing.T) {
	assertSnippet(t, typeLineAndEnter(t, "latex", `\[`), "\\[\n\n\\]")
}

// As typed: auto-pairing leaves \[] with the cursor before the ],
// and Enter must still give \[ … \] instead of a stray ].
func TestTypedBracketClosesOnEnterUnderLatex(t *testing.T) {
	for _, typed := range []string{`\[`, `\begin{align}`} {
		t.Run(typed, func(t *testing.T) {
			a, buf := newTagTestApp(t, vimPrefs(), "# Draft")
			a.prefs.TerminalMath = "latex"
			feed(a, vim.R('o'))
			typeKeys(a, typed)
			feed(a, vim.KeyEnter)
			closer := `\]`
			if typed != `\[` {
				closer = `\end{align}`
			}
			if got, want := buf.ed.Text(), "# Draft\n"+typed+"\n\n"+closer; got != want {
				t.Fatalf("typed %q + Enter: %q, want %q", typed, got, want)
			}
			if got := buf.ed.Cursor(); got != (vim.Pos{Line: 2, Col: 0}) {
				t.Fatalf("cursor should sit inside the block: %v", got)
			}
		})
	}
}

func TestSnippetBeginClosesAnyEnvironment(t *testing.T) {
	for _, env := range []string{"align", "foo*"} {
		t.Run(env, func(t *testing.T) {
			buf := typeLineAndEnter(t, "latex", `\begin{`+env+`}`)
			assertSnippet(t, buf, `\begin{`+env+"}\n\n"+`\end{`+env+`}`)
		})
	}
}

func TestSnippetDollarUnchangedUnderLatex(t *testing.T) {
	assertSnippet(t, typeLineAndEnter(t, "latex", "$$"), "$$\n\n$$")
}

func TestSnippetTypstUnchangedAndNoBracket(t *testing.T) {
	assertSnippet(t, typeLineAndEnter(t, "typst", "$$"), "$$\n\n$$")
	for _, line := range []string{`\[`, `\begin{align}`} {
		buf := typeLineAndEnter(t, "typst", line)
		if got := buf.ed.Text(); strings.Contains(got, `\]`) || strings.Contains(got, `\end{`) {
			t.Fatalf("typst %q + Enter inserted a closer: %q", line, got)
		}
	}
}

// A closer that already follows is not added a second time.
func TestSnippetLatexSkipsExistingCloser(t *testing.T) {
	for _, body := range []string{"\\[\n\\]", "\\begin{align}\n\\end{align}"} {
		a, _ := newFeatureTestApp(t)
		a.prefs.TerminalMath = "latex"
		a.openNote("note.md", true)
		buf := a.activeBuffer()
		buf.ed.ReplaceText(body)
		buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
		buf.ed.HandleKey(vim.R('A'))
		a.handleKey(vim.Key{Name: "enter"})
		if got := buf.ed.Text(); strings.Count(got, `\]`)+strings.Count(got, `\end{`) != 1 {
			t.Fatalf("%q + Enter duplicated the closer: %q", body, got)
		}
	}
}
