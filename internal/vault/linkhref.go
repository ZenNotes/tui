package vault

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	explicitExternalRe = regexp.MustCompile(`(?i)^(https?:|mailto:|tel:)`)
	bareDomainRe       = regexp.MustCompile(`(?i)^[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	localFileExtRe     = regexp.MustCompile(`(?i)\.(md|markdown|txt|png|apng|avif|gif|jpe?g|svg|webp|pdf|mp3|m4a|aac|flac|ogg|wav|mp4|m4v|mov|ogv|webm|canvas|excalidraw)$`)
	anyFileExtRe       = regexp.MustCompile(`(?i)\.[a-z0-9]{1,12}$`)
	mdDestTitleRe      = regexp.MustCompile(`\s+(?:"[^"]*"|'[^']*')$`)
)

// UnwrapMarkdownDest reduces what sits between a Markdown link's parentheses to
// its target, mirroring unwrapMdUrl in internal-links.ts: a trailing title is
// not part of the path, and angle brackets only wrap a path holding spaces.
func UnwrapMarkdownDest(dest string) string {
	trimmed := mdDestTitleRe.ReplaceAllString(strings.TrimSpace(dest), "")
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") && len(trimmed) >= 2 {
		return trimmed[1 : len(trimmed)-1]
	}
	return trimmed
}

// ExternalLinkURL is the address an href opens in the browser, mirroring
// externalLinkUrl in internal-links.ts: an explicit http(s), mailto or tel link,
// or a bare domain typed without a scheme (`google.com`, `example.com/path`),
// which is not a dead relative link. Anything naming a local file stays local.
func ExternalLinkURL(href string) (string, bool) {
	h := strings.TrimSpace(href)
	if h == "" {
		return "", false
	}
	if explicitExternalRe.MatchString(h) {
		return h, true
	}
	if hrefSchemeRe.MatchString(h) || strings.HasPrefix(h, "#") || strings.HasPrefix(h, "/") || strings.HasPrefix(h, ".") {
		return "", false
	}
	host := h
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		host = h[:i]
	}
	if !bareDomainRe.MatchString(host) || localFileExtRe.MatchString(host) {
		return "", false
	}
	return "https://" + h, true
}

// HrefFragment is the percent-decoded text after an href's `#`, or "".
func HrefFragment(href string) string {
	_, fragment, _ := strings.Cut(strings.TrimSpace(href), "#")
	decoded, err := url.PathUnescape(fragment)
	if err != nil {
		decoded = fragment
	}
	return strings.TrimSpace(decoded)
}

// HrefNamesFile reports an href that names a file rather than a note, so a dead
// one never turns into an offer of `photo.png.md`: a known file type, a path
// from the home directory, or an absolute path with a non-Markdown extension
// (externalFileLink in external-file-link.ts).
func HrefNamesFile(href string) bool {
	h, _, _ := strings.Cut(strings.TrimSpace(href), "#")
	if h == "~" || strings.HasPrefix(h, "~/") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(h), ".md") {
		return false
	}
	if strings.HasPrefix(h, "/") && anyFileExtRe.MatchString(h) {
		return true
	}
	return localFileExtRe.MatchString(h)
}
