package vault

import (
	"regexp"
	"strings"
	"unicode"
)

// Regexes mirror the extractors in the desktop's vault-ops.ts and the
// server's parse.go so the extracted metadata matches those builds.
var (
	fenceLineRe   = regexp.MustCompile("^[ \t]*(`{3,}|~{3,})(.*)$")
	inlineCodeRe  = regexp.MustCompile("`[^`\n]*`")
	tagRe         = regexp.MustCompile(`(?:^|\s)#(\p{L}[\p{L}\d_/-]*)`)
	wikilinkRe    = regexp.MustCompile(`(!?)\[\[([^\]|]+?)(?:\|[^\]]+)?\]\]`)
	linkRe        = regexp.MustCompile(`(!?)\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	embedRe       = regexp.MustCompile(`!\[\[([^\]|]+?)(?:\|[^\]]+)?\]\]`)
	frontmatterRe = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)
	headingRe     = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	imageMdRe     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLinkRe      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdWikiAltRe   = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	markupTrimRe  = regexp.MustCompile(`[*_~>]+`)
	wsCollapseRe  = regexp.MustCompile(`\s+`)
	schemeRe      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z\d+.\-]*:`)
	atxHeadingRe  = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	setextRe      = regexp.MustCompile(`^(=+|-+)\s*$`)
	h1LineRe      = regexp.MustCompile(`^( {0,3})#(?:[ \t].*)?$`)
)

var attachmentExts = map[string]bool{
	".apng": true, ".avif": true, ".gif": true, ".jpeg": true, ".jpg": true,
	".png": true, ".svg": true, ".webp": true, ".pdf": true,
	".aac": true, ".flac": true, ".m4a": true, ".mp3": true, ".ogg": true, ".wav": true,
	".m4v": true, ".mov": true, ".mp4": true, ".ogv": true, ".webm": true,
}

// StripCodeContent blanks fenced and inline code so the tag, link and excerpt
// scanners never read code as content. Line-based and indentation-tolerant:
// a fence nested under a list item is still a code block.
func StripCodeContent(body string) string {
	if !strings.Contains(body, "`") && !strings.Contains(body, "~") {
		return body
	}
	lines := strings.Split(body, "\n")
	inFence := false
	var fenceChar byte
	fenceLen := 0
	for i, line := range lines {
		if m := fenceLineRe.FindStringSubmatch(line); m != nil {
			marker := m[1]
			char := marker[0]
			rest := m[2]
			if !inFence {
				// A backtick fence's info string may not contain a backtick.
				if char == '~' || !strings.Contains(rest, "`") {
					inFence = true
					fenceChar = char
					fenceLen = len(marker)
					lines[i] = " "
					continue
				}
			} else if char == fenceChar && len(marker) >= fenceLen && strings.TrimSpace(rest) == "" {
				inFence = false
				lines[i] = " "
				continue
			}
		}
		if inFence {
			lines[i] = " "
		}
	}
	return inlineCodeRe.ReplaceAllString(strings.Join(lines, "\n"), " ")
}

// Frontmatter splits a leading `---` block off the body. ok is false when
// there is none; block is the text between the fences.
func Frontmatter(body string) (block string, rest string, ok bool) {
	m := frontmatterRe.FindStringSubmatchIndex(body)
	if m == nil {
		return "", body, false
	}
	return body[m[2]:m[3]], body[m[1]:], true
}

// ParseFrontmatterFields parses a frontmatter block into flat fields:
// scalars, inline arrays (`tags: [a, b]`) and block lists (`tags:` then
// indented `- a`). Keys are lower-cased; every value is a slice (a scalar is
// a single-element slice). Best-effort, never fails.
func ParseFrontmatterFields(block string) map[string][]string {
	data := map[string][]string{}
	listKey := ""
	for _, rawLine := range strings.Split(block, "\n") {
		rawLine = strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		item := fmListItemRe.FindStringSubmatch(rawLine)
		if listKey != "" && fmLeadWsRe.MatchString(rawLine) && item != nil {
			data[listKey] = append(data[listKey], Unquote(item[1]))
			continue
		}
		kv := fmKvRe.FindStringSubmatch(rawLine)
		if kv == nil {
			listKey = ""
			continue
		}
		key := strings.ToLower(kv[1])
		rest := strings.TrimSpace(kv[2])
		if rest == "" {
			listKey = key
			data[key] = []string{}
			continue
		}
		listKey = ""
		if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
			var arr []string
			for _, s := range strings.Split(rest[1:len(rest)-1], ",") {
				if v := Unquote(s); v != "" {
					arr = append(arr, v)
				}
			}
			data[key] = arr
		} else {
			data[key] = []string{Unquote(rest)}
		}
	}
	return data
}

var (
	fmListItemRe = regexp.MustCompile(`^\s*-\s+(.*)$`)
	fmKvRe       = regexp.MustCompile(`^([A-Za-z0-9_][\w-]*)\s*:\s*(.*)$`)
	fmLeadWsRe   = regexp.MustCompile(`^\s`)
)

// Unquote strips one layer of matching quotes and surrounding whitespace.
func Unquote(v string) string {
	t := strings.TrimSpace(v)
	if len(t) >= 2 {
		first, last := t[0], t[len(t)-1]
		if (first == '"' || first == '\'') && first == last {
			return t[1 : len(t)-1]
		}
	}
	return t
}

// ExtractTags returns the unique tags of a note: frontmatter `tags` (a bare
// scalar splits on commas and whitespace) plus inline `#tags` outside code.
func ExtractTags(body string) []string {
	seen := map[string]bool{}
	out := []string{}
	if block, _, ok := Frontmatter(body); ok {
		fm := ParseFrontmatterFields(block)
		for _, raw := range fm["tags"] {
			for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
				return r == ',' || unicode.IsSpace(r)
			}) {
				tag := strings.TrimPrefix(part, "#")
				if tag != "" && !seen[tag] {
					seen[tag] = true
					out = append(out, tag)
				}
			}
		}
	}
	markdownBody := frontmatterRe.ReplaceAllString(body, "")
	if !strings.Contains(markdownBody, "#") {
		return out
	}
	stripped := StripCodeContent(markdownBody)
	for _, m := range tagRe.FindAllStringSubmatch(stripped, -1) {
		tag := m[1]
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

// ExtractWikilinks returns unique [[wikilink]] targets outside code, embeds
// included, exactly as the desktop CLI reports them.
func ExtractWikilinks(body string) []string {
	if !strings.Contains(body, "[[") {
		return []string{}
	}
	stripped := StripCodeContent(body)
	seen := map[string]bool{}
	out := []string{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(stripped, -1) {
		target := strings.TrimSpace(m[2])
		if target == "" {
			continue
		}
		if !seen[target] {
			seen[target] = true
			out = append(out, target)
		}
	}
	return out
}

// BodyHasLocalAsset reports whether the note embeds or links a local
// attachment (an image, a PDF, audio or video).
func BodyHasLocalAsset(body string) bool {
	if !strings.Contains(body, "](") && !strings.Contains(body, "![[") {
		return false
	}
	stripped := StripCodeContent(body)
	for _, m := range linkRe.FindAllStringSubmatch(stripped, -1) {
		href := strings.TrimSpace(m[2])
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "//") {
			continue
		}
		if schemeRe.MatchString(href) {
			continue
		}
		if localAssetTargetKind(href) != "" {
			return true
		}
	}
	for _, m := range embedRe.FindAllStringSubmatch(stripped, -1) {
		if localAssetTargetKind(strings.TrimSpace(m[1])) != "" {
			return true
		}
	}
	return false
}

func localAssetTargetKind(target string) string {
	clean := target
	if i := strings.IndexAny(clean, "#?"); i >= 0 {
		clean = clean[:i]
	}
	dot := strings.LastIndexByte(clean, '.')
	if dot < 0 {
		return ""
	}
	ext := strings.ToLower(clean[dot:])
	if attachmentExts[ext] {
		return ext
	}
	return ""
}

// BuildExcerpt makes a short plaintext preview from markdown: no
// frontmatter, no code, links reduced to their text, markup stripped,
// whitespace collapsed, capped at 220 characters.
func BuildExcerpt(body string) string {
	withoutFront := body
	if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
		withoutFront = frontmatterRe.ReplaceAllString(body, "")
	}
	text := StripCodeContent(withoutFront)
	if strings.Contains(text, "](") {
		text = imageMdRe.ReplaceAllString(text, " ")
		text = mdLinkRe.ReplaceAllString(text, "$1")
	}
	if strings.Contains(text, "[[") {
		text = mdWikiAltRe.ReplaceAllStringFunc(text, func(s string) string {
			m := mdWikiAltRe.FindStringSubmatch(s)
			if len(m) >= 3 && m[2] != "" {
				return m[2]
			}
			return m[1]
		})
	}
	if strings.Contains(text, "#") {
		text = headingRe.ReplaceAllString(text, "")
	}
	if strings.ContainsAny(text, "*_~>") {
		text = markupTrimRe.ReplaceAllString(text, "")
	}
	text = strings.TrimSpace(wsCollapseRe.ReplaceAllString(text, " "))
	runes := []rune(text)
	if len(runes) > 220 {
		text = string(runes[:220])
	}
	return text
}

// Outline lists the ATX and setext headings of a note, skipping a leading
// frontmatter block and anything inside fenced code. Lines are 1-based.
func Outline(body string) []OutlineItem {
	items := []OutlineItem{}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	start := 0
	if len(lines) > 0 && lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				start = i + 1
				break
			}
		}
	}
	inFence := false
	fenceMarker := ""
	for i := start; i < len(lines); i++ {
		raw := lines[i]
		if m := fenceLineRe.FindStringSubmatch(raw); m != nil {
			marker := m[1]
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker[0] == fenceMarker[0] && len(marker) >= len(fenceMarker) && strings.TrimSpace(m[2]) == "" {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}
		if atx := atxHeadingRe.FindStringSubmatch(raw); atx != nil {
			items = append(items, OutlineItem{Level: len(atx[1]), Text: strings.TrimSpace(atx[2]), Line: i + 1})
			continue
		}
		if i+1 < len(lines) && strings.TrimSpace(raw) != "" {
			if under := setextRe.FindStringSubmatch(lines[i+1]); under != nil {
				level := 2
				if strings.HasPrefix(under[1], "=") {
					level = 1
				}
				items = append(items, OutlineItem{Level: level, Text: strings.TrimSpace(raw), Line: i + 1})
			}
		}
	}
	return items
}

// RetitleLeadingHeading rewrites the note's leading `# Heading` to title,
// skipping frontmatter and blank lines. A note whose first line is anything
// but an H1 is returned unchanged: the heading is never invented.
func RetitleLeadingHeading(body, title string) string {
	if title == "" || strings.ContainsAny(title, "\r\n") {
		return body
	}
	lineStart := 0
	if m := frontmatterRe.FindStringIndex(body); m != nil {
		lineStart = m[1]
	}
	for lineStart < len(body) {
		nl := strings.IndexByte(body[lineStart:], '\n')
		lineEnd := len(body)
		if nl >= 0 {
			lineEnd = lineStart + nl
		}
		raw := body[lineStart:lineEnd]
		line := strings.TrimSuffix(raw, "\r")
		if strings.TrimSpace(line) != "" {
			m := h1LineRe.FindStringSubmatch(line)
			if m == nil {
				return body
			}
			next := m[1] + "# " + title
			if next == line {
				return body
			}
			return body[:lineStart] + next + body[lineStart+len(line):]
		}
		if nl < 0 {
			break
		}
		lineStart = lineEnd + 1
	}
	return body
}

// IsObsidianExcalidrawPath is true for `*.excalidraw.md` drawings.
func IsObsidianExcalidrawPath(p string) bool {
	return strings.HasSuffix(strings.ToLower(p), ".excalidraw.md")
}

var excalidrawPluginRe = regexp.MustCompile(`(?m)^excalidraw-plugin:\s*\S`)

// IsObsidianExcalidrawMarkdown is true when the frontmatter carries
// Obsidian's `excalidraw-plugin` marker.
func IsObsidianExcalidrawMarkdown(content string) bool {
	block, _, ok := Frontmatter(content)
	if !ok {
		return false
	}
	return excalidrawPluginRe.MatchString(block)
}

// YAMLValue quotes a scalar for a `key: value` frontmatter line whenever a
// YAML reader could misread it bare.
func YAMLValue(value string) string {
	needsQuote := value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, ":#\"'\n")
	if !needsQuote && len(value) > 0 {
		switch value[0] {
		case '[', ']', '{', '}', ',', '>', '|', '&', '*', '!', '%', '@', '`':
			needsQuote = true
		case '-', '?':
			if len(value) == 1 || value[1] == ' ' || value[1] == '\t' {
				needsQuote = true
			}
		}
	}
	if !needsQuote {
		return value
	}
	return jsonQuote(value)
}

func jsonQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[r>>4])
				b.WriteByte("0123456789abcdef"[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

var fmKeyRe = regexp.MustCompile(`^\s*([A-Za-z0-9_][\w-]*)\s*:`)

// UpdateFrontmatterFields sets (or, with a nil value, removes) scalar
// frontmatter fields, preserving key order and every other line. Creates the
// block when the note has none. Keys match case-insensitively but are
// written with the casing given.
func UpdateFrontmatterFields(body string, updates []FrontmatterUpdate) string {
	if len(updates) == 0 {
		return body
	}
	norm := strings.ReplaceAll(body, "\r\n", "\n")
	remaining := make([]FrontmatterUpdate, len(updates))
	copy(remaining, updates)
	block, rest, ok := Frontmatter(norm)
	if !ok {
		lines := []string{"---"}
		for _, u := range remaining {
			if u.Value != nil {
				lines = append(lines, u.Key+": "+YAMLValue(*u.Value))
			}
		}
		if len(lines) == 1 {
			return body
		}
		lines = append(lines, "---", "")
		return strings.Join(lines, "\n") + norm
	}
	out := []string{}
	for _, line := range strings.Split(block, "\n") {
		if km := fmKeyRe.FindStringSubmatch(line); km != nil {
			lk := strings.ToLower(km[1])
			hit := -1
			for i, u := range remaining {
				if strings.ToLower(u.Key) == lk {
					hit = i
					break
				}
			}
			if hit >= 0 {
				u := remaining[hit]
				remaining = append(remaining[:hit], remaining[hit+1:]...)
				if u.Value != nil {
					out = append(out, u.Key+": "+YAMLValue(*u.Value))
				}
				continue
			}
		}
		out = append(out, line)
	}
	for _, u := range remaining {
		if u.Value != nil {
			out = append(out, u.Key+": "+YAMLValue(*u.Value))
		}
	}
	return "---\n" + strings.Join(out, "\n") + "\n---\n" + rest
}

// FrontmatterUpdate is one field to set (Value non-nil) or remove (nil).
type FrontmatterUpdate struct {
	Key   string
	Value *string
}

// StrPtr is a convenience for FrontmatterUpdate values.
func StrPtr(s string) *string { return &s }

// ParseFrontmatterScalars is the template/record-page reader: flat
// `key: value` lines only, quotes stripped, keys kept as written.
func ParseFrontmatterScalars(raw string) (data map[string]string, body string) {
	data = map[string]string{}
	block, rest, ok := Frontmatter(raw)
	if !ok {
		return data, raw
	}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, "\r")
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		value := strings.TrimSpace(line[idx+1:])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		data[key] = value
	}
	return data, rest
}
