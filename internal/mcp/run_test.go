package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
)

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
