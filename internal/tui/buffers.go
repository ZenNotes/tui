package tui

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// noteBuffer is an open note: its Vim editor plus save state. One buffer
// serves every tab and split showing the same note.
type noteBuffer struct {
	// codeHL caches the syntax highlighting of fenced code per version.
	codeHL      *codeHighlights
	path        string
	ed          modalEditor
	meta        vault.NoteMeta
	savedText   string
	diskChanged bool
	saveAt      time.Time
	saving      bool
	loading     bool
	// seeded marks an editor still holding the empty placeholder of a note no
	// read has returned yet. The first read replaces it outside the history.
	seeded    bool
	afterLoad []func(*noteBuffer)
	loadErr   error
	rows      []rowInfo
	// focusRows is the wrapping of the pane that last drew this buffer with
	// focus; rows belongs to whichever pane drew it last.
	focusRows []rowInfo
	// focusW is the drawn width of that editor, gutter included.
	focusW int
	// standalone marks an editor drawn outside the tabs, such as quick
	// capture's, whose cursor its host places.
	standalone bool

	words        int
	wordsVersion int
}

const autosaveDelay = 800 * time.Millisecond

type saveResultMsg struct {
	path string
	buf  *noteBuffer
	meta vault.NoteMeta
	text string
	err  error
}

// editorOptions derives the engine options from the preferences.
func (a *App) editorOptions() vim.Options {
	opts := vim.DefaultOptions()
	opts.WrappedLineMotions = a.prefs.VimWrappedLineMotions == "display"
	opts.TabSize = a.prefs.EditorTabSize
	opts.ScrollOff = a.prefs.EditorScrollOff
	opts.InsertEscape = a.prefs.VimInsertEscape
	opts.AutoPairs = a.prefs.AutoPairs
	opts.AutoPairQuotes = a.prefs.AutoPairQuotesInProse
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
	if a.deferReads {
		meta, _ := a.noteMeta(path)
		buf := &noteBuffer{path: path, meta: meta, loading: true, seeded: true}
		buf.ed = a.newModalEditor("", a.editorHooks(buf), &a.registers)
		buf.ed.MarkSaved()
		a.buffers[path] = buf
		a.queue(a.readBufferCmd(buf))
		return buf, nil
	}
	content, err := readSnapshot(context.Background(), a.backend, path)
	if err != nil {
		return nil, err
	}
	buf := &noteBuffer{path: path, meta: content.NoteMeta, savedText: content.Body}
	buf.ed = a.newModalEditor(content.Body, a.editorHooks(buf), &a.registers)
	buf.ed.MarkSaved()
	a.buffers[path] = buf
	return buf, nil
}

func (a *App) editorHooks(buf *noteBuffer) vim.Hooks {
	return vim.Hooks{
		DisplayRowBounds: func(p vim.Pos) (int, int) {
			line := buf.ed.LineRunes(p.Line)
			if !a.prefs.WordWrap {
				return 0, len(line)
			}
			for _, s := range wrapLine(line, a.editorTextWidth(buf)) {
				if p.Col < s.end || s.end == len(line) {
					return s.start, s.end
				}
			}
			return 0, len(line)
		},
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
			if a.prefs.Autosave && buf.undoEmptied() {
				a.notify("Undo emptied this note, so it was not saved. Redo brings the text back; :w saves it empty")
			}
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
	return !b.loading && b.ed.Text() != vim.NormalizedText(b.savedText)
}

// undoEmptied reports a buffer that undo or redo left blank while its file has
// text. Only a save the user names may write that: everything else leaves the
// file alone, so an undo can never wipe a note on its own.
func (b *noteBuffer) undoEmptied() bool {
	return b.ed.ChangedByUndo() && strings.TrimSpace(b.ed.Text()) == "" && strings.TrimSpace(b.savedText) != ""
}

// saveBuffer saves for the commands that flush first: closing, quitting and
// whatever acts on the file itself. An undo-emptied buffer is not written.
func (a *App) saveBuffer(buf *noteBuffer) error {
	if buf != nil && buf.undoEmptied() {
		return nil
	}
	return a.writeBuffer(buf)
}

// writeBuffer writes a dirty buffer through the backend.
func (a *App) writeBuffer(buf *noteBuffer) error {
	if buf != nil && buf.loading {
		return fmt.Errorf("note is still loading")
	}
	if buf == nil || !buf.dirty() {
		return nil
	}
	if buf.saving {
		return fmt.Errorf("save is still in progress; retry when it finishes")
	}
	if buf.diskChanged {
		return errNoteConflict
	}
	text := buf.ed.Text()
	meta, err := writeSnapshot(context.Background(), a.backend, buf.path, buf.savedText, text)
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

// hasUnsaved reports a buffer with edits that are not on disk.
func (a *App) hasUnsaved() bool {
	for _, buf := range a.buffers {
		if buf.dirty() {
			return true
		}
	}
	return false
}

// autosavePending reports a buffer autosave should write when its timer ends.
func (a *App) autosavePending(buf *noteBuffer) bool {
	return a.prefs.Autosave && !buf.saving && !buf.diskChanged && buf.dirty() && !buf.undoEmptied() && !buf.saveAt.IsZero()
}

// autosaveDue lists buffers whose autosave timer expired.
func (a *App) autosaveDue(now time.Time) []*noteBuffer {
	out := []*noteBuffer{}
	for _, buf := range a.buffers {
		if a.autosavePending(buf) && now.After(buf.saveAt) {
			out = append(out, buf)
		}
	}
	return out
}

// reloadBuffer re-reads a note the watcher saw change. A clean buffer takes
// the new text; a dirty one is flagged and keeps the edits.
func (a *App) reloadBuffer(buf *noteBuffer) {
	content, err := readSnapshot(context.Background(), a.backend, buf.path)
	a.reconcileBuffer(buf, content, err)
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
		// With autosave off, closeTab already asked before dropping edits.
		if a.prefs.Autosave {
			if err := a.saveBuffer(buf); err != nil {
				a.notifyError(err.Error())
				return
			}
		}
		delete(a.buffers, path)
	}
}

// --- links under the cursor ---

var (
	wikilinkAtRe = regexp.MustCompile(`!?\[\[([^\]\n]+)\]\]`)
	// mdLinkAtRe is the editor's reading of a Markdown link, as loose as the
	// desktop's LINK_SHAPES: the destination may hold spaces with or without
	// angle brackets, and angle brackets may hold a `)`.
	mdLinkAtRe = regexp.MustCompile(`!?\[([^\]]*)\]\((<[^>]+>|[^)]+)\)`)
	urlAtRe    = regexp.MustCompile(`(?:https?://|mailto:)[^\s<>()\[\]]+`)
)

type linkTarget struct {
	kind   string // "wikilink" | "url" | "path" | "file"
	target string // a wikilink's or path's keeps its #heading or ^block anchor
	alias  string
}

// wikilinkTargetOf builds the link for what sits inside `[[...]]`.
func wikilinkTargetOf(content string) linkTarget {
	target, anchor, alias := vault.SplitWikilinkContent(content)
	alias = strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(alias, `\`), "|"), `\|`, "|")
	return linkTarget{kind: "wikilink", target: strings.TrimSpace(target + anchor), alias: alias}
}

// mdLinkTargetOf builds the link for a Markdown link's raw destination.
func mdLinkTargetOf(label, dest string) linkTarget {
	target := vault.UnwrapMarkdownDest(dest)
	if url, ok := vault.ExternalLinkURL(target); ok {
		return linkTarget{kind: "url", target: url, alias: label}
	}
	if strings.Contains(target, "://") {
		return linkTarget{kind: "url", target: target, alias: label}
	}
	return linkTarget{kind: "path", target: target, alias: label}
}

// linkAt finds the link covering a rune column on a line.
func linkAt(line string, col int) (linkTarget, bool) {
	runes := []rune(line)
	byteAt := func(rc int) int { return len(string(runes[:min(rc, len(runes))])) }
	byteCol := byteAt(col)
	for _, m := range wikilinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		if m[0] <= byteCol && byteCol < m[1] {
			if link := wikilinkTargetOf(line[m[2]:m[3]]); link.target != "" {
				return link, true
			}
		}
	}
	for _, m := range mdLinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		if m[0] <= byteCol && byteCol < m[1] {
			return mdLinkTargetOf(line[m[2]:m[3]], line[m[4]:m[5]]), true
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
	return linksOnLineWhere(line, func(int, int) bool { return true })
}

// linksOnLineWhere lists the links on a line whose byte span keep accepts.
func linksOnLineWhere(line string, keep func(start, end int) bool) []linkTarget {
	out := []linkTarget{}
	seen := map[int]bool{}
	for _, m := range wikilinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		if !keep(m[0], m[1]) {
			continue
		}
		if link := wikilinkTargetOf(line[m[2]:m[3]]); link.target != "" {
			out = append(out, link)
		}
		seen[m[0]] = true
	}
	for _, m := range mdLinkAtRe.FindAllStringSubmatchIndex(line, -1) {
		if !keep(m[0], m[1]) {
			continue
		}
		out = append(out, mdLinkTargetOf(line[m[2]:m[3]], line[m[4]:m[5]]))
	}
	for _, m := range urlAtRe.FindAllStringIndex(line, -1) {
		if !keep(m[0], m[1]) {
			continue
		}
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
	case "wikilink", "path":
		if a.idx == nil {
			return
		}
		from := ""
		if active := a.activeBuffer(); active != nil {
			from = active.path
		}
		if notePath, anchor, ok := resolveNoteLink(a.idx.notes, from, link); ok {
			a.openNoteAtAnchor(notePath, anchor)
			return
		}
		a.offerNoteFromLink(from, link)
	}
}

// resolveNoteLink finds the note a link opens and the anchor to land on, in the
// desktop's order: a same-note anchor, then a wikilink by name and a Markdown
// link relative to this note, each falling back on the other's reading.
func resolveNoteLink(notes []vault.NoteMeta, from string, link linkTarget) (notePath, anchor string, ok bool) {
	if from != "" && vault.IsSameNoteAnchor(link.target) {
		return from, link.target, true
	}
	byHref := func() (string, string, bool) {
		meta, ok := vault.ResolveNoteHref(notes, from, link.target)
		return meta.Path, "#" + vault.HrefFragment(link.target), ok
	}
	byName := func() (string, string, bool) {
		meta, ok := vault.ResolveWikilink(notes, link.target)
		return meta.Path, link.target, ok
	}
	first, second := byHref, byName
	if link.kind == "wikilink" {
		first, second = byName, byHref
	}
	if notePath, anchor, ok = first(); ok {
		return notePath, anchor, true
	}
	return second()
}

// openNoteAtAnchor opens a note and puts the anchor at the top of the view. An
// anchor the note no longer carries opens it where it was, as on the desktop.
func (a *App) openNoteAtAnchor(notePath, anchor string) {
	a.openNote(notePath, true)
	t := a.activeTab()
	a.withLoadedBuffer(notePath, func(buf *noteBuffer) {
		line, ok := vault.AnchorLine(buf.ed.Text(), anchor)
		if !ok {
			return
		}
		buf.ed.GotoLine(line)
		buf.ed.SetScrollTop(line - 1)
		buf.ed.EnsureCursorVisible()
		if t != nil && t.path == notePath && t.mode == modePreview {
			if t.preview == nil {
				t.preview = &previewState{}
			}
			t.preview.jumpLine, t.preview.jumpPending = line-1, true
		}
	})
}

// offerNoteFromLink asks before creating the note a dead link names, so a link
// to a note not written yet is never a dead end.
func (a *App) offerNoteFromLink(from string, link linkTarget) {
	if link.kind == "path" {
		folder, sub, title, ok := a.hrefNoteLocation(from, link.target)
		if !ok {
			shown, _, _ := strings.Cut(strings.TrimPrefix(link.target, "./"), "#")
			a.notify("Not a note: " + shown)
			return
		}
		shown := title
		if sub != "" {
			shown = sub + "/" + title
		}
		a.confirm("Create note \""+shown+"\"?", func() {
			a.createNote(folder, title, sub, nil, true)
		})
		return
	}
	name, _, _ := vault.SplitWikilinkContent(link.target)
	name = strings.TrimSpace(name)
	if name == "" {
		a.notify("This link names no note")
		return
	}
	a.confirm("Create note \""+name+"\"?", func() {
		folder, sub := a.currentFolderContext()
		a.createNote(folder, name, sub, nil, true)
	})
}

// hrefNoteLocation is where the note a dead href names goes: the path the href
// points at, so the link resolves once the note exists. A path from the vault
// root starts at the Inbox, as the desktop reads it, and false means the href
// cannot name a note at all.
func (a *App) hrefNoteLocation(from, href string) (folder vault.NoteFolder, sub, title string, ok bool) {
	if vault.HrefNamesFile(href) {
		return "", "", "", false
	}
	rel, ok := vault.NoteHrefPath(from, href)
	if !ok || strings.ContainsAny(rel, `\:*?"<>|#^[]`) {
		return "", "", "", false
	}
	if strings.HasSuffix(strings.ToLower(rel), ".md") {
		rel = rel[:len(rel)-len(".md")]
	}
	folder, known := vault.FolderForRelativePath(rel, a.folderPaths())
	if !known {
		return "", "", "", false
	}
	dir, title := path.Split(rel)
	dir = strings.TrimSuffix(dir, "/")
	if folder == vault.FolderTrash {
		return vault.FolderInbox, "", title, true
	}
	sub = vault.FolderSubpathOf(rel, folder, a.primaryAtRoot(), a.folderPaths())
	if sub == "" && folder == vault.FolderInbox && dir != "" && !strings.EqualFold(dir, vault.ResolveFolderPath(folder, a.folderPaths())) {
		sub = dir
	}
	return folder, sub, title, true
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
