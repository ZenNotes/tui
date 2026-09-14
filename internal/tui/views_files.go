package tui

import (
	"context"
	"fmt"
	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type filesView struct {
	assets                 []vault.AssetMeta
	uses                   map[string][]vault.NoteMeta
	list                   listCursor
	rows                   int
	filter, sort, notePath string
	loading                bool
	err                    error
	generation             int
}
type filesLoadedMsg struct {
	view       *filesView
	generation int
	assets     []vault.AssetMeta
	uses       map[string][]vault.NoteMeta
	err        error
	epoch      *int
}

func (a *App) openFiles() {
	note := a.activePath()
	a.openVirtual(tabFiles, func() view { return &filesView{notePath: note} })
	if v, ok := a.activeTab().view.(*filesView); ok && !isVirtualPath(note) {
		v.notePath = note
	}
}
func (v *filesView) title() string { return "Files" }
func (v *filesView) hint(a *App) string {
	return "↑/↓ select · Enter actions · / filter · s sort · n attach · d deleted files"
}
func (v *filesView) refresh(a *App) {
	if v.sort == "" {
		v.sort = "name"
	}
	if v.loading {
		return
	}
	v.loading = true
	v.generation++
	gen, b, epoch := v.generation, a.backend, a.epoch
	a.queue(func() tea.Msg {
		assets, err := b.ListAssets(context.Background())
		uses := map[string][]vault.NoteMeta{}
		if err == nil {
			notes, e := b.ListNotes(context.Background())
			if e != nil {
				err = e
			} else {
				for _, n := range notes {
					body, e := b.ReadNote(context.Background(), n.Path)
					if e != nil {
						err = e
						continue
					}
					for _, as := range assets {
						if _, count := vault.RewriteAssetLinks(body.Body, n.Path, as.Path, as.Path); count > 0 {
							uses[as.Path] = append(uses[as.Path], n)
						}
					}
				}
			}
		}
		return filesLoadedMsg{v, gen, assets, uses, err, epoch}
	})
}
func (v *filesView) filtered() []vault.AssetMeta {
	out := []vault.AssetMeta{}
	q := strings.ToLower(v.filter)
	for _, as := range v.assets {
		if strings.Contains(strings.ToLower(as.Path), q) {
			out = append(out, as)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch v.sort {
		case "size":
			return out[i].Size > out[j].Size
		case "used":
			return len(v.uses[out[i].Path]) > len(v.uses[out[j].Path])
		case "modified":
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
func (v *filesView) render(a *App, w, h int, focused bool) string {
	list := v.filtered()
	v.list.clamp(len(list))
	v.rows = max(1, h-2)
	v.list.ensureVisible(v.rows)
	status := fmt.Sprintf("%d files · sort: %s · %s", len(list), v.sort, v.filter)
	if v.loading {
		status += " · loading…"
	}
	if v.err != nil {
		status += " · " + v.err.Error()
	}
	lines := []string{truncateCells(status, w), truncateCells(v.hint(a), w)}
	for i := v.list.scroll; i < len(list) && len(lines) < h; i++ {
		as := list[i]
		line := padRight(truncateCells(fmt.Sprintf(" %-*s %8s %3d uses · %s · %s", max(8, w/3), as.Name, humanSize(as.Size), len(v.uses[as.Path]), strings.TrimPrefix(filepath.Ext(as.Name), "."), as.Path), w), w)
		if focused && i == v.list.cursor {
			line = a.theme.SelectedFocus.Render(line)
		}
		lines = append(lines, line)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}
func (v *filesView) handleKey(a *App, k vim.Key) bool {
	list := v.filtered()
	if a.listNav(&v.list, k, len(list), v.rows) {
		return true
	}
	switch {
	case k.Is("enter"):
		if v.list.cursor < len(list) {
			v.menu(a, list[v.list.cursor])
		}
	case k.IsRune('/'):
		a.promptFor("Filter files", v.filter, "", func(a *App, s string) { v.filter = s; v.list = listCursor{} })
	case k.IsRune('s'):
		opts := []string{"name", "size", "used", "modified"}
		for i, s := range opts {
			if v.sort == s {
				v.sort = opts[(i+1)%len(opts)]
				return true
			}
		}
		v.sort = "name"
	case k.IsRune('n'):
		v.attach(a)
	case k.IsRune('d'):
		v.deleted(a)
	case k.IsRune(':'):
		a.openLocalEx()
	default:
		return false
	}
	return true
}
func assetMarkdown(as vault.AssetMeta) string {
	switch strings.ToLower(filepath.Ext(as.Name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif":
		return "![[" + as.Path + "]]"
	}
	return "[" + strings.ReplaceAll(as.Name, "]", "\\]") + "](<" + as.Path + ">)"
}
func (v *filesView) insert(a *App, text string) {
	buf := a.buffers[v.notePath]
	if buf == nil {
		a.copyText(text)
		a.notify("No source note is open; link copied")
		return
	}
	a.openNote(v.notePath, true)
	a.withLoadedBuffer(v.notePath, func(buf *noteBuffer) { buf.ed.InsertAtCursor(text) })
}
func (v *filesView) menu(a *App, as vault.AssetMeta) {
	a.showMenu(as.Name, []menuItem{
		{key: "i", label: "Insert link/embed in source note", run: func(a *App) { v.insert(a, assetMarkdown(as)) }},
		{key: "c", label: "Copy link/embed", run: func(a *App) { a.copyText(assetMarkdown(as)) }},
		{key: "o", label: "Open in external app", run: func(a *App) {
			if a.backend.Kind() == backend.KindLocal {
				abs, err := vault.SafeJoin(a.backend.Root(), as.Path)
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				a.openExternal(abs)
			} else {
				data, err := a.backend.ReadAsset(a.ctx, as.Path)
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				f, err := os.CreateTemp("", "zn-asset-*"+filepath.Ext(as.Name))
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				name := f.Name()
				_, err = f.Write(data)
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					os.Remove(name)
					a.notifyError("Could not download file")
					return
				}
				a.tempFiles = append(a.tempFiles, name)
				a.openExternal(name)
			}
		}},
		{key: "u", label: fmt.Sprintf("Show %d referencing notes", len(v.uses[as.Path])), run: func(a *App) {
			items := []paletteItem{}
			for _, n := range v.uses[as.Path] {
				items = append(items, paletteItem{label: n.Title, detail: n.Path, id: n.Path})
			}
			a.overlay = &palette{title: "Notes using " + as.Name, items: items, filtered: items, onSelect: func(a *App, it paletteItem) { a.openNote(it.id, true) }}
		}},
		{key: "r", label: "Rename and update note references", run: func(a *App) { v.rename(a, as) }},
		{key: "f", label: "Open containing folder (local vault)", run: func(a *App) {
			if a.backend.Kind() != backend.KindLocal {
				a.notify("Use the server's file manager to reveal a remote file")
				return
			}
			abs, err := vault.SafeJoin(a.backend.Root(), as.Path)
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			a.openExternal(filepath.Dir(abs))
		}},
		{key: "t", label: "Move to file trash", run: func(a *App) {
			m, ok := a.backend.(backend.AssetManager)
			if !ok {
				return
			}
			a.confirm(fmt.Sprintf("Trash %s? %d notes reference it; their links remain for restoration.", as.Name, len(v.uses[as.Path])), func() {
				if _, err := m.DeleteAsset(a.ctx, as.Path); err != nil {
					a.notifyError(err.Error())
					return
				}
				v.refresh(a)
			})
		}},
	})
}
func (v *filesView) attach(a *App) {
	m, ok := a.backend.(backend.AssetManager)
	if !ok {
		a.notifyError("This backend does not support file imports")
		return
	}
	a.promptFor("Attach a local file", "", "Absolute file path (up to 64 MiB)", func(a *App, name string) {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, "~/") {
			home, _ := os.UserHomeDir()
			name = filepath.Join(home, name[2:])
		}
		f, err := os.Open(name)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		defer f.Close()
		as, err := m.ImportAsset(a.ctx, v.notePath, filepath.Base(name), f)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		v.insert(a, as.Markdown)
		v.refresh(a)
	})
}
func (v *filesView) rename(a *App, as vault.AssetMeta) {
	m, ok := a.backend.(backend.AssetManager)
	if !ok {
		return
	}
	a.promptFor("Rename file", as.Name, "Updates references in notes; other files keep their names", func(a *App, name string) {
		if err := a.saveAllBuffers(); err != nil {
			a.notifyError(err.Error())
			return
		}
		notes, err := a.backend.ListNotes(a.ctx)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		next, err := m.RenameAsset(a.ctx, as.Path, name)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		failures := []string{}
		for _, n := range notes {
			err := a.mutateNote(n.Path, func(body string) (string, bool) {
				text, count := vault.RewriteAssetLinks(body, n.Path, as.Path, next.Path)
				return text, count > 0
			})
			if err != nil {
				failures = append(failures, n.Path+": "+err.Error())
			}
		}
		v.refresh(a)
		if len(failures) > 0 {
			a.notifyError("File renamed; references need repair in " + strings.Join(failures, "; "))
			a.overlay = &textReader{title: "File renamed · reference updates need attention", body: strings.Join(failures, "\n\n")}
		} else {
			a.notify("Renamed file and updated references")
		}
	})
}
func (v *filesView) deleted(a *App) {
	m, ok := a.backend.(backend.AssetManager)
	if !ok {
		return
	}
	list, err := m.ListDeletedAssets(a.ctx)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	items := []paletteItem{}
	for _, d := range list {
		items = append(items, paletteItem{label: d.Name, detail: d.Path, id: d.UndoToken, data: d})
	}
	a.overlay = &palette{title: "Deleted files · Enter restores", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		restored, err := m.RestoreDeletedAsset(a.ctx, it.data.(vault.DeletedAsset))
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.notify("Restored " + restored.Path)
		v.refresh(a)
	}}
}
