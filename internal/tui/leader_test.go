package tui

import (
	"github.com/ZenNotes/tui/internal/vim"
	"testing"
)

func TestLeaderAppearsImmediatelyAndTimedSequenceExpires(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.handleKey(vim.R(' '))
	if a.leader == nil || !a.leader.showHints {
		t.Fatal("leader hints must appear on the first key")
	}
	seq := a.leader.seq
	a.Update(leaderHintMsg{seq: seq})
	if a.leader != nil {
		t.Fatal("timed expiry must disarm the sequence")
	}
}

func TestLeaderSubmenuIgnoresPreviousExpiry(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.handleKey(vim.R(' '))
	seq := a.leader.seq
	a.handleKey(vim.R('l'))
	a.Update(leaderHintMsg{seq: seq})
	if a.leader == nil || !a.leader.showHints {
		t.Fatal("old timeout closed the submenu")
	}
	a.Update(leaderHintMsg{seq: a.leader.seq})
	if a.leader != nil {
		t.Fatal("submenu timeout did not expire")
	}
}

func TestStickyLeaderDismissesWithLeaderKey(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.WhichKeyHintMode = "sticky"
	a.handleKey(vim.R(' '))
	if a.leader == nil || !a.leader.showHints {
		t.Fatal("sticky hints must appear immediately")
	}
	a.handleKey(vim.R(' '))
	if a.leader != nil || a.messageErr || a.message != "" {
		t.Fatal("leader should quietly dismiss sticky hints")
	}
}

func TestCommandAliasesAreUnique(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	seen := map[string]string{}
	for _, c := range a.commandTable() {
		for _, n := range c.names {
			if prev, ok := seen[n]; ok {
				t.Errorf("%s belongs to both %s and %s", n, prev, c.title)
			}
			seen[n] = c.title
		}
	}
}
