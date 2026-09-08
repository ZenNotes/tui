package tui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// --- note list views: Quick Notes, Archive, Trash ---

type noteListView struct {
	path   string
	notes  []vault.NoteMeta
	list   listCursor
	filter string
	rows   int
}

func newNoteListView(path string) *noteListView {
	return &noteListView{path: path}
}

func (v *noteListView) folder() vault.NoteFolder {
	switch v.path {
	case tabArchive:
		return vault.FolderArchive
	case tabTrash:
		return vault.FolderTrash
	}
	return vault.FolderQuick
}

func (v *noteListView) title() string {
	switch v.path {
	case tabArchive:
		return "Archive"
	case tabTrash:
		return "Trash"
	}
	return "Quick Notes"
}

func (v *noteListView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

func (v *noteListView) refresh(a *App) {
	all := a.notesIn(v.folder())
	if v.filter != "" {
		q := strings.ToLower(v.filter)
		out := []vault.NoteMeta{}
		for _, n := range all {
			if strings.Contains(strings.ToLower(n.Title), q) || strings.Contains(strings.ToLower(n.Excerpt), q) {
				out = append(out, n)
			}
		}
		all = out
	}
	v.notes = all
	v.list.clamp(len(v.notes))
}

func (v *noteListView) selected() *vault.NoteMeta {
	if v.list.cursor < len(v.notes) {
		return &v.notes[v.list.cursor]
	}
	return nil
}

func (v *noteListView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	header := " " + th.Title.Render(v.title()) + th.Muted.Render(fmt.Sprintf("  %d notes", len(v.notes)))
	if v.filter != "" {
		header += th.Muted.Render("  · filter: ") + th.Tag.Render(v.filter)
	}
	lines := []string{padRight(header, w)}
	rowsPer := 2
	v.rows = max(1, (h-1)/rowsPer)
	v.list.ensureVisible(v.rows)
	if len(v.notes) == 0 {
		msg := "Nothing here."
		switch v.path {
		case tabQuickNotes:
			msg = "No quick notes yet. Press n to write one."
		case tabTrash:
			msg = "The trash is empty."
		case tabArchive:
			msg = "Nothing archived. Archive a note with Space l a."
		}
		lines = append(lines, "", padRight(" "+th.Muted.Render(msg), w))
	}
	for i := v.list.scroll; i < len(v.notes) && len(lines)+rowsPer <= h+1; i++ {
		n := v.notes[i]
		age := formatAge(n.UpdatedAt)
		titleLine := " " + truncateCells(n.Title, w-4-len(age)) + strings.Repeat(" ", max(1, w-3-cellWidth(n.Title)-len(age))) + age
		excerpt := "   " + truncateCells(strings.ReplaceAll(n.Excerpt, "\n", " "), w-4)
		if focused && i == v.list.cursor {
			bar := th.BorderFocus.Render("▍")
			lines = append(lines, bar+th.SelectedFocus.Render(padRight(strings.TrimPrefix(titleLine, " "), w-1)), bar+th.Selected.Render(padRight(strings.TrimPrefix(excerpt, " "), w-1)))
		} else {
			lines = append(lines, padRight(th.Bold.Render(" "+truncateCells(n.Title, w-4-len(age)))+strings.Repeat(" ", max(1, w-3-cellWidth(n.Title)-len(age)))+th.Muted.Render(age), w), padRight(th.Dim.Render(excerpt), w))
		}
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func formatAge(unixMs int64) string {
	if unixMs == 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(unixMs))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return time.UnixMilli(unixMs).Format("2006-01-02")
}

func (v *noteListView) handleKey(a *App, k vim.Key) bool {
	n := len(v.notes)
	if a.listNav(&v.list, k, n, v.rows) {
		return true
	}
	sel := v.selected()
	if k.Is("enter") {
		if sel != nil {
			a.openNote(sel.Path, true)
		}
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.openResult", "nav.restore", "nav.delete", "nav.unarchive", "nav.newQuickNote", "nav.filter", "nav.contextMenu", "nav.localEx", "nav.peekPreview")
	if pending {
		return true
	}
	switch id {
	case "nav.openResult", "nav.peekPreview":
		if sel != nil {
			a.openNote(sel.Path, true)
			if id == "nav.peekPreview" {
				a.setPaneMode(modePreview)
			}
		}
	case "nav.restore":
		if sel != nil {
			if v.path == tabTrash {
				a.restoreNote(sel.Path)
			} else {
				a.renameNote(sel.Path)
			}
		}
	case "nav.delete":
		if sel != nil {
			if v.path == tabTrash {
				a.deleteNoteForever(sel.Path)
			} else {
				a.trashNote(sel.Path)
			}
		}
	case "nav.unarchive":
		if sel != nil && v.path == tabArchive {
			a.unarchiveNote(sel.Path)
		}
	case "nav.newQuickNote":
		if v.path == tabQuickNotes {
			a.newQuickNote()
		}
	case "nav.filter":
		a.promptFor("Filter "+v.title(), v.filter, "", func(a *App, text string) {
			v.filter = strings.TrimSpace(text)
			v.refresh(a)
		})
	case "nav.contextMenu":
		if sel != nil {
			a.noteContextMenu(sel.Path)
		}
	case "nav.localEx":
		a.openLocalEx()
	default:
		switch {
		case k.IsRune('E') && v.path == tabTrash:
			a.emptyTrash()
		case k.IsRune('?'):
			a.openKeyHelp()
		default:
			return false
		}
	}
	return true
}

func (v *noteListView) hintTargets(a *App, p *pane) []hintTarget {
	out := []hintTarget{}
	y0 := a.contentRect(p).y + 1
	for i := v.list.scroll; i < len(v.notes) && i-v.list.scroll < v.rows; i++ {
		path := v.notes[i].Path
		out = append(out, hintTarget{x: a.contentRect(p).x + 1, y: y0 + 2*(i-v.list.scroll), run: func(a *App) { a.openNote(path, true) }})
	}
	return out
}

// --- Tags view ---

type tagsView struct {
	tags     []tagCount
	selected map[string]bool
	matchAny bool
	notes    []vault.NoteMeta
	side     int // 0 tags, 1 notes
	tagList  listCursor
	noteList listCursor
	rows     int
}

func newTagsView() *tagsView {
	return &tagsView{selected: map[string]bool{}}
}

func (v *tagsView) title() string      { return "Tags" }
func (v *tagsView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

func (v *tagsView) refresh(a *App) {
	if a.idx == nil {
		return
	}
	v.tags = a.idx.tags
	v.tagList.clamp(len(v.tags))
	selected := []string{}
	for t, on := range v.selected {
		if on {
			selected = append(selected, t)
		}
	}
	sort.Strings(selected)
	v.notes = nil
	if len(selected) == 0 {
		if v.tagList.cursor < len(v.tags) {
			selected = []string{v.tags[v.tagList.cursor].tag}
		}
	}
	for _, n := range a.idx.notes {
		if n.Folder == vault.FolderTrash {
			continue
		}
		have := map[string]bool{}
		for _, t := range n.Tags {
			have[strings.ToLower(t)] = true
			// A nested tag also matches its parents.
			parts := strings.Split(strings.ToLower(t), "/")
			for i := 1; i < len(parts); i++ {
				have[strings.Join(parts[:i], "/")] = true
			}
		}
		matches := 0
		for _, s := range selected {
			if have[s] {
				matches++
			}
		}
		if (v.matchAny && matches > 0) || (!v.matchAny && matches == len(selected) && len(selected) > 0) {
			v.notes = append(v.notes, n)
		}
	}
	sortNotes(v.notes, a.prefs.NoteSortOrder)
	v.noteList.clamp(len(v.notes))
}

func (v *tagsView) selectTag(a *App, tag string) {
	tag = strings.ToLower(tag)
	v.selected = map[string]bool{tag: true}
	for i, t := range v.tags {
		if t.tag == tag {
			v.tagList.cursor = i
		}
	}
	v.side = 1
	v.refresh(a)
}

func (v *tagsView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	leftW := min(32, max(18, w/3))
	rightW := w - leftW - 1
	v.rows = h - 1
	v.tagList.ensureVisible(v.rows)
	v.noteList.ensureVisible(v.rows)
	mode := "all"
	if v.matchAny {
		mode = "any"
	}
	left := []string{padRight(" "+th.Muted.Render("match ")+th.Bold.Render(mode), leftW)}
	for i := v.tagList.scroll; i < len(v.tags) && len(left) < h; i++ {
		t := v.tags[i]
		mark := " "
		if v.selected[t.tag] {
			mark = "✓"
		}
		count := fmt.Sprint(t.count)
		label := fmt.Sprintf(" %s #%s", mark, truncateCells(t.tag, leftW-6-len(count)))
		row := padRight(th.Tag.Render(label)+strings.Repeat(" ", max(1, leftW-1-cellWidth(label)-len(count)))+th.Muted.Render(count), leftW)
		if focused && v.side == 0 && i == v.tagList.cursor {
			row = th.SelectedFocus.Render(padRight(label+strings.Repeat(" ", max(1, leftW-1-cellWidth(label)-len(count)))+count, leftW))
		} else if v.side == 1 && i == v.tagList.cursor && len(v.selected) == 0 {
			row = th.Selected.Render(padRight(label+strings.Repeat(" ", max(1, leftW-1-cellWidth(label)-len(count)))+count, leftW))
		}
		left = append(left, row)
	}
	right := []string{padRight(" "+th.Title.Render("Notes")+th.Muted.Render(fmt.Sprintf("  %d", len(v.notes))), rightW)}
	for i := v.noteList.scroll; i < len(v.notes) && len(right) < h; i++ {
		n := v.notes[i]
		tags := " " + th.Tag.Render(truncateCells("#"+strings.Join(n.Tags, " #"), max(0, rightW-4-cellWidth(n.Title))))
		row := padRight(" "+th.Base.Render(truncateCells(n.Title, rightW-2))+tags, rightW)
		if focused && v.side == 1 && i == v.noteList.cursor {
			row = th.SelectedFocus.Render(padRight(" "+truncateCells(n.Title, rightW-2)+" "+stripAnsi(tags), rightW))
		}
		right = append(right, row)
	}
	return joinColumns(a, []string{fitBlock(strings.Join(left, "\n"), leftW, h), fitBlock(strings.Join(right, "\n"), rightW, h)}, h)
}

// joinColumns puts blocks side by side with borders between them.
func joinColumns(a *App, blocks []string, h int) string {
	parts := []string{}
	for i, b := range blocks {
		if i > 0 {
			parts = append(parts, a.renderVerticalBorder(h, false))
		}
		parts = append(parts, b)
	}
	return joinHorizontal(parts...)
}

func (v *tagsView) handleKey(a *App, k vim.Key) bool {
	if k.Is("tab") && !k.Shift {
		if v.tagList.cursor < len(v.tags) {
			t := v.tags[v.tagList.cursor].tag
			v.selected[t] = !v.selected[t]
			if !v.selected[t] {
				delete(v.selected, t)
			}
			v.refresh(a)
		}
		return true
	}
	if k.Is("enter") {
		if v.side == 0 {
			v.side = 1
			return true
		}
		if v.noteList.cursor < len(v.notes) {
			a.openNote(v.notes[v.noteList.cursor].Path, true)
		}
		return true
	}
	if k.Is("left") {
		v.side = 0
		return true
	}
	if k.Is("right") {
		v.side = 1
		return true
	}
	cur := &v.tagList
	n := len(v.tags)
	if v.side == 1 {
		cur = &v.noteList
		n = len(v.notes)
	}
	if a.listNav(cur, k, n, v.rows) {
		if v.side == 0 && len(v.selected) == 0 {
			v.refresh(a)
		}
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.openSideItem", "nav.back", "nav.openResult", "nav.delete", "nav.localEx", "nav.filter", "nav.contextMenu")
	if pending {
		return true
	}
	switch id {
	case "nav.openSideItem":
		if v.side == 0 {
			v.side = 1
		} else if v.noteList.cursor < len(v.notes) {
			a.openNote(v.notes[v.noteList.cursor].Path, true)
		}
	case "nav.back":
		v.side = 0
	case "nav.openResult":
		if v.side == 1 && v.noteList.cursor < len(v.notes) {
			a.openNote(v.notes[v.noteList.cursor].Path, true)
		}
	case "nav.delete":
		if v.side == 0 && v.tagList.cursor < len(v.tags) {
			a.deleteTagConfirm(v.tags[v.tagList.cursor].tag)
		} else if v.side == 1 && v.noteList.cursor < len(v.notes) {
			a.trashNote(v.notes[v.noteList.cursor].Path)
		}
	case "nav.localEx":
		a.openLocalEx()
	case "nav.filter":
		a.openNoteSearch("#")
	case "nav.contextMenu":
		if v.side == 0 && v.tagList.cursor < len(v.tags) {
			tag := v.tags[v.tagList.cursor].tag
			a.showMenu("#"+tag, []menuItem{
				{key: "r", label: "Rename tag everywhere", run: func(a *App) { a.renameTagPrompt(tag) }},
				{key: "x", label: "Remove tag everywhere", run: func(a *App) { a.deleteTagConfirm(tag) }},
			})
		}
	default:
		switch {
		case k.IsRune('a'):
			v.matchAny = !v.matchAny
			v.refresh(a)
		case k.IsRune('c'):
			v.selected = map[string]bool{}
			v.refresh(a)
		case k.IsRune('r') && v.side == 0 && v.tagList.cursor < len(v.tags):
			a.renameTagPrompt(v.tags[v.tagList.cursor].tag)
		case k.IsRune('?'):
			a.openKeyHelp()
		default:
			return false
		}
	}
	return true
}

func (v *tagsView) hintTargets(a *App, p *pane) []hintTarget {
	out := []hintTarget{}
	y0 := a.contentRect(p).y + 1
	leftW := min(32, max(18, a.contentRect(p).w/3))
	for i := v.noteList.scroll; i < len(v.notes) && i-v.noteList.scroll < v.rows; i++ {
		path := v.notes[i].Path
		out = append(out, hintTarget{x: a.contentRect(p).x + leftW + 2, y: y0 + (i - v.noteList.scroll), run: func(a *App) { a.openNote(path, true) }})
	}
	return out
}

// --- tag rewrites ---

func (a *App) renameTagPrompt(tag string) {
	a.promptFor("Rename #"+tag+" everywhere", tag, "New tag name", func(a *App, next string) {
		next = strings.TrimPrefix(strings.TrimSpace(next), "#")
		if next == "" || strings.EqualFold(next, tag) {
			return
		}
		n := a.rewriteTag(tag, next)
		a.notify(fmt.Sprintf("Renamed #%s in %d notes", tag, n))
	})
}

func (a *App) deleteTagConfirm(tag string) {
	a.confirm("Remove #"+tag+" from every note?", func() {
		n := a.rewriteTag(tag, "")
		a.notify(fmt.Sprintf("Removed #%s from %d notes", tag, n))
	})
}

// rewriteTag replaces a tag token outside code in every note carrying it.
func (a *App) rewriteTag(tag, next string) int {
	if a.idx == nil {
		return 0
	}
	re := regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_/])#` + regexp.QuoteMeta(tag) + `\b`)
	count := 0
	for _, n := range a.idx.notes {
		has := false
		for _, t := range n.Tags {
			if strings.EqualFold(t, tag) || strings.HasPrefix(strings.ToLower(t), strings.ToLower(tag)+"/") {
				has = true
			}
		}
		if !has {
			continue
		}
		err := a.mutateNote(n.Path, func(body string) (string, bool) {
			out := rewriteOutsideCode(body, func(text string) string {
				return re.ReplaceAllStringFunc(text, func(m string) string {
					loc := re.FindStringSubmatchIndex(m)
					prefix := m[loc[2]:loc[3]]
					if next == "" {
						return strings.TrimRight(prefix, " ")
					}
					return prefix + "#" + next
				})
			})
			return out, out != body
		})
		if err == nil {
			count++
		}
	}
	a.refreshIndex()
	return count
}

// rewriteOutsideCode applies fn to the non-code portions of a note.
func rewriteOutsideCode(body string, fn func(string) string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		parts := strings.Split(line, "`")
		for j := range parts {
			if j%2 == 0 {
				parts[j] = fn(parts[j])
			}
		}
		lines[i] = strings.Join(parts, "`")
	}
	return strings.Join(lines, "\n")
}

func (a *App) copyText(text string) {
	if err := clipboard.WriteAll(text); err != nil {
		a.notifyError("Clipboard unavailable: " + err.Error())
		return
	}
	a.notify("Copied")
}

var _ = context.Background
