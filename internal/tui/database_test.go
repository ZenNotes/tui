package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/config"
	"github.com/ZenNotes/zennotescli/internal/database"
	"github.com/ZenNotes/zennotescli/internal/keymaps"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

func enterKey() vim.Key { return vim.Key{Name: "enter"} }

func typeText(a *App, text string) {
	for _, r := range text {
		a.overlay.handleKey(a, vim.R(r))
	}
}

// newDatabaseTestApp builds an App over a temp vault holding one loose CSV.
func newDatabaseTestApp(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".zennotes"), 0o755); err != nil {
		t.Fatal(err)
	}
	csv := "Title,Status,Done\nDune,todo,false\nNeuromancer,done,true\n"
	if err := os.WriteFile(filepath.Join(root, "Books.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		backend:   b,
		keymap:    keymaps.NewResolver(nil),
		theme:     buildTheme(true),
		prefs:     prefsView{Prefs: config.Prefs{VimMode: true}},
		panes:     newPaneTree(),
		buffers:   map[string]*noteBuffer{},
		noteModes: map[string]paneMode{},
		width:     120,
		height:    40,
		ready:     true,
	}
	a.activePane = a.panes.leaves()[0]
	a.sidebar = newSidebar(a)
	return a, root
}

func openTestDatabase(t *testing.T, a *App) *databaseView {
	t.Helper()
	v := newDatabaseView("Books")
	v.refresh(a)
	if v.err != nil {
		t.Fatal(v.err)
	}
	return v
}

func fieldNames(v *databaseView) string {
	names := []string{}
	for _, f := range v.fields {
		names = append(names, f.Name)
	}
	return strings.Join(names, ",")
}

func TestLooseCSVOpensAsDatabase(t *testing.T) {
	a, root := newDatabaseTestApp(t)
	names := a.databaseNames()
	if len(names) != 1 || names[0] != "Books" {
		t.Fatalf("loose csv should be listed: %v", names)
	}
	v := openTestDatabase(t, a)
	if fieldNames(v) != "Title,Status,Done" || len(v.rows) != 2 {
		t.Fatalf("grid %s rows %d", fieldNames(v), len(v.rows))
	}
	if _, err := os.Stat(filepath.Join(root, "Books.csv.base.json")); err != nil {
		t.Fatal("opening a loose csv writes its sidecar next to it")
	}
	v.render(a, 100, 20, true)
	if !database.IsLooseCSVPath(v.csvPath) || database.HasRecordPages(v.csvPath) {
		t.Fatal("a loose csv has no record pages")
	}
}

func TestHeaderRowEditsFields(t *testing.T) {
	a, _ := newDatabaseTestApp(t)
	v := openTestDatabase(t, a)
	v.render(a, 100, 20, true)
	v.handleKey(a, vim.R('k'))
	if !v.header {
		t.Fatal("k on the first row enters the header")
	}
	v.handleKey(a, vim.R('L'))
	if fieldNames(v) != "Status,Title,Done" || v.col != 1 {
		t.Fatalf("L moves the column right: %s col %d", fieldNames(v), v.col)
	}
	v.handleKey(a, vim.R('t'))
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatal("t opens the type menu")
	}
	m.handleKey(a, vim.R('s'))
	if v.doc.FieldByID(v.fields[1].ID).Type != "select" {
		t.Fatal("s retypes the field to select")
	}
	v.handleKey(a, vim.R('x'))
	if fieldNames(v) != "Status,Done" {
		t.Fatalf("x hides the column: %s", fieldNames(v))
	}
	// Reopen from disk: the schema is persisted.
	v2 := openTestDatabase(t, a)
	if fieldNames(v2) != "Status,Done" || v2.doc.HiddenColumns(v2.viewID)[0].Name != "Title" {
		t.Fatalf("hidden column persists: %s", fieldNames(v2))
	}
	v.handleKey(a, vim.R('a'))
	typeText(a, "Pages")
	a.overlay.handleKey(a, enterKey())
	a.overlay.handleKey(a, vim.R('n'))
	if fieldNames(v) != "Status,Done,Pages" || v.fields[2].Type != "number" || v.col != 2 || v.header {
		t.Fatalf("a adds a typed field and lands on it: %s col %d header %v", fieldNames(v), v.col, v.header)
	}
	v.header = true
	v.handleKey(a, vim.R('d'))
	v.handleKey(a, vim.R('d'))
	a.overlay.handleKey(a, vim.R('y'))
	if fieldNames(v) != "Status,Done" {
		t.Fatalf("dd deletes the field after confirming: %s", fieldNames(v))
	}
}

func TestSelectCellPickerAndSelection(t *testing.T) {
	a, _ := newDatabaseTestApp(t)
	v := openTestDatabase(t, a)
	v.render(a, 100, 20, true)
	if err := v.doc.RetypeField(v.fields[1].ID, "select"); err != nil {
		t.Fatal(err)
	}
	v.persist(a)
	v.col = 1
	v.handleKey(a, enterKey())
	p, ok := a.overlay.(*palette)
	if !ok {
		t.Fatal("Enter on a select cell opens the option picker")
	}
	typeText(a, "reading")
	p.handleKey(a, enterKey())
	if v.rows[0].Cells[v.fields[1].ID] != "reading" {
		t.Fatalf("a new value is written: %q", v.rows[0].Cells[v.fields[1].ID])
	}
	found := false
	for _, o := range v.doc.FieldByID(v.fields[1].ID).Options {
		if o.Value == "reading" {
			found = true
		}
	}
	if !found {
		t.Fatal("a new select value mints an option")
	}
	v.col = 2
	v.handleKey(a, enterKey())
	if !database.IsCheckboxTrue(v.rows[0].Cells[v.fields[2].ID]) {
		t.Fatal("Enter on a checkbox toggles it")
	}
	v.row = 0
	v.handleKey(a, vim.R('x'))
	if !v.selected[v.rows[0].ID] || v.row != 1 {
		t.Fatal("x selects the row and moves down")
	}
	v.handleKey(a, vim.Key{Name: "esc"})
	if len(v.selected) != 0 {
		t.Fatal("Esc clears the selection")
	}
	v.handleKey(a, vim.R('D'))
	if len(v.rows) != 3 || v.rows[2].Cells[v.fields[0].ID] != "Neuromancer" || v.row != 2 {
		t.Fatalf("D duplicates the row after it: %d rows, cursor %d", len(v.rows), v.row)
	}
}

func TestFiltersSortsAndBoard(t *testing.T) {
	a, _ := newDatabaseTestApp(t)
	v := openTestDatabase(t, a)
	v.render(a, 100, 20, true)
	status := v.fields[1]
	if err := v.doc.RetypeField(status.ID, "select"); err != nil {
		t.Fatal(err)
	}
	v.persist(a)
	v.saveFilters(a, []database.FilterRule{{FieldID: status.ID, Op: "is", Value: "todo"}}, "and")
	if len(v.rows) != 1 || v.rows[0].Cells[v.fields[0].ID] != "Dune" {
		t.Fatalf("filter applies: %d rows", len(v.rows))
	}
	v.saveFilters(a, nil, "and")
	v.col = 0
	v.cycleSort(a, v.currentField())
	if v.rows[0].Cells[v.fields[0].ID] != "Dune" || v.activeView().Sorts[0].Direction != "asc" {
		t.Fatal("s sorts ascending")
	}
	v.cycleSort(a, v.currentField())
	if v.rows[0].Cells[v.fields[0].ID] != "Neuromancer" {
		t.Fatal("s again sorts descending")
	}
	v.cycleSort(a, v.currentField())
	if len(v.activeView().Sorts) != 0 {
		t.Fatal("s a third time clears the sort")
	}
	v.addView(a, "board")
	if !v.isBoard() || v.board.group == nil || v.board.group.ID != status.ID {
		t.Fatal("a board view groups by the select field")
	}
	if len(v.board.columns) != 3 || v.board.columns[2].Key != database.EmptyGroup {
		t.Fatalf("columns: %d", len(v.board.columns))
	}
	v.render(a, 120, 30, true)
	v.board.col, v.board.card = 0, 0
	dune := v.boardRow().ID
	v.handleKey(a, vim.R('L'))
	if v.doc.RowByID(dune).Cells[status.ID] != "done" || v.board.col != 1 || v.boardRow().ID != dune {
		t.Fatalf("L moves the card to the next column and follows it: col %d", v.board.col)
	}
	v.handleKey(a, vim.R('L'))
	if v.doc.RowByID(dune).Cells[status.ID] != "" {
		t.Fatal("moving into the empty column clears the cell")
	}
	v.handleKey(a, vim.R('A'))
	typeText(a, "later")
	a.overlay.handleKey(a, enterKey())
	if len(v.board.columns) != 4 || v.board.columns[v.board.col].Key != "later" {
		t.Fatalf("A adds a column and focuses it: %d", len(v.board.columns))
	}
	v.handleKey(a, vim.R('<'))
	if v.board.columns[1].Key != "later" {
		t.Fatalf("< moves the column left: %v", v.boardOrder())
	}
	v.handleKey(a, vim.R('v'))
	if v.isBoard() {
		t.Fatal("v cycles back to the table view")
	}
}

func TestConvertLooseCSVToFolder(t *testing.T) {
	a, root := newDatabaseTestApp(t)
	v := openTestDatabase(t, a)
	ops := a.backend.DatabaseOps()
	next, err := ops.ConvertToFolder(v.csvPath)
	if err != nil || next != "Books.base/data.csv" {
		t.Fatalf("convert: %q %v", next, err)
	}
	if _, err := os.Stat(filepath.Join(root, "Books.base", "schema.json")); err != nil {
		t.Fatal("the sidecar becomes schema.json")
	}
	if _, err := os.Stat(filepath.Join(root, "Books.csv")); !os.IsNotExist(err) {
		t.Fatal("the loose csv is gone")
	}
	list, _ := ops.ListDatabases()
	if len(list) != 1 || list[0].Path != next {
		t.Fatalf("one database after converting: %+v", list)
	}
	v.csvPath = next
	v.refresh(a)
	if v.err != nil || len(v.rows) != 2 || !database.HasRecordPages(v.csvPath) {
		t.Fatal("the folder database opens with its rows")
	}
}

func TestParseDateInput(t *testing.T) {
	now := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC) // a Tuesday
	cases := map[string]string{
		"": "", "today": "2026-09-08", "tomorrow": "2026-09-09", "yesterday": "2026-09-07",
		"+3d": "2026-09-11", "-1w": "2026-09-01", "+1m": "2026-10-08", "fri": "2026-09-11",
		"tue": "2026-09-15", "2026-9-8": "2026-09-08", "2026-12-01": "2026-12-01",
	}
	for in, want := range cases {
		got, ok := parseDateInput(in, now)
		if !ok || got != want {
			t.Errorf("%q: got %q ok %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := parseDateInput("someday", now); ok {
		t.Fatal("nonsense is rejected")
	}
}
