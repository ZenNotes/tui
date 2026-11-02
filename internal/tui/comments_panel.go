package tui

import (
	"context"
	"fmt"
	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

type commentsPanel struct {
	path                     string
	threads                  []backend.CommentThreadView
	list                     listCursor
	rows                     int
	loading, includeResolved bool
	err                      error
	generation               int
}
type commentsLoadedMsg struct {
	panel      *commentsPanel
	generation int
	threads    []backend.CommentThreadView
	err        error
}

func (a *App) openComments() {
	if a.comments == nil {
		a.comments = &commentsPanel{}
	}
	a.toggleSidePanel(&a.commentsOpen, focusComments)
	a.comments.refresh(a)
}
func (p *commentsPanel) refresh(a *App) {
	buf := a.activeBuffer()
	if buf == nil {
		p.generation++
		p.path = ""
		p.threads = nil
		p.loading = false
		return
	}
	path := buf.path
	if p.path == path && p.loading {
		return
	}
	p.path = path
	p.generation++
	gen, b, resolved := p.generation, a.backend, p.includeResolved
	p.loading = true
	a.queue(func() tea.Msg {
		threads, err := backend.ListCommentThreads(context.Background(), b, path, resolved)
		return commentsLoadedMsg{p, gen, threads, err}
	})
}
func (p *commentsPanel) render(a *App, w, h int) string {
	lines := []string{" Comments", " Enter actions · n new · R resolved"}
	if p.path == "" {
		return fitBlock(" Comments\n Open a note to read its comments", w, h)
	}
	if p.err != nil {
		lines = append(lines, p.err.Error())
	}
	if p.loading {
		lines = append(lines, "Loading…")
	}
	p.rows = max(1, min(6, h/3))
	p.list.ensureVisible(p.rows)
	for i := p.list.scroll; i < len(p.threads) && i < p.list.scroll+p.rows; i++ {
		t := p.threads[i]
		mark := "○"
		if t.Resolved {
			mark = "✓"
		}
		line := padRight(truncateCells(fmt.Sprintf("%s L%d %s", mark, t.Line, t.Body), w), w)
		if i == p.list.cursor {
			line = a.theme.SelectedFocus.Render(line)
		}
		lines = append(lines, line)
	}
	if len(p.threads) == 0 && !p.loading {
		lines = append(lines, "No comments. Press n to start one.")
	}
	if p.list.cursor < len(p.threads) {
		t := p.threads[p.list.cursor]
		lines = append(lines, "", "Enter → Read full thread")
		lines = append(lines, wrapCommentText(commentThreadText(t), w)...)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}
func wrapCommentText(s string, w int) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		r := []rune(line)
		for _, seg := range wrapLine(r, max(1, w)) {
			out = append(out, string(r[seg.start:seg.end]))
		}
	}
	return out
}
func commentThreadText(t backend.CommentThreadView) string {
	lines := []string{"> " + t.AnchorText, "", t.Body}
	for _, r := range t.Replies {
		author := "Reply"
		if r.Author != nil {
			author = *r.Author
		}
		lines = append(lines, "", author+":", r.Body)
	}
	return strings.Join(lines, "\n")
}
func (p *commentsPanel) handleKey(a *App, k vim.Key) {
	if k.Is("esc") {
		a.focus = focusPane
		return
	}
	if a.listNav(&p.list, k, len(p.threads), p.rows) {
		return
	}
	switch {
	case k.IsRune('n'):
		p.add(a)
	case k.IsRune('R'):
		p.includeResolved = !p.includeResolved
		p.refresh(a)
	case k.IsRune('r'):
		p.refresh(a)
	case k.Is("enter") || k.IsRune('m'):
		if p.list.cursor < len(p.threads) {
			p.menu(a, p.threads[p.list.cursor])
		}
	case k.IsRune(':'):
		a.openLocalEx()
	}
}
func (p *commentsPanel) add(a *App) {
	buf := a.activeBuffer()
	if buf == nil {
		return
	}
	if err := a.saveBuffer(buf); err != nil {
		a.notifyError(err.Error())
		return
	}
	path := buf.path
	anchor := strings.TrimSpace(buf.ed.Line(buf.ed.Cursor().Line))
	a.promptFor("Comment anchor", anchor, "Text from this note; empty creates an unanchored comment", func(a *App, anchor string) {
		a.promptFor("New comment", "", "", func(a *App, body string) {
			_, err := backend.AddComment(a.ctx, a.backend, backend.AddCommentInput{Path: path, AnchorText: anchor, Body: body})
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			p.refresh(a)
		})
	})
}
func (p *commentsPanel) menu(a *App, t backend.CommentThreadView) {
	path := p.path
	a.showMenu("Comment thread", []menuItem{
		{key: "v", label: "Read full thread", run: func(a *App) { a.overlay = &textReader{title: "Comment thread", body: commentThreadText(t)} }},
		{key: "a", label: "Jump to anchor", run: func(a *App) {
			a.openNote(path, true)
			a.withLoadedBuffer(path, func(buf *noteBuffer) {
				if t.AnchorText != "" && !strings.Contains(buf.ed.Text(), t.AnchorText) {
					a.notify("Anchor text has moved or changed; showing its nearest stored position")
				}
				buf.ed.GotoLine(max(1, t.Line))
			})
		}},
		{key: "r", label: "Reply", run: func(a *App) {
			a.promptFor("Reply", "", "", func(a *App, s string) {
				_, err := backend.ReplyToComment(a.ctx, a.backend, backend.ReplyInput{Path: path, ID: t.ID, Body: s})
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				p.refresh(a)
			})
		}},
		{key: "x", label: "Resolve / reopen", run: func(a *App) {
			_, err := backend.ResolveComment(a.ctx, a.backend, path, t.ID, !t.Resolved)
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			p.refresh(a)
		}},
		{key: "e", label: "Edit comment or reply", run: func(a *App) { p.chooseComment(a, t, false) }},
		{key: "d", label: "Delete comment or reply", run: func(a *App) { p.chooseComment(a, t, true) }},
	})
}
func (p *commentsPanel) chooseComment(a *App, t backend.CommentThreadView, remove bool) {
	path := p.path
	items := []paletteItem{{id: t.ID, label: t.Body}}
	for _, r := range t.Replies {
		items = append(items, paletteItem{id: r.ID, label: r.Body})
	}
	a.overlay = &palette{title: "Select comment", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		edit := func(body string) {
			comments, err := a.backend.ListComments(a.ctx, path)
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			next := []vault.NoteComment{}
			for _, c := range comments {
				if remove && (c.ID == it.id || c.ParentID == it.id) {
					continue
				}
				if c.ID == it.id {
					c.Body = body
					c.UpdatedAt = time.Now().UnixMilli()
				}
				next = append(next, c)
			}
			if _, err = a.backend.WriteComments(a.ctx, path, next); err != nil {
				a.notifyError(err.Error())
				return
			}
			p.refresh(a)
		}
		if remove {
			a.confirm("Delete this comment and its replies?", func() { edit("") })
		} else {
			a.promptFor("Edit comment", it.label, "", func(a *App, s string) {
				if strings.TrimSpace(s) == "" {
					a.notifyError("Comment cannot be empty")
					return
				}
				edit(s)
			})
		}
	}}
}
