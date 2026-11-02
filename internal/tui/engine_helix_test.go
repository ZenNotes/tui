package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

// newHelixTestApp is newFeatureTestApp with the helix engine.
func newHelixTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a, root := newFeatureTestApp(t)
	a.prefs.Motions = "helix"
	return a, root
}

func writeTabNotes(t *testing.T, root string) {
	t.Helper()
	for name, text := range map[string]string{"a.md": "A\n", "b.md": "B\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGotoNextPreviousTab(t *testing.T) {
	a, root := newHelixTestApp(t)
	writeTabNotes(t, root)
	a.openNote("a.md", true)
	a.openNote("b.md", true)
	if got := a.activeBuffer().path; got != "b.md" {
		t.Fatalf("expected b.md active, got %q", got)
	}
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('n'))
	if got := a.activeBuffer().path; got != "a.md" {
		t.Fatalf("gn did not switch to the next tab, active %q", got)
	}
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('p'))
	if got := a.activeBuffer().path; got != "b.md" {
		t.Fatalf("gp did not switch to the previous tab, active %q", got)
	}
	// gg must still reach the engine now that g sequences are parked.
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('g'))
	if cur := a.activeBuffer().ed.Cursor(); cur.Line != 0 {
		t.Fatalf("gg moved the cursor unexpectedly: %+v", cur)
	}
}

func TestGotoNextPreviousTabFromHome(t *testing.T) {
	a, root := newHelixTestApp(t)
	writeTabNotes(t, root)
	a.openNote("a.md", true)
	a.openNote("b.md", true)
	a.openHome()
	if got := a.activeTab().path; got != tabHome {
		t.Fatalf("expected Home active, got %q", got)
	}
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('n'))
	if got := a.activeTab().path; got != "a.md" {
		t.Fatalf("gn did not leave Home for the next tab, active %q", got)
	}
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('p'))
	if got := a.activeTab().path; got != tabHome {
		t.Fatalf("gp did not go back to the previous tab, active %q", got)
	}
}

// The helix engine shows its own mode names and reports a selection as
// vim's visual mode to the rest of the app.
func TestHelixSelectModeLabelAndMode(t *testing.T) {
	a, _ := newHelixTestApp(t)
	a.openNote("note.md", true)
	ed := a.activeBuffer().ed
	ed.Feed("v")
	if got := modeLabel(ed); got != "SELECT" {
		t.Fatalf("label = %q, want SELECT", got)
	}
	if got := ed.Mode(); got != vim.ModeVisual {
		t.Fatalf("mode = %v, want vim visual", got)
	}
}

// Key help follows the engine: helix lists its own fold keys and gn/gp.
func TestEngineHintsFollowTheEngine(t *testing.T) {
	for _, tc := range []struct {
		motions, folds string
		tabs           bool
	}{{"vim", "key:zc/zo/zM/zR", false}, {"helix", "key:zf/zo/za/zM/zR", true}} {
		a, _ := newFeatureTestApp(t)
		a.prefs.Motions = tc.motions
		pairs := a.engineHints([]hintPair{{"key:zc/zo/zM/zR", "folds"}, {"vim.bufferNext|vim.bufferPrevious", "tabs"}})
		if pairs[0].id != tc.folds {
			t.Errorf("%s: folds hint %q, want %q", tc.motions, pairs[0].id, tc.folds)
		}
		if got := strings.Contains(pairs[1].id, "helix.gotoTabNext"); got != tc.tabs {
			t.Errorf("%s: tab hint %q", tc.motions, pairs[1].id)
		}
	}
}

// Helix buffers share registers with each other, as vim buffers do.
func TestHelixBuffersShareRegisters(t *testing.T) {
	a, _ := newHelixTestApp(t)
	one := a.newModalEditor("alpha\n", vim.Hooks{}, &a.registers)
	two := a.newModalEditor("", vim.Hooks{}, &a.registers)
	one.Feed("xy")
	two.Feed("p")
	if got := two.Text(); !strings.Contains(got, "alpha") {
		t.Fatalf("paste in another helix buffer = %q", got)
	}
}

// [keys] tables in config.toml rebind the helix engine's keys, in Helix's
// own format; a binding zn cannot read is reported and the rest still apply.
func TestHelixKeysFromConfig(t *testing.T) {
	a, _ := newHelixTestApp(t)
	toml := "[vim]\nmotions = \"helix\"\n\n[keys.normal]\nD = \"move_line_down\"\nQ = \"no_such_command\"\n\n[keys.normal.g]\na = \"goto_last_line\"\n"
	if err := os.WriteFile(config.ConfigTomlPath(), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _, err := config.LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	a.prefs.Prefs = p
	if a.prefs.Motions != "helix" {
		t.Fatalf("motions = %q", a.prefs.Motions)
	}
	ed := a.newModalEditor("one\ntwo\nthree", vim.Hooks{}, &a.registers)
	ed.Feed("D")
	if got := ed.Cursor().Line; got != 1 {
		t.Fatalf("D (move_line_down) = line %d, want 1", got)
	}
	ed.Feed("ga")
	if got := ed.Cursor().Line; got != 2 {
		t.Fatalf("g a (goto_last_line) = line %d, want 2", got)
	}
	if !a.messageErr || !strings.Contains(a.message, "no_such_command") {
		t.Fatalf("bad binding not reported: %q", a.message)
	}
}

// The vim engine ignores [keys]; it has its own [keymaps].
func TestVimIgnoresHelixKeys(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.HelixKeys = map[string]any{"normal": map[string]any{"D": "move_line_down"}}
	ed := a.newModalEditor("one\ntwo\n", vim.Hooks{}, &a.registers)
	ed.Feed("D")
	if got := ed.Text(); got != "\ntwo\n" {
		t.Fatalf("vim D = %q, want the line cleared", got)
	}
}
