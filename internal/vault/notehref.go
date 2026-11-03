package vault

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var hrefSchemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z\d+.-]*:`)

// ResolveNoteHref resolves a Markdown link's href to a note, mirroring the
// app's resolveInternalNoteHref in internal-links.ts. Unlike a wikilink, an
// href is a path relative to the note holding it (fromPath); a leading slash
// starts at the vault root. The `.md` may be left off, the path may be
// percent-encoded, and a fragment is ignored. Trashed notes never resolve.
func ResolveNoteHref(notes []NoteMeta, fromPath, href string) (NoteMeta, bool) {
	raw := strings.TrimSpace(href)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "//") || hrefSchemeRe.MatchString(raw) {
		return NoteMeta{}, false
	}
	rawPath, _, _ := strings.Cut(raw, "#")
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		decoded = rawPath
	}
	target := strings.TrimLeft(decoded, "/")
	if !strings.HasPrefix(decoded, "/") {
		target = path.Join(path.Dir(fromPath), decoded)
	}
	target = path.Clean(target)
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return NoteMeta{}, false
	}
	return matchNoteHref(notes, target)
}

func matchNoteHref(notes []NoteMeta, target string) (NoteMeta, bool) {
	candidates := []string{target + ".md", target}
	if strings.HasSuffix(strings.ToLower(target), ".md") {
		candidates = []string{target}
	}
	for _, candidate := range candidates {
		for _, n := range notes {
			if n.Folder != FolderTrash && strings.EqualFold(n.Path, candidate) {
				return n, true
			}
		}
	}
	// A single note with that file name: the link was written before it moved.
	var match NoteMeta
	found := 0
	for _, n := range notes {
		if n.Folder != FolderTrash && strings.EqualFold(path.Base(n.Path), path.Base(candidates[0])) {
			match = n
			found++
		}
	}
	return match, found == 1
}
