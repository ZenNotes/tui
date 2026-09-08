package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/config"
	"github.com/ZenNotes/zennotescli/internal/periodic"
	"github.com/ZenNotes/zennotescli/internal/remote"
	"github.com/ZenNotes/zennotescli/internal/templates"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// The desktop's command palette, ported. Everything here mirrors an entry
// of app-core's commands.ts that makes sense without a window: vault
// switching, task actions at the cursor, paths and reveal, tab helpers,
// files, templates, and the preference toggles. What stays behind is
// GUI-bound (zoom, floating windows, drawings, PDF export, comments) or
// belongs to the desktop's own installer.

const (
	appWebsiteURL  = "https://zennotes.org"
	appDiscordURL  = "https://discord.gg/W4fWzapKS6"
	appRepoURL     = "https://github.com/ZenNotes/zennotes"
	appReleasesURL = "https://github.com/ZenNotes/zennotes/releases/latest"
	appIssuesURL   = "https://github.com/ZenNotes/zennotes/issues"
)

func (a *App) parityCommands() []command {
	return []command{
		{names: []string{"vault", "vaults"}, title: "Switch vault…", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			arg := strings.TrimSpace(cmd.Args)
			if arg == "" {
				a.openVaultSwitcher()
				return nil
			}
			a.switchByText(arg)
			return nil
		}},
		{names: []string{"server", "connect"}, title: "Connect to remote vault…", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			arg := strings.TrimSpace(cmd.Args)
			if arg == "" {
				a.openServerPicker()
				return nil
			}
			// A saved name switches; anything else goes through the connect
			// flow, which verifies the token before the app changes vaults.
			if t, ok := backend.TargetForWorkspace(config.LoadWorkspaces(), arg, ""); ok && t.Kind == backend.KindRemote {
				a.switchTarget(t)
				return nil
			}
			a.connectServerFlow(arg)
			return nil
		}},
		{names: []string{"local"}, title: "Switch to local vault", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			arg := strings.TrimSpace(cmd.Args)
			var target backend.Target
			var err error
			if arg != "" {
				target, err = backend.ResolveVaultTarget(arg, "")
			} else {
				var root string
				root, err = config.ResolveVaultRoot("")
				target = backend.Target{Kind: backend.KindLocal, Root: root}
			}
			if err != nil {
				return err
			}
			if target.Kind != backend.KindLocal {
				return fmt.Errorf("%s is a server; :server connects to one", arg)
			}
			a.switchTarget(target)
			return nil
		}},
		{names: []string{"rollover"}, title: "Roll over unfinished tasks to today", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			return a.rolloverNow()
		}},
		{names: []string{"copypath", "yankpath"}, title: "Copy note path", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			return a.copyPathCommand(buf, strings.TrimSpace(cmd.Args))
		}},
		{names: []string{"reveal", "finder"}, title: "Reveal note in file manager", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			return a.revealCommand(buf, strings.TrimSpace(cmd.Args))
		}},
		{names: []string{"forward", "fwd"}, title: "Forward task to today's note", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			t, err := a.taskAtCursor(buf)
			if err != nil {
				return err
			}
			a.forwardTask(t)
			return nil
		}},
		{names: []string{"inprogress", "start", "wip"}, title: "Mark task in progress", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			return a.setTaskStateAtCursor(buf, "inprogress")
		}},
		{names: []string{"cancel", "cancelled"}, title: "Cancel task", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			return a.setTaskStateAtCursor(buf, "cancel")
		}},
		{names: []string{"taskfile", "tasknote"}, title: "New task in folder…", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.newTaskFile(strings.TrimSpace(cmd.Args))
		}},
		{names: []string{"tabcloseright", "tabcr"}, title: "Close tabs to the right", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.closeTabsRight()
			return nil
		}},
		{names: []string{"tabmenu"}, title: "Open active tab menu", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openActiveTabMenu()
			return nil
		}},
		{names: []string{"unarchive"}, title: "Unarchive note", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			if folder, _ := a.folderOfPath(buf.path); folder != vault.FolderArchive {
				return fmt.Errorf("this note is not archived")
			}
			a.unarchiveNote(buf.path)
			return nil
		}},
		{names: []string{"purge", "deleteforever"}, title: "Delete note permanently", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			if folder, _ := a.folderOfPath(buf.path); folder != vault.FolderTrash {
				return fmt.Errorf("only notes in the trash can be deleted permanently; :delete moves this one there first")
			}
			a.deleteNoteForever(buf.path)
			return nil
		}},
		{names: []string{"assets", "files"}, title: "Go to files", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openAssetsPicker()
			return nil
		}},
		{names: []string{"ref", "reference"}, title: "Pin active note as reference", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			a.openReference(buf.path)
			return nil
		}},
		{names: []string{"notesort", "sortnotes"}, title: "Sort notes in the sidebar", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			arg := strings.TrimSpace(cmd.Args)
			if arg == "" {
				a.openNoteSortMenu()
				return nil
			}
			return a.setNoteSort(arg)
		}},
		{names: []string{"donestyle"}, title: "Completed tasks style", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			arg := strings.TrimSpace(cmd.Args)
			if arg == "" {
				a.openDoneStyleMenu()
				return nil
			}
			return a.setDoneStyle(arg)
		}},
		{names: []string{"whichkey"}, title: "Toggle leader key hints", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.prefs.WhichKeyHints = !a.prefs.WhichKeyHints
			a.notify(onOff("Leader key hints", a.prefs.WhichKeyHints) + " (vim.which_key_hints in config.toml keeps it)")
			return nil
		}},
		{names: []string{"tabs"}, title: "Toggle tabs", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.prefs.TabsOff = !a.prefs.TabsOff
			a.layout()
			a.notify(onOff("Tab strip", !a.prefs.TabsOff))
			return nil
		}},
		{names: []string{"quickdate"}, title: "Toggle quick note date titles", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.prefs.QuickNoteDateTitle = !a.prefs.QuickNoteDateTitle
			a.notify(onOff("Quick note date titles", a.prefs.QuickNoteDateTitle) + " (view.quick_note_date_title in config.toml keeps it)")
			return nil
		}},
		{names: []string{"website"}, title: "Open ZenNotes website", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openExternal(appWebsiteURL)
			return nil
		}},
		{names: []string{"discord"}, title: "Join ZenNotes Discord", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openExternal(appDiscordURL)
			return nil
		}},
		{names: []string{"github", "repo"}, title: "Open GitHub repository", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openExternal(appRepoURL)
			return nil
		}},
		{names: []string{"releases", "release"}, title: "View latest release", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openExternal(appReleasesURL)
			return nil
		}},
		{names: []string{"issue", "bug"}, title: "Report an issue", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openExternal(appIssuesURL)
			return nil
		}},
	}
}

func onOff(what string, on bool) string {
	if on {
		return what + " on"
	}
	return what + " off"
}

// --- vaults ---

// switchTarget rebinds the whole app to another vault or server without
// leaving the terminal: buffers are saved and the session stored under
// the old vault's key, then every piece of per-vault state starts over
// and the index loads, which restores the new vault's session.
func (a *App) switchTarget(target backend.Target) {
	if a.sameTarget(target) {
		a.notify("Already on " + a.vaultLabel())
		return
	}
	if err := a.saveAllBuffers(); err != nil {
		a.notifyError("Save failed: " + err.Error())
		return
	}
	// A server is checked before anything is torn down, so a wrong URL or
	// token leaves the current vault in place with a plain message.
	if target.Kind == backend.KindRemote {
		if target.AuthToken == "" {
			a.connectServerFlow(target.BaseURL)
			return
		}
		if _, err := remote.NewClient(target.BaseURL, target.AuthToken).GetCurrentVault(a.ctx); err != nil {
			switch remote.StatusOf(err) {
			case 401, 403:
				a.notifyError(target.BaseURL + " rejected the stored token; :server " + target.BaseURL + " asks for a new one")
				_ = config.DeleteToken(target.BaseURL)
			case 0:
				a.notifyError(remote.ConnectionErrorMessage(target.BaseURL, err))
			default:
				a.notifyError(err.Error())
			}
			return
		}
	}
	b, err := backend.New(target, backend.Options{SyncTitleHeading: a.prefs.SyncTitleHeadingOnRename})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.saveSession()
	if a.watcher != nil {
		a.watcher.close()
		a.watcher = nil
	}
	a.opts.Target = target
	a.opts.Backend = b
	a.backend = b
	a.buffers = map[string]*noteBuffer{}
	a.noteModes = map[string]paneMode{}
	a.panes = newPaneTree()
	a.activePane = a.panes.leaves()[0]
	a.idx = nil
	a.recent = nil
	a.jumps = nil
	a.jumpIdx = -1
	a.closedTabs = nil
	a.ignoredPaths = map[string]time.Time{}
	a.sidebar = newSidebar(a)
	a.outline = &outlinePanel{}
	a.connections = &connectionsPanel{}
	a.calendar = newCalendarPanel(time.Now())
	a.outlineOpen, a.connectionsOpen, a.calendarOpen = false, false, false
	a.focus = focusPane
	a.overlay = nil
	a.leader = nil
	a.pendingOpen = ""
	a.layout()
	if target.Kind == backend.KindLocal {
		if w, err := startWatcher(target.Root); err == nil {
			a.watcher = w
			a.queue(a.watchCmd())
		}
	}
	a.refreshIndex()
	if err := backend.RememberTarget(target); err != nil {
		a.notifyError("Could not save the workspace list: " + err.Error())
	}
	a.notify("Switched to " + b.Label())
}

func (a *App) sameTarget(t backend.Target) bool {
	cur := a.opts.Target
	if t.Kind != cur.Kind {
		return false
	}
	if t.Kind == backend.KindRemote {
		return strings.EqualFold(strings.TrimRight(t.BaseURL, "/"), strings.TrimRight(cur.BaseURL, "/"))
	}
	return filepath.Clean(t.Root) == filepath.Clean(cur.Root)
}

// openVaultSwitcher lists every vault and server zn or the desktop app
// knows, the current one marked, with entries to add a folder or connect
// to a server; a typed path or URL that matches nothing is used as-is.
func (a *App) openVaultSwitcher() {
	items := []paletteItem{}
	seenRoot := map[string]bool{}
	seenURL := map[string]bool{}
	current := func(t backend.Target) string {
		if a.sameTarget(t) {
			return "current"
		}
		if t.Kind == backend.KindRemote {
			return "server"
		}
		return "local"
	}
	ws := config.LoadWorkspaces()
	for _, v := range ws.Vaults {
		seenRoot[filepath.Clean(v.Root)] = true
		t := backend.Target{Kind: backend.KindLocal, Root: v.Root}
		items = append(items, paletteItem{label: v.Name, detail: v.Root, hint: current(t), id: "local:" + v.Root, data: t})
	}
	for _, s := range ws.Servers {
		seenURL[strings.ToLower(s.URL)] = true
		t := backend.Target{Kind: backend.KindRemote, Name: s.Name, BaseURL: s.URL, AuthToken: backend.ResolveAuthTokenFor(s.URL, "", "")}
		items = append(items, paletteItem{label: s.Name, detail: s.URL, hint: current(t), id: "server:" + s.URL, data: t})
	}
	for _, v := range config.KnownVaults() {
		if seenRoot[filepath.Clean(v.Root)] {
			continue
		}
		t := backend.Target{Kind: backend.KindLocal, Root: v.Root}
		items = append(items, paletteItem{label: v.Name, detail: v.Root, hint: current(t), id: "local:" + v.Root, data: t})
	}
	for _, p := range config.RemoteProfiles() {
		if seenURL[strings.ToLower(p.BaseURL)] {
			continue
		}
		t := backend.Target{Kind: backend.KindRemote, Name: p.Name, BaseURL: p.BaseURL, AuthToken: backend.ResolveAuthTokenFor(p.BaseURL, "", p.AuthToken)}
		items = append(items, paletteItem{label: p.Name, detail: p.BaseURL, hint: current(t), id: "server:" + p.BaseURL, data: t})
	}
	items = append(items,
		paletteItem{label: "＋ Add a folder as a vault…", id: actionAddVault},
		paletteItem{label: "＋ Connect to a server…", id: actionConnect},
	)
	p := &palette{title: "Switch vault", placeholder: "Vault or server; a path or URL works too", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) {
		switch it.id {
		case actionAddVault:
			a.addVaultFlow()
		case actionConnect:
			a.connectServerFlow("")
		default:
			a.switchTarget(it.data.(backend.Target))
		}
	}
	p.onEmpty = func(a *App, query string) { a.switchByText(query) }
	p.emptyHint = "Nothing saved matches. Enter opens the typed path or URL."
	a.overlay = p
}

const (
	actionAddVault = "\x00add-vault"
	actionConnect  = "\x00connect"
)

// openServerPicker lists saved servers, or starts the connect flow when
// none is saved yet.
func (a *App) openServerPicker() {
	items := []paletteItem{}
	seenURL := map[string]bool{}
	for _, s := range config.LoadWorkspaces().Servers {
		seenURL[strings.ToLower(s.URL)] = true
		items = append(items, paletteItem{label: s.Name, detail: s.URL, hint: "server", data: backend.Target{Kind: backend.KindRemote, Name: s.Name, BaseURL: s.URL, AuthToken: backend.ResolveAuthTokenFor(s.URL, "", "")}})
	}
	for _, p := range config.RemoteProfiles() {
		if seenURL[strings.ToLower(p.BaseURL)] {
			continue
		}
		items = append(items, paletteItem{label: p.Name, detail: p.BaseURL, hint: "server", data: backend.Target{Kind: backend.KindRemote, Name: p.Name, BaseURL: p.BaseURL, AuthToken: backend.ResolveAuthTokenFor(p.BaseURL, "", p.AuthToken)}})
	}
	if len(items) == 0 {
		a.connectServerFlow("")
		return
	}
	items = append(items, paletteItem{label: "＋ Connect to a new server…", id: actionConnect})
	pal := &palette{title: "Connect to remote vault", placeholder: "Server; a URL works too", items: items, filtered: items}
	pal.onSelect = func(a *App, it paletteItem) {
		if it.id == actionConnect {
			a.connectServerFlow("")
			return
		}
		a.switchTarget(it.data.(backend.Target))
	}
	pal.onEmpty = func(a *App, query string) { a.connectServerFlow(query) }
	pal.emptyHint = "No saved server matches. Enter connects to the typed URL."
	a.overlay = pal
}

// connectServerFlow asks for a URL (unless given) and, when no token is
// known for it, the token, then verifies, saves both and switches.
func (a *App) connectServerFlow(rawURL string) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		a.promptFor("Connect to server", "", "URL like https://notes.example.com or localhost:7878", func(a *App, text string) {
			if strings.TrimSpace(text) != "" {
				a.connectServerFlow(text)
			}
		})
		return
	}
	baseURL := remote.NormalizeBaseURL(rawURL)
	if saved := config.LoadWorkspaces().FindServer(rawURL); saved != nil {
		baseURL = saved.URL
	}
	finish := func(a *App, token string) {
		client := remote.NewClient(baseURL, token)
		if _, err := client.GetCurrentVault(a.ctx); err != nil {
			switch remote.StatusOf(err) {
			case 401, 403:
				a.notifyError(baseURL + " rejected the token; it must match the server's ZENNOTES_AUTH_TOKEN")
			case 0:
				a.notifyError(remote.ConnectionErrorMessage(baseURL, err))
			default:
				a.notifyError(err.Error())
			}
			return
		}
		ws := config.LoadWorkspaces()
		entry := ws.AddServer("", baseURL)
		ws.Default = entry.Name
		if err := config.SaveWorkspaces(ws); err != nil {
			a.notifyError(err.Error())
			return
		}
		if err := config.SaveToken(baseURL, token); err != nil {
			a.notifyError(err.Error())
			return
		}
		a.switchTarget(backend.Target{Kind: backend.KindRemote, Name: entry.Name, BaseURL: baseURL, AuthToken: token})
	}
	if token := backend.ResolveAuthTokenFor(baseURL, "", ""); token != "" {
		finish(a, token)
		return
	}
	a.promptSecret("Token for "+baseURL, "The server's ZENNOTES_AUTH_TOKEN; stored for next time", func(a *App, text string) {
		if strings.TrimSpace(text) == "" {
			a.notifyError("A token is required")
			return
		}
		finish(a, strings.TrimSpace(text))
	})
}

// addVaultFlow remembers a folder as a vault and switches to it.
func (a *App) addVaultFlow() {
	a.promptFor("Add a vault", "", "Folder of Markdown notes; ~ works", func(a *App, text string) {
		raw := strings.TrimSpace(text)
		if raw == "" {
			return
		}
		root, err := filepath.Abs(config.ExpandHome(raw))
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			a.notifyError(root + " is not a folder")
			return
		}
		a.switchTarget(backend.Target{Kind: backend.KindLocal, Root: root})
	})
}

func (a *App) switchByText(text string) {
	var target backend.Target
	var err error
	if backend.LooksLikeServerURL(text) {
		a.connectServerFlow(text)
		return
	}
	target, err = backend.ResolveVaultTarget(text, "")
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.switchTarget(target)
}

// --- daily rollover ---

// rolloverNow is the manual rollover: today's note is created when it is
// missing, the open tasks of the newest earlier daily note move into it,
// and it opens.
func (a *App) rolloverNow() error {
	if a.idx == nil || !a.idx.settings.DailyNotes.Enabled {
		return fmt.Errorf("daily notes are off for this vault (dailyNotes.enabled in vault.json)")
	}
	today := time.Now()
	_, _, title, rel := a.periodicLocation(periodic.Daily, today)
	if _, ok := a.noteMeta(rel); !ok {
		body := a.periodicBody(periodic.Daily, title, today)
		meta, err := a.backend.CreateNote(a.ctx, vault.FolderInbox, title, "", &body)
		if err != nil {
			return err
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		rel = meta.Path
	}
	n := a.rolloverInto(rel, today)
	if n == 0 {
		a.notify("Nothing to roll over")
	}
	a.openNote(rel, true)
	return nil
}

// --- paths and reveal ---

// copyPathCommand copies the active note's vault-relative path, its
// absolute path with `abs`, or the selected folder's path when the
// sidebar has one under the cursor.
func (a *App) copyPathCommand(buf *noteBuffer, arg string) error {
	rel := ""
	if a.focus == focusSidebar {
		if folder, sub, ok := a.sidebar.selectedFolder(); ok {
			rel = a.vaultRelDir(folder, sub)
		}
	}
	if rel == "" {
		if buf == nil {
			return fmt.Errorf("no note is active")
		}
		rel = buf.path
	}
	if arg == "abs" || arg == "absolute" {
		if a.opts.Target.Kind != backend.KindLocal {
			return fmt.Errorf("a server vault has no local path; the vault-relative path is %s", rel)
		}
		a.copyText(filepath.Join(a.backend.Root(), filepath.FromSlash(rel)))
		return nil
	}
	a.copyText(rel)
	return nil
}

// revealCommand shows the note, or with `vault` the vault root, in the
// system file manager. Local vaults only.
func (a *App) revealCommand(buf *noteBuffer, arg string) error {
	if a.opts.Target.Kind != backend.KindLocal {
		return fmt.Errorf("reveal needs a local vault")
	}
	root := a.backend.Root()
	target := ""
	switch {
	case arg == "vault" || arg == "root":
		target = root
	case buf != nil:
		target = filepath.Join(root, filepath.FromSlash(buf.path))
	default:
		return fmt.Errorf("no note is active; :reveal vault shows the vault root")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if target == root {
			cmd = exec.Command("open", target)
		} else {
			cmd = exec.Command("open", "-R", target)
		}
	case "windows":
		if target == root {
			cmd = exec.Command("explorer", target)
		} else {
			cmd = exec.Command("explorer", "/select,"+target)
		}
	default:
		dir := target
		if target != root {
			dir = filepath.Dir(target)
		}
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// --- tasks at the cursor ---

// taskAtCursor is the inline task on the editor's cursor line.
func (a *App) taskAtCursor(buf *noteBuffer) (vault.Task, error) {
	if buf == nil {
		return vault.Task{}, fmt.Errorf("no note is active")
	}
	meta, _ := a.noteMeta(buf.path)
	title := meta.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(buf.path), ".md")
	}
	folder := meta.Folder
	if folder == "" {
		folder, _ = a.folderOfPath(buf.path)
	}
	tasks := vault.ParseTasks(buf.path, title, folder, buf.ed.Text(), vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectApp})
	line := buf.ed.Cursor().Line
	for _, t := range tasks {
		if t.Kind == "" && t.LineNumber == line {
			return t, nil
		}
	}
	return vault.Task{}, fmt.Errorf("put the cursor on a task line")
}

// setTaskStateAtCursor flips the cursor line's task between open and in
// progress, or open and cancelled.
func (a *App) setTaskStateAtCursor(buf *noteBuffer, state string) error {
	t, err := a.taskAtCursor(buf)
	if err != nil {
		return err
	}
	err = a.mutateNote(buf.path, func(body string) (string, bool) {
		var next string
		switch state {
		case "cancel":
			next = vault.SetTaskCancelled(body, t.TaskIndex, !t.Cancelled)
		default:
			next = vault.SetTaskInProgress(body, t.TaskIndex, !t.InProgress)
		}
		return next, next != body
	})
	if err != nil {
		return err
	}
	a.afterTaskChange()
	return nil
}

// --- tabs ---

// closeTabsRight closes every closable tab after the active one.
func (a *App) closeTabsRight() {
	p := a.activePane
	if p == nil {
		return
	}
	closed := 0
	for i := len(p.tabs) - 1; i > p.active; i-- {
		if p.tabs[i].pinned {
			continue
		}
		before := len(p.tabs)
		a.closeTab(p, i)
		if len(p.tabs) < before {
			closed++
		}
	}
	if closed == 0 {
		a.notify("No tabs to the right")
	}
}

// openActiveTabMenu opens the active tab's menu from the keyboard, anchored
// under the tab the way a right click would.
func (a *App) openActiveTabMenu() {
	p := a.activePane
	if p == nil || p.activeTab() == nil {
		return
	}
	if p.active < len(p.tabSpans) && p.tabSpans[p.active][0] >= 0 {
		a.menuAnchor = &point{x: p.rect.x + p.tabSpans[p.active][0], y: p.rect.y + 1}
		defer func() { a.menuAnchor = nil }()
	}
	a.tabMenu(p, p.active)
}

// openReference is the terminal's reference pane: the note opens in a new
// split to the right, in preview, pinned so it stays put.
func (a *App) openReference(path string) {
	a.splitPane(true)
	p := a.activePane
	t := p.activeTab()
	if t == nil || t.path != path {
		a.openNote(path, true)
		t = p.activeTab()
	}
	if t == nil {
		return
	}
	t.mode = modePreview
	if !t.pinned {
		a.togglePin(p, p.active)
	}
	a.notify("Reference pinned; Ctrl+W h returns to the editor")
}

// --- files ---

// openAssetsPicker lists the vault's attachments; Enter opens one with the
// system handler on a local vault and copies its path on a server.
func (a *App) openAssetsPicker() {
	list, err := a.backend.ListAssets(a.ctx)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if len(list) == 0 {
		a.notify("No files in the attachment folders yet")
		return
	}
	items := make([]paletteItem, 0, len(list))
	for _, as := range list {
		items = append(items, paletteItem{label: as.Name, detail: as.Path, hint: humanSize(as.Size), id: as.Path})
	}
	p := &palette{title: "Files", placeholder: "Attachment", items: items, filtered: items}
	p.onSelect = func(a *App, it paletteItem) {
		if a.opts.Target.Kind == backend.KindLocal {
			a.openExternal(filepath.Join(a.backend.Root(), filepath.FromSlash(it.id)))
			return
		}
		a.copyText(it.id)
	}
	a.overlay = p
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// --- templates ---

// saveNoteAsTemplate stores the active note's body as a custom template
// in the vault's template folder, where the desktop app finds it too.
func (a *App) saveNoteAsTemplate(buf *noteBuffer, name string) {
	if buf == nil {
		a.notifyError("No note is active")
		return
	}
	meta, _ := a.noteMeta(buf.path)
	initial := meta.Title
	if initial == "" {
		initial = strings.TrimSuffix(filepath.Base(buf.path), ".md")
	}
	write := func(a *App, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		raw := templates.ComposeFile(templates.ComposeInput{Name: name, Category: "Custom", Body: buf.ed.Text()})
		if _, err := a.backend.WriteTemplate(context.Background(), vault.WriteTemplateInput{Slug: templates.SlugifyName(name), Raw: raw}); err != nil {
			a.notifyError(err.Error())
			return
		}
		a.refreshIndex()
		a.notify("Saved template " + name)
	}
	if name != "" {
		write(a, name)
		return
	}
	a.promptFor("Save note as template", initial, "Shown in :template and the desktop's picker", write)
}

// --- preferences ---

var noteSortOrders = []string{"name-asc", "name-desc", "updated-desc", "updated-asc", "created-desc", "created-asc", "none"}

func noteSortLabel(order string) string {
	switch order {
	case "name-asc":
		return "Name (A → Z)"
	case "name-desc":
		return "Name (Z → A)"
	case "updated-desc":
		return "Updated (newest first)"
	case "updated-asc":
		return "Updated (oldest first)"
	case "created-desc":
		return "Created (newest first)"
	case "created-asc":
		return "Created (oldest first)"
	}
	return "Manual (file order)"
}

func (a *App) setNoteSort(order string) error {
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "manual" {
		order = "none"
	}
	found := false
	for _, o := range noteSortOrders {
		if o == order {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("unknown order %q; one of %s", order, strings.Join(noteSortOrders, ", "))
	}
	a.prefs.NoteSortOrder = order
	a.sidebar.rebuild(a)
	a.notify("Notes sorted by " + noteSortLabel(order) + " (view.note_sort_order in config.toml keeps it)")
	return nil
}

func (a *App) openNoteSortMenu() {
	items := []menuItem{}
	for i, o := range noteSortOrders {
		order := o
		label := noteSortLabel(o)
		if o == a.prefs.NoteSortOrder {
			label += "  (current)"
		}
		items = append(items, menuItem{key: fmt.Sprint(i + 1), label: label, run: func(a *App) { _ = a.setNoteSort(order) }})
	}
	a.showMenu("Sort notes", items)
}

var doneStyles = []string{"none", "strikethrough", "gray", "gray-strikethrough"}

func (a *App) setDoneStyle(style string) error {
	style = strings.ToLower(strings.TrimSpace(style))
	switch style {
	case "strike":
		style = "strikethrough"
	case "dim", "muted":
		style = "gray"
	case "gray-strike", "graystrike":
		style = "gray-strikethrough"
	}
	found := false
	for _, s := range doneStyles {
		if s == style {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("unknown style %q; one of %s", style, strings.Join(doneStyles, ", "))
	}
	a.prefs.CompletedTaskStyle = style
	a.notify("Completed tasks: " + style + " (editor.completed_task_style in config.toml keeps it)")
	return nil
}

func (a *App) openDoneStyleMenu() {
	items := []menuItem{}
	for i, s := range doneStyles {
		style := s
		label := s
		if s == a.prefs.CompletedTaskStyle {
			label += "  (current)"
		}
		items = append(items, menuItem{key: fmt.Sprint(i + 1), label: label, run: func(a *App) { _ = a.setDoneStyle(style) }})
	}
	a.showMenu("Completed tasks", items)
}

// --- task notes ---

// newTaskFile creates a whole-note task (a note tagged `task`, the
// desktop's task file) in the given folder, or in the current one.
func (a *App) newTaskFile(target string) error {
	folder, sub := a.currentFolderContext()
	if target != "" {
		f, s, err := a.parseFolderTarget(target)
		if err != nil {
			return err
		}
		folder, sub = f, s
	}
	if folder == vault.FolderTrash {
		folder, sub = vault.FolderInbox, ""
	}
	where := a.vaultRelDir(folder, sub)
	if where == "" {
		where = "the vault root"
	}
	a.promptFor("New task in "+where, "", "Task title", func(a *App, text string) {
		title := strings.TrimSpace(text)
		if title == "" {
			return
		}
		body := "---\ntags: [" + vault.TaskFileTag + "]\nstatus: open\n---\n\n# " + title + "\n\n"
		meta, err := a.backend.CreateNote(a.ctx, folder, title, sub, &body)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		a.openNote(meta.Path, true)
		a.afterTaskChange()
	})
	return nil
}
