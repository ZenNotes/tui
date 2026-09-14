package tui

import (
	"context"
	"errors"
	"fmt"
	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"path"
	"strings"
	"time"
)

var errNoteConflict = errors.New("external edits detected; use :conflict to reload, keep local, or save a copy")

// A preflight check prevents overwriting known external edits. Servers without
// conditional writes still have a race between this read and the write.
func writeSnapshot(ctx context.Context, b backend.Backend, path, baseline, text string) (vault.NoteMeta, error) {
	current, err := readSnapshot(ctx, b, path)
	if err != nil {
		return vault.NoteMeta{}, fmt.Errorf("read before save: %w", err)
	}
	if current.Body != baseline && current.Body != text {
		return vault.NoteMeta{}, errNoteConflict
	}
	if current.Body == text {
		return current.NoteMeta, nil
	}
	if strings.HasPrefix(path, ".zennotes/templates/") {
		file, err := b.WriteTemplate(ctx, vault.WriteTemplateInput{Slug: strings.TrimSuffix(current.Title, ".md"), Raw: text, PreviousSourcePath: path})
		return vault.NoteMeta{Path: file.SourcePath, Title: current.Title}, err
	}
	return b.WriteNote(ctx, path, text)
}
func (a *App) queueSave(buf *noteBuffer) {
	if buf == nil || buf.saving || !buf.dirty() || buf.diskChanged {
		return
	}
	b, path, text, baseline := a.backend, buf.path, buf.ed.Text(), buf.savedText
	buf.saving = true
	buf.saveAt = time.Time{}
	a.queue(func() tea.Msg {
		meta, err := writeSnapshot(context.Background(), b, path, baseline, text)
		return saveResultMsg{path: path, buf: buf, meta: meta, text: text, err: err}
	})
}
func (a *App) finishSave(m saveResultMsg) {
	if a.buffers[m.path] != m.buf {
		return
	}
	buf := m.buf
	buf.saving = false
	if m.err != nil {
		buf.loadErr = m.err
		buf.saveAt = time.Time{}
		if errors.Is(m.err, errNoteConflict) {
			buf.diskChanged = true
		}
		a.notifyError("Save failed: " + m.err.Error())
		return
	}
	buf.savedText = m.text
	buf.meta = m.meta
	buf.loadErr = nil
	a.ignoreChange(m.path)
	if !buf.dirty() {
		buf.ed.MarkSaved()
	} else {
		buf.saveAt = time.Now().Add(autosaveDelay)
		a.armAutosave()
	}
}

type bufferReadMsg struct {
	buf      *noteBuffer
	path     string
	content  vault.NoteContent
	err      error
	baseline string
}

func (a *App) readBufferCmd(buf *noteBuffer) tea.Cmd {
	b, path, baseline := a.backend, buf.path, buf.savedText
	return func() tea.Msg {
		content, err := readSnapshot(context.Background(), b, path)
		return bufferReadMsg{buf, path, content, err, baseline}
	}
}
func (a *App) applyBufferRead(m bufferReadMsg) {
	if a.buffers[m.path] != m.buf || m.buf.saving || m.buf.savedText != m.baseline {
		return
	}
	m.buf.loading = false
	a.reconcileBuffer(m.buf, m.content, m.err)
	callbacks := m.buf.afterLoad
	m.buf.afterLoad = nil
	if m.err == nil {
		for _, f := range callbacks {
			f(m.buf)
		}
	}
}
func (a *App) reconcileBuffer(buf *noteBuffer, content vault.NoteContent, err error) {
	if err != nil {
		buf.loadErr = err
		buf.diskChanged = true
		return
	}
	buf.loadErr = nil
	if content.Body == buf.savedText {
		return
	}
	if content.Body == buf.ed.Text() {
		buf.savedText = content.Body
		buf.meta = content.NoteMeta
		buf.diskChanged = false
		buf.ed.MarkSaved()
		return
	}
	if buf.dirty() {
		buf.diskChanged = true
		return
	}
	cur, scroll := buf.ed.Cursor(), buf.ed.ScrollTop()
	buf.ed.ReplaceText(content.Body)
	buf.ed.SetCursor(cur)
	buf.ed.SetScrollTop(scroll)
	buf.savedText = content.Body
	buf.meta = content.NoteMeta
	buf.diskChanged = false
	buf.ed.MarkSaved()
	a.invalidatePreviews()
}
func (a *App) conflictMenu() {
	buf := a.activeBuffer()
	if buf == nil {
		return
	}
	if buf.saving {
		a.notify("Wait for the current save to finish")
		return
	}
	a.showMenu("Resolve external changes · "+buf.path, []menuItem{
		{key: "r", label: "Reload external version (discard local edits)", run: func(a *App) {
			a.confirm("Discard local edits?", func() {
				content, err := readSnapshot(a.ctx, a.backend, buf.path)
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				buf.ed.ReplaceText(content.Body)
				buf.savedText = content.Body
				buf.meta = content.NoteMeta
				buf.diskChanged = false
				buf.loadErr = nil
				buf.ed.MarkSaved()
			})
		}},
		{key: "l", label: "Keep local version (overwrite external edits)", run: func(a *App) {
			a.confirm("Overwrite external version?", func() {
				content, err := readSnapshot(a.ctx, a.backend, buf.path)
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				buf.savedText = content.Body
				buf.diskChanged = false
				a.queueSave(buf)
			})
		}},
		{key: "c", label: "Save local edits as a new note", run: func(a *App) {
			a.promptFor("Save copy", buf.meta.Title+" (local copy)", "Original remains unchanged", func(a *App, title string) {
				if title == "" {
					return
				}
				body := buf.ed.Text()
				folder, sub := a.folderOfPath(buf.path)
				a.createNote(folder, title, sub, &body, true)
			})
		}},
	})
}

type searchResultMsg struct {
	palette    *palette
	generation int
	items      []paletteItem
	err        error
}

func (p *palette) searchAsync(a *App, query string) {
	if p.cancel != nil {
		p.cancel()
	}
	p.generation++
	p.filtered = nil
	p.cursor = 0
	if len([]rune(query)) < p.minQuery {
		p.emptyHint = "Type at least 2 characters"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	generation, b := p.generation, a.backend
	p.emptyHint = "Searching…"
	a.queue(func() tea.Msg {
		timer := time.NewTimer(120 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		matches, err := b.SearchText(ctx, query, 200)
		items := []paletteItem{}
		for _, m := range matches {
			items = append(items, paletteItem{label: m.LineText, detail: fmt.Sprintf("%s:%d", m.Title, m.LineNumber), id: m.Path, data: m})
		}
		return searchResultMsg{p, generation, items, err}
	})
}

func readSnapshot(ctx context.Context, b backend.Backend, rel string) (vault.NoteContent, error) {
	if !strings.HasPrefix(rel, ".zennotes/templates/") {
		return b.ReadNote(ctx, rel)
	}
	files, err := b.ListTemplates(ctx)
	if err != nil {
		return vault.NoteContent{}, err
	}
	for _, f := range files {
		if f.SourcePath == rel {
			return vault.NoteContent{NoteMeta: vault.NoteMeta{Path: rel, Title: path.Base(rel)}, Body: f.Raw}, nil
		}
	}
	return vault.NoteContent{}, fmt.Errorf("template no longer exists: %s", rel)
}

func (a *App) withLoadedBuffer(path string, run func(*noteBuffer)) {
	if buf := a.buffers[path]; buf != nil {
		if buf.loading {
			buf.afterLoad = append(buf.afterLoad, run)
		} else if buf.loadErr == nil {
			run(buf)
		}
	}
}
