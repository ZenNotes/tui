package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
)

// The scope rules of ZenNotes/tui#21 (docs/ideas/mcp-scoped-access.md),
// driven through the server the way a client reaches them.

// seedScopedVault lays out a root-mode vault with notes the scope below
// shares, notes it hides, and the ways a hidden note could be reached
// anyway: a symlink from a shared folder, a case variant, the internal
// directory.
func seedScopedVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".zennotes/vault.json":          `{"primaryNotesLocation":"root"}`,
		"Projects/Plan.md":              "# Plan\n\n- [ ] ship it\n\nSee [[Diary]]. #proj needle\n",
		"Projects/Sub/Step.md":          "# Step\n",
		"Projects/Clients/Acme.md":      "# Acme\n\nclient secret needle #private\n- [ ] invoice acme\n",
		"Projects/diagram.png":          "png",
		"Areas/Health/Run.md":           "# Run\n\nafter [[Plan]]\n",
		"Areas/Money.md":                "# Money\n\nneedle\n",
		"Journal/Diary.md":              "# Diary\n\nprivate needle #private\n\nabout [[Plan]]\n- [ ] call mum\n",
		"archive/Projects/Old.md":       "# Old\n",
		"attachements/pic.png":          "png",
		"Journal/Many1.md":              "needle\nneedle\nneedle\n",
		"Journal/Many2.md":              "needle\nneedle\nneedle\n",
		"trash/Projects/Thrown.md":      "# Thrown\n",
		"Projects/Clients/Acme Two.md":  "# Acme Two\n",
		"Journal/Sub/Deep.md":           "# Deep\n",
		"Areas/Health/Clinic/Visit.md":  "# Visit\n",
		"Projects/Sub/More/Further.md":  "# Further\n",
		"Areas/Health/Clinic/Notes.md":  "# Notes\n",
		"Projects/Sub/More/Again.md":    "# Again\n",
		"Journal/Sub/Deeper/Bottom.md":  "# Bottom\n",
		"Areas/Health/Clinic/Bills.md":  "# Bills\n",
		"Projects/Sub/More/Last.md":     "# Last\n",
		"Journal/Sub/Deeper/Bottom2.md": "# Bottom2\n",
	}
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("..", "Journal"), filepath.Join(root, "Projects", "inner")); err != nil {
		t.Fatal(err)
	}
	return root
}

func testScope(t *testing.T, access string, allow, deny []string) *Scope {
	t.Helper()
	s, err := NewScope(access, false, allow, deny)
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

// runScoped serves a vault under a scope over an in-memory transport.
func runScoped(t *testing.T, root string, scope *Scope) *sdk.ClientSession {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	serverT, clientT := sdk.NewInMemoryTransports()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, Options{
			ResolveTarget: func() (backend.Target, error) { return backend.Target{Kind: backend.KindLocal, Root: root}, nil },
			OpenBackend:   func(t backend.Target) (Backend, error) { return backend.New(t, backend.Options{}) },
			Transport:     serverT,
			Scope:         scope,
		})
	}()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
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

func mustCall(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTool(t, cs, name, args)
	if isErr {
		t.Fatalf("%s %v failed: %s", name, args, text)
	}
	return text
}

func mustFail(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTool(t, cs, name, args)
	if !isErr {
		t.Fatalf("%s %v should fail, got: %s", name, args, text)
	}
	return text
}

func notePaths(t *testing.T, text string) []string {
	t.Helper()
	var notes []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(text), &notes); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	out := []string{}
	for _, n := range notes {
		out = append(out, n.Path)
	}
	sort.Strings(out)
	return out
}

func listedTools(t *testing.T, cs *sdk.ClientSession) map[string]*sdk.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*sdk.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

func sharedScope(t *testing.T) *Scope {
	// The issue's lowercase spelling finds `Projects/`.
	return testScope(t, "write", []string{"/projects", "Areas/Health"}, []string{"Projects/Clients"})
}

func TestScopeFlagsRefuseWhatCouldNeverBeShared(t *testing.T) {
	for _, bad := range []string{".zennotes", "Projects/../Journal", "./", "a/.hidden/b", ""} {
		if _, err := NewScope("", false, []string{bad}, nil); err == nil {
			t.Errorf("--allow-path %q should be refused", bad)
		}
	}
	if _, err := NewScope("admin", false, nil, nil); err == nil {
		t.Error("--access admin should be refused")
	}
	s, err := NewScope("write", true, []string{"/Projects/", `Areas\Health`, "Projects"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Access != AccessRead || !reflect.DeepEqual(s.Allow, []string{"Projects", "Areas/Health"}) {
		t.Fatalf("got %+v", s)
	}
}

func TestEveryToolIsClassified(t *testing.T) {
	for _, tool := range tools() {
		if _, ok := toolClasses[tool.name]; !ok {
			t.Errorf("%s has no access class in toolClasses", tool.name)
		}
	}
}

func TestScopedListingsShowOnlyWhatIsShared(t *testing.T) {
	root := seedScopedVault(t)
	cs := runScoped(t, root, sharedScope(t))

	got := notePaths(t, mustCall(t, cs, "list_notes", map[string]any{}))
	want := []string{
		"Areas/Health/Clinic/Bills.md", "Areas/Health/Clinic/Notes.md", "Areas/Health/Clinic/Visit.md", "Areas/Health/Run.md",
		"Projects/Plan.md", "Projects/Sub/More/Again.md", "Projects/Sub/More/Further.md", "Projects/Sub/More/Last.md", "Projects/Sub/Step.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list_notes:\n got %v\nwant %v", got, want)
	}
	if trash := notePaths(t, mustCall(t, cs, "list_notes", map[string]any{"folder": "trash"})); len(trash) != 0 {
		t.Fatalf("trash is not shared: %v", trash)
	}

	var folders []struct {
		Folder  string `json:"folder"`
		Subpath string `json:"subpath"`
	}
	if err := json.Unmarshal([]byte(mustCall(t, cs, "list_folders", map[string]any{})), &folders); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range folders {
		names = append(names, f.Folder+":"+f.Subpath)
	}
	sort.Strings(names)
	wantFolders := []string{"inbox:Areas", "inbox:Areas/Health", "inbox:Areas/Health/Clinic", "inbox:Projects", "inbox:Projects/Sub", "inbox:Projects/Sub/More"}
	if !reflect.DeepEqual(names, wantFolders) {
		t.Fatalf("list_folders:\n got %v\nwant %v", names, wantFolders)
	}

	tags := mustCall(t, cs, "list_tags", map[string]any{})
	if strings.Contains(tags, "private") || !strings.Contains(tags, "proj") {
		t.Fatalf("list_tags: %s", tags)
	}
	tasks := mustCall(t, cs, "list_tasks", map[string]any{})
	if !strings.Contains(tasks, "ship it") || strings.Contains(tasks, "invoice") || strings.Contains(tasks, "mum") {
		t.Fatalf("list_tasks: %s", tasks)
	}
	assets := mustCall(t, cs, "list_assets", map[string]any{})
	if !strings.Contains(assets, "Projects/diagram.png") || strings.Contains(assets, "attachements") {
		t.Fatalf("list_assets: %s", assets)
	}
	if links := notePaths(t, mustCall(t, cs, "backlinks", map[string]any{"path": "Projects/Plan.md"})); !reflect.DeepEqual(links, []string{"Areas/Health/Run.md"}) {
		t.Fatalf("backlinks: %v", links)
	}
	if hits := notePaths(t, mustCall(t, cs, "search_by_tag", map[string]any{"tag": "private"})); len(hits) != 0 {
		t.Fatalf("search_by_tag: %v", hits)
	}
	if hits := notePaths(t, mustCall(t, cs, "search_by_title", map[string]any{"query": "diary"})); len(hits) != 0 {
		t.Fatalf("search_by_title: %v", hits)
	}
}

// The walk stops at its limit, so filtering its first page would let hidden
// matches crowd out the shared one and say, by a short page, that they exist.
func TestScopedSearchFiltersBeforeItLimits(t *testing.T) {
	root := seedScopedVault(t)
	cs := runScoped(t, root, sharedScope(t))
	var matches []struct {
		Path     string `json:"path"`
		LineText string `json:"lineText"`
	}
	if err := json.Unmarshal([]byte(mustCall(t, cs, "search_text", map[string]any{"query": "needle", "limit": 1})), &matches); err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Path != "Projects/Plan.md" {
		t.Fatalf("search_text: %+v", matches)
	}
}

// A hidden note answers exactly like a missing one, whichever way it is
// reached, and no error carries the vault's absolute path.
func TestHiddenNotesAreIndistinguishableFromMissingOnes(t *testing.T) {
	root := seedScopedVault(t)
	cs := runScoped(t, root, sharedScope(t))
	for _, p := range []string{
		"Journal/Diary.md",         // outside allow
		"journal/DIARY.md",         // case variant of it
		"Projects/inner/Diary.md",  // through a symlink in a shared folder
		"Projects/Clients/Acme.md", // denied inside allow
		"projects/clients/ACME.md", // case variant of the denied note
		".zennotes/vault.json",     // the internal directory
		"Projects/../Journal/Diary.md",
		"Journal/Missing.md", // does not exist at all
	} {
		got := mustFail(t, cs, "read_note", map[string]any{"path": p})
		if got != "Error: Note not found: "+p {
			t.Errorf("read_note %q: %s", p, got)
		}
		if strings.Contains(got, root) {
			t.Errorf("read_note %q leaks the vault path: %s", p, got)
		}
	}
	body := mustCall(t, cs, "read_note", map[string]any{"path": "projects/plan.md"})
	if !strings.Contains(body, `"path": "Projects/Plan.md"`) {
		t.Fatalf("a case variant of a shared note reads it under its own name: %s", body)
	}
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"list_comments", map[string]any{"path": "Journal/Diary.md"}},
		{"backlinks", map[string]any{"path": "Journal/Diary.md"}},
		{"toggle_task", map[string]any{"id": "Journal/Diary.md#0"}},
		{"append_to_note", map[string]any{"path": "Journal/Diary.md", "text": "x"}},
		{"move_to_trash", map[string]any{"path": "Projects/Clients/Acme.md"}},
	} {
		if got := mustFail(t, cs, call.tool, call.args); !strings.HasPrefix(got, "Error: Note not found: ") {
			t.Errorf("%s: %s", call.tool, got)
		}
	}
	diary, _ := os.ReadFile(filepath.Join(root, "Journal", "Diary.md"))
	if string(diary) != "# Diary\n\nprivate needle #private\n\nabout [[Plan]]\n- [ ] call mum\n" {
		t.Fatalf("a hidden note changed: %q", diary)
	}
}

func TestScopedWritesStayInsideTheScope(t *testing.T) {
	root := seedScopedVault(t)
	cs := runScoped(t, root, sharedScope(t))

	created := mustCall(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": "projects/New", "title": "Idea"})
	if !strings.Contains(created, `"path": "Projects/New/Idea.md"`) {
		t.Fatalf("create_note in a shared folder, in the vault's spelling: %s", created)
	}
	for _, sub := range []string{"", "Journal", "Areas", ".zennotes", "Attachements", "archive", "Projects/Clients", "Projects/../Journal", "Projects/Books.base"} {
		got := mustFail(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": sub, "title": "Nope"})
		if !strings.Contains(got, "can create and move notes only inside: Projects, Areas/Health.") {
			t.Errorf("create_note subpath %q: %s", sub, got)
		}
	}
	mustFail(t, cs, "create_folder", map[string]any{"folder": "inbox", "subpath": "Journal/Planted"})
	if _, err := os.Stat(filepath.Join(root, "Journal", "Planted")); err == nil {
		t.Fatal("create_folder made a folder in a hidden one")
	}
	mustFail(t, cs, "move_note", map[string]any{"path": "Projects/Plan.md", "targetFolder": "inbox", "targetSubpath": "Journal"})

	// Renaming rewrites links in shared notes only: Run follows the new name,
	// the hidden Diary keeps its old link rather than gain one the assistant
	// chose.
	mustCall(t, cs, "rename_note", map[string]any{"path": "Projects/Plan.md", "newTitle": "Plan B"})
	run, _ := os.ReadFile(filepath.Join(root, "Areas", "Health", "Run.md"))
	diary, _ := os.ReadFile(filepath.Join(root, "Journal", "Diary.md"))
	if !strings.Contains(string(run), "[[Plan B]]") || !strings.Contains(string(diary), "[[Plan]]") {
		t.Fatalf("link rewrite:\nRun: %s\nDiary: %s", run, diary)
	}
	mustFail(t, cs, "rename_note", map[string]any{"path": "Projects/Clients/Acme Two.md", "newTitle": "Acme"})

	// Folders: a folder holding something hidden is never moved or deleted
	// as a whole, and the folder a scope names cannot move out of it.
	if got := mustFail(t, cs, "delete_folder", map[string]any{"folder": "inbox", "subpath": "Projects", "confirm": true}); !strings.Contains(got, "cannot delete that folder") {
		t.Fatalf("delete_folder Projects: %s", got)
	}
	if got := mustFail(t, cs, "delete_folder", map[string]any{"folder": "inbox", "subpath": "Journal", "confirm": true}); got != "Error: Folder not found: Journal" {
		t.Fatalf("delete_folder Journal: %s", got)
	}
	mustFail(t, cs, "delete_folder", map[string]any{"folder": "inbox", "subpath": "attachements", "confirm": true})
	mustFail(t, cs, "rename_folder", map[string]any{"folder": "inbox", "oldSubpath": "Areas/Health", "newSubpath": "Health"})
	mustCall(t, cs, "rename_folder", map[string]any{"folder": "inbox", "oldSubpath": "Projects/Sub", "newSubpath": "Projects/Steps"})
	for _, kept := range []string{"Journal/Diary.md", "attachements/pic.png", "Projects/Clients/Acme.md", "Projects/Steps/Step.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(kept))); err != nil {
			t.Errorf("%s: %v", kept, err)
		}
	}

	// Trashing is a lifecycle exit: allowed, and the note leaves the scope.
	trashed := mustCall(t, cs, "move_to_trash", map[string]any{"path": "Projects/Steps/Step.md"})
	var meta struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(trashed), &meta)
	mustFail(t, cs, "read_note", map[string]any{"path": meta.Path})
	mustFail(t, cs, "restore_from_trash", map[string]any{"path": meta.Path})
}

func TestScopeListsOnlyTheToolsItOffers(t *testing.T) {
	root := seedScopedVault(t)
	full := listedTools(t, runScoped(t, root, nil))
	if len(full) != len(tools()) {
		t.Fatalf("full access lists %d of %d tools", len(full), len(tools()))
	}
	if !full["read_note"].Annotations.ReadOnlyHint || *full["delete_note"].Annotations.DestructiveHint != true || *full["append_to_note"].Annotations.DestructiveHint {
		t.Fatal("annotations")
	}

	read := runScoped(t, root, testScope(t, "read", nil, nil))
	listed := listedTools(t, read)
	for name, tool := range listed {
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("read access lists %s", name)
		}
	}
	if _, ok := listed["vault_info"]; !ok || len(listed) != 12 {
		t.Fatalf("read access lists %d tools", len(listed))
	}
	if _, err := read.CallTool(context.Background(), &sdk.CallToolParams{Name: "write_note", Arguments: map[string]any{"path": "Projects/Plan.md", "body": "gone"}}); err == nil {
		t.Fatal("write_note must be unknown to a read-only client")
	}
	if body, _ := os.ReadFile(filepath.Join(root, "Projects", "Plan.md")); string(body) == "gone" {
		t.Fatal("read-only client wrote a note")
	}

	comment := listedTools(t, runScoped(t, root, testScope(t, "comment", nil, nil)))
	if _, ok := comment["add_comment"]; !ok {
		t.Fatal("comment access lists add_comment")
	}
	if _, ok := comment["toggle_task"]; ok {
		t.Fatal("comment access must not list toggle_task")
	}

	scoped := listedTools(t, runScoped(t, root, sharedScope(t)))
	if _, ok := scoped["empty_trash"]; ok {
		t.Fatal("empty_trash acts on every trashed note, so no path-restricted client gets it")
	}
	if _, ok := scoped["delete_note"]; !ok {
		t.Fatal("write access keeps delete_note")
	}
}

func TestScopedVaultInfoNamesNoAbsolutePathsOrDenyEntries(t *testing.T) {
	root := seedScopedVault(t)
	cs := runScoped(t, root, testScope(t, "read", []string{"projects", "Areas/Gone"}, []string{"Projects/Clients"}))
	text := mustCall(t, cs, "vault_info", map[string]any{})
	if strings.Contains(text, root) || strings.Contains(text, "vaultRoot") || strings.Contains(text, "Clients") || strings.Contains(text, "Journal") {
		t.Fatalf("vault_info says too much: %s", text)
	}
	var info struct {
		Access struct {
			Level        string   `json:"level"`
			Partial      bool     `json:"partial"`
			Allow        []string `json:"allow"`
			MissingAllow []string `json:"missingAllow"`
		} `json:"access"`
	}
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatal(err)
	}
	if info.Access.Level != "read" || !info.Access.Partial || !reflect.DeepEqual(info.Access.Allow, []string{"Areas/Gone", "Projects"}) || !reflect.DeepEqual(info.Access.MissingAllow, []string{"Areas/Gone"}) {
		t.Fatalf("access: %+v", info.Access)
	}
}

// A deny entry that matches nothing may have been renamed outside ZenNotes,
// so what it hid could sit under a new name: share nothing.
func TestAStaleDenyEntrySharesNothing(t *testing.T) {
	root := seedScopedVault(t)
	cs := runScoped(t, root, testScope(t, "read", nil, []string{"Diary Folder"}))
	got := mustFail(t, cs, "list_notes", map[string]any{})
	if !strings.Contains(got, "nothing is shared") || strings.Contains(got, "Diary Folder") {
		t.Fatalf("list_notes: %s", got)
	}
	info := mustCall(t, cs, "vault_info", map[string]any{})
	if !strings.Contains(info, `"blocked"`) {
		t.Fatalf("vault_info should explain: %s", info)
	}
}

// A server rewrites inbound links in every note itself, so a scoped rename
// there would write into notes the client cannot see.
func TestScopedRenameNeedsALinkScopedBackend(t *testing.T) {
	root := seedScopedVault(t)
	local, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	opaque := struct{ Backend }{local}
	_, err = CallToolScoped(context.Background(), "rename_note", map[string]any{"path": "Projects/Plan.md", "newTitle": "Other"}, opaque, sharedScope(t))
	if err == nil || !strings.Contains(err.Error(), "server rewrites links in every note") {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "Projects", "Plan.md")); statErr != nil {
		t.Fatal("the note must stay where it was")
	}
}

func TestScopedErrorsCarryNoAbsolutePath(t *testing.T) {
	root := seedScopedVault(t)
	local, _ := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	scoped := newScopedBackend(local, *sharedScope(t))
	err := errors.New("open " + filepath.Join(root, "Projects", "Plan.md") + ": permission denied")
	if got := DescribeToolError(err, scoped); strings.Contains(got, root) || !strings.Contains(got, "Projects") {
		t.Fatalf("got %s", got)
	}
	if got := DescribeToolError(err, local); !strings.Contains(got, root) {
		t.Fatal("full access keeps today's message")
	}
}

func TestScopeInstructionsSayWhatTheClientCanDo(t *testing.T) {
	text := scopeInstructions(*testScope(t, "read", []string{"Projects"}, nil))
	for _, want := range []string{"do not guess", "read but not change", "vault_info"} {
		if !strings.Contains(text, want) {
			t.Errorf("instructions lack %q: %s", want, text)
		}
	}
	if strings.Contains(scopeInstructions(*testScope(t, "comment", nil, nil)), "do not guess") {
		t.Error("an access-only scope hides no paths")
	}
}

// Entries are paths as they exist on disk, so remapped system folders need no
// translation; archiving keeps the subpath, so `99 Archive/Projects` is where
// a shared folder's archived notes go.
func TestScopeFollowsRemappedSystemFolders(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		".zennotes/vault.json":       `{"primaryNotesLocation":"inbox","systemFolderPaths":{"inbox":"01 Inbox","archive":"99 Archive"}}`,
		"01 Inbox/Projects/A.md":     "# A\n",
		"01 Inbox/Private/B.md":      "# B\n",
		"99 Archive/Projects/Old.md": "# Old\n",
		"99 Archive/Private/Gone.md": "# Gone\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs := runScoped(t, root, testScope(t, "write", []string{"01 Inbox/Projects", "99 Archive/Projects"}, nil))
	if got := notePaths(t, mustCall(t, cs, "list_notes", map[string]any{"folder": "archive"})); !reflect.DeepEqual(got, []string{"99 Archive/Projects/Old.md"}) {
		t.Fatalf("archive: %v", got)
	}
	created := mustCall(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": "Projects", "title": "C"})
	if !strings.Contains(created, `"path": "01 Inbox/Projects/C.md"`) {
		t.Fatalf("create_note: %s", created)
	}
	mustFail(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": "Private", "title": "D"})
	archived := mustCall(t, cs, "archive_note", map[string]any{"path": "01 Inbox/Projects/A.md"})
	if !strings.Contains(archived, `"path": "99 Archive/Projects/A.md"`) {
		t.Fatalf("archive_note: %s", archived)
	}
	mustCall(t, cs, "unarchive_note", map[string]any{"path": "99 Archive/Projects/A.md"})
	mustFail(t, cs, "unarchive_note", map[string]any{"path": "99 Archive/Private/Gone.md"})

	// With only the inbox side shared, archiving is still allowed, as an exit.
	narrow := runScoped(t, root, testScope(t, "write", []string{"01 Inbox/Projects"}, nil))
	mustCall(t, narrow, "archive_note", map[string]any{"path": "01 Inbox/Projects/C.md"})
	mustFail(t, narrow, "read_note", map[string]any{"path": "99 Archive/Projects/C.md"})
}
