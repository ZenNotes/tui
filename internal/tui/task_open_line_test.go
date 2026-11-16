package tui

import (
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
)

// A task opened from the Tasks view puts the cursor on the task line.
func TestOpenTaskSourcePutsCursorOnTaskLine(t *testing.T) {
	body := "# Plan\n\nintro\n- [ ] first\n- [ ] second\n"
	a, _ := linkTestApp(t, map[string]string{"plan.md": body})
	a.openTaskSource(vault.Task{SourcePath: "plan.md", Kind: "inline", LineNumber: 4})
	buf := a.activeBuffer()
	if buf == nil || buf.path != "plan.md" {
		t.Fatal("note did not open")
	}
	if got := buf.ed.Cursor().Line; got != 4 {
		t.Fatalf("cursor on line index %d (%q), want 4", got, buf.ed.Line(got))
	}
}
