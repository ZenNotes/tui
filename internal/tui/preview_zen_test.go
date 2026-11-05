package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func zenPreviewText(a *App, body string) string {
	rows := []string{}
	for _, line := range a.renderPreviewLines(body, 200) {
		rows = append(rows, strings.TrimRight(ansi.Strip(line.text), " "))
	}
	return strings.Join(rows, "\n")
}

func TestZenPreviewKeepsEmbedsLiteralInCode(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want string }{
		{"wiki embed", "`![[pic.png]]`", "![[pic.png]]"},
		{"wiki embed with alias", "`![[pic.png|small]]`", "![[pic.png|small]]"},
		{"image embed", "`![alt](b.png)`", "![alt](b.png)"},
		{"image embed with title", "`![alt](b.png \"t\")`", "![alt](b.png \"t\")"},
		{"in prose", "use `![[pic.png]]` here", "use ![[pic.png]] here"},
		{"heading", "## `![[pic.png]]`", "![[pic.png]]"},
		{"bullet", "- `![alt](b.png)`", "• ![alt](b.png)"},
		{"ordered", "1. `![[pic.png]]`", "1. ![[pic.png]]"},
		{"task", "- [ ] `![[pic.png]]`", "☐ ![[pic.png]]"},
		{"quote", "> `![[pic.png]]`", "▍ ![[pic.png]]"},
		{"table cell", "| a |\n|---|\n| `![[pic.png]]` |", "│ ![[pic.png]] │"},
		{"table cell with an alias pipe", "| `![[pic.png|small]]` | x |", "│ ![[pic.png|small]] │ x │"},
		{"fence", "```\n![[pic.png]]\n![alt](b.png)\n```", "│ ![[pic.png]]\n│ ![alt](b.png)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := zenPreviewText(a, tc.body)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
			if strings.Contains(got, "⟦image") {
				t.Fatalf("code became a placeholder:\n%s", got)
			}
		})
	}
}

func TestZenPreviewStillShowsEmbedsOutsideCode(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want string }{
		{"wiki embed", "![[pic.png]]", "⟦image: pic.png⟧"},
		{"wiki embed with alias", "![[pic.png|small]]", "⟦image: pic.png⟧"},
		{"image embed", "![alt](b.png)", "⟦image: alt⟧"},
		{"either side of code", "![[a.png]] `![[b.png]]` ![c](d.png)", "⟦image: a.png⟧ ![[b.png]] ⟦image: c⟧"},
		{"after a code span that opens an embed", "`![` then ![c](d.png)", "![ then ⟦image: c⟧"},
		{"after a code span that opens a wiki embed", "`![[` then ![[a.png]]", "![[ then ⟦image: a.png⟧"},
		{"code in the alt text", "![see `x`](b.png)", "⟦image: see x⟧"},
		{"bracket in the alt text's code", "![see `]`](b.png)", "⟦image: see ]⟧"},
		{"lone backtick", "a ` ![[pic.png]]", "a ` ⟦image: pic.png⟧"},
		{"unmatched double run, then a span", "`` ![[a.png]] `b`", "`` ⟦image: a.png⟧ b"},
		{"unmatched double run, then an image", "`` ![a](b.png) `c`", "`` ⟦image: a⟧ c"},
		{"single run against a double run", "` ![[a.png]] ``", "` ⟦image: a.png⟧ ``"},
		{"double run against a single run", "`` ![[a.png]] `", "`` ⟦image: a.png⟧ `"},
		{"escaped backticks", "\\`![[a.png]]\\`", "\\`⟦image: a.png⟧\\`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zenPreviewText(a, tc.body); got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestZenPreviewCodeSpansFollowBacktickRuns(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want string }{
		{"double run", "`` ![[x.png]] ``", "![[x.png]]"},
		{"backtick inside a double run", "`` ` ![[x.png]] ``", "` ![[x.png]]"},
		{"wikilink beside a backtick in a double run", "`` ` [[x]] ``", "` [[x]]"},
		{"backslash ends a span", "`a\\` ![[x.png]]", "a\\ ⟦image: x.png⟧"},
		{"escaped backslash leaves the backtick real", "\\\\`![[x.png]]`", "\\\\![[x.png]]"},
		{"only spaces stay", "a `  ` b", "a    b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zenPreviewText(a, tc.body); got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestZenPreviewFindsLinksPastCodeThatOpensThem(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want, target string }{
		{"wikilink", "`[[` then [[Note]]", "[[ then Note", "Note"},
		{"markdown link", "`[` then [label](https://example.com)", "[ then label", "https://example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := a.renderPreviewLines(tc.body, 200)
			if len(rows) != 1 || ansi.Strip(rows[0].text) != tc.want {
				t.Fatalf("got %#v", rows)
			}
			if len(rows[0].links) != 1 || rows[0].links[0].target != tc.target {
				t.Fatalf("links %#v", rows[0].links)
			}
		})
	}
	if rows := a.renderPreviewLines("`[[Note]]`", 200); len(rows[0].links) != 0 {
		t.Fatalf("code linked: %#v", rows[0].links)
	}
}

func TestZenPreviewFencesCloseOnlyOnTheirOwnMarker(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want string }{
		{
			"tilde line inside a backtick fence",
			"```\n~~~\n![[pic.png]]\n```\n![[out.png]]",
			"┌\n│ ~~~\n│ ![[pic.png]]\n└\n⟦image: out.png⟧",
		},
		{
			"shorter marker inside a long fence",
			"````\n```\n![[pic.png]]\n````\n![[out.png]]",
			"┌\n│ ```\n│ ![[pic.png]]\n└\n⟦image: out.png⟧",
		},
		{
			"marker with text after it does not close",
			"```\n``` not yet\n![[pic.png]]\n```\n![[out.png]]",
			"┌\n│ ``` not yet\n│ ![[pic.png]]\n└\n⟦image: out.png⟧",
		},
		{
			"tab-indented marker does not close",
			"```\n\t```\n![[pic.png]]\n```\n![[out.png]]",
			"┌\n│     ```\n│ ![[pic.png]]\n└\n⟦image: out.png⟧",
		},
		{
			"a code span on its own line is no fence",
			"``` ![[pic.png]] ```\n![[out.png]]",
			"![[pic.png]]\n⟦image: out.png⟧",
		},
		{
			"language label",
			"```go\nx\n```",
			"┌ go\n│ x\n└",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zenPreviewText(a, tc.body); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// Glamour's code guards and its hiding of a note's own token brackets both pass
// over a row, and neither may undo the other.
func TestGlamourKeepsBracketsInCodeBesideTokens(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"code span beside a link", "`⟦x⟧` and ⟦y⟧ and [[Note|an alias]]", "⟦x⟧ and ⟦y⟧ and an alias"},
		{"code span with a hard space", "`a b` and [[Note|an alias]]", "a b and an alias"},
		{"code block", "```\nlet a = ⟦1, 2⟧;\n```", "let a = ⟦1, 2⟧;"},
		{"list item", "- `⟦x⟧` and [[Note|an alias]]", "⟦x⟧ and an alias"},
		{"table cell", "| a | b |\n|---|---|\n| `⟦x⟧` | [[Note|alias]] |", "⟦x⟧"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []string{}
			for _, line := range glamourTestLines(t, tc.body, 80) {
				rows = append(rows, ansi.Strip(line.text))
			}
			all := strings.Join(rows, "\n")
			if !strings.Contains(all, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, all)
			}
			if strings.ContainsAny(all, "﷑﷘\U0010FFF0\U0010FFF1\U0010FFF2") {
				t.Fatalf("a guard reached the row: %q", all)
			}
		})
	}
}

func TestZenPreviewStylesEmbedSourceAsCode(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	runes, _ := a.inlineRunes("`![[pic.png]]`")
	var text strings.Builder
	for _, r := range runes {
		text.WriteRune(r.r)
		if r.class != stCode {
			t.Fatalf("%q has class %d, not code", string(r.r), r.class)
		}
	}
	if text.String() != "![[pic.png]]" {
		t.Fatalf("got %q", text.String())
	}
}

func TestZenCalloutTitleIsInlineText(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want string }{
		{"embed", "> [!note] ![[a.png]]", "▍ NOTE · ⟦image: a.png⟧"},
		{"code", "> [!tip] use `this` here", "▍ TIP · use this here"},
		{"embed in code", "> [!tip] `![[a.png]]`", "▍ TIP · ![[a.png]]"},
		{"plain", "> [!warning] Careful", "▍ WARNING · Careful"},
		{"no title", "> [!info]", "▍ INFO"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zenPreviewText(a, tc.body); !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	lines := a.renderPreviewLines("> [!note] see [[Other]]", 60)
	if len(lines[0].links) != 1 || lines[0].links[0].target != "Other" {
		t.Errorf("the title's link is not on its row: %v", lines[0].links)
	}
}

func TestZenCodeInsideBoldAndLinks(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	for _, tc := range []struct{ name, body, want string }{
		{"bold around code", "**`![[a.png]]`**", "![[a.png]]"},
		{"bold around code and text", "**a `b` c**", "a b c"},
		{"emphasis around code", "*`x`*", "x"},
		{"strike around code", "~~`x`~~", "x"},
		{"bold around a link", "**[[Note]]**", "Note"},
		{"link with code", "[`x`](https://e.com) y", "x y"},
		{"link with code and text", "[see `x` now](https://e.com)", "see x now"},
		{"a star in code stays", "`a**b` and `c**d`", "a**b and c**d"},
		{"code past the markers", "**a `b** c`", "**a b** c"},
		{"plain bold", "**a** b", "a b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zenPreviewText(a, tc.body); got != tc.want+"" && !strings.HasPrefix(got, tc.want+"\n") {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	lines := a.renderPreviewLines("[`x`](https://e.com)", 60)
	if len(lines[0].links) != 1 || lines[0].links[0].target != "https://e.com" {
		t.Errorf("the link is not on its row: %v", lines[0].links)
	}
}

// The default style reads these the same way, and a cell is as wide as its text.
func TestZenTableCellEmbedIsNotCutBySourceWidth(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	if got := zenPreviewText(a, "| ![[x.png]] | b |\n|---|---|\n| 1 | 2 |"); !strings.Contains(got, "⟦image: x.png⟧") {
		t.Errorf("got %q", got)
	}
}

