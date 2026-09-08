package tui

import (
	"testing"
	"time"
)

func TestDayAtCell(t *testing.T) {
	first := time.Date(2026, 8, 31, 0, 0, 0, 0, time.Local) // Monday before September 2026
	day, ok := dayAtCell(first, 6, 0, 2, false)
	if !ok || day.Day() != 31 || day.Month() != time.August {
		t.Fatalf("first cell: %v %v", day, ok)
	}
	day, ok = dayAtCell(first, 6, 4+5*1, 3, true) // week numbers shown, second column, second week
	if !ok || day.Day() != 8 || day.Month() != time.September {
		t.Fatalf("second row second column: %v %v", day, ok)
	}
	if _, ok := dayAtCell(first, 6, 0, 1, false); ok {
		t.Fatal("header row is not a day")
	}
	if _, ok := dayAtCell(first, 6, 40, 2, false); ok {
		t.Fatal("past the last column is not a day")
	}
}

func TestTabAtUsesRenderedSpans(t *testing.T) {
	p := &pane{tabs: []*tab{{path: "a.md"}, {path: "b.md"}}, tabSpans: [][2]int{{0, 6}, {7, 14}}}
	if p.tabAt(3) != 0 || p.tabAt(7) != 1 || p.tabAt(13) != 1 {
		t.Fatal("span lookup")
	}
	if p.tabAt(6) != -1 || p.tabAt(20) != -1 {
		t.Fatal("gaps are not tabs")
	}
}

func TestDividerHitTest(t *testing.T) {
	a := &App{panes: newPaneTree(), width: 100, height: 40, ready: true}
	first := a.panes.leaves()[0]
	first.tabs = []*tab{{path: "a.md"}}
	a.panes.split(first, true)
	a.panes.layout(rect{0, 0, 100, 38})
	second := a.panes.leaves()[1]
	if node := a.dividerAt(a.panes, second.rect.x-1, 5); node == nil || !node.vertical {
		t.Fatal("vertical divider not found")
	}
	if node := a.dividerAt(a.panes, second.rect.x+2, 5); node != nil {
		t.Fatal("pane body is not a divider")
	}
}

func TestPinnedTabsStayFirstAndSurviveCloseOthers(t *testing.T) {
	a := &App{panes: newPaneTree(), buffers: map[string]*noteBuffer{}, noteModes: map[string]paneMode{}}
	p := a.panes.leaves()[0]
	a.activePane = p
	p.tabs = []*tab{{path: "zen://home"}, {path: "zen://tasks"}, {path: "zen://tags"}}
	p.active = 1
	a.togglePin(p, 1)
	if p.tabs[0].path != "zen://tasks" || !p.tabs[0].pinned || p.active != 0 {
		t.Fatalf("pinned tab should move first: %+v active=%d", p.tabs, p.active)
	}
	p.active = 2
	a.closeOtherTabs()
	if len(p.tabs) != 2 || p.tabs[0].path != "zen://tasks" || p.tabs[1].path != "zen://tags" {
		t.Fatalf("close others must keep the pinned tab: %+v", p.tabs)
	}
	a.closeTab(p, 0)
	if len(p.tabs) != 2 {
		t.Fatal("a pinned tab must not close")
	}
	a.moveTab(p, -1)
	if p.tabs[1].path != "zen://tags" {
		t.Fatal("moving across the pinned boundary must be refused")
	}
}

func TestTabBarOverflowKeepsActiveVisible(t *testing.T) {
	a := &App{theme: buildTheme(true), buffers: map[string]*noteBuffer{}, prefs: prefsView{}}
	p := &pane{}
	for i := 0; i < 12; i++ {
		p.tabs = append(p.tabs, &tab{path: "zen://database/Some long database name " + string(rune('a'+i))})
	}
	p.active = 11
	line := a.renderTabBar(p, 60, true)
	if cellWidth(stripAnsi(line)) != 60 {
		t.Fatalf("tab bar width %d", cellWidth(stripAnsi(line)))
	}
	if p.tabSpans[11][0] < 0 {
		t.Fatal("active tab should be drawn")
	}
	if p.tabSpans[0][0] >= 0 {
		t.Fatal("first tab should have scrolled out of view")
	}
}
