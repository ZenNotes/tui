package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// A rename must follow the `task:` link of a session line and of an event
// note, whose link sits in the frontmatter. A note name can hold spaces.
func TestRenameRewritesTaskLinks(t *testing.T) {
	v := newTestVault(t)
	files := map[string]string{
		"inbox/Work Plan.md": "- [ ] Write report due:2026-11-06 ^ab12cd\n",
		"inbox/Plans.md": "- Write report task:[[Work Plan#^ab12cd]] when:2026-11-03T14:00-15:30\n" +
			"- task:[[Work Plan#^ab12cd]] when:2026-11-04T09:00\n" +
			"- Other task:[[Elsewhere#^zz99zz]] when:2026-11-05T09:00\n",
		"inbox/Report sprint.md": "---\npage-type: event\nwhen: 2026-11-04T09:00-12:00\n" +
			"task: \"[[Work Plan#^ab12cd]]\"\n---\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(v.Root(), rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.RenameNote("inbox/Work Plan.md", "Job Plan"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"inbox/Plans.md": "- Write report task:[[Job Plan#^ab12cd]] when:2026-11-03T14:00-15:30\n" +
			"- task:[[Job Plan#^ab12cd]] when:2026-11-04T09:00\n" +
			"- Other task:[[Elsewhere#^zz99zz]] when:2026-11-05T09:00\n",
		"inbox/Report sprint.md": "---\npage-type: event\nwhen: 2026-11-04T09:00-12:00\n" +
			"task: \"[[Job Plan#^ab12cd]]\"\n---\n",
	}
	for rel, body := range want {
		got, err := os.ReadFile(filepath.Join(v.Root(), rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Errorf("%s:\n%s\nwant:\n%s", rel, got, body)
		}
	}
}
