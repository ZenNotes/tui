package mcp

import (
	"fmt"
	"sync"

	"github.com/ZenNotes/tui/internal/backend"
)

// session is the vault the tools run against. It is resolved again before
// every call, so the server follows the desktop app (or zn's own default)
// the way a fresh zn process does; pinning the first vault for the life of
// the process left an agent working on a server after the app had moved
// back to a local vault, until the client restarted the server.
//
// A switch still never redirects work silently. An agent that read notes in
// one vault and writes after the user switched would change a note at the
// same path in the other one, so once the vault changes, every call except
// vault_info stops with a switchError until vault_info confirms the new
// vault. That holds for a whole batch of parallel calls, not only the first.
type session struct {
	resolve func() (backend.Target, error)
	open    func(backend.Target) (Backend, error)

	mu      sync.Mutex
	target  backend.Target
	backend Backend
}

// switchError says the vault changed after this session last served a call.
type switchError struct{ from, to backend.Target }

func (e *switchError) Error() string {
	return fmt.Sprintf("The ZenNotes vault changed since this session's last call: it was %s and is now %s. Nothing ran. Call vault_info to confirm the new vault and tell the user, then re-read anything you meant to change before you retry.", describeTarget(e.from), describeTarget(e.to))
}

// backendFor resolves the vault for one call. confirm marks vault_info, the
// only call that moves the session to a new vault; previous is the vault it
// moved away from, nil when the vault did not change.
func (s *session) backendFor(confirm bool) (b Backend, previous *backend.Target, err error) {
	target, err := s.resolve()
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backend != nil && !backend.SameVault(s.target, target) {
		if !confirm {
			return nil, nil, &switchError{from: s.target, to: target}
		}
		left := s.target
		previous = &left
	}
	// A new token or profile name for the same vault reopens quietly.
	if s.backend == nil || s.target != target {
		opened, err := s.open(target)
		if err != nil {
			return nil, nil, err
		}
		s.target, s.backend = target, opened
	}
	return s.backend, previous, nil
}

// describeTarget names a vault in a sentence. The app often names a saved
// server after its host already ("Server Vault (10.0.0.5:7878)"), so the name
// is quoted and the URL follows "at" rather than a second parenthesis.
func describeTarget(t backend.Target) string {
	if t.Kind != backend.KindRemote {
		return "the local vault " + t.Root
	}
	if t.Name != "" {
		return fmt.Sprintf("the server %q at %s", t.Name, t.BaseURL)
	}
	return "the server at " + t.BaseURL
}
