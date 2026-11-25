package vault

import "testing"

func TestRewriteWikilinksForRenameKeepsFrontmatter(t *testing.T) {
	notes := []NoteMeta{{Path: "inbox/C.md", Title: "C", Folder: "inbox"}}
	body := "---\nup: \"[[C]]\"\n---\n\nSee [[C]].\n"
	want := "---\nup: \"[[C]]\"\n---\n\nSee [[D]].\n"
	got, n := RewriteWikilinksForRename(body, notes, "inbox/C.md", "D")
	if got != want || n != 1 {
		t.Fatalf("got %q (%d changed), want %q (1 changed)", got, n, want)
	}
}

func TestRewriteWikilinksForRenameFrontmatterOnly(t *testing.T) {
	notes := []NoteMeta{{Path: "inbox/C.md", Title: "C", Folder: "inbox"}}
	body := "---\r\nup: [[C]]\r\n---\r\n"
	got, n := RewriteWikilinksForRename(body, notes, "inbox/C.md", "D")
	if got != body || n != 0 {
		t.Fatalf("got %q (%d changed), want the body unchanged", got, n)
	}
}
