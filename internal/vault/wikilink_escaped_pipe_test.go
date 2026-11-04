package vault

import "testing"

// A table writes a wikilink's separator as `\|`, the escape GFM needs to keep
// the pipe inside its cell. The backslash belongs to the separator.
func TestSplitWikilinkContentReadsAnEscapedPipe(t *testing.T) {
	for _, c := range []struct{ content, target, anchor, alias string }{
		{`C\|d`, "C", "", `\|d`},
		{`C#Heading\|d`, "C", "#Heading", `\|d`},
		{`C^block\|d`, "C", "^block", `\|d`},
		{`Work/C\|d`, "Work/C", "", `\|d`},
		{`\|d`, "", "", `\|d`},
		{`C|d`, "C", "", "|d"},
		{`C#Heading|d`, "C", "#Heading", "|d"},
		{`C`, "C", "", ""},
		{`Work\C`, `Work\C`, "", ""},
		{`Work\C|d`, `Work\C`, "", "|d"},
	} {
		target, anchor, alias := SplitWikilinkContent(c.content)
		if target != c.target || anchor != c.anchor || alias != c.alias {
			t.Errorf("%q split to %q %q %q, want %q %q %q", c.content, target, anchor, alias, c.target, c.anchor, c.alias)
		}
		if joined := target + anchor + alias; joined != c.content {
			t.Errorf("%q did not join back: %q", c.content, joined)
		}
	}
}

func TestResolveWikilinkReadsAnEscapedPipe(t *testing.T) {
	notes := []NoteMeta{{Path: "inbox/C.md", Title: "C", Folder: FolderInbox}}
	for _, target := range []string{`C\|d`, `C#Heading\|d`, `C|d`, `C`} {
		if n, ok := ResolveWikilink(notes, target); !ok || n.Path != "inbox/C.md" {
			t.Errorf("%q resolved to %+v (%v)", target, n, ok)
		}
	}
}

// A rename has to leave the escape in place: an unescaped pipe in a table row
// ends the cell and breaks the row.
func TestRenameKeepsAnEscapedPipeEscaped(t *testing.T) {
	notes := []NoteMeta{{Path: "inbox/C.md", Title: "C", Folder: FolderInbox}}
	body := "| [[C\\|d]] | ![[C#Heading\\|e]] |\n[[C|f]] and [[C]]\n"
	want := "| [[E\\|d]] | ![[E#Heading\\|e]] |\n[[E|f]] and [[E]]\n"
	got, n := RewriteWikilinksForRename(body, notes, "inbox/C.md", "E")
	if got != want || n != 4 {
		t.Fatalf("rewrote %d:\n%s", n, got)
	}
}
