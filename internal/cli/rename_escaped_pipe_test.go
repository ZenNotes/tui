package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// `zn rename` only opens notes the index says link to the old name, so the index
// has to read a table's `[[Note\|d]]` as a link to Note. The rewrite keeps the
// escape: a bare pipe would end the cell.
func TestRenameUpdatesAnEscapedPipeLink(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(inbox, "Src.md")
	for path, body := range map[string]string{
		filepath.Join(inbox, "Note.md"): "# Note\n",
		src:                             "# Src\n\n| [[Note\\|d]] | ![[Note#Part\\|e]] |\n\n[[Note|f]] and [[Note]]\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	captureOutput(t, func() {
		if code := Main([]string{"rename", "inbox/Note.md", "--to", "Renamed", "--vault", root}); code != 0 {
			t.Errorf("zn rename exited %d", code)
		}
	})
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Src\n\n| [[Renamed\\|d]] | ![[Renamed#Part\\|e]] |\n\n[[Renamed|f]] and [[Renamed]]\n"
	if string(got) != want {
		t.Fatalf("Src.md:\n%s\nwant:\n%s", got, want)
	}
}

// A note whose only link to the old name is escaped has to be rewritten too: an
// unescaped link elsewhere in the note must not be what gets it opened.
func TestRenameUpdatesANoteWhoseOnlyLinkIsEscaped(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(inbox, "Src.md")
	for path, body := range map[string]string{
		filepath.Join(inbox, "Note.md"): "# Note\n",
		src:                             "| [[Note\\|d]] | x |\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	captureOutput(t, func() {
		if code := Main([]string{"rename", "inbox/Note.md", "--to", "Renamed", "--vault", root}); code != 0 {
			t.Errorf("zn rename exited %d", code)
		}
	})
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if want := "| [[Renamed\\|d]] | x |\n"; string(got) != want {
		t.Fatalf("Src.md = %q, want %q", got, want)
	}
}
