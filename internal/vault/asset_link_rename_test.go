package vault

import "testing"

func TestRewriteAssetReferencesFollowsEveryReadingAndLeavesCode(t *testing.T) {
	assets := []AssetMeta{{Path: "assets/pic.png"}, {Path: "assets/other.png"}}
	body := "![[pic.png]] ![[assets/pic.png|300]] [rel](../assets/pic.png#page=2)\n" +
		"`![[pic.png]]` `[rel](../assets/pic.png)`\n```md\n![[pic.png]]\n```\n![[other.png]]"
	got, n := RewriteAssetReferencesInBody(body, assets, "notes/n.md", "assets/pic.png", "assets/new.png")
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
		got, n := RewriteAssetReferencesInBody(c.body, assets, "notes/n.md", "assets/report.pdf", c.next)
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
	got := AssetsReferencedBy(body, assets, "notes/n.md")
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
