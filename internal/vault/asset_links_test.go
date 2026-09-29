package vault

import (
	"strings"
	"testing"
)

func TestRewriteAssetLinksPreservesRelativeTargetsFragmentsAndCode(t *testing.T) {
	body := "[PDF](../assets/report.pdf#page=2)\n![[assets/report.pdf#page=3|Report]]\n`[example](../assets/report.pdf)`\n```md\n[example](../assets/report.pdf)\n```\n[other](./assets/report.pdf)"
	got, n := RewriteAssetLinks(body, "notes/n.md", "assets/report.pdf", "assets/new report.pdf")
	if n != 2 {
		t.Fatalf("rewrote %d destinations: %s", n, got)
	}
	for _, want := range []string{"[PDF](<../assets/new report.pdf#page=2>)", "![[assets/new report.pdf#page=3|Report]]", "`[example](../assets/report.pdf)`", "[other](./assets/report.pdf)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
