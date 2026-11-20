package remote

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Extra HTTP headers sent with every request to a server (ZenNotes/zennotes
// #1091). They exist for an access gateway in front of the server, such as a
// Cloudflare Access service token (CF-Access-Client-Id and
// CF-Access-Client-Secret), which turns away any request without them before
// the server sees it.
//
// The format and every rule here mirror packages/shared-domain/src/
// remote-headers.ts in the desktop app, which keeps the same lines in the OS
// keychain and hands its bundled zn and MCP server the same variable: one
// `Name: value` per line, the way curl and every proxy's documentation write
// a header. zn reads them only from the environment. credentials.toml holds
// tokens and nothing else, and the values are as secret as a token.

// HeadersEnv is the variable zn, zn tui and zn mcp read the headers from.
const HeadersEnv = "ZENNOTES_REMOTE_HEADERS"

// MaxHeaders is more than any gateway needs; a pasted file is likelier.
const MaxHeaders = 20

// Header is one extra request header.
type Header struct {
	Name  string
	Value string
}

// RFC 9110 field-name token characters.
var headerNameRe = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// Names the connection itself owns: Go's transport and the WebSocket
// handshake set these, and a second copy breaks the request instead of
// reaching the gateway. Authorization carries the server's own token.
var reservedHeaderNames = map[string]bool{
	"authorization":     true,
	"connection":        true,
	"content-length":    true,
	"content-type":      true,
	"host":              true,
	"keep-alive":        true,
	"proxy-connection":  true,
	"te":                true,
	"trailer":           true,
	"transfer-encoding": true,
	"upgrade":           true,
}

func reservedReason(name string) string {
	lower := strings.ToLower(name)
	if lower == "authorization" {
		return "use the auth token for Authorization"
	}
	if reservedHeaderNames[lower] || strings.HasPrefix(lower, "sec-websocket-") {
		return name + " is set by the connection itself"
	}
	return ""
}

// hasControl reports a control character other than tab: a CR or LF would
// split the request.
func hasControl(value string) bool {
	for _, r := range value {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return true
		}
	}
	return false
}

// ParseHeaders reads `Name: value` lines. Blank lines and lines starting with
// `#` are skipped. The first problem wins and names its line; nothing is
// half-accepted.
func ParseHeaders(text string) ([]Header, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	headers := []Header{}
	seen := map[string]bool{}
	for index, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		where := fmt.Sprintf("Line %d", index+1)
		colon := strings.Index(line, ":")
		if colon <= 0 {
			return nil, fmt.Errorf("%s: write each header as Name: value.", where)
		}
		name := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		if !headerNameRe.MatchString(name) {
			return nil, fmt.Errorf("%s: %q is not a valid header name.", where, name)
		}
		if reason := reservedReason(name); reason != "" {
			return nil, fmt.Errorf("%s: %s.", where, reason)
		}
		if value == "" {
			return nil, fmt.Errorf("%s: %s has no value.", where, name)
		}
		if hasControl(value) {
			return nil, fmt.Errorf("%s: %s has a control character in its value.", where, name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("%s: %s is listed twice.", where, name)
		}
		seen[key] = true
		headers = append(headers, Header{Name: name, Value: value})
		if len(headers) > MaxHeaders {
			return nil, fmt.Errorf("At most %d headers are allowed.", MaxHeaders)
		}
	}
	return headers, nil
}

// HeadersFromEnv reads HeadersEnv through getenv. Unset or blank means none.
// A JSON env block (an MCP client's config) can only carry a line break as
// the two characters `\n`, so those count as one.
func HeadersFromEnv(getenv func(string) string) ([]Header, error) {
	raw := getenv(HeadersEnv)
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	headers, err := ParseHeaders(strings.ReplaceAll(raw, `\n`, "\n"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", HeadersEnv, err)
	}
	return headers, nil
}

var (
	envHeadersOnce sync.Once
	envHeaders     []Header
	envHeadersErr  error
)

// EnvHeaders is HeadersFromEnv for this process, read once. A value that does
// not parse is an error every request returns before it is sent: running on
// without the headers would only trade this message for the gateway's 403.
func EnvHeaders() ([]Header, error) {
	envHeadersOnce.Do(func() {
		envHeaders, envHeadersErr = HeadersFromEnv(os.Getenv)
	})
	return envHeaders, envHeadersErr
}

// ApplyHeaders adds the headers to h. They never replace one already set.
func ApplyHeaders(h http.Header, headers []Header) {
	for _, header := range headers {
		if h.Get(header.Name) == "" {
			h.Set(header.Name, header.Value)
		}
	}
}

// GatewayRefusedMessage names a gateway in front of the server that turned a
// request away. The ZenNotes server answers in plain text, so an HTML 403
// came from something in front of it.
func GatewayRefusedMessage(baseURL string) string {
	return fmt.Sprintf("Something in front of the ZenNotes server at %s refused the request. If it sits behind an access gateway such as Cloudflare Access, set its headers in %s.", baseURL, HeadersEnv)
}

// gatewayRedirectMessage is non-empty when a request ended on another host:
// a gateway such as Cloudflare Access sends a request without its headers to
// a sign-in page, and the HTML there would otherwise fail as JSON. A redirect
// that stays on the server's host (http to https) is fine.
func gatewayRedirectMessage(baseURL string, final *url.URL) string {
	if final == nil {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	if strings.EqualFold(base.Hostname(), final.Hostname()) {
		return ""
	}
	return fmt.Sprintf("The ZenNotes server at %s sent the request to a sign-in page at %s. If the server sits behind an access gateway such as Cloudflare Access, set its headers in %s.", baseURL, final.Hostname(), HeadersEnv)
}
