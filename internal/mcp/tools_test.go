package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
)

// The desktop MCP server's tool names; the Go server must offer them all.
var desktopTools = []string{
	"add_comment", "append_to_note", "archive_note", "backlinks", "create_folder", "create_note", "delete_folder", "delete_note",
	"duplicate_note", "empty_trash", "insert_at_line", "list_assets", "list_comments", "list_folders", "list_notes", "list_tags",
	"list_tasks", "move_note", "move_to_trash", "prepend_to_note", "read_note", "rename_folder", "rename_note", "replace_in_note",
	"reply_to_comment", "resolve_comment", "restore_from_trash", "search_by_tag", "search_by_title", "search_text", "toggle_task",
	"unarchive_note", "vault_info", "write_note",
}

func TestToolsMatchTheDesktopServer(t *testing.T) {
	have := map[string]bool{}
	for _, tool := range tools() {
		have[tool.name] = true
	}
	for _, name := range desktopTools {
		if !have[name] {
			t.Errorf("missing tool %s", name)
		}
	}
}

// A repeated key in a schema is valid JSON, and some clients refuse it.
func TestToolSchemasRepeatNoKey(t *testing.T) {
	for _, def := range tools() {
		decoder := json.NewDecoder(strings.NewReader(def.schema))
		var walk func(string)
		walk = func(where string) {
			token, err := decoder.Token()
			if err != nil {
				t.Fatalf("%s: %v", def.name, err)
			}
			delim, isDelim := token.(json.Delim)
			if !isDelim {
				return
			}
			seen := map[string]bool{}
			for decoder.More() {
				if delim == '{' {
					key, err := decoder.Token()
					if err != nil {
						t.Fatalf("%s: %v", def.name, err)
					}
					if seen[key.(string)] {
						t.Errorf("%s repeats %q at %s", def.name, key, where)
					}
					seen[key.(string)] = true
					walk(where + "." + key.(string))
				} else {
					walk(where + "[]")
				}
			}
			if _, err := decoder.Token(); err != nil {
				t.Fatalf("%s: %v", def.name, err)
			}
		}
		walk(def.name)
	}
}

// The size is what decides whether an answer goes out, so it must not come in under the
// real thing, and it is what an ordinary answer should be charged.
func TestEncodedStringSize(t *testing.T) {
	for _, text := range []string{
		"plain words", `"quoted"`, `back\\slash`, "line\nbreak\r\ttab", "<b>&amp;</b>", "\x01\x1f",
		"caf\u00e9 \u6f22\u5b57 \U0001f600", "\u2028\u2029", "",
	} {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(text); err != nil {
			t.Fatal(err)
		}
		// The quotes around it and the newline after it do not count.
		real := int64(buf.Len() - 3)
		if got := encodedStringSize(text); got != real {
			t.Errorf("%q: %d, want %d", text, got, real)
		}
	}
	// An encoder may write a bad byte as a character or an escape. The larger is counted.
	if got := encodedStringSize("bad\xff"); got != 9 {
		t.Errorf("a bad byte: %d", got)
	}
}

// toggle_task rolls a repeating task the way `zn task toggle` does: the open
// copy for the next date goes above the ticked line.
func TestToggleTaskToolInsertsNextOccurrence(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "plants.md")
	note := "intro\n- [ ] Water plants scheduled:2026-11-02 repeat:mon,thu\noutro\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/plants.md#0"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	want := "intro\n- [ ] Water plants scheduled:2026-11-05 repeat:mon,thu\n" +
		"- [x] Water plants scheduled:2026-11-02 repeat:mon,thu\noutro\n"
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("note (%v):\n%s\nwant:\n%s", err, got, want)
	}
}
