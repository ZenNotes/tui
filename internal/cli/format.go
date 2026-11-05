package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/ZenNotes/tui/internal/termsafe"
)

// Output is where a command writes. Tests swap it for buffers.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// useTerminalFilter wraps a stream that is a terminal so control sequences in
// note text are not sent to it. Pipes and files keep every byte.
func useTerminalFilter() {
	if f, ok := stdout.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		stdout = termsafe.Writer{W: f}
	}
	if f, ok := stderr.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		stderr = termsafe.Writer{W: f}
	}
}

// screenText is note text as a terminal should show it, colours included, since
// plain output has none of its own. Other output keeps every byte.
func screenText(s string) string {
	if _, ok := stdout.(termsafe.Writer); ok {
		return termsafe.Visible(s)
	}
	return s
}

// The default mode is terse, human-readable text; `--json` swaps in
// machine-friendly JSON. No colors or box drawing here: the CLI composes in
// pipelines and CI logs, where escape codes render badly.

func emitJSON(value any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(stderr, "zn: "+err.Error())
		return
	}
	_, _ = stdout.Write(buf.Bytes())
}

// emitJSONLine writes value as one line of JSON: the shape a stream of events
// takes, so a reader can act on each line as it arrives.
func emitJSONLine(value any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(stderr, "zn: "+err.Error())
		return
	}
	_, _ = stdout.Write(buf.Bytes())
}

func emitLine(line string) {
	fmt.Fprintln(stdout, screenText(line))
}

// emitOK prints one status line. A name in it is text on a terminal, on that one
// row, so it cannot colour the output or pass for another line. Other output keeps
// every byte.
func emitOK(message string) {
	fmt.Fprintln(stdout, oneRow(stdout, message))
}

// oneRow is text for one row of a stream, cleaned when that stream is a terminal.
func oneRow(w io.Writer, s string) string {
	if _, ok := w.(termsafe.Writer); ok {
		return termsafe.Line(s)
	}
	return s
}

func emitError(message string) {
	if _, ok := stderr.(termsafe.Writer); ok {
		message = termsafe.Visible(message)
	}
	fmt.Fprintf(stderr, "zn: %s\n", message)
}

func truncate(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return strings.TrimRight(string(runes[:max-1]), " \t") + "…"
}

// formatRelativeAge renders an epoch-millisecond timestamp as `3h ago`.
func formatRelativeAge(updatedAt int64) string {
	diff := time.Now().UnixMilli() - updatedAt
	if diff < 0 {
		return "just now"
	}
	minutes := diff / 60_000
	if minutes < 1 {
		return "just now"
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	if days < 30 {
		return fmt.Sprintf("%dd ago", days)
	}
	months := days / 30
	if months < 12 {
		return fmt.Sprintf("%dmo ago", months)
	}
	return fmt.Sprintf("%dy ago", months/12)
}

func pad(value string, width int) string {
	n := utf8.RuneCountInString(value)
	if n >= width {
		return value
	}
	return value + strings.Repeat(" ", width-n)
}
