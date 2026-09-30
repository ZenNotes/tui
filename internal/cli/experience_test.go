package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func isolatedCLI(t *testing.T) string {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	t.Setenv("EDITOR", "false")
	t.Setenv("VISUAL", "false")
	for _, key := range []string{"ZENNOTES_VAULT", "ZENNOTES_SERVER", "ZENNOTES_REMOTE_TOKEN", "ZENNOTES_WORKSPACE_SOURCE"} {
		t.Setenv(key, "")
	}
	return t.TempDir()
}

func TestCommandHelpNeverRunsTheCommand(t *testing.T) {
	root := isolatedCLI(t)
	for _, args := range [][]string{
		{"create", "--help", "--vault", root},
		{"help", "create"},
		{"folder", "create", "--help"},
		{"--vault", root, "write", "--help"},
		{"server", "setup", "home", "--help", "--vault", root},
	} {
		out := captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("%v: exit %d", args, code)
			}
		})
		if !strings.Contains(out, "USAGE") {
			t.Errorf("%v: missing help: %s", args, out)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help modified vault: %v %v", entries, err)
	}
}

func TestServerVersionFlagSelectsRelease(t *testing.T) {
	name, args, err := parseCommand([]string{"server", "update", "home", "--version", "2.56.0", "--check"})
	if err != nil || name != "server update" || args.Str("version") != "2.56.0" || !args.Bool("check") || args.Positional(0) != "home" {
		t.Fatalf("server update parse: %s %+v %v", name, args, err)
	}
	isolatedCLI(t)
	out := captureOutput(t, func() {
		if code := Main([]string{"server", "list", "--json"}); code != 0 {
			t.Errorf("exit %d", code)
		}
	})
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty server list: %q", out)
	}
}

func TestGlobalVersionDoesNotCreateANote(t *testing.T) {
	root := isolatedCLI(t)
	captureOutput(t, func() {
		if code := Main([]string{"create", "--version", "--vault", root}); code != 0 {
			t.Errorf("version exit %d", code)
		}
	})
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("global --version created a note")
	}
}

func TestInvalidFlagsFailBeforeCreatingANote(t *testing.T) {
	root := isolatedCLI(t)
	for _, args := range [][]string{
		{"create", "--titel", "Oops", "--vault", root},
		{"create", "--title", "--vault", root},
		{"list", "--limit", "1.5", "--vault", root},
		{"create", "one", "two", "--vault", root},
		{"server", "stop"},
		{"config", "get"},
	} {
		captureOutput(t, func() {
			if code := Main(args); code != 2 {
				t.Errorf("%v: exit %d, want usage error 2", args, code)
			}
		})
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("invalid invocation modified vault")
	}
}

func TestGlobalSwitchAndRepeatedFlags(t *testing.T) {
	root := isolatedCLI(t)
	out := captureOutput(t, func() {
		if code := Main([]string{"--no-color", "--vault", root, "create", "--title", "Hello", "--tag", "one", "--tag", "two", "--json"}); code != 0 {
			t.Errorf("exit %d", code)
		}
	})
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid JSON: %s", out)
	}
	body, err := os.ReadFile(filepath.Join(root, "inbox", "Hello.md"))
	if err != nil || !strings.Contains(string(body), "#one #two") {
		t.Fatalf("note: %s %v", body, err)
	}
}

func TestJSONErrorsAreMachineReadable(t *testing.T) {
	isolatedCLI(t)
	for _, args := range [][]string{{"list", "--json"}, {"create", "--titel", "x", "--json"}, {"unknown", "--json"}} {
		out := captureOutput(t, func() { Main(args) })
		var got struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.Error.Code == "" || got.Error.Message == "" {
			t.Fatalf("unstructured error: %s", out)
		}
	}
}

func TestConfigCommandsPersistTypedValues(t *testing.T) {
	isolatedCLI(t)
	for _, args := range [][]string{{"config", "set", "editor.word_wrap", "false"}, {"config", "set", "editor.tab_size", "2"}} {
		captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("%v: exit %d", args, code)
			}
		})
	}
	prefs, _, err := config.LoadPrefs()
	if err != nil || prefs.WordWrap || prefs.EditorTabSize != 2 {
		t.Fatalf("settings did not persist: %+v %v", prefs, err)
	}
	out := captureOutput(t, func() { Main([]string{"config", "get", "editor.word_wrap", "--json"}) })
	if strings.TrimSpace(out) != "false" {
		t.Fatalf("get: %s", out)
	}
	captureOutput(t, func() {
		if code := Main([]string{"config", "set", "editor.word_wrap", "maybe"}); code == 0 {
			t.Fatal("accepted invalid boolean")
		}
	})
}

func TestStatusExplainsSelectionWithoutLeakingToken(t *testing.T) {
	isolatedCLI(t)
	t.Setenv("ZENNOTES_REMOTE_TOKEN", "must-never-be-printed")
	out := captureOutput(t, func() {
		if code := Main([]string{"status", "--server", "https://notes.example.com", "--json"}); code != 0 {
			t.Errorf("status exit %d", code)
		}
	})
	if !json.Valid([]byte(out)) || strings.Contains(out, "must-never-be-printed") || !strings.Contains(out, "--server") {
		t.Fatalf("status: %s", out)
	}
}

func TestCompletionUsesExplicitVaultAndQuotesSpaces(t *testing.T) {
	root := isolatedCLI(t)
	if err := os.WriteFile(filepath.Join(root, "Alpha note.md"), []byte("# Alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, func() {
		if code := Main([]string{"__complete", "--", "--vault", root, "read", "Al"}); code != 0 {
			t.Errorf("completion exit %d", code)
		}
	})
	if strings.TrimSpace(out) != "Alpha note.md" {
		t.Fatalf("completion: %q", out)
	}
}

func TestGeneratedShellCompletionsParse(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell is not installed")
			}
			script := captureOutput(t, func() {
				if code := Main([]string{"completion", shell}); code != 0 {
					t.Errorf("completion exit %d", code)
				}
			})
			cmd := exec.Command(binary, "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated %s script: %v %s", shell, err, out)
			}
		})
	}
}
