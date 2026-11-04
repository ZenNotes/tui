package mcp

import (
	"context"
	"fmt"
	"path/filepath"
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
//
// The session moves only once vault_info has its answer ready, so a vault_info
// that fails leaves the stop in place, and any call that began before the move
// stops too, vault_info beside it in a batch included. The one call that can
// pass is one that begins after the move but was sent before the client had
// the answer.
//
// A self-hosted server can change its own vault with the URL staying the same,
// so each call on a server first asks which vault it is serving: one request.
// The answer can go stale before the call's own requests reach the server. A
// switch landing there sends those requests to the other vault, and a tool that
// reads a note and writes it back (replace_in_note, for one) then overwrites
// the other vault's note with this one's body and reports success. Closing that
// would take the server refusing a request meant for another vault.
type session struct {
	resolve func() (backend.Target, error)
	open    func(backend.Target) (Backend, error)
	// served fills in the vault a server is serving (backend.Served). Nil asks
	// nothing, for a test about the session alone.
	served func(context.Context, backend.Target) (backend.Target, error)
	// scoped keeps absolute paths out of what a switch reports.
	scoped bool

	mu      sync.Mutex
	target  backend.Target
	backend Backend
}

// switchError says the vault changed after this session last served a call.
type switchError struct {
	from, to backend.Target
	scoped   bool
}

func (e *switchError) Error() string {
	from, to := describeTarget(e.from, e.scoped), describeTarget(e.to, e.scoped)
	if from == to {
		to = "a different vault with the same name"
	}
	return fmt.Sprintf("The ZenNotes vault changed since this session's last call: it was %s and is now %s. Nothing ran. Call vault_info to confirm the new vault and tell the user, then re-read anything you meant to change before you retry.", from, to)
}

// vaultMove is a switch vault_info is confirming: the vault it is leaving and
// the one it runs on. The session stays where it is until finishMove.
type vaultMove struct {
	from, to backend.Target
	opened   Backend
}

// finishMove moves the session once vault_info has its answer ready.
func (s *session) finishMove(m *vaultMove) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target, s.backend = m.to, m.opened
}

// backendFor resolves the vault for one call. confirm marks vault_info, the
// only call that can move the session to a new vault; move is non-nil when it
// would, for the caller to finish once the call has succeeded. When a server
// cannot say which vault it is serving, the call stops with that error, and the
// backend comes back too so the error can name the server.
func (s *session) backendFor(ctx context.Context, confirm bool) (b Backend, move *vaultMove, err error) {
	target, err := s.resolve()
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	entered, pinned := s.target, s.backend != nil
	s.mu.Unlock()
	// A call that will stop anyway because the session is on another server or
	// a local vault stops without contacting the new one. Not under the lock:
	// one slow answer should not hold up a whole batch.
	if s.served != nil && (confirm || !pinned || backend.SameServer(entered, target)) {
		asked, err := s.served(ctx, target)
		if err != nil {
			opened, openErr := s.open(target)
			if openErr != nil {
				return nil, nil, err
			}
			return opened, nil, err
		}
		target = asked
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A vault_info may have finished while this call waited for its answer. The
	// call began on the old vault, so it stops even though the session has moved.
	from := s.target
	if pinned && !backend.SameVault(entered, target) {
		from = entered
	}
	if s.backend != nil && !backend.SameVault(from, target) {
		if !confirm {
			return nil, nil, &switchError{from: from, to: target, scoped: s.scoped}
		}
		opened, err := s.open(target)
		if err != nil {
			return nil, nil, err
		}
		return opened, &vaultMove{from: from, to: target, opened: opened}, nil
	}
	// A new token or profile name for the same vault reopens quietly.
	if s.backend == nil || s.target != target {
		opened, err := s.open(target)
		if err != nil {
			return nil, nil, err
		}
		s.target, s.backend = target, opened
	}
	return s.backend, nil, nil
}

// describeTarget names a vault in a sentence. The app often names a saved
// server after its host already ("Server Vault (10.0.0.5:7878)"), so the name
// is quoted and the URL follows "at" rather than a second parenthesis. A
// server's vault goes by its name, never its path; a client with scoped access
// gets no absolute path for a local vault either.
func describeTarget(t backend.Target, scoped bool) string {
	if t.Kind != backend.KindRemote {
		if scoped {
			return fmt.Sprintf("the local vault %q", filepath.Base(t.Root))
		}
		return "the local vault " + t.Root
	}
	server := "the server at " + t.BaseURL
	if t.Name != "" {
		server = fmt.Sprintf("the server %q at %s", t.Name, t.BaseURL)
	}
	if t.VaultName != "" {
		return fmt.Sprintf("the vault %q on %s", t.VaultName, server)
	}
	return server
}
