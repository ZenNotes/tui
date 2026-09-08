package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestWrapLineBreaksAtSpaces(t *testing.T) {
	segs := wrapLine([]rune("the quick brown fox jumps"), 10)
	if len(segs) != 3 {
		t.Fatalf("segments: %+v", segs)
	}
	got := string([]rune("the quick brown fox jumps")[segs[0].start:segs[0].end])
	if got != "the quick " {
		t.Fatalf("first row %q", got)
	}
}

func TestBindingKeys(t *testing.T) {
	keys := bindingKeys("Alt+H")
	if len(keys) != 1 || !keys[0].Alt || keys[0].Rune != 'h' {
		t.Fatalf("Alt+H parsed as %+v", keys)
	}
	keys = bindingKeys("g t")
	if len(keys) != 2 || keys[0].Rune != 'g' || keys[1].Rune != 't' {
		t.Fatalf("g t parsed as %+v", keys)
	}
	keys = bindingKeys("Ctrl+Shift+P")
	if len(keys) != 1 || !keys[0].Ctrl || keys[0].Rune != 'p' {
		t.Fatalf("Ctrl+Shift+P parsed as %+v", keys)
	}
	complete, prefix := matchSequence([]vim.Key{vim.R('g')}, keys)
	if complete || prefix {
		t.Fatal("g should not match Ctrl+P")
	}
	complete, prefix = matchSequence([]vim.Key{vim.R('g')}, bindingKeys("g t"))
	if complete || !prefix {
		t.Fatal("g should be a prefix of g t")
	}
}

func TestLineClasses(t *testing.T) {
	classes := lineClasses("- [x] done #tag", false, false)
	if classes[2] != stCheckbox {
		t.Fatalf("checkbox class %d", classes[2])
	}
	if classes[len(classes)-1] != stDone {
		t.Fatalf("done line should carry the done class, got %d", classes[len(classes)-1])
	}
	classes = lineClasses("see [[Note]] and #idea", false, false)
	if classes[4] != stLink {
		t.Fatalf("wikilink class %d", classes[4])
	}
	if classes[len(classes)-1] != stTag {
		t.Fatalf("tag class %d", classes[len(classes)-1])
	}
	classes = lineClasses("title: x", false, true)
	if classes[0] != stFront {
		t.Fatal("frontmatter class")
	}
}

func TestLinkAt(t *testing.T) {
	line := "See [[Project Plan|plan]] or [site](https://zennotes.org) or https://x.y"
	l, ok := linkAt(line, 6)
	if !ok || l.kind != "wikilink" || l.target != "Project Plan" || l.alias != "plan" {
		t.Fatalf("wikilink: %+v %v", l, ok)
	}
	l, ok = linkAt(line, 30)
	if !ok || l.kind != "url" || l.target != "https://zennotes.org" {
		t.Fatalf("md link: %+v %v", l, ok)
	}
	if links := linksOnLine(line); len(links) != 3 {
		t.Fatalf("links on line: %d", len(links))
	}
}

func TestFilterTasks(t *testing.T) {
	tasks := []vault.Task{
		{Content: "Write docs", NoteTitle: "Plan", Due: "2026-01-01", Priority: "high", Tags: []string{"tui"}},
		{Content: "Ship", NoteTitle: "Other", Waiting: true, Fields: map[string]string{"status": "review"}},
	}
	if got := filterTasks(tasks, "docs"); len(got) != 1 || got[0].Content != "Write docs" {
		t.Fatalf("word filter: %+v", got)
	}
	if got := filterTasks(tasks, "#tui priority:high"); len(got) != 1 {
		t.Fatalf("tag+priority filter: %+v", got)
	}
	if got := filterTasks(tasks, "status:review"); len(got) != 1 || got[0].Content != "Ship" {
		t.Fatalf("status filter: %+v", got)
	}
	if got := filterTasks(tasks, "-docs"); len(got) != 1 || got[0].Content != "Ship" {
		t.Fatalf("negated filter: %+v", got)
	}
	if got := filterTasks(tasks, "is:waiting"); len(got) != 1 {
		t.Fatalf("is:waiting: %+v", got)
	}
}

func TestParseDueInput(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.Local) // a Tuesday
	cases := map[string]string{"": "", "today": "2026-09-08", "tomorrow": "2026-09-09", "+3": "2026-09-11", "fri": "2026-09-11", "tue": "2026-09-15", "2026-12-24": "2026-12-24"}
	for in, want := range cases {
		got, err := parseDueInput(in, now)
		if err != nil || got != want {
			t.Fatalf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := parseDueInput("someday", now); err == nil {
		t.Fatal("garbage should error")
	}
}

func TestPaneSplitAndRemove(t *testing.T) {
	tree := newPaneTree()
	first := tree.leaves()[0]
	first.tabs = []*tab{{path: "a.md", mode: modeEdit}}
	second := tree.split(first, true)
	if second == nil || len(tree.leaves()) != 2 || len(second.tabs) != 1 {
		t.Fatalf("split: %+v", tree.leaves())
	}
	tree.layout(rect{0, 0, 100, 40})
	if first.rect.w+second.rect.w+1 != 100 {
		t.Fatalf("widths: %d %d", first.rect.w, second.rect.w)
	}
	if tree.neighbor(first, 'l') != second || tree.neighbor(second, 'h') != first {
		t.Fatal("neighbors")
	}
	if !tree.remove(second) || len(tree.leaves()) != 1 {
		t.Fatal("remove")
	}
}

func TestHelpFilter(t *testing.T) {
	a := &App{}
	a.keymap = newTestResolver()
	lines := helpLines(a, "board")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("filtered help: %v", lines)
	}
	found := false
	for _, l := range lines {
		if contains(l, "Board:") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the board keys in the board filter")
	}
}

func TestIsDatabaseDir(t *testing.T) {
	if !isDatabaseDir("Reading List.base") || !isDatabaseDir("Work/Books.base/pages") || isDatabaseDir("Work/Base camp") {
		t.Fatal("database dir detection")
	}
}

func TestWithBackgroundFillsAfterNestedResets(t *testing.T) {
	seq := backgroundSeq(lipgloss.Color("#504945"))
	if seq == "" {
		t.Skip("no color profile in this environment")
	}
	line := "\x1b[1mbold\x1b[0m   plain"
	got := withBackground(line, lipgloss.Color("#504945"))
	if !strings.HasPrefix(got, seq) || !strings.Contains(got, "\x1b[0m"+seq+"   plain") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("background not re-applied after the reset: %q", got)
	}
}

func TestFrameBoxIsExactlySized(t *testing.T) {
	th := buildTheme(true)
	box := frameBox(th, "editor", "one\ntwo", 20, 5, true)
	lines := strings.Split(box, "\n")
	if len(lines) != 5 {
		t.Fatalf("height %d", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 20 {
			t.Fatalf("line %d is %d wide: %q", i, w, l)
		}
	}
	plain := stripAnsi(lines[0])
	if !strings.HasPrefix(plain, "╭─ editor ") || !strings.HasSuffix(plain, "╮") {
		t.Fatalf("title edge: %q", plain)
	}
	if stripAnsi(lines[1]) != "│ one              │" {
		t.Fatalf("padded body row: %q", stripAnsi(lines[1]))
	}
}

func TestContentRectInsetsFramedPanes(t *testing.T) {
	a := &App{panes: newPaneTree(), width: 100, height: 40, ready: true}
	p := a.panes.leaves()[0]
	p.rect = rect{10, 0, 60, 30}
	inner := a.contentRect(p)
	if inner != (rect{12, 2, 56, 27}) {
		t.Fatalf("framed inset: %+v", inner)
	}
	a.zen = true
	if got := a.contentRect(p); got != p.rect {
		t.Fatalf("zen keeps the whole pane: %+v", got)
	}
	a.zen = false
	p.rect = rect{0, 0, 10, 4}
	if got := a.contentRect(p); got != (rect{0, 1, 10, 3}) {
		t.Fatalf("tiny panes drop the frame but keep the strip: %+v", got)
	}
}

func TestSortNotesFollowsTheDesktopOrders(t *testing.T) {
	mk := func() []vault.NoteMeta {
		return []vault.NoteMeta{{Title: "b", UpdatedAt: 2, CreatedAt: 30}, {Title: "a", UpdatedAt: 3, CreatedAt: 10}, {Title: "c", UpdatedAt: 1, CreatedAt: 20}}
	}
	titles := func(n []vault.NoteMeta) string {
		out := ""
		for _, m := range n {
			out += m.Title
		}
		return out
	}
	cases := map[string]string{"name-asc": "abc", "name-desc": "cba", "updated-desc": "abc", "updated-asc": "cba", "created-desc": "bca", "created-asc": "acb", "none": "bac", "manual": "bac", "": "abc"}
	for order, want := range cases {
		n := mk()
		sortNotes(n, order)
		if got := titles(n); got != want {
			t.Errorf("%q: got %s want %s", order, got, want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	if humanSize(512) != "512 B" || humanSize(2048) != "2 KB" || humanSize(3<<20) != "3.0 MB" {
		t.Fatalf("%s %s %s", humanSize(512), humanSize(2048), humanSize(3<<20))
	}
}
