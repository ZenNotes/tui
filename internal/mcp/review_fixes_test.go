package mcp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(root, rel string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

const rootVault = `{"primaryNotesLocation":"root"}`

// A name with one of these in it makes every link to the file stop resolving,
// while the rename still reports success.
func TestRenameAssetRefusesNamesThatBreakLinks(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/pic.png":       "png",
		"n.md":                 "![[assets/pic.png]] [f](assets/pic.png)\n",
	})
	cs := runScoped(t, root, nil)
	before := readFile(t, root, "n.md")
	for _, name := range []string{"b#c.png", "h?i.png", "50%25.png", "d|e.png", "f]g.png", "g[h.png", "j^k.png", "a:b.png", "pic.png.", "a\nb.png", "a\tb.png", "a\x7fb.png"} {
		got := mustFail(t, cs, "rename_asset", map[string]any{"path": "assets/pic.png", "name": name})
		if !strings.Contains(got, "links to it would break") {
			t.Errorf("%q: %s", name, got)
		}
	}
	if !exists(root, "assets/pic.png") || readFile(t, root, "n.md") != before {
		t.Fatal("a refused rename changed something")
	}
}

// What is still allowed has to keep working through both kinds of link.
func TestRenameAssetKeepsLinksWorkingForNamesItAllows(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/pic.png":       "png",
		"wiki.md":              "![[assets/pic.png]]\n",
		"plain.md":             "[f](assets/pic.png)\n",
	})
	cs := runScoped(t, root, nil)
	current := "assets/pic.png"
	for _, name := range []string{"a b.png", "a(1).png", "a&b.png", "a+b.png", "é.png", "a'b.png", "a,b.png", "a=b.png", "a@b.png", "a!b.png", "a~b.png"} {
		mustCall(t, cs, "rename_asset", map[string]any{"path": current, "name": name})
		current = "assets/" + name
		assets := []vault.AssetMeta{{Path: current, Name: name}}
		for _, note := range []string{"wiki.md", "plain.md"} {
			if !vault.AssetsReferencedBy(readFile(t, root, note), assets, nil, note)[current] {
				t.Errorf("%s no longer resolves in %s: %q", name, note, readFile(t, root, note))
			}
		}
	}
}

// An upload takes the same names as the app's import, which cleans them.
func TestUploadAssetStillCleansANameRatherThanRefusingIt(t *testing.T) {
	root := seedFiles(t, map[string]string{".zennotes/vault.json": rootVault})
	text := mustCall(t, runScoped(t, root, nil), "upload_asset", map[string]any{"filename": "b#c.png", "content_base64": "cG5n"})
	if !strings.Contains(text, `"path": "assets/b-c.png"`) {
		t.Fatalf("%s", text)
	}
}

func TestDoneOnARecurringTaskWritesTheNextOccurrence(t *testing.T) {
	line := "- [ ] water @repeat:daily due:2026-01-01\n"
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"updated.md":           line,
		"toggled.md":           line,
		"cancelled.md":         line,
		"plain.md":             "- [ ] plain due:2026-01-01\n",
	})
	cs := runScoped(t, root, nil)
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	want := "- [ ] water @repeat:daily due:" + next + "\n- [x] water due:2026-01-01\n"

	var task vault.Task
	if err := json.Unmarshal([]byte(mustCall(t, cs, "update_task", map[string]any{"id": "updated.md#0", "state": "done"})), &task); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, "updated.md"); got != want {
		t.Fatalf("update_task wrote %q, want %q", got, want)
	}
	if !task.Checked || task.ID != "updated.md#1" || task.LineNumber != 1 {
		t.Fatalf("update_task should return the finished task: %+v", task)
	}

	task = vault.Task{}
	if err := json.Unmarshal([]byte(mustCall(t, cs, "toggle_task", map[string]any{"id": "toggled.md#0"})), &task); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, "toggled.md"); got != want {
		t.Fatalf("toggle_task wrote %q, want %q", got, want)
	}
	if !task.Checked || task.ID != "toggled.md#1" {
		t.Fatalf("toggle_task should return the finished task: %+v", task)
	}

	// A change made in the same call counts: the series follows the new date.
	root2 := seedFiles(t, map[string]string{".zennotes/vault.json": rootVault, "n.md": line})
	far := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	after := time.Now().AddDate(1, 0, 1).Format("2006-01-02")
	mustCall(t, runScoped(t, root2, nil), "update_task", map[string]any{"id": "n.md#0", "state": "done", "due": far})
	if got := readFile(t, root2, "n.md"); got != "- [ ] water @repeat:daily due:"+after+"\n- [x] water due:"+far+"\n" {
		t.Fatalf("%q", got)
	}

	// Only finishing repeats. Cancelling ends it, and an ordinary task is one line.
	mustCall(t, cs, "update_task", map[string]any{"id": "cancelled.md#0", "state": "cancelled"})
	if got := readFile(t, root, "cancelled.md"); got != "- [-] water @repeat:daily due:2026-01-01\n" {
		t.Fatalf("%q", got)
	}
	mustCall(t, cs, "update_task", map[string]any{"id": "plain.md#0", "state": "done"})
	if got := readFile(t, root, "plain.md"); got != "- [x] plain due:2026-01-01\n" {
		t.Fatalf("%q", got)
	}
}

func TestAssetRewriteReportsNotesItCouldNotChange(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("needs a folder that refuses a write")
	}
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/pic.png":       "png",
		"Locked/n1.md":         "![[assets/pic.png]]\n",
		"open.md":              "![[assets/pic.png]]\n",
		"other.md":             "nothing here\n",
	})
	// A note is replaced through a temp file beside it, so it is the folder that
	// has to refuse.
	locked := filepath.Join(root, "Locked")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	cs := runScoped(t, root, nil)

	var got struct {
		Path            string
		LinksNotUpdated []string
	}
	if err := json.Unmarshal([]byte(mustCall(t, cs, "rename_asset", map[string]any{"path": "assets/pic.png", "name": "b.png"})), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "assets/b.png" || len(got.LinksNotUpdated) != 1 || got.LinksNotUpdated[0] != "Locked/n1.md" {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(readFile(t, root, "open.md"), "assets/b.png") {
		t.Fatal("the writable note was not rewritten")
	}

	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "n1.md"), []byte("![[assets/b.png]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	got.LinksNotUpdated = nil
	if err := json.Unmarshal([]byte(mustCall(t, cs, "move_asset", map[string]any{"path": "assets/b.png", "targetDir": "elsewhere"})), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.LinksNotUpdated) != 1 || got.LinksNotUpdated[0] != "Locked/n1.md" {
		t.Fatalf("move: %+v", got)
	}

	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if text := mustCall(t, cs, "rename_asset", map[string]any{"path": "elsewhere/b.png", "name": "c.png"}); strings.Contains(text, "linksNotUpdated") {
		t.Fatalf("nothing failed: %s", text)
	}
}

// Whether a file of that name exists in a hidden folder must not show in what
// the client's own note ends up saying.
func TestAssetRewriteDoesNotShowAHiddenFileOfTheSameName(t *testing.T) {
	run := func(extra map[string]string) string {
		files := map[string]string{
			".zennotes/vault.json": rootVault,
			"Shared/pic.png":       "png",
			"Shared/n.md":          "![[pic.png]]\n",
			"Hidden/keep.png":      "x",
		}
		for k, v := range extra {
			files[k] = v
		}
		root := seedFiles(t, files)
		cs := runScoped(t, root, testScope(t, "write", []string{"Shared"}, nil))
		mustCall(t, cs, "rename_asset", map[string]any{"path": "Shared/pic.png", "name": "new.png"})
		return readFile(t, root, "Shared/n.md")
	}
	without := run(nil)
	with := run(map[string]string{"Hidden/new.png": "x"})
	if without != with {
		t.Fatalf("the note differs with a hidden file of that name: %q vs %q", without, with)
	}
	if without != "![[Shared/new.png]]\n" {
		t.Fatalf("%q", without)
	}

	// With no scope a bare name stays bare while it is unique.
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"Shared/pic.png":       "png",
		"Shared/n.md":          "![[pic.png]]\n",
	})
	mustCall(t, runScoped(t, root, nil), "rename_asset", map[string]any{"path": "Shared/pic.png", "name": "new.png"})
	if got := readFile(t, root, "Shared/n.md"); got != "![[new.png]]\n" {
		t.Fatalf("%q", got)
	}
}

func TestMoveAssetCannotLandOnADeniedPath(t *testing.T) {
	t.Run("the name itself", func(t *testing.T) {
		root := seedFiles(t, map[string]string{
			".zennotes/vault.json": rootVault,
			"assets/cafe.png":      "png",
			"dest/Café.png":        "denied",
			"dest/ok.txt":          "ok",
			"n.md":                 "![[assets/cafe.png]]\n",
		})
		cs := runScoped(t, root, testScope(t, "write", nil, []string{"dest/Café.png"}))
		if got := mustFail(t, cs, "move_asset", map[string]any{"path": "assets/cafe.png", "targetDir": "dest"}); !strings.Contains(got, "cannot put files there") {
			t.Fatalf("%s", got)
		}
		if !exists(root, "assets/cafe.png") || exists(root, "dest/cafe.png") || readFile(t, root, "n.md") != "![[assets/cafe.png]]\n" {
			t.Fatal("a refused move changed something")
		}
	})
	t.Run("the number a taken name gets", func(t *testing.T) {
		root := seedFiles(t, map[string]string{
			".zennotes/vault.json": rootVault,
			"assets/cafe.png":      "png",
			"dest/cafe.png":        "taken",
			"dest/Café 2.png":      "denied",
		})
		cs := runScoped(t, root, testScope(t, "write", nil, []string{"dest/Café 2.png"}))
		if got := mustFail(t, cs, "move_asset", map[string]any{"path": "assets/cafe.png", "targetDir": "dest"}); !strings.Contains(got, "cannot put files there") {
			t.Fatalf("%s", got)
		}
		if !exists(root, "assets/cafe.png") || exists(root, "dest/cafe 2.png") {
			t.Fatal("the file moved")
		}
	})
}

func TestDailyNoteDoesNotNameAHiddenFolder(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"Private Days","titlePattern":"yyyy-MM-dd"}}`,
		"Elsewhere/Plan.md":    "# Plan\n",
		"Private Days/old.md":  "# old\n",
	})
	result := func(scopeText string) map[string]any {
		var r struct{ CreateWith map[string]any }
		if err := json.Unmarshal([]byte(scopeText), &r); err != nil {
			t.Fatal(err)
		}
		return r.CreateWith
	}
	narrow := mustCall(t, runScoped(t, root, testScope(t, "read", []string{"Elsewhere"}, nil)), "daily_note", map[string]any{"date": "2026-03-05"})
	if strings.Contains(narrow, "Private Days") {
		t.Fatalf("a hidden folder is named: %s", narrow)
	}
	if _, has := result(narrow)["subpath"]; has {
		t.Fatalf("%s", narrow)
	}
	denied := mustCall(t, runScoped(t, root, testScope(t, "read", nil, []string{"Private Days"})), "daily_note", map[string]any{"date": "2026-03-05"})
	if strings.Contains(denied, "Private Days") {
		t.Fatalf("a denied folder is named: %s", denied)
	}
	full := mustCall(t, runScoped(t, root, nil), "daily_note", map[string]any{"date": "2026-03-05"})
	if result(full)["subpath"] != "Private Days" {
		t.Fatalf("%s", full)
	}
	shared := mustCall(t, runScoped(t, root, testScope(t, "read", []string{"Private Days"}, nil)), "daily_note", map[string]any{"date": "2026-03-05"})
	if result(shared)["subpath"] != "Private Days" {
		t.Fatalf("a shared folder is named as usual: %s", shared)
	}
}

// The check has to see the half of the pair the client cannot.
func TestAScopeCannotSplitADatabasePair(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"Data/t.csv":           "a\n1\n",
		"Data/t.csv.base.json": "{}",
		"Data/plain.csv":       "a\n1\n",
	})
	cs := runScoped(t, root, testScope(t, "write", []string{"Data/t.csv", "Data/plain.csv"}, nil))
	for tool, args := range map[string]map[string]any{
		"delete_asset":    {"path": "Data/t.csv"},
		"rename_asset":    {"path": "Data/t.csv", "name": "u.csv"},
		"move_asset":      {"path": "Data/t.csv", "targetDir": "Data"},
		"duplicate_asset": {"path": "Data/t.csv"},
	} {
		if got := mustFail(t, cs, tool, args); !strings.Contains(got, "belongs to a ZenNotes database") {
			t.Errorf("%s: %s", tool, got)
		}
	}
	if !exists(root, "Data/t.csv") || !exists(root, "Data/t.csv.base.json") {
		t.Fatal("the pair was split")
	}
	if text := mustCall(t, cs, "read_asset", map[string]any{"path": "Data/t.csv"}); !strings.Contains(text, "a\n1") {
		t.Fatalf("reading stays allowed: %s", text)
	}
	mustCall(t, cs, "delete_asset", map[string]any{"path": "Data/plain.csv"})
}

func TestUpdateTaskExpectedTextCatchesAnIdThatMoved(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"s.md":                 "- [ ] first\n- [ ] second\n- [ ] third\n",
	})
	cs := runScoped(t, root, nil)
	var tasks []vault.Task
	if err := json.Unmarshal([]byte(mustCall(t, cs, "list_tasks", map[string]any{})), &tasks); err != nil {
		t.Fatal(err)
	}
	var second vault.Task
	for _, task := range tasks {
		if task.Content == "second" {
			second = task
		}
	}
	if second.ID != "s.md#1" {
		t.Fatalf("%+v", tasks)
	}
	if err := os.WriteFile(filepath.Join(root, "s.md"), []byte("- [ ] second\n- [ ] third\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := mustFail(t, cs, "update_task", map[string]any{"id": second.ID, "state": "done", "expectedText": second.Content})
	if !strings.Contains(got, `now reads "third", not "second"`) || !strings.Contains(got, "list_tasks") {
		t.Fatalf("%s", got)
	}
	if body := readFile(t, root, "s.md"); body != "- [ ] second\n- [ ] third\n" {
		t.Fatalf("a refused update wrote %q", body)
	}
	mustCall(t, cs, "update_task", map[string]any{"id": "s.md#0", "state": "done", "expectedText": "  second "})
	if body := readFile(t, root, "s.md"); body != "- [x] second\n- [ ] third\n" {
		t.Fatalf("%q", body)
	}
}

func TestTaskIdsAreNotDescribedAsStable(t *testing.T) {
	if strings.Contains(strings.ToLower(DefaultInstructions), "are stable") {
		t.Error("the instructions call task ids stable, and they count position")
	}
	for _, tool := range tools() {
		if (tool.name == "toggle_task" || tool.name == "update_task") && strings.Contains(strings.ToLower(tool.description+tool.schema), "stable") {
			t.Errorf("%s: %s", tool.name, tool.description)
		}
	}
}

func TestFileTaskFieldsTheParserReads(t *testing.T) {
	note := func(status string) string {
		return "---\ntags: [task]\nstatus: " + status + "\n---\n# T\n"
	}
	task := func(t *testing.T, text string) vault.Task {
		t.Helper()
		var out vault.Task
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	setup := func(t *testing.T, status string) (string, func(map[string]any) vault.Task, func(map[string]any) string) {
		t.Helper()
		root := seedFiles(t, map[string]string{".zennotes/vault.json": rootVault, "t.md": note(status)})
		cs := runScoped(t, root, nil)
		call := func(args map[string]any) map[string]any {
			args["id"] = "t.md#task"
			return args
		}
		return root,
			func(args map[string]any) vault.Task { return task(t, mustCall(t, cs, "update_task", call(args))) },
			func(args map[string]any) string { return mustFail(t, cs, "update_task", call(args)) }
	}

	t.Run("waiting is a status", func(t *testing.T) {
		root, update, _ := setup(t, "open")
		if got := update(map[string]any{"waiting": true}); !got.Waiting || got.Status != "waiting" {
			t.Fatalf("%+v", got)
		}
		if body := readFile(t, root, "t.md"); strings.Contains(body, "waiting: true") {
			t.Fatalf("a field the parser ignores: %q", body)
		}
		if got := update(map[string]any{"waiting": false}); got.Waiting || got.Status != "open" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("not waiting leaves another status alone", func(t *testing.T) {
		_, update, _ := setup(t, "backlog")
		if got := update(map[string]any{"waiting": false}); got.Status != "backlog" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("leaving done clears the date", func(t *testing.T) {
		for _, leave := range []map[string]any{{"status": "backlog"}, {"status": ""}, {"waiting": true}} {
			root, update, _ := setup(t, "open")
			if got := update(map[string]any{"state": "done"}); !got.Checked || got.CompletedDate == "" {
				t.Fatalf("%+v", got)
			}
			if got := update(leave); got.Checked || got.CompletedDate != "" {
				t.Fatalf("%v: %+v", leave, got)
			}
			if body := readFile(t, root, "t.md"); strings.Contains(body, "completedDate") {
				t.Fatalf("%v: %q", leave, body)
			}
		}
	})
	t.Run("a done status dates it", func(t *testing.T) {
		_, update, _ := setup(t, "open")
		if got := update(map[string]any{"status": "done"}); !got.Checked || got.CompletedDate == "" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("two answers to one question are refused", func(t *testing.T) {
		for _, both := range []map[string]any{
			{"state": "done", "status": "backlog"},
			{"waiting": true, "status": "backlog"},
			{"waiting": true, "state": "open"},
		} {
			root, _, refuse := setup(t, "open")
			if got := refuse(both); !strings.Contains(got, "one frontmatter status") {
				t.Errorf("%v: %s", both, got)
			}
			if body := readFile(t, root, "t.md"); body != note("open") {
				t.Errorf("%v wrote %q", both, body)
			}
		}
	})
}

func TestEditSectionUnderAHeadingWithOnlySpaces(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"n.md":                 "#   \nfirst\nsecond\n# Next\n",
	})
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "edit_section", map[string]any{"path": "n.md", "heading": "#", "mode": "replace", "text": "x"})
	if got := readFile(t, root, "n.md"); got != "#   \n\nx\n\n# Next\n" {
		t.Fatalf("%q", got)
	}
}

func TestListAssetsLeavesOutWhatTheAppHides(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json":          rootVault,
		"Lists/Books.csv":               "a\n",
		"Lists/Books.csv.base.json":     "{}",
		"Lists/Books.csv.bak":           "a\n",
		"Lists/Books.csv.base.json.bak": "{}",
		"Lists/report.bak":              "kept",
		"assets/pic.png":                "png",
	})
	cs := runScoped(t, root, nil)
	text := mustCall(t, cs, "list_assets", map[string]any{})
	for _, hidden := range []string{"Books.csv.base.json", "Books.csv.bak"} {
		if strings.Contains(text, hidden) {
			t.Errorf("%s is listed: %s", hidden, text)
		}
	}
	for _, kept := range []string{"Lists/Books.csv\"", "Lists/report.bak", "assets/pic.png"} {
		if !strings.Contains(text, kept) {
			t.Errorf("%s is missing: %s", kept, text)
		}
	}
	// The pair is still seen, or the CSV would stop being a database.
	if got := mustFail(t, cs, "delete_asset", map[string]any{"path": "Lists/Books.csv"}); !strings.Contains(got, "belongs to a ZenNotes database") {
		t.Errorf("%s", got)
	}
	for _, tool := range tools() {
		if tool.name == "list_assets" && strings.Contains(tool.description, "attachments directory") {
			t.Errorf("assets live anywhere in the vault: %s", tool.description)
		}
	}
}

// A note that lands as one once a system drops the end of its name is a note, and
// a device name is nothing a file can be called there.
func TestAssetNamesThatDoNotSurviveEverySystem(t *testing.T) {
	for name, want := range map[string]string{
		"note.md.":       "Notes are not attachments",
		"note.md ":       "Notes are not attachments",
		"note.md::$DATA": "Notes are not attachments",
		"CON":            "reserved on Windows",
		"nul.txt":        "reserved on Windows",
		"lpt1.png":       "reserved on Windows",
	} {
		err := checkAssetName(name)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"console.png", "null.txt", "lpt.png", "com0.png", "notes.mdx"} {
		if err := checkAssetName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestMoveAssetRefusesFolderNamesThatDoNotSurviveEverySystem(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/pic.png":       "png",
	})
	cs := runScoped(t, root, nil)
	for _, dir := range []string{"Files.", "Files ", "a/b.", "a:b"} {
		if got := mustFail(t, cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": dir}); !strings.Contains(got, "cannot put files there") {
			t.Errorf("%q: %s", dir, got)
		}
	}
	if !exists(root, "assets/pic.png") {
		t.Fatal("the file moved")
	}
}

func TestFolderNameListedIsWhatTheParentLists(t *testing.T) {
	root := seedFiles(t, map[string]string{"Kept/x.png": "png", "Café/y.png": "png"})
	for name, want := range map[string]bool{"Kept": true, "kept": true, "cafe": true, "Other": false, "Kep": false} {
		if got := folderNameListed(root, name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
	if !foldersListed(root, "Kept/new/deeper") || !foldersListed(root, "Missing/x") {
		t.Error("a folder that does not exist yet is fine")
	}
	if folderNameListed(filepath.Join(root, "Missing"), "x") {
		t.Error("a parent that cannot be read lists nothing")
	}
}

func TestEditsKeepAWindowsNotesLineEndings(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"task.md":              "# T\r\n\r\n- [ ] one\r\n- [ ] two\r\n",
		"rec.md":               "- [ ] water @repeat:daily due:2026-01-01\r\n- [ ] after\r\n",
		"sec.md":               "# A\r\n\r\nbody\r\n\r\n# B\r\n\r\nmore\r\n",
		"mixed.md":             "- [ ] one\r\n- [ ] two\nlast\n",
	})
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "update_task", map[string]any{"id": "task.md#1", "state": "done", "due": "2026-02-02"})
	if got := readFile(t, root, "task.md"); got != "# T\r\n\r\n- [ ] one\r\n- [x] two due:2026-02-02\r\n" {
		t.Fatalf("%q", got)
	}
	mustCall(t, cs, "update_task", map[string]any{"id": "mixed.md#0", "priority": "high"})
	if got := readFile(t, root, "mixed.md"); got != "- [ ] one !high\r\n- [ ] two\nlast\n" {
		t.Fatalf("each line keeps its own ending: %q", got)
	}
	var done vault.Task
	if err := json.Unmarshal([]byte(mustCall(t, cs, "update_task", map[string]any{"id": "rec.md#0", "state": "done"})), &done); err != nil {
		t.Fatal(err)
	}
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if got := readFile(t, root, "rec.md"); got != "- [ ] water @repeat:daily due:"+next+"\r\n- [x] water due:2026-01-01\r\n- [ ] after\r\n" {
		t.Fatalf("%q", got)
	}
	if done.LineNumber != 1 || !done.Checked {
		t.Fatalf("%+v", done)
	}
	mustCall(t, cs, "edit_section", map[string]any{"path": "sec.md", "heading": "A", "mode": "append", "text": "x\ny"})
	if got := readFile(t, root, "sec.md"); got != "# A\r\n\r\nbody\r\n\r\nx\r\ny\r\n\r\n# B\r\n\r\nmore\r\n" {
		t.Fatalf("%q", got)
	}
}

// Windows makes a junction without any privilege. Elsewhere this is a symbolic
// link, which the same rules cover.
func linkFolder(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestAFolderLinkShowsNothingAndTakesNoWrite(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"Journal/Diary.md":     "# Diary\n",
		"Journal/secret.png":   "png",
		"Open/Free.md":         "# Free\n",
		"Open/pic.png":         "png",
	})
	linkFolder(t, filepath.Join(root, "Journal"), filepath.Join(root, "Open", "jn"))
	cs := runScoped(t, root, testScope(t, "write", []string{"Open"}, nil))
	if got := notePaths(t, mustCall(t, cs, "list_notes", map[string]any{})); len(got) != 1 || got[0] != "Open/Free.md" {
		t.Fatalf("%v", got)
	}
	if text := mustCall(t, cs, "list_assets", map[string]any{}); strings.Contains(text, "secret.png") {
		t.Fatalf("%s", text)
	}
	if got := mustFail(t, cs, "read_note", map[string]any{"path": "Open/jn/Diary.md"}); !strings.Contains(got, "Note not found") {
		t.Fatalf("%s", got)
	}
	if got := mustFail(t, cs, "read_asset", map[string]any{"path": "Open/jn/secret.png"}); !strings.Contains(got, "Asset not found") {
		t.Fatalf("%s", got)
	}
	mustFail(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": "Open/jn", "title": "In"})
	mustFail(t, cs, "move_note", map[string]any{"path": "Open/Free.md", "targetFolder": "inbox", "targetSubpath": "Open/jn"})
	mustFail(t, cs, "move_asset", map[string]any{"path": "Open/pic.png", "targetDir": "Open/jn"})
	if exists(root, "Journal/In.md") || exists(root, "Journal/Free.md") || exists(root, "Journal/pic.png") {
		t.Fatal("something landed behind the link")
	}
}
