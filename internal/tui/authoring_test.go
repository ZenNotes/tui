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
