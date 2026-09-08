package tui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZenNotes/zennotescli/internal/database"
	"github.com/ZenNotes/zennotescli/internal/search"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

// Cell editing is type-aware: checkboxes flip, select fields open option
// pickers, note fields open the note picker, dates accept shorthand, and
// text keeps the `[[` note-link completion the editor has.

func (v *databaseView) editCell(a *App) {
	r := v.selectedRow()
	f := v.currentField()
	if r == nil || f == nil {
		return
	}
	rowID := r.ID
	field := *f
	current := r.Cells[f.ID]
	set := func(a *App, val string) { v.setCellValue(a, rowID, field, val) }
	switch f.Type {
	case "checkbox":
		next := "false"
		if !database.IsCheckboxTrue(current) {
			next = "true"
		}
		set(a, next)
	case "select":
		v.pickSelect(a, field, current, set)
	case "multiSelect":
		v.pickMulti(a, field, current, set)
	case "note":
		v.pickNote(a, field, current, set)
	case "noteMulti":
		v.pickNotes(a, field, current, set)
	case "date":
		a.datePrompt(f.Name, current, set)
	case "number":
		a.numberPrompt(f.Name, current, set)
	default:
		a.textPrompt(f.Name, current, "[[ links a note", set)
	}
}

// setCellValue writes a cell; select values mint options first.
func (v *databaseView) setCellValue(a *App, rowID string, f database.Field, val string) {
	if database.IsSelectType(f.Type) {
		for _, x := range database.SplitMultiSelect(val) {
			v.doc.EnsureSelectOption(f.ID, x)
		}
	}
	v.doc.SetCell(rowID, f.ID, strings.TrimSpace(val))
	v.persist(a)
}

// selectChoices are a field's options plus, when it draws from notes, the
// matching note titles.
func (a *App) selectChoices(f database.Field) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, o := range f.Options {
		if !seen[o.Value] {
			out = append(out, o.Value)
			seen[o.Value] = true
		}
	}
	for _, t := range a.optionsFromSource(f.OptionsSource) {
		if !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out
}

// optionsFromSource lists note titles for a select field's source.
func (a *App) optionsFromSource(src *database.OptionsSource) []string {
	if src == nil || a.idx == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	folder := strings.Trim(strings.ReplaceAll(src.Path, "\\", "/"), "/")
	for _, n := range a.idx.notes {
		if n.Folder == vault.FolderTrash {
			continue
		}
		switch src.Kind {
		case "folder":
			if folder != "" && !strings.HasPrefix(n.Path, folder+"/") {
				continue
			}
		case "tag":
			if !hasTag(n.Tags, src.Tag) {
				continue
			}
		}
		title := strings.TrimSpace(n.Title)
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		out = append(out, title)
	}
	sort.Strings(out)
	return out
}

func hasTag(tags []string, want string) bool {
	want = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(want), "#"))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimPrefix(t, "#"))
		if t == want || strings.HasPrefix(t, want+"/") {
			return true
		}
	}
	return false
}

const clearChoice = "\x00clear"

func (v *databaseView) pickSelect(a *App, f database.Field, current string, set func(a *App, val string)) {
	items := []paletteItem{}
	explicit := map[string]bool{}
	for _, o := range f.Options {
		explicit[o.Value] = true
	}
	for _, val := range a.selectChoices(f) {
		it := paletteItem{label: "● " + val, id: val}
		if !explicit[val] {
			it.detail = "from notes"
		}
		if val == strings.TrimSpace(current) {
			it.hint = "current"
		}
		items = append(items, it)
	}
	if strings.TrimSpace(current) != "" {
		items = append(items, paletteItem{label: "(clear)", id: clearChoice})
	}
	p := &palette{title: f.Name, placeholder: "Option; a new value creates it", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) {
		if it.id == clearChoice {
			set(a, "")
			return
		}
		set(a, it.id)
	}
	p.onEmpty = func(a *App, query string) { set(a, query) }
	p.emptyHint = "No option matches. Enter creates it."
	a.overlay = p
}

func (v *databaseView) pickMulti(a *App, f database.Field, current string, set func(a *App, val string)) {
	items := []pickItem{}
	for _, val := range a.selectChoices(f) {
		items = append(items, pickItem{id: val, label: val})
	}
	p := newPickOverlay(f.Name, "Filter options; Enter on a new value adds it", items, database.SplitMultiSelect(current))
	p.allowNew = true
	p.onDone = func(a *App, ids []string) { set(a, database.JoinMultiSelect(ids)) }
	p.refilter(a)
	a.overlay = p
}

// noteCandidates are the notes a link picker offers: recent notes first
// when there is no query, else the search matches.
func (a *App) noteCandidates(query, exclude string) []vault.NoteMeta {
	if a.idx == nil {
		return nil
	}
	var notes []vault.NoteMeta
	if strings.TrimSpace(query) == "" {
		for _, r := range a.recent {
			if meta, ok := a.noteMeta(r); ok && meta.Path != exclude {
				notes = append(notes, meta)
			}
		}
		for _, n := range a.idx.notes {
			if len(notes) >= 30 {
				break
			}
			if n.Folder != vault.FolderTrash && n.Path != exclude {
				notes = append(notes, n)
			}
		}
	} else {
		notes = search.Notes(a.idx.search, query, 40, false)
	}
	out := make([]vault.NoteMeta, 0, len(notes))
	seen := map[string]bool{}
	for _, n := range notes {
		if n.Path == exclude || seen[n.Path] {
			continue
		}
		seen[n.Path] = true
		out = append(out, n)
	}
	return out
}

// notePaletteSource feeds a palette with noteCandidates.
func (a *App) notePaletteSource(exclude string) func(a *App, query string) []paletteItem {
	return func(a *App, query string) []paletteItem {
		notes := a.noteCandidates(query, exclude)
		items := make([]paletteItem, 0, len(notes))
		for _, n := range notes {
			items = append(items, paletteItem{label: n.Title, detail: n.Path, id: n.Path, data: n})
		}
		return items
	}
}

func (v *databaseView) pickNote(a *App, f database.Field, current string, set func(a *App, val string)) {
	base := a.notePaletteSource("")
	p := &palette{title: f.Name, placeholder: "Note title (Enter links it)", highlight: true}
	p.source = func(a *App, query string) []paletteItem {
		items := base(a, query)
		if strings.TrimSpace(query) == "" && strings.TrimSpace(current) != "" {
			items = append([]paletteItem{{label: "(clear)", id: clearChoice}}, items...)
		}
		return items
	}
	p.onSelect = func(a *App, it paletteItem) {
		if it.id == clearChoice {
			set(a, "")
			return
		}
		set(a, database.JoinNoteLinks([]string{it.data.(vault.NoteMeta).Title}))
	}
	p.onEmpty = func(a *App, query string) { set(a, database.JoinNoteLinks([]string{query})) }
	p.emptyHint = "No note matches. Enter links the typed title anyway."
	p.refilter(a)
	a.overlay = p
}

func (v *databaseView) pickNotes(a *App, f database.Field, current string, set func(a *App, val string)) {
	p := newPickOverlay(f.Name, "Note title; Tab checks it, Enter saves", nil, database.SplitNoteLinks(current))
	p.source = func(a *App, query string) []pickItem {
		notes := a.noteCandidates(query, "")
		items := make([]pickItem, 0, len(notes))
		for _, n := range notes {
			items = append(items, pickItem{id: n.Title, label: n.Title, detail: n.Path})
		}
		return items
	}
	p.allowNew = true
	p.onDone = func(a *App, ids []string) { set(a, database.JoinNoteLinks(ids)) }
	p.refilter(a)
	a.overlay = p
}

// textPrompt is a prompt whose text may link notes with `[[`.
func (a *App) textPrompt(title, initial, help string, onSubmit func(a *App, text string)) {
	p := &prompt{title: title, input: newTextInput(initial), help: help, onSubmit: onSubmit}
	p.onLink = func(a *App, p *prompt) { a.linkIntoPrompt(p) }
	a.overlay = p
}

// linkIntoPrompt completes a `[[` typed into a prompt, then returns to it.
func (a *App) linkIntoPrompt(p *prompt) {
	pal := &palette{title: "Link to note", placeholder: "Note title", highlight: true}
	pal.source = a.notePaletteSource("")
	insert := func(a *App, title string) {
		p.input.insert(title + "]]")
		a.overlay = p
	}
	pal.onSelect = func(a *App, it paletteItem) { insert(a, it.data.(vault.NoteMeta).Title) }
	pal.onEmpty = func(a *App, query string) { insert(a, query) }
	pal.onCancel = func(a *App) { a.overlay = p }
	pal.emptyHint = "No note matches. Enter links the typed title anyway."
	pal.refilter(a)
	a.overlay = pal
}

// datePrompt asks for a date and normalizes shorthand before handing it on.
func (a *App) datePrompt(title, initial string, then func(a *App, val string)) {
	a.promptFor(title, initial, "YYYY-MM-DD · today · tomorrow · fri · +3d · -1w", func(a *App, text string) {
		val, ok := parseDateInput(text, time.Now())
		if !ok {
			a.notifyError("Not a date: " + strings.TrimSpace(text))
			a.datePrompt(title, text, then)
			return
		}
		then(a, val)
	})
}

// numberPrompt asks for a number, refusing anything that does not parse.
func (a *App) numberPrompt(title, initial string, then func(a *App, val string)) {
	a.promptFor(title, initial, "A number, or empty to clear", func(a *App, text string) {
		text = strings.TrimSpace(text)
		if text != "" {
			if _, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", ""), 64); err != nil {
				a.notifyError("Not a number: " + text)
				a.numberPrompt(title, text, then)
				return
			}
		}
		then(a, text)
	})
}
