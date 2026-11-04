package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

// assetOnlyBackend hides Local's link rewriter, the shape of a server's backend.
type assetOnlyBackend struct {
	Backend
}

func (a *assetOnlyBackend) ImportAsset(ctx context.Context, note, name string, r io.Reader) (vault.ImportedAsset, error) {
	return a.Backend.(backend.AssetManager).ImportAsset(ctx, note, name, r)
}

func mustLocalBackend(t *testing.T) Backend {
	t.Helper()
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"assets/pic.png":       "png",
	})
	b, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDailyNoteStartsFromTheVaultsTemplate(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json":       `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"Days","titlePattern":"yyyy-MM-dd","templateId":"custom:day"}}`,
		".zennotes/templates/day.md": "---\nname: Day\n---\n# {{title}}\n\n## Log\n",
		"Days/2026-03-04.md":         "# old\n",
		"Elsewhere/Plan.md":          "# Plan\n",
	})
	full := runScoped(t, root, nil)
	var result struct {
		Exists     bool
		Enabled    bool
		Path       string
		CreateWith map[string]any
	}
	if err := json.Unmarshal([]byte(mustCall(t, full, "daily_note", map[string]any{"date": "2026-03-05"})), &result); err != nil {
		t.Fatal(err)
	}
	if result.Exists || !result.Enabled || result.CreateWith["subpath"] != "Days" || result.CreateWith["title"] != "2026-03-05" {
		t.Fatalf("%+v", result)
	}
	if body, _ := result.CreateWith["body"].(string); !strings.Contains(body, "# 2026-03-05") || !strings.Contains(body, "## Log") {
		t.Fatalf("the template is not applied: %q", body)
	}
	found := mustCall(t, full, "daily_note", map[string]any{"date": "2026-03-04"})
	if !strings.Contains(found, `"exists": true`) || !strings.Contains(found, `"path": "Days/2026-03-04.md"`) {
		t.Fatalf("an existing note: %s", found)
	}

	// A client that sees part of the vault gets no template, and a hidden daily
	// note reads as absent, in the same shape.
	narrow := runScoped(t, root, testScope(t, "read", []string{"Elsewhere"}, nil))
	text := mustCall(t, narrow, "daily_note", map[string]any{"date": "2026-03-04"})
	if !strings.Contains(text, `"exists": false`) || strings.Contains(text, "## Log") {
		t.Fatalf("a path scope saw the template or a hidden note: %s", text)
	}

	// A read-only whole-vault client changes nothing and still gets the template.
	read := runScoped(t, root, testScope(t, "read", nil, nil))
	if text := mustCall(t, read, "daily_note", map[string]any{"date": "2026-03-05"}); !strings.Contains(text, "## Log") {
		t.Fatalf("read access: %s", text)
	}
	if _, err := os.Stat(filepath.Join(root, "Days", "2026-03-05.md")); err == nil {
		t.Fatal("daily_note created a note")
	}
}

func TestDailyNoteFindsANoteUnderAnOlderPattern(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"New","titlePattern":"yyyy-MM-dd","legacyPatterns":[{"directory":"Old","titlePattern":"yyyy.MM.dd"}]}}`,
		"Old/2026.03.04.md":    "# old pattern\n",
	})
	text := mustCall(t, runScoped(t, root, nil), "daily_note", map[string]any{"date": "2026-03-04"})
	if !strings.Contains(text, `"path": "Old/2026.03.04.md"`) {
		t.Fatalf("%s", text)
	}
}

func TestEditSectionWhenAHeadingIsAmbiguous(t *testing.T) {
	root := seedFiles(t, map[string]string{"inbox/N.md": "# N\n\n## A\n\n## A\n"})
	cs := runScoped(t, root, nil)
	if got := mustFail(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "A", "mode": "append", "text": "x"}); !strings.Contains(got, "more than once, on lines 2, 4") {
		t.Fatalf("%s", got)
	}
	mustCall(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "A", "lineNumber": float64(4), "mode": "append", "text": "x"})
	data, _ := os.ReadFile(filepath.Join(root, "inbox", "N.md"))
	if string(data) != "# N\n\n## A\n\n## A\n\nx\n" {
		t.Fatalf("%q", data)
	}
	if got := mustFail(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "N", "mode": "append", "text": " "}); !strings.Contains(got, "only replace can clear") {
		t.Fatalf("%s", got)
	}
	if got := mustFail(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "N", "mode": "rewrite", "text": "x"}); !strings.Contains(got, "mode must be") {
		t.Fatalf("%s", got)
	}
}
