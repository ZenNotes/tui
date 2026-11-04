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

// zenTable renders one table row in the zen preview, under a header and separator.
func zenTable(t *testing.T, a *App, row string) string {
	t.Helper()
	text := ""
	for _, l := range a.renderPreviewLines("| Note | Value |\n| --- | --- |\n"+row+"\n", 60) {
		text += stripAnsi(l.text) + "\n"
	}
	return text
}

func TestZenPreviewKeepsAnEscapedPipeInItsCell(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	for _, c := range []struct{ name, row, shown, gone string }{
		{"link with alias", `| [[C\|alias]] | x |`, "alias", `\`},
		{"alias with its own pipe", `| [[C\|a\|b]] | x |`, "a|b", `\`},
		{"plain text", `| a\|b | x |`, "a|b", `\`},
		{"unescaped link", `| [[C]] | x |`, "C", `\`},
		{"pipe inside inline code", "| `![[pic.png|small]]` | x |", "pic.png", `\`},
	} {
		t.Run(c.name, func(t *testing.T) {
			text := zenTable(t, a, c.row)
			if !strings.Contains(text, c.shown) || strings.Contains(text, c.gone) {
				t.Fatalf("want %q and no %q in:\n%s", c.shown, c.gone, text)
			}
			if !strings.Contains(text, " x") {
				t.Fatalf("second cell lost:\n%s", text)
			}
			// Header and body rows have the same number of column bars.
			rows := strings.Split(strings.TrimRight(text, "\n"), "\n")
			header, body := strings.Count(rows[0], "│"), strings.Count(rows[len(rows)-1], "│")
			if header != body {
				t.Fatalf("header has %d bars, body %d:\n%s", header, body, text)
			}
		})
	}
}

func TestPreviewsShowAnAliasWithItsOwnPipe(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	if got := wikilinkTargetOf(`C\|a\|b`); got.target != "C" || got.alias != "a|b" {
		t.Fatalf("link %+v", got)
	}
	zen := ""
	for _, l := range a.renderPreviewLines(`see [[C\|a\|b]] here`, 60) {
		zen += stripAnsi(l.text)
	}
	if !strings.Contains(zen, "a|b") || strings.Contains(zen, `\`) {
		t.Fatalf("zen paragraph: %q", zen)
	}
	// Glamour has to keep the escape until its parser reads the cell, or the
	// row gains a column.
	lines, err := a.renderGlamourLines("| Note | Value |\n| --- | --- |\n"+`| [[C\|a\|b]] | x |`+"\n", 60)
	if err != nil {
		t.Fatal(err)
	}
	last := stripAnsi(lines[len(lines)-1].text)
	for i := len(lines) - 1; i >= 0 && strings.TrimSpace(last) == ""; i-- {
		last = stripAnsi(lines[i].text)
	}
	if !strings.Contains(last, "a|b") || strings.Contains(last, `\`) || !strings.Contains(last, "x") {
		t.Fatalf("glamour row: %q", last)
	}
}

func TestResolveEmbedReadsAnEscapedPipe(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	note := a.resolveEmbed(`C\|d`, "Index.md")
	if note.kind != "note" || note.rel != "C.md" {
		t.Fatalf("note embed: %+v", note)
	}
	image := a.resolveEmbed(`pic.png\|300`, "Index.md")
	if image.kind != "image" || image.title != "pic.png" {
		t.Fatalf("image embed: %+v", image)
	}
	for _, raw := range []string{`C|d`, `C`} {
		if got := a.resolveEmbed(raw, "Index.md"); got.kind != "note" || got.rel != "C.md" {
			t.Fatalf("%q: %+v", raw, got)
		}
	}
	if got := a.resolveEmbed(`pic.png|300`, "Index.md"); got.kind != "image" || got.title != "pic.png" {
		t.Fatalf("unescaped image: %+v", got)
	}
}

// A one-line embed and `gd` on that line have to name the same note.
func TestOneLineEmbedAgreesWithFollowing(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n", "Index.md": "![[C\\|d]]\n"})
	card := a.resolveEmbed(`C\|d`, "Index.md")
	gdOn(t, a, "Index.md", "![[C")
	if got := a.activeBuffer().path; got != card.rel || got != "C.md" {
		t.Fatalf("gd opened %s, card names %s", got, card.rel)
	}
}

func TestAWikilinkWithNoTargetIsNotALink(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	for _, raw := range []string{`[[\|d]]`, `![[\|d]]`, `[[|d]]`, `![[|d]]`} {
		t.Run(raw, func(t *testing.T) {
			if got, ok := linkAt(raw, 3); ok {
				t.Errorf("linkAt: %+v", got)
			}
			if got := linksOnLine(raw); len(got) != 0 {
				t.Errorf("linksOnLine: %+v", got)
			}
			for _, row := range []string{raw, "see " + raw + " here", "| " + raw + " | x |"} {
				zen := a.renderPreviewLines(row, 60)
				glamour, err := a.renderGlamourLines(row, 60)
				if err != nil {
					t.Fatal(err)
				}
				for _, l := range append(zen, glamour...) {
					if text := stripAnsi(l.text); strings.Contains(text, "image:") {
						t.Errorf("%q shows an embed: %q", row, text)
					}
					if len(l.links) != 0 {
						t.Errorf("%q has links %+v", row, l.links)
					}
				}
			}
		})
	}
	// An anchor alone still is one: it targets the note it is in.
	if got, ok := linkAt(`[[#Heading\|d]]`, 3); !ok || got.target != "#Heading" {
		t.Fatalf("same-note anchor: %+v (%v)", got, ok)
	}
}

func TestANamelessLinkOffersNoNote(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{"C.md": "# C\n"})
	a.offerNoteFromLink("C.md", linkTarget{kind: "wikilink", target: ""})
	if a.overlay != nil {
		t.Fatalf("offered a nameless note: %+v", a.overlay)
	}
	a.offerNoteFromLink("C.md", linkTarget{kind: "wikilink", target: "New"})
	if _, ok := a.overlay.(*confirmDialog); !ok {
		t.Fatalf("a named link offers its note, got %+v", a.overlay)
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
