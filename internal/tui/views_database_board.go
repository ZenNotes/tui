package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/vim"
)

// boardState is the kanban layout of a board view: one column per option
// of the group-by field plus the empty column, cards three rows tall.
type boardState struct {
	columns   []database.BoardColumn
	group     *database.Field
	col, card int
	scroll    []int
	first     int
	colSpans  [][2]int
	colIdx    []int
	colW      int
	cardH     int
	avail     int
}

func (v *databaseView) rebuildBoard() {
	view := v.activeView()
	b := &v.board
	b.group = nil
	if view != nil && view.GroupByFieldID != "" {
		b.group = v.doc.FieldByID(view.GroupByFieldID)
	}
	if b.group == nil {
		for i := range v.doc.Fields {
			if v.doc.Fields[i].Type == "select" {
				b.group = &v.doc.Fields[i]
				break
			}
		}
	}
	if b.group == nil {
		b.columns = nil
		return
	}
	b.columns = database.BoardColumns(v.rows, *b.group, v.boardOrder())
	if len(b.scroll) != len(b.columns) {
		b.scroll = make([]int, len(b.columns))
	}
	b.col = clampInt(b.col, 0, len(b.columns)-1)
	b.card = clampInt(b.card, 0, len(v.boardColumnRows())-1)
	if b.cardH == 0 {
		b.cardH = 3
	}
}

// boardOrder is the column order: the view's saved order first, then any
// option it does not mention.
func (v *databaseView) boardOrder() []string {
	group := v.board.group
	if group == nil {
		return nil
	}
	known := map[string]bool{}
	for _, o := range group.Options {
		known[o.Value] = true
	}
	order := []string{}
	seen := map[string]bool{}
	if view := v.activeView(); view != nil {
		for _, key := range view.BoardColumnOrder {
			if known[key] && !seen[key] {
				order = append(order, key)
				seen[key] = true
			}
		}
	}
	for _, o := range group.Options {
		if !seen[o.Value] {
			order = append(order, o.Value)
			seen[o.Value] = true
		}
	}
	return order
}

func (v *databaseView) boardColumnRows() []database.Row {
	if v.board.col < len(v.board.columns) {
		return v.board.columns[v.board.col].Rows
	}
	return nil
}

func (v *databaseView) boardRow() *database.Row {
	rows := v.boardColumnRows()
	if v.board.card < len(rows) {
		return &rows[v.board.card]
	}
	return nil
}

// cardFields are the fields a card lists under its title.
func (v *databaseView) cardFields() []database.Field {
	view := v.activeView()
	titleID := v.doc.TitleFieldID()
	groupID := ""
	if v.board.group != nil {
		groupID = v.board.group.ID
	}
	out := []database.Field{}
	if view != nil && len(view.CardFieldIDs) > 0 {
		for _, id := range view.CardFieldIDs {
			if f := v.doc.FieldByID(id); f != nil && f.ID != titleID && f.ID != v.doc.IDFieldID && f.ID != groupID {
				out = append(out, *f)
			}
		}
		return out
	}
	for _, f := range v.doc.Fields {
		if f.ID == v.doc.IDFieldID || f.ID == titleID || f.ID == groupID {
			continue
		}
		out = append(out, f)
	}
	return out
}

func (v *databaseView) renderBoard(a *App, w, h int, focused bool) string {
	th := a.theme
	b := &v.board
	if b.group == nil {
		return " " + th.Muted.Render("Board views group by a select field. A adds one; gb picks it.")
	}
	n := len(b.columns)
	if n == 0 {
		return ""
	}
	colW := max(16, min(44, (w-(n-1))/n))
	visible := max(1, min(n, (w+1)/(colW+1)))
	if b.col < b.first {
		b.first = b.col
	}
	if b.col >= b.first+visible {
		b.first = b.col - visible + 1
	}
	b.first = clampInt(b.first, 0, max(0, n-visible))
	b.colW = colW
	b.cardH = 3
	b.avail = max(1, (h-2)/b.cardH)
	b.colSpans = b.colSpans[:0]
	b.colIdx = b.colIdx[:0]
	blocks := []string{}
	x := 0
	fields := v.cardFields()
	for ci := b.first; ci < n && ci < b.first+visible; ci++ {
		c := b.columns[ci]
		b.colSpans = append(b.colSpans, [2]int{x, x + colW})
		b.colIdx = append(b.colIdx, ci)
		x += colW + 1
		label := "● " + c.Key
		style := optionStyle(th, *b.group, c.Key)
		if c.Key == database.EmptyGroup {
			label = "No " + b.group.Name
			style = th.Muted
		}
		lead := " "
		if ci == b.col && focused {
			lead = th.KeyHint.Render("▸")
		}
		edge := ""
		if ci == b.first && b.first > 0 {
			edge = th.Muted.Render("‹")
		}
		if ci == b.first+visible-1 && ci < n-1 {
			edge = th.Muted.Render("›")
		}
		count := fmt.Sprintf(" (%d)", len(c.Rows))
		head := lead + style.Bold(true).Render(truncateCells(label, colW-cellWidth(count)-3)) + th.Muted.Render(count)
		head = padRight(head, colW-1) + edge
		lines := []string{padRight(head, colW), th.Muted.Render(strings.Repeat("─", colW))}
		if ci == b.col {
			if b.card < b.scroll[ci] {
				b.scroll[ci] = b.card
			}
			if b.card >= b.scroll[ci]+b.avail {
				b.scroll[ci] = b.card - b.avail + 1
			}
		}
		b.scroll[ci] = clampInt(b.scroll[ci], 0, max(0, len(c.Rows)-1))
		for ri := b.scroll[ci]; ri < len(c.Rows) && ri < b.scroll[ci]+b.avail; ri++ {
			r := c.Rows[ri]
			title := v.doc.RecordTitle(r)
			check := " "
			if v.selected[r.ID] {
				check = "✓"
			}
			cur := focused && ci == b.col && ri == b.card
			var first string
			if cur {
				first = th.SelectedFocus.Render(padRight(" "+check+" "+truncateCells(title, colW-4), colW))
			} else {
				first = " " + th.KeyHint.Render(check) + " " + th.Bold.Render(truncateCells(title, colW-4))
				if ci == b.col && ri == b.card {
					first = th.Selected.Render(padRight(" "+check+" "+truncateCells(title, colW-4), colW))
				}
			}
			parts := []string{}
			for _, f := range fields {
				val := v.cellText(f, r.Cells[f.ID])
				if strings.TrimSpace(val) == "" {
					continue
				}
				if f.Type == "checkbox" {
					parts = append(parts, val+" "+f.Name)
					continue
				}
				parts = append(parts, f.Name+": "+val)
			}
			second := "   " + th.Muted.Render(truncateCells(strings.Join(parts, " · "), colW-4))
			if _, has := v.doc.Pages[r.ID]; has && v.doc.PageHasContent[r.ID] {
				second = "   " + th.Muted.Render(truncateCells("📄 "+strings.Join(parts, " · "), colW-4))
			}
			lines = append(lines, padRight(first, colW), padRight(second, colW), strings.Repeat(" ", colW))
		}
		if len(c.Rows) == 0 && ci == b.col {
			lines = append(lines, "   "+th.Muted.Render(truncateCells("a adds a card here", colW-4)))
		}
		blocks = append(blocks, fitBlock(strings.Join(lines, "\n"), colW, h))
		if ci < n-1 && ci < b.first+visible-1 {
			blocks = append(blocks, a.renderVerticalBorder(h, false))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}

// boardCardPos is a card's position inside the pane, relative to the
// content area's top-left, when it is on screen.
func (v *databaseView) boardCardPos(ci, ri int) (int, int, bool) {
	b := &v.board
	for i, idx := range b.colIdx {
		if idx != ci {
			continue
		}
		if ci >= len(b.scroll) || ri < b.scroll[ci] || ri >= b.scroll[ci]+b.avail {
			return 0, 0, false
		}
		return b.colSpans[i][0] + 1, 3 + (ri-b.scroll[ci])*b.cardH, true
	}
	return 0, 0, false
}

func (v *databaseView) boardCursorPos() (int, int) {
	if x, y, ok := v.boardCardPos(v.board.col, v.board.card); ok {
		return x, y
	}
	for i, idx := range v.board.colIdx {
		if idx == v.board.col {
			return v.board.colSpans[i][0] + 1, 1
		}
	}
	return 1, 1
}

func (v *databaseView) boardKey(a *App, k vim.Key) bool {
	b := &v.board
	rows := v.boardColumnRows()
	switch {
	case k.Is("enter"):
		v.openRecordPage(a)
		return true
	case k.Is("esc"):
		if len(v.selected) > 0 {
			v.selected = map[string]bool{}
			return true
		}
		return false
	case k.Is("down"):
		b.card = clampInt(b.card+1, 0, len(rows)-1)
		return true
	case k.Is("up"):
		b.card = clampInt(b.card-1, 0, len(rows)-1)
		return true
	case k.Is("left") || k.Is("shift+tab"):
		v.boardMove(-1)
		return true
	case k.Is("right") || k.Is("tab"):
		v.boardMove(1)
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
				b.card = 0
			case seq[0].IsRune('g') && seq[1].IsRune('b'):
				v.groupByFlow(a)
			case seq[0].IsRune('y') && seq[1].IsRune('y'):
				v.copyRows(a)
			}
		}
		return true
	}
	switch {
	case k.IsRune('h'):
		v.boardMove(-1)
	case k.IsRune('l'):
		v.boardMove(1)
	case k.IsRune('j'):
		b.card = clampInt(b.card+1, 0, len(rows)-1)
	case k.IsRune('k'):
		b.card = clampInt(b.card-1, 0, len(rows)-1)
	case k.IsRune('G'):
		b.card = max(0, len(rows)-1)
	case k.IsRune('0') || k.IsRune('^'):
		b.col, b.card = 0, 0
	case k.IsRune('$'):
		b.col, b.card = max(0, len(b.columns)-1), 0
	case k.IsCtrl('d'):
		b.card = clampInt(b.card+max(1, b.avail/2), 0, len(rows)-1)
	case k.IsCtrl('u'):
		b.card = clampInt(b.card-max(1, b.avail/2), 0, len(rows)-1)
	case k.IsRune('H'):
		v.moveCard(a, -1)
	case k.IsRune('L'):
		v.moveCard(a, 1)
	case k.IsRune('<'):
		v.moveBoardColumn(a, -1)
	case k.IsRune('>'):
		v.moveBoardColumn(a, 1)
	case k.IsRune('d') || k.IsRune('g') || k.IsRune('y'):
		v.navKeys = []vim.Key{k}
	case k.IsRune('i') || k.IsRune('c') || k.IsRune('e'):
		v.editCell(a)
	case k.IsRune('x'):
		v.toggleSelect()
	case k.IsRune('a'):
		v.addRow(a)
	case k.IsRune('A'):
		v.addBoardColumn(a)
	case k.IsRune('D'):
		v.duplicateRow(a)
	case k.IsRune('o'):
		v.openRecordPage(a)
	case k.IsRune('m'):
		v.rowMenu(a)
	case k.IsRune('s'):
		v.sortMenu(a)
	case k.IsRune('C'):
		v.colorFlow(a)
	default:
		return v.commonKey(a, k)
	}
	return true
}

func (v *databaseView) boardMove(delta int) {
	b := &v.board
	b.col = clampInt(b.col+delta, 0, len(b.columns)-1)
	b.card = clampInt(b.card, 0, len(v.boardColumnRows())-1)
}

// moveCard puts the current card in the neighboring column by writing its
// group cell; the empty column clears it.
func (v *databaseView) moveCard(a *App, delta int) {
	b := &v.board
	r := v.boardRow()
	if r == nil || b.group == nil {
		return
	}
	target := b.col + delta
	if target < 0 || target >= len(b.columns) {
		return
	}
	key := b.columns[target].Key
	val := key
	if key == database.EmptyGroup {
		val = ""
	}
	v.doc.SetCell(r.ID, b.group.ID, val)
	v.persist(a)
}

// moveBoardColumn swaps the current column with its neighbor in the view's
// saved order; the empty column stays last.
func (v *databaseView) moveBoardColumn(a *App, delta int) {
	view := v.activeView()
	b := &v.board
	if view == nil || b.col >= len(b.columns) {
		return
	}
	key := b.columns[b.col].Key
	if key == database.EmptyGroup {
		return
	}
	order := v.boardOrder()
	i := -1
	for idx, k := range order {
		if k == key {
			i = idx
		}
	}
	j := i + delta
	if i < 0 || j < 0 || j >= len(order) {
		return
	}
	order[i], order[j] = order[j], order[i]
	if err := v.doc.SetBoardColumnOrder(view.ID, order); err != nil {
		a.notifyError(err.Error())
		return
	}
	if v.persist(a) {
		v.focusBoardColumn(key)
	}
}

func (v *databaseView) focusBoardColumn(key string) {
	for i, c := range v.board.columns {
		if c.Key == key {
			v.board.col = i
			v.board.card = clampInt(v.board.card, 0, len(c.Rows)-1)
			return
		}
	}
}

// addBoardColumn mints an option of the group field, which is a column.
func (v *databaseView) addBoardColumn(a *App) {
	view := v.activeView()
	b := &v.board
	if view == nil || b.group == nil {
		a.notify("Boards group by a select field. A adds one in a table view; gb picks it here.")
		return
	}
	groupID := b.group.ID
	viewID := view.ID
	a.promptFor("New column", "", "Option of "+b.group.Name, func(a *App, text string) {
		name := strings.ReplaceAll(strings.TrimSpace(text), ",", " ")
		if name == "" {
			return
		}
		v.doc.EnsureSelectOption(groupID, name)
		if vw := v.doc.ViewByID(viewID); vw != nil && len(vw.BoardColumnOrder) > 0 {
			order := append(append([]string{}, v.boardOrder()...), name)
			if err := v.doc.SetBoardColumnOrder(viewID, order); err != nil {
				a.notifyError(err.Error())
				return
			}
		}
		if v.persist(a) {
			v.focusBoardColumn(name)
		}
	})
}

// boardClick maps a click inside the board to a column or card.
func (v *databaseView) boardClick(a *App, x, y int, m tea.MouseMsg, double bool) {
	b := &v.board
	right := m.Button == tea.MouseButtonRight
	ci := -1
	for i, span := range b.colSpans {
		if x >= span[0] && x < span[1]+1 {
			ci = b.colIdx[i]
		}
	}
	if ci < 0 {
		return
	}
	if y <= 2 {
		b.col = ci
		b.card = clampInt(b.card, 0, len(v.boardColumnRows())-1)
		if right {
			v.boardColumnMenu(a)
		}
		return
	}
	ri := b.scroll[ci] + (y-3)/max(1, b.cardH)
	if ri >= len(b.columns[ci].Rows) {
		b.col = ci
		b.card = clampInt(b.card, 0, len(v.boardColumnRows())-1)
		return
	}
	b.col, b.card = ci, ri
	if right {
		v.rowMenu(a)
		return
	}
	if double {
		v.openRecordPage(a)
	}
}

// boardColumnMenu is the right-click menu of a column header.
func (v *databaseView) boardColumnMenu(a *App) {
	b := &v.board
	if b.col >= len(b.columns) {
		return
	}
	key := b.columns[b.col].Key
	items := []menuItem{
		{key: "a", label: "Add card here", run: func(a *App) { v.addRow(a) }},
		{key: "A", label: "Add column", run: func(a *App) { v.addBoardColumn(a) }},
	}
	if key != database.EmptyGroup {
		items = append(items,
			menuItem{key: "C", label: "Color", run: func(a *App) { v.colorFlow(a) }},
			menuItem{key: "<", label: "Move left", run: func(a *App) { v.moveBoardColumn(a, -1) }},
			menuItem{key: ">", label: "Move right", run: func(a *App) { v.moveBoardColumn(a, 1) }},
		)
	}
	items = append(items, menuItem{sep: true}, menuItem{key: "g", label: "Group by another field", run: func(a *App) { v.groupByFlow(a) }})
	title := key
	if key == database.EmptyGroup && b.group != nil {
		title = "No " + b.group.Name
	}
	a.showMenu(title, items)
}
