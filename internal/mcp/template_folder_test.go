package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
)

// The vault's template folder (ZenNotes/zennotes#1024): list_tasks leaves its
// placeholders out as the desktop's MCP server does, includeExcluded reaches
// them, and search_text skips its notes as the app's text search and the
// server do.
func TestTemplateFolderStaysOutOfListTasksAndSearchText(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		".zennotes/vault.json":    `{"primaryNotesLocation":"root","templates":{"folder":"Templates"}}`,
		"Templates/Meeting.md":    "- [ ] Agenda item for the standup\n",
		"Templates/Work/Retro.md": "- [ ] Action item\n",
		"Notes/Monday.md":         "- [ ] Book the room for the standup\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	sources := func(args map[string]any) []string {
		text, isErr := callTool(t, cs, "list_tasks", args)
		if isErr {
			t.Fatalf("list_tasks %v: %s", args, text)
		}
		var tasks []struct {
			SourcePath string `json:"sourcePath"`
		}
		if err := json.Unmarshal([]byte(text), &tasks); err != nil {
			t.Fatalf("list_tasks %v: %v\n%s", args, err, text)
		}
		out := []string{}
		for _, task := range tasks {
			out = append(out, task.SourcePath)
		}
		slices.Sort(out)
		return out
	}
	if got := sources(map[string]any{}); !reflect.DeepEqual(got, []string{"Notes/Monday.md"}) {
		t.Fatalf("list_tasks: %v", got)
	}
	if got := sources(map[string]any{"includeExcluded": true}); len(got) != 3 {
		t.Fatalf("list_tasks includeExcluded: %v", got)
	}

	text, isErr := callTool(t, cs, "search_text", map[string]any{"query": "standup"})
	if isErr {
		t.Fatalf("search_text: %s", text)
	}
	var matches []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(text), &matches); err != nil {
		t.Fatalf("search_text: %v\n%s", err, text)
	}
	if len(matches) != 1 || matches[0].Path != "Notes/Monday.md" {
		t.Fatalf("search_text = %+v, want only Notes/Monday.md", matches)
	}
}

// The served instructions stay the desktop's, which name the template folder
// among what list_tasks leaves out.
func TestInstructionsSayListTasksLeavesTheTemplateFolderOut(t *testing.T) {
	if !strings.Contains(DefaultInstructions, "excluded folders, the template folder,\n  and notes outside the vault's Task folders when it has any") {
		t.Fatal("the list_tasks paragraph is not the desktop's")
	}
}

// A note in the template folder contributes no tags: list_tags leaves its
// tags out of names and counts, and search_by_tag and list_notes' tag filter
// do not return it. list_notes without a tag still lists it.
func TestTemplateFolderContributesNoTags(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","templates":{"folder":"Templates"}}`,
		"Templates/Meeting.md": "---\ntags: [work, tpl-only]\n---\n# {{title}}\n",
		"Notes/Monday.md":      "# Monday\n\nPlanning #work\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	call := func(name string, args map[string]any, into any) {
		t.Helper()
		text, isErr := callTool(t, cs, name, args)
		if isErr {
			t.Fatalf("%s %v: %s", name, args, text)
		}
		if err := json.Unmarshal([]byte(text), into); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, text)
		}
	}
	var tags []struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	call("list_tags", map[string]any{}, &tags)
	if len(tags) != 1 || tags[0].Tag != "work" || tags[0].Count != 1 {
		t.Fatalf("list_tags = %+v, want only work with 1 note", tags)
	}
	paths := func(name string, args map[string]any) []string {
		var notes []struct {
			Path string `json:"path"`
		}
		call(name, args, &notes)
		out := []string{}
		for _, n := range notes {
			out = append(out, n.Path)
		}
		slices.Sort(out)
		return out
	}
	if got := paths("search_by_tag", map[string]any{"tag": "#work"}); !reflect.DeepEqual(got, []string{"Notes/Monday.md"}) {
		t.Fatalf("search_by_tag work = %v", got)
	}
	if got := paths("search_by_tag", map[string]any{"tag": "tpl-only"}); len(got) != 0 {
		t.Fatalf("search_by_tag tpl-only = %v, want none", got)
	}
	if got := paths("list_notes", map[string]any{"tag": "work"}); !reflect.DeepEqual(got, []string{"Notes/Monday.md"}) {
		t.Fatalf("list_notes tag work = %v", got)
	}
	if got := paths("list_notes", map[string]any{}); len(got) != 2 {
		t.Fatalf("list_notes = %v, want both notes", got)
	}
}
