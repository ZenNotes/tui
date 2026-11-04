package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// Keeping what a note said before an assistant changed it, so the user can
// review the change in the desktop app (ZenNotes/zennotes
// docs/ideas/agent-edit-review.md). Every tool that rewrites a note's text
// runs through runRecording: it writes the record BEFORE the edit, lets the
// tool work, then notes how the note looks now. Mirrors
// apps/desktop/src/mcp/agent-edit-recorder.ts in ZenNotes/zennotes: same
// tools, same order, same record.

// recordedTools are the tools that change a note's text, and where each one
// names the note. Creating, moving, renaming and deleting notes are not
// recorded: a new note has nothing to compare against, and Trash keeps the rest.
var recordedTools = map[string]func(a args) string{
	"write_note":      pathArg,
	"append_to_note":  pathArg,
	"prepend_to_note": pathArg,
	"insert_at_line":  pathArg,
	"replace_in_note": pathArg,
	"edit_section":    pathArg,
	"toggle_task":     taskNoteArg,
	"update_task":     taskNoteArg,
}

func taskNoteArg(a args) string {
	id, _ := a["id"].(string)
	hash := strings.LastIndex(id, "#")
	if hash <= 0 {
		return ""
	}
	return id[:hash]
}

func pathArg(a args) string {
	rel, _ := a["path"].(string)
	return strings.TrimSpace(rel)
}

// clientDisplayNames mirrors CLIENT_DISPLAY_NAMES in the desktop's MCP server,
// so a record names the assistant the way its comments do.
var clientDisplayNames = map[string]string{
	"claude-ai":      "Claude",
	"claude-code":    "Claude Code",
	"claude-desktop": "Claude",
	"codex-cli":      "Codex",
	"codex":          "Codex",
}

// agentForClient is the display name for an MCP client's initialize name.
func agentForClient(name string) string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return "Assistant"
	}
	if known, ok := clientDisplayNames[strings.ToLower(raw)]; ok {
		return known
	}
	words := strings.FieldsFunc(raw, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// reviewAgentEditsEnabled reads Settings → MCP → "Review agent edits" from the
// portable config the app writes (`[mcp] review_agent_edits`). On when the
// file or the key is missing, matching the app's default.
func reviewAgentEditsEnabled() bool {
	raw, err := os.ReadFile(config.ConfigTomlPath())
	if err != nil {
		return true
	}
	var doc struct {
		MCP struct {
			ReviewAgentEdits *bool `toml:"review_agent_edits"`
		} `toml:"mcp"`
	}
	if _, err := toml.Decode(string(raw), &doc); err != nil || doc.MCP.ReviewAgentEdits == nil {
		return true
	}
	return *doc.MCP.ReviewAgentEdits
}

// Tool calls are served concurrently, and two edits to one note racing on its
// record would lose one of them, so edits to the same note take turns.
var noteLocks sync.Map

func lockNote(key string) func() {
	value, _ := noteLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func noteText(ctx context.Context, b Backend, rel string) (string, bool) {
	content, err := b.ReadNote(ctx, rel)
	if err != nil {
		return "", false
	}
	return content.Body, true
}

// runRecording runs a tool, recording its edit when it rewrites a note of a
// local vault. Recording never stands in the way of the edit the user asked
// for: if the record cannot be written, the tool still runs and the change is
// simply not offered for review.
func runRecording(ctx context.Context, tool string, a args, b Backend, agent string, run func() (any, error)) (any, error) {
	noteOf, ok := recordedTools[tool]
	// A remote vault's notes are written by its server; recording there needs
	// the server's own change.
	if !ok || b.Kind() != backend.KindLocal {
		return run()
	}
	rel := noteOf(a)
	if rel == "" {
		return run()
	}
	unlock := lockNote(b.Root() + "\x00" + rel)
	defer unlock()

	if !reviewAgentEditsEnabled() {
		return run()
	}
	before, ok := noteText(ctx, b, rel)
	// Not a note we can read (the tool reports its own error), or one too
	// large to keep copies of.
	if !ok || !vault.AgentEditRecordable(before) {
		return run()
	}
	existing := vault.ReadAgentEdit(b.Root(), rel)
	started := vault.BeginAgentEdit(existing, before)
	if err := vault.WriteAgentEdit(b.Root(), rel, started); err != nil {
		fmt.Fprintf(os.Stderr, "zn mcp: could not record the edit to %s: %v\n", rel, err)
		return run()
	}

	result, runErr := run()

	after, ok := noteText(ctx, b, rel)
	var finish error
	if !ok || after == before {
		// Nothing changed (no match, a task that moved, a failed write): the
		// record goes back to what it was.
		finish = vault.WriteAgentEdit(b.Root(), rel, existing)
	} else {
		entry := vault.AgentEditEntry{At: time.Now().UnixMilli(), Agent: agent, Tool: tool}
		finish = vault.WriteAgentEdit(b.Root(), rel, vault.FinishAgentEdit(started, after, entry))
	}
	if finish != nil {
		fmt.Fprintf(os.Stderr, "zn mcp: could not finish the record for %s: %v\n", rel, finish)
	}
	return result, runErr
}
