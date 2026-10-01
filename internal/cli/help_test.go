package cli

import (
	"strings"
	"testing"
)

// checkHelpRows asserts every row keeps its description in the description
// column: either on the same line after at least columnGap spaces, or, for a
// name too wide for the column, on the next line at the column. A name must
// never run straight into its description (`"<body>"Answer in a thread`).
func checkHelpRows(t *testing.T, lines []string, rows []helpRow) {
	t.Helper()
	indent := "  " + strings.Repeat(" ", commandColumnWidth)
	for _, row := range rows {
		prefix := "  " + row.name
		idx := -1
		for i, line := range lines {
			if line == prefix || strings.HasPrefix(line, prefix+" ") {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Errorf("%q: not rendered as its own row (glued to its description?)", row.name)
			continue
		}
		firstWord := strings.Fields(row.description)[0]
		rest := strings.TrimPrefix(lines[idx], prefix)
		if strings.TrimSpace(rest) == "" {
			if idx+1 >= len(lines) || !strings.HasPrefix(lines[idx+1], indent+firstWord) {
				t.Errorf("%q: description should start on the next line at column %d, got %q", row.name, len(indent), lines[idx+1])
			}
			continue
		}
		column := len(lines[idx]) - len(strings.TrimLeft(rest, " "))
		if column != len(indent) || !strings.HasPrefix(strings.TrimLeft(rest, " "), firstWord) {
			t.Errorf("%q: description starts at column %d, want %d: %q", row.name, column, len(indent), lines[idx])
		}
		if len(rest)-len(strings.TrimLeft(rest, " ")) < columnGap {
			t.Errorf("%q: fewer than %d spaces before the description: %q", row.name, columnGap, lines[idx])
		}
	}
}

func TestHelpRowsNeverGlueNameToDescription(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	rows := append([]helpRow{}, globalFlags...)
	rows = append(rows, environmentRows...)
	for _, sec := range helpSections {
		rows = append(rows, sec.rows...)
	}
	checkHelpRows(t, strings.Split(RenderHelp(nil), "\n"), rows)

	// Scoped help renders the global flags through the same layout.
	for _, name := range []string{"comment reply", "list", "server"} {
		lines := strings.Split(RenderScopedHelp(name, nil), "\n")
		start := -1
		for i, line := range lines {
			if line == "GLOBAL FLAGS" {
				start = i
			}
		}
		if start < 0 {
			t.Fatalf("scoped help for %q has no GLOBAL FLAGS section", name)
		}
		checkHelpRows(t, lines[start:], globalFlags)
	}
}

func TestHelpWrapsLongDescriptionsAtTheColumn(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	out := RenderHelp(nil)
	indent := "  " + strings.Repeat(" ", commandColumnWidth)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, indent+" ") {
			t.Errorf("continuation line indented past the column: %q", line)
		}
	}
	for _, glued := range []string{`"<body>"Answer`, `"<body>"Start`, `<id>Resolve`, `<app|terminal>Follow`} {
		if strings.Contains(out, glued) {
			t.Errorf("help still glues a name to its description: %q", glued)
		}
	}
}
