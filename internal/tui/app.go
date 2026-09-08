package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/config"
	"github.com/ZenNotes/zennotescli/internal/keymaps"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// Options configure a terminal session.
type Options struct {
	Backend  backend.Backend
	Target   backend.Target
	OpenPath string
	Version  string
}

// focusTarget is which panel owns the keyboard.
type focusTarget int

const (
	focusPane focusTarget = iota
	focusSidebar
	focusOutline
	focusConnections
	focusCalendar
)

type toastExpireMsg struct{ id int }
type vaultChangedMsg struct{ paths []string }
type autosaveMsg struct{}
type sessionSaveMsg struct{}
type remotePollMsg struct{}

// App is the whole terminal UI.
type App struct {
	ctx     context.Context
	opts    Options
	backend backend.Backend
	prefs   prefsView
	rawPref config.Prefs
	keymap  *keymaps.Resolver
	theme   Theme

	width, height int
	ready         bool

	idx     *index
	loadErr error

	buffers    map[string]*noteBuffer
	noteModes  map[string]paneMode
	panes      *paneNode
	activePane *pane
	focus      focusTarget

	sidebar         *sidebarState
	sidebarOpen     bool
	sidebarWidth    int
	outline         *outlinePanel
	connections     *connectionsPanel
	calendar        *calendarPanel
	outlineOpen     bool
	connectionsOpen bool
	calendarOpen    bool
	zen             bool

	overlay  overlay
	leader   *leaderState
	navKeys  []vim.Key
	paneKeys bool

	jumps      []jumpEntry
	jumpIdx    int
	recent     []string
	closedTabs []closedTab

	message     string
	messageErr  bool
	toasts      []toast
	nextToastID int

	watcher      *vaultWatcher
	ignoredPaths map[string]time.Time
	sessionDirty bool
	sessionSaved time.Time
	quitting     bool
	program      *tea.Program
	leaderSeq    int

	pendingCmds         []tea.Cmd
	leaderHintScheduled int
	commands            []command
	styleCache          map[styleKey]lipgloss.Style
	pendingOpen         string
	imageStore          *imageStore
	diagramCache        *diagramCache
	assetPaths          map[string]string
	previewDepth        int
	previewPath         string

	systemDark    bool
	exHistory     []string
	mouse         mouseState
	menuAnchor    *point
	overlayRect   rect
	mouseEnabled  bool
	glamour       *glamourState
	glamourWarned bool
	autosaveArmed bool
	sessionArmed  bool
	pollArmed     bool
}

type jumpEntry struct {
	path string
	pos  vim.Pos
}

type toast struct {
	id    int
	text  string
	isErr bool
	until time.Time
}

// Run starts the terminal UI and blocks until it exits.
func Run(ctx context.Context, opts Options) error {
	prefs, _, err := config.LoadPrefs()
	if err != nil {
		return fmt.Errorf("config.toml: %w", err)
	}
	// The background query must happen before Bubble Tea owns the terminal.
	systemDark := true
	if strings.EqualFold(strings.TrimSpace(prefs.ThemeMode), "system") || strings.TrimSpace(prefs.ThemeMode) == "" || strings.EqualFold(prefs.ThemeMode, "auto") {
		systemDark = DetectDarkBackground()
	}
	app := newApp(ctx, opts, prefs, systemDark)
	teaOpts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx)}
	if prefs.TerminalMouse {
		teaOpts = append(teaOpts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(app, teaOpts...)
	app.program = p
	_, err = p.Run()
	app.shutdown()
	return err
}

func newApp(ctx context.Context, opts Options, prefs config.Prefs, systemDark bool) *App {
	a := &App{
		ctx:          ctx,
		opts:         opts,
		backend:      opts.Backend,
		rawPref:      prefs,
		prefs:        prefsView{Prefs: prefs},
		keymap:       keymaps.NewResolver(prefs.KeymapOverrides),
		theme:        NewTheme(prefs.ThemeMode, systemDark),
		systemDark:   systemDark,
		mouseEnabled: prefs.TerminalMouse,
		buffers:      map[string]*noteBuffer{},
		noteModes:    map[string]paneMode{},
		panes:        newPaneTree(),
		sidebarOpen:  true,
		sidebarWidth: 32,
		ignoredPaths: map[string]time.Time{},
		jumpIdx:      -1,
	}
	a.activePane = a.panes.leaves()[0]
	a.sidebar = newSidebar(a)
	a.outline = &outlinePanel{}
	a.connections = &connectionsPanel{}
	a.calendar = newCalendarPanel(time.Now())
	a.focus = focusPane
	return a
}

// prefsView wraps the preferences with the derived answers the UI asks for.
type prefsView struct {
	config.Prefs
	// TabsOff hides the tab strip for this session (:tabs).
	TabsOff bool
}

func (p prefsView) TabsEnabled() bool             { return !p.TabsOff }
func (p prefsView) KeepViewModeAcrossNotes() bool { return false }

// Init kicks off the index load and the ticker.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg { return a.loadIndexCmd()() },
	}
	if a.opts.Target.Kind == backend.KindLocal {
		w, err := startWatcher(a.opts.Target.Root)
		if err == nil {
			a.watcher = w
			cmds = append(cmds, a.watchCmd())
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) watchCmd() tea.Cmd {
	if a.watcher == nil {
		return nil
	}
	return func() tea.Msg {
		paths, ok := a.watcher.next()
		if !ok {
			return nil
		}
		return vaultChangedMsg{paths: paths}
	}
}

func (a *App) shutdown() {
	_ = a.saveAllBuffers()
	a.saveSession()
	if a.watcher != nil {
		a.watcher.close()
	}
}

// Update is the Bubble Tea message loop.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.ready = true
		a.layout()
		return a, nil
	case indexLoadedMsg:
		if m.err != nil {
			a.loadErr = m.err
			a.notifyError("Could not load the vault: " + m.err.Error())
			return a, nil
		}
		first := a.idx == nil
		a.idx = m.idx
		a.sidebar.rebuild(a)
		a.refreshViews()
		if a.pendingOpen != "" {
			if _, ok := a.noteMeta(a.pendingOpen); ok {
				a.openNote(a.pendingOpen, true)
				a.pendingOpen = ""
			}
		}
		cmds := []tea.Cmd{func() tea.Msg { return a.loadTasksCmd()() }}
		a.armRemotePoll()
		if first {
			a.restoreSession()
			if a.opts.OpenPath != "" {
				a.openPathArgument(a.opts.OpenPath)
			}
			if a.activeTab() == nil {
				a.openHome()
			}
			a.layout()
		}
		cmds = append(cmds, a.flush())
		return a, tea.Batch(cmds...)
	case tasksLoadedMsg:
		if m.err == nil && a.idx != nil {
			a.idx.tasks = m.tasks
			a.idx.tasksAt = time.Now()
			a.sidebar.rebuild(a)
			a.refreshViews()
		}
		return a, a.flush()
	case vaultChangedMsg:
		return a, tea.Batch(a.handleVaultChange(m.paths), a.watchCmd(), a.flush())
	case autosaveMsg:
		a.autosaveArmed = false
		now := time.Now()
		for _, buf := range a.autosaveDue(now) {
			if err := a.saveBuffer(buf); err != nil {
				a.notifyError("Save failed: " + err.Error())
			}
		}
		a.armAutosave()
		return a, a.flush()
	case sessionSaveMsg:
		a.sessionArmed = false
		if a.sessionDirty {
			a.saveSession()
		}
		return a, a.flush()
	case remotePollMsg:
		a.pollArmed = false
		if a.opts.Target.Kind == backend.KindRemote && a.idx != nil {
			return a, tea.Batch(func() tea.Msg { return a.loadIndexCmd()() }, a.flush())
		}
		return a, a.flush()
	case embedRenderedMsg:
		a.invalidatePreviews()
		return a, a.flush()
	case editorDoneMsg:
		a.finishExternalEdit(m)
		return a, a.flush()
	case leaderHintMsg:
		if a.leader != nil && a.leader.seq == m.seq {
			a.leader.showHints = true
		}
		return a, nil
	case toastExpireMsg:
		a.expireToasts()
		return a, a.flush()
	case tea.KeyMsg:
		if a.quitting {
			return a, nil
		}
		// Fast typing (or a paste) arrives as one message carrying several
		// runes; the engine wants them one at a time.
		if m.Type == tea.KeyRunes && len(m.Runes) > 1 && !m.Alt {
			var cmds []tea.Cmd
			for _, r := range m.Runes {
				if cmd := a.handleKey(vim.Key{Rune: r}); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return a, tea.Batch(cmds...)
		}
		return a, a.handleKey(keyFromTea(m))
	case tea.MouseMsg:
		a.handleMouse(m)
		return a, a.flush()
	}
	return a, a.flush()
}

// flush hands queued commands to Bubble Tea.
func (a *App) flush() tea.Cmd {
	if len(a.pendingCmds) == 0 {
		return nil
	}
	cmds := a.pendingCmds
	a.pendingCmds = nil
	return tea.Batch(cmds...)
}

// armAutosave schedules one wake-up for the earliest pending autosave.
func (a *App) armAutosave() {
	if a.autosaveArmed {
		return
	}
	var earliest time.Time
	for _, buf := range a.buffers {
		if buf.dirty() && !buf.saveAt.IsZero() && (earliest.IsZero() || buf.saveAt.Before(earliest)) {
			earliest = buf.saveAt
		}
	}
	if earliest.IsZero() {
		return
	}
	delay := time.Until(earliest)
	if delay < 50*time.Millisecond {
		delay = 50 * time.Millisecond
	}
	a.autosaveArmed = true
	a.queue(tea.Tick(delay, func(time.Time) tea.Msg { return autosaveMsg{} }))
}

// armRemotePoll schedules the next remote re-sync.
func (a *App) armRemotePoll() {
	if a.pollArmed || a.opts.Target.Kind != backend.KindRemote {
		return
	}
	a.pollArmed = true
	a.queue(tea.Tick(30*time.Second, func(time.Time) tea.Msg { return remotePollMsg{} }))
}

// handleVaultChange refreshes the index and reloads buffers the change
// touched, ignoring echoes of this process's own writes.
func (a *App) handleVaultChange(paths []string) tea.Cmd {
	external := false
	for _, p := range paths {
		if until, ok := a.ignoredPaths[p]; ok && time.Now().Before(until) {
			continue
		}
		external = true
		if buf, ok := a.buffers[p]; ok {
			a.reloadBuffer(buf)
		}
	}
	if !external {
		return nil
	}
	return func() tea.Msg { return a.loadIndexCmd()() }
}

// ignoreChange tells the watcher that the next event for a path is ours.
func (a *App) ignoreChange(path string) {
	a.ignoredPaths[path] = time.Now().Add(2 * time.Second)
}

// --- messages and toasts ---

func (a *App) notify(text string) {
	a.message, a.messageErr = text, false
	a.addToast(text, false)
}

func (a *App) notifyError(text string) {
	a.message, a.messageErr = text, true
	a.addToast(text, true)
}

func (a *App) addToast(text string, isErr bool) {
	a.nextToastID++
	id := a.nextToastID
	a.toasts = append(a.toasts, toast{id: id, text: text, isErr: isErr, until: time.Now().Add(4 * time.Second)})
	if len(a.toasts) > 3 {
		a.toasts = a.toasts[len(a.toasts)-3:]
	}
	a.queue(tea.Tick(4*time.Second+50*time.Millisecond, func(time.Time) tea.Msg { return toastExpireMsg{id: id} }))
}

func (a *App) expireToasts() {
	now := time.Now()
	kept := a.toasts[:0]
	for _, t := range a.toasts {
		if now.Before(t.until) {
			kept = append(kept, t)
		}
	}
	a.toasts = kept
}

// --- layout ---

func (a *App) mainRect() rect {
	top := 0
	bottom := a.height - 2 // status bar + command line
	if a.zen {
		return rect{0, 0, a.width, bottom}
	}
	// One blank column between the sidebar's divider and the pane frames,
	// and one at the right edge, so the frames do not touch other lines.
	left := 0
	if a.sidebarOpen {
		left = a.sidebarWidth + 2
	}
	right := a.width - 1
	if a.sidePanelOpen() {
		right -= a.sidePanelWidth() + 1
	}
	return rect{left, top, max(10, right-left), max(3, bottom-top)}
}

func (a *App) sidePanelOpen() bool {
	return !a.zen && (a.outlineOpen || a.connectionsOpen || a.calendarOpen)
}

func (a *App) sidePanelWidth() int {
	return min(36, max(24, a.width/4))
}

func (a *App) layout() {
	if !a.ready {
		return
	}
	a.panes.layout(a.mainRect())
	for _, buf := range a.buffers {
		buf.ed.SetViewport(a.editorRows(buf))
	}
}

// editorTextWidth is the text width of the pane showing a buffer.
func (a *App) editorTextWidth(buf *noteBuffer) int {
	for _, p := range a.panes.leaves() {
		if t := p.activeTab(); t != nil && t.path == buf.path {
			w := a.contentRect(p).w
			if t.mode == modeSplit {
				w = w / 2
			}
			return w - a.gutterWidth(buf)
		}
	}
	return a.mainRect().w - a.gutterWidth(buf)
}

func (a *App) gutterWidth(buf *noteBuffer) int {
	if a.prefs.LineNumberMode == "off" || a.prefs.LineNumberMode == "" {
		return 1
	}
	n := len(fmt.Sprint(buf.ed.LineCount()))
	return max(4, n+2)
}

func (a *App) editorRows(buf *noteBuffer) int {
	for _, p := range a.panes.leaves() {
		if t := p.activeTab(); t != nil && t.path == buf.path {
			return a.contentRect(p).h
		}
	}
	return max(1, a.mainRect().h-1)
}

func (a *App) tabBarHeight() int {
	if a.zen || !a.prefs.TabsEnabled() {
		return 0
	}
	return 1
}

// --- View ---

func (a *App) View() string {
	if !a.ready {
		return "Loading…"
	}
	main := a.mainRect()
	var columns []string
	if a.sidebarOpen && !a.zen {
		columns = append(columns, a.sidebar.render(a, a.sidebarWidth, main.h), a.renderVerticalBorder(main.h, a.focus == focusSidebar), fitBlock("", 1, main.h))
	}
	columns = append(columns, a.renderPanes(main))
	if a.sidePanelOpen() {
		columns = append(columns, fitBlock("", 1, main.h), a.renderSidePanel(a.sidePanelWidth(), main.h))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, columns...)
	body = fitBlock(body, a.width, main.h)
	screen := body + "\n" + a.renderStatusBar() + "\n" + a.renderCommandLine()
	if _, isHints := a.overlay.(*hintOverlay); isHints {
		screen = a.paintHints(screen)
	} else if m, ok := a.overlay.(*menu); ok && m.anchor != nil {
		screen, a.overlayRect = overlayPlaceAt(screen, m.render(a, a.width, a.height), m.anchor.x, m.anchor.y, a.width, a.height)
	} else if a.overlay != nil {
		screen, a.overlayRect = overlayPlace(screen, a.overlay.render(a, a.width, a.height), a.width, a.height)
	} else if a.leader != nil && a.leader.showHints {
		screen = overlayOn(screen, a.renderWhichKey(), a.width, a.height)
	}
	if len(a.toasts) > 0 {
		screen = a.renderToasts(screen)
	}
	return screen
}

func (a *App) renderVerticalBorder(h int, focused bool) string {
	style := a.theme.Border
	if focused {
		style = a.theme.BorderFocus
	}
	lines := make([]string, h)
	for i := range lines {
		lines[i] = style.Render("│")
	}
	return strings.Join(lines, "\n")
}

// renderPanes draws the split tree into a block of the main rect's size.
func (a *App) renderPanes(main rect) string {
	canvas := newCanvas(main.w, main.h)
	a.paintNode(a.panes, canvas, main)
	return canvas.String()
}

func (a *App) paintNode(n *paneNode, c *canvas, origin rect) {
	if n.leaf != nil {
		p := n.leaf
		content := a.renderPane(p, p.rect.w, p.rect.h)
		c.paint(p.rect.x-origin.x, p.rect.y-origin.y, content)
		return
	}
	a.paintNode(n.first, c, origin)
	a.paintNode(n.second, c, origin)
	// Framed panes carry their own borders, so the divider between them
	// stays a blank gutter; in zen mode a line separates the bare panes.
	if !a.zen {
		return
	}
	if n.vertical {
		x := n.second.leaves()[0].rect.x - 1 - origin.x
		for y := 0; y < origin.h; y++ {
			c.set(x, y, a.theme.Border.Render("│"))
		}
	} else {
		y := n.second.leaves()[0].rect.y - 1 - origin.y
		c.fillRow(y, a.theme.Border.Render(strings.Repeat("─", origin.w)))
	}
}

// renderPane draws one pane: the tab strip, then the content inside its
// frame (or bare, in zen mode and in panes too small for a frame).
func (a *App) renderPane(p *pane, w, h int) string {
	focused := a.focus == focusPane && p == a.activePane
	lines := []string{}
	if a.tabBarHeight() > 0 {
		lines = append(lines, a.renderTabBar(p, w, focused))
	}
	inner := a.contentRect(p)
	t := p.activeTab()
	var content string
	switch {
	case t == nil:
		content = a.renderEmptyPane(inner.w, inner.h)
	case t.view != nil:
		content = t.view.render(a, inner.w, inner.h, focused)
	default:
		content = a.renderNoteTab(p, t, inner.w, inner.h, focused)
	}
	content = fitBlock(content, inner.w, inner.h)
	if a.framed(p) {
		lines = append(lines, strings.Split(frameBox(a.theme, a.paneFrameTitle(p), content, w, h-len(lines), focused), "\n")...)
	} else {
		lines = append(lines, strings.Split(content, "\n")...)
	}
	return strings.Join(lines, "\n")
}

func (a *App) renderEmptyPane(w, h int) string {
	th := a.theme
	rows := []string{
		th.Title.Render("ZenNotes"),
		th.Dim.Render(a.vaultLabel()),
		"",
		th.KeyHint.Render(padRight(a.keyOf("global.searchNotes"), 10)) + th.Base.Render("search notes"),
		th.KeyHint.Render(padRight(a.keyOf("global.newNoteHere"), 10)) + th.Base.Render("new note"),
		th.KeyHint.Render(padRight(a.keyOf("vim.leaderPrefix")+" d", 10)) + th.Base.Render("today's daily note"),
		th.KeyHint.Render(padRight(a.keyOf("vim.leaderPrefix")+" q", 10)) + th.Base.Render("quick capture"),
		th.KeyHint.Render(padRight(a.keyOf("vim.leaderPrefix")+" x", 10)) + th.Base.Render("tasks"),
		th.KeyHint.Render(padRight(":help", 10)) + th.Base.Render("the manual"),
	}
	if len(a.recent) > 0 {
		rows = append(rows, "", th.Dim.Render("Recent"))
		for i, r := range a.recent {
			if i >= 5 {
				break
			}
			if meta, ok := a.noteMeta(r); ok {
				rows = append(rows, th.Base.Render("  "+truncateCells(meta.Title, 40)))
			}
		}
	}
	card := cardLines(th, "Welcome", rows, min(w-4, 52))
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat(" ", w)
	}
	top := max(0, (h-len(card))/3)
	for i, line := range card {
		if top+i < h {
			lines[top+i] = centerLine(line, w)
		}
	}
	return strings.Join(lines, "\n")
}

func (a *App) renderNoteTab(p *pane, t *tab, w, h int, focused bool) string {
	buf, ok := a.buffers[t.path]
	if !ok {
		return a.theme.StatusError.Render("Note is not loaded: " + t.path)
	}
	switch t.mode {
	case modePreview:
		return a.renderPreview(t, buf, w, h, focused)
	case modeSplit:
		left := w / 2
		right := w - left - 1
		editor := a.renderEditor(buf, left, h, focused)
		if t.preview == nil {
			t.preview = &previewState{}
		}
		t.preview.followLine = buf.ed.Cursor().Line
		preview := a.renderPreview(t, buf, right, h, false)
		return lipgloss.JoinHorizontal(lipgloss.Top, fitBlock(editor, left, h), a.renderVerticalBorder(h, false), fitBlock(preview, right, h))
	}
	return a.renderEditor(buf, w, h, focused)
}

// --- status bar and command line ---

func (a *App) renderStatusBar() string {
	t := a.theme
	mode := "NORMAL"
	suffix := ""
	modeStyle := t.StatusMode
	if buf := a.activeBuffer(); buf != nil && a.focus == focusPane {
		if a.prefs.VimMode {
			mode = buf.ed.Mode().Label()
		} else {
			mode = "EDIT"
		}
		suffix = buf.ed.StatusSuffix()
		if tab := a.activeTab(); tab != nil {
			switch tab.mode {
			case modePreview:
				mode = "PREVIEW"
				suffix = "Esc back to editor"
			case modeSplit:
				mode += " · SPLIT"
			}
		}
		switch mode {
		case "PREVIEW":
			modeStyle = modeStyle.Background(t.Blue)
		case "INSERT", "REPLACE":
			modeStyle = modeStyle.Background(t.Green)
		case "VISUAL", "V-LINE", "V-BLOCK":
			modeStyle = modeStyle.Background(t.Purple)
		case "COMMAND":
			modeStyle = modeStyle.Background(t.Yellow)
		}
	} else {
		switch a.focus {
		case focusSidebar:
			mode = "SIDEBAR"
		case focusOutline:
			mode = "OUTLINE"
		case focusConnections:
			mode = "LINKS"
		case focusCalendar:
			mode = "CALENDAR"
		default:
			if tab := a.activeTab(); tab != nil && tab.view != nil {
				mode = strings.ToUpper(tab.view.title())
			}
		}
		modeStyle = modeStyle.Background(t.BgSelected).Foreground(t.Fg)
	}
	if a.leader != nil {
		suffix = "SPC " + a.leader.pendingLabel()
	} else if len(a.navKeys) > 0 {
		suffix = vim.KeysString(a.navKeys)
	} else if a.paneKeys {
		suffix = "^W"
	}
	segments := []string{modeStyle.Render(mode)}
	if suffix != "" {
		segments = append(segments, t.Status.Render(" "+t.Hint.Render(suffix)))
	}
	// Note segment: title, folder, and unsaved or changed markers.
	if tab := a.activeTab(); tab != nil {
		name := a.tabTitle(tab)
		info := ""
		if buf := a.activeBuffer(); buf != nil {
			if buf.dirty() {
				name += " ●"
			}
			if buf.diskChanged {
				info += "  changed on disk"
			}
			folder, sub := a.folderOfPath(buf.path)
			where := a.folderLabel(folder)
			if sub != "" {
				where += "/" + sub
			}
			segments = append(segments, t.Status.Render("  "+t.Bold.Render(name)+t.Dim.Render("  "+where+info)))
		} else {
			segments = append(segments, t.Status.Render("  "+t.Bold.Render(name)))
		}
	}
	left := strings.Join(segments, "")
	// Right side: position, words, vault.
	right := []string{}
	if buf := a.activeBuffer(); buf != nil {
		cur := buf.ed.Cursor()
		pct := 0
		if n := buf.ed.LineCount(); n > 1 {
			pct = cur.Line * 100 / (n - 1)
		}
		right = append(right, t.Dim.Render(fmt.Sprintf("%d:%d", cur.Line+1, cur.Col+1)), t.Dim.Render(fmt.Sprintf("%d%%", pct)), t.Dim.Render(fmt.Sprintf("%d words", a.wordCount(buf))))
	}
	vaultSeg := a.vaultLabel()
	if a.opts.Target.Kind == backend.KindRemote {
		vaultSeg = "⇅ " + vaultSeg
	}
	right = append(right, t.Base.Foreground(t.Accent).Render(vaultSeg))
	rightText := strings.Join(right, t.Muted.Render("  │  "))
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(rightText) - 2
	if gap < 1 {
		rightText = truncateAnsi(rightText, max(0, a.width-lipgloss.Width(left)-2))
		gap = 1
	}
	return withBackground(padRight(left+strings.Repeat(" ", gap)+rightText+" ", a.width), t.BgStatus)
}

// wordCount counts words in a buffer, cached per version.
func (a *App) wordCount(buf *noteBuffer) int {
	if buf.wordsVersion == buf.ed.Version() && buf.wordsVersion != 0 {
		return buf.words
	}
	n := 0
	inFront := false
	for i, line := range buf.ed.Lines() {
		if i == 0 && line == "---" {
			inFront = true
			continue
		}
		if inFront {
			if line == "---" || line == "..." {
				inFront = false
			}
			continue
		}
		n += len(strings.Fields(line))
	}
	buf.words, buf.wordsVersion = n, buf.ed.Version()
	return n
}

func (a *App) vaultLabel() string {
	if a.opts.Target.Kind == backend.KindRemote {
		if a.opts.Target.Name != "" {
			return a.opts.Target.Name
		}
		return a.opts.Target.BaseURL
	}
	root := a.opts.Target.Root
	if i := strings.LastIndex(root, "/"); i >= 0 {
		return root[i+1:]
	}
	return root
}

func (a *App) renderCommandLine() string {
	t := a.theme
	if buf := a.activeBuffer(); buf != nil && a.focus == focusPane {
		if kind, text, cursor, active := buf.ed.Cmdline(); active {
			runes := []rune(text)
			before := string(runes[:min(cursor, len(runes))])
			after := ""
			if cursor < len(runes) {
				after = string(runes[cursor:])
			}
			line := string(kind) + before + t.Cursor.Render(firstCellOr(after, " ")) + restAfterFirst(after)
			if comps, idx := buf.ed.Completions(); len(comps) > 0 {
				line += "   " + a.renderWildmenu(comps, idx)
			}
			return padRight(line, a.width)
		}
		if msg, isErr := buf.ed.Message(); msg != "" {
			if isErr {
				return padRight(t.StatusError.Render(msg), a.width)
			}
			return padRight(msg, a.width)
		}
	}
	if a.message != "" {
		if a.messageErr {
			return padRight(t.StatusError.Render(a.message), a.width)
		}
		return padRight(a.message, a.width)
	}
	return padRight(t.Muted.Render(a.hintLine()), a.width)
}

func firstCellOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return string([]rune(s)[0])
}

func restAfterFirst(s string) string {
	r := []rune(s)
	if len(r) <= 1 {
		return ""
	}
	return string(r[1:])
}

func (a *App) renderWildmenu(comps []string, idx int) string {
	parts := []string{}
	for i, c := range comps {
		if i == idx {
			parts = append(parts, a.theme.SelectedFocus.Render(c))
		} else {
			parts = append(parts, a.theme.Dim.Render(c))
		}
		if i > 8 {
			parts = append(parts, "…")
			break
		}
	}
	return strings.Join(parts, " ")
}

func (a *App) renderToasts(screen string) string {
	lines := strings.Split(screen, "\n")
	row := 1
	for _, t := range a.toasts {
		style := a.theme.Overlay
		if t.isErr {
			style = style.BorderForeground(a.theme.Red)
		}
		block := style.Render(truncateCells(t.text, max(10, a.width-8)))
		blockLines := strings.Split(block, "\n")
		bw := lipgloss.Width(block)
		left := max(0, a.width-bw-2)
		for i, bl := range blockLines {
			if row+i >= len(lines)-2 {
				break
			}
			orig := lines[row+i]
			lines[row+i] = padRight(truncateAnsi(orig, left), left) + bl
		}
		row += len(blockLines)
	}
	return strings.Join(lines, "\n")
}

// --- helpers ---

func (a *App) openExternal(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		a.notifyError("Could not open " + url)
		return
	}
	a.notify("Opened " + url)
}

// currentFolderContext is where a new note goes: the folder of the active
// note, or the folder selected in the sidebar, else the inbox.
func (a *App) currentFolderContext() (vault.NoteFolder, string) {
	if a.focus == focusSidebar {
		if folder, sub, ok := a.sidebar.selectedFolder(); ok {
			return folder, sub
		}
	}
	if buf := a.activeBuffer(); buf != nil {
		return a.folderOfPath(buf.path)
	}
	if folder, sub, ok := a.sidebar.selectedFolder(); ok {
		return folder, sub
	}
	return vault.FolderInbox, ""
}

func (a *App) openPathArgument(arg string) {
	rel := vault.NormalizeRelPath(arg)
	if root := a.opts.Target.Root; root != "" && strings.HasPrefix(arg, "/") {
		if strings.HasPrefix(arg, root+"/") {
			rel = arg[len(root)+1:]
		}
	}
	if _, ok := a.noteMeta(rel); ok {
		a.openNote(rel, true)
		return
	}
	if !strings.HasSuffix(strings.ToLower(rel), ".md") {
		if _, ok := a.noteMeta(rel + ".md"); ok {
			a.openNote(rel+".md", true)
			return
		}
	}
	a.notifyError("No such note: " + arg)
}

func (a *App) markSessionDirty() {
	a.sessionDirty = true
	if a.sessionArmed {
		return
	}
	a.sessionArmed = true
	a.queue(tea.Tick(2*time.Second, func(time.Time) tea.Msg { return sessionSaveMsg{} }))
}

var _ = os.Getenv
