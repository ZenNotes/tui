package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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
