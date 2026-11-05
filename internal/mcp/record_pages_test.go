package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const recordPage = "memory/Books.base/Alpha.md"

// seedRecordPages is a vault in root mode with a note beside a database.
func seedRecordPages(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		".zennotes/vault.json":          `{"primaryNotesLocation":"root"}`,
		"memory/note.md":                "# Note\n\nneedle in the note\n\n[[Alpha]]\n",
		"memory/Books.base/data.csv":    "title\nAlpha\n",
		"memory/Books.base/schema.json": "{}\n",
		recordPage:                      "# Alpha\n\nneedle on the page\n\n- [ ] page task\n",
		"memory/Empty.base/data.csv":    "title\n",
		"memory/Empty.base/schema.json": "{}\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// sees asks every listing whether the record page is there, and they must agree.
func seesRecordPage(t *testing.T, cs *sdk.ClientSession) bool {
	t.Helper()
	notes := strings.Contains(mustCall(t, cs, "list_notes", nil), recordPage)
	search := strings.Contains(mustCall(t, cs, "search_text", map[string]any{"query": "needle"}), recordPage)
	tasks := strings.Contains(mustCall(t, cs, "list_tasks", nil), "page task")
	text, failed := callTool(t, cs, "read_note", map[string]any{"path": recordPage})
	read := !failed && strings.Contains(text, "needle on the page")
	if notes != search || notes != tasks || notes != read {
		t.Errorf("the tools disagree: list_notes %v, search_text %v, list_tasks %v, read_note %v", notes, search, tasks, read)
	}
	return notes
}

func testRecordPages(t *testing.T, run func(*testing.T, string, *Scope) *sdk.ClientSession) {
	for _, tc := range []struct {
		name        string
		allow, deny []string
		sees, note  bool
	}{
		{name: "no paths", sees: true, note: true},
		{name: "allow the folder holding it", allow: []string{"memory"}, sees: true, note: true},
		{name: "allow the database", allow: []string{"memory/Books.base"}, sees: true},
		{name: "allow the page", allow: []string{recordPage}, sees: true},
		{name: "allow another folder", allow: []string{"memory/Empty.base"}},
		{name: "deny the page", deny: []string{recordPage}, note: true},
		{name: "deny the page in another case", deny: []string{"MEMORY/books.BASE/ALPHA.md"}, note: true},
		{name: "deny the database", deny: []string{"memory/Books.base"}, note: true},
		{name: "deny the database with no pages", deny: []string{"memory/Empty.base"}, sees: true, note: true},
		{name: "deny its folder", deny: []string{"memory"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scope *Scope
			if tc.allow != nil || tc.deny != nil {
				scope = testScope(t, "write", tc.allow, tc.deny)
			}
			root := seedRecordPages(t)
			cs := run(t, root, scope)
			if got := seesRecordPage(t, cs); got != tc.sees {
				t.Errorf("sees the page: %v, want %v", got, tc.sees)
			}
			if got := strings.Contains(mustCall(t, cs, "list_notes", nil), "memory/note.md"); got != tc.note {
				t.Errorf("sees the other note: %v, want %v", got, tc.note)
			}
			listed := mustCall(t, cs, "list_notes", nil)
			if strings.Contains(listed, "data.csv") || strings.Contains(listed, "schema.json") {
				t.Errorf("a database's own files are listed: %s", listed)
			}
		})
	}
}

func TestRecordPagesOnALocalVault(t *testing.T) { testRecordPages(t, runScoped) }

func TestRecordPagesOnAServer(t *testing.T) {
	scopeServerBinary(t)
	testRecordPages(t, runScopedOnServer)
}

func TestRecordPagesThroughHTTP(t *testing.T) {
	httpMCPBinary(t)
	testRecordPages(t, runScopedOnHTTPMCP)
}

// An entry that really names nothing still shuts the client out.
func TestStaleDenyEntryNextToADatabaseStillStopsTheClient(t *testing.T) {
	for _, deny := range []string{"memory/Gone.base", "memory/Books.base/Gone.md"} {
		cs := runScoped(t, seedRecordPages(t), testScope(t, "write", nil, []string{deny}))
		if got := mustFail(t, cs, "list_notes", nil); !strings.Contains(got, "does not exist in this vault") {
			t.Errorf("%s: %s", deny, got)
		}
	}
}

// A hidden page keeps what holds it: its folder cannot go as a whole, and the
// database's own files stay out of reach.
func TestScopeHoldsAroundARecordPage(t *testing.T) {
	root := seedRecordPages(t)
	cs := runScoped(t, root, testScope(t, "write", nil, []string{recordPage}))
	mustFail(t, cs, "delete_folder", map[string]any{"folder": "inbox", "subpath": "memory"})
	mustFail(t, cs, "rename_folder", map[string]any{"folder": "inbox", "subpath": "memory", "newSubpath": "moved"})
	mustFail(t, cs, "read_asset", map[string]any{"path": "memory/Books.base/data.csv"})
	mustFail(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": "memory/Books.base", "title": "Nope"})
	if !exists(root, recordPage) || exists(root, "memory/Books.base/Nope.md") {
		t.Error("the database changed")
	}

	cs = runScoped(t, root, testScope(t, "write", []string{"memory/Books.base"}, nil))
	mustFail(t, cs, "delete_folder", map[string]any{"folder": "inbox", "subpath": "memory/Books.base"})
	if !exists(root, recordPage) {
		t.Error("a scoped client deleted the database")
	}
}

// A link to a record page follows a rename, as on a server.
func TestRenamingARecordPageRewritesLinksToIt(t *testing.T) {
	root := seedRecordPages(t)
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "rename_note", map[string]any{"path": recordPage, "newTitle": "Beta"})
	if got := readFile(t, root, "memory/note.md"); !strings.Contains(got, "[[Beta]]") {
		t.Errorf("the link did not follow: %s", got)
	}
	if !exists(root, "memory/Books.base/Beta.md") {
		t.Error("the page did not move")
	}
}

// A client with a path scope may not see the database a page belongs to, so it reads
// and edits the page and leaves it where it is, wherever the vault lives.
func testRecordPageStaysPut(t *testing.T, run func(*testing.T, string, *Scope) *sdk.ClientSession) {
	const schema = `{"pages":{"row-1":"Alpha.md"}}`
	for _, tc := range []struct {
		name        string
		allow, deny []string
	}{
		{name: "allow", allow: []string{"memory", "archive", "trash", "elsewhere"}},
		{name: "deny", deny: []string{"memory/note.md"}},
	} {
		for tool, a := range map[string]map[string]any{
			"rename_note":    {"path": recordPage, "newTitle": "Beta"},
			"move_note":      {"path": recordPage, "targetFolder": "inbox", "targetSubpath": "memory"},
			"archive_note":   {"path": recordPage},
			"move_to_trash":  {"path": recordPage},
			"duplicate_note": {"path": recordPage},
			"delete_note":    {"path": recordPage, "confirm": true},
		} {
			t.Run(tc.name+"/"+tool, func(t *testing.T) {
				root := seedRecordPages(t)
				if err := os.WriteFile(filepath.Join(root, "memory/Books.base/schema.json"), []byte(schema), 0o644); err != nil {
					t.Fatal(err)
				}
				cs := run(t, root, testScope(t, "write", tc.allow, tc.deny))
				// A hosted client is not offered rename_note under a path scope at all.
				if got := mustFail(t, cs, tool, a); !strings.Contains(got, "record page") && got != "Unknown tool: "+tool {
					t.Errorf("refused with: %s", got)
				}
				if !exists(root, recordPage) || exists(root, "memory/Books.base/Alpha copy.md") || exists(root, "memory/Books.base/Beta.md") || exists(root, "memory/Alpha.md") {
					t.Error("a file moved or appeared")
				}
				if got := readFile(t, root, "memory/Books.base/schema.json"); got != schema {
					t.Errorf("the schema changed: %s", got)
				}
				mustCall(t, cs, "write_note", map[string]any{"path": recordPage, "body": "edited\n"})
			})
		}
	}
}

func TestRecordPageStaysPutOnALocalVault(t *testing.T) { testRecordPageStaysPut(t, runScoped) }

func TestRecordPageStaysPutOnAServer(t *testing.T) {
	scopeServerBinary(t)
	testRecordPageStaysPut(t, runScopedOnServer)
}

func TestRecordPageStaysPutThroughHTTP(t *testing.T) {
	httpMCPBinary(t)
	testRecordPageStaysPut(t, runScopedOnHTTPMCP)
}

// A hidden page answers as a missing one, before anything about what it is.
func TestHiddenRecordPageIsNotFoundEvenForARefusedTool(t *testing.T) {
	cs := runScoped(t, seedRecordPages(t), testScope(t, "write", nil, []string{recordPage}))
	if got := mustFail(t, cs, "duplicate_note", map[string]any{"path": recordPage}); !strings.Contains(got, "Note not found") {
		t.Errorf("got %s", got)
	}
}

// With no paths in its scope, a client has the vault to itself, on a server as here.
func TestRecordPageIsAnOrdinaryNoteAtFullAccess(t *testing.T) {
	root := seedRecordPages(t)
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "duplicate_note", map[string]any{"path": recordPage})
	if !exists(root, "memory/Books.base/Alpha copy.md") {
		t.Error("no copy")
	}
	mustCall(t, cs, "delete_note", map[string]any{"path": recordPage, "confirm": true})
	if exists(root, recordPage) {
		t.Error("the page was not deleted")
	}
}
