package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The desktop renders ```tikz fences and the zn preview renders them under
// either engine, so the served text stays the desktop's whatever the math
// engine: an agent writes notes that draw on every surface.
func TestInstructionsAreTheDesktopsUnderEitherEngine(t *testing.T) {
	for _, engine := range []string{"typst", "latex"} {
		dir := t.TempDir()
		t.Setenv("ZENNOTES_CONFIG_DIR", dir)
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[terminal]\nmath = \""+engine+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ResolveInstructions(); got != DefaultInstructions {
			t.Fatalf("math = %q serves instructions that differ from DefaultInstructions", engine)
		}
	}
}

func TestInstructionsDocumentTaskScheduling(t *testing.T) {
	// Line breaks in the served text are not part of a phrase.
	flat := strings.Join(strings.Fields(DefaultInstructions), " ")
	for _, want := range []string{
		"due:YYYY-MM-DD[THH:MM[-HH:MM]]",
		"@repeat:daily",
		"`mon/thu`",
		"`after-3d`",
		"`monthly-last-fri`",
		"another app treats an extension as an ordinary task",
		"page-type: task",
		"count a task's position",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("instructions do not mention %q", want)
		}
	}
}

func TestInstructionsDocumentFieldsEventsAndSessions(t *testing.T) {
	flat := strings.Join(strings.Fields(DefaultInstructions), " ")
	for _, want := range []string{
		"`@until:YYYY-MM-DD`",
		"`@skip:2027-03-13/2027-03-21`",
		"`@skip:Spring-Break`",
		"the keys `repeat`, `until` and `skip`",
		"`when:2027-03-13/2027-03-21`",
		"`page-type: event`",
		"`page-type: event-list`",
		"Leave events as they are unless the user asks",
		"`task:[[Note#^ab12cd]]`",
		"block id",
		"`path#^blockid`",
		"cancelling it with update_task",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("instructions do not mention %q", want)
		}
	}
}

func TestInstructionsNeverMentionScheduledOrBareTokens(t *testing.T) {
	flat := strings.Join(strings.Fields(DefaultInstructions), " ")
	if strings.Contains(strings.ToLower(flat), "scheduled") {
		t.Error("instructions mention scheduled")
	}
	for _, token := range []string{"repeat:", "skip:", "until:"} {
		for i := 0; i+len(token) <= len(flat); i++ {
			if flat[i:i+len(token)] != token {
				continue
			}
			if i > 0 && (flat[i-1] == '@' || flat[i-1] >= 'a' && flat[i-1] <= 'z') {
				continue
			}
			t.Errorf("instructions have a bare %q at %q", token, flat[max(0, i-20):min(len(flat), i+30)])
		}
	}
}
