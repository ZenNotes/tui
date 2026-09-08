package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ZenNotes/tui/internal/config"
)

// sessionFile remembers open tabs and sidebar state per vault.
type sessionFile struct {
	Vaults map[string]sessionState `json:"vaults"`
}

type sessionState struct {
	Tabs         []sessionTab      `json:"tabs"`
	Active       int               `json:"active"`
	Collapsed    map[string]bool   `json:"collapsed"`
	FavOpen      map[string]bool   `json:"favOpen,omitempty"`
	SidebarOpen  bool              `json:"sidebarOpen"`
	SidebarWidth int               `json:"sidebarWidth"`
	Recent       []string          `json:"recent"`
	NoteModes    map[string]string `json:"noteModes"`
	SavedAt      int64             `json:"savedAt"`
}

type sessionTab struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Pinned bool   `json:"pinned,omitempty"`
}

func sessionPath() string {
	return filepath.Join(config.UserDataDir(), "zennotes.tui-session.json")
}

func (a *App) sessionKey() string {
	if a.opts.Target.BaseURL != "" {
		return a.opts.Target.BaseURL
	}
	return a.opts.Target.Root
}

func readSessionFile() sessionFile {
	var f sessionFile
	data, err := os.ReadFile(sessionPath())
	if err == nil {
		_ = json.Unmarshal(data, &f)
	}
	if f.Vaults == nil {
		f.Vaults = map[string]sessionState{}
	}
	return f
}

func (a *App) saveSession() {
	f := readSessionFile()
	st := sessionState{Collapsed: a.sidebar.collapsed, FavOpen: a.sidebar.favOpen, SidebarOpen: a.sidebarOpen, SidebarWidth: a.sidebarWidth, Recent: a.recent, NoteModes: map[string]string{}, SavedAt: time.Now().UnixMilli()}
	for p, m := range a.noteModes {
		st.NoteModes[p] = string(m)
	}
	for _, t := range a.activePane.tabs {
		st.Tabs = append(st.Tabs, sessionTab{Path: t.path, Mode: string(t.mode), Pinned: t.pinned})
	}
	st.Active = a.activePane.active
	f.Vaults[a.sessionKey()] = st
	if err := os.MkdirAll(filepath.Dir(sessionPath()), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(sessionPath(), data, 0o644)
	a.sessionDirty = false
	a.sessionSaved = time.Now()
}

// restoreSession reopens the previous tabs of this vault.
func (a *App) restoreSession() {
	f := readSessionFile()
	st, ok := f.Vaults[a.sessionKey()]
	if !ok {
		return
	}
	if st.Collapsed != nil {
		a.sidebar.collapsed = st.Collapsed
	}
	if st.FavOpen != nil {
		a.sidebar.favOpen = st.FavOpen
	}
	if st.Collapsed != nil || st.FavOpen != nil {
		a.sidebar.rebuild(a)
	}
	if st.SidebarWidth >= 20 && st.SidebarWidth <= 60 {
		a.sidebarWidth = st.SidebarWidth
	}
	a.recent = nil
	for _, r := range st.Recent {
		if _, ok := a.noteMeta(r); ok {
			a.recent = append(a.recent, r)
		}
	}
	for p, m := range st.NoteModes {
		a.noteModes[p] = paneMode(m)
	}
	for _, t := range st.Tabs {
		if isVirtualPath(t.Path) {
			a.openVirtualByPath(t.Path)
			continue
		}
		if _, ok := a.noteMeta(t.Path); !ok {
			continue
		}
		a.openNoteQuiet(t.Path)
		if tab := a.activePane.activeTab(); tab != nil {
			if t.Mode != "" {
				tab.mode = paneMode(t.Mode)
				if tab.mode != modeEdit {
					tab.preview = &previewState{}
				}
			}
			tab.pinned = t.Pinned
		}
	}
	if st.Active >= 0 && st.Active < len(a.activePane.tabs) {
		a.activePane.active = st.Active
	}
	a.reorderPinned(a.activePane, a.activePane.activeTab())
	a.focus = focusPane
}
