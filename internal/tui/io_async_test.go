package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

type slowIOBackend struct {
	backend.Backend
	searchStarted chan struct{}
	searchRelease <-chan struct{}
	writeStarted  chan string
	writeRelease  <-chan struct{}
	readStarted   chan string
	readRelease   <-chan struct{}
	readErr       error
}

func (b *slowIOBackend) ReadNote(ctx context.Context, path string) (vault.NoteContent, error) {
	if b.readRelease != nil {
		select {
		case b.readStarted <- path:
		default:
		}
		select {
		case <-b.readRelease:
		case <-ctx.Done():
			return vault.NoteContent{}, ctx.Err()
		}
	}
	if b.readErr != nil {
		return vault.NoteContent{}, b.readErr
	}
	return b.Backend.ReadNote(ctx, path)
}

func (b *slowIOBackend) SearchText(ctx context.Context, query string, limit int) ([]vault.TextSearchMatch, error) {
	if b.searchRelease != nil {
		b.searchStarted <- struct{}{}
		select {
		case <-b.searchRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.Backend.SearchText(ctx, query, limit)
}

func (b *slowIOBackend) WriteNote(ctx context.Context, path, text string) (vault.NoteMeta, error) {
	if b.writeRelease != nil {
		b.writeStarted <- text
		select {
		case <-b.writeRelease:
		case <-ctx.Done():
			return vault.NoteMeta{}, ctx.Err()
		}
	}
	return b.Backend.WriteNote(ctx, path, text)
}

func suppressAsyncTestTimers(a *App) {
	a.pendingCmds = nil
	a.pollArmed = true
	a.sessionArmed = true
	a.autosaveArmed = true
}

func asyncTestGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	return gate, release
}

// A blocked backend is released before reporting a failure, so even the
// synchronous implementation finishes cleanly without leaking a test worker.
func requirePromptIOAction(t *testing.T, action func() tea.Cmd, release func()) tea.Cmd {
	t.Helper()
	returned := make(chan tea.Cmd, 1)
	go func() { returned <- action() }()
	select {
	case cmd := <-returned:
		return cmd
	case <-time.After(500 * time.Millisecond):
		release()
		<-returned
		t.Fatal("UI action blocked on backend I/O instead of returning a command")
		return nil
	}
}

// The fixture suppresses recurring timers before collecting the immediate
// command tree. Results are delivered separately on the model goroutine.
func collectAsyncTestResults(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var results []tea.Msg
		for _, child := range batch {
			results = append(results, collectAsyncTestResults(child)...)
		}
		return results
	}
	return []tea.Msg{msg}
}

func runAsyncTestCommand(cmd tea.Cmd) <-chan []tea.Msg {
	results := make(chan []tea.Msg, 1)
	go func() { results <- collectAsyncTestResults(cmd) }()
	return results
}

func awaitAsyncTestResults(t *testing.T, results <-chan []tea.Msg) []tea.Msg {
	t.Helper()
	select {
	case messages := <-results:
		return messages
	case <-time.After(2 * time.Second):
		t.Fatal("backend command did not finish after its gate opened")
		return nil
	}
}

func TestTextSearchReturnsPromptlyAndDismissesWhileBackendIsSlow(t *testing.T) {
	a, _ := newRemoteRefreshApp(t)
	gate, release := asyncTestGate(t)
	b := &slowIOBackend{Backend: a.backend, searchStarted: make(chan struct{}, 1), searchRelease: gate}
	a.backend = b
	a.opts.Backend = b
	suppressAsyncTestTimers(a)
	cmd := requirePromptIOAction(t, func() tea.Cmd {
		a.openTextSearch("Saved")
		return a.flush()
	}, release)
	results := runAsyncTestCommand(cmd)
	select {
	case <-b.searchStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("opening text search did not schedule a backend search")
	}

	requirePromptIOAction(t, func() tea.Cmd {
		_, next := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
		return next
	}, release)
	if a.overlay != nil {
		t.Fatal("search could not be dismissed while results were loading")
	}
	release()
	for _, msg := range awaitAsyncTestResults(t, results) {
		a.Update(msg)
	}
	if a.overlay != nil {
		t.Error("late search results reopened the dismissed palette")
	}
}

func TestAutosaveReturnsPromptlyAndKeepsEditsMadeDuringSaveDirty(t *testing.T) {
	a, root := newRemoteRefreshApp(t)
	gate, release := asyncTestGate(t)
	b := &slowIOBackend{Backend: a.backend, writeStarted: make(chan string, 1), writeRelease: gate}
	a.backend = b
	a.opts.Backend = b
	buf := a.activeBuffer()
	snapshot := "# Alpha\n\nThe snapshot being saved.\n"
	newer := "# Alpha\n\nMore typing while the save was in flight.\n"
	buf.ed.ReplaceText(snapshot)
	buf.saveAt = time.Now().Add(-time.Second)
	suppressAsyncTestTimers(a)
	cmd := requirePromptIOAction(t, func() tea.Cmd {
		_, next := a.Update(autosaveMsg{})
		return next
	}, release)
	results := runAsyncTestCommand(cmd)
	select {
	case got := <-b.writeStarted:
		if got != snapshot {
			t.Errorf("save captured %q, want the original snapshot", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("autosave did not schedule a backend write")
	}

	requirePromptIOAction(t, func() tea.Cmd {
		a.openHelp()
		return nil
	}, release)
	if a.activeTab().path != tabHelp {
		t.Fatal("could not navigate to Help while autosave was in flight")
	}
	buf.ed.ReplaceText(newer)
	release()
	for _, msg := range awaitAsyncTestResults(t, results) {
		a.Update(msg)
	}
	if buf.ed.Text() != newer || !buf.dirty() {
		t.Error("save completion discarded newer edits or marked them saved")
	}
	if buf.savedText != snapshot {
		t.Errorf("saved baseline = %q, want the snapshot actually written", buf.savedText)
	}
	got, err := os.ReadFile(filepath.Join(root, "Alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != snapshot {
		t.Errorf("backend saved %q, want the in-flight snapshot", string(got))
	}
}

func TestQueuedAutosaveCannotWriteOrModifyTheNextVault(t *testing.T) {
	a, _ := newRemoteRefreshApp(t)
	gate, release := asyncTestGate(t)
	b := &slowIOBackend{Backend: a.backend, writeStarted: make(chan string, 1), writeRelease: gate}
	a.backend = b
	a.opts.Backend = b
	a.activeBuffer().ed.ReplaceText("# Old vault\n\nQueued old-vault edit.\n")
	a.activeBuffer().saveAt = time.Now().Add(-time.Second)
	suppressAsyncTestTimers(a)
	cmd := requirePromptIOAction(t, func() tea.Cmd {
		_, next := a.Update(autosaveMsg{})
		return next
	}, release)

	nextRoot := newSessionParityVault(t, "Alpha.md")
	next := newSessionParityApp(t, nextRoot)
	next.openNote("Alpha.md", true)
	newBuffer := next.activeBuffer()
	onDisk := newBuffer.ed.Text()
	newBuffer.ed.ReplaceText("# Next vault\n\nUnsaved work belongs to this vault.\n")
	newer := newBuffer.ed.Text()
	suppressAsyncTestTimers(next)
	// Keep the same model identity while installing a successfully switched
	// workspace; old commands must not consult its new backend or buffers.
	*a = *next
	release()
	for _, msg := range awaitAsyncTestResults(t, runAsyncTestCommand(cmd)) {
		a.Update(msg)
	}

	got, err := os.ReadFile(filepath.Join(nextRoot, "Alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != onDisk {
		t.Errorf("old-vault command wrote into the next vault: got %q", string(got))
	}
	if a.activeBuffer() != newBuffer || newBuffer.ed.Text() != newer || !newBuffer.dirty() || newBuffer.savedText != onDisk {
		t.Error("old-vault completion modified the next vault's open buffer")
	}
}

func openAsyncTestNoteFromKeyboard(t *testing.T, a *App, path string, release func()) tea.Cmd {
	t.Helper()
	a.openNoteSearch(path)
	p, ok := a.overlay.(*palette)
	if !ok || len(p.filtered) != 1 || p.filtered[0].id != path {
		t.Fatalf("note picker did not select %q", path)
	}
	suppressAsyncTestTimers(a)
	return requirePromptIOAction(t, func() tea.Cmd {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return cmd
	}, release)
}

func TestKeyboardNoteOpenRemainsResponsiveWhileReadIsBlocked(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	gate, release := asyncTestGate(t)
	b := &slowIOBackend{Backend: a.backend, readStarted: make(chan string, 1), readRelease: gate}
	a.backend, a.opts.Backend = b, b
	cmd := openAsyncTestNoteFromKeyboard(t, a, "Alpha.md", release)
	buf := a.activeBuffer()
	if buf == nil || !buf.loading {
		t.Fatal("keyboard open did not expose a loading note")
	}
	results := runAsyncTestCommand(cmd)
	select {
	case <-b.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("keyboard open did not schedule the note read")
	}

	requirePromptIOAction(t, func() tea.Cmd {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		return cmd
	}, release)
	if _, ok := a.overlay.(*palette); !ok {
		t.Fatal("note navigation could not open while the current note was loading")
	}
	release()
	for _, msg := range awaitAsyncTestResults(t, results) {
		a.Update(msg)
	}
	if buf.loading || buf.loadErr != nil || buf.ed.Text() != "# Alpha.md\n\nSaved content.\n" || buf.dirty() {
		t.Errorf("read did not populate a clean note: loading %v, error %v, text %q", buf.loading, buf.loadErr, buf.ed.Text())
	}
	if _, ok := a.overlay.(*palette); !ok {
		t.Error("read completion displaced the navigation palette")
	}
}

func TestFailedKeyboardNoteReadCannotOverwriteOrRecreateTheFile(t *testing.T) {
	for _, failure := range []string{"unavailable", "deleted"} {
		t.Run(failure, func(t *testing.T) {
			root := newSessionParityVault(t, "Alpha.md")
			a := newSessionParityApp(t, root)
			gate, release := asyncTestGate(t)
			b := &slowIOBackend{Backend: a.backend, readStarted: make(chan string, 1), readRelease: gate}
			if failure == "unavailable" {
				b.readErr = errors.New("backend temporarily unavailable")
			}
			a.backend, a.opts.Backend = b, b
			cmd := openAsyncTestNoteFromKeyboard(t, a, "Alpha.md", release)
			buf := a.activeBuffer()
			if buf == nil {
				t.Fatal("keyboard open created no loading buffer")
			}
			if failure == "deleted" {
				if err := os.Remove(filepath.Join(root, "Alpha.md")); err != nil {
					t.Fatal(err)
				}
			}
			release()
			for _, msg := range awaitAsyncTestResults(t, runAsyncTestCommand(cmd)) {
				a.Update(msg)
			}
			if buf.loadErr == nil || buf.loading {
				t.Fatal("failed read did not leave a completed error state")
			}
			buf.ed.ReplaceText("Text typed after the failed load.")
			if err := a.saveBuffer(buf); err == nil {
				t.Error("explicit save accepted a buffer whose initial read failed")
			}
			buf.saveAt = time.Now().Add(-time.Second)
			suppressAsyncTestTimers(a)
			_, saveCmd := a.Update(autosaveMsg{})
			for _, msg := range awaitAsyncTestResults(t, runAsyncTestCommand(saveCmd)) {
				a.Update(msg)
			}
			got, err := os.ReadFile(filepath.Join(root, "Alpha.md"))
			if failure == "deleted" {
				if !os.IsNotExist(err) {
					t.Errorf("failed read recreated the deleted file: content %q, error %v", got, err)
				}
			} else if err != nil || string(got) != "# Alpha.md\n\nSaved content.\n" {
				t.Errorf("failed read replaced existing note: content %q, error %v", got, err)
			}
		})
	}
}

func TestStaleBufferReadIsIgnoredAfterNoteRepoint(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md", "Renamed.md")
	a := newSessionParityApp(t, root)
	a.openNote("Alpha.md", true)
	buf := a.activeBuffer()
	late := a.readBufferCmd(buf)()
	a.repointNote("Alpha.md", "Renamed.md")
	want := buf.ed.Text()
	if want == "# Alpha.md\n\nSaved content.\n" {
		t.Fatal("fixture did not repoint to different note content")
	}
	a.Update(late)
	if a.activeBuffer() != buf || buf.path != "Renamed.md" || buf.ed.Text() != want || buf.dirty() {
		t.Error("late old-path read changed the repointed note")
	}
}

func TestStaleWorkspaceReadsAndIndexMessagesAreIgnored(t *testing.T) {
	oldRoot := newSessionParityVault(t, "Alpha.md", "Old.md")
	a := newSessionParityApp(t, oldRoot)
	a.openNote("Alpha.md", true)
	oldBuffer := a.activeBuffer()
	readCmd := a.readBufferCmd(oldBuffer)
	indexCmd := a.loadIndexCmd()
	tasksCmd := a.loadTasksCmd()
	oldEpoch := a.epoch
	nextRoot := newSessionParityVault(t, "Alpha.md", "Next.md")
	next := newSessionParityApp(t, nextRoot)
	next.openNote("Alpha.md", true)
	nextBuffer, nextIndex := next.activeBuffer(), next.idx
	nextBuffer.ed.ReplaceText("Unsaved text in the next vault.")
	nextIndex.tasks = []vault.Task{{ID: "next-vault-task"}}
	suppressAsyncTestTimers(next)
	*a = *next

	for _, msg := range []tea.Msg{readCmd(), indexCmd(), tasksCmd(), indexLoadedMsg{epoch: oldEpoch, err: errors.New("old vault unavailable")}} {
		a.Update(msg)
	}
	if a.idx != nextIndex || a.loadErr != nil || len(a.idx.tasks) != 1 || a.idx.tasks[0].ID != "next-vault-task" {
		t.Error("old-vault index or task response replaced current workspace data")
	}
	if a.activeBuffer() != nextBuffer || nextBuffer.ed.Text() != "Unsaved text in the next vault." || !nextBuffer.dirty() {
		t.Error("old-vault read replaced or modified the current note")
	}
}

func TestCtrlSpaceRoutesToSidebarMarkAndInsertCompletion(t *testing.T) {
	t.Run("sidebar mark", func(t *testing.T) {
		root := newSessionParityVault(t, "Alpha.md")
		a := newSessionParityApp(t, root)
		a.focus = focusSidebar
		found := false
		for i, row := range a.sidebar.rows {
			if row.path == "Alpha.md" {
				a.sidebar.cursor = i
				found = true
				break
			}
		}
		if !found {
			t.Fatal("fixture note is absent from sidebar")
		}
		// Terminals encode Ctrl+Space as the same NUL byte as Ctrl+@.
		a.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
		if !a.markedNotes["Alpha.md"] {
			t.Error("Ctrl+Space did not mark the selected sidebar note")
		}
	})
	t.Run("insert completion", func(t *testing.T) {
		root := newSessionParityVault(t, "Alpha.md")
		a := newSessionParityApp(t, root)
		a.openNote("Alpha.md", true)
		buf := a.activeBuffer()
		buf.ed.ReplaceText("> [!NO")
		buf.ed.HandleKey(vim.R('A'))
		if buf.ed.Mode() != vim.ModeInsert {
			t.Fatal("fixture did not enter Insert mode")
		}
		a.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
		p, ok := a.overlay.(*palette)
		if !ok || p.title != "Markdown completion" {
			t.Fatal("Ctrl+Space in Insert mode did not open Markdown completion")
		}
		if len(p.filtered) == 0 || p.filtered[0].id != "NOTE" {
			t.Errorf("callout completion did not offer NOTE: %+v", p.filtered)
		}
	})
}
