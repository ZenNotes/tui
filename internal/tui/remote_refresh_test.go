package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

// remoteRefreshBackend uses real vault reads and writes while exercising the
// remote polling branch, without coupling these UI tests to the HTTP protocol.
type remoteRefreshBackend struct{ backend.Backend }

func (remoteRefreshBackend) Kind() backend.Kind { return backend.KindRemote }

// Index delivery also schedules a task scan; this fixture has no task surface
// under test, so keep that unrelated command immediate and free of I/O.
func (remoteRefreshBackend) ScanTasks(context.Context, vault.ParseTasksOptions) ([]vault.Task, error) {
	return nil, nil
}

func newRemoteRefreshApp(t *testing.T) (*App, string) {
	t.Helper()
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	a.backend = remoteRefreshBackend{Backend: a.backend}
	a.opts.Backend = a.backend
	a.opts.Target = backend.Target{Kind: backend.KindRemote, BaseURL: "https://remote-vault.invalid"}
	a.openNote("Alpha.md", true)
	if a.activeBuffer() == nil {
		t.Fatal("fixture note did not open")
	}
	suppressAsyncTestTimers(a)
	return a, root
}

func deliverRemoteRefreshResults(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range awaitAsyncTestResults(t, runAsyncTestCommand(cmd)) {
		switch msg.(type) {
		case tasksLoadedMsg, autosaveMsg, sessionSaveMsg, remotePollMsg, toastExpireMsg:
			// These surfaces and recurring events are independent of the read
			// reconciliation under test. Do not dispatch or reschedule them.
		default:
			a.Update(msg)
		}
	}
}

func TestRemoteIndexRefreshReloadsCleanOpenNote(t *testing.T) {
	a, root := newRemoteRefreshApp(t)
	buf := a.activeBuffer()
	want := "# Alpha\n\nUpdated remotely while this tab stayed open.\n"
	if err := os.WriteFile(filepath.Join(root, "Alpha.md"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := a.loadIndexCmd()().(indexLoadedMsg)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	_, cmd := a.Update(loaded)
	deliverRemoteRefreshResults(t, a, cmd)

	if got := buf.ed.Text(); got != want {
		t.Errorf("clean open note remained stale after remote index refresh: got %q, want %q", got, want)
	}
	if buf.dirty() {
		t.Error("loading a remote change marked a clean note modified")
	}
	if a.activeBuffer() != buf {
		t.Error("remote refresh replaced the open tab's buffer")
	}
}

func TestRemoteIndexRefreshPreservesDirtyNoteAndDetectsConflict(t *testing.T) {
	a, root := newRemoteRefreshApp(t)
	buf := a.activeBuffer()
	local := "# Alpha\n\nUnsaved work in this terminal.\n"
	remote := "# Alpha\n\nA concurrent edit from another client.\n"
	buf.ed.ReplaceText(local)
	if err := os.WriteFile(filepath.Join(root, "Alpha.md"), []byte(remote), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := a.loadIndexCmd()().(indexLoadedMsg)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	suppressAsyncTestTimers(a)
	_, cmd := a.Update(loaded)
	deliverRemoteRefreshResults(t, a, cmd)

	if got := buf.ed.Text(); got != local {
		t.Errorf("remote refresh discarded unsaved work: got %q", got)
	}
	if !buf.diskChanged {
		t.Error("remote refresh did not flag concurrent external changes")
	}
	if !buf.dirty() {
		t.Error("remote refresh treated unsaved work as saved")
	}
}

func TestExternalChangeConflictIsNotSilentlySaved(t *testing.T) {
	for _, save := range []string{"explicit save", "autosave"} {
		t.Run(save, func(t *testing.T) {
			a, root := newRemoteRefreshApp(t)
			buf := a.activeBuffer()
			local := "# Alpha\n\nUnsaved work in this terminal.\n"
			remote := "# Alpha\n\nA concurrent edit from another client.\n"
			buf.ed.ReplaceText(local)
			if err := os.WriteFile(filepath.Join(root, "Alpha.md"), []byte(remote), 0o644); err != nil {
				t.Fatal(err)
			}
			a.reloadBuffer(buf)
			if !buf.diskChanged {
				t.Fatal("fixture did not detect the external edit")
			}

			if save == "explicit save" {
				if err := a.saveBuffer(buf); err == nil {
					t.Error("saving a conflicting buffer reported success without resolving the conflict")
				}
			} else {
				buf.saveAt = time.Now().Add(-time.Second)
				suppressAsyncTestTimers(a)
				_, cmd := a.Update(autosaveMsg{})
				deliverRemoteRefreshResults(t, a, cmd)
			}

			got, err := os.ReadFile(filepath.Join(root, "Alpha.md"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != remote {
				t.Errorf("%s overwrote the external edit: got %q, want %q", save, string(got), remote)
			}
			if buf.ed.Text() != local || !buf.dirty() || !buf.diskChanged {
				t.Errorf("%s did not preserve the unsaved conflict for resolution", save)
			}
		})
	}
}
