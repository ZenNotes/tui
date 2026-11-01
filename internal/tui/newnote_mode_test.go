package tui

import (
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/templates"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// A freshly created note is entered in insert mode, so it must show the
// editor even when the default pane mode is preview.
func TestCreateNoteOpensInEditMode(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.DefaultPaneMode = string(modePreview)
	a.createNote(vault.FolderInbox, "Fresh", "", nil, true)
	tb := a.activeTab()
	if tb == nil {
		t.Fatal("no active tab")
	}
	if tb.mode != modeEdit {
		t.Fatalf("new note opened in mode %q, want edit", tb.mode)
	}
}

// Every way of creating a note opens it in the editor under a preview or
// split default, as the desktop does, and leaves the default view mode and
// the remembered per-note modes alone.
func TestEveryNewNoteOpensInEditMode(t *testing.T) {
	paths := map[string]func(t *testing.T, a *App){
		":new": func(t *testing.T, a *App) {
			if _, err := a.runEx(nil, vim.ExCommand{Name: "new", Args: "Fresh"}); err != nil {
				t.Fatal(err)
			}
		},
		"quick note": func(t *testing.T, a *App) {
			a.newQuickNote()
		},
		"search palette": func(t *testing.T, a *App) {
			a.openNoteSearch("Brand new idea")
			a.overlay.handleKey(a, enterKey())
		},
		"link to a missing note": func(t *testing.T, a *App) {
			a.followLink(linkTarget{kind: "wikilink", target: "Not yet written"})
			a.overlay.handleKey(a, enterKey())
		},
		"daily note": func(t *testing.T, a *App) {
			a.openPeriodicOn(periodic.Daily, time.Now())
		},
		"weekly note": func(t *testing.T, a *App) {
			a.idx.settings.WeeklyNotes.Enabled = true
			a.openPeriodicOn(periodic.Weekly, time.Now())
		},
		"template": func(t *testing.T, a *App) {
			a.createFromTemplate(templates.Template{ID: "custom:x", Name: "X", Body: "# {{title}}\n\n"})
			typeText(a, "From template")
			a.overlay.handleKey(a, enterKey())
		},
		"task file": func(t *testing.T, a *App) {
			if err := a.newTaskFile(""); err != nil {
				t.Fatal(err)
			}
			typeText(a, "Write it")
			a.overlay.handleKey(a, enterKey())
		},
		"rollover": func(t *testing.T, a *App) {
			a.idx.settings.DailyNotes.Enabled = true
			if err := a.rolloverNow(); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, create := range paths {
		for _, def := range []paneMode{modePreview, modeSplit} {
			t.Run(name+"/"+string(def), func(t *testing.T) {
				a, _ := newFeatureTestApp(t)
				a.prefs.DefaultPaneMode = string(def)
				a.prefs.RetainViewMode = true
				create(t, a)
				tb := a.activeTab()
				if tb == nil || tb.path == "" || isVirtualPath(tb.path) {
					t.Fatalf("no note tab after creating, got %+v", tb)
				}
				if tb.mode != modeEdit {
					t.Fatalf("new note %s opened in mode %q, want edit", tb.path, tb.mode)
				}
				if a.prefs.DefaultPaneMode != string(def) {
					t.Fatalf("default view mode changed to %q", a.prefs.DefaultPaneMode)
				}
				if len(a.noteModes) != 0 {
					t.Fatalf("remembered modes changed: %v", a.noteModes)
				}
			})
		}
	}
}

// A note that is already there opens in the default view mode, even through
// the commands that would have created it.
func TestExistingNoteKeepsDefaultMode(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.idx.settings.DailyNotes.Enabled = true
	a.openPeriodicOn(periodic.Daily, time.Now())
	daily := a.activeTab().path
	a.closeTab(a.activePane, a.activePane.findTab(daily))
	a.prefs.DefaultPaneMode = string(modePreview)
	for name, open := range map[string]func(){
		"daily note": func() { a.openPeriodicOn(periodic.Daily, time.Now()) },
		"rollover":   func() { _ = a.rolloverNow() },
		"note":       func() { a.openNote("note.md", true) },
	} {
		open()
		tb := a.activeTab()
		if tb == nil {
			t.Fatalf("%s: no active tab", name)
		}
		if tb.mode != modePreview {
			t.Fatalf("%s: %s opened in mode %q, want preview", name, tb.path, tb.mode)
		}
		a.closeTab(a.activePane, a.activePane.active)
	}
}
