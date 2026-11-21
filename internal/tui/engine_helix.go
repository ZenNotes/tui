package tui

import (
	"reflect"
	"strings"

	"github.com/ZenNotes/tui/internal/helix"
	"github.com/ZenNotes/tui/internal/vim"
)

// helixEditor adapts the helix engine to modalEditor. The helix package
// does not depend on vim; this file is the only place that knows both, and
// converts between their types. Helix's select mode reports as vim's visual
// modes, so the app treats a helix selection the way it treats a vim one.
type helixEditor struct {
	*helix.Editor
}

// helixRegisters holds a helix register store per vim store, so helix
// buffers in one session share their registers like vim buffers do.
var helixRegisters = map[*vim.RegisterStore]*helix.RegisterStore{}

func newHelixEditor(a *App, text string, opts vim.Options, hooks vim.Hooks, store *vim.RegisterStore) modalEditor {
	var hs *helix.RegisterStore
	if store != nil {
		hs = helixRegisters[store]
		if hs == nil {
			hs = &helix.RegisterStore{}
			helixRegisters[store] = hs
		}
	}
	ed := helix.NewWithRegisters(text, helixOptions(opts), helixHooks(hooks), hs)
	ed.SetKeymap(a.helixKeymap())
	return helixEditor{ed}
}

// helixKeymapCache holds the keymap parsed from the [keys] tables, so it is
// built, and its errors reported, once per change of those tables.
var helixKeymapCache struct {
	raw map[string]any
	km  *helix.Keymap
}

// helixKeymap is the user's [keys] bindings for the helix engine; nil when
// there are none. Bindings it cannot read are reported once and skipped.
func (a *App) helixKeymap() *helix.Keymap {
	raw := a.prefs.HelixKeys
	if len(raw) == 0 {
		return nil
	}
	c := &helixKeymapCache
	if c.km != nil && reflect.DeepEqual(c.raw, raw) {
		return c.km
	}
	km, errs := helix.ParseKeymap(raw)
	c.raw, c.km = raw, km
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, err := range errs {
			msgs[i] = err.Error()
		}
		a.notifyError("config.toml [keys]: " + strings.Join(msgs, "; "))
	}
	return km
}

func helixOptions(o vim.Options) helix.Options {
	return helix.Options{
		WrappedLineMotions:      o.WrappedLineMotions,
		TabSize:                 o.TabSize,
		ScrollOff:               o.ScrollOff,
		IgnoreCase:              o.IgnoreCase,
		SmartCase:               o.SmartCase,
		WrapScan:                o.WrapScan,
		InsertEscape:            o.InsertEscape,
		AutoPairs:               o.AutoPairs,
		AutoPairQuotes:          o.AutoPairQuotes,
		TextReplacements:        o.TextReplacements,
		TextReplacementsEnabled: o.TextReplacementsEnabled,
		MarkdownLists:           o.MarkdownLists,
		YankToClipboard:         o.YankToClipboard,
		HelixEnabled:            o.VimEnabled,
	}
}

func helixHooks(h vim.Hooks) helix.Hooks {
	out := helix.Hooks{
		ExComplete:     h.ExComplete,
		ReadClipboard:  h.ReadClipboard,
		WriteClipboard: h.WriteClipboard,
		FollowLink:     h.FollowLink,
		CopyLink:       h.CopyLink,
		OpenURL:        h.OpenURL,
		LineRows:       h.LineRows,
		Changed:        h.Changed,
	}
	if f := h.DisplayRowBounds; f != nil {
		out.DisplayRowBounds = func(p helix.Pos) (int, int) { return f(vim.Pos(p)) }
	}
	if f := h.ExCommand; f != nil {
		out.ExCommand = func(c helix.ExCommand) (bool, error) {
			return f(vim.ExCommand{Name: c.Name, Args: c.Args, Bang: c.Bang, Raw: c.Raw,
				Range: vim.Range{Start: c.Range.Start, End: c.Range.End, Given: c.Range.Given}})
		}
	}
	if f := h.OnJump; f != nil {
		out.OnJump = func(p helix.Pos) { f(vim.Pos(p)) }
	}
	return out
}

// mode maps helix modes onto vim's: select mode is visual, by the kind of
// selection.
func (w helixEditor) mode(m helix.Mode) vim.Mode {
	switch m {
	case helix.ModeInsert:
		return vim.ModeInsert
	case helix.ModeCmdline:
		return vim.ModeCmdline
	case helix.ModeSelect:
		switch w.Editor.SelectionKind() {
		case "V":
			return vim.ModeVisualLine
		case "^V":
			return vim.ModeVisualBlock
		}
		return vim.ModeVisual
	}
	return vim.ModeNormal
}

func (w helixEditor) Mode() vim.Mode       { return w.mode(w.Editor.Mode()) }
func (w helixEditor) ModeLabel() string    { return w.Editor.Mode().Label() }
func (w helixEditor) Cursor() vim.Pos      { return vim.Pos(w.Editor.Cursor()) }
func (w helixEditor) BlockCursor() vim.Pos { return vim.Pos(w.Editor.BlockCursor()) }
func (w helixEditor) SetCursor(p vim.Pos)  { w.Editor.SetCursor(helix.Pos(p)) }
func (w helixEditor) HandleKey(k vim.Key) bool {
	return w.Editor.HandleKey(helix.Key(k))
}
func (w helixEditor) SetHooks(h vim.Hooks)        { w.Editor.SetHooks(helixHooks(h)) }
func (w helixEditor) SetOptions(opts vim.Options) { w.Editor.SetOptions(helixOptions(opts)) }

func (w helixEditor) VisualRange() (vim.Pos, vim.Pos, vim.Mode, bool) {
	start, end, m, ok := w.Editor.VisualRange()
	return vim.Pos(start), vim.Pos(end), w.mode(m), ok
}
