package tui

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// noteBuffer is an open note: its Vim editor plus save state. One buffer
// serves every tab and split showing the same note.
type noteBuffer struct {
	// codeHL caches the syntax highlighting of fenced code per version.
	codeHL      *codeHighlights
	path        string
	ed          *vim.Editor
	meta        vault.NoteMeta
	savedText   string
	diskChanged bool
	saveAt      time.Time
	saving      bool
	loadErr     error
	rows        []rowInfo

	words        int
	wordsVersion int
}

const autosaveDelay = 800 * time.Millisecond

type saveResultMsg struct {
	path string
	meta vault.NoteMeta
	text string
	err  error
}

// editorOptions derives the engine options from the preferences.
func (a *App) editorOptions() vim.Options {
	opts := vim.DefaultOptions()
	opts.TabSize = a.prefs.EditorTabSize
	opts.ScrollOff = a.prefs.EditorScrollOff
	opts.InsertEscape = a.prefs.VimInsertEscape
	opts.AutoPairs = a.prefs.AutoPairs
	opts.AutoPairQuotes = false
	opts.TextReplacements = a.prefs.TextReplacements
	opts.TextReplacementsEnabled = a.prefs.TextReplacementsEnabled
	opts.YankToClipboard = a.prefs.VimYankToClipboard
	opts.VimEnabled = a.prefs.VimMode
	opts.MarkdownLists = true
	return opts
}

// openBuffer loads a note into a buffer, reusing one already open.
func (a *App) openBuffer(path string) (*noteBuffer, error) {
	if buf, ok := a.buffers[path]; ok {
		return buf, nil
	}
	content, err := a.backend.ReadNote(context.Background(), path)
	if err != nil {
		return nil, err
	}
	buf := &noteBuffer{path: path, meta: content.NoteMeta, savedText: content.Body}
	buf.ed = vim.New(content.Body, a.editorOptions(), a.editorHooks(buf))
	buf.ed.MarkSaved()
	a.buffers[path] = buf
	return buf, nil
}

func (a *App) editorHooks(buf *noteBuffer) vim.Hooks {
	return vim.Hooks{
		ExCommand: func(cmd vim.ExCommand) (bool, error) {
			return a.runEx(buf, cmd)
		},
		ExComplete: func(text string) (string, []string) { return a.completeEx(text) },
		ReadClipboard: func() (string, bool) {
			text, err := clipboard.ReadAll()
			if err != nil {
				return "", false
			}
			return text, true
		},
		WriteClipboard: func(text string) bool {
			return clipboard.WriteAll(text) == nil
		},
		OnJump: func(from vim.Pos) {
			a.pushJump(buf.path, from)
		},
		FollowLink: func() { a.followLinkAtCursor(buf) },
		CopyLink:   func() { a.copyLinkAtCursor(buf) },
		OpenURL:    func() { a.openURLAtCursor(buf) },
		LineRows: func(line int) int {
			return a.editorLineRows(buf, line)
		},
		Changed: func() {
			buf.saveAt = time.Now().Add(autosaveDelay)
			a.armAutosave()
		},
	}
}

// editorLineRows is how many rows a line takes in the pane showing it.
func (a *App) editorLineRows(buf *noteBuffer, line int) int {
	width := a.editorTextWidth(buf)
	if width <= 0 || !a.prefs.WordWrap {
		return 1
	}
	return len(wrapLine(buf.ed.LineRunes(line), width))
}

// dirty reports unsaved edits.
func (b *noteBuffer) dirty() bool {
	return b.ed.Text() != b.savedText
}

// saveBuffer writes a dirty buffer through the backend.
func (a *App) saveBuffer(buf *noteBuffer) error {
	if buf == nil || !buf.dirty() {
		return nil
	}
	text := buf.ed.Text()
	meta, err := a.backend.WriteNote(context.Background(), buf.path, text)
	if err != nil {
		return err
	}
	buf.savedText = text
	buf.meta = meta
	buf.diskChanged = false
	buf.ed.MarkSaved()
	a.ignoreChange(buf.path)
	return nil
}

// saveAllBuffers flushes every dirty buffer.
func (a *App) saveAllBuffers() error {
	var first error
	for _, buf := range a.buffers {
		if err := a.saveBuffer(buf); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// autosaveDue lists buffers whose autosave timer expired.
func (a *App) autosaveDue(now time.Time) []*noteBuffer {
	out := []*noteBuffer{}
	for _, buf := range a.buffers {
		if buf.dirty() && !buf.saveAt.IsZero() && now.After(buf.saveAt) {
			out = append(out, buf)
		}
	}
	return out
}

// reloadBuffer re-reads a note the watcher saw change. A clean buffer takes
// the new text; a dirty one is flagged and keeps the edits.
func (a *App) reloadBuffer(buf *noteBuffer) {
	content, err := a.backend.ReadNote(context.Background(), buf.path)
	if err != nil {
		return
	}
	if content.Body == buf.ed.Text() {
		buf.savedText = content.Body
		buf.meta = content.NoteMeta
		buf.ed.MarkSaved()
		return
	}
	if buf.dirty() {
		buf.diskChanged = true
		return
	}
	buf.ed.ReplaceText(content.Body)
	buf.savedText = content.Body
	buf.meta = content.NoteMeta
	buf.ed.MarkSaved()
}

// closeBufferIfUnused drops a buffer no tab shows.
func (a *App) closeBufferIfUnused(path string) {
	for _, pane := range a.panes.leaves() {
		for _, tab := range pane.tabs {
			if tab.path == path {
				return
			}
		}
	}
	if buf, ok := a.buffers[path]; ok {
		_ = a.saveBuffer(buf)
		delete(a.buffers, path)
	}
}

// --- links under the cursor ---

var (
	wikilinkAtRe = regexp.MustCompile(`!?\[\[([^\]\n]+)\]\]`)
	mdLinkAtRe   = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	urlAtRe      = regexp.MustCompile(`(?:https?://|mailto:)[^\s<>()\[\]]+`)
)

type linkTarget struct {
	kind   string // "wikilink" | "url" | "path"
	target string
	alias  string
}

// linkAt finds the link covering a rune column on a line.
func linkAt(line string, col int) (linkTarget, bool) {
	runes := []rune(line)
	byteAt := func(rc int) int { return len(string(runes[:min(rc, len(runes))])) }
	byteCol := byteAt(col)
	for _, m := range wikilinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		if m[0] <= byteCol && byteCol < m[1] {
			content := line[m[2]:m[3]]
			target, _, alias := vault.SplitWikilinkContent(content)
			return linkTarget{kind: "wikilink", target: strings.TrimSpace(target), alias: strings.TrimPrefix(alias, "|")}, true
		}
	}
	for _, m := range mdLinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		if m[0] <= byteCol && byteCol < m[1] {
			target := line[m[4]:m[5]]
			kind := "path"
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				kind = "url"
			}
			return linkTarget{kind: kind, target: strings.Trim(target, "<>"), alias: line[m[2]:m[3]]}, true
		}
	}
	for _, m := range urlAtRe.FindAllStringIndex(line, -1) {
		if m[0] <= byteCol && byteCol < m[1] {
			return linkTarget{kind: "url", target: line[m[0]:m[1]]}, true
		}
	}
	return linkTarget{}, false
}

// linksOnLine lists every link on a line, in order.
func linksOnLine(line string) []linkTarget {
	out := []linkTarget{}
	seen := map[int]bool{}
	for _, m := range wikilinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		target, _, alias := vault.SplitWikilinkContent(line[m[2]:m[3]])
		out = append(out, linkTarget{kind: "wikilink", target: strings.TrimSpace(target), alias: strings.TrimPrefix(alias, "|")})
		seen[m[0]] = true
	}
	for _, m := range mdLinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		target := line[m[4]:m[5]]
		kind := "path"
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			kind = "url"
		}
		out = append(out, linkTarget{kind: kind, target: strings.Trim(target, "<>"), alias: line[m[2]:m[3]]})
	}
	for _, m := range urlAtRe.FindAllStringIndex(line, -1) {
		inside := false
		for _, l := range mdLinkAtRe.FindAllStringIndex(line, -1) {
			if l[0] <= m[0] && m[1] <= l[1] {
				inside = true
			}
		}
		if !inside {
			out = append(out, linkTarget{kind: "url", target: line[m[0]:m[1]]})
		}
	}
	return out
}

func (a *App) followLinkAtCursor(buf *noteBuffer) {
	cur := buf.ed.Cursor()
	link, ok := linkAt(buf.ed.Line(cur.Line), cur.Col)
	if !ok {
		links := linksOnLine(buf.ed.Line(cur.Line))
		if len(links) == 0 {
			a.notify("No link under the cursor")
			return
		}
		link = links[0]
	}
	a.followLink(link)
}

// followLink opens what a link points at: a note (created on confirm when
// missing), a URL in the browser, or a vault file path.
func (a *App) followLink(link linkTarget) {
	switch link.kind {
	case "url":
		a.openExternal(link.target)
	case "file":
		// An attachment: the system opener on a local vault, the path on
		// a server, where the file is not on this machine.
		if a.opts.Target.Kind == backend.KindLocal {
			if abs, err := vault.SafeJoin(a.backend.Root(), link.target); err == nil {
				a.openExternal(abs)
				return
			}
		}
		a.copyText(link.target)
	case "wikilink":
		if a.idx == nil {
			return
		}
		if meta, ok := vault.ResolveWikilink(a.idx.notes, link.target); ok {
			a.openNote(meta.Path, true)
			return
		}
		target := link.target
		a.confirm("Create note \""+target+"\"?", func() {
			folder, sub := a.currentFolderContext()
			a.createNote(folder, target, sub, nil, true)
		})
	case "path":
		target := strings.TrimPrefix(link.target, "./")
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target = target[:i]
		}
		if !strings.HasSuffix(strings.ToLower(target), ".md") {
			a.notify("Not a note: " + target)
			return
		}
		if _, ok := a.noteMeta(target); ok {
			a.openNote(target, true)
			return
		}
		// Relative to the current note's folder.
		if active := a.activeBuffer(); active != nil {
			dir := ""
			if i := strings.LastIndex(active.path, "/"); i >= 0 {
				dir = active.path[:i+1]
			}
			candidate := vault.NormalizeRelPath(dir + target)
			if _, ok := a.noteMeta(candidate); ok {
				a.openNote(candidate, true)
				return
			}
		}
		a.notify("Note not found: " + target)
	}
}

func (a *App) copyLinkAtCursor(buf *noteBuffer) {
	cur := buf.ed.Cursor()
	link, ok := linkAt(buf.ed.Line(cur.Line), cur.Col)
	if !ok {
		a.notify("No link under the cursor")
		return
	}
	text := strings.TrimPrefix(link.target, "mailto:")
	if link.kind == "wikilink" {
		text = "[[" + link.target + "]]"
	}
	if err := clipboard.WriteAll(text); err != nil {
		a.notifyError("Clipboard unavailable: " + err.Error())
		return
	}
	a.notify("Copied " + text)
}

func (a *App) openURLAtCursor(buf *noteBuffer) {
	cur := buf.ed.Cursor()
	link, ok := linkAt(buf.ed.Line(cur.Line), cur.Col)
	if !ok || link.kind != "url" {
		a.notify("No URL under the cursor")
		return
	}
	a.openExternal(link.target)
}
