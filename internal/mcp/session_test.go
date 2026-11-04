package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
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

// mustRunOn is one call that succeeds, so a vault_info finishes its move here.
// It returns the vault that move left, nil when there was none.
func (h *sessionHarness) mustRunOn(t *testing.T, confirm bool, want backend.Target) *backend.Target {
	t.Helper()
	b, move, err := h.backendFor(context.Background(), confirm)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if got := b.(*openedBackend).target; got != want {
		t.Fatalf("ran on %+v, want %+v", got, want)
	}
	if move == nil {
		return nil
	}
	h.finishMove(move)
	return &move.from
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
			_, _, errs[i] = h.backendFor(context.Background(), false)
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
	if _, _, err := h.backendFor(context.Background(), false); err == nil {
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
	if _, _, err := h.backendFor(context.Background(), false); err == nil || err.Error() != "no vault yet" {
		t.Fatalf("resolution error: %v", err)
	}
	h.fail = nil
	if previous := h.mustRunOn(t, false, a); previous != nil {
		t.Fatalf("a first success after failures is not a switch: %+v", previous)
	}

	h.fail = errors.New("config unreadable")
	if _, _, err := h.backendFor(context.Background(), true); err == nil {
		t.Fatal("a resolution error must reach the caller")
	}
	h.fail = nil
	h.mustRunOn(t, false, a)

	// vault_info cannot open the new vault: the session stays where it was.
	h.set(b)
	h.openErr = errors.New("not a directory")
	if _, _, err := h.backendFor(context.Background(), true); err == nil || err.Error() != "not a directory" {
		t.Fatalf("open error: %v", err)
	}
	var switched *switchError
	if _, _, err := h.backendFor(context.Background(), false); !errors.As(err, &switched) {
		t.Fatalf("still unconfirmed, want a switchError: %v", err)
	}
	h.openErr = nil
	if previous := h.mustRunOn(t, true, b); previous == nil || *previous != a {
		t.Fatalf("vault_info after the open error: previous %+v", previous)
	}
}

// fakeVaultServer is as much of a ZenNotes server as a vault switch touches.
type fakeVaultServer struct {
	*httptest.Server
	mu       sync.Mutex
	root     string
	name     string
	notes    []string
	requests []string
	// settingsDown fails /api/vault/settings, which vault_info needs after the
	// vault check has already passed.
	settingsDown bool
	// settingsReached and settingsHold, when set, pause /api/vault/settings:
	// vault_info is then mid-flight, past the check and not yet answered.
	settingsReached chan struct{}
	settingsHold    chan struct{}
}

func newFakeVaultServer(t *testing.T, root, name string, notes ...string) *fakeVaultServer {
	t.Helper()
	f := &fakeVaultServer{root: root, name: name, notes: notes}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/vault/settings" {
			f.mu.Lock()
			reached, hold := f.settingsReached, f.settingsHold
			f.settingsReached, f.settingsHold = nil, nil
			f.mu.Unlock()
			if hold != nil {
				close(reached)
				<-hold
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/api/vault/settings" && f.settingsDown {
			http.Error(w, "settings are down", http.StatusInternalServerError)
			return
		}
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		var out any
		switch r.URL.Path {
		case "/api/vault":
			out = map[string]string{"root": f.root, "name": f.name}
		case "/api/vault/settings":
			out = map[string]string{"primaryNotesLocation": "root"}
		case "/api/notes":
			metas := []vault.NoteMeta{}
			for _, n := range f.notes {
				metas = append(metas, vault.NoteMeta{Path: n, Title: path.Base(n), Folder: vault.FolderInbox})
			}
			out = metas
		case "/api/folders":
			out = []vault.FolderEntry{{Folder: vault.FolderInbox, Subpath: "Projects"}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.Close)
	return f
}

// selectVault is POST /api/vault/select: the URL stays, the vault does not.
func (f *fakeVaultServer) selectVault(root, name string, notes ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.root, f.name, f.notes, f.requests = root, name, notes, nil
}

func (f *fakeVaultServer) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func TestRunStopsWhenTheServerSwitchesItsVault(t *testing.T) {
	allow, err := NewScope("", false, []string{"Projects"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, scope := range map[string]*Scope{"unscoped": nil, "allow-path": &allow} {
		t.Run(name, func(t *testing.T) {
			server := newFakeVaultServer(t, "/srv/vaults/home", "Home", "Projects/a.md")
			cs := runScopedServer(t, func() (backend.Target, error) {
				return backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}, nil
			}, scope)

			if text, isErr := callTool(t, cs, "list_notes", nil); isErr || !strings.Contains(text, "Projects/a.md") {
				t.Fatalf("before the switch: %s", text)
			}

			// A matching --allow-path entry is no protection: Projects/ exists
			// in both vaults.
			server.selectVault("/srv/vaults/work", "Work", "Projects/other.md")
			calls := []struct {
				tool string
				args map[string]any
			}{
				{"list_notes", nil},
				{"read_note", map[string]any{"path": "Projects/other.md"}},
				{"write_note", map[string]any{"path": "Projects/a.md", "body": "planned for Home"}},
			}
			for _, c := range calls {
				text, isErr := callTool(t, cs, c.tool, c.args)
				want := `it was the vault "Home" on the server at ` + server.URL + ` and is now the vault "Work" on the server at ` + server.URL + `. Nothing ran. Call vault_info`
				if !isErr || !strings.Contains(text, want) {
					t.Fatalf("%s after the switch: error=%v %s", c.tool, isErr, text)
				}
				if strings.Contains(text, "/srv/vaults") {
					t.Fatalf("%s: the switch error names an absolute path: %s", c.tool, text)
				}
			}
			// Only the question reached the server, nothing of the calls.
			for _, request := range server.seen() {
				if request != "GET /api/vault" {
					t.Fatalf("a stopped call reached the server: %v", server.seen())
				}
			}

			text, isErr := callTool(t, cs, "vault_info", nil)
			if isErr {
				t.Fatalf("vault_info: %s", text)
			}
			var info struct {
				Notes string `json:"notes"`
			}
			if err := json.Unmarshal([]byte(text), &info); err != nil {
				t.Fatalf("vault_info payload: %v\n%s", err, text)
			}
			if !strings.HasPrefix(info.Notes, `The vault changed: until this call the session worked in the vault "Home" on the server at `+server.URL) {
				t.Fatalf("vault_info after the switch: %s", info.Notes)
			}
			if strings.Contains(text, "/srv/vaults/home") {
				t.Fatalf("vault_info names the old vault's path: %s", text)
			}
			if scope != nil && strings.Contains(text, "/srv/vaults") {
				t.Fatalf("a scoped vault_info names an absolute path: %s", text)
			}

			if text, isErr := callTool(t, cs, "list_notes", nil); isErr || !strings.Contains(text, "Projects/other.md") || strings.Contains(text, "Projects/a.md") {
				t.Fatalf("after vault_info: error=%v %s", isErr, text)
			}
			if text, _ := callTool(t, cs, "vault_info", nil); strings.Contains(text, "The vault changed") {
				t.Fatalf("the switch note repeats: %s", text)
			}
		})
	}
}

// A vault_info that fails told the assistant nothing, so the stop stays and
// the next vault_info still reports the switch.
func TestRunFailedVaultInfoKeepsTheStop(t *testing.T) {
	server := newFakeVaultServer(t, "/srv/vaults/home", "Home", "Projects/a.md")
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}, nil
	})
	if text, isErr := callTool(t, cs, "list_notes", nil); isErr {
		t.Fatalf("before the switch: %s", text)
	}
	server.selectVault("/srv/vaults/work", "Work", "Projects/other.md")

	server.mu.Lock()
	server.settingsDown = true
	server.mu.Unlock()
	if text, isErr := callTool(t, cs, "vault_info", nil); !isErr {
		t.Fatalf("vault_info with its settings down must fail: %s", text)
	}
	server.mu.Lock()
	server.settingsDown = false
	server.requests = nil
	server.mu.Unlock()

	text, isErr := callTool(t, cs, "write_note", map[string]any{"path": "Projects/a.md", "body": "planned for Home"})
	if !isErr || !strings.Contains(text, "The ZenNotes vault changed") {
		t.Fatalf("the write after a failed vault_info: error=%v %s", isErr, text)
	}
	for _, request := range server.seen() {
		if request != "GET /api/vault" {
			t.Fatalf("the stopped write reached the server: %v", server.seen())
		}
	}
	if text, isErr := callTool(t, cs, "vault_info", nil); isErr || !strings.Contains(text, `The vault changed: until this call the session worked in the vault \"Home\"`) {
		t.Fatalf("the next vault_info must still report the switch: error=%v %s", isErr, text)
	}
	if text, isErr := callTool(t, cs, "list_notes", nil); isErr || !strings.Contains(text, "Projects/other.md") {
		t.Fatalf("after a vault_info that worked: error=%v %s", isErr, text)
	}
}

// A vault_info still on its way has not told the assistant anything either, so
// a write sent alongside it stops.
func TestRunWriteBesideARunningVaultInfoStops(t *testing.T) {
	server := newFakeVaultServer(t, "/srv/vaults/home", "Home", "Projects/a.md")
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}, nil
	})
	if text, isErr := callTool(t, cs, "list_notes", nil); isErr {
		t.Fatalf("before the switch: %s", text)
	}
	server.selectVault("/srv/vaults/work", "Work", "Projects/other.md")

	reached, hold := make(chan struct{}), make(chan struct{})
	server.mu.Lock()
	server.settingsReached, server.settingsHold = reached, hold
	server.requests = nil
	server.mu.Unlock()
	type answer struct {
		text  string
		isErr bool
	}
	info := make(chan answer, 1)
	go func() {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "vault_info"})
		if err != nil {
			info <- answer{err.Error(), true}
			return
		}
		info <- answer{res.Content[0].(*sdk.TextContent).Text, res.IsError}
	}()
	<-reached

	text, isErr := callTool(t, cs, "write_note", map[string]any{"path": "Projects/a.md", "body": "planned for Home"})
	writes := 0
	for _, request := range server.seen() {
		if strings.HasPrefix(request, "POST ") {
			writes++
		}
	}
	close(hold)
	got := <-info
	if !isErr || !strings.Contains(text, "The ZenNotes vault changed") || writes != 0 {
		t.Fatalf("the write beside vault_info: error=%v writes=%d %s", isErr, writes, text)
	}
	if got.isErr || !strings.Contains(got.text, "The vault changed") {
		t.Fatalf("vault_info: error=%v %s", got.isErr, got.text)
	}
}

// The price of the gate, pinned: one request per call on a server.
func TestRunAsksTheServerOncePerCall(t *testing.T) {
	server := newFakeVaultServer(t, "/srv/vaults/home", "Home", "Projects/a.md")
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}, nil
	})
	if text, isErr := callTool(t, cs, "list_notes", nil); isErr {
		t.Fatalf("list_notes: %s", text)
	}
	if got := strings.Join(server.seen(), ", "); got != "GET /api/vault, GET /api/notes" {
		t.Fatalf("requests for one unscoped call: %s", got)
	}
}

// A client with scoped access gets no absolute path for a local vault either.
func TestRunKeepsLocalPathsFromAScopedClientOnASwitch(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	var mu sync.Mutex
	current := backend.Target{Kind: backend.KindLocal, Root: first}
	readOnly, err := NewScope("read", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := runScopedServer(t, func() (backend.Target, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}, &readOnly)
	if text, isErr := callTool(t, cs, "list_notes", nil); isErr {
		t.Fatalf("before the switch: %s", text)
	}
	mu.Lock()
	current = backend.Target{Kind: backend.KindLocal, Root: second}
	mu.Unlock()

	text, isErr := callTool(t, cs, "list_notes", nil)
	if !isErr || !strings.Contains(text, "The ZenNotes vault changed") {
		t.Fatalf("after the switch: error=%v %s", isErr, text)
	}
	if strings.Contains(text, first) || strings.Contains(text, second) || !strings.Contains(text, filepath.Base(second)) {
		t.Fatalf("the switch error must name the folders, not their paths: %s", text)
	}
	text, isErr = callTool(t, cs, "vault_info", nil)
	if isErr || !strings.Contains(text, "The vault changed") {
		t.Fatalf("vault_info: error=%v %s", isErr, text)
	}
	if strings.Contains(text, first) || strings.Contains(text, second) {
		t.Fatalf("vault_info names an absolute path: %s", text)
	}
}

// serverHarness is a session whose server answers from fields a test can set.
type serverHarness struct {
	*sessionHarness
	root, name string
	askErr     error
	asked      int
}

func newServerHarness(start backend.Target, root, name string) *serverHarness {
	h := &serverHarness{sessionHarness: newSessionHarness(start), root: root, name: name}
	h.session.served = func(_ context.Context, t backend.Target) (backend.Target, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if t.Kind != backend.KindRemote {
			return t, nil
		}
		h.asked++
		if h.askErr != nil {
			return t, h.askErr
		}
		t.VaultPath, t.VaultName = h.root, h.name
		return t, nil
	}
	return h
}

func (h *serverHarness) serve(root, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.root, h.name = root, name
}

func onVault(t backend.Target, root, name string) backend.Target {
	t.VaultPath, t.VaultName = root, name
	return t
}

func TestSessionStopsABatchWhenTheServerSwitches(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, Name: "home", BaseURL: "https://notes.example.com", AuthToken: "tok-secret"}
	h := newServerHarness(server, "/srv/a", "A")
	a := onVault(server, "/srv/a", "A")
	h.mustRunOn(t, false, a)

	h.serve("/srv/b", "B")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = h.backendFor(context.Background(), false)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		var switched *switchError
		if !errors.As(err, &switched) {
			t.Fatalf("call %d after the switch: %v, want a switchError", i, err)
		}
		msg := err.Error()
		if !strings.Contains(msg, `the vault "A" on the server "home" at https://notes.example.com`) || !strings.Contains(msg, `the vault "B" on the server "home"`) {
			t.Fatalf("message must name both vaults: %s", msg)
		}
		if strings.Contains(msg, "secret") || strings.Contains(msg, "/srv/") {
			t.Fatalf("message leaks a token or a path: %s", msg)
		}
	}
	if h.opens != 1 {
		t.Fatalf("a refused call opened the new vault: %d opens", h.opens)
	}

	b := onVault(server, "/srv/b", "B")
	if previous := h.mustRunOn(t, true, b); previous == nil || *previous != a {
		t.Fatalf("vault_info must report the previous vault, got %+v", previous)
	}
	h.mustRunOn(t, false, b)
}

// Until vault_info finishes, the session has not moved: a failed one changes
// nothing, and other calls keep stopping while one runs.
func TestSessionMovesOnlyWhenVaultInfoFinishes(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com"}
	h := newServerHarness(server, "/srv/a", "A")
	a, b := onVault(server, "/srv/a", "A"), onVault(server, "/srv/b", "B")
	h.mustRunOn(t, false, a)
	h.serve("/srv/b", "B")

	_, abandoned, err := h.backendFor(context.Background(), true)
	if err != nil || abandoned == nil || abandoned.from != a || abandoned.to != b {
		t.Fatalf("vault_info: move %+v, err %v", abandoned, err)
	}
	var switched *switchError
	if _, _, err := h.backendFor(context.Background(), false); !errors.As(err, &switched) {
		t.Fatalf("a call beside a running vault_info: %v, want a switchError", err)
	}

	// That vault_info failed, so nothing finished it; the next one reports the
	// same switch and is the one that moves the session.
	if previous := h.mustRunOn(t, true, b); previous == nil || *previous != a {
		t.Fatalf("the next vault_info: previous %+v", previous)
	}
	if previous := h.mustRunOn(t, false, b); previous != nil {
		t.Fatalf("after the move: %+v", previous)
	}
}

// A call that was still waiting for the server's answer when vault_info moved
// the session began on the old vault, so it stops either way, and it does not
// pull the session back.
func TestSessionCallThatBeganBeforeTheMoveStops(t *testing.T) {
	for name, answersOldVault := range map[string]bool{"answered with the new vault": false, "answered with the old vault": true} {
		t.Run(name, func(t *testing.T) {
			server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com"}
			h := newServerHarness(server, "/srv/a", "A")
			a, b := onVault(server, "/srv/a", "A"), onVault(server, "/srv/b", "B")
			h.mustRunOn(t, false, a)

			answer := h.session.served
			waiting, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			h.session.served = func(ctx context.Context, t backend.Target) (backend.Target, error) {
				first := false
				once.Do(func() { first = true })
				if !first {
					return answer(ctx, t)
				}
				var asked backend.Target
				var err error
				if answersOldVault {
					asked, err = answer(ctx, t)
				}
				close(waiting)
				<-release
				if !answersOldVault {
					asked, err = answer(ctx, t)
				}
				return asked, err
			}
			stopped := make(chan error, 1)
			go func() {
				_, _, err := h.backendFor(context.Background(), false)
				stopped <- err
			}()
			<-waiting

			h.serve("/srv/b", "B")
			if previous := h.mustRunOn(t, true, b); previous == nil || *previous != a {
				t.Fatalf("vault_info: previous %+v", previous)
			}
			close(release)
			err := <-stopped
			var switched *switchError
			if !errors.As(err, &switched) {
				t.Fatalf("the call that began before the move: %v, want a switchError", err)
			}
			if h.target != b {
				t.Fatalf("the stopped call moved the session to %+v", h.target)
			}
			if previous := h.mustRunOn(t, false, b); previous != nil {
				t.Fatalf("a call after the move: %+v", previous)
			}
		})
	}
}

func TestSessionServerSwitchAndBackNeedsNoConfirmation(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com"}
	h := newServerHarness(server, "/srv/a", "A")
	a := onVault(server, "/srv/a", "A")
	h.mustRunOn(t, false, a)
	h.serve("/srv/b", "B")
	if _, _, err := h.backendFor(context.Background(), false); err == nil {
		t.Fatal("a switch must stop the call")
	}
	h.serve("/srv/a", "A")
	h.mustRunOn(t, false, a)
	if h.opens != 1 {
		t.Fatalf("returning to the same vault reopened it: %d opens", h.opens)
	}
}

// What stays the same vault: the path decides, not the display name or token.
func TestSessionServerRenameAndNewTokenAreNotSwitches(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com", AuthToken: "old"}
	h := newServerHarness(server, "/srv/a", "A")
	h.mustRunOn(t, false, onVault(server, "/srv/a", "A"))

	h.serve("/srv/a", "A renamed in vault.json")
	renamed := onVault(server, "/srv/a", "A renamed in vault.json")
	if previous := h.mustRunOn(t, false, renamed); previous != nil {
		t.Fatalf("a new display name is not a switch: %+v", previous)
	}

	rotated := server
	rotated.AuthToken = "new"
	h.set(rotated)
	if previous := h.mustRunOn(t, false, onVault(rotated, "/srv/a", "A renamed in vault.json")); previous != nil {
		t.Fatalf("a new token is not a switch: %+v", previous)
	}
}

func TestSessionSwitchBetweenVaultsWithOneName(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com"}
	h := newServerHarness(server, "/home/ada/notes", "notes")
	h.mustRunOn(t, false, onVault(server, "/home/ada/notes", "notes"))
	h.serve("/home/grace/notes", "notes")
	_, _, err := h.backendFor(context.Background(), false)
	var switched *switchError
	if !errors.As(err, &switched) {
		t.Fatalf("one name, two paths is a switch: %v", err)
	}
	if !strings.Contains(err.Error(), `it was the vault "notes" on the server at https://notes.example.com and is now a different vault with the same name.`) {
		t.Fatalf("message: %s", err)
	}
}

func TestSessionServerThatCannotAnswerStopsTheCall(t *testing.T) {
	server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com"}
	h := newServerHarness(server, "/srv/a", "A")
	a := onVault(server, "/srv/a", "A")

	// Nothing is pinned, and the backend comes back so the error can name the
	// server (a 401's hint needs its URL).
	h.askErr = errors.New("server down")
	b, _, err := h.backendFor(context.Background(), false)
	if err == nil || err.Error() != "server down" {
		t.Fatalf("first call: %v", err)
	}
	if b == nil {
		t.Fatal("the failed call must return a backend to describe its error with")
	}
	h.askErr = nil
	if previous := h.mustRunOn(t, false, a); previous != nil {
		t.Fatalf("a first success after a failure is not a switch: %+v", previous)
	}

	// A failure later does not count as an answer: no call runs on a guess,
	// and the session stays put, for vault_info too.
	h.askErr = errors.New("server down")
	for _, confirm := range []bool{false, true} {
		if _, _, err := h.backendFor(context.Background(), confirm); err == nil || err.Error() != "server down" {
			t.Fatalf("confirm=%v: %v", confirm, err)
		}
	}
	h.askErr = nil
	if previous := h.mustRunOn(t, false, a); previous != nil {
		t.Fatalf("an outage is not a switch: %+v", previous)
	}
}

// A call that stops because the app moved elsewhere does not contact the new
// server; vault_info does, since it is what moves the session there.
func TestSessionDoesNotAskAServerItWillNotUse(t *testing.T) {
	local := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	server := backend.Target{Kind: backend.KindRemote, BaseURL: "https://notes.example.com"}
	h := newServerHarness(local, "/srv/a", "A")
	h.mustRunOn(t, false, local)
	asked := h.asked

	h.set(server)
	h.askErr = errors.New("server down")
	var switched *switchError
	if _, _, err := h.backendFor(context.Background(), false); !errors.As(err, &switched) {
		t.Fatalf("a switch to an unreachable server is still a switch: %v", err)
	}
	if h.asked != asked {
		t.Fatalf("a refused call asked the new server: %d", h.asked-asked)
	}
	h.askErr = nil
	if previous := h.mustRunOn(t, true, onVault(server, "/srv/a", "A")); previous == nil || *previous != local {
		t.Fatalf("vault_info onto the server: previous %+v", previous)
	}
}
