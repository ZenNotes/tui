package tui

import (
	"strings"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

// modalEditor is the editing engine behind a buffer. *vim.Editor implements
// it as it is; other engines adapt to its vim types (see engine_helix.go),
// so the rest of the app keeps driving buffers through the vim API.
type modalEditor interface {
	BlockColumns() (left, right int, toEnd bool)
	ClearMessage()
	Cmdline() (kind rune, text string, cursor int, active bool)
	Completions() ([]string, int)
	Cursor() vim.Pos
	EnsureCursorVisible()
	ExecuteEx(line string)
	Feed(notation string)
	Folds() map[int]int
	GotoLine(line int)
	HandleKey(k vim.Key) bool
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
	Mode() vim.Mode
	OpenCmdline(kind rune, initial string)
	Pending() string
	PendingCount() (int, bool)
	ReflowParagraph()
	ReplaceAtCursor(before, after int, text string)
	ReplaceLineText(line int, text string)
	ReplacePending() bool
	ReplaceText(text string)
	ResetPending()
	ScrollTop() int
	SearchHighlights(line int) [][2]int
	SetCursor(p vim.Pos)
	SetHooks(h vim.Hooks)
	SetOptions(opts vim.Options)
	SetScrollTop(line int)
	SetText(text string)
	SetViewport(rows int)
	StatusSuffix() string
	Text() string
	ToggleCheckboxAtCursor()
	Version() int
	VisualRange() (start, end vim.Pos, mode vim.Mode, ok bool)
}

var _ modalEditor = (*vim.Editor)(nil)

// engineProfile is what the app needs to know about an editing engine. The
// [vim] motions option picks one; engine-specific behaviour outside the
// engine itself reads it from here.
type engineProfile struct {
	new func(a *App, text string, opts vim.Options, hooks vim.Hooks, store *vim.RegisterStore) modalEditor
	// tabSequences are keymap ids for the engine's own tab keys, on top of
	// the shared buffer and tab sequences (]b [b gt gT).
	tabSequences []string
	// foldKeys is the key-help label for the fold keys.
	foldKeys string
	// selectWord selects the word under the cursor (mouse double-click).
	selectWord string
}

var engineProfiles = map[string]engineProfile{
	"vim": {
		new: func(_ *App, text string, opts vim.Options, hooks vim.Hooks, store *vim.RegisterStore) modalEditor {
			return vim.NewWithRegisters(text, opts, hooks, store)
		},
		foldKeys:   "key:zc/zo/zM/zR",
		selectWord: "viw",
	},
	"helix": {
		new:          newHelixEditor,
		tabSequences: []string{"helix.gotoTabPrevious", "helix.gotoTabNext"},
		foldKeys:     "key:zf/zo/za/zM/zR",
		selectWord:   "miw",
	},
}

// engine is the profile of the configured engine; vim when motions names
// none.
func (a *App) engine() engineProfile {
	if p, ok := engineProfiles[a.prefs.Motions]; ok {
		return p
	}
	return engineProfiles["vim"]
}

// newModalEditor builds the configured engine over text.
func (a *App) newModalEditor(text string, hooks vim.Hooks, store *vim.RegisterStore) modalEditor {
	return a.newModalEditorWithOpts(text, a.editorOptions(), hooks, store)
}

// newModalEditorWithOpts is newModalEditor with explicit options.
func (a *App) newModalEditorWithOpts(text string, opts vim.Options, hooks vim.Hooks, store *vim.RegisterStore) modalEditor {
	return a.engine().new(a, text, opts, hooks, store)
}

// withEngineSequences adds the engine's own tab sequences to ids.
func (a *App) withEngineSequences(ids []string) []string {
	return append(ids, a.engine().tabSequences...)
}

// engineTabHint is the hint id list for the engine's own tab keys, next
// before previous, "" when it has none.
func (a *App) engineTabHint() string {
	ids := a.engine().tabSequences
	out := make([]string, len(ids))
	for i := range ids {
		out[i] = ids[len(ids)-1-i]
	}
	return strings.Join(out, "|")
}

// runEngineSequence runs an engine's own tab sequence; false for any other
// id.
func (a *App) runEngineSequence(id string, count int) bool {
	dir := 0
	switch id {
	case "helix.gotoTabPrevious":
		dir = -1
	case "helix.gotoTabNext":
		dir = 1
	default:
		return false
	}
	for i := 0; i < max(count, 1); i++ {
		a.cycleTab(dir)
	}
	return true
}

// blockCursor is where the block cursor is drawn: the cursor, unless the
// engine draws it elsewhere (helix keeps it inside the selection).
func blockCursor(ed modalEditor) vim.Pos {
	if b, ok := ed.(interface{ BlockCursor() vim.Pos }); ok {
		return b.BlockCursor()
	}
	return ed.Cursor()
}

// modeLabel is the status-bar name of the editor's mode.
func modeLabel(ed modalEditor) string {
	if l, ok := ed.(interface{ ModeLabel() string }); ok {
		return l.ModeLabel()
	}
	return ed.Mode().Label()
}

// engineHints adapts the key help to the engine: its fold keys, and its own
// tab keys next to the shared ones (or as their own row where none is
// shown; tab sequences work from every surface).
func (a *App) engineHints(pairs []hintPair) []hintPair {
	p := a.engine()
	tabs := a.engineTabHint()
	found := false
	for i := range pairs {
		switch {
		case pairs[i].id == "key:zc/zo/zM/zR":
			pairs[i].id = p.foldKeys
		case strings.Contains(pairs[i].id, "vim.bufferNext") && tabs != "":
			pairs[i].id += "|" + tabs
			found = true
		}
	}
	if !found && tabs != "" {
		pairs = append(pairs, hintPair{tabs, "next/previous tab"})
	}
	return pairs
}

func init() {
	settingChoices["motions"] = config.MotionsChoices
}
