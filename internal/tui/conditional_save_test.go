package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/remote/remotetest"
)

const conditionalNote = "inbox/Alpha.md"

func newConditionalApp(t *testing.T, server *remotetest.Server) *App {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	target := backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}
	b, err := backend.New(target, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(context.Background(), Options{Backend: b, Target: target}, config.DefaultPrefs(), true)
	if loaded := a.loadIndexCmd()().(indexLoadedMsg); loaded.err == nil {
		a.idx = loaded.idx
		a.sidebar.rebuild(a)
	}
	a.openNote(conditionalNote, true)
	buf := a.activeBuffer()
	if buf == nil {
		t.Fatal("note did not open")
	}
	if buf.loading {
		a.applyBufferRead(a.readBufferCmd(buf)().(bufferReadMsg))
	}
	suppressAsyncTestTimers(a)
	return a
}

// finishQueuedSave runs the queued commands side by side, so a toast's
// expiry timer does not hold the test, and delivers the save's result.
func finishQueuedSave(t *testing.T, a *App) {
	t.Helper()
	pending := a.pendingCmds
	a.pendingCmds = nil
	results := make(chan saveResultMsg, len(pending))
	for _, cmd := range pending {
		go func() {
			if m, ok := cmd().(saveResultMsg); ok {
				results <- m
			}
		}()
	}
	select {
	case m := <-results:
		a.finishSave(m)
	case <-time.After(2 * time.Second):
		t.Fatal("no save was queued")
	}
}

// A save that another device's save beats to the server, after the check
// that the note is unchanged and before the write, is refused there and
// shows as a conflict. Nothing is written over, and the buffer keeps the
// user's text.
func TestASaveRacingAnotherDeviceEndsInAConflictNotAnOverwrite(t *testing.T) {
	opened := "# Alpha\n\nSaved content.\n"
	theirs := opened + "\nTheir line.\n"
	mine := opened + "\nMy line.\n"
	server := remotetest.New(t, true, map[string]string{conditionalNote: opened})
	a := newConditionalApp(t, server)
	buf := a.activeBuffer()
	if buf.ed.Text() != opened {
		t.Fatalf("opened %q", buf.ed.Text())
	}
	raced := false
	server.BeforeWrite = func(string) {
		if !raced {
			raced = true
			server.Set(conditionalNote, theirs)
		}
	}
	buf.ed.ReplaceText(mine)
	if err := a.writeBuffer(buf); !errors.Is(err, errNoteConflict) {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if got, _ := server.Body(conditionalNote); got != theirs {
		t.Fatalf("their save was written over: %q", got)
	}
	if buf.ed.Text() != mine || !buf.dirty() {
		t.Fatalf("the buffer lost the edit: %q", buf.ed.Text())
	}
	writes := server.Writes()
	if len(writes) != 1 || !writes[0].Refused || writes[0].Base != remotetest.Sha256(opened) {
		t.Fatalf("writes: %+v", writes)
	}

	// The same through autosave: the result marks the conflict and holds
	// further saves until it is resolved.
	server.Set(conditionalNote, opened)
	raced = false
	buf.savedText = opened
	a.queueSave(buf)
	finishQueuedSave(t, a)
	if !buf.diskChanged {
		t.Fatal("autosave did not mark the conflict")
	}
	if got, _ := server.Body(conditionalNote); got != theirs {
		t.Fatalf("autosave wrote over their save: %q", got)
	}
	if a.autosavePending(buf) {
		t.Fatal("autosave would try again over the conflict")
	}

	// Keep both: their version beside the note, then mine on top of the
	// version just read.
	server.BeforeWrite = nil
	a.keepBothVersions(buf)
	finishQueuedSave(t, a)
	if got, _ := server.Body(conditionalNote); got != mine {
		t.Fatalf("the note holds %q, want the local edits", got)
	}
	copyBody, ok := server.Body("inbox/Alpha (server copy).md")
	if !ok || copyBody != theirs {
		t.Fatalf("server copy: %q (exists %v)", copyBody, ok)
	}
	if buf.diskChanged || buf.dirty() {
		t.Fatalf("conflict not resolved: diskChanged %v dirty %v", buf.diskChanged, buf.dirty())
	}
	last := server.Writes()[len(server.Writes())-1]
	if last.Path != conditionalNote || last.Base != remotetest.Sha256(theirs) {
		t.Fatalf("the final save was not on the version read: %+v", last)
	}
}

// A server without conditional writes saves exactly as before: the check,
// then a write with no base.
func TestASaveToAnOlderServerSendsNoBase(t *testing.T) {
	opened := "# Alpha\n\nSaved content.\n"
	server := remotetest.New(t, false, map[string]string{conditionalNote: opened})
	a := newConditionalApp(t, server)
	buf := a.activeBuffer()
	buf.ed.ReplaceText(opened + "More.\n")
	if err := a.writeBuffer(buf); err != nil {
		t.Fatal(err)
	}
	writes := server.Writes()
	if len(writes) != 1 || writes[0].SentBase {
		t.Fatalf("writes: %+v", writes)
	}
	if got, _ := server.Body(conditionalNote); !strings.HasSuffix(got, "More.\n") {
		t.Fatalf("got %q", got)
	}
}
