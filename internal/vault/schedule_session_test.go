package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventListNotes(t *testing.T) {
	v := newTestVault(t)
	root := v.Root()
	for rel, body := range map[string]string{
		"inbox/B.md": "---\npage-type: event-list\n---\n",
		"inbox/A.md": "---\npage-type: \"event-list\"\n---\n",
		"inbox/C.md": "---\npage-type: events\n---\n",
		"inbox/D.md": "# plain\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(v.EventListNotes(), ","); got != "inbox/A.md,inbox/B.md" {
		t.Errorf("got %q", got)
	}
}

func TestSetTaskBlockID(t *testing.T) {
	cases := []struct {
		name, body string
		index      int
		wantBody   string
		wantID     string
		wantOK     bool
	}{
		{"adds an id", "a\n- [ ] Write due:2026-11-06\n- [ ] Other\n", 0, "a\n- [ ] Write due:2026-11-06 ^new123\n- [ ] Other\n", "new123", true},
		{"second task", "- [ ] One\n- [ ] Two  \n", 1, "- [ ] One\n- [ ] Two ^new123\n", "new123", true},
		{"keeps an id", "- [ ] Write ^draft-1\n", 0, "- [ ] Write ^draft-1\n", "draft-1", true},
		{"keeps line endings", "- [ ] Write\r\nrest\r\n", 0, "- [ ] Write ^new123\r\nrest\r\n", "new123", true},
		{"no such task", "- [ ] One\n", 3, "- [ ] One\n", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, id, ok := SetTaskBlockID(c.body, c.index, func() string { return "new123" })
			if body != c.wantBody || id != c.wantID || ok != c.wantOK {
				t.Errorf("got %q %q %v, want %q %q %v", body, id, ok, c.wantBody, c.wantID, c.wantOK)
			}
		})
	}
}

func TestAppendLine(t *testing.T) {
	cases := map[string][2]string{
		"empty note":       {"", "- x\n"},
		"final newline":    {"a\n", "a\n- x\n"},
		"no final newline": {"a", "a\n- x\n"},
		"crlf":             {"a\r\nb", "a\r\nb\r\n- x\r\n"},
	}
	for name, c := range cases {
		if got := AppendLine(c[0], "- x"); got != c[1] {
			t.Errorf("%s: got %q, want %q", name, got, c[1])
		}
	}
}

func TestSessionLine(t *testing.T) {
	got := SessionLine(" Write report ", "[[Work#^ab12cd]]", "2026-11-03T14:00-15:30")
	want := "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00-15:30"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
