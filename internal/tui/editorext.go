package tui

import (
	"context"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/editor"

	"github.com/ZenNotes/tui/internal/backend"
)

// editorDoneMsg arrives when the external editor exits.
type editorDoneMsg struct {
	path string
	tmp  string
	err  error
}

// editActiveInEditor hands the active note to $VISUAL or $EDITOR at the
// cursor line, the way Glow's `e` does, and reloads it afterwards. Remote
// notes go through a temporary file that is written back on return.
func (a *App) editActiveInEditor() {
	buf := a.activeBuffer()
	if buf == nil {
		a.notify("No note is active")
		return
	}
	if err := a.saveBuffer(buf); err != nil {
		a.notifyError(err.Error())
		return
	}
	line := buf.ed.Cursor().Line + 1
	file := ""
	tmp := ""
	if a.opts.Target.Kind == backend.KindLocal && a.opts.Target.Root != "" {
		file = filepath.Join(a.opts.Target.Root, filepath.FromSlash(buf.path))
	} else {
		f, err := os.CreateTemp("", "zn-*.md")
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		if _, err := f.WriteString(buf.ed.Text()); err != nil {
			a.notifyError(err.Error())
			return
		}
		_ = f.Close()
		tmp = f.Name()
		file = tmp
	}
	cmd, err := editor.Cmd("ZenNotes", file, editor.LineNumber(line))
	if err != nil {
		a.notifyError("No editor: set $EDITOR or $VISUAL")
		return
	}
	path := buf.path
	a.queue(tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{path: path, tmp: tmp, err: err}
	}))
}

// finishExternalEdit reloads the note after the editor closed.
func (a *App) finishExternalEdit(m editorDoneMsg) {
	buf := a.buffers[m.path]
	if m.tmp != "" {
		defer os.Remove(m.tmp)
	}
	if m.err != nil {
		a.notifyError("Editor exited with an error: " + m.err.Error())
		return
	}
	if buf == nil {
		return
	}
	if m.tmp != "" {
		data, err := os.ReadFile(m.tmp)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		if string(data) != buf.ed.Text() {
			cur := buf.ed.Cursor()
			buf.ed.ReplaceText(string(data))
			buf.ed.SetCursor(cur)
			if err := a.saveBuffer(buf); err != nil {
				a.notifyError(err.Error())
				return
			}
		}
	} else {
		a.reloadBuffer(buf)
	}
	a.refreshIndex()
	a.notify("Reloaded " + a.tabTitle(a.activeTab()))
}

var _ = context.Background
