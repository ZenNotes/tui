package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
)

type failingTransport struct{ err error }

func (t failingTransport) Connect(context.Context) (sdk.Connection, error) {
	return nil, t.err
}

func TestRunOnlySuppressesNormalShutdownErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		ok   bool
	}{
		{"EOF", io.EOF, true},
		{"wrapped EOF", fmt.Errorf("read: %w", io.EOF), true},
		{"SDK EOF", errors.New("server is closing: EOF"), true},
		{"canceled", context.Canceled, true},
		{"deadline", context.DeadlineExceeded, false},
		{"truncated input", errors.New("server is closing: unexpected EOF"), false},
		{"transport failure", errors.New("server is closing: input/output error"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Run(context.Background(), Options{Transport: failingTransport{tc.err}})
			if tc.ok && err != nil {
				t.Fatalf("normal shutdown must be quiet: %v", err)
			}
			if !tc.ok && !errors.Is(err, tc.err) {
				t.Fatalf("lost transport failure: got %v, want %v", err, tc.err)
			}
		})
	}
}

func TestRunReturnsOnClientDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	serverT, clientT := sdk.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Transport: serverT}) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("client disconnect must be quiet: %v", err)
	}
}

// runServer serves Run over an in-memory transport and returns a connected
// client; resolve is consulted before every tool call, like the app's config.
func runServer(t *testing.T, resolve func() (backend.Target, error)) *sdk.ClientSession {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	serverT, clientT := sdk.NewInMemoryTransports()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, Options{
			ResolveTarget: resolve,
			OpenBackend:   func(t backend.Target) (Backend, error) { return backend.New(t, backend.Options{}) },
			Transport:     serverT,
		})
	}()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		<-done
	})
	return cs
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func seedVault(t *testing.T, note string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inbox", note), []byte("# "+strings.TrimSuffix(note, ".md")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunFollowsAVaultSwitchOnlyAfterVaultInfo(t *testing.T) {
	first, second := seedVault(t, "First.md"), seedVault(t, "Second.md")
	var mu sync.Mutex
	current := backend.Target{Kind: backend.KindLocal, Root: first}
	cs := runServer(t, func() (backend.Target, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	})

	if text, isErr := callTool(t, cs, "list_notes", nil); isErr || !strings.Contains(text, "inbox/First.md") {
		t.Fatalf("before the switch: %s", text)
	}
	mu.Lock()
	current = backend.Target{Kind: backend.KindLocal, Root: second}
	mu.Unlock()

	// A write planned against the first vault must not land in the second.
	text, isErr := callTool(t, cs, "write_note", map[string]any{"path": "inbox/First.md", "content": "planned for the first vault"})
	if !isErr || !strings.Contains(text, "The ZenNotes vault changed") || !strings.Contains(text, first) || !strings.Contains(text, second) {
		t.Fatalf("write after the switch: error=%v %s", isErr, text)
	}
	if _, err := os.Stat(filepath.Join(second, "inbox", "First.md")); !os.IsNotExist(err) {
		t.Fatalf("the refused write reached the second vault: %v", err)
	}
	if text, isErr := callTool(t, cs, "list_notes", nil); !isErr || !strings.Contains(text, "vault_info") {
		t.Fatalf("reads stop too until vault_info: %s", text)
	}

	text, isErr = callTool(t, cs, "vault_info", nil)
	if isErr {
		t.Fatalf("vault_info: %s", text)
	}
	var info struct {
		VaultRoot string `json:"vaultRoot"`
		Notes     string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("vault_info payload: %v\n%s", err, text)
	}
	if info.VaultRoot != second || !strings.HasPrefix(info.Notes, "The vault changed: until this call the session worked in the local vault "+first) {
		t.Fatalf("vault_info after the switch: %+v", info)
	}

	if text, isErr := callTool(t, cs, "list_notes", nil); isErr || !strings.Contains(text, "inbox/Second.md") || strings.Contains(text, "First.md") {
		t.Fatalf("after vault_info: %s", text)
	}
	if text, _ := callTool(t, cs, "vault_info", nil); strings.Contains(text, "The vault changed") {
		t.Fatalf("the switch note repeats: %s", text)
	}
}

func TestRunNamesTheZnConnectCommandOnA401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}, nil
	})
	text, isErr := callTool(t, cs, "list_notes", nil)
	if !isErr || !strings.Contains(text, "zn connect "+server.URL+" --no-default") || !strings.Contains(text, "ZENNOTES_REMOTE_TOKEN") {
		t.Fatalf("401 hint: %s", text)
	}
}

// A stale ZENNOTES_REMOTE_TOKEN outranks any token `zn connect` saves, so
// the hint has to name it rather than send the user to zn connect again.
func TestRunBlamesAStaleEnvironmentTokenOnA401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	t.Setenv(backend.RemoteTokenEnv, "stale")
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindRemote, BaseURL: server.URL, AuthToken: "stale"}, nil
	})
	text, isErr := callTool(t, cs, "list_notes", nil)
	if !isErr || !strings.Contains(text, "The rejected token is ZENNOTES_REMOTE_TOKEN") || !strings.Contains(text, "zn connect "+server.URL+" --no-default") {
		t.Fatalf("stale environment token hint: %s", text)
	}
}
