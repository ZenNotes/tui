package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The cases mirror packages/shared-domain/src/remote-headers.test.ts in the
// desktop app (ZenNotes/zennotes #1091): both read the same lines.

const cloudflare = "CF-Access-Client-Id: abc123.access\nCF-Access-Client-Secret: s3cr3t"

var cloudflareHeaders = []Header{
	{Name: "CF-Access-Client-Id", Value: "abc123.access"},
	{Name: "CF-Access-Client-Secret", Value: "s3cr3t"},
}

func TestParseHeadersReadsAServiceToken(t *testing.T) {
	got, err := ParseHeaders(cloudflare)
	if err != nil || !reflect.DeepEqual(got, cloudflareHeaders) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseHeadersSkipsBlanksAndCommentsAndKeepsColonsInTheValue(t *testing.T) {
	got, err := ParseHeaders("\n# gateway\n  X-Trace :  a:b:c  \r\n\n")
	if err != nil || !reflect.DeepEqual(got, []Header{{Name: "X-Trace", Value: "a:b:c"}}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := ParseHeaders("  \n "); err != nil || len(got) != 0 {
		t.Fatalf("empty box: %v, %v", got, err)
	}
}

func TestParseHeadersNamesTheLineThatIsWrong(t *testing.T) {
	cases := map[string]string{
		"X-A: 1\nno colon here":   "Line 2: write each header as Name: value.",
		"X-Empty:":                "Line 1: X-Empty has no value.",
		"Authorization: Bearer x": "Line 1: use the auth token for Authorization.",
		"X-A: 1\nx-a: 2":          "Line 2: x-a is listed twice.",
	}
	for input, want := range cases {
		if _, err := ParseHeaders(input); err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", input, err, want)
		}
	}
	for _, input := range []string{"Bad Name: 1", "Host: x", "content-length: 1", "Upgrade: x", "Sec-WebSocket-Key: x", "Connection: x", "X-A: one\x00two"} {
		if _, err := ParseHeaders(input); err == nil {
			t.Errorf("%q: expected a refusal", input)
		}
	}
}

func TestParseHeadersCapsTheList(t *testing.T) {
	var lines []string
	for i := 0; i <= MaxHeaders; i++ {
		lines = append(lines, "X-H"+strings.Repeat("a", i+1)+": v")
	}
	if _, err := ParseHeaders(strings.Join(lines, "\n")); err == nil {
		t.Fatal("expected the cap to refuse 21 headers")
	}
}

func TestHeadersFromEnv(t *testing.T) {
	env := func(value string) func(string) string {
		return func(key string) string {
			if key == HeadersEnv {
				return value
			}
			return ""
		}
	}
	if got, err := HeadersFromEnv(env("")); err != nil || got != nil {
		t.Fatalf("unset: %v, %v", got, err)
	}
	for _, raw := range []string{cloudflare, strings.Replace(cloudflare, "\n", `\n`, 1)} {
		got, err := HeadersFromEnv(env(raw))
		if err != nil || !reflect.DeepEqual(got, cloudflareHeaders) {
			t.Fatalf("%q: got %v, %v", raw, got, err)
		}
	}
	_, err := HeadersFromEnv(env("nonsense"))
	if err == nil || err.Error() != "ZENNOTES_REMOTE_HEADERS: Line 1: write each header as Name: value." {
		t.Fatalf("got %v", err)
	}
}

func TestApplyHeadersNeverReplacesOneAlreadySet(t *testing.T) {
	h := http.Header{}
	h.Set("X-A", "kept")
	ApplyHeaders(h, []Header{{Name: "X-A", Value: "ignored"}, {Name: "CF-Access-Client-Id", Value: "id"}})
	if h.Get("X-A") != "kept" || h.Get("CF-Access-Client-Id") != "id" {
		t.Fatal(h)
	}
}

// gatedServer stands in for ZenNotes behind a gateway: without both headers it
// answers 403 with an HTML page, the way Cloudflare Access does.
func gatedServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	passes := func(r *http.Request) bool {
		return r.Header.Get("CF-Access-Client-Id") == "abc123.access" && r.Header.Get("CF-Access-Client-Secret") == "s3cr3t"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := passes(r)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+map[bool]string{true: " pass", false: " blocked"}[ok])
		mu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><body>Forbidden</body></html>"))
			return
		}
		switch r.URL.Path {
		case "/api/vault":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"root":"/v","name":"v"}`))
		case "/api/assets/raw":
			_, _ = w.Write([]byte("bytes"))
		case "/api/assets/upload":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"path":"assets/a.png"}`))
		case "/api/watch":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"kind":"add","path":"gated.md"}`))
			_, _, _ = conn.Read(ctx)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestClientSendsTheHeadersOnEveryKindOfRequest(t *testing.T) {
	server, seen := gatedServer(t)
	client := NewClient(server.URL, "tok")
	client.Headers, client.headersErr = cloudflareHeaders, nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var info map[string]any
	if err := client.Get(ctx, "/api/vault", &info); err != nil || info["name"] != "v" {
		t.Fatalf("json: %v %v", info, err)
	}
	if raw, err := client.ReadAsset(ctx, "a.png"); err != nil || string(raw) != "bytes" {
		t.Fatalf("raw: %q %v", raw, err)
	}
	if out, err := client.ImportAsset(ctx, "inbox/n.md", "a.png", strings.NewReader("x")); err != nil || out.Path != "assets/a.png" {
		t.Fatalf("upload: %v %v", out, err)
	}
	events, err := client.WatchChanges(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if ev := <-events; ev.Path != "gated.md" || ev.Err != nil {
		t.Fatalf("watch event: %+v", ev)
	}
	for _, line := range seen() {
		if !strings.HasSuffix(line, " pass") {
			t.Fatalf("a request missed the headers: %v", seen())
		}
	}
	if len(seen()) != 4 {
		t.Fatalf("expected 4 requests, got %v", seen())
	}
}

func TestWithoutTheHeadersTheRefusalNamesTheGateway(t *testing.T) {
	server, _ := gatedServer(t)
	client := NewClient(server.URL, "tok")
	client.Headers, client.headersErr = nil, nil
	err := client.Get(context.Background(), "/api/vault", nil)
	if StatusOf(err) != http.StatusForbidden || !strings.Contains(err.Error(), "access gateway such as Cloudflare Access") || strings.Contains(err.Error(), "<html>") {
		t.Fatalf("got %v", err)
	}
	if _, err := client.WatchChanges(context.Background()); err == nil {
		t.Fatal("the handshake should be refused without the headers")
	}
}

func TestABadVariableFailsBeforeAnyRequest(t *testing.T) {
	server, seen := gatedServer(t)
	client := NewClient(server.URL, "tok")
	client.headersErr = errors.New("ZENNOTES_REMOTE_HEADERS: Line 1: write each header as Name: value.")
	if err := client.Get(context.Background(), "/api/vault", nil); err == nil || !strings.HasPrefix(err.Error(), "ZENNOTES_REMOTE_HEADERS: ") {
		t.Fatalf("got %v", err)
	}
	if _, err := client.WatchChanges(context.Background()); err == nil {
		t.Fatal("watch should fail too")
	}
	if len(seen()) != 0 {
		t.Fatalf("nothing should reach the server: %v", seen())
	}
}

func TestARedirectToASignInPageNamesTheGateway(t *testing.T) {
	signIn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>sign in</html>"))
	}))
	defer signIn.Close()
	// localhost and 127.0.0.1 are different host names, as a gateway's
	// sign-in host is to the server's.
	signInURL := strings.Replace(signIn.URL, "127.0.0.1", "localhost", 1)
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, signInURL+"/cdn-cgi/access/login", http.StatusFound)
	}))
	defer gate.Close()
	client := NewClient(gate.URL, "tok")
	client.Headers, client.headersErr = nil, nil
	err := client.Get(context.Background(), "/api/vault", &map[string]any{})
	if StatusOf(err) != http.StatusForbidden || !strings.Contains(err.Error(), "sign-in page at localhost") {
		t.Fatalf("got %v", err)
	}
}

func TestGatewayRedirectMessageLetsTheSameHostThrough(t *testing.T) {
	final, _ := url.Parse("https://notes.example.com/api/vault")
	if msg := gatewayRedirectMessage("http://notes.example.com", final); msg != "" {
		t.Fatalf("http to https on the same host is not a gateway: %q", msg)
	}
}

func TestAPlainText403KeepsTheServersOwnWords(t *testing.T) {
	msg := requestErrorMessage("https://notes.example.com", "/api/fs/browse", 403, "Forbidden", "text/plain; charset=utf-8", "path is outside the allowed browse roots")
	if !strings.Contains(msg, "path is outside the allowed browse roots") {
		t.Fatal(msg)
	}
}
