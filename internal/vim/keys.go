package vim

import "github.com/ZenNotes/tui/internal/helix"

// The vim engine shares the terminal-facing key model with the helix engine
// so the TUI can drive either through one interface. These aliases keep the
// two packages signature-compatible.

type Key = helix.Key

func R(r rune) Key { return helix.R(r) }

func Ctrl(r rune) Key { return helix.Ctrl(r) }

func Alt(r rune) Key { return helix.Alt(r) }

func Special(name string) Key { return helix.Special(name) }

var (
	KeyEsc       = helix.KeyEsc
	KeyEnter     = helix.KeyEnter
	KeyTab       = helix.KeyTab
	KeyShiftTab  = helix.KeyShiftTab
	KeyBackspace = helix.KeyBackspace
	KeyDelete    = helix.KeyDelete
	KeyUp        = helix.KeyUp
	KeyDown      = helix.KeyDown
	KeyLeft      = helix.KeyLeft
	KeyRight     = helix.KeyRight
	KeyHome      = helix.KeyHome
	KeyEnd       = helix.KeyEnd
	KeyPgUp      = helix.KeyPgUp
	KeyPgDn      = helix.KeyPgDn
)

func ParseKeys(s string) []Key { return helix.ParseKeys(s) }

func KeysString(keys []Key) string { return helix.KeysString(keys) }
