// Package mcpserver serves zn's MCP tools over HTTP, for a ZenNotes server to
// mount in its own process. It is the only public package in this module, and
// exists so that the tools and scope rules behind `zn mcp` are one implementation
// wherever they run. See docs/ideas/mcp-http-transport.md.
//
// A caller presents a token, the token picks a scope, and nothing the caller sends
// can change it. Each request stands alone (stateless), so there is no session
// another credential could borrow, and a restart loses nothing.
package mcpserver

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/mcp"
	"github.com/ZenNotes/tui/internal/remote"
)

const (
	defaultReadTimeout  = 60 * time.Second
	defaultWriteTimeout = mcp.HostedCallLimit + 10*time.Second
	// A refusal comes before the body is read. A client still sending it would get a
	// connection reset rather than the answer, so this much is read and dropped first.
	refusalDrainBytes       = 32 << 20
	refusalDrainTime        = 2 * time.Second
	defaultMaxResponseBytes = 64 << 20
	defaultMaxInFlight      = 32
	defaultMaxToolCalls     = 4
	// defaultSlotWait is how long a request waits for a slot before it gets a 503.
	defaultSlotWait = 2 * time.Second
	// maxJSONDepth is far deeper than any tool's arguments. The SDK's decoder takes time
	// quadratic in nesting, which a request of a few hundred KB can turn into minutes.
	maxJSONDepth = 64
	// A credential that cannot write sends only paths, queries and comments. Its limits
	// are far smaller, so a read token cannot tie up what uploads need.
	smallRequestBytes = 1 << 20
	smallReadTimeout  = 15 * time.Second
	// minBodyBudget is how many bytes of request bodies are held at once, shared out
	// between credentials. A credential always has room for its largest allowed request.
	minBodyBudget = 64 << 20
	// rejectionLogEvery spaces the log line about rejected requests, so that nobody
	// without a token can fill the log.
	rejectionLogEvery = time.Minute
)

// Credential is one token and the scope it grants. The token itself never reaches
// this package: a credentials file holds only its hash, so everything does.
type Credential struct {
	// Name identifies the credential in the server's log. It is not a secret.
	Name        string
	TokenSHA256 [sha256.Size]byte
	// Access is read, comment or write. It is required: full access has to be
	// written down.
	Access string
	// Allow and Deny work as --allow-path and --deny-path do on `zn mcp`.
	Allow, Deny []string
}

// HashToken is the form a Credential holds a token in.
func HashToken(token string) [sha256.Size]byte { return sha256.Sum256([]byte(token)) }

// Options configure a handler.
type Options struct {
	// Transport carries the tools' API calls to the server, without the network,
	// and adds whatever credential the server's API wants. Required.
	Transport http.RoundTripper
	// BaseURL is how the transport expects those calls to be addressed, including
	// any base path. Clients never see it.
	BaseURL string
	// Credentials are the tokens this handler accepts. At least one is required.
	Credentials []Credential
	// AllowedOrigins are the Origin header values to accept. A request carrying any
	// other Origin is refused, and one carrying none is unaffected. This adds no
	// CORS headers.
	AllowedOrigins []string
	// MaxNoteBytes is the server's note limit, from which the request limit follows.
	MaxNoteBytes int64
	// Version is reported to clients.
	Version string
	// Logf, when set, gets one line per tool call: credential name, tool, outcome,
	// duration. Never a token, an argument or a result. It also gets how many requests
	// were rejected, and what an operator must fix in the scope.
	Logf func(format string, args ...any)
	// ReadTimeout bounds reading a request body, and WriteTimeout everything after it.
	// They default to 60 seconds and a little over the longest tool call, and a credential that cannot write
	// gets at most 15 seconds to send its body. A server with websockets cannot set
	// either on its listener, so they apply here, per request.
	ReadTimeout, WriteTimeout time.Duration
	// MaxInFlight is how many requests are open at once before the rest get 503. It
	// defaults to 32, shared equally between credentials, so one cannot starve others. An
	// answer stays in memory until its client reads it, so a small server should set this
	// lower.
	MaxInFlight int
	// SlotWait is how long a request waits for one of those. It defaults to 2 seconds.
	SlotWait time.Duration
	// MaxToolCalls is how many tool calls run at once, across credentials, while the
	// rest wait. It defaults to 4. A call can hold a whole listing and a search's matches
	// in memory, and this is what bounds that, so a small server should set it lower.
	MaxToolCalls int
	// MaxResponseBytes is the largest answer to a tool call, as it goes on the wire. It
	// defaults to 64 MiB, and a call over it gets a tool error, since an answer waits in
	// memory until its client reads it.
	MaxResponseBytes int64
	// MaxBodyBytes is how many bytes of request bodies are held at once, shared equally
	// between credentials, before the rest get 503. It defaults to 64 MiB. A credential
	// always has room for its largest allowed request.
	MaxBodyBytes int64
}

type boundCredential struct {
	sum      [sha256.Size]byte
	server   *sdk.Server
	maxBytes int64
	read     time.Duration
	slots    chan struct{}

	// Only bytes that have arrived count, so announcing a large body holds nothing, and
	// each credential has its own share, so filling it stops only that credential.
	bodyMu     sync.Mutex
	bodyBytes  int64
	bodyBudget int64
}

type handler struct {
	credentials []*boundCredential
	origins     map[string]bool
	inner       http.Handler
	slots       chan struct{}
	slotWait    time.Duration
	write       time.Duration
	logf        func(format string, args ...any)

	rejected     atomic.Int64
	rejectedLast atomic.Int64
}

// NewHandler builds the handler. A bad credential is an error, never a wider scope.
func NewHandler(o Options) (http.Handler, error) {
	if o.Transport == nil {
		return nil, errors.New("mcpserver: Transport is required")
	}
	if len(o.Credentials) == 0 {
		return nil, errors.New("no MCP credential")
	}
	h := &handler{
		origins: map[string]bool{},
		write:   o.WriteTimeout,
		logf:    o.Logf,
	}
	readTimeout := o.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = defaultReadTimeout
	}
	if h.write <= 0 {
		h.write = defaultWriteTimeout
	}
	inFlight := o.MaxInFlight
	if inFlight <= 0 {
		inFlight = defaultMaxInFlight
	}
	h.slots = make(chan struct{}, inFlight)
	perCredential := max(1, inFlight/len(o.Credentials))
	h.slotWait = o.SlotWait
	if h.slotWait <= 0 {
		h.slotWait = defaultSlotWait
	}
	maxToolCalls := o.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = defaultMaxToolCalls
	}
	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = defaultMaxResponseBytes
	}
	toolSlots := make(chan struct{}, maxToolCalls)
	writeTurn := make(chan struct{}, 1)
	bodyBudget := o.MaxBodyBytes
	if bodyBudget <= 0 {
		bodyBudget = minBodyBudget
	}
	bodyShare := bodyBudget / int64(len(o.Credentials))
	for _, origin := range o.AllowedOrigins {
		h.origins[origin] = true
	}
	baseURL := remote.NormalizeBaseURL(o.BaseURL)
	largest := mcp.MaxHTTPRequestBytes(o.MaxNoteBytes)
	for _, c := range o.Credentials {
		if err := ValidateScope(c); err != nil {
			return nil, err
		}
		scope, err := mcp.NewScope(c.Access, false, c.Allow, c.Deny)
		if err != nil {
			return nil, err
		}
		for _, seen := range h.credentials {
			if subtle.ConstantTimeCompare(seen.sum[:], c.TokenSHA256[:]) == 1 {
				return nil, fmt.Errorf("MCP credential %q uses the same token as another credential", c.Name)
			}
		}
		bound := &boundCredential{
			sum:      c.TokenSHA256,
			server:   buildServer(o, c, scope, baseURL, toolSlots, writeTurn),
			maxBytes: largest,
			read:     readTimeout,
			slots:    make(chan struct{}, perCredential),
		}
		if scope.Access != mcp.AccessWrite {
			bound.maxBytes = smallRequestBytes
			bound.read = min(readTimeout, smallReadTimeout)
		}
		bound.bodyBudget = max(bodyShare, bound.maxBytes)
		h.credentials = append(h.credentials, bound)
	}
	h.inner = sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		if c := h.credentialFor(r); c != nil {
			return c.server
		}
		return nil
	}, &sdk.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: largest,
		// A server bound to loopback behind a proxy that passes its public Host
		// would otherwise get 403. The Origin check and the token do this job here.
		DisableLocalhostProtection: true,
	})
	return h, nil
}

// flagWords turns zn's flag names into what a server's operator wrote.
var flagWords = strings.NewReplacer("--allow-path", "allow path", "--deny-path", "deny path", "--access", "access")

// ValidateScope reports what is wrong with a credential's scope, so a server can
// refuse to start. Access has to be stated, and the rules are the ones `zn mcp`
// applies to its flags.
func ValidateScope(c Credential) error {
	if strings.TrimSpace(c.Access) == "" {
		return fmt.Errorf("MCP credential %q: access is required: read, comment or write", c.Name)
	}
	if _, err := mcp.NewScope(c.Access, false, c.Allow, c.Deny); err != nil {
		return fmt.Errorf("MCP credential %q: %s", c.Name, flagWords.Replace(err.Error()))
	}
	return nil
}

func buildServer(o Options, c Credential, scope mcp.Scope, baseURL string, toolSlots, writeTurn chan struct{}) *sdk.Server {
	var logCall func(string, string, time.Duration)
	if o.Logf != nil {
		name := c.Name
		logCall = func(tool, outcome string, took time.Duration) {
			o.Logf("mcp: credential=%s tool=%s outcome=%s took=%s", name, tool, outcome, took.Round(time.Millisecond))
		}
	}
	return mcp.BuildServer(mcp.Options{
		Version:          o.Version,
		Scope:            &scope,
		Hosted:           true,
		HostedBaseURL:    baseURL,
		LogCall:          logCall,
		Logf:             o.Logf,
		CommentAuthor:    c.Name,
		ToolSlots:        toolSlots,
		MaxResponseBytes: o.MaxResponseBytes,
		WriteTurn:        writeTurn,
		ResolveTarget: func() (backend.Target, error) {
			return backend.Target{Kind: backend.KindRemote, BaseURL: baseURL}, nil
		},
		OpenBackend: func(t backend.Target) (backend.Backend, error) {
			return backend.NewRemoteWithClient(remote.NewClientWithTransport(t.BaseURL, o.Transport), t), nil
		},
	})
}

// credentialFor is the one place a request becomes a scope: nil unless it carries a
// bearer token some credential holds. Every credential is compared, and the hash
// removes any signal about a token's length.
func (h *handler) credentialFor(r *http.Request) *boundCredential {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return nil
	}
	sum := HashToken(header[len(prefix):])
	var found *boundCredential
	for _, c := range h.credentials {
		if subtle.ConstantTimeCompare(sum[:], c.sum[:]) == 1 {
			found = c
		}
	}
	return found
}

// noteRejection counts a request with no valid token, and says so in the log at most
// once a minute. There is no lockout: behind a proxy or a tunnel every client shares an
// address, so one would let anyone block all of them.
func (h *handler) noteRejection() {
	h.rejected.Add(1)
	if h.logf == nil {
		return
	}
	now := time.Now().UnixNano()
	last := h.rejectedLast.Load()
	if now-last < int64(rejectionLogEvery) || !h.rejectedLast.CompareAndSwap(last, now) {
		return
	}
	h.logf("mcp: rejected %d requests with no valid token since the last report", h.rejected.Swap(0))
}

var errBodyBudget = errors.New("body budget used up")

// reserve holds room for n bytes that have arrived.
func (c *boundCredential) reserve(n int64) bool {
	c.bodyMu.Lock()
	defer c.bodyMu.Unlock()
	if c.bodyBytes+n > c.bodyBudget {
		return false
	}
	c.bodyBytes += n
	return true
}

func (c *boundCredential) release(n int64) {
	c.bodyMu.Lock()
	defer c.bodyMu.Unlock()
	c.bodyBytes -= n
}

// readBody reads a request's body, holding room for it as it arrives. The caller
// releases what was held, which is returned even on an error.
func (c *boundCredential) readBody(w http.ResponseWriter, r *http.Request) ([]byte, int64, error) {
	limited := http.MaxBytesReader(w, r.Body, c.maxBytes)
	var (
		body bytes.Buffer
		held int64
	)
	chunk := make([]byte, 64<<10)
	for {
		n, err := limited.Read(chunk)
		if n > 0 {
			if !c.reserve(int64(n)) {
				return nil, held, errBodyBudget
			}
			held += int64(n)
			body.Write(chunk[:n])
		}
		if errors.Is(err, io.EOF) {
			return body.Bytes(), held, nil
		}
		if err != nil {
			return nil, held, err
		}
	}
}

// tooDeep says valid JSON nests deeper than limit. It does not recurse, and skips strings.
func tooDeep(body []byte, limit int) bool {
	depth := 0
	inString := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case inString:
			if c == '\\' {
				i++
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{' || c == '[':
			depth++
			if depth > limit {
				return true
			}
		case c == '}' || c == ']':
			depth--
		}
	}
	return false
}

// takeSlot waits a moment for a slot, for as long as the client is still there.
func (h *handler) takeSlot(r *http.Request, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
	}
	wait := time.NewTimer(h.slotWait)
	defer wait.Stop()
	select {
	case slots <- struct{}{}:
		return true
	case <-wait.C:
	case <-r.Context().Done():
	}
	return false
}

// refuse answers without having read the whole body, so that a client still sending it
// gets this answer and not a connection reset.
func refuse(w http.ResponseWriter, r *http.Request, status int, text string) {
	if r.Body != nil {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(refusalDrainTime))
		_, _ = io.CopyN(io.Discard, r.Body, refusalDrainBytes)
	}
	w.Header().Set("Connection", "close")
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	http.Error(w, text, status)
}

func busy(w http.ResponseWriter, r *http.Request) {
	refuse(w, r, http.StatusServiceUnavailable, "too many MCP requests at once")
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The MCP spec requires checking Origin. A browser always sends it on a
	// cross-site or rebound POST, so this also covers DNS rebinding, which a
	// same-origin comparison would not.
	if origin := r.Header.Get("Origin"); origin != "" && !h.origins[origin] {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	credential := h.credentialFor(r)
	if credential == nil {
		h.noteRejection()
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.ContentLength > credential.maxBytes {
		refuse(w, r, http.StatusRequestEntityTooLarge, "request too large")
		return
	}
	if !h.takeSlot(r, credential.slots) {
		busy(w, r)
		return
	}
	defer func() { <-credential.slots }()
	if !h.takeSlot(r, h.slots) {
		busy(w, r)
		return
	}
	defer func() { <-h.slots }()

	control := http.NewResponseController(w)
	if r.Body != nil && r.Method == http.MethodPost {
		// net/http clears this when the body ends, so it cannot cut off a slow tool.
		_ = control.SetReadDeadline(time.Now().Add(credential.read))
		body, held, err := credential.readBody(w, r)
		defer credential.release(held)
		if err != nil {
			var tooLarge *http.MaxBytesError
			switch {
			case errors.As(err, &tooLarge):
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			case errors.Is(err, errBodyBudget):
				busy(w, r)
			default:
				http.Error(w, "could not read the request", http.StatusBadRequest)
			}
			return
		}
		// A batch would run every call in it under one slot. MCP dropped batches in
		// 2025-06-18, so nothing current sends one.
		if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
			http.Error(w, "JSON-RPC batches are not supported", http.StatusBadRequest)
			return
		}
		// The SDK's decoder recurses, and deep enough nesting ends the process in a way
		// nothing can recover from. This check does not recurse, and has a depth limit.
		if !json.Valid(body) {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if tooDeep(body, maxJSONDepth) {
			http.Error(w, "JSON nested too deeply", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	// From here, not from the start, so that a slow body does not use up the time for the
	// answer. net/http clears this once the response is finished, so it cannot reach
	// another request on a kept-alive connection.
	_ = control.SetWriteDeadline(time.Now().Add(h.write))
	h.inner.ServeHTTP(w, r)
}
