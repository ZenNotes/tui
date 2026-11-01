package cli

import (
	"maps"
	"os"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

// `zn config set` on a rule that differs from an existing one only in case
// changes the existing rule instead of adding a duplicate.
func TestConfigSetViewModeRuleCaseVariantChangesExisting(t *testing.T) {
	isolatedCLI(t)
	path := config.ConfigTomlPath()
	if err := os.WriteFile(path, []byte("[terminal.view_mode_rules]\n\"page-status=done\" = \"preview\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o, code := runConfig(t, "set", "terminal.view_mode_rules.Page-Status=done", "split"); code != 0 {
		t.Fatalf("set: exit %d: %s", code, o)
	}
	prefs, _, err := config.LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"page-status=done": "split"}; !maps.Equal(prefs.ViewModeRules, want) {
		t.Fatalf("rules %v, want %v", prefs.ViewModeRules, want)
	}
}
