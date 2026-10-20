package helix

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Mode is the editor's state.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeSelect
	ModeCmdline
)

// Label is the mode name for a status bar.
func (m Mode) Label() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeSelect:
		return "SELECT"
	case ModeCmdline:
		return "COMMAND"
	}
	return "NORMAL"
}

// selKind is what a selection covers: single characters, whole lines, or a
// rectangular block (a ZenNotes extension; Helix has no block mode).
type selKind int

const (
	selChar selKind = iota
	selLine
	selBlock
)

// Options tune the engine. Zero values are filled by New.
type Options struct {
	WrappedLineMotions      bool
	TabSize                 int
	ScrollOff               int
	IgnoreCase              bool
	SmartCase               bool
	WrapScan                bool
	InsertEscape            string
	AutoPairs               bool
	AutoPairQuotes          bool
	TextReplacements        map[string]string
	TextReplacementsEnabled bool
	MarkdownLists           bool
	YankToClipboard         bool
	// HelixEnabled false runs the buffer as a plain editor: always inserting,
	// arrows and the usual editing keys only.
	HelixEnabled bool
}

// DefaultOptions are the shipped defaults.
func DefaultOptions() Options {
	return Options{
		TabSize:                 4,
		WrapScan:                true,
		AutoPairs:               true,
		TextReplacements:        map[string]string{"->": "→"},
		TextReplacementsEnabled: true,
		MarkdownLists:           true,
		HelixEnabled:            true,
	}
}

// Register is a yank or delete.
type Register struct {
	Text      string
	Linewise  bool
	Blockwise bool
}

// RegisterStore holds yank, delete, and macro registers shared by editors in
// one host session. Its zero value is ready to use. Like Editor, it is owned by
// the host's event loop rather than accessed concurrently.
type RegisterStore struct {
	values map[rune]Register
}

// Range is a line range for an ex command, zero-based and inclusive.
type Range struct {
	Start, End int
	Given      bool
}

// ExCommand is a command the editor hands to its host.
type ExCommand struct {
	Name  string
	Args  string
	Bang  bool
	Range Range
	Raw   string
}

// Hooks are the host's side of the engine. Every field is optional.
type Hooks struct {
	DisplayRowBounds func(Pos) (int, int)
	// ExCommand runs a command the editor does not own; handled false means
	// unknown command.
	ExCommand func(cmd ExCommand) (handled bool, err error)
	// ExComplete proposes completions for the ex line. It receives the text
	// before the cursor and returns the part to keep plus the candidates
	// that replace the rest, so a host can complete note titles with spaces
	// as one argument. Nil candidates fall back to the built-in command
	// names.
	ExComplete     func(text string) (keep string, candidates []string)
	ReadClipboard  func() (string, bool)
	WriteClipboard func(text string) bool
	// OnJump is told before a jump motion moves the cursor.
	OnJump func(from Pos)
	// FollowLink is `gd`, CopyLink is `gy`, OpenURL is `gx`.
	FollowLink func()
	CopyLink   func()
	OpenURL    func()
	// LineRows is how many screen rows a line takes when soft-wrapped.
	LineRows func(line int) int
	// Changed is called after every committed change.
	Changed func()
}

type snapshot struct {
	lines  [][]rune
	cursor Pos
}

type undoEntry struct {
	before, after snapshot
}

type lastChange struct {
	keys     []Key
	count    int
	register rune
}

type cmdlineState struct {
	kind            rune
	text            []rune
	pos             int
	histIdx         int
	saved           string
	original        []rune
	searchOrg       Pos
	pendingRegister bool
	// completion cycling on the ex line
	completions []string
	compIdx     int
	compPrefix  string
	compBase    string
}

// pendingOpSearch is an operator waiting on a `/` or `?` motion typed on
// the command line, like `d/foo<CR>`.

type insertState struct {
	start        Pos
	escPending   bool
	ctrlO        bool
	ctrlR        bool
	ctrlV        bool
	completion   *completionState
	pairedInsert Pos
}

type completionState struct {
	prefix     string
	candidates []string
	idx        int
	start      Pos
}

// Editor is one buffer with its full Vim state.
type Editor struct {
	buf   *Buffer
	mode  Mode
	opts  Options
	hooks Hooks

	cursor     Pos
	desiredCol int

	// anchor is the selection tail and kind what it covers; both are only
	// meaningful in ModeSelect, where the selection runs from anchor to
	// cursor (either order). Normal mode is a collapsed selection on the
	// cursor character, the way Helix models cursors.
	anchor     Pos
	kind       selKind
	lastSelect struct {
		start, end Pos
		kind       selKind
		set        bool
	}

	registers map[rune]Register
	marks     map[rune]Pos

	undoStack  []undoEntry
	redoStack  []undoEntry
	active     *snapshot
	version    int
	savedAtVer int

	pending []Key

	last          lastChange
	changeKeys    []Key
	recordingIns  bool
	replaying     bool
	dotCountOvr   int
	lastInsertKey []Key

	macroReg   rune
	macroKeys  []Key
	lastMacro  rune
	macroDepth int
	lastExLine string
	lastSubst  *substitution
	lastSearch string
	searchDir  int
	highlight  bool
	lastFind   struct {
		ch   rune
		kind rune
		set  bool
	}
	lastMotion motion
	// motionSelected marks a command that deliberately leaves a selection
	// behind (the selector motions); finishNormal collapses stale anchors
	// after every other normal-mode command.
	motionSelected bool
	// selExcl marks a selection whose upper-bound character is not part of
	// it: word-start motions select up to the next word's first character
	// without including it (Helix keeps that char as the exclusive head).
	selExcl        bool
	lastMotionArgs struct {
		count    int
		hasCount bool
	}
	cmd         cmdlineState
	ins         insertState
	histories   map[rune]*[]string
	incremental *regexp.Regexp
	blockInsert *blockInsertState

	rows      int
	scrollTop int
	msg       string
	msgErr    bool

	folds map[int]int

	inChange bool
}

// New creates an editor over text.
func New(text string, opts Options, hooks Hooks) *Editor {
	return NewWithRegisters(text, opts, hooks, nil)
}

// NewWithRegisters creates an editor with session-wide registers. Cursor,
// selections, marks, and undo history remain local to this editor. A nil store
// gives the editor private registers, as New does.
func NewWithRegisters(text string, opts Options, hooks Hooks, store *RegisterStore) *Editor {
	if opts.TabSize <= 0 {
		opts.TabSize = 4
	}
	if store == nil {
		store = &RegisterStore{}
	}
	if store.values == nil {
		store.values = make(map[rune]Register)
	}
	e := &Editor{
		buf:        NewBuffer(text),
		opts:       opts,
		hooks:      hooks,
		registers:  store.values,
		marks:      map[rune]Pos{},
		desiredCol: -1,
		searchDir:  1,
		rows:       24,
		folds:      map[int]int{},
	}
	if !opts.HelixEnabled {
		e.mode = ModeInsert
	}
	return e
}

// --- accessors ---

func (e *Editor) Text() string           { return e.buf.Text() }
func (e *Editor) Lines() []string        { return e.buf.Lines() }
func (e *Editor) LineCount() int         { return e.buf.LineCount() }
func (e *Editor) Line(i int) string      { return e.buf.LineString(i) }
func (e *Editor) LineRunes(i int) []rune { return e.buf.Line(i) }
func (e *Editor) Cursor() Pos            { return e.cursor }
func (e *Editor) Mode() Mode             { return e.mode }
func (e *Editor) Options() Options       { return e.opts }
func (e *Editor) Version() int           { return e.version }
func (e *Editor) ScrollTop() int         { return e.scrollTop }
func (e *Editor) Rows() int              { return e.rows }

// SetOptions replaces the options live.
func (e *Editor) SetOptions(opts Options) {
	if opts.TabSize <= 0 {
		opts.TabSize = 4
	}
	wasEnabled := e.opts.HelixEnabled
	e.opts = opts
	if wasEnabled && !opts.HelixEnabled {
		e.leaveToInsertOnly()
	}
	if !wasEnabled && opts.HelixEnabled && e.mode == ModeInsert {
		e.mode = ModeNormal
		e.cursor = e.buf.clampNormal(e.cursor)
	}
}

func (e *Editor) leaveToInsertOnly() {
	e.pending = nil
	e.commitChange()
	e.mode = ModeInsert
	e.cursor = e.buf.clampInsert(e.cursor)
}

// SetHooks replaces the host hooks.
func (e *Editor) SetHooks(h Hooks) { e.hooks = h }

// Dirty is true when the buffer changed since the last MarkSaved.
func (e *Editor) Dirty() bool { return e.version != e.savedAtVer }

// MarkSaved records the current version as the on-disk one.
func (e *Editor) MarkSaved() { e.savedAtVer = e.version }

// Message is the last status message and whether it is an error.
func (e *Editor) Message() (string, bool) { return e.msg, e.msgErr }

// ClearMessage drops the status message.
func (e *Editor) ClearMessage() { e.msg, e.msgErr = "", false }

func (e *Editor) setMsg(s string)   { e.msg, e.msgErr = s, false }
func (e *Editor) setError(s string) { e.msg, e.msgErr = s, true }

// Pending is the keys of an unfinished command, for a showcmd area.
func (e *Editor) Pending() string {
	if e.mode == ModeNormal || e.mode == ModeSelect {
		return KeysString(e.pending)
	}
	return ""
}

// Recording is the register a macro is being recorded into, or 0.
func (e *Editor) Recording() rune { return e.macroReg }

// SetViewport tells the editor how many screen rows it has.
func (e *Editor) SetViewport(rows int) {
	if rows < 1 {
		rows = 1
	}
	e.rows = rows
}

// SetScrollTop moves the viewport.
func (e *Editor) SetScrollTop(line int) {
	e.scrollTop = min(max(0, line), e.buf.LineCount()-1)
}

// SetCursor moves the cursor, clamped for the current mode.
func (e *Editor) SetCursor(p Pos) {
	if e.mode == ModeInsert {
		e.cursor = e.buf.clampInsert(p)
	} else {
		e.cursor = e.buf.clampNormal(p)
	}
	if e.mode != ModeSelect {
		e.anchor = e.cursor
		e.selExcl = false
	}
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
}

// SetText replaces the text and resets history.
func (e *Editor) SetText(text string) {
	e.buf.SetText(text)
	e.undoStack, e.redoStack, e.active = nil, nil, nil
	e.version, e.savedAtVer = 0, 0
	e.cursor = e.buf.clampNormal(Pos{})
	e.scrollTop = 0
	e.folds = map[int]int{}
	if e.mode == ModeInsert {
		e.cursor = e.buf.clampInsert(e.cursor)
	}
}

// ReplaceText swaps the text in as a change (undoable) keeping the cursor
// where it was where possible, for a note reloaded from disk.
func (e *Editor) ReplaceText(text string) {
	e.beginChange()
	e.buf.SetText(text)
	e.cursor = e.buf.clampNormal(e.cursor)
	if e.mode == ModeInsert {
		e.cursor = e.buf.clampInsert(e.cursor)
	}
	e.commitChange()
}

// Marks copies the mark table.
func (e *Editor) Marks() map[rune]Pos {
	out := map[rune]Pos{}
	for k, v := range e.marks {
		out[k] = v
	}
	return out
}

// RegisterText reads a register.
func (e *Editor) RegisterText(r rune) (Register, bool) {
	reg, ok := e.registers[r]
	return reg, ok
}

// --- undo ---

func (e *Editor) beginChange() {
	if e.active != nil {
		return
	}
	e.active = &snapshot{lines: e.buf.Snapshot(), cursor: e.cursor}
}

func (e *Editor) commitChange() {
	if e.active == nil {
		return
	}
	before := e.active
	e.active = nil
	if sameLines(before.lines, e.buf.lines) {
		return
	}
	e.undoStack = append(e.undoStack, undoEntry{
		before: *before,
		after:  snapshot{lines: e.buf.Snapshot(), cursor: e.cursor},
	})
	if len(e.undoStack) > 1000 {
		e.undoStack = e.undoStack[len(e.undoStack)-1000:]
	}
	e.redoStack = nil
	e.version++
	e.marks['.'] = e.cursor
	e.repairFolds()
	if e.hooks.Changed != nil {
		e.hooks.Changed()
	}
}

func sameLines(a, b [][]rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

// Undo reverts the last change.
func (e *Editor) Undo() {
	e.commitChange()
	if len(e.undoStack) == 0 {
		e.setError("Already at oldest change")
		return
	}
	entry := e.undoStack[len(e.undoStack)-1]
	e.undoStack = e.undoStack[:len(e.undoStack)-1]
	e.redoStack = append(e.redoStack, entry)
	e.buf.Restore(entry.before.lines)
	e.cursor = e.buf.clampNormal(entry.before.cursor)
	e.version++
	e.repairFolds()
	if e.hooks.Changed != nil {
		e.hooks.Changed()
	}
}

// Redo re-applies an undone change.
func (e *Editor) Redo() {
	e.commitChange()
	if len(e.redoStack) == 0 {
		e.setError("Already at newest change")
		return
	}
	entry := e.redoStack[len(e.redoStack)-1]
	e.redoStack = e.redoStack[:len(e.redoStack)-1]
	e.undoStack = append(e.undoStack, entry)
	e.buf.Restore(entry.after.lines)
	e.cursor = e.buf.clampNormal(entry.after.cursor)
	e.version++
	e.repairFolds()
	if e.hooks.Changed != nil {
		e.hooks.Changed()
	}
}

// --- key entry ---

// Feed runs keys given in Vim notation.
func (e *Editor) Feed(notation string) {
	for _, k := range ParseKeys(notation) {
		e.HandleKey(k)
	}
}

// HandleKey processes one keystroke. It reports false only in plain (non
// Vim) mode for keys the editor has no use for, so the host may act.
func (e *Editor) HandleKey(k Key) bool {
	if e.macroReg != 0 && !e.replaying && e.macroDepth == 0 {
		e.macroKeys = append(e.macroKeys, k)
	}
	if e.recordingIns && !e.replaying {
		e.changeKeys = append(e.changeKeys, k)
	}
	switch e.mode {
	case ModeInsert:
		if !e.opts.HelixEnabled {
			return e.plainKey(k)
		}
		e.insertKey(k)
		return true
	case ModeCmdline:
		e.cmdlineKey(k)
		return true
	case ModeSelect:
		e.selectKey(k)
		return true
	default:
		e.normalKey(k)
		return true
	}
}

// --- normal mode parsing ---

type parseStatus int

const (
	parseIncomplete parseStatus = iota
	parseInvalid
	parseComplete
)

func (e *Editor) normalKey(k Key) {
	e.ClearMessage()
	e.pending = append(e.pending, k)
	status := e.runNormal(e.pending)
	if status != parseIncomplete {
		e.pending = nil
	}
}

// splitPrefix reads an optional register and count off the front of keys.
func splitPrefix(keys []Key) (register rune, count int, hasCount bool, rest []Key, status parseStatus) {
	i := 0
	for {
		if i >= len(keys) {
			return register, count, hasCount, nil, parseIncomplete
		}
		if keys[i].IsRune('"') {
			if i+1 >= len(keys) {
				return register, count, hasCount, nil, parseIncomplete
			}
			r := keys[i+1]
			if !r.Printable() || !validRegister(r.Rune) {
				return 0, 0, false, nil, parseInvalid
			}
			register = r.Rune
			i += 2
			continue
		}
		break
	}
	for i < len(keys) && keys[i].Printable() && keys[i].Rune >= '0' && keys[i].Rune <= '9' && !(keys[i].Rune == '0' && !hasCount) {
		count = count*10 + int(keys[i].Rune-'0')
		hasCount = true
		i++
	}
	if i >= len(keys) {
		return register, count, hasCount, nil, parseIncomplete
	}
	return register, count, hasCount, keys[i:], parseComplete
}

func validRegister(r rune) bool {
	return r == '"' || r == '_' || r == '+' || r == '*' || r == '-' || r == '.' || r == ':' || r == '/' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// runNormal applies a completed normal-mode key sequence. There is no
// operator-pending state: `d`, `y`, `c` and friends act on the current
// selection the Helix way, so a sequence is a prefix plus one command key.
func (e *Editor) runNormal(keys []Key) parseStatus {
	e.motionSelected = false
	register, count, hasCount, rest, status := splitPrefix(keys)
	if status != parseComplete {
		return status
	}
	versionBefore := e.version
	enteredInsert := false

	st, entered := e.normalCommand(rest, count, hasCount, register)
	if st != parseComplete {
		return st
	}
	enteredInsert = entered
	e.finishNormal(keys, versionBefore, enteredInsert, register, count, hasCount)
	return parseComplete
}

// finishNormal closes the undo transaction and records dot-repeat.
func (e *Editor) finishNormal(keys []Key, versionBefore int, enteredInsert bool, register rune, count int, hasCount bool) {
	if enteredInsert {
		if !e.replaying {
			e.recordingIns = true
			e.changeKeys = append([]Key(nil), keys...)
		}
		return
	}
	e.commitChange()
	if e.version != versionBefore && !e.replaying && !isUndoRedo(keys) {
		e.last = lastChange{keys: stripCount(keys), count: count, register: register}
		if !hasCount {
			e.last.count = 0
		}
	}
	e.recordingIns = false
	if !e.motionSelected && e.mode != ModeSelect {
		// Every non-selector normal-mode command leaves a collapsed
		// selection, so a stale anchor never lingers as a highlight.
		e.anchor = e.cursor
		e.selExcl = false
	}
	e.motionSelected = false
	e.cursor = e.buf.clampNormal(e.cursor)
	e.ensureCursorVisible()
}

func isUndoRedo(keys []Key) bool {
	_, _, _, rest, st := splitPrefix(keys)
	if st != parseComplete || len(rest) == 0 {
		return false
	}
	k := rest[0]
	return k.IsRune('u') || k.IsRune('U') || k.IsCtrl('r') || k.IsRune('.') || k.IsRune('@') || k.IsRune('q')
}

// stripCount removes the leading register and count so `.` can supply its
// own count.
func stripCount(keys []Key) []Key {
	i := 0
	for i+1 < len(keys) && keys[i].IsRune('"') {
		i += 2
	}
	for i < len(keys) && keys[i].Printable() && keys[i].Rune >= '0' && keys[i].Rune <= '9' && !(keys[i].Rune == '0' && (i == 0 || !keys[i-1].Printable() || keys[i-1].Rune < '0' || keys[i-1].Rune > '9')) {
		i++
	}
	return append([]Key(nil), keys[i:]...)
}

// --- normal mode commands ---

// normalCommand runs one normal-mode command key (after the register and
// count prefix). Selection operations — delete, yank, change and friends —
// act on the current selection, which is the single character under the
// cursor unless a selection is active. Returns the parse status and whether
// the command entered insert mode.
func (e *Editor) normalCommand(rest []Key, count int, hasCount bool, register rune) (parseStatus, bool) {
	if len(rest) == 0 {
		return parseIncomplete, false
	}
	k := rest[0]
	n := max(1, count)
	if k.Printable() {
		switch k.Rune {
		// --- entering insert ---
		// Helix inserts before the selection (`i`) or after it (`a`);
		// on a collapsed selection that is the usual cursor behaviour.
		case 'i':
			start, _ := e.selRange()
			e.startInsert(e.buf.clampNormal(start))
			return parseComplete, true
		case 'a':
			_, end := e.selRange()
			e.startInsert(e.stepInsertPos(e.buf.clampNormal(end)))
			return parseComplete, true
		case 'I':
			p := Pos{e.cursor.Line, firstNonBlankInsert(e.buf.Line(e.cursor.Line))}
			if e.opts.WrappedLineMotions && e.hooks.DisplayRowBounds != nil {
				start, end := e.hooks.DisplayRowBounds(e.cursor)
				p.Col = start
				line := e.buf.Line(p.Line)
				for p.Col < end && (line[p.Col] == ' ' || line[p.Col] == '\t') {
					p.Col++
				}
			}
			e.startInsert(p)
			return parseComplete, true
		case 'A':
			p := Pos{e.cursor.Line, e.buf.LineLen(e.cursor.Line)}
			if e.opts.WrappedLineMotions && e.hooks.DisplayRowBounds != nil {
				_, p.Col = e.hooks.DisplayRowBounds(e.cursor)
			}
			e.startInsert(p)
			return parseComplete, true
		case 'o':
			e.beginChange()
			e.openLine(false)
			return parseComplete, true
		case 'O':
			e.beginChange()
			e.openLine(true)
			return parseComplete, true
		// --- the selection ---
		case 'v':
			e.enterSelect(selChar)
			return parseComplete, false
		case 'V':
			e.enterSelect(selLine)
			return parseComplete, false
		case 'x':
			e.extendLineBelow()
			return parseComplete, false
		case 'X':
			e.extendToLineBounds()
			return parseComplete, false
		case ';':
			e.collapseToCursor()
			return parseComplete, false
		case '%':
			e.selectAll()
			return parseComplete, false
		// --- selection operations ---
		case 'd':
			e.beginChange()
			e.runOperator("d", e.selectionSpan(), register, false)
			return parseComplete, false
		case 'y':
			e.runOperator("y", e.selectionSpan(), register, false)
			return parseComplete, false
		case 'c':
			e.beginChange()
			entered := e.runOperator("c", e.selectionSpan(), register, false)
			return parseComplete, entered
		case 'p':
			e.beginChange()
			e.putSelection(register, n, true)
			return parseComplete, false
		case 'P':
			e.beginChange()
			e.putSelection(register, n, false)
			return parseComplete, false
		case 'R':
			e.beginChange()
			e.putOverSpan(e.selectionSpan(), register, n, false)
			return parseComplete, false
		case '>':
			e.beginChange()
			e.indentSelection(1)
			return parseComplete, false
		case '<':
			e.beginChange()
			e.indentSelection(-1)
			return parseComplete, false
		case '~':
			e.beginChange()
			e.transformSelection("g~")
			return parseComplete, false
		case '`':
			e.beginChange()
			e.transformSelection("gu")
			return parseComplete, false
		case 'r':
			if len(rest) < 2 {
				return parseIncomplete, false
			}
			if !rest[1].Printable() {
				return parseInvalid, false
			}
			e.beginChange()
			e.replaceSelectionWith(rest[1].Rune)
			return parseComplete, false
		case 'J':
			e.beginChange()
			e.joinSelection(n)
			return parseComplete, false
		// --- search ---
		case '*':
			e.searchSelection(false)
			return parseComplete, false
		case '#':
			e.searchWordUnderCursor(1)
			return parseComplete, false
		case ':':
			e.openCmdline(':', count, hasCount)
			return parseComplete, false
		case '/':
			e.openCmdline('/', count, hasCount)
			return parseComplete, false
		case '?':
			e.openCmdline('?', count, hasCount)
			return parseComplete, false
		// --- macros ---
		case 'Q':
			if e.macroReg != 0 {
				e.stopMacro()
				return parseComplete, false
			}
			reg := register
			if reg == 0 {
				reg = 'q'
			}
			if validRegister(reg) {
				e.macroReg = unicode.ToLower(reg)
				e.macroKeys = nil
				e.setMsg("recording @" + string(e.macroReg))
			}
			return parseComplete, false
		case 'q':
			reg := register
			if reg == 0 {
				reg = 'q'
			}
			e.runMacro(reg, n)
			return parseComplete, false
		case '@':
			if len(rest) < 2 {
				return parseIncomplete, false
			}
			if rest[1].Printable() {
				e.runMacro(rest[1].Rune, n)
			}
			return parseComplete, false
		// --- undo, redo, repeat ---
		case 'u':
			for i := 0; i < n; i++ {
				e.Undo()
			}
			return parseComplete, false
		case 'U':
			for i := 0; i < n; i++ {
				e.Redo()
			}
			return parseComplete, false
		case '.':
			e.repeatLastChange(count, hasCount)
			return parseComplete, false
		// --- extras kept from ZenNotes (unbound in Helix) ---
		case 'D':
			e.beginChange()
			e.deleteToEnd(n, register)
			e.cursor = e.buf.clampNormal(e.cursor)
			return parseComplete, false
		case 'C':
			e.beginChange()
			e.deleteToEnd(n, register)
			e.startInsert(Pos{e.cursor.Line, e.buf.LineLen(e.cursor.Line)})
			return parseComplete, true
		case '&':
			e.beginChange()
			e.repeatSubstitute(false)
			return parseComplete, false
		case ',':
			e.repeatFindChar(-1, n)
			return parseComplete, false
		case 'Z':
			if len(rest) < 2 {
				return parseIncomplete, false
			}
			if rest[1].IsRune('Z') {
				e.hostEx(ExCommand{Name: "wq", Raw: "wq"})
			} else if rest[1].IsRune('Q') {
				e.hostEx(ExCommand{Name: "q", Bang: true, Raw: "q!"})
			}
			return parseComplete, false
		// --- minor modes ---
		case 'g':
			return e.gotoMode(rest, count, hasCount)
		case 'z':
			return e.viewMode(rest, n)
		case 'm':
			return e.matchMode(rest)
		}
	}
	if k.IsCtrl('r') {
		for i := 0; i < n; i++ {
			e.Redo()
		}
		return parseComplete, false
	}
	if k.IsCtrl('a') || k.IsCtrl('x') {
		e.beginChange()
		delta := n
		if k.IsCtrl('x') {
			delta = -n
		}
		e.incrementNumber(delta)
		return parseComplete, false
	}
	if k.IsCtrl('v') {
		e.enterSelect(selBlock)
		return parseComplete, false
	}
	if k.IsAlt('.') {
		e.repeatLastMotion(n)
		return parseComplete, false
	}
	if k.IsCtrl('d') {
		e.scrollHalfPage(1, count, hasCount)
		return parseComplete, false
	}
	if k.IsCtrl('u') {
		e.scrollHalfPage(-1, count, hasCount)
		return parseComplete, false
	}
	if k.IsCtrl('f') || k.Is("pgdn") {
		e.scrollPage(n)
		return parseComplete, false
	}
	if k.IsCtrl('b') || k.Is("pgup") {
		e.scrollPage(-n)
		return parseComplete, false
	}
	if k.IsCtrl('e') {
		e.scrollLines(n)
		return parseComplete, false
	}
	if k.IsCtrl('y') {
		e.scrollLines(-n)
		return parseComplete, false
	}
	if k.IsCtrl('l') {
		e.highlight = false
		return parseComplete, false
	}
	if k.Is("esc") {
		e.highlightOffOnEsc()
		return parseComplete, false
	}
	if k.Is("insert") {
		e.startInsert(e.cursor)
		return parseComplete, true
	}
	if k.Is("delete") {
		e.beginChange()
		e.runOperator("d", e.selectionSpan(), register, false)
		return parseComplete, false
	}
	// Motions. Helix is selection-first: word, paragraph and find motions
	// replace the selection with the span they traverse, so `wd` deletes a
	// word and `n` selects the next match. Character and line moves collapse
	// the selection onto their target; in select mode they extend it.
	m, st := parseMotion(rest)
	if st != parseComplete {
		return st, false
	}
	switch {
	case m.kind == "n" || m.kind == "N":
		e.selectMatch(m, count, hasCount)
	case selectorMotion(m):
		e.selectByMotion(m, count, hasCount)
	case e.mode == ModeSelect:
		e.extendByMotion(m, count, hasCount)
	default:
		e.moveByMotion(m, count, hasCount)
	}
	return parseComplete, false
}

// gotoMode is Helix's goto mode (g): file, line and screen positions plus the
// ZenNotes link keys.
func (e *Editor) gotoMode(rest []Key, count int, hasCount bool) (parseStatus, bool) {
	if len(rest) < 2 {
		return parseIncomplete, false
	}
	k := rest[1]
	if !k.Printable() {
		return parseInvalid, false
	}
	switch k.Rune {
	case 'g':
		// First line, or line N with a count.
		if hasCount {
			e.moveByMotion(motion{kind: "G"}, count, true)
		} else {
			e.moveByMotion(motion{kind: "gg"}, 1, false)
		}
		return parseComplete, false
	case 'e':
		e.moveByMotion(motion{kind: "G"}, 1, false)
		return parseComplete, false
	case 'j', 'k':
		e.moveByMotion(motion{kind: "g" + string(k.Rune)}, 1, false)
		return parseComplete, false
	case 'h':
		e.moveByMotion(motion{kind: "0"}, 1, false)
		return parseComplete, false
	case 'l':
		e.moveByMotion(motion{kind: "$"}, 1, false)
		return parseComplete, false
	case 's':
		e.moveByMotion(motion{kind: "^"}, 1, false)
		return parseComplete, false
	case 't':
		e.moveByMotion(motion{kind: "H"}, 1, false)
		return parseComplete, false
	case 'c':
		e.moveByMotion(motion{kind: "M"}, 1, false)
		return parseComplete, false
	case 'b':
		e.moveByMotion(motion{kind: "L"}, 1, false)
		return parseComplete, false
	case 'd':
		if e.hooks.FollowLink != nil {
			e.hooks.FollowLink()
		}
		return parseComplete, false
	case 'y':
		if e.hooks.CopyLink != nil {
			e.hooks.CopyLink()
		}
		return parseComplete, false
	case 'x':
		if e.hooks.OpenURL != nil {
			e.hooks.OpenURL()
		}
		return parseComplete, false
	case 'v':
		e.reselect()
		return parseComplete, false
	case '&':
		e.beginChange()
		e.repeatSubstitute(true)
		return parseComplete, false
	}
	return parseInvalid, false
}

// viewMode is Helix's view mode (z): where the cursor line sits on screen,
// plus the ZenNotes fold keys (folds moved off z c and z o, which Helix
// gives to centre view and the fold motion pair).
func (e *Editor) viewMode(rest []Key, n int) (parseStatus, bool) {
	if len(rest) < 2 {
		return parseIncomplete, false
	}
	k := rest[1]
	switch {
	case k.IsRune('z') || k.IsRune('c') || k.IsRune('.'):
		e.scrollCursorTo(0.5)
		if k.IsRune('.') {
			e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
		}
	case k.IsRune('t') || k.Is("enter"):
		e.scrollCursorTo(0)
		if k.Is("enter") {
			e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
		}
	case k.IsRune('b') || k.IsRune('-'):
		e.scrollCursorTo(1)
		if k.IsRune('-') {
			e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
		}
	case k.IsRune('j'):
		e.scrollLines(n)
	case k.IsRune('k'):
		e.scrollLines(-n)
	case k.IsRune('f'):
		e.foldAtCursor()
	case k.IsRune('o'):
		e.unfoldAtCursor()
	case k.IsRune('a'):
		e.toggleFoldAtCursor()
	case k.IsRune('M'):
		e.foldAll()
	case k.IsRune('R'):
		e.unfoldAll()
	case k.IsRune('J'):
		e.moveToFold(1, n)
	case k.IsRune('K'):
		e.moveToFold(-1, n)
	default:
		return parseInvalid, false
	}
	return parseComplete, false
}

func (e *Editor) highlightOffOnEsc() {}

// --- registers ---

func (e *Editor) readRegister(r rune) (Register, bool) {
	switch r {
	case 0, '"':
		if e.opts.YankToClipboard {
			if text, ok := e.readClipboard(); ok {
				reg := e.registers['"']
				if text != reg.Text {
					return Register{Text: text, Linewise: strings.HasSuffix(text, "\n")}, true
				}
			}
		}
		reg, ok := e.registers['"']
		return reg, ok
	case '+', '*':
		if text, ok := e.readClipboard(); ok {
			return Register{Text: text, Linewise: strings.HasSuffix(text, "\n") && strings.Count(text, "\n") >= 1}, true
		}
		reg, ok := e.registers['"']
		return reg, ok
	case '_':
		return Register{}, false
	case '/':
		return Register{Text: e.lastSearch}, e.lastSearch != ""
	case ':':
		return Register{Text: e.lastExLine}, e.lastExLine != ""
	}
	reg, ok := e.registers[unicode.ToLower(r)]
	return reg, ok
}

func (e *Editor) readClipboard() (string, bool) {
	if e.hooks.ReadClipboard == nil {
		return "", false
	}
	return e.hooks.ReadClipboard()
}

// storeRegister files a yank or delete the way Vim does: the named
// register when one was given (uppercase appends), the unnamed register
// always, `0` for yanks, `1`..`9` shifting for multi-line deletes and `-`
// for small ones.
func (e *Editor) storeRegister(named rune, reg Register, isYank bool) {
	if named == '_' {
		return
	}
	if named != 0 && named != '"' {
		switch {
		case named == '+' || named == '*':
			e.writeClipboard(reg.Text)
		case named >= 'A' && named <= 'Z':
			lower := unicode.ToLower(named)
			prev := e.registers[lower]
			if prev.Linewise || reg.Linewise {
				text := prev.Text
				if !strings.HasSuffix(text, "\n") && text != "" {
					text += "\n"
				}
				text += reg.Text
				if !strings.HasSuffix(text, "\n") {
					text += "\n"
				}
				e.registers[lower] = Register{Text: text, Linewise: true}
			} else {
				e.registers[lower] = Register{Text: prev.Text + reg.Text}
			}
			reg = e.registers[lower]
		default:
			e.registers[named] = reg
		}
	} else {
		if isYank {
			e.registers['0'] = reg
		} else if reg.Linewise || strings.Contains(reg.Text, "\n") {
			for i := '9'; i > '1'; i-- {
				if prev, ok := e.registers[i-1]; ok {
					e.registers[i] = prev
				}
			}
			e.registers['1'] = reg
		} else {
			e.registers['-'] = reg
		}
	}
	e.registers['"'] = reg
	if e.opts.YankToClipboard && named != '+' && named != '*' {
		e.writeClipboard(reg.Text)
	}
}

func (e *Editor) writeClipboard(text string) {
	if e.hooks.WriteClipboard != nil {
		e.hooks.WriteClipboard(text)
	}
}

// --- dot repeat and macros ---

func (e *Editor) repeatLastChange(count int, hasCount bool) {
	if len(e.last.keys) == 0 {
		return
	}
	keys := e.last.keys
	prefix := []Key{}
	if e.last.register != 0 {
		prefix = append(prefix, R('"'), R(e.last.register))
	}
	c := e.last.count
	if hasCount {
		c = count
		e.last.count = count
	}
	if c > 0 {
		for _, r := range strconv.Itoa(c) {
			prefix = append(prefix, R(r))
		}
	}
	e.replaying = true
	e.pending = nil
	for _, k := range append(prefix, keys...) {
		e.HandleKey(k)
	}
	if e.mode == ModeInsert {
		e.HandleKey(KeyEsc)
	}
	e.replaying = false
}

func (e *Editor) stopMacro() {
	keys := e.macroKeys
	if len(keys) > 0 {
		keys = keys[:len(keys)-1]
	}
	reg := e.macroReg
	e.macroReg = 0
	e.macroKeys = nil
	e.registers[reg] = Register{Text: KeysString(keys)}
	e.setMsg("")
}

func (e *Editor) runMacro(r rune, n int) {
	if r == '@' {
		r = e.lastMacro
	}
	if r == ':' {
		for i := 0; i < n; i++ {
			e.executeExLine(e.lastExLine)
		}
		return
	}
	reg, ok := e.readRegister(unicode.ToLower(r))
	if !ok || reg.Text == "" || e.macroDepth > 50 {
		return
	}
	e.lastMacro = unicode.ToLower(r)
	keys := ParseKeys(reg.Text)
	e.macroDepth++
	e.pending = nil
	for i := 0; i < n; i++ {
		for _, k := range keys {
			e.HandleKey(k)
		}
	}
	e.macroDepth--
}

// --- cursor movement helpers ---

// selectorMotion reports whether the motion selects the span it traverses.
// Helix's word, paragraph and find motions are selectors: `w` selects up to
// the next word start, so `wd` deletes a word and `fx` selects through x.
func selectorMotion(m motion) bool {
	switch m.kind {
	case "w", "W", "b", "B", "e", "E", "f", "F", "t", "T", "{", "}", "(", ")":
		return true
	}
	return false
}

// selectByMotion applies a selector motion: the anchor drops at the old
// cursor and the selection head lands on the motion target.
func (e *Editor) selectByMotion(m motion, count int, hasCount bool) {
	from := e.cursor
	e.lastMotion = m
	e.lastMotionArgs.count, e.lastMotionArgs.hasCount = count, hasCount
	res, ok := e.applyMotion(m, count, hasCount, e.cursor, false)
	if !ok {
		return
	}
	if res.jump && e.cursor != res.pos {
		e.pushJump()
	}
	e.motionSelected = true
	// Word-start motions stop at the next word's first character without
	// selecting it; every other selector includes its target.
	e.selExcl = m.kind == "w" || m.kind == "W" || m.kind == "b" || m.kind == "B"
	e.anchor = e.buf.clampNormal(from)
	e.cursor = e.buf.clampNormal(res.pos)
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
}

// selectMatch replaces the selection with the next search match (Helix's
// `n`/`N` select the match, not just its start).
func (e *Editor) selectMatch(m motion, count int, hasCount bool) {
	e.lastMotion = m
	e.lastMotionArgs.count, e.lastMotionArgs.hasCount = count, hasCount
	res, ok := e.applyMotion(m, count, hasCount, e.cursor, false)
	if !ok {
		return
	}
	if res.jump {
		e.pushJump()
	}
	e.motionSelected = true
	// The selection covers the whole match; the head sits at the end for a
	// forward search and at the start for a backward one.
	if e.searchDir >= 0 {
		e.anchor = e.buf.clampNormal(res.pos)
		e.cursor = e.buf.clampNormal(Pos{res.matchEnd.Line, max(0, res.matchEnd.Col-1)})
	} else {
		e.anchor = e.buf.clampNormal(Pos{res.matchEnd.Line, max(0, res.matchEnd.Col-1)})
		e.cursor = e.buf.clampNormal(res.pos)
	}
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
}

func (e *Editor) moveByMotion(m motion, count int, hasCount bool) {
	from := e.cursor
	e.lastMotion = m
	e.lastMotionArgs.count, e.lastMotionArgs.hasCount = count, hasCount
	res, ok := e.applyMotion(m, count, hasCount, e.cursor, false)
	if !ok {
		return
	}
	if res.jump && e.cursor != res.pos {
		e.pushJump()
	}
	e.cursor = e.buf.clampNormal(res.pos)
	if res.keepCol {
		if e.desiredCol < 0 {
			e.desiredCol = from.Col
		}
	} else if res.stickyEnd {
		e.desiredCol = math.MaxInt32
	} else {
		e.desiredCol = -1
	}
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
}

// repeatLastMotion replays the last motion (Helix's Alt-.); in select mode
// it extends the selection instead of collapsing it.
func (e *Editor) repeatLastMotion(n int) {
	if e.lastMotion.kind == "" {
		return
	}
	count := e.lastMotionArgs.count
	hasCount := e.lastMotionArgs.hasCount
	if n > 1 {
		count = n * max(1, count)
		hasCount = true
	}
	if e.mode == ModeSelect {
		if selectorMotion(e.lastMotion) {
			e.selectByMotion(e.lastMotion, count, hasCount)
		} else {
			e.extendByMotion(e.lastMotion, count, hasCount)
		}
		return
	}
	if selectorMotion(e.lastMotion) {
		e.selectByMotion(e.lastMotion, count, hasCount)
		return
	}
	e.moveByMotion(e.lastMotion, count, hasCount)
}

func (e *Editor) pushJump() {
	e.marks['\''] = e.cursor
	e.marks['`'] = e.cursor
	if e.hooks.OnJump != nil {
		e.hooks.OnJump(e.cursor)
	}
}

// --- scrolling ---

func (e *Editor) lineRows(line int) int {
	if e.hooks.LineRows != nil {
		if n := e.hooks.LineRows(line); n > 0 {
			return n
		}
	}
	return 1
}

// visibleLines walks from scrollTop counting screen rows, skipping folded
// lines, and returns the last line that fits.
func (e *Editor) lastVisibleLine() int {
	rows := 0
	last := e.scrollTop
	for line := e.scrollTop; line < e.buf.LineCount(); line = e.nextVisibleLine(line) {
		rows += e.lineRows(line)
		if rows > e.rows {
			break
		}
		last = line
	}
	return last
}

func (e *Editor) nextVisibleLine(line int) int {
	if end, ok := e.folds[line]; ok {
		return end + 1
	}
	return line + 1
}

func (e *Editor) prevVisibleLine(line int) int {
	prev := line - 1
	if prev < 0 {
		return -1
	}
	if start, ok := e.foldContaining(prev); ok {
		return start
	}
	return prev
}

// EnsureCursorVisible scrolls so the cursor is on screen, honoring
// scrolloff.
func (e *Editor) EnsureCursorVisible() { e.ensureCursorVisible() }

func (e *Editor) ensureCursorVisible() {
	if e.buf.LineCount() == 0 {
		return
	}
	if start, ok := e.foldContaining(e.cursor.Line); ok && start != e.cursor.Line {
		e.cursor.Line = start
	}
	so := min(e.opts.ScrollOff, (e.rows-1)/2)
	cur := e.cursor.Line
	// Cursor above the top.
	top := e.scrollTop
	for i := 0; i < so; i++ {
		top = e.prevVisibleLine(top)
		if top < 0 {
			top = 0
			break
		}
	}
	if cur < e.scrollTop || (cur < top && e.scrollTop > 0) {
		e.scrollTop = cur
		for i := 0; i < so; i++ {
			p := e.prevVisibleLine(e.scrollTop)
			if p < 0 {
				break
			}
			e.scrollTop = p
		}
		return
	}
	// Cursor below the bottom: scroll until cur plus scrolloff rows fits.
	for {
		rows := 0
		fits := false
		line := e.scrollTop
		extra := 0
		for line < e.buf.LineCount() {
			rows += e.lineRows(line)
			if rows > e.rows {
				break
			}
			if line == cur {
				fits = true
			}
			if line > cur {
				extra++
			}
			line = e.nextVisibleLine(line)
		}
		if fits && (extra >= so || line >= e.buf.LineCount()) {
			return
		}
		next := e.nextVisibleLine(e.scrollTop)
		if next >= e.buf.LineCount() || next > cur {
			return
		}
		e.scrollTop = next
	}
}

func (e *Editor) scrollCursorTo(fraction float64) {
	target := int(float64(e.rows-1) * fraction)
	top := e.cursor.Line
	rows := 0
	for top > 0 {
		prev := e.prevVisibleLine(top)
		if prev < 0 {
			break
		}
		rows += e.lineRows(prev)
		if rows > target {
			break
		}
		top = prev
	}
	e.scrollTop = top
}

func (e *Editor) scrollHalfPage(dir int, count int, hasCount bool) {
	amount := max(1, e.rows/2)
	if hasCount {
		amount = count
	}
	for i := 0; i < amount; i++ {
		if dir > 0 {
			next := e.nextVisibleLine(e.scrollTop)
			if next < e.buf.LineCount() {
				e.scrollTop = next
			}
			nc := e.nextVisibleLine(e.cursor.Line)
			if nc < e.buf.LineCount() {
				e.cursor.Line = nc
			}
		} else {
			if p := e.prevVisibleLine(e.scrollTop); p >= 0 {
				e.scrollTop = p
			}
			if p := e.prevVisibleLine(e.cursor.Line); p >= 0 {
				e.cursor.Line = p
			}
		}
	}
	e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
	e.ensureCursorVisible()
}

func (e *Editor) scrollPage(pages int) {
	dir := 1
	if pages < 0 {
		dir, pages = -1, -pages
	}
	for p := 0; p < pages; p++ {
		for i := 0; i < max(1, e.rows-2); i++ {
			if dir > 0 {
				next := e.nextVisibleLine(e.scrollTop)
				if next >= e.buf.LineCount() {
					break
				}
				e.scrollTop = next
			} else {
				prev := e.prevVisibleLine(e.scrollTop)
				if prev < 0 {
					break
				}
				e.scrollTop = prev
			}
		}
	}
	if dir > 0 {
		e.cursor.Line = max(e.cursor.Line, e.scrollTop)
	} else {
		e.cursor.Line = min(e.cursor.Line, e.lastVisibleLine())
	}
	e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
}

func (e *Editor) scrollLines(n int) {
	for i := 0; i < abs(n); i++ {
		if n > 0 {
			next := e.nextVisibleLine(e.scrollTop)
			if next >= e.buf.LineCount() {
				break
			}
			e.scrollTop = next
		} else {
			prev := e.prevVisibleLine(e.scrollTop)
			if prev < 0 {
				break
			}
			e.scrollTop = prev
		}
	}
	if e.cursor.Line < e.scrollTop {
		e.cursor.Line = e.scrollTop
		e.cursor = e.buf.clampNormal(e.cursor)
	}
	if last := e.lastVisibleLine(); e.cursor.Line > last {
		e.cursor.Line = last
		e.cursor = e.buf.clampNormal(e.cursor)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// --- host ex ---

func (e *Editor) hostEx(cmd ExCommand) bool {
	if e.hooks.ExCommand == nil {
		return false
	}
	handled, err := e.hooks.ExCommand(cmd)
	if err != nil {
		e.setError(err.Error())
	}
	return handled
}

// --- misc public helpers ---

// WordUnderCursor is the keyword at or after the cursor on its line.
func (e *Editor) WordUnderCursor() string {
	line := e.buf.Line(e.cursor.Line)
	if len(line) == 0 {
		return ""
	}
	col := min(e.cursor.Col, len(line)-1)
	if !isWordRune(line[col]) {
		for col < len(line) && !isWordRune(line[col]) {
			col++
		}
		if col >= len(line) {
			return ""
		}
	}
	start, end := col, col
	for start > 0 && isWordRune(line[start-1]) {
		start--
	}
	for end < len(line) && isWordRune(line[end]) {
		end++
	}
	return string(line[start:end])
}

// InsertAtCursor inserts text as one undoable change and leaves the cursor
// after it.
func (e *Editor) InsertAtCursor(text string) {
	e.beginChange()
	p := e.cursor
	if e.mode != ModeInsert && e.buf.LineLen(p.Line) > 0 && p.Col < e.buf.LineLen(p.Line) {
		p.Col++
	}
	end := e.buf.InsertText(p, text)
	e.cursor = end
	if e.mode != ModeInsert {
		e.cursor = e.buf.clampNormal(e.cursor)
		e.commitChange()
	}
}

// ReplaceAtCursor swaps the before runes left of the cursor and the after
// runes right of it for text and leaves the cursor behind the text: what
// accepting a completion does. In insert mode it joins the insert in
// progress, so one undo takes the typing and the completion together.
func (e *Editor) ReplaceAtCursor(before, after int, text string) {
	e.beginChange()
	n := e.buf.LineLen(e.cursor.Line)
	start := Pos{e.cursor.Line, max(0, e.cursor.Col-before)}
	end := Pos{e.cursor.Line, min(n, e.cursor.Col+after)}
	e.buf.DeleteRange(start, end)
	e.cursor = e.buf.InsertText(start, text)
	if e.mode != ModeInsert {
		e.cursor = e.buf.clampNormal(e.cursor)
		e.commitChange()
	}
}

// ReplaceLineText swaps one line as a change.
func (e *Editor) ReplaceLineText(line int, text string) {
	e.beginChange()
	e.buf.ReplaceLine(line, []rune(text))
	e.cursor = e.buf.clampNormal(e.cursor)
	if e.mode == ModeInsert {
		e.cursor = e.buf.clampInsert(e.cursor)
	} else {
		e.commitChange()
	}
}

// ReplaceLines swaps a block of lines as a change.
func (e *Editor) ReplaceLines(from, to int, lines []string) {
	e.beginChange()
	e.buf.ReplaceLines(from, to, lines)
	e.cursor = e.buf.clampNormal(e.cursor)
	e.commitChange()
}

// GotoLine moves to a 1-based line as a jump.
func (e *Editor) GotoLine(line int) {
	e.pushJump()
	e.cursor = e.buf.clampNormal(Pos{line - 1, 0})
	e.anchor = e.cursor
	e.selExcl = false
	e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
}

// StatusSuffix is the text a status line shows after the mode: pending
// keys and the macro being recorded.
func (e *Editor) StatusSuffix() string {
	parts := []string{}
	if p := e.Pending(); p != "" {
		parts = append(parts, p)
	}
	if e.macroReg != 0 {
		parts = append(parts, fmt.Sprintf("recording @%c", e.macroReg))
	}
	return strings.Join(parts, "  ")
}

// ResetPending drops an unfinished normal-mode command, for a host that
// takes a key sequence over from the engine.
func (e *Editor) ResetPending() { e.pending = nil }

// PendingCount is the count typed so far when the pending keys are only
// digits: the host reads it before taking over a sequence like `3gt`.
func (e *Editor) PendingCount() (int, bool) {
	if len(e.pending) == 0 {
		return 0, false
	}
	n := 0
	for _, k := range e.pending {
		if !k.Printable() || k.Rune < '0' || k.Rune > '9' {
			return 0, false
		}
		n = n*10 + int(k.Rune-'0')
	}
	return n, true
}
