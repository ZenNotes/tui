package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

// Agent edit records written through the real MCP server, as Claude Code
// would drive it. Mirrors apps/desktop/src/mcp/agent-edit-recorder.test.ts
// in ZenNotes/zennotes.

const planNote = "inbox/Plan.md"
const planBody = "# Plan\n\n- [ ] Ship the beta\nThe migration runs at night.\n"

// serveAs is runServer with a client name, so the record can name it.
func serveAs(t *testing.T, client string, root string) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverT, clientT := sdk.NewInMemoryTransports()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, Options{
			ResolveTarget: func() (backend.Target, error) { return backend.Target{Kind: backend.KindLocal, Root: root}, nil },
			OpenBackend:   func(t backend.Target) (Backend, error) { return backend.New(t, backend.Options{}) },
			Transport:     serverT,
		})
	}()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: client, Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		<-done
	})
	return cs
}

func seedPlan(t *testing.T) string {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inbox", "Plan.md"), []byte(planBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readPlan(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "inbox", "Plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMCPRecordsAnAssistantsEditsForReview(t *testing.T) {
	root := seedPlan(t)
	cs := serveAs(t, "claude-code", root)
	if _, isErr := callTool(t, cs, "replace_in_note", map[string]any{"path": planNote, "find": "at night", "replace": "on Sunday"}); isErr {
		t.Fatal("replace_in_note failed")
	}
	if _, isErr := callTool(t, cs, "append_to_note", map[string]any{"path": planNote, "text": "A new line."}); isErr {
		t.Fatal("append_to_note failed")
	}
	rec := vault.ReadAgentEdit(root, planNote)
	if rec == nil {
		t.Fatal("no record")
	}
	if rec.Before != planBody || rec.After == nil || *rec.After != readPlan(t, root) {
		t.Fatalf("record texts = %q / %v", rec.Before, rec.After)
	}
	if len(rec.Edits) != 2 || rec.Edits[0].Agent != "Claude Code" || rec.Edits[0].Tool != "replace_in_note" || rec.Edits[1].Tool != "append_to_note" {
		t.Fatalf("edits = %+v", rec.Edits)
	}
}

func TestMCPRecordsATaskToggledByID(t *testing.T) {
	root := seedPlan(t)
	cs := serveAs(t, "codex-cli", root)
	if _, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": planNote + "#0"}); isErr {
		t.Fatal("toggle_task failed")
	}
	rec := vault.ReadAgentEdit(root, planNote)
	if rec == nil || rec.Before != planBody || len(rec.Edits) != 1 || rec.Edits[0].Agent != "Codex" || rec.Edits[0].Tool != "toggle_task" {
		t.Fatalf("record = %+v", rec)
	}
}

func TestMCPLeavesNoRecordWhenNothingChanged(t *testing.T) {
	root := seedPlan(t)
	cs := serveAs(t, "claude-code", root)
	callTool(t, cs, "replace_in_note", map[string]any{"path": planNote, "find": "nowhere in the note", "replace": "x"})
	callTool(t, cs, "read_note", map[string]any{"path": planNote})
	if vault.ReadAgentEdit(root, planNote) != nil {
		t.Fatal("no edit, no record")
	}
	if _, err := os.Stat(filepath.Join(root, ".zennotes", "agent-edits")); !os.IsNotExist(err) {
		t.Fatalf("no agent-edits folder should be left: %v", err)
	}
}

func TestMCPRecordsNothingWhenReviewIsOff(t *testing.T) {
	root := seedPlan(t)
	if err := os.WriteFile(filepath.Join(os.Getenv("ZENNOTES_CONFIG_DIR"), "config.toml"), []byte("[mcp]\nreview_agent_edits = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := serveAs(t, "claude-code", root)
	callTool(t, cs, "append_to_note", map[string]any{"path": planNote, "text": "Unrecorded."})
	if vault.ReadAgentEdit(root, planNote) != nil {
		t.Fatal("review is off: nothing may be recorded")
	}
}

func TestMCPCannotWriteIntoZennotes(t *testing.T) {
	root := seedPlan(t)
	cs := serveAs(t, "claude-code", root)
	forged := ".zennotes/agent-edits/inbox/Plan.md.json"
	if _, isErr := callTool(t, cs, "write_note", map[string]any{"path": forged, "body": "{}"}); !isErr {
		t.Fatal("write_note into .zennotes must fail")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(forged))); !os.IsNotExist(err) {
		t.Fatalf("nothing may land in .zennotes: %v", err)
	}
}

func TestAgentForClientMatchesTheDesktop(t *testing.T) {
	for in, want := range map[string]string{
		"claude-code": "Claude Code", "Claude-Code": "Claude Code", "codex": "Codex",
		"": "Assistant", "my_agent-tool": "My Agent Tool",
	} {
		if got := agentForClient(in); got != want {
			t.Errorf("agentForClient(%q) = %q, want %q", in, got, want)
		}
	}
}

const rollWorkNote = "inbox/Work.md"
const rollWorkBody = "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n"
const rollSession = "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"

// A roll that copies a work session into a category note records that note
// as it records the note of the task: the text before, one edit, the text after.
func TestMCPRecordsACategoryNoteThatARollWrites(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	before := map[string]string{rollWorkNote: rollWorkBody, "quick/Plans.md": rollSession}
	writeNotes(t, root, before)
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	if text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": rollWorkNote + "#^ab12cd"}); isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	for rel, want := range before {
		rec := vault.ReadAgentEdit(root, rel)
		if rec == nil {
			t.Fatalf("no record for %s", rel)
		}
		if rec.Before != want {
			t.Fatalf("%s: before = %q", rel, rec.Before)
		}
		if rec.After == nil || *rec.After != readNote(t, root, rel) {
			t.Fatalf("%s: after = %v", rel, rec.After)
		}
		if len(rec.Edits) != 1 || rec.Edits[0].Tool != "toggle_task" {
			t.Fatalf("%s: edits = %+v", rel, rec.Edits)
		}
	}
}

// A scoped client's roll writes only the notes in scope, so only those notes
// get records.
func TestMCPRecordsOnlyTheCategoryNotesInScope(t *testing.T) {
	root := t.TempDir()
	writeNotes(t, root, map[string]string{
		".zennotes/vault.json":     `{"primaryNotesLocation":"root"}`,
		"Projects/Work.md":         "- [ ] Water plants @repeat:mon/thu ^ab12cd\n",
		"Projects/Plans.md":        rollSession,
		"Projects/Clients/Plan.md": rollSession,
	})
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	scope := testScope(t, "write", []string{"Projects"}, []string{"Projects/Clients"})
	cs := runScopedWith(t, root, scope, backend.Options{Now: func() time.Time { return today }})
	if text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "Projects/Work.md#^ab12cd"}); isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	if rec := vault.ReadAgentEdit(root, "Projects/Plans.md"); rec == nil || rec.Before != rollSession || len(rec.Edits) != 1 {
		t.Fatalf("Plans record = %+v", rec)
	}
	if vault.ReadAgentEdit(root, "Projects/Clients/Plan.md") != nil {
		t.Fatal("a note out of scope must have no record")
	}
}

// update_task records a category note that its roll writes, as toggle_task does.
func TestMCPRecordsACategoryNoteThatUpdateTaskWrites(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	before := map[string]string{rollWorkNote: rollWorkBody, "quick/Plans.md": rollSession}
	writeNotes(t, root, before)
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	if text, isErr := callTool(t, cs, "update_task", map[string]any{"id": rollWorkNote + "#0", "state": "done"}); isErr {
		t.Fatalf("update_task: %s", text)
	}
	for rel, want := range before {
		rec := vault.ReadAgentEdit(root, rel)
		if rec == nil {
			t.Fatalf("no record for %s", rel)
		}
		if rec.Before != want {
			t.Fatalf("%s: before = %q", rel, rec.Before)
		}
		if rec.After == nil || *rec.After != readNote(t, root, rel) {
			t.Fatalf("%s: after = %v", rel, rec.After)
		}
		if len(rec.Edits) != 1 || rec.Edits[0].Tool != "update_task" {
			t.Fatalf("%s: edits = %+v", rel, rec.Edits)
		}
	}
}
