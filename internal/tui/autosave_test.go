package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

func noteOnDisk(t *testing.T, root, name string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// openLoaded opens a note the way a key press does: an empty editor first,
// its text when the read returns.
func openLoaded(t *testing.T, a *App, name string) *noteBuffer {
	t.Helper()
	a.deferReads = true
	a.openNote(name, true)
	a.deferReads = false
	buf := a.activeBuffer()
	if buf == nil || !buf.loading {
		t.Fatalf("%s did not open through a deferred read", name)
	}
	a.applyBufferRead(a.readBufferCmd(buf)().(bufferReadMsg))
	suppressAsyncTestTimers(a)
	return buf
}

// runAutosaveNow expires the buffer's timer and runs what the tick would.
func runAutosaveNow(t *testing.T, a *App, buf *noteBuffer) {
	t.Helper()
	if !buf.saveAt.IsZero() {
		buf.saveAt = time.Now().Add(-time.Second)
	}
	suppressAsyncTestTimers(a)
	_, cmd := a.Update(autosaveMsg{})
	for _, msg := range awaitAsyncTestResults(t, runAsyncTestCommand(cmd)) {
		a.Update(msg)
	}
	suppressAsyncTestTimers(a)
}

// The reported data loss: `u` right after opening a note used to reach the
// empty placeholder, and autosave then wrote that over the file.
func TestUndoAfterOpeningStopsAtTheLoadedNote(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	buf := openLoaded(t, a, "Alpha.md")
	want := "# Alpha.md\n\nSaved content.\n"

	buf.ed.Feed("u")
	if got := buf.ed.Text(); got != want {
		t.Fatalf("u went past the loaded note: %q", got)
	}
	if buf.dirty() {
		t.Fatal("u after opening left the note dirty")
	}
	if msg, isErr := buf.ed.Message(); !isErr || !strings.Contains(msg, "oldest") {
		t.Errorf("u at the floor reported %q (error %v)", msg, isErr)
	}
	runAutosaveNow(t, a, buf)
	if got := noteOnDisk(t, root, "Alpha.md"); got != want {
		t.Fatalf("file changed: %q", got)
	}

	// Later edits undo back to the same floor, and no further.
	buf.ed.Feed("Otyped<Esc>uuu")
	if got := buf.ed.Text(); got != want {
		t.Fatalf("undo after an edit gave %q", got)
	}
}

// A first read that fails leaves the placeholder; the read that later works is
// still the floor.
func TestUndoFloorSurvivesAFailedFirstRead(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	real := a.backend
	failing := &slowIOBackend{Backend: real, readErr: errors.New("offline")}
	a.backend, a.opts.Backend = failing, failing
	a.deferReads = true
	a.openNote("Alpha.md", true)
	a.deferReads = false
	buf := a.activeBuffer()
	a.applyBufferRead(a.readBufferCmd(buf)().(bufferReadMsg))
	if buf.loadErr == nil {
		t.Fatal("read did not fail")
	}

	a.backend, a.opts.Backend = real, real
	a.reloadBuffer(buf)
	buf.ed.Feed("u")
	if got := buf.ed.Text(); got != "# Alpha.md\n\nSaved content.\n" {
		t.Fatalf("undo reached the placeholder: %q", got)
	}
}

// A reload from disk is still a change undo can reach, and reading a loaded
// note again keeps its history.
func TestReloadAfterLoadStaysUndoable(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	buf := openLoaded(t, a, "Alpha.md")
	if err := os.WriteFile(filepath.Join(root, "Alpha.md"), []byte("From elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reloadBuffer(buf)
	if got := buf.ed.Text(); got != "From elsewhere\n" {
		t.Fatalf("reload gave %q", got)
	}
	buf.ed.Feed("u")
	if got := buf.ed.Text(); got != "# Alpha.md\n\nSaved content.\n" {
		t.Fatalf("undo of a reload gave %q", got)
	}
}

// A note that started empty really can undo to empty. That still must not
// reach the file by itself, whichever way the save would have happened.
func TestUndoToEmptyIsNotWrittenUnlessAskedFor(t *testing.T) {
	const typed = "Typed then saved\n"
	setup := func(t *testing.T) (*App, *noteBuffer, string) {
		t.Helper()
		root := newSessionParityVault(t)
		if err := os.WriteFile(filepath.Join(root, "Empty.md"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		a := newSessionParityApp(t, root)
		buf := openLoaded(t, a, "Empty.md")
		buf.ed.Feed("iTyped then saved<Esc>")
		runAutosaveNow(t, a, buf)
		if got := noteOnDisk(t, root, "Empty.md"); strings.TrimSpace(got) != strings.TrimSpace(typed) {
			t.Fatalf("typing did not autosave: %q", got)
		}
		buf.ed.Feed("u")
		if buf.ed.Text() != "" && buf.ed.Text() != "\n" {
			t.Fatalf("undo gave %q", buf.ed.Text())
		}
		return a, buf, root
	}
	kept := func(t *testing.T, root string) {
		t.Helper()
		if got := noteOnDisk(t, root, "Empty.md"); strings.TrimSpace(got) != strings.TrimSpace(typed) {
			t.Fatalf("undo wiped the file: %q", got)
		}
	}

	t.Run("autosave", func(t *testing.T) {
		a, buf, root := setup(t)
		if a.autosavePending(buf) {
			t.Error("autosave still wants the emptied buffer, so its timer would spin")
		}
		runAutosaveNow(t, a, buf)
		kept(t, root)
		if !buf.dirty() {
			t.Error("the emptied buffer should show as unsaved")
		}
	})
	t.Run("closing the tab", func(t *testing.T) {
		a, _, root := setup(t)
		a.closeTab(a.activePane, a.activePane.active)
		kept(t, root)
	})
	t.Run("quitting", func(t *testing.T) {
		a, _, root := setup(t)
		a.quit()
		if !a.quitting {
			t.Error("quit was refused")
		}
		if err := a.shutdown(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
		kept(t, root)
	})
	t.Run("redo then autosave", func(t *testing.T) {
		a, buf, root := setup(t)
		buf.ed.Feed("<C-r>")
		if !strings.Contains(buf.ed.Text(), "Typed then saved") || buf.dirty() {
			t.Fatalf("redo gave %q (dirty %v)", buf.ed.Text(), buf.dirty())
		}
		runAutosaveNow(t, a, buf)
		kept(t, root)
	})
	t.Run("typing after the undo saves again", func(t *testing.T) {
		a, buf, root := setup(t)
		buf.ed.Feed("iNew text<Esc>")
		runAutosaveNow(t, a, buf)
		if got := noteOnDisk(t, root, "Empty.md"); !strings.Contains(got, "New text") {
			t.Fatalf("edit after undo was not saved: %q", got)
		}
	})
	t.Run(":w saves it empty", func(t *testing.T) {
		a, buf, root := setup(t)
		if _, err := a.runEx(buf, vim.ExCommand{Name: "w"}); err != nil {
			t.Fatal(err)
		}
		for _, cmd := range a.pendingCmds {
			a.Update(cmd())
		}
		suppressAsyncTestTimers(a)
		if got := noteOnDisk(t, root, "Empty.md"); strings.TrimSpace(got) != "" {
			t.Fatalf(":w did not write the empty note: %q", got)
		}
	})
	t.Run(":wq saves it empty", func(t *testing.T) {
		a, buf, root := setup(t)
		if _, err := a.runEx(buf, vim.ExCommand{Name: "wq"}); err != nil {
			t.Fatal(err)
		}
		if got := noteOnDisk(t, root, "Empty.md"); strings.TrimSpace(got) != "" {
			t.Fatalf(":wq did not write the empty note: %q", got)
		}
	})
}

// Deleting everything by hand is an edit, not an undo: it saves as ever.
func TestDeletingAllTextStillAutosaves(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	buf := openLoaded(t, a, "Alpha.md")
	buf.ed.Feed("ggdG")
	runAutosaveNow(t, a, buf)
	if got := noteOnDisk(t, root, "Alpha.md"); strings.TrimSpace(got) != "" {
		t.Fatalf("deliberate delete was not saved: %q", got)
	}
}

func autosaveOffApp(t *testing.T) (*App, *noteBuffer, string) {
	t.Helper()
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	a.prefs.Autosave = false
	buf := openLoaded(t, a, "Alpha.md")
	buf.ed.Feed("Oedit<Esc>")
	return a, buf, root
}

const alphaOnDisk = "# Alpha.md\n\nSaved content.\n"

func TestAutosaveOffWritesOnlyWhenAsked(t *testing.T) {
	t.Run("no timer write", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		if a.autosavePending(buf) {
			t.Error("autosave is off but a save is pending")
		}
		runAutosaveNow(t, a, buf)
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf("autosave wrote with it off: %q", got)
		}
		if !buf.dirty() {
			t.Error("the edit should show as unsaved")
		}
	})
	t.Run(":w writes", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		if _, err := a.runEx(buf, vim.ExCommand{Name: "w"}); err != nil {
			t.Fatal(err)
		}
		for _, cmd := range a.pendingCmds {
			a.Update(cmd())
		}
		suppressAsyncTestTimers(a)
		if got := noteOnDisk(t, root, "Alpha.md"); !strings.HasPrefix(got, "edit\n") {
			t.Fatalf(":w did not save: %q", got)
		}
	})
	t.Run(":wq writes and closes", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		if _, err := a.runEx(buf, vim.ExCommand{Name: "wq"}); err != nil {
			t.Fatal(err)
		}
		if got := noteOnDisk(t, root, "Alpha.md"); !strings.HasPrefix(got, "edit\n") {
			t.Fatalf(":wq did not save: %q", got)
		}
		if a.buffers["Alpha.md"] != nil {
			t.Error(":wq left the buffer open")
		}
	})
	t.Run(":wqa writes and quits", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		if _, err := a.runEx(buf, vim.ExCommand{Name: "wqa"}); err != nil {
			t.Fatal(err)
		}
		if !a.quitting {
			t.Error(":wqa did not quit")
		}
		if got := noteOnDisk(t, root, "Alpha.md"); !strings.HasPrefix(got, "edit\n") {
			t.Fatalf(":wqa did not save: %q", got)
		}
	})
	t.Run(":q refuses, :q! discards", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		if err := a.closeActiveTabCommand(false); !errors.Is(err, errUnsavedEdits) {
			t.Fatalf(":q returned %v", err)
		}
		if a.buffers["Alpha.md"] != buf || a.activeBuffer() != buf {
			t.Fatal(":q closed the note")
		}
		if err := a.closeActiveTabCommand(true); err != nil {
			t.Fatalf(":q!: %v", err)
		}
		if a.buffers["Alpha.md"] != nil {
			t.Error(":q! left the buffer open")
		}
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf(":q! wrote the edit: %q", got)
		}
	})
	t.Run("closing a tab by key or mouse refuses", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		a.closeTab(a.activePane, a.activePane.active)
		if a.activeBuffer() != buf {
			t.Fatal("the tab closed and dropped the edit")
		}
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf("closing wrote the edit: %q", got)
		}
	})
	t.Run("a second view can close", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		first := a.activePane
		a.splitPane(true)
		if a.activePane == first || a.activeBuffer() != buf {
			t.Fatal("the split does not show the note")
		}
		a.closeTab(a.activePane, a.activePane.active)
		if a.buffers["Alpha.md"] != buf {
			t.Fatal("closing one of two views dropped the buffer")
		}
		if !buf.dirty() || noteOnDisk(t, root, "Alpha.md") != alphaOnDisk {
			t.Fatal("closing one of two views saved the edit")
		}
	})
	t.Run(":qa refuses, :qa! discards", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		if _, err := a.runEx(buf, vim.ExCommand{Name: "qa"}); err != nil {
			t.Fatal(err)
		}
		if a.quitting {
			t.Fatal(":qa quit over unsaved edits")
		}
		if _, err := a.runEx(buf, vim.ExCommand{Name: "qa", Bang: true}); err != nil {
			t.Fatal(err)
		}
		if !a.quitting {
			t.Fatal(":qa! did not quit")
		}
		if err := a.shutdown(); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf(":qa! wrote the edit: %q", got)
		}
		assertNoRecovery(t)
	})
	t.Run("an unplanned exit keeps a recovery copy", func(t *testing.T) {
		a, _, root := autosaveOffApp(t)
		err := a.shutdown()
		if err == nil || !strings.Contains(err.Error(), "recovered") {
			t.Fatalf("shutdown: %v", err)
		}
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf("exit wrote the edit: %q", got)
		}
		if notes := recoveredNotes(t); !strings.HasPrefix(notes["Alpha.md"], "edit\n") {
			t.Fatalf("recovery copy: %q", notes)
		}
	})
	t.Run(":only keeps the edit in memory", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		a.splitPane(true)
		// Only the first pane shows the note, so :only closes its last view.
		a.activePane.tabs, a.activePane.active = nil, 0
		a.onlyPane()
		if a.buffers["Alpha.md"] != buf || !buf.dirty() {
			t.Fatal(":only dropped the edit")
		}
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf(":only wrote the edit: %q", got)
		}
	})
	t.Run("switching vault refuses", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		next := a.opts.Target
		next.Root = t.TempDir()
		a.switchTarget(next)
		if a.buffers["Alpha.md"] != buf || a.opts.Target.Root != root {
			t.Fatal("the vault switched over unsaved edits")
		}
		if got := noteOnDisk(t, root, "Alpha.md"); got != alphaOnDisk {
			t.Fatalf("switching wrote the edit: %q", got)
		}
	})
	t.Run("turning it back on saves", func(t *testing.T) {
		a, buf, root := autosaveOffApp(t)
		a.prefs.Autosave = true
		runAutosaveNow(t, a, buf)
		if got := noteOnDisk(t, root, "Alpha.md"); !strings.HasPrefix(got, "edit\n") {
			t.Fatalf("autosave did not resume: %q", got)
		}
	})
}

func recoveredNotes(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(config.UserDataDir(), "tui-recovery", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("recovery files: %v (%v)", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Notes map[string]string `json:"notes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Notes
}

func assertNoRecovery(t *testing.T) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(config.UserDataDir(), "tui-recovery", "*.json"))
	if len(files) != 0 {
		t.Fatalf("a recovery copy was written: %v", files)
	}
}

func linkVault(t *testing.T) (*App, string) {
	t.Helper()
	root := newSessionParityVault(t)
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("inbox/Index.md", "# Index\n\n[[2020-03-31](./Daily/2020-03-31)]\n[plain](./Daily/2020-03-31)\n[gone](./Daily/1999-01-01)\n[pic](./photo.png)\n[here](Sibling)\n[up](../inbox/Sibling#Part)\n")
	write("inbox/Daily/2020-03-31.md", "# March 31\n")
	write("inbox/Sibling.md", "# Sibling\n")
	return newSessionParityApp(t, root), root
}

// gd on the reported text: outer brackets, a relative target, no `.md`.
func TestFollowLinkResolvesRelativeLinksWithoutExtension(t *testing.T) {
	const reported = "inbox/Daily/2020-03-31.md"
	cases := []struct {
		name string
		line int
		col  int
		want string
	}{
		{"on the outer bracket", 2, 0, reported},
		{"inside the label", 2, 5, reported},
		{"on the target", 2, 20, reported},
		{"past the link, first on the line", 2, 35, reported},
		{"without outer brackets", 3, 8, reported},
		{"same folder", 6, 8, "inbox/Sibling.md"},
		{"up a folder with an anchor", 7, 8, "inbox/Sibling.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := linkVault(t)
			a.openNote("inbox/Index.md", true)
			buf := a.activeBuffer()
			buf.ed.SetCursor(vim.Pos{Line: tc.line, Col: tc.col})
			buf.ed.Feed("gd")
			if got := a.activeBuffer(); got == nil || got.path != tc.want {
				t.Fatalf("gd opened %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestFollowLinkReportsLinksItCannotOpen(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		line       int
	}{
		{"missing note", "Note not found: Daily/1999-01-01", 4},
		{"not a note", "Not a note: photo.png", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := linkVault(t)
			a.openNote("inbox/Index.md", true)
			buf := a.activeBuffer()
			buf.ed.SetCursor(vim.Pos{Line: tc.line, Col: 3})
			buf.ed.Feed("gd")
			if a.activeBuffer() != buf {
				t.Fatalf("gd opened %s", a.activeBuffer().path)
			}
			if len(a.toasts) == 0 || a.toasts[len(a.toasts)-1].text != tc.want {
				t.Fatalf("toasts %+v, want %q", a.toasts, tc.want)
			}
		})
	}
}

// A link from outside any note folder, written from the vault root.
func TestFollowLinkStillFindsAPathFromTheVaultRoot(t *testing.T) {
	a, _ := linkVault(t)
	a.openNote("inbox/Sibling.md", true)
	a.followLink(linkTarget{kind: "path", target: "inbox/Daily/2020-03-31.md"})
	if got := a.activeBuffer(); got == nil || got.path != "inbox/Daily/2020-03-31.md" {
		t.Fatalf("opened %+v", got)
	}
}
