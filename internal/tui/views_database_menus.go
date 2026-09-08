package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/vault"
)

// Menus and multi-step flows: rows, fields, filters, views, colors.

func (v *databaseView) rowMenu(a *App) {
	r := v.selectedRow()
	if r == nil {
		return
	}
	id := r.ID
	n := len(v.selectedIDs())
	pageLabel := "Open record page"
	if _, has := v.doc.Pages[id]; !has {
		pageLabel = "Create record page"
	}
	if !database.HasRecordPages(v.csvPath) {
		pageLabel = "Convert to folder and open record page"
	}
	selectLabel := "Select row"
	if v.selected[id] {
		selectLabel = "Deselect row"
	}
	deleteLabel := "Delete row"
	if n > 1 {
		deleteLabel = fmt.Sprintf("Delete %d selected rows", n)
	}
	items := []menuItem{
		{key: "o", label: pageLabel, run: func(a *App) { v.openRecordPage(a) }},
		{key: "i", label: "Edit cell", run: func(a *App) { v.editCell(a) }},
		{key: "D", label: "Duplicate row", run: func(a *App) { v.duplicateRow(a) }},
		{key: "x", label: selectLabel, run: func(a *App) { v.toggleSelectID(id) }},
		{key: "y", label: "Copy as CSV", run: func(a *App) { v.copyRows(a) }},
		{sep: true},
		{key: "d", label: deleteLabel, run: func(a *App) { v.deleteRows(a) }},
	}
	a.showMenu(v.doc.RecordTitle(*r), items)
}

func (v *databaseView) fieldMenu(a *App, f *database.Field) {
	if f == nil {
		return
	}
	field := *f
	items := []menuItem{
		{key: "i", label: "Rename field", run: func(a *App) { v.renameFieldPrompt(a, &field) }},
		{key: "t", label: "Change type (" + database.FieldTypeLabel(field.Type) + ")", run: func(a *App) { v.typeMenu(a, &field) }},
		{key: "H", label: "Move left", run: func(a *App) { v.moveField(a, &field, "left") }},
		{key: "L", label: "Move right", run: func(a *App) { v.moveField(a, &field, "right") }},
		{key: "x", label: "Hide column", run: func(a *App) { v.hideField(a, &field) }},
	}
	if view := v.activeView(); view != nil && len(v.doc.HiddenColumns(view.ID)) > 0 {
		items = append(items, menuItem{key: "h", label: "Show hidden columns", run: func(a *App) { v.showHiddenFlow(a) }})
	}
	items = append(items,
		menuItem{key: "a", label: "Add field", run: func(a *App) { v.addFieldFlow(a) }},
		menuItem{sep: true},
		menuItem{key: "s", label: "Sort by this column", run: func(a *App) { v.cycleSort(a, &field) }},
		menuItem{key: "f", label: "Filter by this column", run: func(a *App) { v.addFilterFlow(a, &field) }},
	)
	if database.IsSelectType(field.Type) {
		items = append(items,
			menuItem{key: "o", label: "Options from: " + field.OptionsSource.Describe(), run: func(a *App) { v.optionsSourceMenu(a, &field) }},
			menuItem{key: "C", label: "Option colors", run: func(a *App) { v.colorFlow(a) }},
		)
	}
	items = append(items, menuItem{sep: true}, menuItem{key: "d", label: "Delete field", run: func(a *App) { v.deleteFieldFlow(a, &field) }})
	a.showMenu(field.Name, items)
}

var fieldTypeKeys = map[string]string{"text": "t", "number": "n", "checkbox": "c", "date": "d", "select": "s", "multiSelect": "m", "note": "l", "noteMulti": "L"}

// fieldTypeMenu lists the field types; then receives the pick.
func (a *App) fieldTypeMenu(title, current string, then func(a *App, typ string)) {
	items := []menuItem{}
	for _, t := range database.FieldTypes {
		typ := t
		label := database.FieldTypeLabel(t)
		if t == current {
			label += "  (current)"
		}
		items = append(items, menuItem{key: fieldTypeKeys[t], label: label, run: func(a *App) { then(a, typ) }})
	}
	a.showMenu(title, items)
}

func (v *databaseView) typeMenu(a *App, f *database.Field) {
	if f == nil {
		return
	}
	id := f.ID
	a.fieldTypeMenu("Type of "+f.Name, f.Type, func(a *App, typ string) {
		if err := v.doc.RetypeField(id, typ); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.persist(a)
	})
}

func (v *databaseView) renameFieldPrompt(a *App, f *database.Field) {
	if f == nil {
		return
	}
	id := f.ID
	a.promptFor("Rename field", f.Name, "", func(a *App, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if err := v.doc.RenameField(id, text); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.persist(a)
	})
}

func (v *databaseView) moveField(a *App, f *database.Field, direction string) {
	view := v.activeView()
	if f == nil || view == nil {
		return
	}
	id := f.ID
	if err := v.doc.MoveColumn(view.ID, id, direction); err != nil {
		a.notifyError(err.Error())
		return
	}
	if v.persist(a) {
		v.focusField(id)
	}
}

// focusField puts the column cursor on a field id when it is visible.
func (v *databaseView) focusField(id string) {
	for i, f := range v.fields {
		if f.ID == id {
			v.col = i
			return
		}
	}
}

func (v *databaseView) hideField(a *App, f *database.Field) {
	view := v.activeView()
	if f == nil || view == nil {
		return
	}
	if err := v.doc.SetFieldHidden(view.ID, f.ID, true); err != nil {
		a.notifyError(err.Error())
		return
	}
	name := f.Name
	if v.persist(a) {
		a.notify("Hid " + name + ". V, then h shows hidden columns.")
	}
}

// showHiddenFlow picks which columns the table view shows.
func (v *databaseView) showHiddenFlow(a *App) {
	view := v.activeView()
	if view == nil {
		return
	}
	viewID := view.ID
	items := []pickItem{}
	visible := []string{}
	shown := map[string]bool{}
	for _, f := range v.fields {
		shown[f.ID] = true
	}
	for _, f := range v.doc.Fields {
		if f.ID == v.doc.IDFieldID {
			continue
		}
		items = append(items, pickItem{id: f.ID, label: f.Name, detail: database.FieldTypeLabel(f.Type)})
		if shown[f.ID] {
			visible = append(visible, f.ID)
		}
	}
	p := newPickOverlay("Columns", "Tab shows or hides, Enter saves", items, visible)
	p.onDone = func(a *App, ids []string) {
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = true
		}
		for _, f := range v.doc.Fields {
			if f.ID == v.doc.IDFieldID {
				continue
			}
			if err := v.doc.SetFieldHidden(viewID, f.ID, !want[f.ID]); err != nil {
				a.notifyError(err.Error())
				return
			}
		}
		v.persist(a)
	}
	p.refilter(a)
	a.overlay = p
}

func (v *databaseView) deleteFieldFlow(a *App, f *database.Field) {
	if f == nil {
		return
	}
	if f.ID == v.doc.IDFieldID {
		a.notifyError("The id field cannot be deleted")
		return
	}
	id := f.ID
	a.confirm(fmt.Sprintf("Delete field \"%s\" and its values in %d rows?", f.Name, len(v.doc.Rows)), func() {
		if err := v.doc.DeleteField(id); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.header = false
		v.persist(a)
	})
}

// addFieldFlow asks for a name and a type, then appends the column.
func (v *databaseView) addFieldFlow(a *App) {
	a.promptFor("New field", "", "Name", func(a *App, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		a.fieldTypeMenu("Type of "+name, "text", func(a *App, typ string) {
			f, err := v.doc.AddField(name, typ)
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			if v.persist(a) {
				v.header = false
				v.focusField(f.ID)
				a.notify("Added " + f.Name)
			}
		})
	})
}

// optionsSourceMenu chooses where a select field's options come from.
func (v *databaseView) optionsSourceMenu(a *App, f *database.Field) {
	if f == nil || !database.IsSelectType(f.Type) {
		a.notify("Only select fields draw options from notes")
		return
	}
	id := f.ID
	apply := func(a *App, src *database.OptionsSource) {
		if err := v.doc.SetFieldOptionsSource(id, src); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.persist(a)
	}
	items := []menuItem{
		{key: "m", label: "Manual list", run: func(a *App) { apply(a, nil) }},
		{key: "n", label: "Every note's title", run: func(a *App) { apply(a, &database.OptionsSource{Kind: "notes"}) }},
		{key: "f", label: "Notes in a folder", run: func(a *App) {
			a.folderPalette("Options from folder", func(a *App, rel string) {
				apply(a, &database.OptionsSource{Kind: "folder", Path: rel})
			})
		}},
		{key: "t", label: "Notes with a tag", run: func(a *App) {
			a.tagPalette("Options from tag", func(a *App, tag string) {
				apply(a, &database.OptionsSource{Kind: "tag", Tag: tag})
			})
		}},
	}
	a.showMenu("Options for "+f.Name+" (now: "+f.OptionsSource.Describe()+")", items)
}

// folderPalette picks a vault folder by its vault-relative path.
func (a *App) folderPalette(title string, then func(a *App, rel string)) {
	items := []paletteItem{}
	if a.idx != nil {
		for _, f := range a.idx.folders {
			if f.Folder == vault.FolderTrash {
				continue
			}
			rel := a.vaultRelDir(f.Folder, f.Subpath)
			if rel == "" || isDatabaseDir(f.Subpath) {
				continue
			}
			items = append(items, paletteItem{label: rel, id: rel})
		}
	}
	p := &palette{title: title, placeholder: "Folder", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) { then(a, it.id) }
	p.onEmpty = func(a *App, query string) { then(a, strings.Trim(query, "/")) }
	p.emptyHint = "No folder matches. Enter uses the typed path."
	a.overlay = p
}

// vaultRelDir is the vault-relative directory of a bucket subpath.
func (a *App) vaultRelDir(folder vault.NoteFolder, sub string) string {
	sub = strings.Trim(sub, "/")
	if folder == vault.FolderInbox && a.primaryAtRoot() {
		return sub
	}
	var paths map[string]string
	if a.idx != nil {
		paths = a.idx.settings.SystemFolderPaths
	}
	base := strings.Trim(vault.ResolveFolderPath(folder, paths), "/")
	if sub == "" {
		return base
	}
	if base == "" {
		return sub
	}
	return base + "/" + sub
}

// tagPalette picks one of the vault's tags.
func (a *App) tagPalette(title string, then func(a *App, tag string)) {
	items := []paletteItem{}
	if a.idx != nil {
		for _, t := range a.idx.tags {
			items = append(items, paletteItem{label: "#" + t.tag, id: t.tag, detail: fmt.Sprintf("%d notes", t.count)})
		}
	}
	p := &palette{title: title, placeholder: "Tag", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) { then(a, it.id) }
	p.onEmpty = func(a *App, query string) { then(a, strings.TrimPrefix(strings.TrimSpace(query), "#")) }
	p.emptyHint = "No tag matches. Enter uses the typed tag."
	a.overlay = p
}

// --- colors ---

// colorFlow colors a select option: the cell's value in the grid, a column
// on a board, any option from the header.
func (v *databaseView) colorFlow(a *App) {
	if v.isBoard() {
		if v.board.group == nil || v.board.col >= len(v.board.columns) {
			return
		}
		key := v.board.columns[v.board.col].Key
		if key == database.EmptyGroup {
			a.notify("The empty column has no option to color")
			return
		}
		v.colorsMenu(a, *v.board.group, key)
		return
	}
	f := v.currentField()
	if f == nil || !database.IsSelectType(f.Type) {
		a.notify("Colors belong to select options. Move to a select column first.")
		return
	}
	field := *f
	values := []string{}
	if v.header {
		for _, o := range field.Options {
			values = append(values, o.Value)
		}
	} else if r := v.selectedRow(); r != nil {
		values = database.SplitMultiSelect(r.Cells[field.ID])
	}
	switch len(values) {
	case 0:
		a.notify("No option here to color")
	case 1:
		v.colorsMenu(a, field, values[0])
	default:
		items := []paletteItem{}
		for _, val := range values {
			items = append(items, paletteItem{label: "● " + val, id: val})
		}
		p := &palette{title: "Color which option?", placeholder: "Option", items: items, filtered: items}
		p.onSelect = func(a *App, it paletteItem) { v.colorsMenu(a, field, it.id) }
		a.overlay = p
	}
}

var colorKeys = map[string]string{"red": "r", "orange": "o", "amber": "a", "green": "g", "teal": "t", "sky": "s", "blue": "b", "indigo": "i", "violet": "v", "pink": "p"}

func (v *databaseView) colorsMenu(a *App, f database.Field, value string) {
	th := a.theme
	items := []menuItem{}
	current := ""
	for _, o := range f.Options {
		if o.Value == value {
			current = o.Color
		}
	}
	for _, tok := range database.OptionColors {
		token := tok
		label := lipgloss.NewStyle().Foreground(optionColor(th, tok)).Render("● " + tok)
		if tok == current {
			label += "  (current)"
		}
		items = append(items, menuItem{key: colorKeys[tok], label: label, run: func(a *App) { v.setOptionColor(a, f.ID, value, token) }})
	}
	items = append(items, menuItem{sep: true}, menuItem{key: "n", label: "Automatic", run: func(a *App) { v.setOptionColor(a, f.ID, value, "") }})
	a.showMenu("Color of "+value, items)
}

func (v *databaseView) setOptionColor(a *App, fieldID, value, token string) {
	v.doc.EnsureSelectOption(fieldID, value)
	if err := v.doc.SetOptionColor(fieldID, value, token); err != nil {
		a.notifyError(err.Error())
		return
	}
	v.persist(a)
}

// --- filters ---

func (v *databaseView) describeFilter(fl database.FilterRule) string {
	name := fl.FieldID
	if f := v.doc.FieldByID(fl.FieldID); f != nil {
		name = f.Name
	}
	text := name + " " + database.FilterOpLabel(fl.Op)
	if database.FilterNeedsValue(fl.Op) {
		text += " " + fl.Value
	}
	return text
}

func (v *databaseView) filtersMenu(a *App) {
	view := v.activeView()
	if view == nil {
		return
	}
	items := []menuItem{}
	for i, fl := range view.Filters {
		idx := i
		key := ""
		if i < 9 {
			key = fmt.Sprint(i + 1)
		}
		items = append(items, menuItem{key: key, label: v.describeFilter(fl), run: func(a *App) { v.filterItemMenu(a, idx) }})
	}
	if len(view.Filters) > 0 {
		items = append(items, menuItem{sep: true})
	}
	items = append(items, menuItem{key: "a", label: "Add filter", run: func(a *App) { v.addFilterFlow(a, nil) }})
	if len(view.Filters) > 1 {
		toggle := "Match any filter (now: all)"
		next := "or"
		if view.FilterConjunction == "or" {
			toggle = "Match all filters (now: any)"
			next = "and"
		}
		items = append(items, menuItem{key: "o", label: toggle, run: func(a *App) {
			v.saveFilters(a, view.Filters, next)
		}})
	}
	if len(view.Filters) > 0 {
		items = append(items, menuItem{key: "c", label: "Clear all filters", run: func(a *App) { v.saveFilters(a, nil, "and") }})
	}
	title := "Filters"
	if len(view.Filters) == 0 {
		title = "Filters (none)"
	}
	a.showMenu(title, items)
}

func (v *databaseView) saveFilters(a *App, filters []database.FilterRule, conjunction string) {
	view := v.activeView()
	if view == nil {
		return
	}
	if err := v.doc.SetViewFilters(view.ID, filters, conjunction); err != nil {
		a.notifyError(err.Error())
		return
	}
	v.persist(a)
}

func (v *databaseView) filterItemMenu(a *App, idx int) {
	view := v.activeView()
	if view == nil || idx >= len(view.Filters) {
		return
	}
	fl := view.Filters[idx]
	f := v.doc.FieldByID(fl.FieldID)
	items := []menuItem{}
	if f != nil {
		field := *f
		items = append(items,
			menuItem{key: "e", label: "Edit value", run: func(a *App) {
				v.filterValueInput(a, field, fl.Op, fl.Value, func(a *App, val string) {
					next := append([]database.FilterRule{}, view.Filters...)
					next[idx].Value = val
					v.saveFilters(a, next, view.FilterConjunction)
				})
			}},
			menuItem{key: "p", label: "Change operator", run: func(a *App) {
				v.filterOpMenu(a, field, func(a *App, op string) {
					v.filterValueInput(a, field, op, fl.Value, func(a *App, val string) {
						next := append([]database.FilterRule{}, view.Filters...)
						next[idx].Op, next[idx].Value = op, val
						v.saveFilters(a, next, view.FilterConjunction)
					})
				})
			}},
		)
	}
	items = append(items, menuItem{key: "d", label: "Remove filter", run: func(a *App) {
		next := append([]database.FilterRule{}, view.Filters[:idx]...)
		next = append(next, view.Filters[idx+1:]...)
		v.saveFilters(a, next, view.FilterConjunction)
	}})
	a.showMenu(v.describeFilter(fl), items)
}

// addFilterFlow adds a filter: field (unless given), operator, value.
func (v *databaseView) addFilterFlow(a *App, f *database.Field) {
	view := v.activeView()
	if view == nil {
		return
	}
	build := func(a *App, field database.Field) {
		v.filterOpMenu(a, field, func(a *App, op string) {
			v.filterValueInput(a, field, op, "", func(a *App, val string) {
				next := append([]database.FilterRule{}, view.Filters...)
				next = append(next, database.FilterRule{FieldID: field.ID, Op: op, Value: val})
				v.saveFilters(a, next, view.FilterConjunction)
			})
		})
	}
	if f != nil {
		build(a, *f)
		return
	}
	v.fieldPalette(a, "Filter by field", func(a *App, field database.Field) { build(a, field) })
}

// fieldPalette picks one of the database's fields (never the id).
func (v *databaseView) fieldPalette(a *App, title string, then func(a *App, f database.Field)) {
	items := []paletteItem{}
	for _, f := range v.doc.Fields {
		if f.ID == v.doc.IDFieldID {
			continue
		}
		items = append(items, paletteItem{label: f.Name, id: f.ID, detail: database.FieldTypeLabel(f.Type), data: f})
	}
	p := &palette{title: title, placeholder: "Field", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) { then(a, it.data.(database.Field)) }
	a.overlay = p
}

func (v *databaseView) filterOpMenu(a *App, f database.Field, then func(a *App, op string)) {
	items := []menuItem{}
	for i, op := range database.FilterOps(f.Type) {
		o := op
		items = append(items, menuItem{key: fmt.Sprint(i + 1), label: database.FilterOpLabel(op), run: func(a *App) { then(a, o) }})
	}
	a.showMenu(f.Name+" …", items)
}

// filterValueInput asks for a filter value the way the field's cells are
// edited, skipping the question for operators without one.
func (v *databaseView) filterValueInput(a *App, f database.Field, op, initial string, then func(a *App, val string)) {
	if !database.FilterNeedsValue(op) {
		then(a, "")
		return
	}
	switch f.Type {
	case "select", "multiSelect":
		items := []paletteItem{}
		for _, val := range a.selectChoices(f) {
			items = append(items, paletteItem{label: "● " + val, id: val})
		}
		p := &palette{title: f.Name + " " + database.FilterOpLabel(op), placeholder: "Option", items: items, filtered: items}
		p.onSelect = func(a *App, it paletteItem) { then(a, it.id) }
		p.onEmpty = func(a *App, query string) { then(a, query) }
		p.emptyHint = "No option matches. Enter uses the typed value."
		a.overlay = p
	case "date":
		a.datePrompt(f.Name+" "+database.FilterOpLabel(op), initial, then)
	case "number":
		a.numberPrompt(f.Name+" "+database.FilterOpLabel(op), initial, then)
	default:
		a.promptFor(f.Name+" "+database.FilterOpLabel(op), initial, "Value", func(a *App, text string) { then(a, strings.TrimSpace(text)) })
	}
}

// --- views ---

func (v *databaseView) viewsMenu(a *App) {
	view := v.activeView()
	items := []menuItem{}
	for i, vw := range v.doc.Views {
		id := vw.ID
		glyph := "▤ "
		if vw.Type == "board" {
			glyph = "▥ "
		}
		label := glyph + vw.Name
		if vw.ID == v.viewID {
			label += "  (current)"
		}
		key := ""
		if i < 9 {
			key = fmt.Sprint(i + 1)
		}
		items = append(items, menuItem{key: key, label: label, run: func(a *App) { v.switchView(a, id) }})
	}
	items = append(items, menuItem{sep: true},
		menuItem{key: "n", label: "New table view", run: func(a *App) { v.addView(a, "table") }},
		menuItem{key: "b", label: "New board view", run: func(a *App) { v.addView(a, "board") }},
		menuItem{key: "r", label: "Rename view", run: func(a *App) { v.renameViewPrompt(a) }},
	)
	if len(v.doc.Views) > 1 {
		items = append(items, menuItem{key: "d", label: "Delete view", run: func(a *App) { v.deleteViewFlow(a) }})
	}
	if view != nil {
		items = append(items, menuItem{sep: true})
		if view.Type == "table" {
			items = append(items, menuItem{key: "h", label: "Show or hide columns", run: func(a *App) { v.showHiddenFlow(a) }})
		} else {
			items = append(items,
				menuItem{key: "g", label: "Group by", run: func(a *App) { v.groupByFlow(a) }},
				menuItem{key: "c", label: "Card fields", run: func(a *App) { v.cardFieldsFlow(a) }},
			)
		}
		items = append(items, menuItem{key: "s", label: "Sort", run: func(a *App) { v.sortMenu(a) }})
		if len(view.Sorts) > 0 {
			items = append(items, menuItem{key: "S", label: "Clear sort", run: func(a *App) {
				if err := v.doc.SetViewSorts(view.ID, nil); err == nil {
					v.persist(a)
				}
			}})
		}
	}
	a.showMenu("Views", items)
}

func (v *databaseView) addView(a *App, typ string) {
	view, err := v.doc.AddView(typ)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	v.viewID = view.ID
	v.header = false
	v.col, v.hscroll = 0, 0
	if v.persist(a) {
		a.notify("Added " + view.Name + ". V, then r renames it.")
	}
}

func (v *databaseView) renameViewPrompt(a *App) {
	view := v.activeView()
	if view == nil {
		return
	}
	id := view.ID
	a.promptFor("Rename view", view.Name, "", func(a *App, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if err := v.doc.RenameView(id, text); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.persist(a)
	})
}

func (v *databaseView) deleteViewFlow(a *App) {
	view := v.activeView()
	if view == nil {
		return
	}
	id := view.ID
	a.confirm("Delete view \""+view.Name+"\"?", func() {
		if err := v.doc.RemoveView(id); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.viewID = v.doc.ActiveViewID
		v.header = false
		v.persist(a)
	})
}

// sortMenu picks a field and a direction for the view's sort.
func (v *databaseView) sortMenu(a *App) {
	view := v.activeView()
	if view == nil {
		return
	}
	viewID := view.ID
	v.fieldPalette(a, "Sort by", func(a *App, f database.Field) {
		apply := func(dir string) func(a *App) {
			return func(a *App) {
				rules := []database.SortRule{}
				if dir != "" {
					rules = append(rules, database.SortRule{FieldID: f.ID, Direction: dir})
				}
				if err := v.doc.SetViewSorts(viewID, rules); err != nil {
					a.notifyError(err.Error())
					return
				}
				v.persist(a)
			}
		}
		a.showMenu("Sort by "+f.Name, []menuItem{
			{key: "a", label: "Ascending", run: apply("asc")},
			{key: "d", label: "Descending", run: apply("desc")},
			{key: "c", label: "Clear sort", run: apply("")},
		})
	})
}

// groupByFlow points a board at a select field.
func (v *databaseView) groupByFlow(a *App) {
	view := v.activeView()
	if view == nil || view.Type != "board" {
		return
	}
	viewID := view.ID
	items := []paletteItem{}
	for _, f := range v.doc.Fields {
		if f.Type == "select" {
			items = append(items, paletteItem{label: f.Name, id: f.ID})
		}
	}
	if len(items) == 0 {
		a.notify("Boards group by a select field. A adds one.")
		return
	}
	p := &palette{title: "Group by", placeholder: "Select field", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) {
		if err := v.doc.SetGroupBy(viewID, it.id); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.board.col, v.board.card = 0, 0
		v.persist(a)
	}
	a.overlay = p
}

// cardFieldsFlow picks the fields a board card shows under its title.
func (v *databaseView) cardFieldsFlow(a *App) {
	view := v.activeView()
	if view == nil || view.Type != "board" {
		return
	}
	viewID := view.ID
	items := []pickItem{}
	checked := view.CardFieldIDs
	all := []string{}
	for _, f := range v.doc.Fields {
		if f.ID == v.doc.IDFieldID || f.ID == v.doc.TitleFieldID() {
			continue
		}
		items = append(items, pickItem{id: f.ID, label: f.Name, detail: database.FieldTypeLabel(f.Type)})
		all = append(all, f.ID)
	}
	if len(checked) == 0 {
		checked = all
	}
	p := newPickOverlay("Card fields", "Tab shows or hides a field on cards", items, checked)
	p.onDone = func(a *App, ids []string) {
		if err := v.doc.SetCardFields(viewID, ids); err != nil {
			a.notifyError(err.Error())
			return
		}
		v.persist(a)
	}
	p.refilter(a)
	a.overlay = p
}
