package tui

import (
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

const frontNote = "---\ntags: [task]\nstatus: bezig\nrisico: hoog\n---\n\n# Heading\n\nBody text.\n\nSecond paragraph.\n"

// frontRows is the reading view as plain text, one string per row.
func frontRows(a *App) []string {
	a.View()
	rows := []string{}
	for _, l := range a.activeTab().preview.cache.lines {
		rows = append(rows, strings.TrimRight(stripAnsi(l.text), " "))
	}
	return rows
}

func frontmatterApp(t *testing.T, mode, style string) (*App, *previewState) {
	t.Helper()
	a, pv := readingApp(t, frontNote)
	a.prefs.PreviewFrontmatter = mode
	a.prefs.PreviewStyle = style
	a.View()
	return a, pv
}

func TestPreviewFrontmatterModes(t *testing.T) {
	for _, style := range []string{"auto", "zen"} {
		t.Run(style, func(t *testing.T) {
			a, _ := frontmatterApp(t, "raw", style)
			rows := frontRows(a)
			if rows[0] != "tags: [task]" || rows[2] != "risico: hoog" || !strings.HasPrefix(rows[3], "╌") {
				t.Errorf("raw is not the block as written: %q", rows[:4])
			}

			a, _ = frontmatterApp(t, "collapsed", style)
			rows = frontRows(a)
			if rows[0] != "▸ 3 fields" || strings.Contains(strings.Join(rows, "\n"), "risico") {
				t.Errorf("collapsed: %q", rows)
			}

			a, _ = frontmatterApp(t, "hidden", style)
			if all := strings.Join(frontRows(a), "\n"); strings.Contains(all, "fields") || strings.Contains(all, "risico") || !strings.Contains(all, "Heading") {
				t.Errorf("hidden: %q", all)
			}
		})
	}
}

// Enter on the summary row opens the block under it and closes it again.
func TestPreviewFrontmatterOpensOnEnter(t *testing.T) {
	a, pv := frontmatterApp(t, "collapsed", "auto")
	pressAndDraw(a, vim.KeyEnter)
	rows := frontRows(a)
	if rows[0] != "▾ 3 fields" || rows[1] != "tags: [task]" || rows[3] != "risico: hoog" || !strings.HasPrefix(rows[4], "╌") {
		t.Fatalf("after Enter: %q", rows[:5])
	}
	if pv.cursor != 0 {
		t.Errorf("the cursor left the summary row: %d", pv.cursor)
	}
	pressAndDraw(a, vim.KeyEnter)
	if rows = frontRows(a); rows[0] != "▸ 3 fields" || len(rows) > 1 && rows[1] == "tags: [task]" {
		t.Errorf("after a second Enter: %q", rows)
	}
	if got := a.activeBuffer().ed.Text(); got != frontNote {
		t.Error("folding the frontmatter edited the note")
	}
}

// Space z f flips the block from anywhere, and the row under the cursor
// stays the row under the cursor.
func TestPreviewFrontmatterLeaderKeepsTheCursorRow(t *testing.T) {
	a, pv := frontmatterApp(t, "raw", "auto")
	rows := frontRows(a)
	for i, r := range rows {
		if strings.Contains(r, "Second paragraph") {
			pv.cursor = i
		}
	}
	a.View()
	pressAndDraw(a, vim.Key{Rune: ' '}, vim.Key{Rune: 'z'}, vim.Key{Rune: 'f'})
	rows = frontRows(a)
	if rows[0] != "▸ 3 fields" {
		t.Fatalf("Space z f did not collapse a raw block: %q", rows)
	}
	if !strings.Contains(rows[pv.cursor], "Second paragraph") {
		t.Errorf("the cursor is on %q after the fold", rows[pv.cursor])
	}
	pressAndDraw(a, vim.Key{Rune: ' '}, vim.Key{Rune: 'z'}, vim.Key{Rune: 'f'})
	if rows = frontRows(a); rows[0] != "tags: [task]" || !strings.Contains(rows[pv.cursor], "Second paragraph") {
		t.Errorf("a second Space z f: first row %q, cursor on %q", rows[0], rows[pv.cursor])
	}
}

// A hidden block opens under a summary row, so there is a row to close it from.
func TestPreviewFrontmatterHiddenOpens(t *testing.T) {
	a, _ := frontmatterApp(t, "hidden", "auto")
	a.togglePreviewFrontmatter()
	if rows := frontRows(a); rows[0] != "▾ 3 fields" || rows[1] != "tags: [task]" {
		t.Errorf("a hidden block, opened: %q", rows)
	}
	a.togglePreviewFrontmatter()
	if all := strings.Join(frontRows(a), "\n"); strings.Contains(all, "fields") {
		t.Errorf("closed again it is not hidden: %q", all)
	}
}

func TestPreviewFrontmatterCountsOneField(t *testing.T) {
	a, _ := readingApp(t, "---\ntitle: One\n---\n\nBody.\n")
	a.prefs.PreviewFrontmatter = "collapsed"
	if rows := frontRows(a); rows[0] != "▸ 1 field" {
		t.Errorf("summary: %q", rows[0])
	}
	a, _ = readingApp(t, "# No frontmatter\n\nBody.\n")
	a.prefs.PreviewFrontmatter = "collapsed"
	if rows := frontRows(a); strings.Contains(strings.Join(rows, "\n"), "field") {
		t.Errorf("a note without frontmatter got a summary row: %q", rows)
	}
}

// :frontmatter names the mode for every note and saves it as a setting.
func TestFrontmatterExCommand(t *testing.T) {
	a, _ := frontmatterApp(t, "raw", "auto")
	a.runCommandLine("frontmatter collapsed")
	if a.prefs.PreviewFrontmatter != "collapsed" || frontRows(a)[0] != "▸ 3 fields" {
		t.Errorf("after :frontmatter collapsed: %q, %q", a.prefs.PreviewFrontmatter, frontRows(a)[0])
	}
	a.runCommandLine("frontmatter sideways")
	if a.prefs.PreviewFrontmatter != "collapsed" {
		t.Errorf("an unknown mode changed the setting to %q", a.prefs.PreviewFrontmatter)
	}
}
