package vault

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// A link target's anchor, mirroring the app's wikilinks.ts, heading-anchor.ts
// and block-anchors.ts: `Doc#Heading` points at a heading and `Doc^id` or
// `Doc#^id` at a block. Note names cannot hold `#` or `^`, so the first one
// starts the anchor.

var blockIDRe = regexp.MustCompile(`(?:^|\s)\^([A-Za-z0-9][A-Za-z0-9-]*)\s*$`)

// IsSameNoteAnchor reports a target whose note part is empty, as in `#Heading`
// or `^id`, which points inside the note holding the link.
func IsSameNoteAnchor(target string) bool {
	if i := strings.IndexAny(target, "#^"); i >= 0 && strings.TrimSpace(target[:i]) != "" {
		return false
	}
	return headingAnchor(target) != "" || blockAnchor(target) != ""
}

// AnchorLine finds the 1-based line a link target's anchor points at in body,
// or false when the target has no anchor or the note no longer carries it.
func AnchorLine(body, target string) (int, bool) {
	if heading := headingAnchor(target); heading != "" {
		return headingLine(Outline(body), heading)
	}
	if block := blockAnchor(target); block != "" {
		return blockLine(body, block)
	}
	return 0, false
}

// headingAnchor is the text after `#`, unless a `^` comes first or starts it:
// `Doc#^id` is Obsidian's block form, never a heading named "^id".
func headingAnchor(target string) string {
	hash := strings.IndexByte(target, '#')
	if hash < 0 {
		return ""
	}
	if caret := strings.IndexByte(target, '^'); caret >= 0 && caret < hash {
		return ""
	}
	anchor := strings.TrimSpace(target[hash+1:])
	if strings.HasPrefix(anchor, "^") {
		return ""
	}
	return anchor
}

// blockAnchor is the id after `^`. In `Doc#Heading^tail` the heading owns the
// anchor; only a `#` with nothing before the caret leaves it a block.
func blockAnchor(target string) string {
	caret := strings.IndexByte(target, '^')
	if caret < 0 {
		return ""
	}
	if hash := strings.IndexByte(target, '#'); hash >= 0 && hash < caret && strings.TrimSpace(target[hash+1:caret]) != "" {
		return ""
	}
	return strings.TrimSpace(target[caret+1:])
}

// headingLine matches an anchor the ways people write one: the heading's text,
// its percent-encoded form, then the GitHub-style slug of both sides. The first
// hit wins, so an exact name always beats a slug that collides with it.
func headingLine(headings []OutlineItem, anchor string) (int, bool) {
	raw := strings.ToLower(strings.TrimSpace(anchor))
	for _, h := range headings {
		if strings.ToLower(strings.TrimSpace(h.Text)) == raw {
			return h.Line, true
		}
	}
	decodedAnchor, err := url.PathUnescape(anchor)
	if err != nil {
		decodedAnchor = anchor
	}
	decoded := strings.ToLower(strings.TrimSpace(decodedAnchor))
	if decoded != raw {
		for _, h := range headings {
			if strings.ToLower(strings.TrimSpace(h.Text)) == decoded {
				return h.Line, true
			}
		}
	}
	slug := headingSlug(decoded)
	if slug == "" {
		return 0, false
	}
	for _, h := range headings {
		if headingSlug(h.Text) == slug {
			return h.Line, true
		}
	}
	return 0, false
}

// headingSlug is GitHub's heading slug: lowercase, punctuation dropped, each
// space a hyphen. Letters and digits of any script survive.
func headingSlug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('-')
		case r == '_' || r == '-' || unicode.In(r, unicode.L, unicode.N, unicode.M):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// blockLine finds the line a `^id` marks: the line ending in the marker, or for
// a marker alone on its line, the start of the block above it. Ids match
// case-insensitively and the first of a repeated id wins.
func blockLine(body, id string) (int, bool) {
	lines := strings.Split(body, "\n")
	owned := markdownOwnedLines(lines)
	for i, text := range lines {
		if !owned[i] {
			continue
		}
		m := blockIDRe.FindStringSubmatchIndex(text)
		if m == nil || !strings.EqualFold(text[m[2]:m[3]], id) {
			continue
		}
		// The caret sits right before the id.
		if strings.TrimSpace(text[:m[2]-1]) == "" {
			if start, ok := standaloneBlockStart(lines, owned, i); ok {
				return start + 1, true
			}
		}
		return i + 1, true
	}
	return 0, false
}

// standaloneBlockStart is the 0-based first line of the block directly above a
// marker on its own line. Nothing usable above (the top of the note, or only
// frontmatter) leaves the marker's own line as the target.
func standaloneBlockStart(lines []string, owned []bool, marker int) (int, bool) {
	fmEnd := frontmatterEndLine(lines)
	prev := marker - 1
	for prev >= 0 && owned[prev] && strings.TrimSpace(lines[prev]) == "" {
		prev--
	}
	if prev < 0 || prev <= fmEnd || strings.TrimSpace(lines[prev]) == "" {
		return 0, false
	}
	start := prev
	if !owned[prev] {
		// A fence above: the whole run of lines it owns is the block.
		for start-1 > fmEnd && !owned[start-1] {
			start--
		}
		return start, true
	}
	for start-1 > fmEnd && owned[start-1] && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	return start, true
}

// frontmatterEndLine is the 0-based closing `---` of a leading frontmatter
// block, or -1 when the note has none.
func frontmatterEndLine(lines []string) int {
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i
		}
	}
	return -1
}

// markdownOwnedLines marks the lines outside frontmatter and fenced code. The
// fence that opens a block sets what may close it: the same character, at least
// as long, alone on its line. A backtick run with a backtick in its info string
// is an inline code span, not a fence.
func markdownOwnedLines(lines []string) []bool {
	owned := make([]bool, len(lines))
	fmEnd := frontmatterEndLine(lines)
	fence := ""
	for i := fmEnd + 1; i < len(lines); i++ {
		raw := strings.TrimRight(lines[i], "\r")
		m := fenceLineRe.FindStringSubmatch(raw)
		if fence != "" {
			if m != nil && m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(m[2]) == "" {
				fence = ""
			}
			continue
		}
		if m != nil && (m[1][0] != '`' || !strings.Contains(m[2], "`")) {
			fence = m[1]
			continue
		}
		owned[i] = true
	}
	return owned
}
