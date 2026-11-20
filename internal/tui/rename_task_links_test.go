package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// Renaming a note in the terminal follows the `task:` links of session lines
// and event-note frontmatter, for a note name that holds a space.
func TestTuiRenameRewritesTaskLinks(t *testing.T) {
	a, root := newFeatureTestApp(t)
	files := map[string]string{
		"Work Plan.md": "- [ ] Write report due:2026-11-06 ^ab12cd\n",
		"Plans.md":     "- Write report task:[[Work Plan#^ab12cd]] when:2026-11-03T14:00-15:30\n",
		"Report sprint.md": "---\npage-type: event\nwhen: 2026-11-04T09:00-12:00\n" +
			"task: \"[[Work Plan#^ab12cd]]\"\n---\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := a.loadIndexCmd()().(indexLoadedMsg)
	if m.err != nil {
		t.Fatal(m.err)
	}
	a.idx = m.idx
	a.renameNote("Work Plan.md")
	a.overlay.(*prompt).onSubmit(a, "Job Plan")
	want := map[string]string{
		"Plans.md": "- Write report task:[[Job Plan#^ab12cd]] when:2026-11-03T14:00-15:30\n",
		"Report sprint.md": "---\npage-type: event\nwhen: 2026-11-04T09:00-12:00\n" +
			"task: \"[[Job Plan#^ab12cd]]\"\n---\n",
	}
	for rel, body := range want {
		got, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Errorf("%s:\n%s\nwant:\n%s", rel, got, body)
		}
	}
}
