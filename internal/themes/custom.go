package themes

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CustomPrefix turns a custom theme's folder name into its theme id.
const CustomPrefix = "custom-"

// Custom is a theme a user authored: a folder under <config>/themes holding
// a theme.css and, optionally, a manifest.json. The desktop app owns the
// format; the terminal reads the same folder.
type Custom struct {
	Slug string
	Name string
	// Modes is light, dark or both. A single-mode theme pins its mode.
	Modes string
	rules []rule
}

// ID is the theme id the desktop saves for this theme.
func (c Custom) ID() string { return CustomPrefix + c.Slug }

// CustomSlug extracts the folder name from a `custom-<slug>` id.
func CustomSlug(id string) (string, bool) {
	slug, ok := strings.CutPrefix(strings.TrimSpace(id), CustomPrefix)
	return slug, ok && slug != ""
}

func safeSlug(slug string) bool {
	return slug != "" && slug != "." && !strings.ContainsAny(slug, `/\`) && !strings.Contains(slug, "..")
}

// LoadCustom reads one custom theme. The only hard requirement is a
// readable theme.css; a missing or broken manifest names the theme after
// its folder and assumes both modes.
func LoadCustom(dir, slug string) (Custom, error) {
	if !safeSlug(slug) {
		return Custom{}, fmt.Errorf("custom theme %q is not a folder name", slug)
	}
	css, err := os.ReadFile(filepath.Join(dir, slug, "theme.css"))
	if err != nil {
		return Custom{}, fmt.Errorf("custom theme %q has no readable theme.css in %s", slug, filepath.Join(dir, slug))
	}
	c := Custom{Slug: slug, Name: slug, Modes: "both", rules: parseRules(string(css))}
	if raw, err := os.ReadFile(filepath.Join(dir, slug, "manifest.json")); err == nil {
		var manifest struct {
			Name  string `json:"name"`
			Modes any    `json:"modes"`
		}
		if json.Unmarshal(raw, &manifest) == nil {
			if name := strings.TrimSpace(manifest.Name); name != "" {
				c.Name = name
			}
			c.Modes = normalizeModes(manifest.Modes)
		}
	}
	return c, nil
}

// normalizeModes accepts "light", "dark", "both" or a list of modes.
func normalizeModes(raw any) string {
	switch v := raw.(type) {
	case string:
		if v == "light" || v == "dark" {
			return v
		}
	case []any:
		light, dark := false, false
		for _, e := range v {
			light = light || e == "light"
			dark = dark || e == "dark"
		}
		if light != dark {
			if light {
				return "light"
			}
			return "dark"
		}
	}
	return "both"
}

// ListCustom returns every loadable theme folder, by name.
func ListCustom(dir string) []Custom {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := []Custom{}
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if c, err := LoadCustom(dir, e.Name()); err == nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Palette resolves the theme for the wanted mode. Unscoped rules apply to
// both modes; [data-theme-mode] rules are more specific, so they win over
// them whatever the order in the file.
func (c Custom) Palette(wantDark bool) Palette {
	dark := wantDark
	switch c.Modes {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	mode := "light"
	if dark {
		mode = "dark"
	}
	raw := map[string]string{}
	for _, scoped := range []string{"", mode} {
		for _, r := range c.rules {
			for _, sel := range r.selectors {
				if m, ok := selectorMode(sel); ok && m == scoped {
					maps.Copy(raw, r.tokens)
					break
				}
			}
		}
	}
	return Palette{ID: c.ID(), Name: c.Name, Family: FamilyCustom, Dark: dark, Tokens: completeTokens(parseColors(raw), dark)}
}
