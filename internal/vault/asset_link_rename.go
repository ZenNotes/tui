package vault

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Rewriting references to an asset when the asset file is renamed or moved
// (#785). Port of packages/shared-domain/src/asset-link-rename.ts and of the
// resolver in asset-path-resolution.ts: a reference resolves relative to its
// note, then to the vault root, then by unique basename (the renderer's three
// readings); the ones that resolve to the asset are re-targeted in the
// author's own style (relative stays relative, rooted stays rooted, a bare
// name stays bare while unique) and keep everything else: `|alias` / `|300`
// hints, `#page=3` fragments, percent-encoding, angle brackets, link titles.

var (
	assetWikilinkRe = regexp.MustCompile(`(!?)\[\[([^\]\n]+?)\]\]`)
	// Matched from the `](` so the href of an image nested inside a link
	// (`[![alt](a.png)](a.png)`) is found as readily as the outer link's own.
	assetMdDestRe = regexp.MustCompile(`\]\(\s*(<[^>\n]*>|[^)\n]+?)((?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?)\s*\)`)
)

func assetStripQueryAndHash(href string) string {
	if i := strings.IndexByte(href, '#'); i >= 0 {
		href = href[:i]
	}
	if i := strings.IndexByte(href, '?'); i >= 0 {
		href = href[:i]
	}
	return href
}

func assetDecodeHref(value string) string {
	cleaned := assetStripQueryAndHash(value)
	if decoded, err := url.PathUnescape(cleaned); err == nil {
		return decoded
	}
	return cleaned
}

func posixJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case strings.HasSuffix(a, "/"):
		return a + b
	}
	return a + "/" + b
}

func posixNormalize(input string) string {
	out := []string{}
	for _, part := range strings.Split(input, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(out) == 0 {
				return ".."
			}
			out = out[:len(out)-1]
		default:
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}

func lastPathSegment(p string) string {
	parts := strings.Split(p, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return ""
}

// Which of the three readings resolved a reference. Lets a rewrite keep the
// author's style: a note-relative href stays relative, a vault-root path stays
// rooted, a bare file name stays bare.
const (
	assetReadingNoteRelative = "note-relative"
	assetReadingVaultRoot    = "vault-root"
	assetReadingBasename     = "basename"
)

type assetReferenceResolution struct {
	path     string
	reading  string
	absolute bool // written with a leading `/`
}

// resolveAssetPathAmong mirrors the renderer's resolveAssetPathAmong: the
// vault-relative path of the existing asset an href or wikilink target points
// at, or false when it points nowhere (or at more than one file by basename).
func resolveAssetPathAmong(assets []AssetMeta, notePath, href string) (string, bool) {
	r, ok := resolveAssetReference(assets, notePath, href)
	if !ok {
		return "", false
	}
	return r.path, true
}

// resolveAssetReference mirrors the renderer's resolveAssetReference: the
// resolved path plus the reading that found it.
func resolveAssetReference(assets []AssetMeta, notePath, href string) (assetReferenceResolution, bool) {
	none := assetReferenceResolution{}
	trimmed := strings.TrimSpace(href)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return none, false
	}
	if schemeRe.MatchString(trimmed) {
		return none, false
	}
	noteDir := ""
	if i := strings.LastIndexByte(notePath, '/'); i >= 0 {
		noteDir = notePath[:i]
	}
	decoded := assetDecodeHref(trimmed)
	isAbs := strings.HasPrefix(decoded, "/")
	var target string
	switch {
	case isAbs:
		target = strings.TrimLeft(decoded, "/")
	case noteDir != "":
		target = posixJoin(noteDir, decoded)
	default:
		target = decoded
	}
	target = posixNormalize(target)
	if strings.HasPrefix(target, "../") || target == ".." {
		return none, false
	}
	has := func(p string) bool {
		for _, a := range assets {
			if a.Path == p {
				return true
			}
		}
		return false
	}
	if has(target) {
		reading := assetReadingNoteRelative
		if isAbs || noteDir == "" {
			reading = assetReadingVaultRoot
		}
		return assetReferenceResolution{path: target, reading: reading, absolute: isAbs}, true
	}
	if !isAbs && noteDir != "" {
		rootTarget := posixNormalize(decoded)
		if rootTarget != "" && rootTarget != target && !strings.HasPrefix(rootTarget, "../") &&
			rootTarget != ".." && has(rootTarget) {
			return assetReferenceResolution{path: rootTarget, reading: assetReadingVaultRoot}, true
		}
	}
	base := strings.ToLower(lastPathSegment(target))
	if base == "" {
		return none, false
	}
	match, count := "", 0
	for _, a := range assets {
		if strings.ToLower(lastPathSegment(a.Path)) == base {
			match = a.Path
			count++
		}
	}
	if count == 1 {
		return assetReferenceResolution{path: match, reading: assetReadingBasename}, true
	}
	return none, false
}

// assetRelativeTo is the POSIX path from directory fromDir ("" = vault root) to toPath.
func assetRelativeTo(fromDir, toPath string) string {
	from := splitNonEmpty(fromDir)
	to := splitNonEmpty(toPath)
	shared := 0
	for shared < len(from) && shared < len(to) && from[shared] == to[shared] {
		shared++
	}
	parts := make([]string, 0, len(from)-shared+len(to)-shared)
	for i := shared; i < len(from); i++ {
		parts = append(parts, "..")
	}
	parts = append(parts, to[shared:]...)
	return strings.Join(parts, "/")
}

func splitNonEmpty(p string) []string {
	out := []string{}
	for _, part := range strings.Split(p, "/") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// assetEncodeLike percent-encodes next per segment when the author wrote
// original encoded.
func assetEncodeLike(original, next string) string {
	decoded, err := url.PathUnescape(original)
	if err != nil || decoded == original {
		return next
	}
	segments := strings.Split(next, "/")
	for i, seg := range segments {
		if seg != ".." && seg != "." {
			segments[i] = url.PathEscape(seg)
		}
	}
	return strings.Join(segments, "/")
}

// assetRetarget rewrites one reference (a wikilink target or markdown href)
// that resolved via resolution so it points at newPath, in the author's style,
// keeping angle brackets, `#`/`?` suffix and percent-encoding. Wikilinks and
// hrefs differ in one place: a wikilink is written as a bare name or a
// vault-root path (Obsidian resolves them from the root), never with `..`, so
// a wikilink that happened to resolve next to its note stays bare while unique
// and otherwise gets the full path; a markdown href is a real relative path,
// so it follows the file with `..` where needed.
func assetRetarget(reference string, resolution assetReferenceResolution, noteDir, newPath string, bareStillUnique, wikilink bool) string {
	angled := strings.HasPrefix(reference, "<") && strings.HasSuffix(reference, ">")
	inner := reference
	if angled {
		inner = reference[1 : len(reference)-1]
	}
	suffix := ""
	if i := strings.IndexAny(inner, "#?"); i >= 0 {
		suffix = inner[i:]
		inner = inner[:i]
	}
	bare := !strings.Contains(inner, "/")
	var next string
	switch {
	case resolution.reading == assetReadingBasename || (wikilink && bare):
		if bareStillUnique {
			next = lastPathSegment(newPath)
		} else {
			next = newPath
		}
	case resolution.reading == assetReadingNoteRelative && !wikilink:
		next = assetRelativeTo(noteDir, newPath)
	default:
		next = newPath
		if resolution.absolute {
			next = "/" + newPath
		}
	}
	out := assetEncodeLike(inner, next) + suffix
	// A Markdown destination with a space or a parenthesis only stays a link in
	// angle brackets, and a rename or a numbered name can bring either in. This
	// is the one place this copy goes beyond znserver's and the desktop's.
	if angled || (!wikilink && strings.ContainsAny(out, " \t()")) {
		return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(out) + ">"
	}
	return out
}

// rewriteAssetReferencesInBody rewrites every reference in body that resolves
// to the asset at oldPath so it points at newPath (vault-relative; a rename
// changes the name, a move the directory). assets must be the pre-change
// listing so references resolve to the asset under the path they currently
// use; notePath is the note holding body, since a markdown href resolves
// relative to it. Code is skipped.
func rewriteAssetReferencesInBody(body string, assets []AssetMeta, notePath, oldPath, newPath string) (string, int) {
	if newPath == "" || oldPath == newPath {
		return body, 0
	}
	if !strings.Contains(body, "[[") && !strings.Contains(body, "](") {
		return body, 0
	}
	noteDir := ""
	if i := strings.LastIndexByte(notePath, '/'); i >= 0 {
		noteDir = notePath[:i]
	}
	newBase := strings.ToLower(lastPathSegment(newPath))
	sameBase := 0
	for _, a := range assets {
		p := a.Path
		if p == oldPath {
			p = newPath
		}
		if strings.ToLower(lastPathSegment(p)) == newBase {
			sameBase++
		}
	}
	bareStillUnique := sameBase == 1
	resolveOld := func(reference string) (assetReferenceResolution, bool) {
		t := strings.TrimSpace(reference)
		if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
			t = t[1 : len(t)-1]
		}
		r, ok := resolveAssetReference(assets, notePath, t)
		if !ok || r.path != oldPath {
			return assetReferenceResolution{}, false
		}
		return r, true
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	mask := wikiCodeMask(body)
	for _, m := range assetWikilinkRe.FindAllStringSubmatchIndex(body, -1) {
		if mask[m[0]] {
			continue
		}
		embed := body[m[2]:m[3]]
		content := body[m[4]:m[5]]
		target, rest := content, ""
		if p := strings.IndexByte(content, '|'); p >= 0 {
			target, rest = content[:p], content[p:]
		}
		r, ok := resolveOld(target)
		if !ok {
			continue
		}
		next := embed + "[[" + assetRetarget(target, r, noteDir, newPath, bareStillUnique, true) + rest + "]]"
		if next == body[m[0]:m[1]] {
			continue // a bare name that still resolves reads exactly as before
		}
		edits = append(edits, edit{m[0], m[1], next})
	}
	for _, m := range assetMdDestRe.FindAllStringSubmatchIndex(body, -1) {
		if mask[m[0]] {
			continue
		}
		href := body[m[2]:m[3]]
		title := ""
		if m[4] >= 0 {
			title = body[m[4]:m[5]]
		}
		r, ok := resolveOld(href)
		if !ok {
			continue
		}
		next := "](" + assetRetarget(href, r, noteDir, newPath, bareStillUnique, false) + title + ")"
		if next == body[m[0]:m[1]] {
			continue
		}
		edits = append(edits, edit{m[0], m[1], next})
	}
	if len(edits) == 0 {
		return body, 0
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var sb strings.Builder
	last, changed := 0, 0
	for _, e := range edits {
		if e.start < last {
			continue // overlapped an earlier edit; keep the first
		}
		sb.WriteString(body[last:e.start])
		sb.WriteString(e.text)
		last = e.end
		changed++
	}
	sb.WriteString(body[last:])
	return sb.String(), changed
}

// rewriteAssetReferences rewrites every note that referenced the renamed or
// moved asset, skipping the trash and whatever rewrite turns down (nil: every
// note). This vault's listing carries no attachment flag, so it reads each note
// the filter allows; only the ones whose text changes are written.
func (v *Vault) rewriteAssetReferences(notesBefore []NoteMeta, assetsBefore []AssetMeta, oldRel, newRel string, rewrite func(path string) bool) {
	for _, n := range notesBefore {
		if n.Folder == FolderTrash || (rewrite != nil && !rewrite(n.Path)) {
			continue
		}
		content, err := v.ReadNote(n.Path)
		if err != nil {
			continue
		}
		body, changed := rewriteAssetReferencesInBody(content.Body, assetsBefore, n.Path, oldRel, newRel)
		if changed > 0 {
			_, _ = v.WriteNote(n.Path, body)
		}
	}
}
