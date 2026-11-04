package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/termsafe"
)

// On a terminal, a note's text shows as text. Anywhere else, every byte stays.
func TestScreenTextFollowsTheOutput(t *testing.T) {
	saved := stdout
	t.Cleanup(func() { stdout = saved })

	var piped bytes.Buffer
	stdout = &piped
	emitLine("a\x1b]52;c;aGk=\x07b")
	if !strings.Contains(piped.String(), "\x1b]52") {
		t.Fatalf("a pipe lost bytes: %q", piped.String())
	}

	var screen bytes.Buffer
	stdout = termsafe.Writer{W: &screen}
	emitLine("a\x1b]52;c;aGk=\x07b \x1b[8mhidden")
	for _, bad := range []string{"\x1b", "\x07"} {
		if strings.Contains(screen.String(), bad) {
			t.Fatalf("the terminal got %q: %q", bad, screen.String())
		}
	}
	if !strings.Contains(screen.String(), "hidden") {
		t.Fatalf("text lost: %q", screen.String())
	}
}
