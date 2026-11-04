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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zenPreviewText(a, tc.body); got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
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
