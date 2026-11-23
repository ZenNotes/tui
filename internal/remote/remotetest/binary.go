package remotetest

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// BinaryEnv names a ZenNotes server binary for the tests that run against a
// real server; CI's server-contract job sets it from the server's main.
const BinaryEnv = "ZENNOTES_SERVER_CONTRACT_BINARY"

// StartBinary runs the server binary on loopback over vaultRoot, with its
// configuration in a temporary directory and token as its auth token, and
// returns its base URL once it answers. The process stops with the test.
func StartBinary(t testing.TB, binary, vaultRoot, token string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	state := t.TempDir()
	log, err := os.Create(filepath.Join(state, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	command := exec.Command(binary)
	command.Dir = state
	command.Env = append(os.Environ(), "ZENNOTES_BIND="+address, "ZENNOTES_DEFAULT_VAULT_PATH="+vaultRoot, "ZENNOTES_CONFIG_PATH="+filepath.Join(state, "server.json"), "ZENNOTES_BROWSE_ROOTS="+vaultRoot, "ZENNOTES_AUTH_TOKEN="+token)
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { command.Process.Kill(); command.Wait() })
	base := "http://" + address
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/api/capabilities")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base
			}
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(log.Name())
			t.Fatalf("server did not become ready: %v\n%s", err, data)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Proxy forwards to a server and runs BeforeWrite before it forwards each note
// write, for a test to land another client's save in between a read and the
// write that follows it.
type Proxy struct {
	*httptest.Server
	mu          sync.Mutex
	BeforeWrite func()
	writes      []int
}

// NewProxy fronts base and stops with the test.
func NewProxy(t testing.TB, base string) *Proxy {
	t.Helper()
	target, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{}
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.ModifyResponse = func(resp *http.Response) error {
		if resp.Request.URL.Path == "/api/notes/write" {
			p.mu.Lock()
			p.writes = append(p.writes, resp.StatusCode)
			p.mu.Unlock()
		}
		return nil
	}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/notes/write" {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			p.mu.Lock()
			hook := p.BeforeWrite
			p.mu.Unlock()
			if hook != nil {
				hook()
			}
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(p.Close)
	return p
}

// SetBeforeWrite replaces the hook.
func (p *Proxy) SetBeforeWrite(hook func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BeforeWrite = hook
}

// WriteStatuses lists the status the server gave each note write, in order.
func (p *Proxy) WriteStatuses() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.writes...)
}
