package themes

import (
	_ "embed"
	"fmt"
	"sync"
)

// Tokens are the desktop's --z-* color tokens, one field per token.
type Tokens struct {
	Bg, BgSofter, Bg1, Bg2, Bg3, Bg4 RGB
	Fg, Fg1, Fg2                     RGB
	Grey2, Grey1, Grey0, GreyDim     RGB
	Accent, AccentSoft, AccentMuted  RGB
	Red, Green, Yellow, Blue, Purple RGB
	Aqua                             RGB
}

// Palette is a theme resolved to one mode.
type Palette struct {
	ID     string
	Name   string
	Family string
	Dark   bool
	Tokens Tokens
}

// TokenNames lists the color tokens in the order the desktop writes them.
var TokenNames = []string{
	"bg", "bg-softer", "bg-1", "bg-2", "bg-3", "bg-4",
	"fg", "fg-1", "fg-2",
	"grey-2", "grey-1", "grey-0", "grey-dim",
	"accent", "accent-soft", "accent-muted",
	"red", "green", "yellow", "blue", "purple", "aqua",
}

func (t *Tokens) field(name string) *RGB {
	switch name {
	case "bg":
		return &t.Bg
	case "bg-softer":
		return &t.BgSofter
	case "bg-1":
		return &t.Bg1
	case "bg-2":
		return &t.Bg2
	case "bg-3":
		return &t.Bg3
	case "bg-4":
		return &t.Bg4
	case "fg":
		return &t.Fg
	case "fg-1":
		return &t.Fg1
	case "fg-2":
		return &t.Fg2
	case "grey-2":
		return &t.Grey2
	case "grey-1":
		return &t.Grey1
	case "grey-0":
		return &t.Grey0
	case "grey-dim":
		return &t.GreyDim
	case "accent":
		return &t.Accent
	case "accent-soft":
		return &t.AccentSoft
	case "accent-muted":
		return &t.AccentMuted
	case "red":
		return &t.Red
	case "green":
		return &t.Green
	case "yellow":
		return &t.Yellow
	case "blue":
		return &t.Blue
	case "purple":
		return &t.Purple
	case "aqua":
		return &t.Aqua
	}
	return nil
}

// builtinCSS holds one flattened block per built-in theme. It is generated
// from the desktop's stylesheet: see gen/main.go.
//
//go:embed builtin.css
var builtinCSS string

var builtinTokens = sync.OnceValue(func() map[string]Tokens {
	out := map[string]Tokens{}
	for _, r := range parseRules(builtinCSS) {
		for _, sel := range r.selectors {
			id, ok := builtinSelectorID(sel)
			if !ok {
				continue
			}
			opt, _ := Find(id)
			out[id] = completeTokens(parseColors(r.tokens), opt.Dark)
		}
	}
	return out
})

// parseColors keeps the declarations that parse as colors.
func parseColors(raw map[string]string) map[string]RGB {
	colors := map[string]RGB{}
	for name, value := range raw {
		if c, ok := ParseColor(value); ok {
			colors[name] = c
		}
	}
	return colors
}

// builtinSelectorID reads the id out of `:root[data-theme="id"]`.
func builtinSelectorID(sel string) (string, bool) {
	const head, tail = `:root[data-theme="`, `"]`
	if len(sel) <= len(head)+len(tail) || sel[:len(head)] != head || sel[len(sel)-len(tail):] != tail {
		return "", false
	}
	return sel[len(head) : len(sel)-len(tail)], true
}

// BuiltinPalette is the palette of a built-in theme.
func BuiltinPalette(id string) (Palette, error) {
	opt, ok := Find(id)
	if !ok {
		return Palette{}, fmt.Errorf("unknown theme %q", id)
	}
	tokens, ok := builtinTokens()[opt.ID]
	if !ok {
		return Palette{}, fmt.Errorf("theme %q has no colors", id)
	}
	return Palette{ID: opt.ID, Name: opt.Label, Family: opt.Family, Dark: opt.Dark, Tokens: tokens}, nil
}

// semanticDefaults are the syntax hues a theme gets when it names none,
// the desktop's SEMANTIC_DEFAULTS: light, dark.
var semanticDefaults = map[string][2]RGB{
	"red":    {{193, 74, 74}, {251, 73, 52}},
	"green":  {{108, 120, 46}, {184, 187, 38}},
	"yellow": {{180, 113, 9}, {250, 189, 47}},
	"blue":   {{69, 112, 122}, {131, 165, 152}},
	"purple": {{148, 94, 128}, {211, 134, 155}},
	"aqua":   {{76, 122, 93}, {142, 192, 124}},
}

// completeTokens fills the tokens a theme left out. A hand-written theme
// may set only a background, a text color and an accent; the rest follows
// the desktop's deriveThemeTokens so a sparse theme still reads as designed.
func completeTokens(c map[string]RGB, dark bool) Tokens {
	pick := func(fallback RGB, names ...string) RGB {
		for _, n := range names {
			if v, ok := c[n]; ok {
				return v
			}
		}
		return fallback
	}
	// With nothing to derive from, Gruvbox stands in, as it does on the
	// desktop where its tokens sit under every theme.
	paper, ink, orange := RGB{251, 241, 199}, RGB{60, 56, 54}, RGB{195, 94, 10}
	mode := 0
	if dark {
		paper, ink, orange = RGB{29, 32, 33}, RGB{221, 199, 161}, RGB{231, 138, 78}
		mode = 1
	}
	bg := pick(paper, "bg")
	text := pick(ink, "fg-1", "fg")
	accent := pick(orange, "accent")
	surface := pick(Mix(bg, text, 0.05), "bg-1")
	border := pick(Mix(bg, text, 0.16), "bg-3")
	muted := pick(Mix(text, bg, 0.3), "fg-2", "grey-2")
	faint := pick(Mix(text, bg, 0.52), "grey-0")
	t := Tokens{
		Bg:          bg,
		BgSofter:    pick(Mix(bg, surface, 0.5), "bg-softer"),
		Bg1:         surface,
		Bg2:         pick(Mix(surface, border, 0.5), "bg-2"),
		Bg3:         border,
		Bg4:         pick(Mix(border, faint, 0.45), "bg-4"),
		Fg:          pick(Mix(text, muted, 0.12), "fg"),
		Fg1:         text,
		Fg2:         pick(muted, "fg-2"),
		Grey2:       pick(muted, "grey-2"),
		Grey1:       pick(Mix(muted, faint, 0.5), "grey-1"),
		Grey0:       faint,
		GreyDim:     pick(Mix(faint, border, 0.5), "grey-dim"),
		Accent:      accent,
		AccentSoft:  pick(Mix(accent, bg, 0.3), "accent-soft"),
		AccentMuted: pick(Mix(accent, muted, 0.4), "accent-muted"),
	}
	for name, pair := range semanticDefaults {
		*t.field(name) = pick(pair[mode], name)
	}
	return t
}
