package config

// Settings for the helix engine. They live here, away from the shared
// preference code, so the helix branch touches that code only through
// one-line hooks.

// MotionsChoices are the editing engines [vim] motions picks from.
var MotionsChoices = []string{"vim", "helix"}

// applyHelixPrefs reads [vim] motions and the [keys] tables that rebind
// the helix engine's keys, in Helix's own config format. Builds of the helix branch before
// motions existed wrote a [helix] section instead of [vim]; it still applies
// when [vim] is absent.
func applyHelixPrefs(p *Prefs, doc map[string]any) {
	modal, _ := doc["vim"].(map[string]any)
	if modal == nil {
		if legacy, ok := doc["helix"].(map[string]any); ok {
			applyPrefs(p, map[string]any{"vim": legacy})
			modal = legacy
		}
	}
	setString(modal, "motions", &p.Motions)
	p.HelixKeys, _ = doc["keys"].(map[string]any)
}

// addHelixValues adds the helix settings to the editable value tables.
func addHelixValues(t map[string]map[string]any, p Prefs) {
	t["vim"]["motions"] = p.Motions
}

// addHelixEnums adds the helix settings' allowed values.
func addHelixEnums(enums map[string]map[string][]string) {
	enums["vim"]["motions"] = MotionsChoices
}
