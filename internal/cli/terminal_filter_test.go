package cli

import (
	"bytes"
	"runtime"
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

// A status line and a path error name what they acted on. On a terminal that name
// is text, on its own row. Anywhere else, every byte stays.
func TestStatusLinesCleanANameOnATerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no file name with a control character in it")
	}
	savedOut, savedErr := stdout, stderr
	t.Cleanup(func() { stdout, stderr = savedOut, savedErr })
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	const color = "\x1b[38;5;123m"
	name := "inbox/" + color + "red"
	fold := "inbox/" + color + "fold"

	for _, step := range []struct {
		argv []string
		verb string
	}{
		{[]string{"create", name + ".md", "--body", "x"}, "Created"},
		{[]string{"write", name + ".md", "--body", "y"}, "Updated"},
		{[]string{"append", name + ".md", "--body", "z"}, "Updated"},
		{[]string{"duplicate", name + ".md"}, "Duplicated"},
		{[]string{"comment", "add", name + ".md", "--body", "hi"}, "Commented on"},
		{[]string{"folder", "create", fold}, "Created"},
		{[]string{"move", name + " copy.md", "--folder", "inbox", "--subpath", color + "fold"}, "Moved"},
		{[]string{"archive", name + ".md"}, "Archived"},
	} {
		var screen, errs bytes.Buffer
		stdout, stderr = termsafe.Writer{W: &screen}, termsafe.Writer{W: &errs}
		code, err := run(append(step.argv, "--vault", root))
		if err != nil || code != 0 {
			t.Fatalf("%v: code %d, %v", step.argv, code, err)
		}
		line := screen.String()
		if !strings.HasPrefix(line, step.verb) {
			t.Errorf("%v: printed %q", step.argv, line)
		}
		if strings.Contains(line, "\x1b") {
			t.Errorf("%v: the terminal got an escape: %q", step.argv, line)
		}
		if !strings.Contains(line, "␛[38;5;123m") {
			t.Errorf("%v: the name is not shown as text: %q", step.argv, line)
		}
	}

	var piped bytes.Buffer
	stdout, stderr = &piped, &bytes.Buffer{}
	if code, err := run([]string{"unarchive", "archive/" + color + "red.md", "--vault", root}); err != nil || code != 0 {
		t.Fatalf("unarchive: code %d, %v", code, err)
	}
	if !strings.Contains(piped.String(), color) {
		t.Errorf("a pipe lost bytes: %q", piped.String())
	}
}

// A name can hold a newline, and it stays on the status line's one row.
func TestStatusLineStaysOnOneRow(t *testing.T) {
	saved := stdout
	t.Cleanup(func() { stdout = saved })
	var screen bytes.Buffer
	stdout = termsafe.Writer{W: &screen}
	emitOK("Moved a\nzn: not what happened")
	if got := screen.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "␤") {
		t.Errorf("printed %q", got)
	}
}

func TestPathErrorsCleanANameOnATerminal(t *testing.T) {
	saved := stderr
	t.Cleanup(func() { stderr = saved })
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	var errs bytes.Buffer
	stderr = termsafe.Writer{W: &errs}
	_, err := run([]string{"read", "inbox/\x1b[38;5;123mmissing.md", "--vault", root})
	if err == nil {
		t.Fatal("reading a missing note did not fail")
	}
	emitError(err.Error())
	if got := errs.String(); strings.Contains(got, "\x1b") || !strings.Contains(got, "␛[38;5;123m") {
		t.Errorf("printed %q", got)
	}
}
