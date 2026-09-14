package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
)

func newFeatureTestApp(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n\nOriginal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := backend.Target{Kind: backend.KindLocal, Root: root}
	b, err := backend.New(target, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(context.Background(), Options{Backend: b, Target: target}, config.DefaultPrefs(), true)
	m := a.loadIndexCmd()().(indexLoadedMsg)
	if m.err != nil {
		t.Fatal(m.err)
	}
	a.idx = m.idx
	a.width, a.height, a.ready = 120, 40, true
	a.layout()
	return a, root
}
