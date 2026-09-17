package themes

import (
	"fmt"
	"strings"
)

// Selection is what config.toml says about the theme: the three
// [appearance] keys the desktop app writes.
type Selection struct {
	Family string // theme_family
	Mode   string // theme_mode: light | dark | auto
	ID     string // theme_id: a built-in id or custom-<slug>
}

// NormalizeMode maps a theme_mode onto light, dark or auto. The terminal
// has long called auto "system", so both names mean the same.
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "light":
		return "light"
	case "dark":
		return "dark"
	}
	return "auto"
}

// Resolve turns a selection into the palette to draw with. systemDark
// answers "auto"; customDir is where custom themes live. A selection that
// cannot be honored (a custom theme that is gone) still returns a usable
// palette, together with the reason.
func Resolve(sel Selection, systemDark bool, customDir string) (Palette, error) {
	wantDark := systemDark
	switch NormalizeMode(sel.Mode) {
	case "light":
		wantDark = false
	case "dark":
		wantDark = true
	}
	var problem error
	if slug, ok := CustomSlug(sel.ID); ok {
		custom, err := LoadCustom(customDir, slug)
		if err == nil {
			return custom.Palette(wantDark), nil
		}
		problem = err
	}
	family := strings.ToLower(strings.TrimSpace(sel.Family))
	current, known := Find(sel.ID)
	if !IsFamily(family) {
		family = "gruvbox"
		if known {
			family = current.Family
		}
	}
	id := current.ID
	// A saved id that disagrees with the family or the mode is stale: the
	// family's variant for the mode wins, keeping the id's flavor if it can.
	if !known || current.Family != family || current.Dark != wantDark {
		id = ResolveAuto(family, wantDark, sel.ID)
	}
	p, err := BuiltinPalette(id)
	if err != nil {
		return p, err
	}
	return p, problem
}

// Select reads what a user typed after `:theme`, or wrote in
// [terminal] theme, against the current selection: a mode, a family, a
// built-in id, or a custom theme by id or folder name.
func Select(name string, current Selection, customDir string) (Selection, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "light", "dark", "auto", "system":
		current.Mode = NormalizeMode(key)
		return current, nil
	}
	if IsFamily(key) {
		current.Family = key
		if _, custom := CustomSlug(current.ID); custom {
			current.ID = ""
		}
		return current, nil
	}
	// A variant names its own mode: `:theme catppuccin-mocha` means dark.
	if opt, ok := Find(key); ok {
		mode := "light"
		if opt.Dark {
			mode = "dark"
		}
		return Selection{Family: opt.Family, Mode: mode, ID: opt.ID}, nil
	}
	slug, ok := CustomSlug(strings.TrimSpace(name))
	if !ok {
		slug = strings.TrimSpace(name)
	}
	if custom, err := LoadCustom(customDir, slug); err == nil {
		current.Family, current.ID = FamilyCustom, custom.ID()
		return current, nil
	}
	return current, fmt.Errorf("unknown theme %q: a mode (dark, light, auto), a family (%s), a theme id or a custom theme", name, strings.Join(Families(), ", "))
}

// ApplyTweaks overlays the desktop's Quick tweaks ([tweaks] in config.toml):
// the accent and the six syntax hues, keyed by slug.
func (p *Palette) ApplyTweaks(tweaks map[string]string) {
	for _, slug := range []string{"accent", "red", "green", "yellow", "blue", "purple", "aqua"} {
		if c, ok := ParseColor(tweaks[slug]); ok {
			*p.Tokens.field(slug) = c
		}
	}
}
