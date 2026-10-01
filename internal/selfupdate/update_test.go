package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestInstallationOwnerDetection(t *testing.T) {
	for _, tc := range []struct{ path, version, owner string }{
		{"/opt/homebrew/Cellar/zn/0.5.0/bin/zn", "(devel)", "homebrew"},
		{"/Users/alice/Library/Application Support/ZenNotes/cli/terminal/versions/0.5.0-x/zn", "(devel)", "desktop"},
		{"/Users/alice/go/bin/zn", "v0.5.0", "go"},
		{"/usr/local/bin/zn", "(devel)", "standalone"},
		{"/nix/store/hash-zn/bin/zn", "(devel)", "package-manager"},
	} {
		info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/ZenNotes/tui", Version: tc.version}}
		if got := Detect(tc.path, info); got.Owner != tc.owner {
			t.Errorf("%s: got %s, want %s", tc.path, got.Owner, tc.owner)
		}
	}
	// Newer Go toolchains embed a module version in checkout builds too. A
	// version alone does not mean the installation belongs to `go install`.
	checkout := &debug.BuildInfo{Main: debug.Module{Path: "github.com/ZenNotes/tui", Version: "v0.5.1-0.20260929205816-0cfb42256841+dirty"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0cfb42256841"}}}
	if got := Detect("/tmp/zn", checkout); got.Owner != "standalone" {
		t.Fatalf("source-built executable misclassified: %+v", got)
	}
	// Desktop installs its own CLI updates, so `zn update` points to the one
	// control that triggers that check instead of replacing the file itself.
	desktop := Detect("/Applications/ZenNotes.app/Contents/Resources/zn-cli/zn", nil)
	if desktop.Owner != "desktop" || !strings.Contains(desktop.Instruction, "Settings > CLI > Check for updates") {
		t.Fatalf("desktop instruction: %+v", desktop)
	}
}

func TestFailedCandidatePreservesOriginalExecutable(t *testing.T) {
	dir := t.TempDir()
	exe, candidate := filepath.Join(dir, "zn"), filepath.Join(dir, "candidate")
	os.WriteFile(exe, []byte("working"), 0o755)
	os.WriteFile(candidate, []byte("broken"), 0o755)
	probe := func(context.Context, string, string) error { return errors.New("bad candidate") }
	if err := replace(context.Background(), exe, candidate, "0.6.0", probe); err == nil {
		t.Fatal("invalid candidate activated")
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "working" {
		t.Fatal("failed update changed original executable")
	}
	if _, err := os.Stat(exe + ".previous"); !os.IsNotExist(err) {
		t.Fatal("failed probe changed backup")
	}
	probe = func(context.Context, string, string) error { return nil }
	if err := replace(context.Background(), exe, candidate, "0.6.0", probe); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(exe)
	old, _ := os.ReadFile(exe + ".previous")
	if string(got) != "broken" || string(old) != "working" {
		t.Fatalf("replacement or retained backup wrong: %s / %s", got, old)
	}
}
