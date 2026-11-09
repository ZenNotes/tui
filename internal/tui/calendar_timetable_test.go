package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// calTableOf builds the timetable of a day over the given note bodies.
func calTableOf(t *testing.T, notes map[string]string, day time.Time, zoom int) calTimetable {
	t.Helper()
	_, tv := calEventApp(t, notes)
	return tv.calc().timetable(day, zoom)
}

// calBlockLines describes each block as `text first-last Llevel`, in the
// order of the timetable, so a test compares whole layouts.
func calBlockLines(tt calTimetable) []string {
	var out []string
	for _, b := range tt.blocks {
		out = append(out, fmt.Sprintf("%s %d-%d L%d", b.entry.content(), b.first, b.last, b.level))
	}
	return out
}

// calSlotRange is the `first-last` slot range of the only block of a table.
func calSlotRange(t *testing.T, tt calTimetable) string {
	t.Helper()
	if len(tt.blocks) != 1 {
		t.Fatalf("got %d blocks, want 1: %v", len(tt.blocks), calBlockLines(tt))
	}
	return fmt.Sprintf("%d-%d", tt.blocks[0].first, tt.blocks[0].last)
}

func TestBlockCoversSlotsFromStartToEnd(t *testing.T) {
	day := calDate(2026, time.November, 3)
	cases := []struct {
		value string
		// want maps a zoom in minutes to the slot range of the block.
		want map[int]string
	}{
		{"2026-11-03T14:00-15:30", map[int]string{60: "14-15", 30: "28-30", 15: "56-61"}},
		{"2026-11-03T14:00-15:00", map[int]string{60: "14-14", 30: "28-29"}},
		{"2026-11-03T09:40-10:10", map[int]string{60: "9-10", 30: "19-20"}},
		{"2026-11-03T14:20", map[int]string{60: "14-14", 30: "28-28", 15: "57-57"}},
	}
	for _, c := range cases {
		forms := map[string]map[string]string{
			"due": {"Job.md": "- [ ] Job due:" + c.value + "\n"},
			"session": {
				"Work.md":  "- [ ] Job ^aaaaaa\n",
				"Plans.md": "- task:[[Work#^aaaaaa]] when:" + c.value + "\n",
			},
		}
		for name, notes := range forms {
			for zoom, want := range c.want {
				tt := calTableOf(t, notes, day, zoom)
				if got := calSlotRange(t, tt); got != want {
					t.Errorf("%s %s zoom %d: slots %s, want %s", name, c.value, zoom, got, want)
				}
				if len(tt.untimed) != 0 {
					t.Errorf("%s %s zoom %d: untimed %v, want none", name, c.value, zoom, tt.untimed)
				}
			}
		}
	}
	// A block never lies on another day.
	for _, d := range []int{2, 4} {
		tt := calTableOf(t, map[string]string{"Job.md": "- [ ] Job due:2026-11-03T14:00-15:30\n"}, calDate(2026, time.November, d), 60)
		if len(tt.blocks) != 0 || len(tt.untimed) != 0 {
			t.Errorf("day %d: got %v, want nothing", d, calBlockLines(tt))
		}
	}
}

func TestSessionBlockStartsInItsSlot(t *testing.T) {
	notes := map[string]string{
		"Work.md":  "- [ ] Job ^aaaaaa\n",
		"Plans.md": "- task:[[Work#^aaaaaa]] when:2026-11-05T09:40\n",
	}
	day := calDate(2026, time.November, 5)
	for zoom, want := range map[int]string{30: "19-19", 60: "9-9"} {
		if got := calSlotRange(t, calTableOf(t, notes, day, zoom)); got != want {
			t.Errorf("zoom %d: slots %s, want %s", zoom, got, want)
		}
	}
}

func TestWeekPlacesSessionAndDueByOwnTime(t *testing.T) {
	notes := map[string]string{
		"Work.md":  "- [ ] X due:2026-11-10T17:00 ^aaaaaa\n- [ ] Y due:2026-11-10 ^bbbbbb\n",
		"Plans.md": "- task:[[Work#^aaaaaa]] when:2026-11-05T09:00\n- task:[[Work#^bbbbbb]] when:2026-11-05T11:00\n",
	}
	_, tv := calEventApp(t, notes)
	c := tv.calc()
	five := c.timetable(calDate(2026, time.November, 5), 60)
	if got, want := calBlockLines(five), []string{"X 9-9 L1", "Y 11-11 L1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("2026-11-05 blocks = %v, want %v", got, want)
	}
	ten := c.timetable(calDate(2026, time.November, 10), 60)
	if got, want := calBlockLines(ten), []string{"X 17-17 L1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("2026-11-10 blocks = %v, want %v", got, want)
	}
	if len(ten.untimed) != 1 || ten.untimed[0].content() != "Y" {
		t.Errorf("2026-11-10 untimed = %v, want the due entry Y only", ten.untimed)
	}
}

func TestOverlappingBlocksGetLevels(t *testing.T) {
	day := calDate(2026, time.November, 3)
	t.Run("chain", func(t *testing.T) {
		tt := calTableOf(t, map[string]string{"Plan.md": "- [ ] A due:2026-11-03T09:00-11:00\n" +
			"- [ ] B due:2026-11-03T10:00-10:30\n- [ ] C due:2026-11-03T10:15\n- [ ] D due:2026-11-03T11:00-12:00\n"}, day, 60)
		want := []string{"A 9-10 L1", "B 10-10 L2", "C 10-10 L3", "D 11-11 L1"}
		if got := calBlockLines(tt); !reflect.DeepEqual(got, want) {
			t.Errorf("blocks = %v, want %v", got, want)
		}
	})
	t.Run("same start without end", func(t *testing.T) {
		tt := calTableOf(t, map[string]string{"Plan.md": "- [ ] A due:2026-11-03T09:00\n- [ ] B due:2026-11-03T09:00\n- [ ] C due:2026-11-03T09:00\n"}, day, 60)
		want := []string{"A 9-9 L1", "B 9-9 L2", "C 9-9 L3"}
		if got := calBlockLines(tt); !reflect.DeepEqual(got, want) {
			t.Errorf("blocks = %v, want %v", got, want)
		}
		if tt.over[9] != 0 {
			t.Errorf("overflow in slot 9 = %d, want 0", tt.over[9])
		}
	})
	t.Run("touching blocks", func(t *testing.T) {
		notes := map[string]string{"Plan.md": "- [ ] A due:2026-11-03T09:00-09:30\n- [ ] B due:2026-11-03T09:30-10:00\n"}
		if got, want := calBlockLines(calTableOf(t, notes, day, 60)), []string{"A 9-9 L1", "B 9-9 L2"}; !reflect.DeepEqual(got, want) {
			t.Errorf("hourly blocks = %v, want %v", got, want)
		}
		if got, want := calBlockLines(calTableOf(t, notes, day, 30)), []string{"A 18-18 L1", "B 19-19 L1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("half-hourly blocks = %v, want %v", got, want)
		}
	})
	t.Run("event, session and due", func(t *testing.T) {
		tt := calTableOf(t, map[string]string{
			"Work.md":  "- [ ] Report due:2026-11-03T14:00 ^aaaaaa\n",
			"Plans.md": "- Meeting when:2026-11-03T14:00-15:00\n- task:[[Work#^aaaaaa]] when:2026-11-03T14:30-15:30\n",
		}, day, 60)
		levels := map[calEntryKind]int{}
		for _, b := range tt.blocks {
			levels[b.entry.kind] = b.level
		}
		want := map[calEntryKind]int{calDue: 1, calEvent: 2, calSession: 3}
		if !reflect.DeepEqual(levels, want) {
			t.Errorf("levels = %v, want %v (blocks %v)", levels, want, calBlockLines(tt))
		}
	})
}

func TestFifthOverlapSharesLevelFourAndCounts(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&sb, "- [ ] T%d due:2026-11-03T10:00-11:00\n", i)
	}
	tt := calTableOf(t, map[string]string{"Plan.md": sb.String()}, calDate(2026, time.November, 3), 60)
	want := []string{"T1 10-10 L1", "T2 10-10 L2", "T3 10-10 L3", "T4 10-10 L4", "T5 10-10 L4", "T6 10-10 L4"}
	if got := calBlockLines(tt); !reflect.DeepEqual(got, want) {
		t.Errorf("blocks = %v, want %v", got, want)
	}
	if tt.over[10] != 2 {
		t.Errorf("overflow in slot 10 = %d, want 2", tt.over[10])
	}
	if tt.over[9] != 0 || tt.over[11] != 0 {
		t.Errorf("overflow leaked to the next slots: %v", tt.over)
	}
}

func TestCrossDayEventBlocksAndBand(t *testing.T) {
	notes := map[string]string{"Plans.md": "- Shift when:2026-11-03T22:00/2026-11-05T02:00\n"}
	_, tv := calEventApp(t, notes)
	c := tv.calc()
	first := c.timetable(calDate(2026, time.November, 3), 60)
	if got, want := calBlockLines(first), []string{"Shift 22-23 L1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("first day = %v, want %v", got, want)
	}
	mid := c.timetable(calDate(2026, time.November, 4), 60)
	if len(mid.blocks) != 0 || len(mid.untimed) != 1 || mid.untimed[0].label("24h") != "◆Shift" {
		t.Errorf("middle day: blocks %v, untimed %v, want one untimed ◆Shift", calBlockLines(mid), mid.untimed)
	}
	last := c.timetable(calDate(2026, time.November, 5), 60)
	if got, want := calBlockLines(last), []string{"Shift 0-1 L1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("last day = %v, want %v", got, want)
	}
}

func TestSessionAndDueAreSeparateBlocks(t *testing.T) {
	tt := calTableOf(t, map[string]string{
		"Work.md":  "- [ ] Meet due:2026-11-05T17:00 ^ab12cd\n",
		"Plans.md": "- task:[[Work#^ab12cd]] when:2026-11-05T08:00\n",
	}, calDate(2026, time.November, 5), 60)
	if got, want := calBlockLines(tt), []string{"Meet 8-8 L1", "Meet 17-17 L1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks = %v, want %v", got, want)
	}
	if tt.blocks[0].entry.kind != calSession || tt.blocks[1].entry.kind != calDue {
		t.Errorf("kinds = %v and %v, want a session and then a due entry", tt.blocks[0].entry.kind, tt.blocks[1].entry.kind)
	}
}
