package vault

import "testing"

func TestRewriteAssetReferencesFollowsEveryReadingAndLeavesCode(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/pic.png"}, {Path: "assets/other.png"}}
	body := "![[pic.png]] ![[assets/pic.png|300]] [rel](../assets/pic.png#page=2)\n" +
		"`![[pic.png]]` `[rel](../assets/pic.png)`\n```md\n![[pic.png]]\n```\n![[other.png]]"
	got, n := RewriteAssetReferencesInBody(body, assets, nil, "notes/n.md", "assets/pic.png", "assets/new.png")
	want := "![[new.png]] ![[assets/new.png|300]] [rel](../assets/new.png#page=2)\n" +
		"`![[pic.png]]` `[rel](../assets/pic.png)`\n```md\n![[pic.png]]\n```\n![[other.png]]"
	if got != want || n != 3 {
		t.Fatalf("rewrote %d:\n%s", n, got)
	}
}

func TestRewriteAssetReferencesKeepsRawHrefsOneLink(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/report.pdf"}}
	for _, c := range []struct{ next, body, want string }{
		{"assets/new report.pdf", "[r](../assets/report.pdf#page=2)", "[r](<../assets/new report.pdf#page=2>)"},
		{"assets/new (1).pdf", "[r](../assets/report.pdf)", "[r](<../assets/new (1).pdf>)"},
		{"assets/new report.pdf", "[r](report.pdf \"T\")", "[r](<new report.pdf> \"T\")"},
		{"assets/new report.pdf", "`[r](../assets/report.pdf)` [r](../assets/report.pdf)", "`[r](../assets/report.pdf)` [r](<../assets/new report.pdf>)"},
		{"assets/new report.pdf", "[o](../assets/other.pdf) [r](../assets/report.pdf)", "[o](../assets/other.pdf) [r](<../assets/new report.pdf>)"},
		{"assets/new report.pdf", "[r](<../assets/report.pdf>)", "[r](<../assets/new report.pdf>)"},
		{"assets/new report.pdf", "[r](../assets/report%2Epdf)", "[r](../assets/new%20report.pdf)"},
		{"assets/new report.pdf", "![[assets/report.pdf#page=3|Report]]", "![[assets/new report.pdf#page=3|Report]]"},
		{"assets/new.pdf", "[r](../assets/report.pdf \"T\")", "[r](../assets/new.pdf \"T\")"},
	} {
		got, n := RewriteAssetReferencesInBody(c.body, assets, nil, "notes/n.md", "assets/report.pdf", c.next)
		if got != c.want || n != 1 {
			t.Errorf("%s -> %s: rewrote %d, got %s, want %s", c.body, c.next, n, got, c.want)
		}
	}
}

func TestAssetsReferencedByReadsLikeARename(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/bare.png"}, {Path: "assets/sized.png"}, {Path: "notes/near.png"}, {Path: "assets/angled name.png"},
		{Path: "assets/coded.png"}, {Path: "a/same.png"}, {Path: "b/same.png"}, {Path: "assets/unused.png"}}
	body := "![[BARE.png]] ![[assets/sized.png|300]] [n](near.png) [a](<../assets/angled name.png>)\n" +
		"`![[coded.png]]`\n```\n![[coded.png]]\n```\n![[same.png]] [web](https://x.test/unused.png) [[A note]]"
	got := AssetsReferencedBy(body, assets, nil, "notes/n.md")
	want := []string{"assets/bare.png", "assets/sized.png", "notes/near.png", "assets/angled name.png"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, p := range want {
		if !got[p] {
			t.Errorf("missing %s in %v", p, got)
		}
	}
}

func TestRewriteAssetReferencesLeavesEveryCodeForm(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/pic.png"}}
	for name, body := range map[string]string{
		"double backticks":        "``![[pic.png]]``",
		"double holding a tick":   "`` ` ![[pic.png]] ` ``",
		"four-tick fence":         "````md\n```\n![[pic.png]]\n```\n````",
		"tilde closes no ticks":   "```\n~~~\n![[pic.png]]\n```",
		"ticks close no tilde":    "~~~\n```\n![[pic.png]]\n~~~",
		"closer has trailing":     "```\n``` not a closer\n![[pic.png]]\n```",
		"unclosed fence":          "```\n![[pic.png]]",
		"crlf fence":              "```\r\n![[pic.png]]\r\n```\r\n",
		"markdown in double tick": "``[r](pic.png)``",
	} {
		if got, n := RewriteAssetReferencesInBody(body, assets, nil, "n.md", "assets/pic.png", "assets/new.png"); got != body || n != 0 {
			t.Errorf("%s: rewrote %d: %q", name, n, got)
		}
	}
	for name, c := range map[string]struct{ body, want string }{
		"after a closed fence":       {"````\n```\n````\n![[pic.png]]", "````\n```\n````\n![[new.png]]"},
		"after a double-tick span":   {"``a`` ![[pic.png]]", "``a`` ![[new.png]]"},
		"unmatched tick is literal":  {"` ![[pic.png]]", "` ![[new.png]]"},
		"unequal runs do not pair":   {"`` ![[pic.png]] `", "`` ![[new.png]] `"},
		"inline fence-looking ticks": {"```a``` ![[pic.png]]", "```a``` ![[new.png]]"},
		"crlf closer ends the fence": {"```\r\ncode\r\n```\r\n![[pic.png]]\r\n", "```\r\ncode\r\n```\r\n![[new.png]]\r\n"},
		"indented closer ends fence": {"```\ncode\n  ```\n![[pic.png]]", "```\ncode\n  ```\n![[new.png]]"},
		"longer closer ends fence":   {"```\ncode\n`````\n![[pic.png]]", "```\ncode\n`````\n![[new.png]]"},
	} {
		if got, _ := RewriteAssetReferencesInBody(c.body, assets, nil, "n.md", "assets/pic.png", "assets/new.png"); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestRewriteAssetReferencesKeepsAPercentNameReadable(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/report.pdf"}}
	for next, want := range map[string]string{
		"assets/50%20off.pdf": "[r](../assets/50%2520off.pdf)",
		"assets/100%.pdf":     "[r](../assets/100%.pdf)",
		"assets/new.pdf":      "[r](../assets/new.pdf)",
	} {
		got, _ := RewriteAssetReferencesInBody("[r](../assets/report.pdf)", assets, nil, "notes/n.md", "assets/report.pdf", next)
		if got != want {
			t.Errorf("%s: %q, want %q", next, got, want)
		}
		if refs := AssetsReferencedBy(got, []AssetMeta{{Path: next}}, nil, "notes/n.md"); !refs[next] {
			t.Errorf("%s: %q no longer reads as the file", next, got)
		}
	}
}

func TestAWikilinkToANoteIsNotAFileOfTheSameName(t *testing.T) {
	assets := []AssetMeta{{Path: "inbox/TODO"}, {Path: "assets/pic.png"}}
	notes := []NoteMeta{{Path: "TODO.md", Title: "TODO", Folder: FolderInbox}}
	body := "[[TODO]] [[todo|my list]] ![[TODO#Heading]] ![[pic.png]]"
	got, n := RewriteAssetReferencesInBody(body, assets, notes, "links.md", "inbox/TODO", "inbox/TODO.old")
	if got != body || n != 0 {
		t.Fatalf("rewrote %d: %q", n, got)
	}
	if refs := AssetsReferencedBy(body, assets, notes, "links.md"); refs["inbox/TODO"] || !refs["assets/pic.png"] {
		t.Fatalf("%v", refs)
	}
	// A markdown link and a path spell out the file, and a trashed note never
	// resolves, so those still follow it.
	trashed := []NoteMeta{{Path: "trash/TODO.md", Title: "TODO", Folder: FolderTrash}}
	if got, _ := RewriteAssetReferencesInBody("[[TODO]]", assets, trashed, "links.md", "inbox/TODO", "inbox/TODO.old"); got != "[[TODO.old]]" {
		t.Fatalf("trashed note: %q", got)
	}
	if got, _ := RewriteAssetReferencesInBody("[f](inbox/TODO) [[inbox/TODO]]", assets, notes, "links.md", "inbox/TODO", "inbox/TODO.old"); got != "[f](inbox/TODO.old) [[inbox/TODO.old]]" {
		t.Fatalf("spelled-out file: %q", got)
	}
}
