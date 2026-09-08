package vault

import "strings"

// DeepLinkScheme is the URL scheme the desktop app registers.
const DeepLinkScheme = "zennotes"

// BuildOpenNoteDeepLink is the shareable URL that opens a vault-relative
// note in the app. Segments are percent-encoded the way encodeURIComponent
// does it, with parentheses encoded as well because these URLs live inside
// markdown `[title](url)` links, where a bare `)` ends the link.
func BuildOpenNoteDeepLink(relPath string) string {
	parts := strings.Split(strings.ReplaceAll(relPath, "\\", "/"), "/")
	for i, seg := range parts {
		parts[i] = encodeURIComponent(seg)
	}
	return DeepLinkScheme + "://open?path=" + strings.Join(parts, "/")
}

const upperHex = "0123456789ABCDEF"

// encodeURIComponent matches the JavaScript function of the same name, with
// `(` and `)` encoded on top.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '!' || c == '~' || c == '*' || c == '\'':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xf])
		}
	}
	return b.String()
}
