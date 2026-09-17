package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// Tag completion. Typing `#` and a letter in a note offers the vault's
// existing tags in a small menu under the cursor, the way the desktop
// app's editor does, and inside a frontmatter `tags:` value the same menu
// completes bare tags. The menu never takes the keyboard: typing narrows
// it, and only the keys that move, accept or dismiss are held back from the
// editor. Ctrl-X Ctrl-O (Vim's omni completion) opens it on demand, with
// every tag when nothing is typed yet.

const (
	tagMenuMax  = 20 // the desktop's MAX_SUGGESTIONS
	tagMenuRows = 8
)

// tagToken is the text a completion would replace, on the cursor's line.
type tagToken struct {
	line  int
	start int    // column of the first replaced rune
	after int    // runes right of the cursor that go too (frontmatter tokens)
	query string // what ranks the tags: the token without its '#'
	bare  bool   // insert the tag without '#': frontmatter
}

type tagMenu struct {
	path   string
	tok    tagToken
	manual bool
	items  []tagCount
	cursor int
	scroll int
	// at is where the cursor stood when the menu was last refreshed. A cursor
	// that moved by other means (the mouse, a reload) makes the menu stale.
	at vim.Pos
}

func isTagRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '/' || r == '-'
}

// hashTagToken finds the `#query` that ends at the cursor: a '#' at the
// start of the line or after whitespace, then tag characters only. So
// `foo#bar` and `https://x.y/#top` never complete.
func hashTagToken(line []rune, col int) (tagToken, bool) {
	i := min(col, len(line))
	for i > 0 && isTagRune(line[i-1]) {
		i--
	}
	if i == 0 || line[i-1] != '#' {
		return tagToken{}, false
	}
	hash := i - 1
	if hash > 0 && !unicode.IsSpace(line[hash-1]) {
		return tagToken{}, false
	}
	return tagToken{start: hash, query: string(line[i:col])}, true
}

var (
	frontTagsKeyRe  = regexp.MustCompile(`(?i)^tags\s*:\s*`)
	frontKeyRe      = regexp.MustCompile(`^([A-Za-z0-9_][\w-]*)\s*:\s*(.*)$`)
	frontListItemRe = regexp.MustCompile(`^\s*-\s+`)
)

func isFrontTokenDelimiter(r rune, body bool) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(`,[]"'`, r) || (body && r == '#')
}

// frontmatterTagToken finds the tag being typed in a frontmatter `tags:`
// value: the inline list, the scalar, or a `- item` under a bare `tags:`.
func frontmatterTagToken(lines []string, lineNo, col int) (tagToken, bool) {
	text := lines[lineNo]
	valueStart := -1
	if m := frontTagsKeyRe.FindString(text); m != "" {
		valueStart = len([]rune(m))
	} else if m := frontListItemRe.FindString(text); m != "" && underTagsKey(lines, lineNo) {
		valueStart = len([]rune(m))
	}
	runes := []rune(text)
	if valueStart < 0 || col < valueStart || col > len(runes) {
		return tagToken{}, false
	}
	start := col
	for start > valueStart && !isFrontTokenDelimiter(runes[start-1], false) {
		start--
	}
	end := col
	for end < len(runes) && !isFrontTokenDelimiter(runes[end], true) {
		end++
	}
	query := strings.TrimPrefix(string(runes[start:col]), "#")
	return tagToken{start: start, after: end - col, query: query, bare: true}, true
}

// underTagsKey reports whether a frontmatter list item belongs to `tags:`.
func underTagsKey(lines []string, lineNo int) bool {
	for i := lineNo - 1; i >= 1; i-- {
		text := lines[i]
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := frontKeyRe.FindStringSubmatch(text); m != nil {
			return strings.EqualFold(m[1], "tags") && strings.TrimSpace(m[2]) == ""
		}
		if !frontListItemRe.MatchString(text) {
			return false
		}
	}
	return false
}

// tagTokenAt is the token the cursor is completing, if the place allows
// tags at all: not in code, not in a heading, not in a link.
func tagTokenAt(ed *vim.Editor) (tagToken, bool) {
	cur := ed.Cursor()
	lines := ed.Lines()
	if cur.Line >= len(lines) {
		return tagToken{}, false
	}
	blocks := computeBlockState(lines)
	var tok tagToken
	var ok bool
	switch {
	case blocks.fence[cur.Line]:
		return tagToken{}, false
	case blocks.front[cur.Line]:
		tok, ok = frontmatterTagToken(lines, cur.Line, cur.Col)
	default:
		text := lines[cur.Line]
		if headingRe.MatchString(text) {
			return tagToken{}, false
		}
		tok, ok = hashTagToken(ed.LineRunes(cur.Line), cur.Col)
		if ok {
			if class := lineClasses(text, false, false)[tok.start]; class == stCode || class == stLink {
				return tagToken{}, false
			}
		}
	}
	tok.line = cur.Line
	return tok, ok
}

// rankTags orders the candidates the way the desktop does: tags that start
// with the query, then tags that contain it, each by how many notes use
// them and then by name. Matching ignores case, the tag typed in full is
// not offered back, and nested tags match as plain text (`deep` finds
// `work/deep`). An empty query keeps every tag.
func rankTags(query string, pool []tagCount) []tagCount {
	q := strings.ToLower(query)
	type ranked struct {
		tagCount
		rank int
	}
	out := []ranked{}
	for _, t := range pool {
		lower := strings.ToLower(t.tag)
		switch {
		case lower == q:
		case strings.HasPrefix(lower, q):
			out = append(out, ranked{t, 0})
		case strings.Contains(lower, q):
			out = append(out, ranked{t, 1})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		if li, lj := strings.ToLower(out[i].tag), strings.ToLower(out[j].tag); li != lj {
			return li < lj
		}
		return out[i].tag < out[j].tag
	})
	if len(out) > tagMenuMax {
		out = out[:tagMenuMax]
	}
	tags := make([]tagCount, len(out))
	for i, r := range out {
		tags[i] = r.tagCount
	}
	return tags
}

// tagPool counts, per tag as written, the notes that use it. The open
// note counts with the tags in its buffer rather than the ones last
// indexed, so a tag typed a moment ago is already on offer.
func (a *App) tagPool(buf *noteBuffer) []tagCount {
	counts := map[string]int{}
	if a.idx != nil {
		for _, n := range a.idx.notes {
			if n.Folder == vault.FolderTrash || n.Path == buf.path {
				continue
			}
			for _, t := range n.Tags {
				counts[t]++
			}
		}
	}
	for _, t := range vault.ExtractTags(buf.ed.Text()) {
		counts[t]++
	}
	pool := make([]tagCount, 0, len(counts))
	for tag, n := range counts {
		pool = append(pool, tagCount{tag, n})
	}
	return pool
}

// liveTagMenu is the menu, if it still belongs to what is on screen.
func (a *App) liveTagMenu() *tagMenu {
	m := a.tagMenu
	if m == nil {
		return nil
	}
	buf := a.activeBuffer()
	if buf == nil || a.overlay != nil || a.focus != focusPane || buf.path != m.path ||
		buf.ed.Mode() != vim.ModeInsert || buf.ed.Cursor() != m.at {
		a.tagMenu = nil
		return nil
	}
	return m
}

// refreshTagMenu runs after every key the editor took in insert mode: it
// opens the menu when a tag is being typed, narrows it as the token grows,
// and closes it when the cursor leaves the token or nothing matches.
func (a *App) refreshTagMenu(buf *noteBuffer) {
	prev := a.tagMenu
	a.tagMenu = nil
	if buf.ed.Mode() != vim.ModeInsert {
		a.tagDismissed = nil
		return
	}
	cur := buf.ed.Cursor()
	tok, ok := tagTokenAt(buf.ed)
	manual := prev != nil && prev.manual && prev.path == buf.path
	if manual && (!ok || tok.start != prev.tok.start) {
		// A menu opened on a plain word follows that word while it grows.
		tok, ok = wordTagToken(buf.ed.LineRunes(cur.Line), cur, prev.tok.start)
	}
	if !ok {
		a.tagDismissed = nil
		return
	}
	// A dismissed or just-completed token stays quiet until the cursor
	// leaves it, as the desktop's menu does after Escape or a pick.
	if d := a.tagDismissed; d != nil {
		if d.path == buf.path && d.line == tok.line && d.start == tok.start {
			return
		}
		a.tagDismissed = nil
	}
	if tok.query == "" && !manual {
		return
	}
	items := rankTags(tok.query, a.tagPool(buf))
	if len(items) == 0 {
		return
	}
	m := &tagMenu{path: buf.path, tok: tok, manual: manual, items: items, at: cur}
	if prev != nil && prev.path == buf.path && prev.tok.query == tok.query && prev.cursor < len(items) {
		m.cursor, m.scroll = prev.cursor, prev.scroll
	}
	a.tagMenu = m
}

// wordTagToken is the run of tag characters from start to the cursor, for
// a menu that was opened by hand where no '#' had been typed.
func wordTagToken(line []rune, cur vim.Pos, start int) (tagToken, bool) {
	if start > cur.Col || cur.Col > len(line) {
		return tagToken{}, false
	}
	for _, r := range line[start:cur.Col] {
		if !isTagRune(r) {
			return tagToken{}, false
		}
	}
	return tagToken{line: cur.Line, start: start, query: string(line[start:cur.Col])}, true
}

type tagDismissal struct {
	path        string
	line, start int
}

// openTagMenu is Ctrl-X Ctrl-O: the menu on demand. On a `#tag` or in a
// frontmatter `tags:` value it completes that token, even an empty one;
// anywhere else the word before the cursor becomes the query and the pick
// replaces it with `#tag`.
func (a *App) openTagMenu(buf *noteBuffer) {
	if buf.ed.Mode() != vim.ModeInsert {
		return
	}
	cur := buf.ed.Cursor()
	tok, ok := tagTokenAt(buf.ed)
	if !ok {
		line := buf.ed.LineRunes(cur.Line)
		start := min(cur.Col, len(line))
		for start > 0 && isTagRune(line[start-1]) {
			start--
		}
		tok, _ = wordTagToken(line, cur, start)
	}
	a.tagDismissed = nil
	items := rankTags(tok.query, a.tagPool(buf))
	if len(items) == 0 {
		a.tagMenu = nil
		a.notify("No tags match")
		return
	}
	a.tagMenu = &tagMenu{path: buf.path, tok: tok, manual: true, items: items, at: cur}
}

// tagMenuKey takes the keys that drive an open menu. Everything else goes
// on to the editor, which is how typing narrows the list.
func (a *App) tagMenuKey(buf *noteBuffer, k vim.Key) bool {
	m := a.liveTagMenu()
	if m == nil {
		return false
	}
	switch {
	case k.IsCtrl('n') || k.IsCtrl('j') || k.Is("down"):
		m.cursor = (m.cursor + 1) % len(m.items)
	case k.IsCtrl('p') || k.IsCtrl('k') || k.Is("up"):
		m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
	case k.Is("enter") || (k.Is("tab") && !k.Shift) || k.IsCtrl('y'):
		a.acceptTag(buf, m)
	case k.IsCtrl('e'):
		a.dismissTagMenu(m)
	case k.Is("esc") && !a.prefs.VimMode:
		a.dismissTagMenu(m)
	default:
		// Esc in Vim mode closes the menu and leaves insert mode, as Vim's
		// own popup menu does; the refresh after the key drops the menu.
		return false
	}
	return true
}

func (a *App) dismissTagMenu(m *tagMenu) {
	a.tagDismissed = &tagDismissal{path: m.path, line: m.tok.line, start: m.tok.start}
	a.tagMenu = nil
}

// acceptTag replaces the typed token with the chosen tag. No space follows
// it, as on the desktop: the tag may still get a `/child`.
func (a *App) acceptTag(buf *noteBuffer, m *tagMenu) {
	text := m.items[m.cursor].tag
	if !m.tok.bare {
		text = "#" + text
		// A '#' glued to the previous word would not be a tag.
		if line := buf.ed.LineRunes(m.tok.line); m.tok.start > 0 && !unicode.IsSpace(line[m.tok.start-1]) {
			text = " " + text
		}
	}
	cur := buf.ed.Cursor()
	buf.ed.ReplaceAtCursor(cur.Col-m.tok.start, m.tok.after, text)
	a.dismissTagMenu(m)
	if !m.tok.bare && strings.HasPrefix(text, " ") {
		a.tagDismissed.start++
	}
}

// tagMenuAnchor is the screen cell of the token's first rune, so the menu
// stays put while the token grows.
func (a *App) tagMenuAnchor(buf *noteBuffer, m *tagMenu) (x, y int, ok bool) {
	rc := a.contentRect(a.activePane)
	line := buf.ed.LineRunes(m.tok.line)
	for _, row := range buf.rows {
		if row.line != m.tok.line || m.tok.start < row.segStart || m.tok.start > row.segEnd {
			continue
		}
		if m.tok.start == row.segEnd && row.segEnd < len(line) {
			continue // the token starts the next wrapped row
		}
		return rc.x + row.x + cellWidth(string(line[row.segStart:m.tok.start])), rc.y + row.y, true
	}
	return 0, 0, false
}

// paintTagMenu draws the menu under the token, or above it near the bottom
// of the screen.
func (a *App) paintTagMenu(screen string) string {
	m := a.liveTagMenu()
	if m == nil {
		return screen
	}
	x, y, ok := a.tagMenuAnchor(a.activeBuffer(), m)
	if !ok {
		return screen
	}
	block := m.render(a)
	h := strings.Count(block, "\n") + 1
	// The box's border and padding sit left of the text, so the tags line
	// up with the token being typed.
	x = max(0, x-2)
	top := y + 1
	if top+h > a.height-2 {
		top = max(0, y-h)
	}
	screen, _ = overlayPlaceAt(screen, block, x, top, a.width, a.height)
	return screen
}

func (m *tagMenu) render(a *App) string {
	th := a.theme
	rows := min(len(m.items), tagMenuRows)
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+rows {
		m.scroll = m.cursor - rows + 1
	}
	prefix := "#"
	if m.tok.bare {
		prefix = ""
	}
	labelW, countW := 0, 0
	for _, it := range m.items {
		labelW = max(labelW, cellWidth(prefix+it.tag))
		if it.count > 1 {
			countW = max(countW, len(fmt.Sprint(it.count)))
		}
	}
	labelW = min(labelW, max(12, a.width-12))
	inner := labelW
	if countW > 0 {
		inner += 2 + countW
	}
	lines := make([]string, 0, rows+1)
	for i := m.scroll; i < m.scroll+rows; i++ {
		it := m.items[i]
		label := padRight(truncateCells(prefix+it.tag, labelW), labelW)
		count := ""
		if countW > 0 {
			if it.count > 1 {
				count = fmt.Sprintf("  %*d", countW, it.count)
			} else {
				count = strings.Repeat(" ", 2+countW)
			}
		}
		if i == m.cursor {
			lines = append(lines, th.SelectedFocus.Render(label+count))
			continue
		}
		if q := m.tok.query; q != "" {
			label = padRight(highlightMatch(th, truncateCells(prefix+it.tag, labelW), q), labelW)
		}
		lines = append(lines, th.Tag.Render(label)+th.Muted.Render(count))
	}
	if len(m.items) > rows {
		lines = append(lines, th.Muted.Render(padRight(fmt.Sprintf("%d of %d", m.cursor+1, len(m.items)), inner)))
	}
	return th.Overlay.Render(strings.Join(lines, "\n"))
}
