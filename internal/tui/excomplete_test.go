package tui

import (
	"testing"

	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

func testAppWithNotes() *App {
	a := &App{keymap: keymaps.NewResolver(nil), theme: buildTheme(true), prefs: prefsView{}}
	a.idx = &index{byPath: map[string]vault.NoteMeta{}}
	for _, title := range []string{"Launch Plan", "Reading", "Roadmap"} {
		m := vault.NoteMeta{Path: "inbox/" + title + ".md", Title: title, Folder: vault.FolderInbox}
		a.idx.notes = append(a.idx.notes, m)
		a.idx.byPath[m.Path] = m
	}
	a.idx.folders = []vault.FolderEntry{{Folder: vault.FolderInbox, Subpath: "Projects"}}
	return a
}

func TestCompleteExCommandsAndArguments(t *testing.T) {
	a := testAppWithNotes()
	keep, cands := a.completeEx("ta")
	if keep != "" || !contains(join(cands), "tabmove") {
		t.Fatalf("command completion: keep=%q cands=%v", keep, cands)
	}
	keep, cands = a.completeEx("e Lau")
	if keep != "e " || len(cands) != 3 || cands[0] != "Launch Plan" {
		t.Fatalf("title completion: keep=%q cands=%v", keep, cands)
	}
	keep, cands = a.completeEx("move in")
	if keep != "move " || !contains(join(cands), "inbox/Projects") {
		t.Fatalf("folder completion: keep=%q cands=%v", keep, cands)
	}
	if _, cands := a.completeEx("s/foo/bar/ g"); cands != nil {
		t.Fatal("unknown commands get no argument candidates")
	}
	if keep, cands := a.completeEx("s/foo"); keep != "" || len(cands) == 0 {
		t.Fatal("without a space the command name itself is being completed")
	}
}

func TestExPromptTabCyclesLikeVim(t *testing.T) {
	a := testAppWithNotes()
	p := &exPrompt{input: newTextInput("e R")}
	p.handleKey(a, vim.KeyTab)
	if p.input.String() != "e Reading" {
		t.Fatalf("first tab: %q", p.input.String())
	}
	p.handleKey(a, vim.KeyTab)
	if p.input.String() != "e Roadmap" {
		t.Fatalf("second tab: %q", p.input.String())
	}
	p.handleKey(a, vim.KeyTab)
	if p.input.String() != "e R" {
		t.Fatalf("cycling should return to the typed text: %q", p.input.String())
	}
	p.handleKey(a, vim.KeyShiftTab)
	if p.input.String() != "e Roadmap" {
		t.Fatalf("shift+tab: %q", p.input.String())
	}
	p.handleKey(a, vim.R('x'))
	if p.completions != nil {
		t.Fatal("typing resets the completion cycle")
	}
}

func join(list []string) string {
	out := ""
	for _, s := range list {
		out += s + "\n"
	}
	return out
}
