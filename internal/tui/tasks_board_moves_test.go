package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

func TestBoardTodayMoveAndShiftKeepRange(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-18T14:00-15:30\n", "2026-11-02")
	a.setTaskStatusColumn(repeatTaskAt(t, path), "today")
	want := "- [ ] Call Ana due:" + vault.TodayISO(time.Now()) + "T14:00-15:30\n"
	if got := readRepeatNote(t, path); got != want {
		t.Fatalf("Today move: got %q, want %q", got, want)
	}

	a, path = repeatTasksApp(t, "- [ ] Call Ana due:2026-11-18T14:00-15:30\n", "2026-11-02")
	a.shiftTaskDue(repeatTaskAt(t, path), 1)
	if got := readRepeatNote(t, path); !strings.Contains(got, "due:2026-11-19T14:00-15:30") {
		t.Fatalf("shift: got %q", got)
	}
}
