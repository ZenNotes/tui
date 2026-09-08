package tui

import (
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/search"
)

func TestSplitBlocks(t *testing.T) {
	src := "---\ntitle: x\n---\n# Heading\n\npara one\npara two\n\n- [ ] a\n- [x] b\n    - child\n\n```go\ncode\n```\n> quote\n| a | b |\n| - | - |\n"
	blocks := splitBlocks(strings.Split(src, "\n"))
	kinds := []string{}
	for _, b := range blocks {
		kinds = append(kinds, b.kind)
	}
	want := "front heading blank para blank list blank fence quote table blank"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds %q, want %q", got, want)
	}
	if blocks[5].start != 8 || blocks[5].end != 11 {
		t.Fatalf("list block lines %d..%d", blocks[5].start, blocks[5].end)
	}
}

func TestActiveSGR(t *testing.T) {
	if got := activeSGR("\x1b[38;5;252mtext"); got != "\x1b[38;5;252m" {
		t.Fatalf("256 color: %q", got)
	}
	if got := activeSGR("\x1b[1m\x1b[38;2;1;2;3mx\x1b[0m"); got != "" {
		t.Fatalf("reset should clear: %q", got)
	}
	if got := activeSGR("\x1b[31;1mx"); got != "\x1b[31;1m" {
		t.Fatalf("basic color + bold: %q", got)
	}
}

func TestPrepareMarkdown(t *testing.T) {
	out := prepareMarkdown("see [[Note|alias]] and [[Other]] ![[pic.png]]\n> [!tip] Title\n> body\n- [/] going\n- [-] dropped\n- [>] moved")
	for _, want := range []string{"⟦alias⟧", "⟦Other⟧", "⟦image: pic.png⟧", "> **TIP** Title", "- ◩ going", "- ✕ ~~dropped~~", "- → moved"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestResolveGlamourStyle(t *testing.T) {
	for _, name := range []string{"auto", "dark", "light", "dracula", "tokyo-night", "pink", "ascii", "notty"} {
		cfg, err := resolveGlamourStyle(name, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cfg.Document.Margin == nil || *cfg.Document.Margin != 0 {
			t.Fatalf("%s: margin should be zeroed", name)
		}
	}
	if _, err := resolveGlamourStyle("/no/such/style.json", true); err == nil {
		t.Fatal("missing file should error")
	}
}

func TestGlamourRendersBlocksWithSourceLines(t *testing.T) {
	a := &App{prefs: prefsView{}, theme: buildTheme(true)}
	a.prefs.PreviewStyle = "dark"
	text := "# Title\n\nHello [[World]] #tag\n\n- [ ] first\n- [x] second\n"
	lines, err := a.renderGlamourLines(text, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 5 {
		t.Fatalf("too few rows: %d", len(lines))
	}
	seen := map[int]bool{}
	for _, l := range lines {
		seen[l.srcLine] = true
	}
	for _, want := range []int{0, 2, 4, 5} {
		if !seen[want] {
			t.Fatalf("no row maps to source line %d: %+v", want, lines)
		}
	}
	taskRows := 0
	for _, l := range lines {
		if l.task {
			taskRows++
		}
	}
	if taskRows != 2 {
		t.Fatalf("task rows %d", taskRows)
	}
}

func TestFoldAndMatchPositions(t *testing.T) {
	if search.Fold("Résumé Ünïcode") != "resume unicode" {
		t.Fatalf("fold: %q", search.Fold("Résumé Ünïcode"))
	}
	pos := search.MatchPositions("rsm", "Résumé")
	if len(pos) != 3 || pos[0] != 0 || pos[1] != 2 || pos[2] != 4 {
		t.Fatalf("positions %v", pos)
	}
	if search.MatchPositions("zz", "Résumé") != nil {
		t.Fatal("no match should be nil")
	}
}

func TestKeysHintFollowsRebinding(t *testing.T) {
	a := &App{keymap: keymaps.NewResolver(map[string]string{"nav.moveDown": "n", "nav.contextMenu": ""})}
	got := a.keysHint([]hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"nav.contextMenu", "menu"}, {"key:Esc", "back"}})
	if got != "n/k move · Esc back" {
		t.Fatalf("hint %q", got)
	}
	if bindingLabel("g g") != "gg" || bindingLabel("Ctrl+D") != "Ctrl+D" {
		t.Fatal("binding labels")
	}
	if fitHint("a b · c d · e f", 9) != "a b …" {
		t.Fatalf("fit: %q", fitHint("a b · c d · e f", 9))
	}
}
