package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/search"
	"github.com/ZenNotes/tui/internal/vim"
)

// pickItem is one checkable entry of a pickOverlay.
type pickItem struct {
	id     string
	label  string
	detail string
}

// pickOverlay is a filterable list where several entries can be checked:
// multi-select cells, note-link lists, column visibility. Typing filters,
// Tab toggles the highlighted entry, Enter saves. With allowNew, Enter on
// a query that matches nothing adds the typed text as a checked entry.
type pickOverlay struct {
	title       string
	placeholder string
	input       textInput
	items       []pickItem
	source      func(a *App, query string) []pickItem
	checked     map[string]bool
	order       []string
	labels      map[string]string
	filtered    []pickItem
	cursor      int
	scroll      int
	allowNew    bool
	onDone      func(a *App, ids []string)
}

func newPickOverlay(title, placeholder string, items []pickItem, checked []string) *pickOverlay {
	p := &pickOverlay{title: title, placeholder: placeholder, items: items, checked: map[string]bool{}, labels: map[string]string{}}
	for _, it := range items {
		p.labels[it.id] = it.label
	}
	for _, id := range checked {
		p.check(id, id)
	}
	return p
}

func (p *pickOverlay) check(id, label string) {
	if !p.checked[id] {
		p.order = append(p.order, id)
	}
	p.checked[id] = true
	if _, ok := p.labels[id]; !ok {
		p.labels[id] = label
	}
}

func (p *pickOverlay) uncheck(id string) {
	delete(p.checked, id)
	kept := p.order[:0]
	for _, o := range p.order {
		if o != id {
			kept = append(kept, o)
		}
	}
	p.order = kept
}

// result lists the checked ids: in item order for static lists, in the
// order they were checked for source-backed ones, new entries last.
func (p *pickOverlay) result() []string {
	out := []string{}
	seen := map[string]bool{}
	if p.source == nil {
		for _, it := range p.items {
			if p.checked[it.id] {
				out = append(out, it.id)
				seen[it.id] = true
			}
		}
	}
	for _, id := range p.order {
		if p.checked[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

func (p *pickOverlay) refilter(a *App) {
	query := strings.TrimSpace(p.input.String())
	if p.source != nil {
		p.filtered = p.source(a, query)
	} else if query == "" {
		p.filtered = p.items
	} else {
		type scored struct {
			item  pickItem
			score float64
		}
		out := []scored{}
		for _, it := range p.items {
			if s := search.Fuzzy(query, it.label); s > 0 {
				out = append(out, scored{it, s})
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
		p.filtered = make([]pickItem, len(out))
		for i, s := range out {
			p.filtered[i] = s.item
		}
	}
	// Checked entries the filter no longer shows stay reachable at the top
	// of an empty query, so nothing checked is ever invisible.
	if query == "" && p.source != nil {
		present := map[string]bool{}
		for _, it := range p.filtered {
			present[it.id] = true
		}
		extra := []pickItem{}
		for _, id := range p.order {
			if p.checked[id] && !present[id] {
				extra = append(extra, pickItem{id: id, label: p.labels[id]})
			}
		}
		p.filtered = append(extra, p.filtered...)
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

func (p *pickOverlay) toggleCurrent() {
	if p.cursor >= len(p.filtered) {
		return
	}
	it := p.filtered[p.cursor]
	if p.checked[it.id] {
		p.uncheck(it.id)
	} else {
		p.check(it.id, it.label)
	}
}

func (p *pickOverlay) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc") || k.IsCtrl('c') || k.IsCtrl('['):
		a.overlay = nil
	case k.Is("enter"):
		query := strings.TrimSpace(p.input.String())
		if len(p.filtered) == 0 && query != "" && p.allowNew {
			p.check(query, query)
			p.input = newTextInput("")
			p.refilter(a)
			return
		}
		a.overlay = nil
		p.onDone(a, p.result())
	case k.Is("tab") && !k.Shift:
		p.toggleCurrent()
	case k.Is("down") || k.IsCtrl('n') || k.IsCtrl('j'):
		if len(p.filtered) > 0 {
			p.cursor = (p.cursor + 1) % len(p.filtered)
		}
	case k.Is("up") || k.IsCtrl('p') || k.IsCtrl('k') || k.Is("shift+tab"):
		if len(p.filtered) > 0 {
			p.cursor = (p.cursor - 1 + len(p.filtered)) % len(p.filtered)
		}
	default:
		if p.input.handle(k) {
			p.cursor = 0
			p.refilter(a)
		}
	}
}

func (p *pickOverlay) render(a *App, w, h int) string {
	th := a.theme
	width := min(w-4, max(44, w*2/3))
	inner := width - 2
	rows := min(len(p.filtered), max(3, h-11))
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+rows {
		p.scroll = p.cursor - rows + 1
	}
	lines := []string{th.OverlayTitle.Render(p.title)}
	prompt := "› "
	lines = append(lines, prompt+p.input.render(th, inner-cellWidth(prompt)))
	if p.input.String() == "" && p.placeholder != "" {
		lines[len(lines)-1] = prompt + th.Cursor.Render(" ") + th.Muted.Render(padRight(p.placeholder, inner-3))
	}
	lines = append(lines, th.Muted.Render(strings.Repeat("─", inner)))
	if len(p.filtered) == 0 {
		hint := "No matches"
		if p.allowNew && strings.TrimSpace(p.input.String()) != "" {
			hint = "No matches. Enter adds \"" + strings.TrimSpace(p.input.String()) + "\""
		}
		lines = append(lines, th.Muted.Render(padRight(truncateCells(hint, inner), inner)))
	}
	query := strings.TrimSpace(p.input.String())
	for i := p.scroll; i < p.scroll+rows && i < len(p.filtered); i++ {
		it := p.filtered[i]
		box := "☐"
		if p.checked[it.id] {
			box = "☑"
		}
		text := truncateCells(it.label, inner-6)
		if query != "" {
			text = highlightMatch(th, text, query)
		}
		if it.detail != "" {
			room := inner - 6 - cellWidth(it.label) - 2
			if room > 6 {
				text += "  " + th.Muted.Render(truncateCells(it.detail, room))
			}
		}
		row := box + " " + text
		if i == p.cursor {
			row = th.SelectedFocus.Render(padRight("▸ "+ansiStrip(row), inner))
		} else if p.checked[it.id] {
			row = "  " + th.KeyHint.Render(box) + " " + text
		} else {
			row = "  " + row
		}
		lines = append(lines, padRight(row, inner))
	}
	foot := fmt.Sprintf("%d checked", len(p.checked))
	if len(p.filtered) > rows {
		foot = fmt.Sprintf("%d of %d · %s", p.cursor+1, len(p.filtered), foot)
	}
	lines = append(lines, th.Muted.Render(strings.Repeat("─", inner)), th.KeyHint.Render("Tab")+th.Muted.Render(" toggle  ")+th.KeyHint.Render("Enter")+th.Muted.Render(" save  ")+th.KeyHint.Render("Esc")+th.Muted.Render(" cancel  ")+th.Muted.Render(foot))
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}
