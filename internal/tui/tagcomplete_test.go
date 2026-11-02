package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

// newTagTestApp opens Draft.md in a vault whose other notes carry tags:
// project twice, the rest once, one of them only in the trash.
func newTagTestApp(t *testing.T, prefs config.Prefs, draft string) (*App, *noteBuffer) {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	notes := map[string]string{
		"Draft.md":         draft,
		"Plan.md":          "# Plan\n\n#project #work/deep\n",
		"Roadmap.md":       "---\ntags: [project, Idea]\n---\n# Roadmap\n",
		"Log.md":           "# Log\n\n#prose and `#procode` in code\n",
		".trash/Gone.md":   "#protrash\n",
		".zennotes/.keep":  "",
		"archive/Older.md": "#archived-project\n",
	}
	for name, body := range notes {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target := backend.Target{Kind: backend.KindLocal, Root: root}
	b, err := backend.New(target, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(context.Background(), Options{Backend: b, Target: target}, prefs, true)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.Update(a.loadIndexCmd()())
	a.openNote("Draft.md", true)
	buf := a.activeBuffer()
	if buf == nil {
		t.Fatalf("Draft.md did not open: %s", a.message)
	}
	return a, buf
}

func feed(a *App, keys ...vim.Key) {
	for _, k := range keys {
		a.handleKey(k)
	}
}

func typeKeys(a *App, text string) {
	for _, r := range text {
		a.handleKey(vim.R(r))
	}
}

func menuTags(a *App) string {
	m := a.liveTagMenu()
	if m == nil {
		return ""
	}
	tags := []string{}
	for _, it := range m.items {
		tags = append(tags, it.tag)
	}
	return strings.Join(tags, ",")
}

func vimPrefs() config.Prefs {
	p := config.DefaultPrefs()
	p.VimInsertEscape = ""
	return p
}

func TestTypingAHashOffersTheVaultsTags(t *testing.T) {
	a, buf := newTagTestApp(t, vimPrefs(), "# Draft\n\n")
	feed(a, vim.R('G'), vim.R('o'))
	typeKeys(a, "see #")
	if got := menuTags(a); got != "" {
		t.Fatalf("a bare # must stay quiet (it may become a heading), got %q", got)
	}
	typeKeys(a, "pro")
	// Prefix matches by use count then name, then substring matches; the
	// trash and code spans contribute nothing.
	if got := menuTags(a); got != "project,prose,archived-project" {
		t.Fatalf("candidates for #pro: %q", got)
	}
	if !strings.Contains(a.View(), "#archived-project") {
		t.Fatal("the menu is not on screen")
	}
	feed(a, vim.Key{Rune: 'n', Ctrl: true}, vim.Key{Name: "enter"})
	if got := buf.ed.Line(3); got != "see #prose" {
		t.Fatalf("accepted line %q", got)
	}
	if a.liveTagMenu() != nil {
		t.Fatal("the menu should close after a pick")
	}
	// Still in insert mode, and the pick undoes together with the typing.
	if buf.ed.Mode() != vim.ModeInsert {
		t.Fatalf("mode after pick: %v", buf.ed.Mode())
	}
	feed(a, vim.KeyEsc, vim.R('u'))
	if buf.ed.LineCount() != 3 {
		t.Fatalf("undo left %q", buf.ed.Text())
	}
}

func TestTagMenuMatchesTheDesktopsRules(t *testing.T) {
	a, buf := newTagTestApp(t, vimPrefs(), "---\ntags: [x]\n---\n# Draft #\n\n```\n#\n```\nurl.com/#\n`#`\n\n")
	at := func(line int, typed string) string {
		t.Helper()
		feed(a, vim.KeyEsc)
		buf.ed.SetCursor(vim.Pos{Line: line, Col: 0})
		feed(a, vim.R('A'))
		typeKeys(a, typed)
		return menuTags(a)
	}
	for line, name := range map[int]string{3: "heading", 6: "fenced code", 8: "mid-word"} {
		if got := at(line, "pro"); got != "" {
			t.Fatalf("%s offered %q", name, got)
		}
	}
	feed(a, vim.KeyEsc)
	buf.ed.SetCursor(vim.Pos{Line: 9, Col: 1})
	feed(a, vim.R('a'))
	typeKeys(a, "pro")
	if got := menuTags(a); got != "" {
		t.Fatalf("inline code offered %q", got)
	}
	// Case-insensitive, nested tags match as plain text, a tag typed in
	// full is not offered back.
	if got := at(10, "#DEEP"); got != "work/deep" {
		t.Fatalf("nested: %q", got)
	}
	if got := at(10, " #idea"); got != "" {
		t.Fatalf("self match offered %q", got)
	}
	// A tag typed earlier in this very buffer is already a candidate.
	if got := at(10, " #zebra and #ze"); got != "zebra" {
		t.Fatalf("live buffer tag: %q", got)
	}
}

func TestTagMenuKeysAndDismissal(t *testing.T) {
	a, buf := newTagTestApp(t, vimPrefs(), "\n")
	feed(a, vim.R('i'))
	typeKeys(a, "#pro")
	// Ctrl-E closes the menu and keeps it closed while the token grows.
	feed(a, vim.Key{Rune: 'e', Ctrl: true})
	typeKeys(a, "j")
	if a.liveTagMenu() != nil || buf.ed.Line(0) != "#proj" {
		t.Fatalf("after Ctrl-E: menu=%v line=%q", a.liveTagMenu() != nil, buf.ed.Line(0))
	}
	// A new token opens it again; Tab accepts; Ctrl-P wraps to the end.
	typeKeys(a, " #pro")
	feed(a, vim.Key{Rune: 'p', Ctrl: true}, vim.KeyTab)
	if got := buf.ed.Line(0); got != "#proj #archived-project" {
		t.Fatalf("Ctrl-P then Tab: %q", got)
	}
	// Esc leaves insert mode and takes the menu with it, as Vim's does.
	typeKeys(a, " #pro")
	feed(a, vim.KeyEsc)
	if a.liveTagMenu() != nil || buf.ed.Mode() != vim.ModeNormal {
		t.Fatalf("after Esc: menu=%v mode=%v", a.liveTagMenu() != nil, buf.ed.Mode())
	}
}

func TestCtrlXCtrlOOpensTheMenuOnDemand(t *testing.T) {
	a, buf := newTagTestApp(t, vimPrefs(), "\n")
	ctrl := func(r rune) vim.Key { return vim.Key{Rune: r, Ctrl: true} }
	feed(a, vim.R('i'))
	// Nothing typed: every tag, most used first.
	feed(a, ctrl('x'), ctrl('o'))
	if got := menuTags(a); got != "project,archived-project,Idea,prose,work/deep" {
		t.Fatalf("all tags: %q", got)
	}
	// Typing narrows a menu opened by hand, and the pick gains its '#'.
	typeKeys(a, "de")
	if got := menuTags(a); got != "Idea,work/deep" {
		t.Fatalf("narrowed: %q", got)
	}
	typeKeys(a, "e")
	feed(a, ctrl('y'))
	if got := buf.ed.Line(0); got != "#work/deep" {
		t.Fatalf("accepted: %q", got)
	}
	// Ctrl-X followed by anything else is not a completion.
	feed(a, ctrl('x'))
	typeKeys(a, "!")
	if got := buf.ed.Line(0); got != "#work/deep!" || a.liveTagMenu() != nil {
		t.Fatalf("after Ctrl-X !: %q", got)
	}
}

func TestFrontmatterTagsCompleteWithoutTheHash(t *testing.T) {
	a, buf := newTagTestApp(t, vimPrefs(), "---\ntags: [idea, ]\nrelated:\n  - x\ntags:\n  - \n---\n")
	buf.ed.SetCursor(vim.Pos{Line: 1, Col: 12})
	feed(a, vim.R('a'))
	typeKeys(a, "pro")
	if got := menuTags(a); got != "project,prose,archived-project" {
		t.Fatalf("inline list: %q", got)
	}
	feed(a, vim.Key{Name: "enter"})
	if got := buf.ed.Line(1); got != "tags: [idea, project]" {
		t.Fatalf("inline list accepted: %q", got)
	}
	feed(a, vim.KeyEsc)
	buf.ed.SetCursor(vim.Pos{Line: 5, Col: 3})
	feed(a, vim.R('a'))
	typeKeys(a, "wo")
	feed(a, vim.KeyTab)
	if got := buf.ed.Line(5); got != "  - work/deep" {
		t.Fatalf("block list accepted: %q", got)
	}
	// A list under another key is not a tag list.
	feed(a, vim.KeyEsc)
	buf.ed.SetCursor(vim.Pos{Line: 3, Col: 4})
	feed(a, vim.R('a'))
	typeKeys(a, "pro")
	if got := menuTags(a); got != "" {
		t.Fatalf("list under related: offered %q", got)
	}
}

func TestTagMenuWorksWithoutVimMode(t *testing.T) {
	prefs := vimPrefs()
	prefs.VimMode = false
	a, buf := newTagTestApp(t, prefs, "\n")
	typeKeys(a, "#wor")
	if got := menuTags(a); got != "work/deep" {
		t.Fatalf("plain editing: %q", got)
	}
	// Esc only closes the menu here: there is no mode to leave.
	feed(a, vim.KeyEsc)
	if a.liveTagMenu() != nil {
		t.Fatal("Esc should close the menu")
	}
	typeKeys(a, "k")
	if a.liveTagMenu() != nil {
		t.Fatal("a dismissed token stays quiet")
	}
	typeKeys(a, " #dee")
	feed(a, vim.Key{Name: "enter"})
	if got := buf.ed.Line(0); got != "#work #work/deep" {
		t.Fatalf("accepted: %q", got)
	}
}
