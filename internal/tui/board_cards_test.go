package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/vim"
)

// The board of ZenNotes/zennotes#1122: task notes with a `status:` each,
// custom status columns, and a checkbox list.
func boardTestApp(t *testing.T, notes map[string]string) (*App, *tasksView) {
	t.Helper()
	a, _ := linkTestApp(t, notes)
	a.prefs.KanbanGroupBy = "field:status"
	a.prefs.KanbanStatuses = []string{"backlog", "open", "bezig", "review", "klaar"}
	a.prefs.KanbanCardTitleLines = 1
	reloadBoardTasks(t, a)
	if err := a.openTasksMode("kanban"); err != nil {
		t.Fatal(err)
	}
	v, ok := a.activeTab().view.(*tasksView)
	if !ok {
		t.Fatal("the Tasks view did not open")
	}
	return a, v
}

func reloadBoardTasks(t *testing.T, a *App) {
	t.Helper()
	im := a.loadIndexCmd()().(indexLoadedMsg)
	tm := a.loadTasksCmd()().(tasksLoadedMsg)
	if im.err != nil || tm.err != nil {
		t.Fatal(im.err, tm.err)
	}
	im.idx.tasks = tm.tasks
	a.idx = im.idx
	a.refreshViews()
}

func taskNote(status string, extra ...string) string {
	fm := "---\ntags: [task]\ntasks: note\n"
	if status != "" {
		fm += "status: " + status + "\n"
	}
	return fm + strings.Join(extra, "") + "---\n\nBody.\n"
}

func boardColumnIDs(cols []kanbanColumn) []string {
	ids := []string{}
	for _, c := range cols {
		ids = append(ids, c.id)
	}
	return ids
}

// "No status" is a real column: a task note that names no status and a
// checkbox that inherits none both land in it.
func TestBoardNoStatusColumnTakesCards(t *testing.T) {
	_, v := boardTestApp(t, map[string]string{
		"Unsorted.md":  taskNote(""),
		"Checklist.md": "- [ ] Plain checkbox\n",
		"Review.md":    taskNote("review"),
	})
	var none *kanbanColumn
	for i := range v.columns {
		if v.columns[i].id == "__none__" {
			none = &v.columns[i]
		}
	}
	if none == nil || len(none.cards) != 2 {
		t.Fatalf("No status holds %v, want the task note and the checkbox", none)
	}
}

func TestBoardHidesEmptyColumns(t *testing.T) {
	a, v := boardTestApp(t, map[string]string{"A.md": taskNote("open"), "B.md": taskNote("review")})
	all := []string{"backlog", "open", "bezig", "review", "klaar", "__none__"}
	if got := boardColumnIDs(v.columns); !reflect.DeepEqual(got, all) {
		t.Fatalf("columns %v, want %v", got, all)
	}
	a.prefs.KanbanHideEmptyColumns = true
	v.refresh(a)
	if got, want := boardColumnIDs(v.columns), []string{"open", "review"}; !reflect.DeepEqual(got, want) {
		t.Errorf("with empty columns hidden: %v, want %v", got, want)
	}
	if got := boardColumnIDs(v.allColumns); !reflect.DeepEqual(got, all) {
		t.Errorf("the hidden columns are gone from the board itself: %v", got)
	}
	// A filter that leaves a column without cards hides it too.
	v.filter = "nothing matches this"
	v.refresh(a)
	if len(v.columns) != 0 {
		t.Errorf("columns under a filter that matches nothing: %v", boardColumnIDs(v.columns))
	}
	if out := v.renderBoard(a, 100, 10, true); !strings.Contains(out, "Empty columns are hidden") {
		t.Errorf("an all-hidden board does not say why it is blank:\n%s", out)
	}
}

// L from "review" reaches "klaar" while nothing is in it yet, and the
// selection follows the card's column once the vault has been read again.
func TestBoardMovesACardIntoAHiddenColumn(t *testing.T) {
	a, v := boardTestApp(t, map[string]string{"A.md": taskNote("open"), "B.md": taskNote("review")})
	root := a.opts.Target.Root
	a.prefs.KanbanHideEmptyColumns = true
	v.refresh(a)
	v.col, v.card = 1, 0
	v.handleKey(a, vim.Key{Rune: 'L'})
	body, err := os.ReadFile(filepath.Join(root, "B.md"))
	if err != nil || !strings.Contains(string(body), "status: klaar") {
		t.Fatalf("L did not move the card to the next column of the board: %q, %v", body, err)
	}
	reloadBoardTasks(t, a)
	if got, want := boardColumnIDs(v.columns), []string{"open", "klaar"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("columns after the move: %v, want %v", got, want)
	}
	if v.columns[v.col].id != "klaar" {
		t.Errorf("the selection is on %q, want the column the card went to", v.columns[v.col].id)
	}
	// H walks back the same way, through the hidden "review".
	v.handleKey(a, vim.Key{Rune: 'H'})
	body, _ = os.ReadFile(filepath.Join(root, "B.md"))
	if !strings.Contains(string(body), "status: review") {
		t.Errorf("H did not move the card back one column: %q", body)
	}
}

// An index reload carries no tasks; they follow in a message of their own.
// The board keeps what it shows in between, or every column would hide.
func TestIndexReloadKeepsTheTasksOnScreen(t *testing.T) {
	a, v := boardTestApp(t, map[string]string{"A.md": taskNote("open"), "B.md": taskNote("review")})
	a.prefs.KanbanHideEmptyColumns = true
	v.refresh(a)
	v.col = 1
	a.Update(a.loadIndexCmd()())
	if len(a.idx.tasks) != 2 || len(v.columns) != 2 || v.columns[v.col].id != "review" {
		t.Errorf("after an index reload: %d tasks, columns %v, selected %d", len(a.idx.tasks), boardColumnIDs(v.columns), v.col)
	}
}

func TestTaskNoteCardDoesNotRepeatItsName(t *testing.T) {
	a, v := boardTestApp(t, map[string]string{
		"Risico analyse.md": taskNote("open"),
		"Met datum.md":      taskNote("open", "due: 2099-01-02\n"),
		"Checklist.md":      "- [ ] Bel de klant @status:open\n",
	})
	faces := map[string]cardFace{}
	for _, c := range v.tasks {
		faces[c.NoteTitle] = v.cardFace(a, c, 30)
	}
	if f := faces["Risico analyse"]; f.height() != 1 || f.title[0] != "Risico analyse" {
		t.Errorf("a task note's card: %d rows, %q", f.height(), f.title)
	}
	if f := faces["Met datum"]; f.height() != 2 || f.metaPlain != "2099-01-02" {
		t.Errorf("a dated task note's card: %d rows, second row %q", f.height(), f.metaPlain)
	}
	if f := faces["Checklist"]; f.height() != 2 || f.title[0] != "Bel de klant" || f.metaPlain != "Checklist" {
		t.Errorf("a checkbox card names its note: %d rows, %q / %q", f.height(), f.title, f.metaPlain)
	}
}

func TestCardTitleWraps(t *testing.T) {
	for _, tt := range []struct {
		text  string
		width int
		lines int
		want  []string
	}{
		{"Short", 10, 3, []string{"Short"}},
		{"Klantgesprek voorbereiden", 18, 1, []string{"Klantgesprek voor…"}},
		{"Klantgesprek voorbereiden", 18, 2, []string{"Klantgesprek", "voorbereiden"}},
		{"Plain checkbox without status at all", 14, 2, []string{"Plain checkbox", "without statu…"}},
		{"Plain checkbox without status at all", 14, 3, []string{"Plain checkbox", "without status", "at all"}},
		{"Onverdeelbaarlangwoord hier", 10, 3, []string{"Onverdeelb", "aarlangwoo", "rd hier"}},
		{"breedte 日本語のタイトル", 9, 2, []string{"breedte", "日本語の…"}},
	} {
		if got := wrapCardTitle(tt.text, tt.width, tt.lines); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("wrapCardTitle(%q, %d, %d) = %q, want %q", tt.text, tt.width, tt.lines, got, tt.want)
		}
	}
}

// runCardFieldReads runs the read ensureCardFields queued and hands its
// answer back, as Bubble Tea would.
func runCardFieldReads(t *testing.T, a *App) {
	t.Helper()
	if len(a.pendingCmds) == 0 {
		t.Fatal("no frontmatter read was queued")
	}
	msg, ok := a.pendingCmds[len(a.pendingCmds)-1]().(cardFieldsLoadedMsg)
	if !ok {
		t.Fatal("the last queued command is not the frontmatter read")
	}
	a.pendingCmds = nil
	a.Update(msg)
}

func TestCardsShowChosenFrontmatterFields(t *testing.T) {
	a, v := boardTestApp(t, map[string]string{
		"Risico analyse.md": taskNote("open", "Risico: hoog\nsessie: 3\nteam: [a, b]\n"),
		"Leeg.md":           taskNote("open"),
		"Checklist.md":      "---\nrisico: laag\n---\n- [ ] Bel de klant @status:open @risico:midden\n",
	})
	a.pendingCmds = nil
	if v.refresh(a); len(a.pendingCmds) != 0 {
		t.Fatal("frontmatter was read with no card fields chosen")
	}
	a.prefs.KanbanCardFields = []string{"risico", "sessie", "team"}
	v.refresh(a)
	runCardFieldReads(t, a)
	lines := map[string]string{}
	for _, c := range v.tasks {
		lines[c.NoteTitle], _ = a.cardFieldLine(c)
	}
	if got, want := lines["Risico analyse"], "risico: hoog · sessie: 3 · team: a, b"; got != want {
		t.Errorf("a task note's fields: %q, want %q", got, want)
	}
	if got := lines["Leeg"]; got != "" {
		t.Errorf("a note with none of the fields gets a row: %q", got)
	}
	// What the task's own line says wins over its note.
	if got, want := lines["Checklist"], "risico: midden"; got != want {
		t.Errorf("a checkbox card's fields: %q, want %q", got, want)
	}
	// Too narrow for the names, a card keeps the values.
	for _, c := range v.tasks {
		if c.NoteTitle == "Risico analyse" {
			if f := v.cardFace(a, c, 20); f.fields != "hoog · 3 · a, b" {
				t.Errorf("fields in a narrow column: %q", f.fields)
			}
		}
	}
	// Nothing is read twice while the notes stay as they are.
	if v.refresh(a); len(a.pendingCmds) != 0 {
		t.Error("a refresh read the same notes again")
	}
}

func TestBoardCardsOfDifferentHeights(t *testing.T) {
	notes := map[string]string{"Checklist.md": "- [ ] Bel de klant @status:open\n"}
	for i := 0; i < 30; i++ {
		notes[fmt.Sprintf("Note %02d.md", i)] = taskNote("open")
	}
	a, v := boardTestApp(t, notes)
	a.prefs.KanbanHideEmptyColumns = true
	v.refresh(a)
	p := a.activePane
	a.View()
	// One column: the checkbox card is two rows, each task note one, a blank
	// row between them. Clicks land on the card drawn under them.
	spans := v.boardCards[0]
	if len(spans) < 3 || spans[0] != (cardSpan{0, 2}) || spans[1] != (cardSpan{3, 1}) || spans[2] != (cardSpan{5, 1}) {
		t.Fatalf("card rows: %v", spans)
	}
	for y, want := range map[int]int{3: 0, 4: 0, 6: 1, 7: 1, 8: 2} {
		v.card = 9
		v.click(a, p, 2, y, tea.MouseMsg{Button: tea.MouseButtonLeft}, false)
		if v.card != want {
			t.Errorf("a click on row %d selected card %d, want %d", y, v.card, want)
		}
	}
	// The column scrolls far enough to show the selected card whole.
	v.card = len(v.columns[0].cards) - 1
	a.View()
	start := v.boardStarts[0]
	if start == 0 || v.card-start >= len(v.boardCards[0]) {
		t.Fatalf("the last card is not drawn: start %d, %d cards drawn", start, len(v.boardCards[0]))
	}
	last := v.boardCards[0][v.card-start]
	if rows := a.contentRect(p).h - 1 - 2; last.top+last.height > rows {
		t.Errorf("the selected card ends on row %d of %d", last.top+last.height, rows)
	}
}
