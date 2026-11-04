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

// An error can name a note, and shows the same way on a terminal.
func TestEmitErrorFollowsTheOutput(t *testing.T) {
	saved := stderr
	t.Cleanup(func() { stderr = saved })

	var piped bytes.Buffer
	stderr = &piped
	emitError("no note a\x1b[8mb")
	if !strings.Contains(piped.String(), "\x1b[8m") {
		t.Fatalf("a pipe lost bytes: %q", piped.String())
	}

	var screen bytes.Buffer
	stderr = termsafe.Writer{W: &screen}
	emitError("no note a\x1b[8mb")
	if strings.Contains(screen.String(), "\x1b") {
		t.Fatalf("the terminal got an escape: %q", screen.String())
	}
}
