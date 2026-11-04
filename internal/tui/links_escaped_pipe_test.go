package tui

import (
	"strings"
	"testing"
)

const escapedPipeTable = "| Note | Value |\n| --- | --- |\n| [[C\\|d]] | x |\n"

func TestSplitWikiReadsAnEscapedPipe(t *testing.T) {
	for _, c := range []struct{ content, target, heading, alias string }{
		{`C\|d`, "C", "", "d"},
		{`C#Heading\|d`, "C", "Heading", "d"},
		{`C\|`, "C", "", ""},
		{`C|d`, "C", "", "d"},
		{`C#Heading|d`, "C", "Heading", "d"},
		{`C`, "C", "", ""},
	} {
		target, heading, alias := splitWiki(c.content)
		if target != c.target || heading != c.heading || alias != c.alias {
			t.Errorf("%q split to %q %q %q, want %q %q %q", c.content, target, heading, alias, c.target, c.heading, c.alias)
		}
	}
}

func TestWikilinkTargetOfReadsAnEscapedPipe(t *testing.T) {
	for _, c := range []struct{ content, target, alias string }{
		{`C\|d`, "C", "d"},
		{`C#Heading\|d`, "C#Heading", "d"},
		{`C^block\|d`, "C^block", "d"},
		{`C|d`, "C", "d"},
		{`C#Heading|d`, "C#Heading", "d"},
		{`C`, "C", ""},
	} {
		got := wikilinkTargetOf(c.content)
		if got.kind != "wikilink" || got.target != c.target || got.alias != c.alias {
			t.Errorf("%q gave %+v, want target %q alias %q", c.content, got, c.target, c.alias)
		}
	}
}

// glamourLink is the one link the rendered preview carries for text.
func glamourLink(t *testing.T, a *App, text string) linkTarget {
	t.Helper()
	lines, err := a.renderGlamourLines(text, 60)
	if err != nil {
		t.Fatal(err)
	}
	found := []linkTarget{}
	for _, l := range lines {
		found = append(found, l.links...)
	}
	if len(found) != 1 {
		t.Fatalf("links %+v, want one", found)
	}
	return found[0]
}

func TestGlamourPreviewReadsAnEscapedPipeInATable(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	got := glamourLink(t, a, escapedPipeTable)
	if got.kind != "wikilink" || got.target != "C" || got.alias != "d" {
		t.Fatalf("link %+v, want wikilink C with alias d", got)
	}
}

func TestPreviewShowsNoBackslashFromAnEscapedPipe(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	for _, c := range []struct{ name, row, shown string }{
		{"alias", `| [[C\|d]] | x |`, "d"},
		{"empty alias", `| [[C\|]] | x |`, "C"},
		{"embed", `| ![[C\|d]] | x |`, "image: C"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lines, err := a.renderGlamourLines("| Note | Value |\n| --- | --- |\n"+c.row+"\n", 60)
			if err != nil {
				t.Fatal(err)
			}
			text := ""
			for _, l := range lines {
				text += stripAnsi(l.text) + "\n"
			}
			if strings.Contains(text, `\`) || !strings.Contains(text, c.shown) {
				t.Fatalf("want %q and no backslash in:\n%s", c.shown, text)
			}
		})
	}
}

func TestFollowingAnEscapedPipeLinkOpensItsNote(t *testing.T) {
	files := map[string]string{
		"Index.md": escapedPipeTable,
		"C.md":     "# C\n",
	}

	t.Run("from the preview", func(t *testing.T) {
		a, _ := linkTestApp(t, files)
		a.openNote("Index.md", true)
		a.followLink(glamourLink(t, a, escapedPipeTable))
		if got := a.activeBuffer().path; got != "C.md" {
			t.Fatalf("opened %s", got)
		}
	})

	t.Run("from the editor", func(t *testing.T) {
		a, _ := linkTestApp(t, files)
		gdOn(t, a, "Index.md", "[[C")
		if got := a.activeBuffer().path; got != "C.md" {
			t.Fatalf("opened %s", got)
		}
	})
}
