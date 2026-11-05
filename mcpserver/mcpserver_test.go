package mcpserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/mcp"
)

const (
	testToken = "mcp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	initMsg   = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"raw","version":"0"}}}`
)

func call(tool string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, tool)
}

// noServer stands in for the server's API, for tests that never reach it.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var noServer = roundTripFunc(func(*http.Request) (*http.Response, error) {
	return nil, errors.New("this test should not reach the server")
})

func options(mutate ...func(*Options)) Options {
	o := Options{
		Transport:   noServer,
		BaseURL:     "http://zennotes.internal",
		Credentials: []Credential{{Name: "one", TokenSHA256: HashToken(testToken), Access: "read"}},
		Version:     "test",
	}
	for _, m := range mutate {
		m(&o)
	}
	return o
}

func serve(t *testing.T, o Options) *httptest.Server {
	t.Helper()
	h, err := NewHandler(o)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return server
}

func post(t *testing.T, url, token, origin, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestNewHandlerRefuses(t *testing.T) {
	cases := map[string]func(*Options){
		"no transport":   func(o *Options) { o.Transport = nil },
		"no credentials": func(o *Options) { o.Credentials = nil },
		"no access":      func(o *Options) { o.Credentials[0].Access = "" },
		"unknown access": func(o *Options) { o.Credentials[0].Access = "admin" },
		"a bad path":     func(o *Options) { o.Credentials[0].Allow = []string{"../up"} },
		"a repeated token": func(o *Options) {
			o.Credentials = append(o.Credentials, Credential{Name: "two", TokenSHA256: o.Credentials[0].TokenSHA256, Access: "write"})
		},
	}
	for name, mutate := range cases {
		if _, err := NewHandler(options(mutate)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestOperatorWordsInScopeErrors(t *testing.T) {
	_, err := NewHandler(options(func(o *Options) { o.Credentials[0].Allow = []string{"../up"} }))
	if err == nil || strings.Contains(err.Error(), "--allow-path") || !strings.Contains(err.Error(), `"one"`) {
		t.Errorf("error is for someone running zn: %v", err)
	}
}

func TestOriginIsCheckedFirst(t *testing.T) {
	server := serve(t, options(func(o *Options) { o.AllowedOrigins = []string{"https://ok.example"} }))
	for _, tc := range []struct {
		origin string
		token  string
		want   int
	}{
		{"https://evil.example", testToken, http.StatusForbidden},
		{"https://evil.example", "", http.StatusForbidden},
		{"https://ok.example.evil", testToken, http.StatusForbidden},
		{"null", testToken, http.StatusForbidden},
		{"https://ok.example", testToken, http.StatusOK},
		{"https://ok.example", "", http.StatusUnauthorized},
		{"", testToken, http.StatusOK},
	} {
		if status, body := post(t, server.URL, tc.token, tc.origin, initMsg); status != tc.want {
			t.Errorf("origin %q token %q: %d, want %d: %s", tc.origin, tc.token, status, tc.want, body)
		}
	}
}

// A browser page that can reach the server cannot use a rebound name to look local.
func TestLoopbackBehindAProxyAcceptsItsPublicHost(t *testing.T) {
	server := serve(t, options())
	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(initMsg))
	req.Host = "notes.example.com"
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d", resp.StatusCode)
	}
}

// Each request stands alone. A session id would be something one credential could
// present to reach another's scope.
func TestNoSessionIsIssued(t *testing.T) {
	server := serve(t, options())
	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(initMsg))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Mcp-Session-Id") != "" {
		t.Errorf("%d, session %q", resp.StatusCode, resp.Header.Get("Mcp-Session-Id"))
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("content type %q", got)
	}
}

func TestBadBodiesStopAtTheHandler(t *testing.T) {
	var reached sync.Mutex
	touched := false
	server := serve(t, options(func(o *Options) {
		o.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			reached.Lock()
			defer reached.Unlock()
			touched = true
			return nil, errors.New("reached the server")
		})
	}))
	for name, body := range map[string]string{
		"deep":          strings.Repeat("[", 900_000),
		"deep objects":  strings.Repeat(`{"a":`, 150_000),
		"not JSON":      `{"jsonrpc":`,
		"empty":         ``,
		"a batch":       `[` + call("list_notes") + `]`,
		"spaces, batch": " \r\n\t[" + call("list_notes") + `]`,
		"an empty one":  `[]`,
	} {
		if status, _ := post(t, server.URL, testToken, "", body); status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, status)
		}
	}
	reached.Lock()
	defer reached.Unlock()
	if touched {
		t.Error("a bad body reached a tool")
	}
}

// Without the check, this ends the whole process: the SDK's decoder recurses, and
// nothing can recover from a stack overflow.
func TestDeepNestingStopsAtTheHandler(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := serve(t, options(func(o *Options) {
		o.MaxNoteBytes = 10 << 20
		o.Credentials = []Credential{{Name: "w", TokenSHA256: HashToken(writer), Access: "write"}}
	}))
	const depth = 2_000_000
	deep := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"a":` + strings.Repeat("[", depth) + strings.Repeat("]", depth) + `}}`
	if status, _ := post(t, server.URL, writer, "", deep); status != http.StatusBadRequest {
		t.Errorf("status %d", status)
	}
	if status, _ := post(t, server.URL, writer, "", initMsg); status != http.StatusOK {
		t.Errorf("after: %d", status)
	}
}

func TestReadCredentialsSendLess(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	commenter := "mcp-cccccccccccccccccccccccccccccccccccccccccccc"
	server := serve(t, options(func(o *Options) {
		o.MaxNoteBytes = 10 << 20
		o.Credentials = append(o.Credentials,
			Credential{Name: "two", TokenSHA256: HashToken(writer), Access: "write"},
			Credential{Name: "three", TokenSHA256: HashToken(commenter), Access: "comment"})
	}))
	padded := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", smallRequestBytes) + `"}}`
	if status, _ := post(t, server.URL, testToken, "", padded); status != http.StatusRequestEntityTooLarge {
		t.Errorf("read: %d", status)
	}
	if status, _ := post(t, server.URL, commenter, "", padded); status != http.StatusRequestEntityTooLarge {
		t.Errorf("comment: %d", status)
	}
	if status, _ := post(t, server.URL, writer, "", padded); status != http.StatusOK {
		t.Errorf("write: %d", status)
	}
	// Chunked, with no length to check first.
	request, _ := http.NewRequest(http.MethodPost, server.URL, io.MultiReader(strings.NewReader(padded)))
	request.ContentLength = -1
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("chunked: %d", response.StatusCode)
	}
}

// Only bytes that arrive count, so announcing a large body and sending nothing holds
// nothing back from anyone.
func TestAnnouncedBodiesHoldNothing(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := serve(t, options(func(o *Options) {
		o.MaxNoteBytes = 10 << 20
		o.Credentials = []Credential{{Name: "w", TokenSHA256: HashToken(writer), Access: "write"}}
	}))
	for range 5 {
		conn, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n{", writer, 22<<20)
	}
	time.Sleep(100 * time.Millisecond)
	if status, body := post(t, server.URL, writer, "", initMsg); status != http.StatusOK {
		t.Errorf("%d %s", status, body)
	}
}

// Bytes that did arrive count against the one who sent them, and nobody else.
func TestOneCredentialFillsOnlyItsOwnShareOfBodies(t *testing.T) {
	first := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	second := "mcp-cccccccccccccccccccccccccccccccccccccccccccc"
	const budget = 64 << 20
	server := serve(t, options(func(o *Options) {
		o.MaxNoteBytes = 1 << 20
		o.MaxBodyBytes = budget
		o.Credentials = []Credential{
			{Name: "a", TokenSHA256: HashToken(first), Access: "write"},
			{Name: "b", TokenSHA256: HashToken(second), Access: "write"},
		}
	}))
	// Sends most of a large body, and stops. Its share of the 64 MiB is 32.
	hold := func(size int) {
		conn, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", first, 20<<20)
		if _, err := conn.Write(make([]byte, size)); err != nil {
			t.Fatal(err)
		}
	}
	hold(16 << 20)
	hold(15 << 20)
	time.Sleep(200 * time.Millisecond)
	small := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 2<<20) + `"}}`
	if status, _ := post(t, server.URL, first, "", small); status != http.StatusServiceUnavailable {
		t.Errorf("the credential that filled its share: %d", status)
	}
	if status, body := post(t, server.URL, second, "", small); status != http.StatusOK {
		t.Errorf("the other credential: %d %.100s", status, body)
	}
}

// A chunked body says nothing of its size, so it must not be assumed to be the largest.
func TestChunkedRequestsReserveWhatTheySend(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := serve(t, options(func(o *Options) {
		o.MaxNoteBytes = 10 << 20
		o.Credentials = []Credential{{Name: "w", TokenSHA256: HashToken(writer), Access: "write"}}
	}))
	var wait sync.WaitGroup
	statuses := make(chan int, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			pipeReader, pipeWriter := io.Pipe()
			go func() {
				_, _ = io.WriteString(pipeWriter, initMsg)
				time.Sleep(100 * time.Millisecond)
				pipeWriter.Close()
			}()
			request, _ := http.NewRequest(http.MethodPost, server.URL, pipeReader)
			request.Header.Set("Authorization", "Bearer "+writer)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Error(err)
				return
			}
			response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	wait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Errorf("a chunked request: %d", status)
		}
	}
}

// Within what json.Valid allows, and past what any tool needs. The SDK's decoder takes
// time quadratic in depth, and goes on after the client has gone.
func TestDeepButValidJSONIsRefusedQuickly(t *testing.T) {
	server := serve(t, options())
	const depth = 5000
	body := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"a":` + strings.Repeat("[", depth) + strings.Repeat("]", depth) + `}}`
	started := time.Now()
	if status, _ := post(t, server.URL, testToken, "", body); status != http.StatusBadRequest {
		t.Errorf("status %d", status)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("took %s", took)
	}
}

// A refusal has to reach a client still sending its body, which a reset would not.
func TestARefusedClientReadsTheRefusal(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	defer close(release)
	server := serve(t, options(func(o *Options) {
		o.MaxInFlight = 1
		o.SlotWait = 50 * time.Millisecond
		o.MaxNoteBytes = 10 << 20
		o.Credentials = []Credential{{Name: "w", TokenSHA256: HashToken(writer), Access: "write"}}
		o.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			once.Do(func() { close(entered) })
			<-release
			return nil, errors.New("done")
		})
	}))
	go func() {
		request, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(call("list_notes")))
		request.Header.Set("Authorization", "Bearer "+writer)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if response, err := http.DefaultClient.Do(request); err == nil {
			response.Body.Close()
		}
	}()
	<-entered
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const size = 4 << 20
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", writer, size)
	if _, err := conn.Write(make([]byte, size)); err != nil {
		t.Fatalf("the server hung up while the body was being sent: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Retry-After") == "" {
		t.Errorf("%d, Retry-After %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
}

// A body that takes its time does not use up the time for the answer, and the longest
// tool call fits inside that time.
func TestWriteDeadlineStartsAfterTheBody(t *testing.T) {
	if defaultWriteTimeout <= mcp.HostedCallLimit {
		t.Errorf("write deadline %s, longest call %s", defaultWriteTimeout, mcp.HostedCallLimit)
	}
	server := serve(t, options(func(o *Options) {
		o.WriteTimeout = 300 * time.Millisecond
		o.ReadTimeout = 5 * time.Second
	}))
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nContent-Length: %d\r\n\r\n", testToken, len(initMsg))
	time.Sleep(600 * time.Millisecond)
	fmt.Fprint(conn, initMsg)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "serverInfo") {
		t.Errorf("%d %s", response.StatusCode, body)
	}
}

// What counts is the answer as it goes out, JSON inside JSON, and not the note: a note of
// quotes takes four times its size, and an ordinary one about its own.
func TestAnAnswerHasALimitOnTheWire(t *testing.T) {
	var note string
	const size = 1 << 20
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		switch {
		case strings.HasSuffix(r.URL.Path, "/notes/read"):
			raw, _ := json.Marshal(map[string]any{"path": "n.md", "title": "n", "folder": "inbox", "body": note})
			body = string(raw)
		case strings.HasSuffix(r.URL.Path, "/notes"):
			body = `[{"path":"n.md","title":"n","folder":"inbox"}]`
		case strings.HasSuffix(r.URL.Path, "/vault"):
			body = `{"linkedSystemFolders":[]}`
		case strings.HasSuffix(r.URL.Path, "/folders"), strings.HasSuffix(r.URL.Path, "/assets"):
			body = "[]"
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	server := serve(t, options(func(o *Options) {
		o.Transport = transport
		o.MaxResponseBytes = 3 * size
	}))
	message := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_note","arguments":{"path":"n.md"}}}`
	note = strings.Repeat("a", 2*size)
	if status, body := post(t, server.URL, testToken, "", message); status != http.StatusOK || !strings.Contains(body, "aaaa") || strings.Contains(body, `"isError":true`) {
		t.Errorf("an ordinary note: %d %.200s", status, body)
	}
	note = strings.Repeat(`"`, size)
	status, body := post(t, server.URL, testToken, "", message)
	if status != http.StatusOK || !strings.Contains(body, "too large to send") || !strings.Contains(body, `"isError":true`) || len(body) > 1000 {
		t.Errorf("a note of quotes: %d %.200s", status, body)
	}
}

func TestTooDeep(t *testing.T) {
	nested := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	for body, want := range map[string]bool{
		nested(64): false,
		nested(65): true,
		`{"a":"` + strings.Repeat("[", 500) + `"}`:   false,
		`{"a":"\"` + strings.Repeat("[", 500) + `"}`: false,
		`{"a":"\\","b":` + nested(65) + `}`:          true,
		`{"a":` + nested(64) + `}`:                   true,
		`[1,{"b":[2,{"c":3}]}]`:                      false,
	} {
		if got := tooDeep([]byte(body), 64); got != want {
			t.Errorf("%.40s: %v", body, got)
		}
	}
}

// A request waits a moment for a slot rather than being turned away.
func TestARequestWaitsBrieflyForASlot(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	server := serve(t, options(func(o *Options) {
		o.MaxInFlight = 1
		o.SlotWait = 5 * time.Second
		o.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			once.Do(func() { close(entered) })
			<-release
			return nil, errors.New("done")
		})
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		post(t, server.URL, testToken, "", call("list_notes"))
	}()
	<-entered
	time.AfterFunc(300*time.Millisecond, func() { close(release) })
	if status, body := post(t, server.URL, testToken, "", initMsg); status != http.StatusOK {
		t.Errorf("%d %s", status, body)
	}
	<-done
}

// However many are open, only so many tool calls run, across credentials.
func TestToolCallsTakeTurns(t *testing.T) {
	other := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var (
		mu            sync.Mutex
		running, most int
	)
	server := serve(t, options(func(o *Options) {
		o.MaxToolCalls = 2
		o.Credentials = append(o.Credentials, Credential{Name: "two", TokenSHA256: HashToken(other), Access: "read"})
		o.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			running++
			most = max(most, running)
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			return nil, errors.New("done")
		})
	}))
	var wait sync.WaitGroup
	for i := range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			token := testToken
			if i%2 == 0 {
				token = other
			}
			post(t, server.URL, token, "", call("list_notes"))
		}()
	}
	wait.Wait()
	mu.Lock()
	defer mu.Unlock()
	if most > 2 {
		t.Errorf("%d ran at once", most)
	}
	if most == 0 {
		t.Error("nothing ran")
	}
}

func TestNullArgumentsAreNoArguments(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := serve(t, options(func(o *Options) {
		o.Credentials = []Credential{{Name: "w", TokenSHA256: HashToken(writer), Access: "write"}}
	}))
	_, body := post(t, server.URL, writer, "", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_comment","arguments":null}}`)
	if strings.Contains(body, "The tool failed") || !strings.Contains(body, "isError") {
		t.Errorf("%s", body)
	}
}

// What a client reads names no internal route or path on the server.
func TestServerErrorsAreRewordedForAClient(t *testing.T) {
	respond := func(status int, text string) roundTripFunc {
		return func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(text)), Request: r}, nil
		}
	}
	for _, tc := range []struct {
		status int
		text   string
		want   string
	}{
		{400, "open /tmp/multipart-123: no such file or directory", "no such file or directory"},
		{400, "http: request body too large", "request body too large"},
		{413, "too large to read over MCP", "too large to read over MCP"},
		{500, "internal server error", "could not do that"},
	} {
		server := serve(t, options(func(o *Options) { o.Transport = respond(tc.status, tc.text) }))
		_, body := post(t, server.URL, testToken, "", call("list_notes"))
		if !strings.Contains(body, tc.want) {
			t.Errorf("%d %s: %s", tc.status, tc.text, body)
		}
		for _, leak := range []string{"/api/", "/tmp/", "multipart-"} {
			if strings.Contains(body, leak) {
				t.Errorf("says %q: %s", leak, body)
			}
		}
	}
}

// A length that is only claimed reserves nothing, or anyone with a read token could
// use up the budget by announcing a body it will never send.
func TestAnAnnouncedLengthPastTheLimitIsRefusedAtOnce(t *testing.T) {
	server := serve(t, options())
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n{", testToken, 1<<40)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d", response.StatusCode)
	}
}

func TestOneCredentialCannotTakeEverySlot(t *testing.T) {
	other := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := serve(t, options(func(o *Options) {
		o.MaxInFlight = 4
		o.SlotWait = 50 * time.Millisecond
		o.Credentials = append(o.Credentials, Credential{Name: "two", TokenSHA256: HashToken(other), Access: "read"})
	}))
	// Slow bodies, each holding a slot.
	for range 4 {
		conn, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{", testToken)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, _ := post(t, server.URL, testToken, "", initMsg)
		if status == http.StatusServiceUnavailable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the first credential was never limited: %d", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status, body := post(t, server.URL, other, "", initMsg); status != http.StatusOK {
		t.Errorf("the other credential: %d %s", status, body)
	}
}

// A read credential sends little, so it gets little time to send it.
func TestReadCredentialsGetLessTimeToSend(t *testing.T) {
	h, err := NewHandler(options())
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*handler).credentials[0].read; got != smallReadTimeout {
		t.Errorf("read timeout %s", got)
	}
	h, err = NewHandler(options(func(o *Options) { o.Credentials[0].Access = "write" }))
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*handler).credentials[0].read; got != defaultReadTimeout {
		t.Errorf("write timeout %s", got)
	}
}

func TestAPanicAnswersOnlyThatCall(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
	)
	server := serve(t, options(func(o *Options) {
		o.Logf = func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, fmt.Sprintf(format, args...))
		}
		o.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			panic("Private/Note.md went wrong")
		})
	}))
	status, body := post(t, server.URL, testToken, "", call("list_notes"))
	if status != http.StatusOK || !strings.Contains(body, "The tool failed") || !strings.Contains(body, `"isError":true`) {
		t.Errorf("%d %s", status, body)
	}
	mu.Lock()
	defer mu.Unlock()
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "outcome=panic") || strings.Contains(text, "Private") {
		t.Errorf("log: %s", text)
	}
	if status, _ := post(t, server.URL, testToken, "", initMsg); status != http.StatusOK {
		t.Errorf("after the panic: %d", status)
	}
}

func TestRejectionsAreCountedOnceAMinute(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
	)
	server := serve(t, options(func(o *Options) {
		o.Logf = func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, fmt.Sprintf(format, args...))
		}
	}))
	for range 5 {
		post(t, server.URL, "wrong-token-that-must-stay-out-of-logs", "", initMsg)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 || !strings.Contains(lines[0], "rejected 1 requests") {
		t.Errorf("log: %q", lines)
	}
}

// A tool that would always refuse under a path scope is not listed, and is unknown.
func TestToolsThatAlwaysRefuseAreHidden(t *testing.T) {
	writer := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	whole := "mcp-cccccccccccccccccccccccccccccccccccccccccccc"
	server := serve(t, options(func(o *Options) {
		o.Credentials = []Credential{
			{Name: "part", TokenSHA256: HashToken(writer), Access: "write", Allow: []string{"Projects"}},
			{Name: "whole", TokenSHA256: HashToken(whole), Access: "write"},
		}
	}))
	const list = `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`
	for _, tool := range []string{"rename_note", "read_asset", "duplicate_asset", "rename_asset", "move_asset"} {
		_, part := post(t, server.URL, writer, "", list)
		if strings.Contains(part, `"`+tool+`"`) {
			t.Errorf("%s is listed to a path scope", tool)
		}
		_, everything := post(t, server.URL, whole, "", list)
		if !strings.Contains(everything, `"`+tool+`"`) {
			t.Errorf("%s is not listed at full access", tool)
		}
		if _, body := post(t, server.URL, writer, "", call(tool)); !strings.Contains(body, "Unknown tool: "+tool) {
			t.Errorf("%s: %s", tool, body)
		}
	}
}

func TestHiddenToolIsAToolError(t *testing.T) {
	server := serve(t, options())
	for _, tool := range []string{"write_note", "no_such_tool"} {
		status, body := post(t, server.URL, testToken, "", call(tool))
		if status != http.StatusOK || !strings.Contains(body, "Unknown tool: "+tool) || !strings.Contains(body, `"isError":true`) {
			t.Errorf("%s: %d %s", tool, status, body)
		}
	}
}

func TestToolsDependOnTheToken(t *testing.T) {
	other := "mcp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := serve(t, options(func(o *Options) {
		o.Credentials = append(o.Credentials, Credential{Name: "two", TokenSHA256: HashToken(other), Access: "write"})
	}))
	count := func(token string) int {
		_, body := post(t, server.URL, token, "", `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`)
		var reply struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return len(reply.Result.Tools)
	}
	read, write := count(testToken), count(other)
	if read == 0 || read >= write {
		t.Errorf("read lists %d tools, write %d", read, write)
	}
}

// The internal address, and advice meant for someone running zn, stay out of what a
// client reads, however the call fails.
func TestRefusalsSayNothingOfTheInside(t *testing.T) {
	respond := func(status int) roundTripFunc {
		return func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{}, Body: io.NopCloser(strings.NewReader("no")), Request: r}, nil
		}
	}
	transports := map[string]roundTripFunc{
		"401":      respond(http.StatusUnauthorized),
		"403":      respond(http.StatusForbidden),
		"500":      respond(http.StatusInternalServerError),
		"no reach": noServer,
	}
	for name, transport := range transports {
		server := serve(t, options(func(o *Options) { o.Transport = transport }))
		_, body := post(t, server.URL, testToken, "", call("list_notes"))
		if !strings.Contains(body, `"isError":true`) {
			t.Errorf("%s: not an error: %s", name, body)
		}
		for _, leak := range []string{"zennotes.internal", "zn connect", "ZENNOTES_REMOTE_TOKEN", "secret store"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: says %q: %s", name, leak, body)
			}
		}
	}
}

func TestLogLineHoldsNoArguments(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
	)
	server := serve(t, options(func(o *Options) {
		o.Logf = func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, fmt.Sprintf(format, args...))
		}
	}))
	post(t, server.URL, testToken, "", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_note","arguments":{"path":"Private/Name.md"}}}`)
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 || !strings.Contains(lines[0], "credential=one tool=read_note outcome=error") || strings.Contains(lines[0], "Private") {
		t.Errorf("log: %q", lines)
	}
}

func TestRequestsOverTheLimitAre503(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 8)
	server := serve(t, options(func(o *Options) {
		o.MaxInFlight = 2
		o.SlotWait = 50 * time.Millisecond
		o.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			entered <- struct{}{}
			<-release
			return nil, errors.New("done")
		})
	}))
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			post(t, server.URL, testToken, "", call("list_notes"))
		}()
	}
	<-entered
	<-entered
	status, _ := post(t, server.URL, testToken, "", call("list_notes"))
	if status != http.StatusServiceUnavailable {
		t.Errorf("a third request: %d", status)
	}
	// A wrong token never takes a slot, or anyone could fill them.
	if status, _ := post(t, server.URL, "wrong", "", initMsg); status != http.StatusUnauthorized {
		t.Errorf("a wrong token: %d", status)
	}
	close(release)
	wait.Wait()
	if status, body := post(t, server.URL, testToken, "", initMsg); status != http.StatusOK {
		t.Errorf("after the others finished: %d %s", status, body)
	}
}

// A client that stops sending its body does not hold a slot for long.
func TestSlowBodyHitsTheReadDeadline(t *testing.T) {
	server := serve(t, options(func(o *Options) {
		o.ReadTimeout = 150 * time.Millisecond
		o.MaxInFlight = 1
	}))
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{", testToken)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err == nil {
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Errorf("status %d", response.StatusCode)
		}
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("the server waited for the body past its deadline")
	}
	// The slot came back.
	if status, body := post(t, server.URL, testToken, "", initMsg); status != http.StatusOK {
		t.Errorf("after the slow client: %d %s", status, body)
	}
}

// A tool can take longer than a body may take to arrive.
func TestReadDeadlineDoesNotCutOffASlowTool(t *testing.T) {
	server := serve(t, options(func(o *Options) {
		o.ReadTimeout = 100 * time.Millisecond
		o.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			time.Sleep(400 * time.Millisecond)
			return nil, errors.New("slow answer")
		})
	}))
	status, body := post(t, server.URL, testToken, "", call("list_notes"))
	if status != http.StatusOK || !strings.Contains(body, `"isError":true`) {
		t.Errorf("the response was lost: %d %q", status, body)
	}
}

// A client that stops reading a large response does not hold a slot for long.
func TestWriteDeadlineFreesASlot(t *testing.T) {
	note, err := json.Marshal(map[string]any{"path": "n.md", "title": "n", "folder": "inbox", "body": strings.Repeat("x", 32<<20)})
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		switch {
		case strings.HasSuffix(r.URL.Path, "/notes/read"):
			body = string(note)
		case strings.HasSuffix(r.URL.Path, "/notes"):
			body = `[{"path":"n.md","title":"n","folder":"inbox"}]`
		case strings.HasSuffix(r.URL.Path, "/vault"):
			body = `{"linkedSystemFolders":[]}`
		case strings.HasSuffix(r.URL.Path, "/folders"), strings.HasSuffix(r.URL.Path, "/assets"):
			body = "[]"
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	message := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_note","arguments":{"path":"n.md"}}}`
	// The note really is too large to fit in a socket's buffers.
	whole := serve(t, options(func(o *Options) { o.Transport = transport }))
	if status, body := post(t, whole.URL, testToken, "", message); status != http.StatusOK || len(body) < 32<<20 {
		t.Fatalf("the note did not come back: %d, %d bytes", status, len(body))
	}
	server := serve(t, options(func(o *Options) {
		o.WriteTimeout = 500 * time.Millisecond
		o.MaxInFlight = 1
		o.Transport = transport
	}))
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Sent, and never read.
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nContent-Length: %d\r\n\r\n%s", testToken, len(message), message)
	time.Sleep(100 * time.Millisecond)
	deadline := time.Now().Add(15 * time.Second)
	for {
		status, _ := post(t, server.URL, testToken, "", initMsg)
		if status == http.StatusOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the slot never came back: %d", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A kept-alive connection can carry other routes next, through a proxy. A deadline
// this handler set must not still be running for them.
func TestDeadlinesEndWithTheRequest(t *testing.T) {
	h, err := NewHandler(options(func(o *Options) {
		o.WriteTimeout = 200 * time.Millisecond
		o.ReadTimeout = 200 * time.Millisecond
	}))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)
	mux.HandleFunc("/other", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "fine") })
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nContent-Length: %d\r\n\r\n%s", testToken, len(initMsg), initMsg)
	first, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, first.Body)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first: %d", first.StatusCode)
	}

	time.Sleep(400 * time.Millisecond) // past both deadlines

	fmt.Fprintf(conn, "GET /other HTTP/1.1\r\nHost: x\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	second, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("the next request on this connection failed: %v", err)
	}
	body, _ := io.ReadAll(second.Body)
	second.Body.Close()
	if string(body) != "fine" {
		t.Errorf("next request: %d %q", second.StatusCode, body)
	}
}

func TestNotPostIsNotServed(t *testing.T) {
	server := serve(t, options())
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(method, server.URL, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", method, resp.StatusCode)
		}
	}
}
