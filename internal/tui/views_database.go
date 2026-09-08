package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/zennotescli/internal/database"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// databaseView is a `.base` folder or loose `.csv` opened as a grid. The
// active saved view decides between a table and a board, its filters and
// sorts apply on top of the raw rows, and every edit writes the CSV and
// its schema back through the backend, so the desktop app sees the same
// database.
type databaseView struct {
	name    string
	csvPath string
	doc     *database.Doc
	err     error
	viewID  string

	// Derived from doc and the active view on every rebuild.
	rows   []database.Row
	fields []database.Field

	row, col        int
	scroll, hscroll int
	pageRows        int
	navKeys         []vim.Key
	colSpans        [][2]int
	viewSpans       []viewSpan
	widths          []int
	header          bool
	selected        map[string]bool

	raw       bool
	rawScroll int
	rawLines  []string

	board boardState
}

type viewSpan struct {
	x0, x1 int
	id     string
}

// dbGutter is the width of the marker column before the first cell.
const dbGutter = 3

func newDatabaseView(name string) *databaseView {
	return &databaseView{name: name, selected: map[string]bool{}}
}

func (v *databaseView) title() string      { return v.name }
func (v *databaseView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

// databaseNames lists the vault's databases by title.
func (a *App) databaseNames() []string {
	ops := a.backend.DatabaseOps()
	if ops == nil {
		return nil
	}
	list, err := ops.ListDatabases()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(list))
	for _, s := range list {
		names = append(names, s.Title)
	}
	sort.Strings(names)
	return names
}

func (a *App) databasePath(name string) string {
	ops := a.backend.DatabaseOps()
	if ops == nil {
		return ""
	}
	list, err := ops.ListDatabases()
	if err != nil {
		return ""
	}
	for _, s := range list {
		if s.Title == name || s.Path == name {
			return s.Path
		}
	}
	return ""
}

func (v *databaseView) refresh(a *App) {
	ops := a.backend.DatabaseOps()
	if ops == nil {
		v.err = fmt.Errorf("databases are not available on this backend")
		return
	}
	if v.csvPath == "" {
		v.csvPath = a.databasePath(v.name)
	}
	if v.csvPath == "" {
		v.err = fmt.Errorf("no database named %q", v.name)
		return
	}
	doc, err := ops.OpenDatabase(v.csvPath)
	if err != nil {
		v.err = err
		return
	}
	v.err = nil
	v.doc = doc
	v.name = doc.Title
	v.rebuild()
}

// activeView is the view the grid shows: the one picked in this tab, else
// the database's default.
func (v *databaseView) activeView() *database.View {
	if v.doc == nil {
		return nil
	}
	if view := v.doc.ViewByID(v.viewID); view != nil {
		return view
	}
	view := v.doc.ActiveView()
	if view != nil {
		v.viewID = view.ID
	}
	return view
}

func (v *databaseView) isBoard() bool {
	view := v.activeView()
	return view != nil && view.Type == "board"
}

// rebuild derives the visible rows and columns from the document and the
// active view without touching disk.
func (v *databaseView) rebuild() {
	doc := v.doc
	if doc == nil {
		return
	}
	view := v.activeView()
	rows := doc.Rows
	viewID := ""
	if view != nil {
		rows = database.FilterRows(rows, view.Filters, doc, view.FilterConjunction)
		rows = database.SortRows(rows, view.Sorts, doc)
		viewID = view.ID
	}
	v.rows = rows
	v.fields = doc.VisibleColumns(viewID)
	for id := range v.selected {
		if doc.RowByID(id) == nil {
			delete(v.selected, id)
		}
	}
	v.row = clampInt(v.row, 0, len(v.rows)-1)
	v.col = clampInt(v.col, 0, len(v.fields)-1)
	v.rawLines = nil
	if v.isBoard() {
		v.rebuildBoard()
	}
}

func clampInt(n, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// persist writes the schema and rows, then re-derives the grid while
// keeping the cursor on the row it was on.
func (v *databaseView) persist(a *App) bool {
	ops := a.backend.DatabaseOps()
	if ops == nil {
		return false
	}
	keep := v.currentRowID()
	flags := v.doc.PageHasContent
	doc, err := ops.WriteSchema(v.csvPath, v.doc)
	if err != nil {
		a.notifyError(err.Error())
		return false
	}
	doc.PageHasContent = flags
	v.doc = doc
	v.rebuild()
	v.focusRow(keep)
	return true
}

// currentRowID is the row under the cursor in either layout.
func (v *databaseView) currentRowID() string {
	if r := v.selectedRow(); r != nil {
		return r.ID
	}
	return ""
}

// focusRow moves the cursor to a row by id, in the table or on the board.
func (v *databaseView) focusRow(id string) {
	if id == "" {
		return
	}
	if v.isBoard() {
		for ci, c := range v.board.columns {
			for ri, r := range c.Rows {
				if r.ID == id {
					v.board.col, v.board.card = ci, ri
					return
				}
			}
		}
		return
	}
	for i, r := range v.rows {
		if r.ID == id {
			v.row = i
			return
		}
	}
}

func (v *databaseView) selectedRow() *database.Row {
	if v.isBoard() {
		return v.boardRow()
	}
	if v.row < len(v.rows) {
		return &v.rows[v.row]
	}
	return nil
}

// currentField is the column under the cursor; on a board, the title field.
func (v *databaseView) currentField() *database.Field {
	if v.doc == nil {
		return nil
	}
	if v.isBoard() {
		return v.doc.FieldByID(v.doc.TitleFieldID())
	}
	if v.col < len(v.fields) {
		return &v.fields[v.col]
	}
	return nil
}

// selectedIDs are the checked rows, else the current row.
func (v *databaseView) selectedIDs() []string {
	ids := []string{}
	if len(v.selected) > 0 {
		for _, r := range v.doc.Rows {
			if v.selected[r.ID] {
				ids = append(ids, r.ID)
			}
		}
		return ids
	}
	if r := v.selectedRow(); r != nil {
		ids = append(ids, r.ID)
	}
	return ids
}

// --- rendering ---

func (v *databaseView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	if v.doc == nil && v.err == nil {
		v.refresh(a)
	}
	if v.err != nil {
		return fitBlock(" "+th.StatusError.Render(v.err.Error()), w, h)
	}
	lines := []string{v.renderTitle(a, w)}
	v.pageRows = max(1, h-3)
	switch {
	case v.raw:
		lines = append(lines, v.renderRaw(a, w, h-1)...)
	case v.isBoard():
		lines = append(lines, strings.Split(v.renderBoard(a, w, h-1, focused), "\n")...)
	default:
		lines = append(lines, v.renderTable(a, w, h-1, focused)...)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

// renderTitle is the first line: the database name, its views as tabs, and
// the state of the active view on the right.
func (v *databaseView) renderTitle(a *App, w int) string {
	th := a.theme
	left := " " + th.Title.Render(v.doc.Title) + "  "
	x := cellWidth(v.doc.Title) + 3
	v.viewSpans = v.viewSpans[:0]
	for _, view := range v.doc.Views {
		glyph := "▤ "
		if view.Type == "board" {
			glyph = "▥ "
		}
		text := glyph + view.Name
		if view.ID == v.viewID {
			left += th.Bold.Foreground(th.Accent).Render(text)
		} else {
			left += th.Muted.Render(text)
		}
		v.viewSpans = append(v.viewSpans, viewSpan{x0: x, x1: x + cellWidth(text), id: view.ID})
		x += cellWidth(text) + 2
		left += "  "
	}
	stats := []string{fmt.Sprintf("%d rows", len(v.rows))}
	if n := len(v.selected); n > 0 {
		stats = append(stats, fmt.Sprintf("%d selected", n))
	}
	if view := v.activeView(); view != nil {
		if n := len(view.Filters); n > 0 {
			label := "filter"
			if n > 1 {
				label = "filters"
			}
			stats = append(stats, fmt.Sprintf("⌕ %d %s", n, label))
		}
		if len(view.Sorts) > 0 {
			if f := v.doc.FieldByID(view.Sorts[0].FieldID); f != nil {
				arrow := "↑"
				if view.Sorts[0].Direction == "desc" {
					arrow = "↓"
				}
				stats = append(stats, arrow+" "+f.Name)
			}
		}
		if view.Type == "table" {
			if hidden := v.doc.HiddenColumns(view.ID); len(hidden) > 0 {
				stats = append(stats, fmt.Sprintf("%d hidden", len(hidden)))
			}
		}
	}
	if v.raw {
		stats = []string{"raw CSV", v.csvPath}
	}
	right := th.Muted.Render(strings.Join(stats, " · ")) + " "
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return padRight(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// cellText is a cell's plain display text.
func (v *databaseView) cellText(f database.Field, raw string) string {
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r", ""), "\n", " ")
	switch f.Type {
	case "checkbox":
		if database.IsCheckboxTrue(raw) {
			return "☑"
		}
		return "☐"
	case "select":
		if strings.TrimSpace(raw) == "" {
			return ""
		}
		return "● " + strings.TrimSpace(raw)
	case "multiSelect":
		parts := []string{}
		for _, val := range database.SplitMultiSelect(raw) {
			parts = append(parts, "● "+val)
		}
		return strings.Join(parts, "  ")
	case "note", "noteMulti":
		links := database.SplitNoteLinks(raw)
		if len(links) == 0 {
			return strings.TrimSpace(raw)
		}
		return strings.Join(links, ", ")
	}
	return raw
}

func padLeftCells(s string, width int) string {
	w := cellWidth(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

// renderCell styles one table cell to exactly width cells.
func (v *databaseView) renderCell(th Theme, f database.Field, raw string, width int, cursor, rowActive bool) string {
	text := truncateCells(v.cellText(f, raw), width)
	if f.Type == "number" {
		text = padLeftCells(text, width)
	} else {
		text = padRight(text, width)
	}
	switch {
	case cursor:
		return th.SelectedFocus.Render(text)
	case rowActive:
		return th.Selected.Render(text)
	}
	switch f.Type {
	case "checkbox":
		return th.Checkbox.Render(text)
	case "select", "multiSelect":
		return v.renderChips(th, f, raw, width)
	case "note", "noteMulti":
		if len(database.SplitNoteLinks(raw)) > 0 {
			return lipgloss.NewStyle().Foreground(th.Blue).Render(text)
		}
	case "date":
		return th.Dim.Render(text)
	}
	return text
}

// renderChips paints select values as colored chips, dropping the ones
// that do not fit.
func (v *databaseView) renderChips(th Theme, f database.Field, raw string, width int) string {
	vals := []string{}
	if f.Type == "multiSelect" {
		vals = database.SplitMultiSelect(raw)
	} else if s := strings.TrimSpace(raw); s != "" {
		vals = []string{s}
	}
	out := ""
	used := 0
	for i, val := range vals {
		chip := "● " + val
		sep := 0
		if i > 0 {
			sep = 2
		}
		if used+sep+cellWidth(chip) > width {
			if i > 0 && used+sep+1 <= width {
				out += strings.Repeat(" ", sep) + th.Muted.Render("…")
				used += sep + 1
			} else if i == 0 {
				chip = truncateCells(chip, width)
				out += optionStyle(th, f, val).Render(chip)
				used += cellWidth(chip)
			}
			break
		}
		out += strings.Repeat(" ", sep) + optionStyle(th, f, val).Render(chip)
		used += sep + cellWidth(chip)
	}
	return out + strings.Repeat(" ", max(0, width-used))
}

// optionStyle colors a select value: its recorded palette token, else a
// stable color derived from the value so chips stay telling before anyone
// picks colors.
func optionStyle(th Theme, f database.Field, value string) lipgloss.Style {
	token := ""
	for _, o := range f.Options {
		if o.Value == value {
			token = o.Color
			break
		}
	}
	if token == "" {
		h := 0
		for _, r := range value {
			h = (h*31 + int(r)) % 1000003
		}
		token = database.OptionColors[h%len(database.OptionColors)]
	}
	return lipgloss.NewStyle().Foreground(optionColor(th, token))
}

// optionColor maps the desktop's palette tokens onto the theme.
func optionColor(th Theme, token string) lipgloss.Color {
	switch token {
	case "red":
		return th.Red
	case "orange":
		return th.Accent
	case "amber":
		return th.Yellow
	case "green":
		return th.Green
	case "teal":
		return th.Aqua
	case "sky", "blue":
		return th.Blue
	case "indigo", "violet":
		return th.Purple
	case "pink":
		return th.AccentSoft
	}
	return th.FgDim
}

func (v *databaseView) renderTable(a *App, w, h int, focused bool) []string {
	th := a.theme
	if len(v.fields) == 0 {
		return []string{" " + th.Muted.Render("No visible columns. A adds a field; V shows hidden columns.")}
	}
	view := v.activeView()
	sortDir := map[string]string{}
	filtered := map[string]bool{}
	if view != nil {
		for _, s := range view.Sorts {
			sortDir[s.FieldID] = s.Direction
		}
		for _, f := range view.Filters {
			filtered[f.FieldID] = true
		}
	}
	v.widths = make([]int, len(v.fields))
	for i, f := range v.fields {
		v.widths[i] = max(6, cellWidth(f.Name)+2)
		for _, r := range v.rows {
			v.widths[i] = max(v.widths[i], min(28, cellWidth(v.cellText(f, r.Cells[f.ID]))))
		}
	}
	v.col = clampInt(v.col, 0, len(v.fields)-1)
	if v.col < v.hscroll {
		v.hscroll = v.col
	}
	for {
		total := dbGutter
		for i := v.hscroll; i <= v.col; i++ {
			total += v.widths[i] + 3
		}
		if total <= w || v.hscroll >= v.col {
			break
		}
		v.hscroll++
	}
	head := strings.Repeat(" ", dbGutter)
	v.colSpans = v.colSpans[:0]
	cx := dbGutter
	for i := v.hscroll; i < len(v.fields); i++ {
		f := v.fields[i]
		v.colSpans = append(v.colSpans, [2]int{cx, cx + v.widths[i]})
		cx += v.widths[i] + 3
		marks := ""
		if d, ok := sortDir[f.ID]; ok {
			if d == "desc" {
				marks += "↓"
			} else {
				marks += "↑"
			}
		}
		if filtered[f.ID] {
			marks += "⌕"
		}
		room := v.widths[i]
		if marks != "" {
			room -= cellWidth(marks) + 1
		}
		text := truncateCells(f.Name, room)
		if marks != "" {
			text += " " + marks
		}
		text = padRight(text, v.widths[i])
		style := th.Bold
		switch {
		case v.header && i == v.col && focused:
			style = th.SelectedFocus
		case v.header && i == v.col:
			style = th.Selected
		case i == v.col && focused:
			style = th.Bold.Foreground(th.Accent)
		}
		head += style.Render(text) + th.Muted.Render(" │ ")
	}
	lines := []string{padRight(head, w), th.Muted.Render(strings.Repeat("─", w))}
	avail := max(1, h-2)
	if v.row < v.scroll {
		v.scroll = v.row
	}
	if v.row >= v.scroll+avail {
		v.scroll = v.row - avail + 1
	}
	for ri := v.scroll; ri < len(v.rows) && ri < v.scroll+avail; ri++ {
		r := v.rows[ri]
		marker := " "
		if ri == v.row && !v.header {
			marker = th.KeyHint.Render("▸")
		}
		check := " "
		if v.selected[r.ID] {
			check = th.KeyHint.Render("✓")
		}
		line := marker + check + " "
		for i := v.hscroll; i < len(v.fields); i++ {
			f := v.fields[i]
			cursor := focused && !v.header && ri == v.row && i == v.col
			line += v.renderCell(th, f, r.Cells[f.ID], v.widths[i], cursor, !v.header && ri == v.row) + th.Muted.Render(" │ ")
		}
		lines = append(lines, padRight(line, w))
	}
	if len(v.rows) == 0 {
		msg := "No rows. Press a to add one."
		if view != nil && len(view.Filters) > 0 {
			msg = "No rows match the filters. Press f to change them."
		}
		lines = append(lines, "   "+th.Muted.Render(msg))
	}
	return lines
}

func (v *databaseView) renderRaw(a *App, w, h int) []string {
	th := a.theme
	if v.rawLines == nil {
		text := strings.TrimRight(database.SerializeRows(v.doc.Rows, v.doc.Fields), "\n")
		v.rawLines = strings.Split(text, "\n")
	}
	v.rawScroll = clampInt(v.rawScroll, 0, max(0, len(v.rawLines)-h))
	lines := []string{}
	digits := len(fmt.Sprint(len(v.rawLines)))
	for i := v.rawScroll; i < len(v.rawLines) && len(lines) < h; i++ {
		num := th.LineNumber.Render(fmt.Sprintf("%*d ", digits, i+1))
		body := v.rawLines[i]
		if i == 0 {
			body = th.Bold.Render(body)
		}
		lines = append(lines, padRight(" "+num+body, w))
	}
	return lines
}

// --- keys ---

func (v *databaseView) handleKey(a *App, k vim.Key) bool {
	if v.doc == nil {
		return false
	}
	switch {
	case v.raw:
		return v.rawKey(a, k)
	case v.isBoard():
		return v.boardKey(a, k)
	case v.header:
		return v.headerKey(a, k)
	}
	return v.tableKey(a, k)
}

// commonKey handles the keys every layout shares.
func (v *databaseView) commonKey(a *App, k vim.Key) bool {
	switch {
	case k.IsRune('v'):
		v.nextView(a)
	case k.IsRune('V'):
		v.viewsMenu(a)
	case k.IsRune('f'):
		v.filtersMenu(a)
	case k.IsRune('r'):
		a.renameDatabasePrompt(v.doc.Title)
	case k.IsRune('R'):
		v.toggleRaw()
	case k.IsRune('A'):
		v.addFieldFlow(a)
	case k.IsRune(':'):
		a.openLocalEx()
	case k.IsRune('?'):
		a.openKeyHelp()
	default:
		return false
	}
	return true
}

func (v *databaseView) moveRow(delta int) {
	v.row = clampInt(v.row+delta, 0, len(v.rows)-1)
}

func (v *databaseView) moveCol(delta int) {
	v.col = clampInt(v.col+delta, 0, len(v.fields)-1)
}

func (v *databaseView) tableKey(a *App, k vim.Key) bool {
	switch {
	case k.Is("enter"):
		v.editCell(a)
		return true
	case k.Is("esc"):
		if len(v.selected) > 0 {
			v.selected = map[string]bool{}
			return true
		}
		return false
	case k.Is("down"):
		v.moveRow(1)
		return true
	case k.Is("up"):
		if v.row == 0 {
			v.header = true
		} else {
			v.moveRow(-1)
		}
		return true
	case k.Is("left") || k.Is("shift+tab"):
		v.moveCol(-1)
		return true
	case k.Is("right") || k.Is("tab"):
		v.moveCol(1)
		return true
	case k.IsCtrl('a'):
		v.selectAll()
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	if len(v.navKeys) > 0 {
		seq := append(v.navKeys, k)
		v.navKeys = nil
		if len(seq) == 2 {
			switch {
			case seq[0].IsRune('d') && seq[1].IsRune('d'):
				v.deleteRows(a)
			case seq[0].IsRune('g') && seq[1].IsRune('g'):
				v.row = 0
			case seq[0].IsRune('y') && seq[1].IsRune('y'):
				v.copyRows(a)
			}
		}
		return true
	}
	switch {
	case k.IsRune('j'):
		v.moveRow(1)
	case k.IsRune('k'):
		if v.row == 0 || len(v.rows) == 0 {
			v.header = true
		} else {
			v.moveRow(-1)
		}
	case k.IsRune('h'):
		v.moveCol(-1)
	case k.IsRune('l'):
		v.moveCol(1)
	case k.IsRune('G'):
		v.row = max(0, len(v.rows)-1)
	case k.IsRune('0') || k.IsRune('^'):
		v.col = 0
	case k.IsRune('$'):
		v.col = max(0, len(v.fields)-1)
	case k.IsCtrl('d'):
		v.moveRow(max(1, v.pageRows/2))
	case k.IsCtrl('u'):
		v.moveRow(-max(1, v.pageRows/2))
	case k.IsRune('d') || k.IsRune('g') || k.IsRune('y'):
		v.navKeys = []vim.Key{k}
	case k.IsRune('i') || k.IsRune('c') || k.IsRune('e'):
		v.editCell(a)
	case k.IsRune('x'):
		v.toggleSelect()
	case k.IsRune('a'):
		v.addRow(a)
	case k.IsRune('D'):
		v.duplicateRow(a)
	case k.IsRune('o'):
		v.openRecordPage(a)
	case k.IsRune('m'):
		v.rowMenu(a)
	case k.IsRune('s'):
		v.cycleSort(a, v.currentField())
	case k.IsRune('C'):
		v.colorFlow(a)
	default:
		return v.commonKey(a, k)
	}
	return true
}

func (v *databaseView) headerKey(a *App, k vim.Key) bool {
	f := v.currentField()
	switch {
	case k.Is("esc") || k.Is("down"):
		v.header = false
		return true
	case k.Is("enter"):
		v.renameFieldPrompt(a, f)
		return true
	case k.Is("left") || k.Is("shift+tab"):
		v.moveCol(-1)
		return true
	case k.Is("right") || k.Is("tab"):
		v.moveCol(1)
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	if len(v.navKeys) > 0 {
		seq := append(v.navKeys, k)
		v.navKeys = nil
		if len(seq) == 2 && seq[0].IsRune('d') && seq[1].IsRune('d') {
			v.deleteFieldFlow(a, f)
		}
		return true
	}
	switch {
	case k.IsRune('j'):
		v.header = false
	case k.IsRune('h'):
		v.moveCol(-1)
	case k.IsRune('l'):
		v.moveCol(1)
	case k.IsRune('0') || k.IsRune('^'):
		v.col = 0
	case k.IsRune('$'):
		v.col = max(0, len(v.fields)-1)
	case k.IsRune('i') || k.IsRune('c') || k.IsRune('e'):
		v.renameFieldPrompt(a, f)
	case k.IsRune('t'):
		v.typeMenu(a, f)
	case k.IsRune('H'):
		v.moveField(a, f, "left")
	case k.IsRune('L'):
		v.moveField(a, f, "right")
	case k.IsRune('x'):
		v.hideField(a, f)
	case k.IsRune('a'):
		v.addFieldFlow(a)
	case k.IsRune('d'):
		v.navKeys = []vim.Key{k}
	case k.IsRune('s'):
		v.cycleSort(a, f)
	case k.IsRune('f'):
		v.addFilterFlow(a, f)
	case k.IsRune('m'):
		v.fieldMenu(a, f)
	case k.IsRune('C'):
		v.colorFlow(a)
	case k.IsRune('o'):
		v.optionsSourceMenu(a, f)
	default:
		return v.commonKey(a, k)
	}
	return true
}

func (v *databaseView) rawKey(a *App, k vim.Key) bool {
	switch {
	case k.Is("esc") || k.IsRune('q') || k.IsRune('R'):
		v.raw = false
		return true
	case k.Is("down") || k.IsRune('j'):
		v.rawScroll++
	case k.Is("up") || k.IsRune('k'):
		v.rawScroll = max(0, v.rawScroll-1)
	case k.IsCtrl('d') || k.Is("pgdn"):
		v.rawScroll += max(1, v.pageRows/2)
	case k.IsCtrl('u') || k.Is("pgup"):
		v.rawScroll = max(0, v.rawScroll-max(1, v.pageRows/2))
	case k.IsRune('G'):
		v.rawScroll = max(0, len(v.rawLines)-1)
	case k.IsRune('g'):
		v.rawScroll = 0
	case k.IsRune('y'):
		a.copyText(strings.Join(v.rawLines, "\n") + "\n")
	case k.IsRune(':'):
		a.openLocalEx()
	case k.IsRune('?'):
		a.openKeyHelp()
	default:
		return false
	}
	return true
}

func (v *databaseView) toggleRaw() {
	v.raw = !v.raw
	v.rawLines = nil
	v.rawScroll = 0
}

// --- selection ---

func (v *databaseView) toggleSelect() {
	r := v.selectedRow()
	if r == nil {
		return
	}
	v.toggleSelectID(r.ID)
	if v.isBoard() {
		v.board.card = clampInt(v.board.card+1, 0, len(v.boardColumnRows())-1)
	} else {
		v.moveRow(1)
	}
}

func (v *databaseView) toggleSelectID(id string) {
	if v.selected == nil {
		v.selected = map[string]bool{}
	}
	if v.selected[id] {
		delete(v.selected, id)
	} else {
		v.selected[id] = true
	}
}

func (v *databaseView) selectAll() {
	if len(v.selected) == len(v.rows) && len(v.rows) > 0 {
		v.selected = map[string]bool{}
		return
	}
	v.selected = map[string]bool{}
	for _, r := range v.rows {
		v.selected[r.ID] = true
	}
}

// --- rows ---

func (v *databaseView) addRow(a *App) {
	titleID := v.doc.TitleFieldID()
	groupID, groupValue := "", ""
	if v.isBoard() && v.board.group != nil && v.board.col < len(v.board.columns) {
		if key := v.board.columns[v.board.col].Key; key != database.EmptyGroup {
			groupID, groupValue = v.board.group.ID, key
		}
	}
	a.promptFor("New row", "", "Title", func(a *App, text string) {
		row := v.doc.AddRow()
		if titleID != "" {
			v.doc.SetCell(row.ID, titleID, strings.TrimSpace(text))
		}
		if groupID != "" {
			v.doc.SetCell(row.ID, groupID, groupValue)
		}
		if v.persist(a) {
			v.selected = map[string]bool{}
			v.focusRow(row.ID)
		}
	})
}

func (v *databaseView) duplicateRow(a *App) {
	r := v.selectedRow()
	if r == nil {
		return
	}
	row, ok := v.doc.DuplicateRow(r.ID)
	if !ok {
		return
	}
	if v.persist(a) {
		v.focusRow(row.ID)
		a.notify("Duplicated " + v.doc.RecordTitle(row))
	}
}

func (v *databaseView) deleteRows(a *App) {
	ids := v.selectedIDs()
	if len(ids) == 0 {
		return
	}
	msg := fmt.Sprintf("Delete %d rows?", len(ids))
	if len(ids) == 1 {
		if r := v.doc.RowByID(ids[0]); r != nil {
			msg = "Delete row \"" + v.doc.RecordTitle(*r) + "\"?"
		}
	}
	a.confirm(msg, func() {
		for _, id := range ids {
			v.doc.DeleteRow(id)
		}
		v.selected = map[string]bool{}
		v.persist(a)
	})
}

// copyRows puts the checked rows (or the current one) on the clipboard as
// CSV with the header line.
func (v *databaseView) copyRows(a *App) {
	ids := v.selectedIDs()
	if len(ids) == 0 {
		return
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	rows := []database.Row{}
	for _, r := range v.doc.Rows {
		if want[r.ID] {
			rows = append(rows, r)
		}
	}
	a.copyText(database.SerializeRows(rows, v.doc.Fields))
}

// openRecordPage opens the row's page note, creating it on first use. A
// loose CSV has nowhere to keep pages, so it offers to become a folder.
func (v *databaseView) openRecordPage(a *App) {
	r := v.selectedRow()
	if r == nil {
		return
	}
	// Record pages live inside the `.base` folder, which the note index
	// leaves out, so existence is checked by reading the page itself.
	if page, ok := v.doc.Pages[r.ID]; ok {
		if _, err := a.openBuffer(page); err == nil {
			a.openNote(page, true)
			return
		}
	}
	if !database.HasRecordPages(v.csvPath) {
		rowID := r.ID
		v.convertToFolder(a, func(a *App) {
			v.focusRow(rowID)
			v.openRecordPage(a)
		})
		return
	}
	ops := a.backend.DatabaseOps()
	title := v.doc.RecordTitle(*r)
	body := v.doc.ComposePageBody(*r, "")
	path, err := ops.CreateRecordPage(v.csvPath, title, body)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if v.doc.Pages == nil {
		v.doc.Pages = map[string]string{}
	}
	v.doc.Pages[r.ID] = path
	if _, err := ops.WriteSchema(v.csvPath, v.doc); err != nil {
		a.notifyError(err.Error())
	}
	a.refreshIndex()
	a.notify("Created record page " + path)
	a.openNote(path, true)
}

// convertToFolder turns a loose CSV into a `.base` folder database after
// asking, then runs then.
func (v *databaseView) convertToFolder(a *App, then func(a *App)) {
	if !database.IsLooseCSVPath(v.csvPath) {
		if then != nil {
			then(a)
		}
		return
	}
	title := v.doc.Title
	a.confirm(fmt.Sprintf("Record pages need a folder database. Convert %s.csv to %s.base?", title, title), func() {
		ops := a.backend.DatabaseOps()
		next, err := ops.ConvertToFolder(v.csvPath)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		v.csvPath = next
		v.refresh(a)
		a.refreshIndex()
		a.notify("Converted to " + database.FormDirFromCSVPath(next))
		if then != nil && v.err == nil {
			then(a)
		}
	})
}

// --- sorting and views ---

// cycleSort steps a column through ascending, descending and unsorted,
// saving the result into the view.
func (v *databaseView) cycleSort(a *App, f *database.Field) {
	view := v.activeView()
	if f == nil || view == nil {
		return
	}
	next := "asc"
	if len(view.Sorts) > 0 && view.Sorts[0].FieldID == f.ID {
		switch view.Sorts[0].Direction {
		case "asc":
			next = "desc"
		case "desc":
			next = ""
		}
	}
	rules := []database.SortRule{}
	if next != "" {
		rules = append(rules, database.SortRule{FieldID: f.ID, Direction: next})
	}
	if err := v.doc.SetViewSorts(view.ID, rules); err != nil {
		a.notifyError(err.Error())
		return
	}
	if v.persist(a) {
		if next == "" {
			a.notify("Sort cleared")
		} else {
			a.notify("Sorted by " + f.Name + " " + next)
		}
	}
}

func (v *databaseView) switchView(a *App, id string) {
	if v.doc.ViewByID(id) == nil || id == v.viewID {
		return
	}
	v.viewID = id
	v.header = false
	v.col, v.hscroll = 0, 0
	if err := v.doc.SetActiveView(id); err == nil {
		v.persist(a)
	} else {
		v.rebuild()
	}
}

func (v *databaseView) nextView(a *App) {
	if len(v.doc.Views) < 2 {
		a.notify("One view. V adds a table or board view.")
		return
	}
	for i, view := range v.doc.Views {
		if view.ID == v.viewID {
			v.switchView(a, v.doc.Views[(i+1)%len(v.doc.Views)].ID)
			return
		}
	}
	v.switchView(a, v.doc.Views[0].ID)
}

// --- picker, creation, rename ---

func (a *App) openDatabasePicker() {
	names := a.databaseNames()
	if len(names) == 0 {
		a.notify("No databases yet. :dbnew <name> creates one.")
		return
	}
	items := []paletteItem{}
	for _, n := range names {
		items = append(items, paletteItem{label: n, id: n, detail: a.databasePath(n)})
	}
	a.overlay = &palette{title: "Databases", placeholder: "Database", items: items, filtered: items, onSelect: func(a *App, it paletteItem) { a.openDatabase(it.id) }}
}

func (a *App) createDatabase(name string) {
	ops := a.backend.DatabaseOps()
	if ops == nil {
		a.notifyError("Databases are not available on this backend")
		return
	}
	folder, sub := a.currentFolderContext()
	if folder != vault.FolderInbox {
		folder, sub = vault.FolderInbox, ""
	}
	doc, err := ops.CreateDatabase(folder, sub, name)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.notify("Created database " + doc.Title)
	a.refreshIndex()
	a.openDatabase(doc.Title)
}

func (a *App) renameDatabasePrompt(name string) {
	ops := a.backend.DatabaseOps()
	csvPath := a.databasePath(name)
	if ops == nil || csvPath == "" {
		return
	}
	a.promptFor("Rename database", name, "", func(a *App, next string) {
		next = strings.TrimSpace(next)
		if next == "" || next == name {
			return
		}
		if _, err := ops.RenameDatabase(csvPath, next); err != nil {
			a.notifyError(err.Error())
			return
		}
		for _, p := range a.panes.leaves() {
			for _, t := range p.tabs {
				if t.path == tabDatabase+name {
					t.path = tabDatabase + next
					if dv, ok := t.view.(*databaseView); ok {
						dv.name = next
						dv.csvPath = ""
						dv.refresh(a)
					}
				}
			}
		}
		a.notify("Renamed to " + next)
		a.refreshIndex()
	})
}

// --- hints and mouse geometry ---

func (v *databaseView) hintPairs(a *App) []hintPair {
	switch {
	case v.raw:
		return []hintPair{{"key:j/k", "scroll"}, {"key:y", "copy"}, {"key:R/Esc", "back to grid"}}
	case v.doc != nil && v.isBoard():
		return []hintPair{{"key:h/l j/k", "card"}, {"key:H/L", "move card"}, {"key:Enter", "page"}, {"key:i", "edit"}, {"key:a", "add card"}, {"key:A", "add column"}, {"key:x", "select"}, {"key:dd", "delete"}, {"key:m", "menu"}, {"key:gb", "group by"}, {"key:</>", "reorder"}, {"key:C", "color"}, {"key:f", "filter"}, {"key:v/V", "views"}}
	case v.header:
		return []hintPair{{"key:h/l", "column"}, {"key:i", "rename"}, {"key:t", "type"}, {"key:H/L", "move"}, {"key:x", "hide"}, {"key:dd", "delete"}, {"key:a", "add"}, {"key:s", "sort"}, {"key:f", "filter"}, {"key:m", "menu"}, {"key:j/Esc", "back"}}
	}
	return []hintPair{{"key:h/j/k/l", "cell"}, {"key:i/Enter", "edit"}, {"key:x", "select"}, {"key:a", "add row"}, {"key:dd", "delete"}, {"key:D", "duplicate"}, {"key:m", "menu"}, {"key:o", "page"}, {"key:s", "sort"}, {"key:f", "filter"}, {"key:v/V", "views"}, {"key:k↑", "header"}, {"key:A", "add field"}, {"key:R", "raw"}}
}

func (v *databaseView) scrollBy(a *App, delta int) {
	switch {
	case v.raw:
		v.rawScroll = max(0, v.rawScroll+delta)
	case v.isBoard():
		v.board.card = clampInt(v.board.card+delta, 0, len(v.boardColumnRows())-1)
	default:
		v.header = false
		v.moveRow(delta)
	}
}

func (v *databaseView) selectionPos(a *App, p *pane) (int, int, bool) {
	top := a.contentRect(p).y
	if v.isBoard() {
		x, y := v.boardCursorPos()
		return a.contentRect(p).x + x, top + y + 1, true
	}
	x := a.contentRect(p).x + dbGutter
	if i := v.col - v.hscroll; i >= 0 && i < len(v.colSpans) {
		x = a.contentRect(p).x + v.colSpans[i][0]
	}
	if v.header {
		return x, top + 2, true
	}
	return x, top + 3 + (v.row - v.scroll) + 1, true
}

func (v *databaseView) hintTargets(a *App, p *pane) []hintTarget {
	out := []hintTarget{}
	top := a.contentRect(p).y
	if v.raw || v.doc == nil {
		return out
	}
	if v.isBoard() {
		for ci, c := range v.board.columns {
			if ci < len(v.board.colSpans) {
				for ri := range c.Rows {
					x, y, ok := v.boardCardPos(ci, ri)
					if !ok {
						continue
					}
					col, card := ci, ri
					out = append(out, hintTarget{x: a.contentRect(p).x + x, y: top + y, run: func(a *App) { v.board.col, v.board.card = col, card }})
				}
			}
		}
		return out
	}
	for ri := v.scroll; ri < len(v.rows) && ri < v.scroll+v.pageRows; ri++ {
		idx := ri
		out = append(out, hintTarget{x: a.contentRect(p).x + 1, y: top + 3 + (ri - v.scroll), run: func(a *App) { v.header = false; v.row = idx }})
	}
	return out
}
