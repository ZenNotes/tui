package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/ZenNotes/tui/internal/vim"
)

// previewRows renders a note through the Glamour preview.
func previewRows(t *testing.T, style, text string) []previewLine {
	t.Helper()
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	a.prefs.PreviewStyle = style
	rows, err := a.renderGlamourLines(text, 70)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func previewLinks(rows []previewLine) []string {
	out := []string{}
	for _, r := range rows {
		for _, l := range r.links {
			out = append(out, l.target)
		}
	}
	return out
}

// Code shows its exact source in the preview, whatever it holds, and offers
// no links; what sits around it still renders and can be followed.
func TestPreviewKeepsCodeLiteral(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		want  []string // each on one rendered row
		not   []string
		links []string
	}{
		{
			name:  "inline wikilink",
			src:   "Use `[[Bookmarks|bookmarks]]` here and [[Real|alias]].",
			want:  []string{"[[Bookmarks|bookmarks]]", "alias."},
			not:   []string{"[[Real"},
			links: []string{"Real"},
		},
		{
			name: "inline embeds",
			src:  "`![[pic.png]]` and `![alt](b.png)`",
			want: []string{"![[pic.png]]", "![alt](b.png)"},
			not:  []string{"image:"},
		},
		{
			name: "inline post-styled text",
			src:  "`a ⟦x⟧ #tag @me` end",
			want: []string{"a ⟦x⟧ #tag @me"},
		},
		{
			name: "code span across lines",
			src:  "`multi\n[[Note|alias]] line`",
			want: []string{"[[Note|alias]] line"},
		},
		{
			name: "fence",
			src:  "```\n[[Note|alias]]\n![[pic.png]]\n![alt](b.png)\n- [/] going\n- [-] dropped\n- [>] moved\n> [!tip] Title\n#tag @me due:2026 ⟦x⟧\n• ◩ glyph\nhttps://example.com\n```",
			want: []string{"[[Note|alias]]", "![[pic.png]]", "![alt](b.png)", "- [/] going", "- [-] dropped", "- [>] moved", "> [!tip] Title", "#tag @me due:2026 ⟦x⟧", "• ◩ glyph", "https://example.com"},
		},
		{
			name: "tilde fence",
			src:  "~~~\n[[Note|alias]]\n~~~",
			want: []string{"[[Note|alias]]"},
		},
		{
			name: "fence inside a longer fence",
			src:  "````\n```\n[[Note|alias]]\n```\n````",
			want: []string{"```", "[[Note|alias]]"},
		},
		{
			name: "tildes inside a backtick fence",
			src:  "```\n~~~\n[[Note|alias]]\n~~~\n```",
			want: []string{"~~~", "[[Note|alias]]"},
		},
		{
			name:  "fence in a quote",
			src:   "> ```\n> [[Note|alias]]\n> ```\n> outside [[Real|alias]]",
			want:  []string{"[[Note|alias]]", "outside alias"},
			links: []string{"Real"},
		},
		{
			name:  "fence in a list item",
			src:   "- item\n  ```\n  [[Note|alias]]\n  ```\n- `[[A|b]]` and [[C|d]]",
			want:  []string{"[[Note|alias]]", "[[A|b]]"},
			links: []string{"C"},
		},
		{
			name: "fence opening a list item",
			src:  "- ```\n  [[Note|alias]]\n  ```",
			want: []string{"[[Note|alias]]"},
		},
		{
			name: "indented code",
			src:  "    [[Note|alias]]",
			want: []string{"[[Note|alias]]"},
		},
		{
			name: "indented embed",
			src:  "    ![[pic.png]]",
			want: []string{"![[pic.png]]"},
		},
		{
			name:  "pipes in a table's code",
			src:   "| syntax | link |\n| - | - |\n| `[[Note|alias]]` | [[C|d]] |",
			want:  []string{"[[Note|alias]]", "│ d"},
			links: []string{"C"},
		},
	}
	for _, style := range []string{"auto", "dark"} {
		for _, c := range cases {
			t.Run(style+"/"+c.name, func(t *testing.T) {
				rows := previewRows(t, style, c.src)
				plain := make([]string, len(rows))
				for i, r := range rows {
					plain[i] = ansi.Strip(r.text)
				}
				all := strings.Join(plain, "\n")
				for _, want := range c.want {
					if !strings.Contains(all, want) {
						t.Errorf("missing %q in:\n%s", want, all)
					}
				}
				for _, not := range c.not {
					if strings.Contains(all, not) {
						t.Errorf("unexpected %q in:\n%s", not, all)
					}
				}
				if strings.ContainsAny(all, "﷐﷑﷒﷓﷔﷕﷖") {
					t.Errorf("a code guard leaked into:\n%q", all)
				}
				if got, want := strings.Join(previewLinks(rows), ","), strings.Join(c.links, ","); got != want {
					t.Errorf("links %q, want %q", got, want)
				}
			})
		}
	}
}

// The default style has no background for inline code, so the padding of
// Glamour's base style is only stray spaces around it.
func TestPreviewInlineCodeHasNoStraySpaces(t *testing.T) {
	rows := previewRows(t, "auto", "Use `[[Bookmarks|bookmarks]]` here")
	if plain := ansi.Strip(rows[0].text); !strings.HasPrefix(plain, "Use [[Bookmarks|bookmarks]] here") {
		t.Fatalf("row %q", plain)
	}
}

// Links outside code keep working where code sits close by, and a backtick
// that opens no code span leaves them alone.
func TestPreviewLinksBesideCode(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"escaped backticks", "\\`[[Note|alias]]\\`", "`alias`"},
		{"one-line triple backticks", "```inline``` and [[Note|alias]] after", "and alias after"},
		{"unmatched backtick", "a ` b [[Note|alias]]", "b alias"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := previewRows(t, "auto", c.src)
			if plain := ansi.Strip(rows[0].text); !strings.Contains(plain, c.want) {
				t.Fatalf("row %q, want %q", plain, c.want)
			}
			if got := strings.Join(previewLinks(rows), ","); got != "Note" {
				t.Fatalf("links %q", got)
			}
		})
	}
}

// A link written around a code span is still a link, with its whole label.
func TestPreviewLinksAroundCodeSpans(t *testing.T) {
	rows := previewRows(t, "auto", "[`name`](https://example.com/a) and [[`Odd` Title]]")
	got := []string{}
	for _, l := range rows[0].links {
		got = append(got, l.target+"="+l.alias)
	}
	if want := "`Odd` Title=,https://example.com/a=`name`"; strings.Join(got, ",") != want {
		t.Fatalf("links %q, want %q", strings.Join(got, ","), want)
	}
}

// Guards take a cell each like what they replace, so code wraps where it would.
func TestPreviewCodeWrapsLikePlainCode(t *testing.T) {
	marks := strings.Repeat("a#b@c!d:e[f ", 12)
	plain := strings.Repeat("axbxcxdxexf ", 12)
	got := previewRows(t, "auto", "`"+marks+"`")
	want := previewRows(t, "auto", "`"+plain+"`")
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d", len(got), len(want))
	}
	for i := range got {
		if a, b := ansi.StringWidth(got[i].text), ansi.StringWidth(want[i].text); a != b {
			t.Fatalf("row %d: %d cells, want %d", i, a, b)
		}
	}
}

// A row that looks like a table but has no delimiter row is a paragraph, so a
// pipe in its code must not pick up a backslash.
func TestPreviewLoneTableRowKeepsCodePipe(t *testing.T) {
	rows := previewRows(t, "auto", "| `a|b` |")
	if plain := ansi.Strip(rows[0].text); !strings.Contains(plain, "a|b") {
		t.Fatalf("row %q", plain)
	}
}

// Code is not given the colors of tags, task metadata or links.
func TestPreviewDoesNotStyleCodeAsMarkup(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	th := buildTheme(true)
	for _, src := range []string{"```\n#tag\n```", "`a #tag b`"} {
		rows := previewRows(t, "auto", src)
		if tag := th.Tag.Render("#tag"); strings.Contains(rows[0].text, tag) {
			t.Fatalf("%q was styled as a tag: %q", src, rows[0].text)
		}
	}
	if tag := th.Tag.Render("#real"); !strings.Contains(previewRows(t, "auto", "`a` #real")[0].text, tag) {
		t.Fatal("a tag beside code should keep its style")
	}
}

// A task written inside code is not a task: toggling it would edit the code.
func TestPreviewCodeHoldsNoTasks(t *testing.T) {
	rows := previewRows(t, "auto", "- item\n  ```\n  - [ ] sample\n  ```\n- [ ] real")
	found := false
	for _, r := range rows {
		plain := ansi.Strip(r.text)
		if strings.Contains(plain, "sample") && r.task {
			t.Fatalf("code row is a task: %q", plain)
		}
		if strings.Contains(plain, "real") {
			found = true
			if !r.task || r.srcLine != 4 {
				t.Fatalf("real task row: task=%v srcLine=%d", r.task, r.srcLine)
			}
		}
	}
	if !found {
		t.Fatal("real task row missing")
	}
}

// Each block is parsed alone, so content indented under a list item must stay
// with its list or lose its meaning to an indented code block.
func TestPreviewListContentAcrossBlankLines(t *testing.T) {
	for _, c := range []struct {
		name, src, want string
		links           string
	}{
		{"link in a paragraph", "1. Step one\n\n    See [[Real|alias]] for more", "See alias for more", "Real"},
		{"link with two spaces", "- item\n\n  See [[Real|alias]] for more", "See alias for more", "Real"},
		{"link on a later item's paragraph", "- a\n- b\n\n    See [[Real|alias]]\n- c [[C|d]]", "See alias", "Real,C"},
		{"tab indent", "- item\n\n\tSee [[Real|alias]]", "See alias", "Real"},
		{"text after a standalone math line", "1. item\n\n    $$x^2$$\n\n    See [[Real|alias]]", "See alias", "Real"},
		{"code after a paragraph", "- item\n\n  See [[Real|alias]]\n\n      [[Code|c]]", "[[Code|c]]", "Real"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := previewRows(t, "auto", c.src)
			all := []string{}
			for _, r := range rows {
				all = append(all, ansi.Strip(r.text))
			}
			if joined := strings.Join(all, "\n"); !strings.Contains(joined, c.want) {
				t.Fatalf("missing %q in:\n%s", c.want, joined)
			}
			if got := strings.Join(previewLinks(rows), ","); got != c.links {
				t.Fatalf("links %q, want %q", got, c.links)
			}
		})
	}
}

// A paragraph under a list item is its own row: its links are followed from
// where they are drawn, and the row leads back to its own source line.
func TestPreviewItemParagraphRowsKeepTheirLinesAndLinks(t *testing.T) {
	for _, c := range []struct {
		name, src, row string
		line           int
		links          string
	}{
		{"bullet", "- item\n\n  para [[Real]] here", "para Real here", 2, "Real"},
		{"ordered", "1. Step one\n\n    See [[Real|alias]]", "See alias", 2, "Real"},
		{"second paragraph", "- item\n\n  first [[A]]\n\n  second [[B]]", "second B", 4, "B"},
		{"line after a paragraph's first", "- item\n\n  first\n  more [[A]]", "more A", 3, "A"},
		{"continuation with no blank line", "- item\n  more [[A]]", "more A", 1, "A"},
		{"on a later item", "- a\n- b\n\n  para [[Real]]\n- c", "para Real", 3, "Real"},
		{"after a fence", "- a\n  ```\n  x\n  ```\n\n  para [[Real]]", "para Real", 5, "Real"},
		{"starts with its link", "- a\n\n  [[Real|alias]] first", "alias first", 2, "Real"},
		{"starts with emphasis", "- a\n\n  **\"b\"** [[Real]]", "Real", 0, "Real"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := previewRows(t, "auto", c.src)
			for _, r := range rows {
				if !strings.Contains(ansi.Strip(r.text), c.row) {
					continue
				}
				if c.name != "starts with emphasis" && r.srcLine != c.line {
					t.Fatalf("row %q on line %d, want %d", c.row, r.srcLine, c.line)
				}
				got := []string{}
				for _, l := range r.links {
					got = append(got, l.target)
				}
				if c.name != "starts with emphasis" && strings.Join(got, ",") != c.links {
					t.Fatalf("row %q has links %q, want %q", c.row, got, c.links)
				}
				if plain := previewPlain(t, c.src); strings.Contains(plain, `**`) {
					t.Fatalf("emphasis broken:\n%s", plain)
				}
				if got := strings.Join(previewLinks(rows), ","); !strings.Contains(got, c.links) {
					t.Fatalf("links %q lost %q", got, c.links)
				}
				return
			}
			t.Fatalf("no row %q in:\n%s", c.row, previewPlain(t, c.src))
		})
	}
}

// GotoLine counts from one and a row's source line from zero.
func TestPreviewRowLeadsToItsOwnEditorLine(t *testing.T) {
	src := "- item\n\n  para here\n\nlast"
	rows := previewRows(t, "auto", src)
	ed := vim.New(src, vim.Options{}, vim.Hooks{})
	for _, want := range []struct {
		row  string
		text string
	}{{"para here", "  para here"}, {"item", "- item"}, {"last", "last"}} {
		found := false
		for _, r := range rows {
			if strings.Contains(ansi.Strip(r.text), want.row) {
				ed.GotoLine(r.editorLine())
				if got := ed.Line(ed.Cursor().Line); got != want.text {
					t.Fatalf("row %q leads to %q", want.row, got)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no row %q", want.row)
		}
	}
}

// x on a task item's paragraph still toggles the item, the row only leads to
// its own line for everything else.
func TestPreviewTaskParagraphRowTogglesItsItem(t *testing.T) {
	rows := previewRows(t, "auto", "- [ ] task\n\n  note\n- [ ] other")
	for _, r := range rows {
		if !strings.Contains(ansi.Strip(r.text), "note") {
			continue
		}
		if r.srcLine != 2 || !r.task || r.taskLine() != 0 {
			t.Fatalf("srcLine=%d task=%v taskLine=%d", r.srcLine, r.task, r.taskLine())
		}
		return
	}
	t.Fatal("no note row")
}

// A blank line before a quote under an item stays a blank row.
func TestPreviewItemKeepsItsBlankRowBeforeAQuote(t *testing.T) {
	rows := []string{}
	for _, r := range previewRows(t, "auto", "- item\n\n  > q") {
		rows = append(rows, strings.TrimSpace(ansi.Strip(r.text)))
	}
	if len(rows) < 3 || rows[0] != "• item" || rows[1] != "" || !strings.Contains(strings.Join(rows, "\n"), "q") {
		t.Fatalf("rows %q", rows)
	}
}

// Glamour runs a list item's paragraphs together unless they are kept apart.
func TestPreviewItemParagraphsStayApart(t *testing.T) {
	for _, c := range []struct {
		name, src string
		want      []string
	}{
		{"ordered", "1. Step one\n\n    Then two", []string{"1. Step one", "", "Then two"}},
		{"bullet", "- Step one\n\n  Then two", []string{"• Step one", "", "Then two"}},
		{"three paragraphs", "- one\n\n  two\n\n  three", []string{"• one", "", "two", "", "three"}},
		{"whitespace on the blank line", "- one\n  \n  two", []string{"• one", "", "two"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := []string{}
			for _, r := range previewRows(t, "auto", c.src) {
				got = append(got, strings.TrimSpace(ansi.Strip(r.text)))
			}
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("rows %q, want %q", got, c.want)
			}
		})
	}
	// A blank line inside a fence is the code's own and stays blank.
	src := "- a\n  ```\n  x\n\n  y\n  ```"
	if got := markItemContent(src); got != src {
		t.Fatalf("got %q", got)
	}
}

func TestPreviewEmbedUnderAListItemKeepsItsCard(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"Index.md": "x", "Other.md": "embedded body"})
	a.prefs.PreviewStyle = "auto"
	rows, err := a.renderGlamourLinesFrom("- item\n\n    ![[Other]]\n", 70, "Index.md")
	if err != nil {
		t.Fatal(err)
	}
	all := []string{}
	for _, r := range rows {
		all = append(all, ansi.Strip(r.text))
	}
	if joined := strings.Join(all, "\n"); !strings.Contains(joined, "embedded body") {
		t.Fatalf("embed not transcluded:\n%s", joined)
	}
}

func TestPreviewNestedTaskAfterABlankLineToggles(t *testing.T) {
	rows := previewRows(t, "auto", "- top\n\n    - [ ] nested task")
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r.text), "nested task") {
			if !r.task || r.srcLine != 2 {
				t.Fatalf("task=%v srcLine=%d", r.task, r.srcLine)
			}
			return
		}
	}
	t.Fatal("nested task row missing")
}

// Rows are matched to list items by their markers, so a code line that looks
// like an item must not push the rows after it onto the wrong source line.
func TestPreviewTaskRowsStayOnTheirLinesPastCode(t *testing.T) {
	for _, code := range []string{"[x] note", "- dash", "1. one", "☐ box", "-", "* star"} {
		t.Run(code, func(t *testing.T) {
			rows := previewRows(t, "auto", "- a\n  ```\n  "+code+"\n  ```\n- [ ] b\n- [ ] c\n- [ ] d")
			want := map[string]int{"b": 4, "c": 5, "d": 6}
			seen := 0
			for _, r := range rows {
				plain := strings.TrimSpace(ansi.Strip(r.text))
				if strings.HasPrefix(plain, "☐ ") && code != "☐ box" || strings.HasPrefix(plain, "☐ ") && plain != "☐ box" {
					line, ok := want[strings.TrimPrefix(plain, "☐ ")]
					if !ok {
						t.Fatalf("unexpected row %q", plain)
					}
					seen++
					if r.srcLine != line || !r.task {
						t.Fatalf("%q: srcLine=%d task=%v, want line %d", plain, r.srcLine, r.task, line)
					}
				}
				if plain == code && r.srcLine != 0 {
					t.Fatalf("code row %q mapped to line %d, want its item's line 0", plain, r.srcLine)
				}
			}
			if seen != 3 {
				t.Fatalf("saw %d task rows", seen)
			}
		})
	}
}

// A blank line inside a fence is still inside the fence.
func TestPreviewBlankLineInAFenceInAList(t *testing.T) {
	src := "- item\n  ```\n  [[A|b]]\n\n  [[C|d]]\n  ```\n- after [[R|r]]"
	blocks := splitBlocks(strings.Split(src, "\n"), false)
	if len(blocks) != 1 || blocks[0].kind != "list" {
		t.Fatalf("blocks %+v", blocks)
	}
	rows := previewRows(t, "auto", src)
	all := []string{}
	for _, r := range rows {
		all = append(all, ansi.Strip(r.text))
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{"[[A|b]]", "[[C|d]]", "after r"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if got := strings.Join(previewLinks(rows), ","); got != "R" {
		t.Fatalf("links %q", got)
	}
	// An unclosed fence ends with its list item.
	blocks = splitBlocks(strings.Split("- a\n  ```\n  x\n\nafter", "\n"), false)
	if blocks[len(blocks)-1].kind != "para" {
		t.Fatalf("blocks %+v", blocks)
	}
}

// Glamour draws a table wherever goldmark finds one, not only for a block of
// rows with outer pipes.
func TestPreviewTableShapesKeepCodeWhole(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"outer pipes", "| a | b |\n|---|---|\n| `[[N|al]]` | z |"},
		{"in a quote", "> | a | b |\n> |---|---|\n> | `[[N|al]]` | z |"},
		{"in a list item", "- | a | b |\n  |---|---|\n  | `[[N|al]]` | z |"},
		{"no outer pipes", "a | b\n--|--\n`[[N|al]]` | z"},
		{"below a preamble row", "| x |\n| a | b |\n|---|---|\n| `[[N|al]]` | r |"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := previewRows(t, "auto", c.src)
			all := []string{}
			for _, r := range rows {
				all = append(all, ansi.Strip(r.text))
			}
			if joined := strings.Join(all, "\n"); !strings.Contains(joined, "[[N|al]]") {
				t.Fatalf("code split:\n%s", joined)
			}
			if got := previewLinks(rows); len(got) != 0 {
				t.Fatalf("links %v", got)
			}
		})
	}
}

// Pipes are only escaped in what goldmark reads as a code span.
func TestEscapeCodePipes(t *testing.T) {
	for _, c := range []struct{ row, want string }{
		{"| `a|b` | c |", "| `a\\|b` | c |"},
		{"| `a\\|b` | c |", "| `a\\|b` | c |"},
		{"| \\`a|b\\` | c |", "| \\`a|b\\` | c |"},
		{"| ``a|`b`` | c |", "| ``a\\|`b`` | c |"},
		{"| `a\\` | `b|c` |", "| `a\\` | `b\\|c` |"},
		{"| `a|b | c |", "| `a|b | c |"},
	} {
		if got := escapeCodePipes(c.row); got != c.want {
			t.Errorf("%q: got %q, want %q", c.row, got, c.want)
		}
	}
}

// A table's rows look the same inside a fence, where a pipe is only a pipe.
func TestPreviewTableInAFenceKeepsItsPipes(t *testing.T) {
	src := "```\n| a | b |\n|---|---|\n| `x|y` | z |\n```"
	rows := previewRows(t, "auto", src)
	all := []string{}
	for _, r := range rows {
		all = append(all, ansi.Strip(r.text))
	}
	if joined := strings.Join(all, "\n"); !strings.Contains(joined, "| `x|y` | z |") {
		t.Fatalf("fence changed:\n%s", joined)
	}
}

// Link targets and labels come from the source, code spans included, and a
// link that ends in code is not a link.
func TestPreviewLinkTextIsNotBlanked(t *testing.T) {
	rows := previewRows(t, "auto", "[`a:b`](https://example.com/x) and [[`c:d` e]]")
	got := []string{}
	for _, l := range rows[0].links {
		got = append(got, l.target+"="+l.alias)
	}
	if want := "`c:d` e=,https://example.com/x=`a:b`"; strings.Join(got, ",") != want {
		t.Fatalf("links %q, want %q", strings.Join(got, ","), want)
	}
	rows = previewRows(t, "auto", "see [[foo `bar]]` baz")
	if plain := ansi.Strip(rows[0].text); !strings.Contains(plain, "bar]]") {
		t.Fatalf("row %q lost its brackets", plain)
	}
	if links := previewLinks(rows); len(links) != 0 {
		t.Fatalf("links %v", links)
	}
}

// PR 39's token handling strips brackets after postStyleRow, so guards have to
// outlast every row step: a row's code comes back only at the end of mapRows.
func TestPostStyleRowLeavesGuardsInPlace(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	if got := a.postStyleRow(guardToken + "x" + guardToken); got != guardToken+"x"+guardToken {
		t.Fatalf("got %q", got)
	}
	for _, src := range []string{"`⟦x⟧`", "```\n⟦x⟧\n```", "- a\n  ```\n  ⟦x⟧\n  ```"} {
		if plain := previewPlain(t, src); !strings.Contains(plain, "⟦x⟧") {
			t.Fatalf("%q: %q", src, plain)
		}
	}
}

func previewPlain(t *testing.T, src string) string {
	t.Helper()
	all := []string{}
	for _, r := range previewRows(t, "auto", src) {
		all = append(all, ansi.Strip(r.text))
	}
	return strings.Join(all, "\n")
}

// A tab indents past a fence, so that line opens an indented code block, and a
// diagram written that way is not drawn.
func TestFenceIndentIsSpacesOnly(t *testing.T) {
	if isFenceOpen("\t```") || isFenceOpen("    ```") {
		t.Fatal("an indented line is not a fence")
	}
	if !isFenceOpen("   ```") || !fenceCloses("   ```", '`', 3) {
		t.Fatal("three spaces is still a fence")
	}
	if fenceCloses("\t```", '`', 3) {
		t.Fatal("a tab-indented line does not close a fence")
	}
	if plain := previewPlain(t, "\t```mermaid\n\tgraph TD\n\t```"); !strings.Contains(plain, "graph TD") {
		t.Fatalf("drawn as a diagram: %q", plain)
	}
}

func TestSplitBlocksFollowsFenceRules(t *testing.T) {
	kinds := func(src string) string {
		out := []string{}
		for _, b := range splitBlocks(strings.Split(src, "\n"), false) {
			out = append(out, b.kind)
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct{ src, want string }{
		{"````\n```\nx\n```\n````", "fence"},
		{"```\n~~~\nx\n~~~\n```", "fence"},
		{"~~~\n```\nx\n```\n~~~", "fence"},
		{"```\nx\n``` not a closer\n```", "fence"},
		{"```inline``` and text\nmore", "para"},
		{"before\n```inline``` text", "para"},
		{"before\n```\nx\n```", "para fence"},
		{"```\nunclosed", "fence"},
		{"~~~ a`b\nx\n~~~", "fence"},
	} {
		if got := kinds(c.src); got != c.want {
			t.Errorf("%q: kinds %q, want %q", c.src, got, c.want)
		}
	}
}
