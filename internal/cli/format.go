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
)

// Output is where a command writes. Tests swap it for buffers.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

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

func emitLine(line string) {
	fmt.Fprintln(stdout, line)
}

func emitOK(message string) {
	fmt.Fprintln(stdout, message)
}

func emitError(message string) {
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
