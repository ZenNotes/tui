package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/vim"

	"github.com/ZenNotes/tui/internal/config"
)

// sessionFile remembers open tabs and sidebar state per vault.
type sessionFile struct {
	Vaults map[string]sessionState `json:"vaults"`
}

type sessionState struct {
	BoardOrder   map[string][]string        `json:"boardOrder,omitempty"`
	Layout       *sessionPane               `json:"layout,omitempty"`
	ActivePane   int                        `json:"activePane,omitempty"`
	Positions    map[string]sessionPosition `json:"positions,omitempty"`
	Panel        string                     `json:"panel,omitempty"`
	Tabs         []sessionTab               `json:"tabs"`
	Active       int                        `json:"active"`
	Collapsed    map[string]bool            `json:"collapsed"`
	FavOpen      map[string]bool            `json:"favOpen,omitempty"`
	SidebarOpen  bool                       `json:"sidebarOpen"`
	SidebarWidth int                        `json:"sidebarWidth"`
	Recent       []string                   `json:"recent"`
	NoteModes    map[string]string          `json:"noteModes"`
	SavedAt      int64                      `json:"savedAt"`
}

type sessionTab struct {
	PreviewScroll int    `json:"previewScroll,omitempty"`
	PreviewCursor int    `json:"previewCursor,omitempty"`
	ViewMode      string `json:"viewMode,omitempty"`
	Filter        string `json:"filter,omitempty"`
	GroupBy       string `json:"groupBy,omitempty"`
	Day           string `json:"day,omitempty"`

	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Pinned bool   `json:"pinned,omitempty"`
}

type sessionPane struct {
	Tabs     []sessionTab `json:"tabs,omitempty"`
	Active   int          `json:"active,omitempty"`
	Vertical bool         `json:"vertical,omitempty"`
	Ratio    float64      `json:"ratio,omitempty"`
	First    *sessionPane `json:"first,omitempty"`
	Second   *sessionPane `json:"second,omitempty"`
}

type sessionPosition struct {
	Cursor vim.Pos `json:"cursor"`
	Scroll int     `json:"scroll"`
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
		st.Tabs = append(st.Tabs, snapshotSessionTab(t))
	}
	st.BoardOrder = a.boardOrder
	st.Active = a.activePane.active
	st.Layout = snapshotSessionPane(a.panes)
	for i, p := range a.panes.leaves() {
		if p == a.activePane {
			st.ActivePane = i
		}
	}
	st.Positions = map[string]sessionPosition{}
	for path, buf := range a.buffers {
		st.Positions[path] = sessionPosition{Cursor: buf.ed.Cursor(), Scroll: buf.ed.ScrollTop()}
	}
	switch {
	case a.commentsOpen:
		st.Panel = "comments"
	case a.outlineOpen:
		st.Panel = "outline"
	case a.connectionsOpen:
		st.Panel = "connections"
	case a.calendarOpen:
		st.Panel = "calendar"
	}
	f.Vaults[a.sessionKey()] = st
	if err := os.MkdirAll(filepath.Dir(sessionPath()), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(sessionPath()), ".session-*.tmp")
	if err != nil {
		a.notifyError("Save workspace: " + err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), sessionPath())
	}
	if err != nil {
		a.notifyError("Save workspace: " + err.Error())
		return
	}
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
	a.boardOrder = st.BoardOrder
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
	a.sidebarOpen = st.SidebarOpen
	layout := st.Layout
	if layout == nil {
		layout = &sessionPane{Tabs: st.Tabs, Active: st.Active}
	}
	a.panes = a.restoreSessionPane(layout, 0)
	leaves := a.panes.leaves()
	a.activePane = leaves[max(0, min(st.ActivePane, len(leaves)-1))]
	for path, pos := range st.Positions {
		if buf := a.buffers[path]; buf != nil {
			buf.ed.SetCursor(pos.Cursor)
			buf.ed.SetScrollTop(pos.Scroll)
		}
	}
	a.commentsOpen = st.Panel == "comments"
	if a.commentsOpen {
		a.comments = &commentsPanel{}
		a.comments.refresh(a)
	}
	a.outlineOpen, a.connectionsOpen, a.calendarOpen = st.Panel == "outline", st.Panel == "connections", st.Panel == "calendar"
	a.focus = focusPane
	a.sessionDirty = false
	a.layout()
}

func snapshotSessionPane(n *paneNode) *sessionPane {
	if n == nil {
		return nil
	}
	if n.leaf == nil {
		return &sessionPane{Vertical: n.vertical, Ratio: n.ratio, First: snapshotSessionPane(n.first), Second: snapshotSessionPane(n.second)}
	}
	s := &sessionPane{Active: n.leaf.active}
	for _, t := range n.leaf.tabs {
		s.Tabs = append(s.Tabs, snapshotSessionTab(t))
	}
	return s
}

func (a *App) restoreSessionPane(s *sessionPane, depth int) *paneNode {
	if s == nil || depth > 10 {
		return newPaneTree()
	}
	if s.First != nil && s.Second != nil {
		ratio := s.Ratio
		if ratio < 0.1 || ratio > 0.9 {
			ratio = 0.5
		}
		return &paneNode{vertical: s.Vertical, ratio: ratio, first: a.restoreSessionPane(s.First, depth+1), second: a.restoreSessionPane(s.Second, depth+1)}
	}
	n := newPaneTree()
	a.activePane = n.leaf
	selected := ""
	if s.Active >= 0 && s.Active < len(s.Tabs) {
		selected = s.Tabs[s.Active].Path
	}
	for _, saved := range s.Tabs {
		if len(n.leaf.tabs) >= 200 {
			break
		}
		if isVirtualPath(saved.Path) {
			a.openVirtualByPath(saved.Path)
		} else {
			if _, ok := a.noteMeta(saved.Path); !ok && !strings.HasPrefix(saved.Path, ".zennotes/templates/") {
				continue
			}
			a.openNoteQuiet(saved.Path)
		}
		tab := n.leaf.activeTab()
		if tab == nil || tab.path != saved.Path {
			continue
		}
		tab.pinned = saved.Pinned
		restoreSessionTab(a, tab, saved)
		if tab.view != nil {
			tab.mode = paneMode(saved.Mode)
		} else {
			tab.mode = paneMode(saved.Mode)
			if tab.mode != modeEdit && tab.mode != modeSplit && tab.mode != modePreview {
				tab.mode = modeEdit
			}
			if tab.mode != modeEdit && tab.preview == nil {
				tab.preview = &previewState{}
			}
		}
	}
	if idx := n.leaf.findTab(selected); idx >= 0 {
		n.leaf.active = idx
	} else {
		n.leaf.active = max(0, min(s.Active, len(n.leaf.tabs)-1))
	}
	a.reorderPinned(n.leaf, n.leaf.activeTab())
	return n
}

func snapshotSessionTab(t *tab) sessionTab {
	s := sessionTab{Path: t.path, Mode: string(t.mode), Pinned: t.pinned}
	if t.preview != nil {
		s.PreviewScroll = t.preview.scroll
		s.PreviewCursor = t.preview.cursor
	}
	switch v := t.view.(type) {
	case *tasksView:
		s.ViewMode = v.mode
		s.Filter = v.filter
		s.GroupBy = v.groupBy
		s.Day = v.day.Format("2006-01-02")
	case *noteListView:
		s.Filter = v.filter
	case *helpView:
		s.Filter = v.filter
	case *settingsView:
		s.Filter = v.filter
	case *filesView:
		s.Filter = v.filter
		s.ViewMode = v.sort
	case *templatesView:
		s.Filter = v.filter
	}
	return s
}
func restoreSessionTab(a *App, t *tab, s sessionTab) {
	if s.PreviewScroll > 0 || s.PreviewCursor > 0 {
		t.preview = &previewState{scroll: max(0, s.PreviewScroll), cursor: max(0, s.PreviewCursor)}
	}
	switch v := t.view.(type) {
	case *tasksView:
		if s.ViewMode != "" {
			v.setMode(s.ViewMode)
		}
		v.filter = s.Filter
		if s.GroupBy != "" {
			v.groupBy = s.GroupBy
		}
		if day, err := time.Parse("2006-01-02", s.Day); err == nil {
			v.day = day
			v.month = time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
		}
		v.refresh(a)
	case *noteListView:
		v.filter = s.Filter
		v.refresh(a)
	case *helpView:
		v.filter = s.Filter
		v.refresh(a)
	case *settingsView:
		v.filter = s.Filter
		v.refresh(a)
	case *filesView:
		v.filter = s.Filter
		v.sort = s.ViewMode
	case *templatesView:
		v.filter = s.Filter
		v.refresh(a)
	}
}
