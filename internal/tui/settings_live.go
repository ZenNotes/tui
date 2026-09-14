package tui

import (
	"crypto/sha256"
	"fmt"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/keymaps"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/editor"
	"os"
	"reflect"
	"time"
)

type configPollMsg struct{}
type configEditedMsg struct{ err error }

func configPollCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return configPollMsg{} })
}
func (a *App) applyPreferences(p config.Prefs) {
	old := a.rawPref
	a.prefs = prefsView{Prefs: p, TabsOff: !p.TabsEnabled}
	a.rawPref = p
	a.keymap = keymaps.NewResolver(p.KeymapOverrides)
	a.theme = NewTheme(p.ThemeMode, a.systemDark)
	if a.mouseEnabled != p.TerminalMouse {
		a.mouseEnabled = p.TerminalMouse
		if p.TerminalMouse {
			a.queue(tea.EnableMouseCellMotion)
		} else {
			a.queue(tea.DisableMouse)
		}
	}
	for _, buf := range a.buffers {
		buf.ed.SetOptions(a.editorOptions())
	}
	for _, pane := range a.panes.leaves() {
		for _, t := range pane.tabs {
			if v, ok := t.view.(*tasksView); ok {
				if old.KanbanGroupBy != p.KanbanGroupBy {
					v.groupBy = p.KanbanGroupBy
				}
				if old.TasksViewMode != p.TasksViewMode {
					v.setMode(p.TasksViewMode)
				}
			}
		}
	}
	a.invalidatePreviews()
	if a.idx != nil {
		a.sidebar.rebuild(a)
		a.refreshViews()
	}
	a.layout()
}
func (a *App) reloadPreferences() error {
	p, _, err := config.LoadPrefs()
	if err != nil {
		return fmt.Errorf("config.toml: %w", err)
	}
	a.applyPreferences(p)
	data, _ := os.ReadFile(config.ConfigTomlPath())
	a.configHash = sha256.Sum256(data)
	return nil
}
func (a *App) persistPreferences() {
	if !a.prefsLoaded {
		return
	}
	a.prefs.Prefs.TabsEnabled = !a.prefs.TabsOff
	a.prefs.TerminalMouse = a.mouseEnabled
	if reflect.DeepEqual(a.rawPref, a.prefs.Prefs) {
		return
	}
	if err := config.SaveChanges(a.rawPref, a.prefs.Prefs); err != nil {
		a.applyPreferences(a.rawPref)
		a.notifyError("Setting was not saved: " + err.Error())
		return
	}
	// Read the latest file so concurrent edits to unrelated settings are retained.
	if err := a.reloadPreferences(); err != nil {
		a.notifyError(err.Error())
	}
}
func (a *App) editConfig() error {
	path := config.ConfigTomlPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := config.UpdateConfig(func(map[string]any) error { return nil }); err != nil {
			return err
		}
	}
	cmd, err := editor.Cmd("ZenNotes", path)
	if err != nil {
		return err
	}
	a.queue(tea.ExecProcess(cmd, func(err error) tea.Msg { return configEditedMsg{err} }))
	return nil
}
func (a *App) saveKeymap(id, binding string) error {
	previous := a.keymap.Overrides()[id]
	if !a.keymap.Set(id, binding) {
		return fmt.Errorf("unknown action id: %s (see :keymaps)", id)
	}
	if a.prefsLoaded {
		if err := config.SetValue("keymaps", id, binding); err != nil {
			a.keymap.Set(id, previous)
			return err
		}
		return a.reloadPreferences()
	}
	return nil
}
