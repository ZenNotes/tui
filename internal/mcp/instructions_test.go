package mcp

import (
	"os"
	"path/filepath"
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
