package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A table cell escapes a wikilink's separator as `\|`, because GFM would end the
// cell at a bare pipe. Note names cannot hold a backslash, so one right before
// the pipe is always the escape and never part of the target.

func TestExtractWikilinksReadsAnEscapedPipe(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       []string
	}{
		{"table cell", `| [[C\|d]] | x |`, []string{"C"}},
		{"bare, outside a table", `see [[C\|d]] here`, []string{"C"}},
		{"anchor", `| [[C#Heading\|d]] |`, []string{"C#Heading"}},
		{"block anchor", `| [[C^block\|d]] |`, []string{"C^block"}},
		{"path", `| [[notes/C.md\|d]] |`, []string{"notes/C.md"}},
		{"embed", `| ![[pic.png\|300]] |`, []string{"pic.png"}},
		{"alias with more pipes", `[[C\|d\|e]]`, []string{"C"}},
		{"both forms are one link", `[[C|d]] and | [[C\|e]] |`, []string{"C"}},
		{"unescaped alias", `[[C|d]]`, []string{"C"}},
		{"unescaped alias holding an escape", `[[C|d\|e]]`, []string{"C"}},
		{"plain", `[[C]]`, []string{"C"}},
		{"a backslash with no pipe stays", `[[C\]]`, []string{`C\`}},
		{"a backslash inside a target stays", `[[a\b|d]]`, []string{`a\b`}},
		{"inline code", "`[[C\\|d]]`", []string{}},
		{"a separator with no target", `[[\|d]]`, []string{}},
		{"a backslash alone", `[[\]]`, []string{}},
	} {
		if got := ExtractWikilinks(c.body); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ExtractWikilinks(%q) = %q, want %q", c.name, c.body, got, c.want)
		}
	}
}

func TestBacklinksInCountsAnEscapedPipe(t *testing.T) {
	notes := []NoteMeta{
		{Path: "inbox/C.md", Title: "C"},
		{Path: "inbox/Table.md", Title: "Table", Wikilinks: ExtractWikilinks(`| [[C\|d]] | x |`)},
		{Path: "inbox/Plain.md", Title: "Plain", Wikilinks: ExtractWikilinks(`[[C|d]]`)},
		{Path: "inbox/Other.md", Title: "Other", Wikilinks: ExtractWikilinks(`| [[D\|d]] |`)},
	}
	got := []string{}
	for _, n := range BacklinksIn(notes, "inbox/C.md") {
		got = append(got, n.Path)
	}
	if want := []string{"inbox/Table.md", "inbox/Plain.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backlinks of C = %q, want %q", got, want)
	}
}

func TestVaultListsAnEscapedPipeLinkAsABacklink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"C.md":     "# C\n",
		"Table.md": "| Link | Note |\n| --- | --- |\n| [[C\\|d]] | x |\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "inbox", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	links, err := v.Backlinks("inbox/C.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Path != "inbox/Table.md" {
		t.Fatalf("backlinks of C = %+v, want the table note", links)
	}
}

func TestBodyHasLocalAssetReadsAnEscapedPipe(t *testing.T) {
	for body, want := range map[string]bool{
		`| ![[pic.png\|300]] |`: true,
		`![[pic.png|300]]`:      true,
		`| ![[Note\|300]] |`:    false,
		"`![[pic.png\\|300]]`":  false,
	} {
		if got := BodyHasLocalAsset(body); got != want {
			t.Errorf("BodyHasLocalAsset(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestRewriteAssetReferencesKeepsAnEscapedPipe(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/pic.png"}, {Path: "assets/other.png"}}
	for _, c := range []struct {
		name, body, newPath, want string
		changed                   int
	}{
		{"rename", `| ![[pic.png\|300]] |`, "assets/new.png", `| ![[new.png\|300]] |`, 1},
		{"rooted path", `| ![[assets/pic.png\|300]] |`, "assets/new.png", `| ![[assets/new.png\|300]] |`, 1},
		{"move", `| ![[assets/pic.png\|300]] |`, "media/pic.png", `| ![[media/pic.png\|300]] |`, 1},
		{"a bare name that still resolves", `| ![[pic.png\|300]] |`, "media/pic.png", `| ![[pic.png\|300]] |`, 0},
		{"fragment", `| ![[pic.png#page=2\|300]] |`, "assets/new.png", `| ![[new.png#page=2\|300]] |`, 1},
		{"plain link", `| [[pic.png\|the picture]] |`, "assets/new.png", `| [[new.png\|the picture]] |`, 1},
		{"unescaped stays unescaped", `![[pic.png|300]]`, "assets/new.png", `![[new.png|300]]`, 1},
		{"code stays", "`![[pic.png\\|300]]`", "assets/new.png", "`![[pic.png\\|300]]`", 0},
		{"another asset stays", `| ![[other.png\|300]] |`, "assets/new.png", `| ![[other.png\|300]] |`, 0},
	} {
		got, n := RewriteAssetReferencesInBody(c.body, assets, nil, "notes/n.md", "assets/pic.png", c.newPath)
		if got != c.want || n != c.changed {
			t.Errorf("%s: rewrote %d, got %s, want %d, %s", c.name, n, got, c.changed, c.want)
		}
	}
}

func TestRewritesKeepTheSpacesAroundATarget(t *testing.T) {
	notes := []NoteMeta{{Path: "inbox/C.md", Title: "C", Folder: FolderInbox}}
	for in, want := range map[string]string{
		`[[C |d]]`:     `[[Renamed |d]]`,
		`[[ C|d]]`:     `[[ Renamed|d]]`,
		`[[ C ]]`:      `[[ Renamed ]]`,
		`[[a/C |d]]`:   `[[a/C |d]]`,
		`[[inbox/C ]]`: `[[inbox/Renamed ]]`,
	} {
		if got, _ := RewriteWikilinksForRename(in, notes, "inbox/C.md", "Renamed"); got != want {
			t.Errorf("rename: %s -> %s, want %s", in, got, want)
		}
	}
	assets := []AssetMeta{{Path: "assets/pic.png"}}
	for _, c := range []struct {
		in, newPath, want string
		changed           int
	}{
		{`| ![[pic.png \|300]] |`, "assets/new.png", `| ![[new.png \|300]] |`, 1},
		{`![[pic.png |300]]`, "assets/new.png", `![[new.png |300]]`, 1},
		{`![[ pic.png|300]]`, "assets/new.png", `![[ new.png|300]]`, 1},
		{`![[pic.png |300]]`, "media/pic.png", `![[pic.png |300]]`, 0},
	} {
		got, n := RewriteAssetReferencesInBody(c.in, assets, nil, "notes/n.md", "assets/pic.png", c.newPath)
		if got != c.want || n != c.changed {
			t.Errorf("asset: %s -> %d, %s, want %d, %s", c.in, n, got, c.changed, c.want)
		}
	}
}

func TestVaultRenameAssetRewritesAnEscapedPipeInATable(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"assets/pic.png": "png",
		"inbox/Table.md": "| Shot | Note |\n| --- | --- |\n| ![[pic.png\\|300]] | x |\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.RenameAssetRewritingLinksIn("assets/pic.png", "new.png", nil); err != nil {
		t.Fatal(err)
	}
	got, err := v.ReadNote("inbox/Table.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := "| Shot | Note |\n| --- | --- |\n| ![[new.png\\|300]] | x |\n"; got.Body != want {
		t.Fatalf("body = %q, want %q", got.Body, want)
	}
}

func TestAWikilinkToANoteIsNotAFileEvenWithAnEscapedPipe(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/pic.png"}}
	notes := []NoteMeta{{Path: "inbox/pic.png.md", Title: "pic.png"}}
	body := `| [[pic.png\|d]] |`
	if got, n := RewriteAssetReferencesInBody(body, assets, notes, "inbox/n.md", "assets/pic.png", "assets/new.png"); got != body || n != 0 {
		t.Fatalf("a note's link was rewritten: %d, %s", n, got)
	}
	if got := AssetsReferencedBy(body, assets, notes, "inbox/n.md"); len(got) != 0 {
		t.Fatalf("a note's link counted as an asset use: %v", got)
	}
}

func TestAssetsReferencedByReadsAnEscapedPipe(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/escaped.png"}, {Path: "assets/plain.png"}, {Path: "assets/unused.png"}}
	got := AssetsReferencedBy(`| ![[escaped.png\|300]] | ![[plain.png|300]] |`, assets, nil, "notes/n.md")
	want := map[string]bool{"assets/escaped.png": true, "assets/plain.png": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("referenced = %v, want %v", got, want)
	}
}
