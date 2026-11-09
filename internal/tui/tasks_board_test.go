package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// boardDay is the day that the board tests treat as today.
var boardDay = time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)

// boardWidth gives six columns that are 16 cells wide.
const boardWidth = 16*6 + 5

// statusBoard gives the board grouped by status for some note bodies, with
// the clock set to day.
func statusBoard(day time.Time, notes map[string]string) (*App, *tasksView) {
	a := sessionApp("note", notes)
	a.taskNow = func() time.Time { return day }
	a.prefs.KanbanGroupBy = "status"
	v := newTasksView(a)
	v.refresh(a)
	return a, v
}

// columnContents gives the content of the cards in each column.
func columnContents(v *tasksView) map[string]string {
	out := map[string]string{}
	for _, col := range v.columns {
		names := []string{}
		for _, c := range col.cards {
			names = append(names, c.Content)
		}
		sort.Strings(names)
		out[col.id] = strings.Join(names, "|")
	}
	return out
}

// boardCells gives the text of each board cell, without style and without the
// space around it.
func boardCells(board string) []string {
	cells := []string{}
	for _, line := range strings.Split(stripAnsi(board), "\n") {
		for _, cell := range strings.Split(line, "│") {
			cells = append(cells, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cell), "▍")))
		}
	}
	return cells
}

func hasCell(board, want string) bool {
	for _, cell := range boardCells(board) {
		if cell == want {
			return true
		}
	}
	return false
}

func TestBoardStatusColumnsFollowSessionDate(t *testing.T) {
	_, v := statusBoard(sessionDay, map[string]string{
		"Work": `- [ ] Fresh ^aaaaaa
- [ ] Late due:2026-11-20 ^bbbbbb
- [ ] Held @waiting ^cccccc
- [ ] Future ^dddddd
- [/] Busy ^eeeeee
- [x] Finished ^ffffff
`,
		"Plans": `- Fresh task:[[Work#^aaaaaa]] when:2026-11-05
- Late task:[[Work#^bbbbbb]] when:2026-11-02
- Held task:[[Work#^cccccc]] when:2026-11-02
- Future task:[[Work#^dddddd]] when:2026-11-06
- Busy task:[[Work#^eeeeee]] when:2026-11-02
- Finished task:[[Work#^ffffff]] when:2026-11-02
`,
	})
	got := columnContents(v)
	for id, want := range map[string]string{
		"today":       "Fresh|Late",
		"upcoming":    "",
		"undated":     "Future",
		"waiting":     "Held",
		"in-progress": "Busy",
		"done":        "Finished",
	} {
		if got[id] != want {
			t.Errorf("column %s = %q, want %q", id, got[id], want)
		}
	}
}

// boardMoveApp gives an app on a real vault, so that a move writes a note.
func boardMoveApp(t *testing.T, work, plans string, day time.Time) (*App, string, string) {
	t.Helper()
	root := newSessionParityVault(t)
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	workPath := filepath.Join(root, "inbox", "Work.md")
	plansPath := filepath.Join(root, "inbox", "Plans.md")
	for path, body := range map[string]string{workPath: work, plansPath: plans} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newSessionParityApp(t, root)
	a.taskNow = func() time.Time { return day }
	a.prefs.KanbanGroupBy = "status"
	msg := a.loadTasksCmd()().(tasksLoadedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	a.idx.tasks, a.idx.events = msg.tasks, msg.events
	return a, workPath, plansPath
}

// moveBoardCard moves the card of a task to a column of the board.
func moveBoardCard(t *testing.T, a *App, name, column string) {
	t.Helper()
	v := newTasksView(a)
	v.refresh(a)
	target := -1
	var card *vault.Task
	for ci, col := range v.columns {
		if col.id == column {
			target = ci
		}
		for i := range col.cards {
			if col.cards[i].Content == name {
				card = &col.cards[i]
			}
		}
	}
	if target < 0 || card == nil {
		t.Fatalf("no column %q or no card %q", column, name)
	}
	v.moveCard(a, *card, target)
}

func readBoardNote(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBoardMoveKeepsTodayCardAndSaysWhy(t *testing.T) {
	day := time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local)
	plans := "- T task:[[Work#^ab12cd]] when:2026-11-03T09:00\n"
	message := "T remains in Today. Its work session is on 2026-11-03 09:00"

	a, work, plansPath := boardMoveApp(t, "- [ ] T due:2026-11-06 ^ab12cd\n", plans, day)
	a.prefs.TimeFormat = "24h"
	moveBoardCard(t, a, "T", "undated")
	if got := readBoardNote(t, work); got != "- [ ] T ^ab12cd\n" {
		t.Errorf("Work.md = %q", got)
	}
	if got := readBoardNote(t, plansPath); got != plans {
		t.Errorf("Plans.md = %q", got)
	}
	if a.message != message {
		t.Errorf("message = %q, want %q", a.message, message)
	}
	loadTasksForTest(t, a)
	v := newTasksView(a)
	v.refresh(a)
	if got := columnContents(v); got["today"] != "T" || got["undated"] != "" {
		t.Errorf("columns after the move = %v", got)
	}

	a, work, plansPath = boardMoveApp(t, "- [ ] T due:2099-01-01 ^ab12cd\n", plans, day)
	a.prefs.TimeFormat = "24h"
	moveBoardCard(t, a, "T", "upcoming")
	if got := readBoardNote(t, work); got != "- [ ] T due:2099-01-01 ^ab12cd\n" {
		t.Errorf("Work.md after Upcoming = %q", got)
	}
	if got := readBoardNote(t, plansPath); got != plans {
		t.Errorf("Plans.md after Upcoming = %q", got)
	}
	if a.message != message {
		t.Errorf("message after Upcoming = %q, want %q", a.message, message)
	}

	a, work, _ = boardMoveApp(t, "- [ ] T due:2026-11-06 ^ab12cd\n", "- T task:[[Work#^ab12cd]] when:2026-11-06T09:00\n", day)
	moveBoardCard(t, a, "T", "undated")
	if got := readBoardNote(t, work); got != "- [ ] T ^ab12cd\n" {
		t.Errorf("Work.md with a future session = %q", got)
	}
	if strings.Contains(a.message, "remains in Today") {
		t.Errorf("message = %q", a.message)
	}
}

func TestBoardCardShowsTimedDateOnItsOwnLine(t *testing.T) {
	a, v := statusBoard(boardDay, map[string]string{
		"Work": `- [ ] Same day due:2026-11-02T21:00
- [ ] Next day due:2026-11-03T14:00
- [ ] Next year due:2027-01-05T09:00
- [ ] Ranged due:2026-11-02T14:00-15:30
`,
	})
	a.prefs.TimeFormat = "24h"
	board := v.renderBoard(a, boardWidth, 24, false)
	for _, want := range []string{"today 21:00", "Nov 3 14:00", "Jan 5 2027 09:00", "today 14:00"} {
		if !hasCell(board, want) {
			t.Errorf("no cell %q in\n%s", want, stripAnsi(board))
		}
	}
	if strings.Contains(stripAnsi(board), "15:30") {
		t.Errorf("a card shows an end time:\n%s", stripAnsi(board))
	}

	a.prefs.TimeFormat = "12h"
	board = v.renderBoard(a, boardWidth, 24, false)
	for _, want := range []string{"today 9:00 PM", "Nov 3 2:00 PM"} {
		if !hasCell(board, want) {
			t.Errorf("12h: no cell %q in\n%s", want, stripAnsi(board))
		}
	}
}

func TestBoardCardShowsSessionLine(t *testing.T) {
	a, v := statusBoard(boardDay, map[string]string{
		"Work":  "- [ ] Draft report ^aaaaaa\n",
		"Plans": "- Draft task:[[Work#^aaaaaa]] when:2026-11-02T21:00\n",
	})
	a.prefs.TimeFormat = "24h"
	for _, focused := range []bool{false, true} {
		board := v.renderBoard(a, boardWidth, 20, focused)
		if !hasCell(board, "ses today 21:00") {
			t.Errorf("focused %v: no session cell in\n%s", focused, stripAnsi(board))
		}
	}
}

func TestBoardCardWithoutTimeIsUnchanged(t *testing.T) {
	a, v := statusBoard(boardDay, map[string]string{
		"Work": "- [ ] Plain due:2026-11-03\n",
	})
	var card vault.Task
	for _, col := range v.columns {
		for _, c := range col.cards {
			card = c
		}
	}
	if card.Content != "Plain" {
		t.Fatalf("card = %+v", card)
	}
	lines := boardCardLines(a, card, 16, false)
	if len(lines) != 2 {
		t.Fatalf("card has %d lines", len(lines))
	}
	if got := strings.TrimRight(stripAnsi(lines[0]), " "); got != " Plain" {
		t.Errorf("title = %q", got)
	}
	if got := strings.TrimRight(stripAnsi(lines[1]), " "); got != " 2026-11-03 Work" {
		t.Errorf("details = %q", got)
	}
	if boardCardHeight(card) != 3 {
		t.Errorf("height = %d", boardCardHeight(card))
	}
}

// A column of cards with a date line scrolls so that the selected card stays
// in view.
func TestBoardScrollCountsDateLines(t *testing.T) {
	body := ""
	for _, name := range []string{"One", "Two", "Three", "Four", "Five", "Six"} {
		body += "- [ ] " + name + " due:2026-11-09T10:00\n"
	}
	a, v := statusBoard(boardDay, map[string]string{"Work": body})
	v.col = 1
	v.card = 5
	board := stripAnsi(v.renderBoard(a, boardWidth, 12, true))
	if !strings.Contains(board, "Six") || !strings.Contains(board, "Nov 9 10:00") {
		t.Fatalf("selected card is not in view:\n%s", board)
	}
	if v.boardStarts[1] == 0 {
		t.Fatalf("column did not scroll: %v", v.boardStarts)
	}
}
