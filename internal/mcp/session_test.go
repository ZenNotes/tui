package mcp

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
)

// openedBackend stands in for a real backend: the session only hands it
// out, so which target opened it is all a test needs to see.
type openedBackend struct {
	backend.Backend
	target backend.Target
}

type sessionHarness struct {
	*session
	mu      sync.Mutex
	current backend.Target
	fail    error
	openErr error
	opens   int
}

func newSessionHarness(start backend.Target) *sessionHarness {
	h := &sessionHarness{current: start}
	h.session = &session{
		resolve: func() (backend.Target, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.current, h.fail
		},
		open: func(t backend.Target) (Backend, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.openErr != nil {
				return nil, h.openErr
			}
			h.opens++
			return &openedBackend{target: t}, nil
		},
	}
	return h
}

func (h *sessionHarness) set(t backend.Target) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current = t
}

func (h *sessionHarness) mustRunOn(t *testing.T, confirm bool, want backend.Target) *backend.Target {
	t.Helper()
	b, previous, err := h.backendFor(confirm)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if got := b.(*openedBackend).target; got != want {
		t.Fatalf("ran on %+v, want %+v", got, want)
	}
	return previous
}

func TestSessionFollowsTheVaultBetweenCalls(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, Name: "home", BaseURL: "https://notes.example.com", AuthToken: "tok-old-secret"}
	local := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	h := newSessionHarness(server)

	if previous := h.mustRunOn(t, false, server); previous != nil {
		t.Fatalf("the first call left nothing, got %+v", previous)
	}
	h.mustRunOn(t, false, server)
	if h.opens != 1 {
		t.Fatalf("an unchanged vault reopened: %d opens", h.opens)
	}

	// A new token for the same server is not a switch.
	rotated := server
	rotated.AuthToken = "tok-rotated-secret"
	h.set(rotated)
	h.mustRunOn(t, false, rotated)
	if h.opens != 2 {
		t.Fatalf("a new token must reopen the backend: %d opens", h.opens)
	}

	// The app moves to a local vault: every call stops, the whole batch.
	h.set(local)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = h.backendFor(false)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		var switched *switchError
		if !errors.As(err, &switched) {
			t.Fatalf("call %d after the switch: %v, want a switchError", i, err)
		}
		msg := err.Error()
		if !strings.Contains(msg, `the server "home" at https://notes.example.com`) || !strings.Contains(msg, "the local vault "+local.Root) || !strings.Contains(msg, "vault_info") {
			t.Fatalf("message must name both vaults and the way on: %s", msg)
		}
		if strings.Contains(msg, "secret") {
			t.Fatalf("message leaks a token: %s", msg)
		}
	}
	if h.opens != 2 {
		t.Fatalf("a refused call opened the new vault: %d opens", h.opens)
	}

	// vault_info confirms the new vault and reports the one it left.
	previous := h.mustRunOn(t, true, local)
	if previous == nil || *previous != rotated {
		t.Fatalf("vault_info must report the previous vault, got %+v", previous)
	}
	h.mustRunOn(t, false, local)
	if previous := h.mustRunOn(t, true, local); previous != nil {
		t.Fatalf("a second vault_info reported a switch: %+v", previous)
	}
}

func TestSessionSwitchAndBackNeedsNoConfirmation(t *testing.T) {
	a := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	b := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	h := newSessionHarness(a)
	h.mustRunOn(t, false, a)
	h.set(b)
	if _, _, err := h.backendFor(false); err == nil {
		t.Fatal("a switch must stop the call")
	}
	h.set(a)
	h.mustRunOn(t, false, a)
	if h.opens != 1 {
		t.Fatalf("returning to the same vault reopened it: %d opens", h.opens)
	}
}

func TestSessionFailuresLeaveTheVaultInPlace(t *testing.T) {
	a := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	b := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	h := newSessionHarness(a)

	// Nothing resolves yet: the error is reported and nothing is pinned.
	h.fail = errors.New("no vault yet")
	if _, _, err := h.backendFor(false); err == nil || err.Error() != "no vault yet" {
		t.Fatalf("resolution error: %v", err)
	}
	h.fail = nil
	if previous := h.mustRunOn(t, false, a); previous != nil {
		t.Fatalf("a first success after failures is not a switch: %+v", previous)
	}

	h.fail = errors.New("config unreadable")
	if _, _, err := h.backendFor(true); err == nil {
		t.Fatal("a resolution error must reach the caller")
	}
	h.fail = nil
	h.mustRunOn(t, false, a)

	// vault_info cannot open the new vault: the session stays where it was.
	h.set(b)
	h.openErr = errors.New("not a directory")
	if _, _, err := h.backendFor(true); err == nil || err.Error() != "not a directory" {
		t.Fatalf("open error: %v", err)
	}
	var switched *switchError
	if _, _, err := h.backendFor(false); !errors.As(err, &switched) {
		t.Fatalf("still unconfirmed, want a switchError: %v", err)
	}
	h.openErr = nil
	if previous := h.mustRunOn(t, true, b); previous == nil || *previous != a {
		t.Fatalf("vault_info after the open error: previous %+v", previous)
	}
}
