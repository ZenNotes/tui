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

func TestRewriteWikilinksForRenameFrontmatterEdgeCases(t *testing.T) {
	notes := []NoteMeta{{Path: "inbox/C.md", Title: "C", Folder: "inbox"}}
	cases := []struct {
		name string
		body string
		want string
		n    int
	}{
		{"empty block", "---\n---\nSee [[C]].\n", "---\n---\nSee [[D]].\n", 1},
		{"no closing fence", "---\n[[C]]\n", "---\n[[D]]\n", 1},
		{"code span after frontmatter", "---\nup: [[C]]\n---\n`[[C]]` and [[C]]\n", "---\nup: [[C]]\n---\n`[[C]]` and [[D]]\n", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n := RewriteWikilinksForRename(tc.body, notes, "inbox/C.md", "D")
			if got != tc.want || n != tc.n {
				t.Fatalf("got %q (%d changed), want %q (%d changed)", got, n, tc.want, tc.n)
			}
		})
	}
}
