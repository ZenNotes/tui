package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func runConfig(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var code int
	out := captureOutput(t, func() { code = Main(append([]string{"config"}, args...)) })
	return out, code
}

func TestConfigListShowsViewModeRules(t *testing.T) {
	isolatedCLI(t)
	if _, code := runConfig(t, "set", "terminal.view_mode_rules.page-status=done", "preview"); code != 0 {
		t.Fatalf("set exit %d", code)
	}
	out, code := runConfig(t, "list")
	if code != 0 || !strings.Contains(out, "terminal.view_mode_rules.page-status=done = preview\n") {
		t.Fatalf("list (exit %d): %s", code, out)
	}
	out, _ = runConfig(t, "list", "--json")
	if !strings.Contains(out, `"view_mode_rules.page-status=done": "preview"`) {
		t.Fatalf("list --json: %s", out)
	}
}

func TestConfigGetViewModeRule(t *testing.T) {
	isolatedCLI(t)
	runConfig(t, "set", "terminal.view_mode_rules.kind=journal", "split")
	out, code := runConfig(t, "get", "terminal.view_mode_rules.kind=journal")
	if code != 0 || strings.TrimSpace(out) != "split" {
		t.Fatalf("get (exit %d): %q", code, out)
	}
	if _, code := runConfig(t, "get", "terminal.view_mode_rules.kind=other"); code == 0 {
		t.Fatal("get of an unset rule succeeded")
	}
}

func TestConfigSetViewModeRuleAddsAndChanges(t *testing.T) {
	isolatedCLI(t)
	path := config.ConfigTomlPath()
	if err := os.WriteFile(path, []byte("[terminal]\nmath = \"latex\"\n\n[editor]\nword_wrap = false\n\n[custom]\nkeep = \"me\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"preview", "Edit"} {
		if o, code := runConfig(t, "set", "terminal.view_mode_rules.page-status=done", mode); code != 0 {
			t.Fatalf("set %s: exit %d: %s", mode, code, o)
		}
	}
	body, _ := os.ReadFile(path)
	for _, want := range []string{`math = "latex"`, "[editor]", "word_wrap = false", "[custom]", `keep = "me"`, `"page-status=done" = "edit"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), "preview") {
		t.Fatalf("old mode left behind:\n%s", body)
	}
	prefs, _, err := config.LoadPrefs()
	if err != nil || len(prefs.ViewModeRules) != 1 {
		t.Fatalf("reload: %+v %v", prefs.ViewModeRules, err)
	}
}

func TestConfigSetViewModeRuleRejectsBadMode(t *testing.T) {
	isolatedCLI(t)
	path := config.ConfigTomlPath()
	original := "[terminal]\nmath = \"latex\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"set", "terminal.view_mode_rules.page-status=done", "reading"},
		{"set", "terminal.view_mode_rules.page-status", "preview"},
		{"set", "terminal.view_mode_rules.=done", "preview"},
		{"set", "terminal.view_mode_rules.status=", "preview"},
	} {
		if _, code := runConfig(t, args...); code == 0 {
			t.Fatalf("%v succeeded", args)
		}
	}
	body, _ := os.ReadFile(path)
	if string(body) != original {
		t.Fatalf("config changed:\n%s", body)
	}
}

func TestConfigSetViewModeField(t *testing.T) {
	isolatedCLI(t)
	if _, code := runConfig(t, "set", "terminal.view_mode_field", "view-mode"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	prefs, _, err := config.LoadPrefs()
	if err != nil || prefs.TerminalViewModeField != "view-mode" {
		t.Fatalf("field: %q %v", prefs.TerminalViewModeField, err)
	}
	if _, code := runConfig(t, "set", "terminal.view_mode_field", ""); code != 0 {
		t.Fatalf("clear exit %d", code)
	}
	prefs, _, err = config.LoadPrefs()
	if err != nil || prefs.TerminalViewModeField != "" {
		t.Fatalf("cleared field: %q %v", prefs.TerminalViewModeField, err)
	}
}
