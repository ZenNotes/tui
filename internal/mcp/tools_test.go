package mcp

import "testing"

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
