package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/vim"
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

// Opening a record whose page file is already in the folder but not mapped
// (a workaround script wrote it) links that file instead of writing a
// `Dune 2.md` beside it.
func TestOpenRecordPageLinksAPageFileAlreadyThere(t *testing.T) {
	a, root := newDatabaseTestApp(t)
	v := openTestDatabase(t, a)
	next, err := a.backend.DatabaseOps().ConvertToFolder(v.csvPath)
	if err != nil {
		t.Fatal(err)
	}
	notes := "# Dune\n\nMy notes on Dune.\n"
	if err := os.WriteFile(filepath.Join(root, "Books.base", "Dune.md"), []byte(notes), 0o644); err != nil {
		t.Fatal(err)
	}
	v.csvPath = next
	v.refresh(a)
	var dune string
	for _, row := range v.doc.Rows {
		if v.doc.RecordTitle(row) == "Dune" {
			dune = row.ID
		}
	}
	v.focusRow(dune)
	v.openRecordPage(a)
	if _, err := os.Stat(filepath.Join(root, "Books.base", "Dune 2.md")); !os.IsNotExist(err) {
		t.Fatal("opening wrote a second page beside the one already there")
	}
	if got := v.doc.Pages[dune]; got != "Books.base/Dune.md" {
		t.Fatalf("Dune maps to %q", got)
	}
	if kept, _ := os.ReadFile(filepath.Join(root, "Books.base", "Dune.md")); string(kept) != notes {
		t.Fatalf("the page changed: %q", kept)
	}
}

// As + Row does in the app's grid, a row added while a view with picked
// rows is on screen joins that view's picks, or the app would leave it out
// of the view it was added in. Other views keep their picks, and a view of
// every row changes nothing.
func TestAddRowJoinsThePicksOfTheViewOnScreen(t *testing.T) {
	a, root := newDatabaseTestApp(t)
	v := openTestDatabase(t, a)
	ids := map[string]string{}
	for _, row := range v.doc.Rows {
		ids[v.doc.RecordTitle(row)] = row.ID
	}
	sidecarPath := filepath.Join(root, "Books.csv.base.json")
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	sidecar, err := database.ParseObject(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	views := []any{}
	for _, spec := range []string{
		`{"id":"shelf","name":"Shelf","type":"table","filters":[],"sorts":[],"pickedRowIds":["` + ids["Dune"] + `"]}`,
		`{"id":"all","name":"All","type":"table","filters":[],"sorts":[]}`,
		`{"id":"later","name":"Later","type":"table","filters":[],"sorts":[],"pickedRowIds":["` + ids["Neuromancer"] + `"]}`,
	} {
		view, err := database.ParseObject(spec)
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, view)
	}
	sidecar.Set("views", views)
	sidecar.Set("activeViewId", "shelf")
	text, err := database.Stringify(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecarPath, []byte(text+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v.refresh(a)
	v.render(a, 100, 20, true)

	add := func(title string) string {
		t.Helper()
		v.handleKey(a, vim.R('a'))
		typeText(a, title)
		a.overlay.handleKey(a, enterKey())
		for _, row := range v.doc.Rows {
			if v.doc.RecordTitle(row) == title {
				return row.ID
			}
		}
		t.Fatalf("a did not add %q", title)
		return ""
	}
	onDisk := func() map[string][]string {
		t.Helper()
		raw, err := os.ReadFile(sidecarPath)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Views []struct {
				ID           string    `json:"id"`
				PickedRowIDs *[]string `json:"pickedRowIds"`
			} `json:"views"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		out := map[string][]string{}
		for _, view := range schema.Views {
			if view.PickedRowIDs != nil {
				out[view.ID] = *view.PickedRowIDs
			}
		}
		return out
	}

	if v.activeView().ID != "shelf" {
		t.Fatalf("opened on %q", v.activeView().ID)
	}
	ulysses := add("Ulysses")
	want := map[string][]string{"shelf": {ids["Dune"], ulysses}, "later": {ids["Neuromancer"]}}
	if got := onDisk(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after an add on Shelf: %v", got)
	}

	v.switchView(a, "later")
	emma := add("Emma")
	want["later"] = []string{ids["Neuromancer"], emma}
	if got := onDisk(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after an add on Later: %v", got)
	}

	v.switchView(a, "all")
	add("Persuasion")
	if got := onDisk(); !reflect.DeepEqual(got, want) {
		t.Fatalf("an add on a view of every row changed picks: %v", got)
	}
}
