package mcp

import (
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/remote/remotetest"
)

// The MCP tools that read a note, change it and write it back keep a save
// another device made in between, under full access and under a path scope
// (whose backend must pass the version through).
func TestMCPEditsKeepASaveMadeBetweenTheirReadAndWrite(t *testing.T) {
	const rel = "inbox/Plan.md"
	start := "# Plan\n\n## Next\n\n- [ ] ship\n"
	theirs := start + "\nTheir line.\n"
	tools := map[string]map[string]any{
		"append_to_note":  {"path": rel, "text": "Mine."},
		"edit_section":    {"path": rel, "heading": "Next", "mode": "append", "text": "Mine."},
		"replace_in_note": {"path": rel, "find": "ship", "replace": "ship it"},
		"toggle_task":     {"id": rel + "#0"},
		"update_task":     {"id": rel + "#0", "priority": "high"},
	}
	scopes := map[string]*Scope{
		"full access":  nil,
		"a path scope": testScope(t, "write", []string{"inbox"}, nil),
	}
	for scopeName, scope := range scopes {
		for tool, args := range tools {
			t.Run(scopeName+"/"+tool, func(t *testing.T) {
				server := remotetest.New(t, true, map[string]string{rel: start})
				raced := false
				server.BeforeWrite = func(string) {
					if !raced {
						raced = true
						server.Set(rel, theirs)
					}
				}
				cs := serveScoped(t, backend.Target{Kind: backend.KindRemote, BaseURL: server.URL}, scope)
				if text, failed := contractCall(cs, tool, args); failed {
					t.Fatalf("%s failed: %s", tool, text)
				}
				got, _ := server.Body(rel)
				if !strings.Contains(got, "Their line.") {
					t.Fatalf("their save was written over: %q", got)
				}
				if got == theirs {
					t.Fatalf("the edit was lost: %q", got)
				}
				writes := server.Writes()
				if len(writes) != 2 || !writes[0].Refused || writes[1].Base != remotetest.Sha256(theirs) {
					t.Fatalf("writes: %+v", writes)
				}
			})
		}
	}
}
