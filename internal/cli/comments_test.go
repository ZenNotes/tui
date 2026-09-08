package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
)

func TestCommentCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	t.Setenv("ZENNOTES_VAULT", root)
	t.Setenv("ZENNOTES_SERVER", "")
	if err := os.MkdirAll(filepath.Join(root, ".zennotes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Plan.md"), []byte("# Plan\n\nShip the terminal build next week.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, func() {
		if code := Main([]string{"comment", "add", "Plan.md", "Too soon?", "--anchor", "next week", "--author", "Reviewer", "--json"}); code != 0 {
			t.Errorf("add failed")
		}
	})
	var thread backend.CommentThreadView
	if err := json.Unmarshal([]byte(out), &thread); err != nil || thread.Line != 3 || thread.AnchorText != "next week" || thread.Author == nil || *thread.Author != "Reviewer" {
		t.Fatalf("add: %v %s", err, out)
	}
	out = captureOutput(t, func() { _ = Main([]string{"comment", "reply", "Plan.md", thread.ID, "Fine by me"}) })
	if !strings.Contains(out, "Replied in "+thread.ID) || !strings.Contains(out, "1 reply") {
		t.Fatalf("reply: %s", out)
	}
	out = captureOutput(t, func() { _ = Main([]string{"comment", "list", "Plan.md"}) })
	if !strings.Contains(out, "Reviewer") || !strings.Contains(out, "line 3") || !strings.Contains(out, "> next week") || !strings.Contains(out, "Fine by me") {
		t.Fatalf("list: %s", out)
	}
	captureOutput(t, func() { _ = Main([]string{"comment", "resolve", "Plan.md", thread.ID}) })
	out = captureOutput(t, func() { _ = Main([]string{"comment", "list", "Plan.md"}) })
	if !strings.Contains(out, "No open comments") {
		t.Fatalf("resolved threads hide: %s", out)
	}
	out = captureOutput(t, func() { _ = Main([]string{"comment", "list", "Plan.md", "--all"}) })
	if !strings.Contains(out, "(resolved)") {
		t.Fatalf("--all shows them: %s", out)
	}
	out = captureOutput(t, func() {
		if code := Main([]string{"comment", "add", "Plan.md", "Where?", "--anchor", "nowhere"}); code == 0 {
			t.Error("a missing anchor must fail")
		}
	})
	if !strings.Contains(out, "anchor_text was not found") {
		t.Fatalf("anchor error: %s", out)
	}
}
