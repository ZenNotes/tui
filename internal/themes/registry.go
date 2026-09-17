package themes

import "strings"

// Option is one built-in theme variant, the desktop's ThemeOption.
type Option struct {
	ID      string
	Label   string
	Family  string
	Dark    bool
	Variant string
}

// FamilyCustom is the family of the themes users author themselves.
const FamilyCustom = "custom"

// DefaultID is the theme a fresh install starts on, as on the desktop.
const DefaultID = "dark-hard"

// Builtin mirrors THEMES in the desktop's lib/themes.ts, in its order.
var Builtin = []Option{
	{ID: "apple-light", Label: "Apple · Light", Family: "apple"},
	{ID: "apple-dark", Label: "Apple · Dark", Family: "apple", Dark: true},

	{ID: "light-hard", Label: "Gruvbox · Light Hard", Family: "gruvbox", Variant: "hard"},
	{ID: "light-medium", Label: "Gruvbox · Light Medium", Family: "gruvbox", Variant: "medium"},
	{ID: "light-soft", Label: "Gruvbox · Light Soft", Family: "gruvbox", Variant: "soft"},
	{ID: "dark-hard", Label: "Gruvbox · Dark Hard", Family: "gruvbox", Dark: true, Variant: "hard"},
	{ID: "dark-medium", Label: "Gruvbox · Dark Medium", Family: "gruvbox", Dark: true, Variant: "medium"},
	{ID: "dark-soft", Label: "Gruvbox · Dark Soft", Family: "gruvbox", Dark: true, Variant: "soft"},

	{ID: "catppuccin-latte", Label: "Catppuccin · Latte", Family: "catppuccin", Variant: "latte"},
	{ID: "catppuccin-frappe", Label: "Catppuccin · Frappé", Family: "catppuccin", Dark: true, Variant: "frappe"},
	{ID: "catppuccin-macchiato", Label: "Catppuccin · Macchiato", Family: "catppuccin", Dark: true, Variant: "macchiato"},
	{ID: "catppuccin-mocha", Label: "Catppuccin · Mocha", Family: "catppuccin", Dark: true, Variant: "mocha"},

	{ID: "github-light", Label: "GitHub · Light", Family: "github", Variant: "default"},
	{ID: "github-light-high-contrast", Label: "GitHub · Light High Contrast", Family: "github", Variant: "high-contrast"},
	{ID: "github-dark", Label: "GitHub · Dark", Family: "github", Dark: true, Variant: "default"},
	{ID: "github-dark-dimmed", Label: "GitHub · Dark Dimmed", Family: "github", Dark: true, Variant: "dimmed"},
	{ID: "github-dark-high-contrast", Label: "GitHub · Dark High Contrast", Family: "github", Dark: true, Variant: "high-contrast"},

	{ID: "solarized-light", Label: "Solarized · Light", Family: "solarized"},
	{ID: "solarized-dark", Label: "Solarized · Dark", Family: "solarized", Dark: true},

	{ID: "one-light", Label: "One · Light", Family: "one"},
	{ID: "one-dark", Label: "One · Dark", Family: "one", Dark: true},

	{ID: "nord-light", Label: "Nord · Light", Family: "nord"},
	{ID: "nord-dark", Label: "Nord · Dark", Family: "nord", Dark: true},

	{ID: "tokyo-night-day", Label: "Tokyo Night · Day", Family: "tokyo-night"},
	{ID: "tokyo-night-storm", Label: "Tokyo Night · Storm", Family: "tokyo-night", Dark: true},

	{ID: "kanagawa-wave", Label: "Kanagawa · Wave", Family: "kanagawa", Dark: true, Variant: "wave"},
	{ID: "kanagawa-dragon", Label: "Kanagawa · Dragon", Family: "kanagawa", Dark: true, Variant: "dragon"},
	{ID: "kanagawa-paper-ink", Label: "Kanagawa · Paper Ink", Family: "kanagawa", Dark: true, Variant: "paper-ink"},
	{ID: "kanagawa-lotus", Label: "Kanagawa · Lotus", Family: "kanagawa", Variant: "lotus"},

	{ID: "black-metal", Label: "Black Metal · Black", Family: "black-metal", Dark: true},
	{ID: "black-metal-day", Label: "Black Metal · Day", Family: "black-metal"},

	{ID: "rose-pine-main", Label: "Rosé Pine · Main", Family: "rose-pine", Dark: true, Variant: "main"},
	{ID: "rose-pine-moon", Label: "Rosé Pine · Moon", Family: "rose-pine", Dark: true, Variant: "moon"},
	{ID: "rose-pine-dawn", Label: "Rosé Pine · Dawn", Family: "rose-pine", Variant: "dawn"},
}

// familyDefaults is the variant `auto` lands on per family: light, dark.
var familyDefaults = map[string][2]string{
	"apple":       {"apple-light", "apple-dark"},
	"gruvbox":     {"light-medium", "dark-medium"},
	"catppuccin":  {"catppuccin-latte", "catppuccin-mocha"},
	"github":      {"github-light", "github-dark"},
	"solarized":   {"solarized-light", "solarized-dark"},
	"one":         {"one-light", "one-dark"},
	"nord":        {"nord-light", "nord-dark"},
	"tokyo-night": {"tokyo-night-day", "tokyo-night-storm"},
	"kanagawa":    {"kanagawa-lotus", "kanagawa-wave"},
	"black-metal": {"black-metal-day", "black-metal"},
	"rose-pine":   {"rose-pine-dawn", "rose-pine-main"},
}

// Find looks a built-in theme up by id.
func Find(id string) (Option, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, o := range Builtin {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

// Families lists the built-in families in registry order.
func Families() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, o := range Builtin {
		if !seen[o.Family] {
			seen[o.Family] = true
			out = append(out, o.Family)
		}
	}
	return out
}

// IsFamily reports whether name is a built-in family.
func IsFamily(name string) bool {
	_, ok := familyDefaults[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// ResolveAuto picks a family's light or dark variant, the desktop's
// resolveAuto: the current theme's variant carries across the mode flip
// (Gruvbox Hard stays Hard), else the family's canonical default wins.
func ResolveAuto(family string, dark bool, currentID string) string {
	if cur, ok := Find(currentID); ok && cur.Family == family && cur.Variant != "" {
		for _, o := range Builtin {
			if o.Family == family && o.Dark == dark && o.Variant == cur.Variant {
				return o.ID
			}
		}
	}
	pair, ok := familyDefaults[family]
	if !ok {
		pair = familyDefaults["github"]
	}
	if dark {
		return pair[1]
	}
	return pair[0]
}
