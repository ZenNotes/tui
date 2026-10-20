package tui

import (
	"github.com/ZenNotes/tui/internal/helix"
	"github.com/ZenNotes/tui/internal/vim"
)

// modalEditor is the editing engine behind buffers. The [vim] motions
// option picks the implementation: "vim" (default) or "helix". Both engines
// share the helix key, position, register and hook types, so the host drives
// either one the same way; the vim adapter converts its engine-local Mode.
type modalEditor interface {
	BlockColumns() (left, right int, toEnd bool)
	ClearMessage()
	Cmdline() (kind rune, text string, cursor int, active bool)
	Completions() ([]string, int)
	Cursor() helix.Pos
	EnsureCursorVisible()
	ExecuteEx(line string)
	Feed(notation string)
	GotoLine(line int)
	HandleKey(k helix.Key) bool
	Hidden(line int) bool
	IncrementalHighlights(line int) [][2]int
	InsertAtCursor(text string)
	IsFoldStart(line int) (int, bool)
	Line(i int) string
	LineCount() int
	LineRunes(i int) []rune
	Lines() []string
	MarkSaved()
	Message() (string, bool)
	Mode() helix.Mode
	OpenCmdline(kind rune, initial string)
	Pending() string
	PendingCount() (int, bool)
	ReflowParagraph()
	ReplaceAtCursor(before, after int, text string)
	ReplaceLineText(line int, text string)
	ReplaceText(text string)
	ResetPending()
	ScrollTop() int
	SearchHighlights(line int) [][2]int
	SelectionKind() string
	SetCursor(p helix.Pos)
	SetHooks(h helix.Hooks)
	SetOptions(opts helix.Options)
	SetScrollTop(line int)
	SetViewport(rows int)
	StatusSuffix() string
	Text() string
	ToggleCheckboxAtCursor()
	Version() int
	VisualRange() (start, end helix.Pos, mode helix.Mode, ok bool)
	ModeLabel() string
}

// vimEditor adapts the vim engine to modalEditor. The embedded editor
// provides every method; the adapter converts the engine-local Mode.
type vimEditor struct {
	*vim.Editor
}

func (w vimEditor) Mode() helix.Mode {
	switch w.Editor.Mode() {
	case vim.ModeInsert:
		return helix.ModeInsert
	case vim.ModeCmdline:
		return helix.ModeCmdline
	}
	return helix.ModeNormal
}

// ModeLabel is the status-bar name of the current mode, including vim's
// visual and replace modes.
func (w vimEditor) ModeLabel() string { return w.Editor.Mode().Label() }

// VisualRange reports the selection; the vim engine's mode maps to a plain
// helix selection, with the kind carried by SelectionKind.
func (w vimEditor) VisualRange() (helix.Pos, helix.Pos, helix.Mode, bool) {
	start, end, _, ok := w.Editor.VisualRange()
	return start, end, helix.ModeNormal, ok
}

// newModalEditor builds the configured engine over text.
func (a *App) newModalEditor(text string, hooks helix.Hooks, store *helix.RegisterStore) modalEditor {
	return a.newModalEditorWithOpts(text, a.editorOptions(), hooks, store)
}

// newModalEditorWithOpts is newModalEditor with explicit options.
func (a *App) newModalEditorWithOpts(text string, opts helix.Options, hooks helix.Hooks, store *helix.RegisterStore) modalEditor {
	if a.prefs.Motions == "helix" {
		return helix.NewWithRegisters(text, opts, hooks, store)
	}
	return vimEditor{vim.NewWithRegisters(text, opts, hooks, store)}
}
