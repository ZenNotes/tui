package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/zennotescli/internal/search"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// overlay is a modal surface: a palette, prompt, menu or dialog.
type overlay interface {
	handleKey(a *App, k vim.Key)
	render(a *App, w, h int) string
}

// --- text input shared by prompts and palettes ---

type textInput struct {
	runes  []rune
	cursor int
}

func newTextInput(initial string) textInput {
	r := []rune(initial)
	return textInput{runes: r, cursor: len(r)}
}

func (t *textInput) String() string { return string(t.runes) }

// insert types text at the cursor.
func (t *textInput) insert(s string) {
	r := []rune(s)
	t.runes = append(t.runes[:t.cursor], append(r, t.runes[t.cursor:]...)...)
	t.cursor += len(r)
}

// endsWith is true when the text before the cursor ends with suffix.
func (t *textInput) endsWith(suffix string) bool {
	r := []rune(suffix)
	if t.cursor < len(r) {
		return false
	}
	return string(t.runes[t.cursor-len(r):t.cursor]) == suffix
}

// handle applies an editing key; false means the key was not for the input.
func (t *textInput) handle(k vim.Key) bool {
	switch {
	case k.Printable():
		t.runes = append(t.runes[:t.cursor], append([]rune{k.Rune}, t.runes[t.cursor:]...)...)
		t.cursor++
	case k.Is("backspace") || k.IsCtrl('h'):
		if t.cursor > 0 {
			t.runes = append(t.runes[:t.cursor-1], t.runes[t.cursor:]...)
			t.cursor--
		}
	case k.Is("delete"):
		if t.cursor < len(t.runes) {
			t.runes = append(t.runes[:t.cursor], t.runes[t.cursor+1:]...)
		}
	case k.Is("left") || k.IsCtrl('b'):
		if t.cursor > 0 {
			t.cursor--
		}
	case k.Is("right") || k.IsCtrl('f'):
		if t.cursor < len(t.runes) {
			t.cursor++
		}
	case k.Is("home") || k.IsCtrl('a'):
		t.cursor = 0
	case k.Is("end") || k.IsCtrl('e'):
		t.cursor = len(t.runes)
	case k.IsCtrl('u'):
		t.runes = t.runes[t.cursor:]
		t.cursor = 0
	case k.IsCtrl('w'):
		i := t.cursor
		for i > 0 && t.runes[i-1] == ' ' {
			i--
		}
		for i > 0 && t.runes[i-1] != ' ' {
			i--
		}
		t.runes = append(t.runes[:i], t.runes[t.cursor:]...)
		t.cursor = i
	default:
		return false
	}
	return true
}

func (t *textInput) render(th Theme, width int) string {
	before := string(t.runes[:t.cursor])
	after := ""
	if t.cursor < len(t.runes) {
		after = string(t.runes[t.cursor:])
	}
	line := before + th.Cursor.Render(firstCellOr(after, " ")) + restAfterFirst(after)
	return padRight(line, width)
}

// --- palette ---

type paletteItem struct {
	label  string
	detail string
	hint   string
	id     string
	data   any
}

type palette struct {
	title       string
	placeholder string
	input       textInput
	items       []paletteItem
	filtered    []paletteItem
	cursor      int
	scroll      int
	source      func(a *App, query string) []paletteItem
	onSelect    func(a *App, item paletteItem)
	onEmpty     func(a *App, query string)
	onDelete    func(a *App, item paletteItem)
	emptyHint   string
	minQuery    int
	// highlight underlines the query's matched characters in labels; on by
	// default for fuzzy-filtered palettes, opt-in for source-backed ones.
	highlight bool
	// onCancel runs when the palette closes without a choice, so a flow
	// that opened it from another overlay can restore that overlay.
	onCancel func(a *App)
}

func (p *palette) refilter(a *App) {
	query := strings.TrimSpace(p.input.String())
	if p.source != nil {
		if len([]rune(query)) < p.minQuery {
			p.filtered = nil
		} else {
			p.filtered = p.source(a, query)
		}
	} else if query == "" {
		p.filtered = p.items
	} else {
		type scored struct {
			item  paletteItem
			score float64
		}
		out := []scored{}
		for _, it := range p.items {
			s := search.Fuzzy(query, it.label)
			if s <= 0 && it.detail != "" {
				s = search.Fuzzy(query, it.detail) * 0.6
			}
			if s > 0 {
				out = append(out, scored{it, s})
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
		p.filtered = make([]paletteItem, len(out))
		for i, s := range out {
			p.filtered[i] = s.item
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

func (p *palette) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc") || k.IsCtrl('c') || k.IsCtrl('['):
		a.overlay = nil
		if p.onCancel != nil {
			p.onCancel(a)
		}
	case k.Is("enter"):
		if len(p.filtered) == 0 {
			if p.onEmpty != nil && strings.TrimSpace(p.input.String()) != "" {
				query := strings.TrimSpace(p.input.String())
				a.overlay = nil
				p.onEmpty(a, query)
			}
			return
		}
		item := p.filtered[p.cursor]
		a.overlay = nil
		p.onSelect(a, item)
	case k.Is("down") || k.IsCtrl('n') || k.IsCtrl('j') || (k.Is("tab") && !k.Shift):
		if len(p.filtered) > 0 {
			p.cursor = (p.cursor + 1) % len(p.filtered)
		}
	case k.Is("up") || k.IsCtrl('p') || k.IsCtrl('k') || k.Is("shift+tab"):
		if len(p.filtered) > 0 {
			p.cursor = (p.cursor - 1 + len(p.filtered)) % len(p.filtered)
		}
	case k.Is("pgdn"):
		p.cursor = min(len(p.filtered)-1, p.cursor+10)
	case k.Is("pgup"):
		p.cursor = max(0, p.cursor-10)
	case k.IsCtrl('d'):
		if p.onDelete != nil && len(p.filtered) > 0 {
			item := p.filtered[p.cursor]
			p.onDelete(a, item)
			p.refilter(a)
		}
	default:
		if p.input.handle(k) {
			p.cursor = 0
			p.refilter(a)
		}
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *palette) render(a *App, w, h int) string {
	th := a.theme
	width := min(w-4, max(40, w*2/3))
	inner := width - 2
	rows := min(len(p.filtered), max(3, h-10))
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+rows {
		p.scroll = p.cursor - rows + 1
	}
	lines := []string{th.OverlayTitle.Render(p.title)}
	prompt := "› "
	lines = append(lines, prompt+p.input.render(th, inner-lipgloss.Width(prompt)))
	if p.input.String() == "" && p.placeholder != "" {
		lines[len(lines)-1] = prompt + th.Cursor.Render(" ") + th.Muted.Render(padRight(p.placeholder, inner-3))
	}
	lines = append(lines, th.Muted.Render(strings.Repeat("─", inner)))
	if len(p.filtered) == 0 {
		hint := p.emptyHint
		if hint == "" {
			hint = "No matches"
		}
		lines = append(lines, th.Muted.Render(padRight(hint, inner)))
	}
	for i := p.scroll; i < p.scroll+rows && i < len(p.filtered); i++ {
		it := p.filtered[i]
		label := it.label
		hint := it.hint
		avail := inner - 2
		if hint != "" {
			avail -= lipgloss.Width(hint) + 2
		}
		text := truncateCells(label, avail)
		if q := strings.TrimSpace(p.input.String()); q != "" && p.source == nil || q != "" && p.highlight {
			text = highlightMatch(th, text, q)
		}
		if it.detail != "" {
			room := avail - cellWidth(text) - 2
			if room > 6 {
				text += "  " + th.Muted.Render(truncateCells(it.detail, room))
			}
		}
		row := padRight(text, inner-2-func() int {
			if hint == "" {
				return 0
			}
			return lipgloss.Width(hint) + 2
		}())
		if hint != "" {
			row += "  " + th.KeyHint.Render(hint)
		}
		if i == p.cursor {
			row = th.SelectedFocus.Render(padRight("▸ "+ansiStrip(row), inner))
		} else {
			row = "  " + row
		}
		lines = append(lines, padRight(row, inner))
	}
	if len(p.filtered) > rows {
		lines = append(lines, th.Muted.Render(fmt.Sprintf("%d of %d", p.cursor+1, len(p.filtered))))
	}
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}

// ansiStrip removes styling so a selected row can be restyled whole.
func ansiStrip(s string) string {
	return stripAnsi(s)
}

// --- prompt ---

type prompt struct {
	title    string
	input    textInput
	help     string
	onSubmit func(a *App, text string)
	// onLink runs when `[[` is typed, for prompts whose text may link notes.
	onLink func(a *App, p *prompt)
	// masked hides what is typed: tokens and other secrets.
	masked bool
}

func (a *App) promptFor(title, initial, help string, onSubmit func(a *App, text string)) {
	a.overlay = &prompt{title: title, input: newTextInput(initial), help: help, onSubmit: onSubmit}
}

// promptSecret is promptFor with the input masked.
func (a *App) promptSecret(title, help string, onSubmit func(a *App, text string)) {
	a.overlay = &prompt{title: title, input: newTextInput(""), help: help, onSubmit: onSubmit, masked: true}
}

func (p *prompt) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc") || k.IsCtrl('c'):
		a.overlay = nil
	case k.Is("enter"):
		text := p.input.String()
		a.overlay = nil
		p.onSubmit(a, text)
	default:
		p.input.handle(k)
		if p.onLink != nil && k.IsRune('[') && p.input.endsWith("[[") {
			p.onLink(a, p)
		}
	}
}

func (p *prompt) render(a *App, w, h int) string {
	th := a.theme
	width := min(w-4, max(40, w/2))
	inner := width - 2
	field := p.input.render(th, inner)
	if p.masked {
		dots := strings.Repeat("•", len(p.input.runes))
		field = padRight(dots+th.Cursor.Render(" "), inner)
	}
	lines := []string{th.OverlayTitle.Render(p.title), field}
	if p.help != "" {
		lines = append(lines, th.Muted.Render(truncateCells(p.help, inner)))
	}
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}

// --- confirm ---

type confirmDialog struct {
	message string
	onYes   func()
	onNo    func()
}

func (a *App) confirm(message string, onYes func()) {
	a.overlay = &confirmDialog{message: message, onYes: onYes}
}

func (c *confirmDialog) handleKey(a *App, k vim.Key) {
	switch {
	case k.IsRune('y') || k.IsRune('Y') || k.Is("enter"):
		a.overlay = nil
		c.onYes()
	case k.IsRune('n') || k.IsRune('N') || k.Is("esc") || k.IsCtrl('c'):
		a.overlay = nil
		if c.onNo != nil {
			c.onNo()
		}
	}
}

func (c *confirmDialog) render(a *App, w, h int) string {
	th := a.theme
	width := min(w-4, max(30, lipgloss.Width(c.message)+6))
	lines := []string{c.message, "", th.KeyHint.Render("y") + th.Dim.Render(" yes    ") + th.KeyHint.Render("n") + th.Dim.Render(" no")}
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}

// --- menu ---

type menuItem struct {
	key   string
	label string
	run   func(a *App)
	sep   bool
}

type menu struct {
	title  string
	items  []menuItem
	cursor int
	// anchor is the screen cell the menu opens at; nil centers it.
	anchor *point
}

type point struct{ x, y int }

// showMenu opens a menu next to whatever it is about: the pointer when a
// mouse button opened it, else the selected row of the focused surface,
// else the middle of the screen.
func (a *App) showMenu(title string, items []menuItem) {
	anchor := a.menuAnchor
	if anchor == nil {
		anchor = a.selectionAnchor()
	}
	a.overlay = &menu{title: title, items: items, anchor: anchor}
}

func (m *menu) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc") || k.IsCtrl('c') || k.IsRune('q'):
		a.overlay = nil
		return
	case k.Is("enter"):
		if m.cursor < len(m.items) && !m.items[m.cursor].sep {
			item := m.items[m.cursor]
			a.overlay = nil
			item.run(a)
		}
		return
	case k.Is("down") || k.IsRune('j') || k.IsCtrl('n'):
		m.step(1)
		return
	case k.Is("up") || k.IsRune('k') || k.IsCtrl('p'):
		m.step(-1)
		return
	}
	if k.Printable() {
		for _, it := range m.items {
			if !it.sep && it.key == string(k.Rune) {
				a.overlay = nil
				it.run(a)
				return
			}
		}
	}
}

func (m *menu) step(delta int) {
	if len(m.items) == 0 {
		return
	}
	for i := 0; i < len(m.items); i++ {
		m.cursor = ((m.cursor+delta)%len(m.items) + len(m.items)) % len(m.items)
		if !m.items[m.cursor].sep {
			return
		}
	}
}

func (m *menu) render(a *App, w, h int) string {
	th := a.theme
	width := 0
	for _, it := range m.items {
		width = max(width, cellWidth(it.label)+8)
	}
	width = max(width, cellWidth(m.title)+4)
	width = min(w-4, max(28, width))
	inner := width - 2
	lines := []string{th.OverlayTitle.Render(m.title)}
	for i, it := range m.items {
		if it.sep {
			lines = append(lines, th.Muted.Render(strings.Repeat("─", inner)))
			continue
		}
		key := padRight(th.KeyHint.Render(it.key), 3)
		row := key + it.label
		if i == m.cursor {
			row = th.SelectedFocus.Render(padRight(padRight(it.key, 3)+ansiStrip(it.label), inner))
		}
		lines = append(lines, padRight(row, inner))
	}
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}

// --- quick capture ---

type captureOverlay struct {
	ed *vim.Editor
}

func (a *App) openQuickCapture() {
	opts := a.editorOptions()
	ed := vim.New("", opts, vim.Hooks{})
	ed.SetViewport(8)
	if a.prefs.VimMode {
		ed.HandleKey(vim.R('i'))
	}
	a.overlay = &captureOverlay{ed: ed}
}

func (c *captureOverlay) handleKey(a *App, k vim.Key) {
	text := strings.TrimSpace(c.ed.Text())
	switch {
	case k.IsCtrl('s'):
		a.overlay = nil
		a.saveCapture(c.ed.Text())
		return
	case k.IsCtrl('c'):
		a.overlay = nil
		return
	case k.Is("esc") && (!a.prefs.VimMode || c.ed.Mode() == vim.ModeNormal):
		if text == "" {
			a.overlay = nil
			return
		}
		a.confirm("Discard the capture?", func() { a.overlay = nil })
		return
	}
	c.ed.HandleKey(k)
}

func (c *captureOverlay) render(a *App, w, h int) string {
	th := a.theme
	width := min(w-4, max(50, w*2/3))
	inner := width - 2
	lines := []string{th.OverlayTitle.Render("Quick capture") + th.Muted.Render("  first line becomes the title · Ctrl+S save · Esc cancel")}
	c.ed.SetViewport(8)
	c.ed.EnsureCursorVisible()
	body := renderPlainEditor(a, c.ed, inner, 8, true)
	lines = append(lines, strings.Split(body, "\n")...)
	mode := c.ed.Mode().Label()
	if !a.prefs.VimMode {
		mode = "EDIT"
	}
	lines = append(lines, th.Muted.Render(mode))
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}

// saveCapture files a capture as a quick note: first line title, rest body.
func (a *App) saveCapture(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		a.notify("Nothing to capture")
		return
	}
	lines := strings.SplitN(trimmed, "\n", 2)
	title := strings.TrimSpace(strings.TrimLeft(lines[0], "# "))
	body := ""
	if len(lines) > 1 {
		body = strings.TrimSpace(lines[1])
	}
	if title == "" {
		title = "Quick Note"
	}
	folder, sub := vault.FolderQuick, ""
	content := "# " + title + "\n\n" + body + "\n"
	meta, err := a.backend.CreateNote(context.Background(), folder, title, sub, &content)
	if err != nil {
		a.notifyError("Capture failed: " + err.Error())
		return
	}
	a.notify("Saved to Quick Notes: " + meta.Title)
	a.refreshIndex()
}

// --- which-key hints ---

func (a *App) renderWhichKey() string {
	th := a.theme
	l := a.leader
	if l == nil {
		return ""
	}
	title := "Space"
	if len(l.keys) > 0 {
		title += " " + l.pendingLabel()
	}
	lines := []string{th.OverlayTitle.Render(title)}
	items := []string{}
	for _, child := range l.node.children {
		if child.hidden {
			continue
		}
		label := child.title
		if len(child.children) > 0 {
			label = "+" + label
		}
		items = append(items, th.KeyHint.Render(padRight(child.key, 2))+th.Dim.Render("→ ")+label)
	}
	cols := 2
	if len(items) > 10 {
		cols = 3
	}
	colW := 0
	for _, it := range items {
		colW = max(colW, lipgloss.Width(it)+2)
	}
	rows := (len(items) + cols - 1) / cols
	for r := 0; r < rows; r++ {
		row := ""
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i < len(items) {
				row += padRight(items[i], colW)
			}
		}
		lines = append(lines, row)
	}
	return th.Overlay.Render(strings.Join(lines, "\n"))
}

// --- hint mode ---

type hintTarget struct {
	x, y   int
	label  string
	run    func(a *App)
	length int
}

type hintOverlay struct {
	targets []hintTarget
	typed   string
}

var hintAlphabet = []rune("asdfghjklqwertyuiopzxcvbnm")

func hintLabels(n int) []string {
	if n <= len(hintAlphabet) {
		out := make([]string, n)
		for i := range out {
			out[i] = string(hintAlphabet[i])
		}
		return out
	}
	out := make([]string, 0, n)
	for _, a := range hintAlphabet {
		for _, b := range hintAlphabet {
			out = append(out, string(a)+string(b))
			if len(out) == n {
				return out
			}
		}
	}
	return out
}

// startHintMode labels the links or rows of the focused surface.
func (a *App) startHintMode() {
	targets := a.hintTargets()
	if len(targets) == 0 {
		a.notify("Nothing to hint here")
		return
	}
	labels := hintLabels(len(targets))
	for i := range targets {
		targets[i].label = labels[i]
	}
	a.overlay = &hintOverlay{targets: targets}
}

func (h *hintOverlay) handleKey(a *App, k vim.Key) {
	if k.Is("esc") || k.IsCtrl('c') || !k.Printable() {
		a.overlay = nil
		return
	}
	h.typed += string(k.Rune)
	matched := false
	for _, t := range h.targets {
		if t.label == h.typed {
			a.overlay = nil
			t.run(a)
			return
		}
		if strings.HasPrefix(t.label, h.typed) {
			matched = true
		}
	}
	if !matched {
		a.overlay = nil
	}
}

func (h *hintOverlay) render(a *App, w, hgt int) string {
	// Hints are painted directly by the app over the base screen.
	return ""
}

// paintHints draws hint labels on the finished screen.
func (a *App) paintHints(screen string) string {
	h, ok := a.overlay.(*hintOverlay)
	if !ok {
		return screen
	}
	lines := strings.Split(screen, "\n")
	for _, t := range h.targets {
		if t.y < 0 || t.y >= len(lines) {
			continue
		}
		if !strings.HasPrefix(t.label, h.typed) {
			continue
		}
		label := a.theme.Hint.Reverse(true).Render(t.label)
		lines[t.y] = splice(lines[t.y], t.x, label, a.width)
	}
	return strings.Join(lines, "\n")
}

// highlightMatch underlines the characters of a label that a query matches.
func highlightMatch(th Theme, label, query string) string {
	positions := search.MatchPositions(query, label)
	if len(positions) == 0 {
		return label
	}
	set := map[int]bool{}
	for _, p := range positions {
		set[p] = true
	}
	var b strings.Builder
	runes := []rune(label)
	i := 0
	for i < len(runes) {
		j := i
		for j < len(runes) && set[j] == set[i] {
			j++
		}
		chunk := string(runes[i:j])
		if set[i] {
			b.WriteString(th.Match.Render(chunk))
		} else {
			b.WriteString(chunk)
		}
		i = j
	}
	return b.String()
}

// --- ex prompt for panels without an editor ---

// exPrompt is the `:` line of the sidebar and the views, with Vim's
// wildmenu behavior: Tab cycles the completions of the command or its
// argument, Shift+Tab cycles back, Up and Down recall history.
type exPrompt struct {
	input       textInput
	completions []string
	compIdx     int
	compBase    string
	histIdx     int
	saved       string
}

func (p *exPrompt) resetCompletion() {
	p.completions = nil
	p.compIdx = -1
}

func (p *exPrompt) complete(a *App, backward bool) {
	if p.completions == nil {
		text := p.input.String()
		keep, cands := a.completeEx(text)
		word := text
		if cands != nil && strings.HasPrefix(text, keep) {
			word = text[len(keep):]
		} else {
			keep = ""
			cands = a.exCommandNames()
		}
		p.compBase = keep
		matches := []string{}
		lower := strings.ToLower(word)
		seen := map[string]bool{}
		for _, c := range cands {
			if strings.HasPrefix(strings.ToLower(c), lower) && !seen[c] {
				seen[c] = true
				matches = append(matches, c)
			}
		}
		if len(matches) == 0 {
			return
		}
		p.completions = append(matches, word)
		p.compIdx = -1
	}
	if backward {
		p.compIdx--
		if p.compIdx < 0 {
			p.compIdx = len(p.completions) - 1
		}
	} else {
		p.compIdx = (p.compIdx + 1) % len(p.completions)
	}
	p.input = newTextInput(p.compBase + p.completions[p.compIdx])
}

func (p *exPrompt) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc") || k.IsCtrl('c'):
		a.overlay = nil
	case k.Is("enter"):
		text := p.input.String()
		a.overlay = nil
		if strings.TrimSpace(text) != "" {
			a.exHistory = append(a.exHistory, text)
			if len(a.exHistory) > 50 {
				a.exHistory = a.exHistory[1:]
			}
		}
		a.runCommandLine(text)
	case k.Is("tab") && !k.Shift:
		p.complete(a, false)
	case k.Is("shift+tab"):
		p.complete(a, true)
	case k.Is("up") || k.IsCtrl('p'):
		if len(a.exHistory) == 0 {
			return
		}
		if p.histIdx == 0 {
			p.saved = p.input.String()
			p.histIdx = len(a.exHistory)
		}
		if p.histIdx > 0 {
			p.histIdx--
			p.input = newTextInput(a.exHistory[p.histIdx])
		}
		p.resetCompletion()
	case k.Is("down") || k.IsCtrl('n'):
		if p.histIdx == 0 {
			return
		}
		p.histIdx++
		if p.histIdx >= len(a.exHistory) {
			p.histIdx = 0
			p.input = newTextInput(p.saved)
		} else {
			p.input = newTextInput(a.exHistory[p.histIdx])
		}
		p.resetCompletion()
	default:
		if p.input.handle(k) {
			p.resetCompletion()
		}
	}
}

func (p *exPrompt) render(a *App, w, h int) string {
	th := a.theme
	width := min(w-4, max(50, w*2/3))
	inner := width - 2
	lines := []string{th.OverlayTitle.Render(":") + " " + p.input.render(th, inner-2)}
	if len(p.completions) > 1 {
		parts := []string{}
		shown := 0
		for i, c := range p.completions[:len(p.completions)-1] {
			if i == p.compIdx {
				parts = append(parts, th.SelectedFocus.Render(" "+c+" "))
			} else {
				parts = append(parts, th.Dim.Render(" "+c+" "))
			}
			shown++
			if lipgloss.Width(strings.Join(parts, "")) > inner-6 {
				break
			}
		}
		row := strings.Join(parts, "")
		if shown < len(p.completions)-1 {
			row += th.Muted.Render(" …")
		}
		lines = append(lines, truncateAnsi(row, inner))
	} else {
		lines = append(lines, th.Muted.Render("Tab completes · Shift+Tab back · ↑↓ history"))
	}
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}
