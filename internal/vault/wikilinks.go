package vault

import (
	"regexp"
	"strings"
)

// Wikilink resolution mirrors the app's wikilinks.ts: a target resolves by
// note title (case-insensitive) unless it looks like a path, in which case it
// resolves by explicit or suffix path match. Trashed notes never resolve.

var wikilinkRewriteRe = regexp.MustCompile(`(!?)\[\[([^\]\n]+)\]\]`)

var wikiTopFolders = []string{"inbox", "quick", "archive", "trash"}

func wikiNormalizeSlashes(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	return value
}

func wikiStripMd(value string) string {
	if strings.HasSuffix(strings.ToLower(value), ".md") {
		return value[:len(value)-3]
	}
	return value
}

func wikiNormCompare(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func wikiIsPathLike(target string) bool {
	t := strings.TrimSpace(target)
	return strings.HasPrefix(t, "/") || strings.Contains(t, "/") || strings.HasSuffix(strings.ToLower(t), ".md")
}

func wikiResolveExplicitPath(notes []NoteMeta, target string) (NoteMeta, bool) {
	normalized := wikiNormalizeSlashes(strings.TrimSpace(target))
	if normalized == "" {
		return NoteMeta{}, false
	}
	trimmed := strings.Trim(wikiStripMd(normalized), "/")
	if trimmed == "" {
		return NoteMeta{}, false
	}
	relPath := ""
	if strings.HasPrefix(normalized, "/") {
		relPath = "inbox/" + trimmed + ".md"
	} else {
		lower := strings.ToLower(trimmed)
		for _, f := range wikiTopFolders {
			if strings.HasPrefix(lower, f+"/") {
				relPath = trimmed + ".md"
				break
			}
		}
	}
	if relPath == "" {
		return NoteMeta{}, false
	}
	needle := wikiNormCompare(relPath)
	for _, n := range notes {
		if wikiNormCompare(n.Path) == needle {
			return n, true
		}
	}
	return NoteMeta{}, false
}

func wikiResolvePathSuffix(notes []NoteMeta, target string) (NoteMeta, bool) {
	trimmed := strings.Trim(wikiStripMd(wikiNormalizeSlashes(strings.TrimSpace(target))), "/")
	if trimmed == "" {
		return NoteMeta{}, false
	}
	suffix := wikiNormCompare("/" + trimmed + ".md")
	exact := wikiNormCompare(trimmed + ".md")
	var match NoteMeta
	count := 0
	for _, n := range notes {
		p := wikiNormCompare(n.Path)
		if p == exact || strings.HasSuffix(p, suffix) {
			match = n
			count++
		}
	}
	if count == 1 {
		return match, true
	}
	return NoteMeta{}, false
}

// ResolveWikilink finds the note a `[[target]]` points at, or false.
func ResolveWikilink(notes []NoteMeta, target string) (NoteMeta, bool) {
	visible := make([]NoteMeta, 0, len(notes))
	for _, n := range notes {
		if n.Folder != FolderTrash {
			visible = append(visible, n)
		}
	}
	target, _, _ = SplitWikilinkContent(target)
	if wikiIsPathLike(target) {
		if n, ok := wikiResolveExplicitPath(visible, target); ok {
			return n, true
		}
		return wikiResolvePathSuffix(visible, target)
	}
	needle := wikiNormCompare(wikiStripMd(target))
	for _, n := range visible {
		if wikiNormCompare(n.Title) == needle {
			return n, true
		}
	}
	return NoteMeta{}, false
}

// SplitWikilinkContent separates `Target#anchor|alias` into its parts; the
// anchor keeps its leading `#` or `^` and the alias its leading `|`.
func SplitWikilinkContent(content string) (target, anchor, alias string) {
	rest := content
	if pipe := strings.IndexByte(rest, '|'); pipe >= 0 {
		alias = rest[pipe:]
		rest = rest[:pipe]
	}
	if idx := strings.IndexAny(rest, "#^"); idx >= 0 {
		anchor = rest[idx:]
		rest = rest[:idx]
	}
	return rest, anchor, alias
}

func wikiSwapBasename(target, newTitle string) string {
	dir, base := "", target
	if slash := strings.LastIndexByte(target, '/'); slash >= 0 {
		dir, base = target[:slash+1], target[slash+1:]
	}
	md := ""
	if strings.HasSuffix(strings.ToLower(base), ".md") {
		md = base[len(base)-3:]
	}
	return dir + newTitle + md
}

// wikiCodeMask marks byte positions inside fenced or inline code so links
// there are left untouched. Fences follow the desktop's stripCodeContent: only
// the same character, at least as long as the opener with nothing after it,
// closes one, and a backtick fence's info string has no backtick. An inline span
// runs between two backtick runs of equal length on one line.
func wikiCodeMask(body string) []bool {
	mask := make([]bool, len(body))
	fenceChar, fenceLen := byte(0), 0
	for start := 0; start < len(body); {
		end := len(body)
		if i := strings.IndexByte(body[start:], '\n'); i >= 0 {
			end = start + i
		}
		line := body[start:end]
		char, n, rest := codeFenceMarker(line)
		switch {
		case fenceChar != 0:
			markAll(mask[start:end])
			if char == fenceChar && n >= fenceLen && strings.TrimSpace(rest) == "" {
				fenceChar = 0
			}
		case n > 0 && (char == '~' || !strings.Contains(rest, "`")):
			fenceChar, fenceLen = char, n
			markAll(mask[start:end])
		default:
			markInlineCode(mask[start:end], line)
		}
		start = end + 1
	}
	return mask
}

// codeFenceMarker reads a line's fence marker of three or more backticks or
// tildes, and what follows it. n is 0 when the line has none.
func codeFenceMarker(line string) (char byte, n int, rest string) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, ""
	}
	run := repeatedPrefix(trimmed)
	if run < 3 {
		return 0, 0, ""
	}
	return trimmed[0], run, trimmed[run:]
}

// repeatedPrefix is how many times s's first byte repeats at its start.
func repeatedPrefix(s string) int {
	n := 1
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return n
}

func markAll(mask []bool) {
	for i := range mask {
		mask[i] = true
	}
}

// markInlineCode marks the inline code spans of one line. A backtick run with no
// partner of its length is literal text.
func markInlineCode(mask []bool, line string) {
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		n := repeatedPrefix(line[i:])
		closed := -1
		for j := i + n; j < len(line); {
			if line[j] != '`' {
				j++
				continue
			}
			m := repeatedPrefix(line[j:])
			if m == n {
				closed = j + m
				break
			}
			j += m
		}
		if closed < 0 {
			i += n
			continue
		}
		markAll(mask[i:closed])
		i = closed
	}
}

// RewriteWikilinksForRename rewrites every `[[target]]` / `![[target]]` in
// body whose target resolves to the note at oldPath, pointing it at newTitle.
// Aliases, anchors and embeds are preserved; code is skipped. notes must
// reflect the vault BEFORE the rename.
func RewriteWikilinksForRename(body string, notes []NoteMeta, oldPath, newTitle string) (string, int) {
	matches := wikilinkRewriteRe.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return body, 0
	}
	mask := wikiCodeMask(body)
	var sb strings.Builder
	last := 0
	changed := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if mask[start] {
			continue
		}
		embed := body[m[2]:m[3]]
		content := body[m[4]:m[5]]
		target, anchor, alias := SplitWikilinkContent(content)
		if n, ok := ResolveWikilink(notes, target); !ok || n.Path != oldPath {
			continue
		}
		newTarget := wikiSwapBasename(target, newTitle)
		if newTarget == target {
			continue
		}
		sb.WriteString(body[last:start])
		sb.WriteString(embed + "[[" + newTarget + anchor + alias + "]]")
		last = end
		changed++
	}
	if changed == 0 {
		return body, 0
	}
	sb.WriteString(body[last:])
	return sb.String(), changed
}

// BacklinksIn lists which of notes wikilink to the note at relPath, matching
// on title and never counting the note itself. Pure, so the remote backend
// gets the same answer from a listing it fetched.
func BacklinksIn(notes []NoteMeta, relPath string) []NoteMeta {
	fileName := relPath
	if i := strings.LastIndexByte(relPath, '/'); i >= 0 {
		fileName = relPath[i+1:]
	}
	target := fileName
	if dot := strings.LastIndexByte(fileName, '.'); dot > 0 {
		target = fileName[:dot]
	}
	target = strings.ToLower(target)
	out := []NoteMeta{}
	for _, meta := range notes {
		if meta.Path == relPath {
			continue
		}
		for _, w := range meta.Wikilinks {
			if strings.ToLower(w) == target {
				out = append(out, meta)
				break
			}
		}
	}
	return out
}
