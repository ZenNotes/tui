package mcp

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Tool calls are served concurrently. A tool that reads a note and writes it back
// loses whatever landed in between, unless it waits its turn.
func TestParallelWritesLoseNothing(t *testing.T) {
	const rounds = 8
	const edits = 12
	note := "# N\n\n## A\n\n- [ ] one\n- [ ] two\n\n![[assets/pic0.png]]\n"
	for name, edit := range map[string]func(i int) (string, map[string]any, string){
		"append_to_note": func(i int) (string, map[string]any, string) {
			return "append_to_note", map[string]any{"path": "n.md", "text": fmt.Sprintf("marker-%d", i)}, fmt.Sprintf("marker-%d", i)
		},
		"edit_section": func(i int) (string, map[string]any, string) {
			return "edit_section", map[string]any{"path": "n.md", "heading": "A", "mode": "append", "text": fmt.Sprintf("marker-%d", i)}, fmt.Sprintf("marker-%d", i)
		},
		"insert_at_line": func(i int) (string, map[string]any, string) {
			return "insert_at_line", map[string]any{"path": "n.md", "lineNumber": float64(1), "text": fmt.Sprintf("marker-%d", i)}, fmt.Sprintf("marker-%d", i)
		},
		"update_task": func(i int) (string, map[string]any, string) {
			return "append_to_note", map[string]any{"path": "n.md", "text": fmt.Sprintf("marker-%d", i)}, fmt.Sprintf("marker-%d", i)
		},
	} {
		t.Run(name, func(t *testing.T) {
			for round := 0; round < rounds; round++ {
				root := seedFiles(t, map[string]string{
					".zennotes/vault.json": rootVault,
					"assets/pic0.png":      "png",
					"n.md":                 note,
				})
				cs := runScoped(t, root, nil)
				var wg sync.WaitGroup
				failures := make(chan string, edits+1)
				wants := make([]string, edits)
				wg.Add(1)
				go func() {
					defer wg.Done()
					if text, isErr := callTool(t, cs, "rename_asset", map[string]any{"path": "assets/pic0.png", "name": "renamed.png"}); isErr {
						failures <- text
					}
				}()
				for i := 0; i < edits; i++ {
					tool, args, want := edit(i)
					wants[i] = want
					wg.Add(1)
					go func() {
						defer wg.Done()
						if text, isErr := callTool(t, cs, tool, args); isErr {
							failures <- text
						}
					}()
				}
				if name == "update_task" {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if text, isErr := callTool(t, cs, "update_task", map[string]any{"id": "n.md#0", "due": "2026-02-02"}); isErr {
							failures <- text
						}
					}()
				}
				wg.Wait()
				close(failures)
				for text := range failures {
					t.Fatalf("a call failed: %s", text)
				}
				body := readFile(t, root, "n.md")
				for _, want := range wants {
					if !strings.Contains(body, want) {
						t.Fatalf("round %d lost %s:\n%s", round, want, body)
					}
				}
				if !strings.Contains(body, "assets/renamed.png") {
					t.Fatalf("round %d lost the link rewrite:\n%s", round, body)
				}
				if name == "update_task" && !strings.Contains(body, "due:2026-02-02") {
					t.Fatalf("round %d lost the task edit:\n%s", round, body)
				}
			}
		})
	}
}

// write_note replaces the body, so a rewrite that read the note before it must
// not put the old text back.
func TestAParallelWriteIsNotUndoneByALinkRewrite(t *testing.T) {
	for round := 0; round < 40; round++ {
		root := seedFiles(t, map[string]string{
			".zennotes/vault.json": rootVault,
			"assets/pic0.png":      "png",
			"n.md":                 "![[assets/pic0.png]]\n",
		})
		cs := runScoped(t, root, nil)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			callTool(t, cs, "rename_asset", map[string]any{"path": "assets/pic0.png", "name": "renamed.png"})
		}()
		go func() {
			defer wg.Done()
			callTool(t, cs, "write_note", map[string]any{"path": "n.md", "body": "written ![[assets/pic0.png]]\n"})
		}()
		wg.Wait()
		if body := readFile(t, root, "n.md"); !strings.HasPrefix(body, "written") {
			t.Fatalf("round %d: the write was undone: %q", round, body)
		}
	}
}
