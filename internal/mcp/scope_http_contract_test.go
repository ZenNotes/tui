package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The contract through a server's built-in /mcp route (mcpserver), over real HTTP,
// against a real server process. See docs/ideas/mcp-http-transport.md. This uses its
// own variable, not ZENNOTES_SERVER_CONTRACT_BINARY, because a server only has the
// route once it has been built against this module: see docs/shared-contracts.md.

const (
	httpMCPToken   = "mcp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	httpMCPToken2  = "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	httpMCPServer  = "server-cccccccccccccccccccccccccccccccccccccccc"
	httpMCPNoteTxt = "Secret-note-text-that-must-stay-out-of-logs"
)

func httpMCPBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("ZENNOTES_SERVER_MCP_CONTRACT_BINARY")
	if binary == "" {
		t.Skip("set ZENNOTES_SERVER_MCP_CONTRACT_BINARY to a server built with /mcp to run the contract through it")
	}
	return binary
}

// httpMCPProcess is a running server.
type httpMCPProcess struct {
	base string
	log  string
}

func (p httpMCPProcess) logged(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(p.log)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func serverEnv(root, state string, extra ...string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "ZENNOTES_") {
			env = append(env, entry)
		}
	}
	env = append(env,
		"ZENNOTES_VAULT_PATH="+root,
		"ZENNOTES_CONFIG_PATH="+filepath.Join(state, "server.json"),
	)
	return append(env, extra...)
}

// startHTTPMCPServer starts the server with the given settings and waits for it.
func startHTTPMCPServer(t *testing.T, root string, extra ...string) httpMCPProcess {
	t.Helper()
	binary := httpMCPBinary(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	state := t.TempDir()
	logPath := filepath.Join(state, "server.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary)
	command.Dir = state
	command.Env = append(serverEnv(root, state, "ZENNOTES_BIND="+address), extra...)
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		log.Close()
	})
	base := "http://" + address
	deadline := time.Now().Add(10 * time.Second)
	for {
		// Any answer will do: under a base path, /healthz is a 404.
		response, err := http.Get(base + "/healthz")
		if err == nil {
			response.Body.Close()
			return httpMCPProcess{base: base, log: logPath}
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(logPath)
			t.Fatalf("server did not become ready: %v\n%s", err, raw)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}

// connectHTTPMCP connects the SDK's own HTTP client, which is what a Go client does.
func connectHTTPMCP(t *testing.T, endpoint, token string) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: bearerTransport{token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
	})
	return cs
}

func scopeEnv(t *testing.T, scope *Scope) []string {
	t.Helper()
	access := "write"
	var allow, deny []string
	if scope != nil {
		access, allow, deny = scope.Access.String(), scope.Allow, scope.Deny
	}
	env := []string{"ZENNOTES_MCP_ACCESS=" + access}
	if len(allow) > 0 {
		raw, _ := json.Marshal(allow)
		env = append(env, "ZENNOTES_MCP_ALLOW_PATHS="+string(raw))
	}
	if len(deny) > 0 {
		raw, _ := json.Marshal(deny)
		env = append(env, "ZENNOTES_MCP_DENY_PATHS="+string(raw))
	}
	return env
}

func runScopedOnHTTPMCP(t *testing.T, root string, scope *Scope) *sdk.ClientSession {
	t.Helper()
	env := append([]string{
		"ZENNOTES_AUTH_TOKEN=" + httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN=" + httpMCPToken,
	}, scopeEnv(t, scope)...)
	server := startHTTPMCPServer(t, root, env...)
	return connectHTTPMCP(t, server.base+"/mcp", httpMCPToken)
}

// TestSharedScopeContractThroughHTTP runs every scope case through /mcp. Its answers
// differ from `zn mcp --server` only where the differences below say.
func TestSharedScopeContractThroughHTTP(t *testing.T) {
	httpMCPBinary(t)
	differences := []scopeContractDifference{{
		Case: "full-access-lists-every-tool-with-annotations", Call: 0, Tool: "vault_info",
		Why: "a client on another machine has no use for the server's absolute paths, so this route hides them at full access too",
		Expect: scopeContractExpect{
			Match:      []byte(`{"kind":"remote","access":{"level":"write","partial":false,"allow":[],"missingAllow":[]}}`),
			AbsentKeys: []string{"server", "serverName", "vaultPath", "vaultRoot", "inboxAbsolutePath"},
			Excludes:   []string{"$VAULT"},
		},
	}}
	const staleDeny = "This assistant's scope hides a folder or note that does not exist in this vault (renamed or deleted outside ZenNotes, or it belongs to another vault), so nothing is shared until the server's deny entries for this credential are fixed."
	blocked, _ := json.Marshal(staleDeny)
	for call, tool := range []string{"list_notes", "read_note"} {
		differences = append(differences, scopeContractDifference{
			Case: "a-stale-deny-entry-shares-nothing", Call: call, Tool: tool,
			Why:    "the scope comes from the server's settings here, not `zn mcp`'s flags",
			Expect: scopeContractExpect{Error: ptr(staleDeny)},
		})
	}
	differences = append(differences, scopeContractDifference{
		Case: "a-stale-deny-entry-shares-nothing", Call: 2, Tool: "vault_info",
		Why:    "the same wording, as vault_info reports it",
		Expect: scopeContractExpect{Match: []byte(`{"subfolders":[],"access":{"level":"read","partial":true,"allow":[],"missingAllow":[],"blocked":` + string(blocked) + `}}`)},
	})
	differences = append(differences, scopeServerDifferences...)
	runScopeContractHiding(t, runScopedOnHTTPMCP, differences, hostedAlwaysRefused)
}

// mcpPost sends one raw JSON-RPC message, the way anything but the SDK would.
func mcpPost(t *testing.T, url, token string, headers map[string]string, message string) (int, http.Header, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, response.Header, string(body)
}

const mcpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"raw","version":"0"}}}`

func seedHTTPVault(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	for rel, body := range map[string]string{
		"Projects/Plan.md": "# Plan\n\n" + httpMCPNoteTxt + "\n",
		"Journal/Day.md":   "# Day\n\nprivate\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestHTTPMCPIsOffByDefault(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root, "ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		// Setting a token without turning it on opens nothing.
		"ZENNOTES_MCP_TOKEN="+httpMCPToken, "ZENNOTES_MCP_ACCESS=write")
	for _, token := range []string{"", httpMCPToken, httpMCPServer} {
		status, _, body := mcpPost(t, server.base+"/mcp", token, nil, mcpInitialize)
		if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("token %q: POST /mcp answered %d: %s", token, status, body)
		}
		if strings.Contains(body, "serverInfo") {
			t.Errorf("token %q: POST /mcp spoke MCP: %s", token, body)
		}
	}
}

func TestHTTPMCPKeepsTheTwoKindsOfTokenApart(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	get := func(path, token string) int {
		request, _ := http.NewRequest(http.MethodGet, server.base+path, nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}

	// An MCP token works on /mcp and nothing else.
	if status, _, body := mcpPost(t, server.base+"/mcp", httpMCPToken, nil, mcpInitialize); status != http.StatusOK {
		t.Fatalf("MCP token on /mcp: %d %s", status, body)
	}
	for _, path := range []string{"/api/notes", "/notes", "/api/vault", "/api/notes/read?path=Projects/Plan.md", "/api/assets"} {
		if status := get(path, httpMCPToken); status != http.StatusUnauthorized {
			t.Errorf("MCP token on GET %s: %d", path, status)
		}
	}
	for path, message := range map[string]string{
		"/api/session/rotate-token": `{"currentToken":"` + httpMCPToken + `","newToken":"` + strings.Repeat("x", 40) + `"}`,
		"/api/vault/select":         `{"path":"/"}`,
		"/api/notes/write":          `{"path":"inbox/x.md","body":"no"}`,
	} {
		if status, _, _ := mcpPost(t, server.base+path, httpMCPToken, nil, message); status != http.StatusUnauthorized {
			t.Errorf("MCP token on POST %s: %d", path, status)
		}
	}
	// The server's token works on the REST API, and not on /mcp.
	if status := get("/api/notes", httpMCPServer); status != http.StatusOK {
		t.Errorf("server token on the REST API: %d", status)
	}
	if status, header, _ := mcpPost(t, server.base+"/mcp", httpMCPServer, nil, mcpInitialize); status != http.StatusUnauthorized || header.Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("server token on /mcp: %d, challenge %q", status, header.Get("WWW-Authenticate"))
	}

	// Nor does a browser login open /mcp.
	status, header, _ := mcpPost(t, server.base+"/api/session/login", "", nil, `{"token":"`+httpMCPServer+`"}`)
	cookie := strings.SplitN(header.Get("Set-Cookie"), ";", 2)[0]
	if status != http.StatusOK || cookie == "" {
		t.Fatalf("login with the server's token: %d, cookie %q", status, cookie)
	}
	if status, _, _ := mcpPost(t, server.base+"/mcp", "", map[string]string{"Cookie": cookie}, mcpInitialize); status != http.StatusUnauthorized {
		t.Errorf("browser session on /mcp: %d", status)
	}

	// An MCP token is not a login.
	status, header, _ = mcpPost(t, server.base+"/api/session/login", "", nil, `{"token":"`+httpMCPToken+`"}`)
	if status == http.StatusOK || header.Get("Set-Cookie") != "" {
		t.Errorf("MCP token logged in: %d, cookie %q", status, header.Get("Set-Cookie"))
	}

	// Everything else that is not a token gets the same answer.
	wrong := map[string]map[string]string{
		"no credential":   nil,
		"wrong token":     {"Authorization": "Bearer " + strings.Repeat("z", 48)},
		"empty bearer":    {"Authorization": "Bearer "},
		"basic":           {"Authorization": "Basic " + httpMCPToken},
		"token alone":     {"Authorization": httpMCPToken},
		"as a query only": {"X-Token": httpMCPToken},
	}
	for name, headers := range wrong {
		status, header, body := mcpPost(t, server.base+"/mcp", "", headers, mcpInitialize)
		if status != http.StatusUnauthorized || header.Get("WWW-Authenticate") != "Bearer" || strings.TrimSpace(body) != "unauthorized" {
			t.Errorf("%s: %d %q %q", name, status, header.Get("WWW-Authenticate"), body)
		}
	}
	if status, _, _ := mcpPost(t, server.base+"/mcp?token="+httpMCPToken, "", nil, mcpInitialize); status != http.StatusUnauthorized {
		t.Errorf("token in the query: %d", status)
	}

	// A browser page cannot call it, even holding the token.
	for _, origin := range []string{"https://evil.example", server.base, "null"} {
		if status, _, _ := mcpPost(t, server.base+"/mcp", httpMCPToken, map[string]string{"Origin": origin}, mcpInitialize); status != http.StatusForbidden {
			t.Errorf("Origin %s: %d", origin, status)
		}
	}
}

// Rotating the server's token to an MCP token would make it open both doors.
func TestHTTPMCPRotationCannotJoinTheTokens(t *testing.T) {
	root := seedHTTPVault(t)
	state := t.TempDir()
	config := filepath.Join(state, "stored.json")
	if err := os.WriteFile(config, []byte(`{"authToken":"`+httpMCPServer+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_CONFIG_PATH="+config,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	url := server.base + "/api/session/rotate-token"
	if status, _, body := mcpPost(t, url, httpMCPServer, nil, `{"currentToken":"`+httpMCPServer+`","newToken":"`+httpMCPToken+`"}`); status != http.StatusBadRequest {
		t.Fatalf("rotating to the MCP token: %d %s", status, body)
	}
	if status, _, _ := mcpPost(t, server.base+"/mcp", httpMCPToken, nil, mcpInitialize); status != http.StatusOK {
		t.Fatalf("the MCP token stopped working: %d", status)
	}
	next := strings.Repeat("n", 48)
	if status, _, body := mcpPost(t, url, httpMCPServer, nil, `{"currentToken":"`+httpMCPServer+`","newToken":"`+next+`"}`); status != http.StatusOK {
		t.Fatalf("ordinary rotation: %d %s", status, body)
	}
	// MCP tools keep working: the in-process calls read the new token.
	cs := connectHTTPMCP(t, server.base+"/mcp", httpMCPToken)
	if text := mustCall(t, cs, "read_note", map[string]any{"path": "Projects/Plan.md"}); !strings.Contains(text, httpMCPNoteTxt) {
		t.Fatalf("read after rotation: %s", text)
	}
	if status, _, _ := mcpPost(t, server.base+"/mcp", next, nil, mcpInitialize); status != http.StatusUnauthorized {
		t.Fatalf("the new server token on /mcp: %d", status)
	}
}

func TestHTTPMCPCredentialsFileBindsTokensToScopes(t *testing.T) {
	root := seedHTTPVault(t)
	sum := func(token string) string {
		digest := sha256.Sum256([]byte(token))
		return hex.EncodeToString(digest[:])
	}
	file := filepath.Join(t.TempDir(), "credentials.json")
	raw := fmt.Sprintf(`{"version":1,"credentials":[
		{"name":"reader","tokenSha256":%q,"access":"read","allow":["Projects"]},
		{"name":"writer","tokenSha256":%q,"access":"write","allow":["Journal"]}
	]}`, sum(httpMCPToken), sum(httpMCPToken2))
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_CREDENTIALS_FILE="+file,
	)
	reader := connectHTTPMCP(t, server.base+"/mcp", httpMCPToken)
	writer := connectHTTPMCP(t, server.base+"/mcp", httpMCPToken2)

	if text := mustCall(t, reader, "list_notes", map[string]any{}); !strings.Contains(text, "Plan.md") || strings.Contains(text, "Day.md") {
		t.Errorf("reader sees %s", text)
	}
	if text := mustCall(t, writer, "list_notes", map[string]any{}); !strings.Contains(text, "Day.md") || strings.Contains(text, "Plan.md") {
		t.Errorf("writer sees %s", text)
	}
	if listed := listedTools(t, reader); len(listed) >= len(listedTools(t, writer)) {
		t.Errorf("reader lists %d tools, writer %d", len(listed), len(listedTools(t, writer)))
	}
	// Nothing a caller sends picks the other scope: not a tool argument, and not the
	// session id a stateful server would have used.
	if text := mustFail(t, reader, "read_note", map[string]any{"path": "Journal/Day.md"}); strings.Contains(text, "private") {
		t.Errorf("reader read Journal: %s", text)
	}
	status, header, body := mcpPost(t, server.base+"/mcp", httpMCPToken, map[string]string{"Mcp-Session-Id": "anything"}, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_note","arguments":{"path":"Journal/Day.md"}}}`)
	if strings.Contains(body, "private") {
		t.Errorf("a session header reached Journal: %d %s", status, body)
	}
	if header.Get("Mcp-Session-Id") != "" {
		t.Errorf("issued a session id: %q", header.Get("Mcp-Session-Id"))
	}
	// A hidden tool is a tool error, and the connection survives it.
	if text := mustFail(t, reader, "write_note", map[string]any{"path": "Projects/Plan.md", "body": "no"}); !strings.Contains(text, "Unknown tool") {
		t.Errorf("hidden tool: %s", text)
	}
	if text := mustCall(t, reader, "vault_info", map[string]any{}); strings.Contains(text, "zennotes.internal") || strings.Contains(text, root) {
		t.Errorf("vault_info tells too much: %s", text)
	}
}

// A full-access client gets every tool, and still no absolute path.
func TestHTTPMCPFullAccess(t *testing.T) {
	root := seedHTTPVault(t)
	cs := runScopedOnHTTPMCP(t, root, nil)
	if got, want := len(listedTools(t, cs)), len(ToolNames()); got != want {
		t.Errorf("full access lists %d tools, want %d", got, want)
	}
	text := mustCall(t, cs, "vault_info", map[string]any{})
	for _, secret := range []string{root, "zennotes.internal", `"server"`, "vaultPath"} {
		if strings.Contains(text, secret) {
			t.Errorf("vault_info has %q: %s", secret, text)
		}
	}
	mustCall(t, cs, "write_note", map[string]any{"path": "Projects/Plan.md", "body": "# New\n"})
	// The server's own write path did this: its file mode, not zn's.
	info, err := os.Stat(filepath.Join(root, "Projects/Plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	created := mustCall(t, cs, "create_note", map[string]any{"folder": "inbox", "title": "Fresh"})
	var note struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(created), &note); err != nil {
		t.Fatalf("%v: %s", err, created)
	}
	fresh, err := os.Stat(filepath.Join(root, filepath.FromSlash(note.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Mode().Perm() != 0o600 {
		t.Errorf("a note made over MCP has mode %o, want the server's 0600 (the existing one has %o)", fresh.Mode().Perm(), info.Mode().Perm())
	}
}

func TestHTTPMCPLogsNoSecrets(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	cs := connectHTTPMCP(t, server.base+"/mcp", httpMCPToken)
	mustCall(t, cs, "read_note", map[string]any{"path": "Projects/Plan.md"})
	mustCall(t, cs, "search_text", map[string]any{"query": "Secret-note-text"})
	mustCall(t, cs, "write_note", map[string]any{"path": "Projects/Plan.md", "body": "# " + httpMCPNoteTxt + "\n"})
	mustFail(t, cs, "read_note", map[string]any{"path": "Missing-private-name.md"})
	mcpPost(t, server.base+"/mcp", strings.Repeat("z", 48), nil, mcpInitialize)
	mcpPost(t, server.base+"/mcp", httpMCPServer, nil, mcpInitialize)
	logged := server.logged(t)
	for _, secret := range []string{httpMCPToken, httpMCPServer, strings.Repeat("z", 48), httpMCPNoteTxt, "Plan.md", "Missing-private-name"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log holds %q:\n%s", secret, logged)
		}
	}
	for _, want := range []string{"tool=read_note outcome=ok", "tool=read_note outcome=error", "tool=write_note outcome=ok", "on at /mcp, credentials: assistant"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log lacks %q:\n%s", want, logged)
		}
	}
}

func TestHTTPMCPUnderABasePath(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_BASE_PATH=/notes",
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=read",
	)
	cs := connectHTTPMCP(t, server.base+"/notes/mcp", httpMCPToken)
	if text := mustCall(t, cs, "list_notes", map[string]any{}); !strings.Contains(text, "Plan.md") {
		t.Errorf("list_notes: %s", text)
	}
	if status, _, _ := mcpPost(t, server.base+"/mcp", httpMCPToken, nil, mcpInitialize); status == http.StatusOK {
		t.Errorf("/mcp answered outside the base path")
	}
}

func TestHTTPMCPRequestLimits(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	cs := connectHTTPMCP(t, server.base+"/mcp", httpMCPToken)
	// A 5 MiB note, over the SDK's 4 MiB default.
	large := strings.Repeat("a", 5<<20)
	mustCall(t, cs, "write_note", map[string]any{"path": "Projects/Plan.md", "body": large})
	// The largest upload a tool call can carry.
	encoded := base64.StdEncoding.EncodeToString(make([]byte, maxUploadAssetBytes))
	if text, failed := callTool(t, cs, "upload_asset", map[string]any{"filename": "big.bin", "content_base64": encoded}); failed {
		t.Errorf("the largest upload failed: %s", text)
	}
	// Past the server's note limit, the server answers and the tool reports it.
	mustFail(t, cs, "write_note", map[string]any{"path": "Projects/Plan.md", "body": strings.Repeat("a", 11<<20)})
	// Past the request limit, HTTP refuses it before anything reads it.
	status, _, _ := mcpPost(t, server.base+"/mcp", httpMCPToken, nil, strings.Repeat(" ", int(MaxHTTPRequestBytes(10<<20))+1))
	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("past the limit: %d", status)
	}
	// Nor does a refused body cost anything without a token.
	if status, _, _ := mcpPost(t, server.base+"/mcp", "", nil, strings.Repeat(" ", 1<<20)); status != http.StatusUnauthorized {
		t.Errorf("large body with no token: %d", status)
	}
}

// A token must not end up on another vault, by a switch or by the host config on the
// next start. ZENNOTES_VAULT_PATH rules out both, and the server requires it.
func TestHTTPMCPVaultIsPinned(t *testing.T) {
	root := seedHTTPVault(t)
	other := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_ALLOW_UNSCOPED_BROWSE=1",
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	if status, _, body := mcpPost(t, server.base+"/api/vault/select", httpMCPServer, nil, `{"path":"`+other+`"}`); status != http.StatusConflict {
		t.Errorf("switching: %d %s", status, body)
	}
	cs := connectHTTPMCP(t, server.base+"/mcp", httpMCPToken)
	if text := mustCall(t, cs, "read_note", map[string]any{"path": "Projects/Plan.md"}); !strings.Contains(text, httpMCPNoteTxt) {
		t.Errorf("read after the refused switch: %s", text)
	}
}

// Whatever a client with a valid token sends, the server goes on answering.
func TestHTTPMCPSurvivesWhatARequestCanSend(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	url := server.base + "/mcp"
	alive := func(what string) {
		t.Helper()
		if status, _, body := mcpPost(t, url, httpMCPToken, nil, mcpInitialize); status != http.StatusOK {
			t.Fatalf("after %s: %d %s", what, status, body)
		}
		if status, _, _ := mcpPost(t, server.base+"/api/notes", httpMCPServer, nil, ""); status == 0 {
			t.Fatalf("after %s: the API is gone", what)
		}
	}
	const call = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_notes","arguments":{"a":`
	for name, body := range map[string]string{
		"2 MB of brackets":      strings.Repeat("[", 2_000_000),
		"deep arguments":        call + strings.Repeat("[", 1_000_000) + strings.Repeat("]", 1_000_000) + `}}}`,
		"deep and unterminated": `{"jsonrpc":"2.0","id":1,"method":"ping","params":` + strings.Repeat(`{"a":`, 400_000),
		"not JSON":              `{"jsonrpc"`,
		"a batch":               `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
		"a batch after spaces":  " \t\r\n" + `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
	} {
		if status, _, _ := mcpPost(t, url, httpMCPToken, nil, body); status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, status)
		}
		alive(name)
	}
	// Deep, but within what the check allows, and so what the SDK gets to decode.
	deep := call + strings.Repeat("[", 9_990) + strings.Repeat("]", 9_990) + `}}}`
	mcpPost(t, url, httpMCPToken, nil, deep)
	alive("deep but allowed")

	cs := connectHTTPMCP(t, url, httpMCPToken)
	for _, limit := range []float64{-1, -1e300, 1e300, 0.5, 1e19} {
		for _, tool := range []struct {
			name string
			args map[string]any
		}{
			{"search_by_title", map[string]any{"query": "Plan"}},
			{"search_by_tag", map[string]any{"tag": "x"}},
			{"search_text", map[string]any{"query": "Plan"}},
			{"list_notes", map[string]any{}},
		} {
			tool.args["limit"] = limit
			callTool(t, cs, tool.name, tool.args)
		}
		alive(fmt.Sprintf("limit %v", limit))
	}
	// Names no file can have. The notes API held its lock for ever on these.
	for _, title := range []string{strings.Repeat("x", 300), strings.Repeat("漢", 90), "a\x00b", "\x00"} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			callTool(t, cs, "create_note", map[string]any{"title": title})
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("create_note with a %d-byte title never returned", len(title))
		}
		alive("a bad title")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("x"))
	callTool(t, cs, "upload_asset", map[string]any{"filename": strings.Repeat("y", 300) + ".bin", "content_base64": encoded})
	alive("a long file name")
}

// A tool reads a note and writes it back, so clients at the same time must take turns.
func TestHTTPMCPParallelEditsAllLand(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	url := server.base + "/mcp"
	parallel := func(n int, message func(int) string) {
		t.Helper()
		var wait sync.WaitGroup
		for i := range n {
			wait.Add(1)
			go func() {
				defer wait.Done()
				for {
					status, _, _ := mcpPost(t, url, httpMCPToken, nil, message(i))
					if status != http.StatusServiceUnavailable {
						return
					}
					time.Sleep(20 * time.Millisecond)
				}
			}()
		}
		wait.Wait()
	}
	parallel(40, func(i int) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"append_to_note","arguments":{"path":"Projects/Plan.md","text":"line %d"}}}`, i)
	})
	raw, err := os.ReadFile(filepath.Join(root, "Projects/Plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		if !strings.Contains(string(raw), fmt.Sprintf("line %d\n", i)) && !strings.HasSuffix(string(raw), fmt.Sprintf("line %d", i)) {
			t.Errorf("line %d did not land", i)
		}
	}
	parallel(20, func(i int) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_comment","arguments":{"path":"Projects/Plan.md","body":"comment %d"}}}`, i)
	})
	cs := connectHTTPMCP(t, url, httpMCPToken)
	text := mustCall(t, cs, "list_comments", map[string]any{"path": "Projects/Plan.md"})
	for i := range 20 {
		if !strings.Contains(text, fmt.Sprintf("comment %d\"", i)) {
			t.Errorf("comment %d did not land", i)
		}
	}
}

// Hidden matches must not use up the server's limit before a visible one is reached.
func TestHTTPMCPScopedSearchSeesPastHiddenMatches(t *testing.T) {
	root := seedHTTPVault(t)
	var hidden strings.Builder
	for i := range 250 {
		fmt.Fprintf(&hidden, "needle %d\n", i)
	}
	// Named to sort first, so the server reaches it first.
	if err := os.MkdirAll(filepath.Join(root, "AAA"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AAA/Many.md"), []byte(hidden.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Projects/Found.md"), []byte("needle visible\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runScopedOnHTTPMCP(t, root, &Scope{Access: AccessRead, Allow: []string{"Projects"}})
	if text := mustCall(t, cs, "search_text", map[string]any{"query": "needle"}); !strings.Contains(text, "Found.md") || strings.Contains(text, "Many.md") {
		t.Errorf("search: %.300s", text)
	}
}

func TestHTTPMCPLimitsByWhatACredentialMayDo(t *testing.T) {
	root := seedHTTPVault(t)
	sum := func(token string) string {
		digest := sha256.Sum256([]byte(token))
		return hex.EncodeToString(digest[:])
	}
	file := filepath.Join(t.TempDir(), "credentials.json")
	raw := fmt.Sprintf(`{"version":1,"credentials":[
		{"name":"reader","tokenSha256":%q,"access":"read"},
		{"name":"writer","tokenSha256":%q,"access":"write"}
	]}`, sum(httpMCPToken), sum(httpMCPToken2))
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	server := startHTTPMCPServer(t, root,
		"TMPDIR="+tmp,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_CREDENTIALS_FILE="+file,
	)
	url := server.base + "/mcp"
	large := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_text","arguments":{"query":"` + strings.Repeat("q", 2<<20) + `"}}}`
	if status, _, _ := mcpPost(t, url, httpMCPToken, nil, large); status != http.StatusRequestEntityTooLarge {
		t.Errorf("2 MiB from a read credential: %d", status)
	}
	if status, _, _ := mcpPost(t, url, httpMCPToken2, nil, large); status != http.StatusOK {
		t.Errorf("2 MiB from a write credential: %d", status)
	}
	// Past 8 MiB an upload spills to a temp file, which must not stay.
	cs := connectHTTPMCP(t, url, httpMCPToken2)
	encoded := base64.StdEncoding.EncodeToString(make([]byte, 12<<20))
	if text, failed := callTool(t, cs, "upload_asset", map[string]any{"filename": "big.bin", "content_base64": encoded}); failed {
		t.Errorf("a 12 MiB upload: %s", text)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}

// A credential that may read everything except one folder must not read it once the
// owner archives or trashes a note from there.
func TestHTTPMCPDenyFollowsANoteIntoTheArchive(t *testing.T) {
	root := seedHTTPVault(t)
	for _, rel := range []string{"archive/Journal/Old.md", "trash/Journal/Gone.md", "Journal/Day.md"} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("# private\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "archive/Other.md"), []byte("# fine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runScopedOnHTTPMCP(t, root, &Scope{Access: AccessRead, Deny: []string{"Journal"}})
	for _, folder := range []string{"archive", "trash"} {
		text := mustCall(t, cs, "list_notes", map[string]any{"folder": folder})
		if strings.Contains(text, "Journal") {
			t.Errorf("%s lists: %s", folder, text)
		}
	}
	for _, rel := range []string{"archive/Journal/Old.md", "trash/Journal/Gone.md"} {
		if text := mustFail(t, cs, "read_note", map[string]any{"path": rel}); strings.Contains(text, "private") {
			t.Errorf("read %s: %s", rel, text)
		}
	}
	if text := mustCall(t, cs, "read_note", map[string]any{"path": "archive/Other.md"}); !strings.Contains(text, "fine") {
		t.Errorf("read a note the entry does not cover: %s", text)
	}
}

// A note moved into a system folder stays hidden however the deny entry names its folder.
func TestHTTPMCPDenyEntriesReachMovedNotes(t *testing.T) {
	seed := func(t *testing.T, files ...string) string {
		root := filepath.Join(t.TempDir(), "vault")
		for _, rel := range files {
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte("# private\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	inboxMode := func(root string) {
		if err := os.MkdirAll(filepath.Join(root, ".zennotes"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".zennotes/vault.json"), []byte(`{"primaryNotesLocation":"inbox"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		inbox bool
		deny  string
		files []string
		moved string
	}{
		{"another case, inbox mode", true, "INBOX/journal", []string{"inbox/Journal/d.md", "archive/Journal/s.md"}, "archive/Journal/s.md"},
		{"quick into archive", false, "quick/Q", []string{"quick/Q/p.md", "archive/Q/q.md"}, "archive/Q/q.md"},
		{"archive into trash", false, "archive/AJ", []string{"archive/AJ/p.md", "trash/AJ/a.md"}, "trash/AJ/a.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := seed(t, tc.files...)
			if tc.inbox {
				inboxMode(root)
			}
			cs := runScopedOnHTTPMCP(t, root, &Scope{Access: AccessRead, Deny: []string{tc.deny}})
			if text := mustFail(t, cs, "read_note", map[string]any{"path": tc.moved}); strings.Contains(text, "private") {
				t.Errorf("read: %s", text)
			}
			if text := mustCall(t, cs, "search_text", map[string]any{"query": "private"}); strings.Contains(text, tc.moved) {
				t.Errorf("search: %s", text)
			}
		})
	}
}

// Writes take one turn across credentials, since they all write the same vault.
func TestHTTPMCPCredentialsTakeTurnsToWrite(t *testing.T) {
	root := seedHTTPVault(t)
	sum := func(token string) string {
		digest := sha256.Sum256([]byte(token))
		return hex.EncodeToString(digest[:])
	}
	file := filepath.Join(t.TempDir(), "credentials.json")
	raw := fmt.Sprintf(`{"version":1,"credentials":[
		{"name":"one","tokenSha256":%q,"access":"write"},
		{"name":"two","tokenSha256":%q,"access":"write"}
	]}`, sum(httpMCPToken), sum(httpMCPToken2))
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_CREDENTIALS_FILE="+file,
	)
	url := server.base + "/mcp"
	var wait sync.WaitGroup
	for i := range 40 {
		token := httpMCPToken
		if i%2 == 0 {
			token = httpMCPToken2
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			message := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"append_to_note","arguments":{"path":"Projects/Plan.md","text":"line %d"}}}`, i)
			for {
				status, _, _ := mcpPost(t, url, token, nil, message)
				if status != http.StatusServiceUnavailable {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	wait.Wait()
	got, err := os.ReadFile(filepath.Join(root, "Projects/Plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		if !strings.Contains(string(got)+"\n", fmt.Sprintf("line %d\n", i)) {
			t.Errorf("line %d did not land", i)
		}
	}
}

// An edit that started before a note went to the trash must not write it back.
func TestHTTPMCPAnEditCannotBringBackATrashedNote(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	url := server.base + "/mcp"
	for round := range 25 {
		name := fmt.Sprintf("Race%d", round)
		if err := os.WriteFile(filepath.Join(root, "Projects", name+".md"), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var wait sync.WaitGroup
		for _, message := range []string{
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"append_to_note","arguments":{"path":"Projects/` + name + `.md","text":"more"}}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"move_to_trash","arguments":{"path":"Projects/` + name + `.md"}}}`,
		} {
			wait.Add(1)
			go func() {
				defer wait.Done()
				mcpPost(t, url, httpMCPToken, nil, message)
			}()
		}
		wait.Wait()
		if _, err := os.Stat(filepath.Join(root, "Projects", name+".md")); err == nil {
			t.Fatalf("round %d: the trashed note came back", round)
		}
	}
}

// A folder going away must not leave a copy of a note an edit was still writing.
func TestHTTPMCPAnEditCannotBringBackADeletedFolder(t *testing.T) {
	root := seedHTTPVault(t)
	server := startHTTPMCPServer(t, root,
		"ZENNOTES_AUTH_TOKEN="+httpMCPServer,
		"ZENNOTES_MCP_ENABLED=1",
		"ZENNOTES_MCP_TOKEN="+httpMCPToken,
		"ZENNOTES_MCP_ACCESS=write",
	)
	url := server.base + "/mcp"
	for round := range 25 {
		dir := fmt.Sprintf("Gone%d", round)
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "N.md"), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var wait sync.WaitGroup
		for _, message := range []string{
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"append_to_note","arguments":{"path":"` + dir + `/N.md","text":"more"}}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_folder","arguments":{"folder":"inbox","subpath":"` + dir + `","confirm":true}}}`,
		} {
			wait.Add(1)
			go func() {
				defer wait.Done()
				mcpPost(t, url, httpMCPToken, nil, message)
			}()
		}
		wait.Wait()
		if _, err := os.Stat(filepath.Join(root, dir, "N.md")); err == nil {
			t.Fatalf("round %d: the note came back", round)
		}
	}
}

// Answers wait in memory until a client reads them, so one search's has a size.
func TestHTTPMCPSearchHasACeiling(t *testing.T) {
	root := seedHTTPVault(t)
	if err := os.WriteFile(filepath.Join(root, "Many.md"), []byte(strings.Repeat("needle\n", 1500)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []*Scope{nil, {Access: AccessRead, Deny: []string{"Journal"}}} {
		cs := runScopedOnHTTPMCP(t, root, scope)
		var matches []any
		text := mustCall(t, cs, "search_text", map[string]any{"query": "needle", "limit": 1e8})
		if err := json.Unmarshal([]byte(text), &matches); err != nil {
			t.Fatal(err)
		}
		if len(matches) != hostedSearchLimit {
			t.Errorf("scope %v: %d matches", scope, len(matches))
		}
	}
}

// A note can be on disk that was never small enough for the API to accept. Its answer has
// a limit, and an ordinary note under it is unaffected.
func TestHTTPMCPAnswersHaveALimit(t *testing.T) {
	root := seedHTTPVault(t)
	if err := os.WriteFile(filepath.Join(root, "Projects/Quotes.md"), []byte(strings.Repeat(`"`, 17<<20)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Projects/Big.md"), []byte(strings.Repeat("words and more words\n", 9<<20/21)), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runScopedOnHTTPMCP(t, root, &Scope{Access: AccessRead})
	if text := mustFail(t, cs, "read_note", map[string]any{"path": "Projects/Quotes.md"}); !strings.Contains(text, "too large to send") {
		t.Errorf("a note of quotes: %.200s", text)
	}
	if text := mustCall(t, cs, "read_note", map[string]any{"path": "Projects/Big.md"}); !strings.Contains(text, "words and more words") {
		t.Errorf("an ordinary note: %.200s", text)
	}
}

// With no paths to keep a file inside, a subfolder its last file left is still a folder.
func TestHTTPMCPFullAccessMovesBackIntoAnEmptiedSubfolder(t *testing.T) {
	root := seedHTTPVault(t)
	if err := os.MkdirAll(filepath.Join(root, "assets/sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets/sub/a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runScopedOnHTTPMCP(t, root, nil)
	mustCall(t, cs, "move_asset", map[string]any{"path": "assets/sub/a.txt", "targetDir": "assets"})
	if text, failed := callTool(t, cs, "move_asset", map[string]any{"path": "assets/a.txt", "targetDir": "assets/sub"}); failed {
		t.Errorf("moving it back: %s", text)
	}
}

func TestFoldersHoldingHiddenNotesStayThroughHTTP(t *testing.T) {
	httpMCPBinary(t)
	testDenyFolder(t, runScopedOnHTTPMCP)
}

// A note that is a link can lead out of the vault. No client on another machine gets it,
// whatever its scope, by any tool.
func TestHTTPMCPLinksStayOutAtFullAccess(t *testing.T) {
	root := seedHTTPVault(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# outside\n\nserver-secret-line\n- [ ] server-secret-task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Projects/Link.md")); err != nil {
		t.Skip(err)
	}
	cs := runScopedOnHTTPMCP(t, root, nil)
	for tool, args := range map[string]map[string]any{
		"list_notes":  {},
		"search_text": {"query": "server-secret"},
		"list_tasks":  {"status": "all"},
	} {
		if text := mustCall(t, cs, tool, args); strings.Contains(text, "server-secret") || strings.Contains(text, "Link.md") {
			t.Errorf("%s: %s", tool, text)
		}
	}
	mustFail(t, cs, "read_note", map[string]any{"path": "Projects/Link.md"})
	// Other notes still answer: search goes on past what it dropped.
	if text := mustCall(t, cs, "search_text", map[string]any{"query": "Secret-note-text"}); !strings.Contains(text, "Plan.md") {
		t.Errorf("search: %s", text)
	}
}

func TestHTTPMCPCopiesStayInsideANoteOnlyScope(t *testing.T) {
	root := seedHTTPVault(t)
	cs := runScopedOnHTTPMCP(t, root, &Scope{Access: AccessWrite, Allow: []string{"Projects/Plan.md"}})
	mustFail(t, cs, "duplicate_note", map[string]any{"path": "Projects/Plan.md"})
	if _, err := os.Stat(filepath.Join(root, "Projects/Plan copy.md")); err == nil {
		t.Error("the copy landed outside what this client can see")
	}
}

// Text that JSON would escape for HTML still fits the note limit.
func TestHTTPMCPNotesWithAngleBracketsFitTheLimit(t *testing.T) {
	root := seedHTTPVault(t)
	cs := runScopedOnHTTPMCP(t, root, nil)
	body := strings.Repeat("<", 6<<20)
	if text, failed := callTool(t, cs, "write_note", map[string]any{"path": "Projects/Plan.md", "body": body}); failed {
		t.Errorf("%.200s", text)
	}
}

// Each setting someone meant to work and that cannot stops the server, with a
// message that names the problem and never a token.
func TestHTTPMCPRefusesToStart(t *testing.T) {
	binary := httpMCPBinary(t)
	sum := sha256.Sum256([]byte(httpMCPToken))
	digest := hex.EncodeToString(sum[:])
	credentials := func(t *testing.T, raw string) string {
		path := filepath.Join(t.TempDir(), "credentials.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	on := []string{"ZENNOTES_AUTH_TOKEN=" + httpMCPServer, "ZENNOTES_MCP_ENABLED=1"}
	cases := []struct {
		name string
		env  func(t *testing.T) []string
		want string
	}{
		{"no token", func(*testing.T) []string { return []string{"ZENNOTES_MCP_ACCESS=read"} }, "no token"},
		{"short token", func(*testing.T) []string { return []string{"ZENNOTES_MCP_TOKEN=short", "ZENNOTES_MCP_ACCESS=read"} }, "fewer than 32"},
		{"no access", func(*testing.T) []string { return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken} }, "access is required"},
		{"bad access", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_ACCESS=admin"}
		}, "access must be"},
		{"the server's token", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPServer, "ZENNOTES_MCP_ACCESS=read"}
		}, "server's own token"},
		{"a bad path", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_ACCESS=read", `ZENNOTES_MCP_ALLOW_PATHS=["../up"]`}
		}, "allow path"},
		{"a hidden path", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_ACCESS=read", `ZENNOTES_MCP_DENY_PATHS=[".git"]`}
		}, "deny path"},
		{"paths not JSON", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_ACCESS=read", "ZENNOTES_MCP_ALLOW_PATHS=Projects,Areas"}
		}, "JSON array"},
		{"any origin", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_ACCESS=read", "ZENNOTES_MCP_ALLOWED_ORIGINS=*"}
		}, "cannot be *"},
		{"file and variables", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[]}`)}
		}, "cannot be used together"},
		{"a missing file", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + filepath.Join(t.TempDir(), "nope.json")}
		}, "could not be read"},
		{"unknown version", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":2,"credentials":[]}`)}
		}, "version 2"},
		{"invalid JSON", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,`)}
		}, "CREDENTIALS_FILE"},
		{"a misspelled field", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[{"name":"a","tokenSha256":"`+digest+`","access":"read","alow":["Projects"]}]}`)}
		}, "unknown field"},
		{"an empty allow list", func(*testing.T) []string {
			return []string{"ZENNOTES_MCP_TOKEN=" + httpMCPToken, "ZENNOTES_MCP_ACCESS=read", "ZENNOTES_MCP_ALLOW_PATHS=[]"}
		}, "is empty"},
		{"a null allow list", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[{"name":"a","tokenSha256":"`+digest+`","access":"read","allow":null}]}`)}
		}, "is empty"},
		{"no credentials", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[]}`)}
		}, "no credentials"},
		{"a bad hash", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[{"name":"a","tokenSha256":"abc","access":"read"}]}`)}
		}, "tokenSha256"},
		{"two names", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[{"name":"a","tokenSha256":"`+digest+`","access":"read"},{"name":"a","tokenSha256":"`+digest+`","access":"read"}]}`)}
		}, "appears twice"},
		{"one token twice", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[{"name":"a","tokenSha256":"`+digest+`","access":"read"},{"name":"b","tokenSha256":"`+digest+`","access":"write"}]}`)}
		}, "same token"},
		{"file access missing", func(t *testing.T) []string {
			return []string{"ZENNOTES_MCP_CREDENTIALS_FILE=" + credentials(t, `{"version":1,"credentials":[{"name":"a","tokenSha256":"`+digest+`"}]}`)}
		}, "access is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := t.TempDir()
			command := exec.Command(binary)
			command.Dir = state
			command.Env = append(append(serverEnv(filepath.Join(state, "vault"), state, "ZENNOTES_BIND=127.0.0.1:0"), on...), tc.env(t)...)
			done := make(chan struct{})
			var out []byte
			var err error
			go func() { out, err = command.CombinedOutput(); close(done) }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				t.Fatal("the server started")
			}
			if err == nil {
				t.Fatalf("the server exited cleanly:\n%s", out)
			}
			text := string(out)
			if !strings.Contains(text, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, text)
			}
			for _, secret := range []string{httpMCPToken, httpMCPServer} {
				if strings.Contains(text, secret) {
					t.Errorf("the message holds a token:\n%s", text)
				}
			}
		})
	}
	t.Run("a vault that can change", func(t *testing.T) {
		state := t.TempDir()
		command := exec.Command(binary)
		command.Dir = state
		for _, entry := range serverEnv(filepath.Join(state, "vault"), state, "ZENNOTES_BIND=127.0.0.1:0", "ZENNOTES_AUTH_TOKEN="+httpMCPServer,
			"ZENNOTES_MCP_ENABLED=1", "ZENNOTES_MCP_TOKEN="+httpMCPToken, "ZENNOTES_MCP_ACCESS=read") {
			if !strings.HasPrefix(entry, "ZENNOTES_VAULT_PATH=") {
				command.Env = append(command.Env, entry)
			}
		}
		out, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "ZENNOTES_VAULT_PATH") {
			t.Errorf("%v:\n%s", err, out)
		}
	})
	// No server token at all, on loopback where the server otherwise allows it.
	t.Run("an open REST API", func(t *testing.T) {
		state := t.TempDir()
		command := exec.Command(binary)
		command.Dir = state
		command.Env = append(serverEnv(filepath.Join(state, "vault"), state, "ZENNOTES_BIND=127.0.0.1:0"),
			"ZENNOTES_MCP_ENABLED=1", "ZENNOTES_MCP_TOKEN="+httpMCPToken, "ZENNOTES_MCP_ACCESS=read")
		out, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "ZENNOTES_AUTH_TOKEN") {
			t.Errorf("%v:\n%s", err, out)
		}
	})
}
