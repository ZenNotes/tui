package mcp

import (
	"encoding/json"
	"fmt"
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
	for _, name := range []string{"b#c.png", "h?i.png", "50%25.png", "d|e.png", "f]g.png", "g[h.png", "j^k.png", "a:b.png", "pic.png.", "a*b.png", "a\"b.png", "a<b.png", "a>b.png", "a`b.png", "a\nb.png", "a\tb.png", "a\x7fb.png"} {
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
	// A toggle in a local vault leaves the rule on the finished line.
	wantToggle := "- [ ] water @repeat:daily due:" + next + "\n- [x] water @repeat:daily due:2026-01-01\n"

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
	if got := readFile(t, root, "toggled.md"); got != wantToggle {
		t.Fatalf("toggle_task wrote %q, want %q", got, wantToggle)
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

func TestDailyNoteOffersNoWayToStartANoteItCannotFile(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"Private Days","titlePattern":"yyyy-MM-dd"}}`,
		"Elsewhere/Plan.md":    "# Plan\n",
		"Private Days/old.md":  "# old\n",
	})
	result := func(text string) (map[string]any, string) {
		var r struct {
			CreateWith   map[string]any
			CannotCreate string
		}
		if err := json.Unmarshal([]byte(text), &r); err != nil {
			t.Fatal(err)
		}
		return r.CreateWith, r.CannotCreate
	}
	for name, cs := range map[string]func() string{
		"allow elsewhere": func() string {
			return mustCall(t, runScoped(t, root, testScope(t, "read", []string{"Elsewhere"}, nil)), "daily_note", map[string]any{"date": "2026-03-05"})
		},
		"deny the folder": func() string {
			return mustCall(t, runScoped(t, root, testScope(t, "read", nil, []string{"Private Days"})), "daily_note", map[string]any{"date": "2026-03-05"})
		},
	} {
		text := cs()
		with, cannot := result(text)
		if strings.Contains(text, "Private Days") || with != nil || !strings.Contains(cannot, "cannot be created here") {
			t.Errorf("%s: %s", name, text)
		}
	}
	full := mustCall(t, runScoped(t, root, nil), "daily_note", map[string]any{"date": "2026-03-05"})
	if with, cannot := result(full); with["subpath"] != "Private Days" || cannot != "" {
		t.Fatalf("%s", full)
	}
	shared := mustCall(t, runScoped(t, root, testScope(t, "read", []string{"Private Days"}, nil)), "daily_note", map[string]any{"date": "2026-03-05"})
	if with, _ := result(shared); with["subpath"] != "Private Days" {
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

// A rename keeps the name it is given, so one that does not survive is refused.
func TestRenameRefusesNamesThatDoNotSurviveEverySystem(t *testing.T) {
	for name, want := range map[string]string{
		"note.md.":       "Notes are not attachments",
		"note.md ":       "Notes are not attachments",
		"note.md::$DATA": "Notes are not attachments",
		"notes.md:x.png": "Notes are not attachments",
		"CON":            "reserved on Windows",
		"con .png":       "reserved on Windows",
		"CONIN$":         "reserved on Windows",
		"conout$.txt":    "reserved on Windows",
		"COM¹":           "reserved on Windows",
		"lpt².png":       "reserved on Windows",
		"nul.txt":        "reserved on Windows",
		"aux.tar.gz":     "reserved on Windows",
		"x.csv.bak":      "cannot be managed as an attachment",
	} {
		err := checkAssetRename(name)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"console.png", "null.txt", "lpt.png", "com0.png", "notes.mdx", "conin.png", "com⁴.png", "report.bak"} {
		if err := checkAssetRename(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

// An upload cleans what a rename refuses, as the app's import does.
func TestUploadCleansNamesThatDoNotSurviveEverySystem(t *testing.T) {
	root := seedFiles(t, map[string]string{".zennotes/vault.json": rootVault})
	cs := runScoped(t, root, nil)
	for name, want := range map[string]string{
		"aux.png":        "assets/_aux.png",
		"con.tar.gz":     "assets/_con.tar.gz",
		"nul.txt":        "assets/_nul.txt",
		"CON":            "assets/_CON",
		"notes.md:x.png": "assets/notes.md-x.png",
		"x.png.":         "assets/x.png",
	} {
		var got struct{ Path string }
		if err := json.Unmarshal([]byte(mustCall(t, cs, "upload_asset", map[string]any{"filename": name, "content_base64": "cG5n"})), &got); err != nil {
			t.Fatal(err)
		}
		if got.Path != want || !exists(root, want) {
			t.Errorf("%q: %q, want %q", name, got.Path, want)
		}
	}
	for name, want := range map[string]string{"x.csv.bak": "cannot be managed", "x.csv.base.json.bak": "cannot be managed", "note.md.": "Notes are not attachments"} {
		if got := mustFail(t, cs, "upload_asset", map[string]any{"filename": name, "content_base64": "cG5n"}); !strings.Contains(got, want) {
			t.Errorf("%q: %s", name, got)
		}
	}
}

func TestMoveAssetRefusesFolderNamesThatDoNotSurviveEverySystem(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/pic.png":       "png",
	})
	cs := runScoped(t, root, nil)
	for _, dir := range []string{"Files.", "Files ", "a/b.", "a:b", " Files", "Files\n", " trash", "\u00a0trash", " .hidden", " .ZENNOTES", "\u00a0.zennotes"} {
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
	for name, want := range map[string]string{"Kept": "Kept", "kept": "Kept", "cafe": "Café", "Other": "", "Kep": ""} {
		if got, ok := folderNameListed(root, name); got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", name, got, ok)
		}
	}
	for rel, want := range map[string]string{"Kept/new/deeper": "Kept/new/deeper", "Missing/x": "Missing/x"} {
		if got, ok := foldersAsListed(root, rel); !ok || got != want {
			t.Errorf("a folder that does not exist yet is fine: %q %q %v", rel, got, ok)
		}
	}
	if _, ok := folderNameListed(filepath.Join(root, "Missing"), "x"); ok {
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

// A name can pass while the name the upload will use does not.
func TestUploadChecksTheNameItWillUse(t *testing.T) {
	root := seedFiles(t, map[string]string{".zennotes/vault.json": rootVault})
	cs := runScoped(t, root, nil)
	for name, want := range map[string]string{
		"a:.md":             "Notes are not attachments",
		"d.csv.base.json .": "cannot be managed as an attachment",
		"x.excalidraw .":    "cannot be managed as an attachment",
	} {
		if got := mustFail(t, cs, "upload_asset", map[string]any{"filename": name, "content_base64": "cG5n"}); !strings.Contains(got, want) {
			t.Errorf("%q: %s", name, got)
		}
	}
	if exists(root, "assets") {
		t.Fatal("a refused upload wrote something")
	}
}

func TestMoveAssetWritesAnExistingFolderAsItsParentListsIt(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/pic.png":       "png",
		"Dest/Inner/keep.txt":  "x",
		"n.md":                 "![[assets/pic.png]]\n",
	})
	if _, err := os.Stat(filepath.Join(root, "DEST")); err != nil {
		t.Skip("needs a case-insensitive volume")
	}
	var got struct{ Path string }
	if err := json.Unmarshal([]byte(mustCall(t, runScoped(t, root, nil), "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": "DEST/inner/new"})), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "Dest/Inner/new/pic.png" {
		t.Fatalf("%+v", got)
	}
	if body := readFile(t, root, "n.md"); body != "![[Dest/Inner/new/pic.png]]\n" {
		t.Fatalf("%q", body)
	}
}

func TestDuplicateAssetUndoesACopyOutsideTheView(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json":   rootVault,
		"assets/cafe.png":        "png",
		"assets/cafe copy.png":   "taken",
		"assets/Café copy 2.png": "denied",
	})
	cs := runScoped(t, root, testScope(t, "write", nil, []string{"assets/Café copy 2.png"}))
	if got := mustFail(t, cs, "duplicate_asset", map[string]any{"path": "assets/cafe.png"}); !strings.Contains(got, "cannot give the file that name") {
		t.Fatalf("%s", got)
	}
	if exists(root, "assets/cafe copy 2.png") {
		t.Fatal("the copy stayed")
	}
}

func TestADeletedRecordIsJudgedByItsCleanedPath(t *testing.T) {
	files := map[string]string{
		".zennotes/vault.json": rootVault,
		"Journal/Diary.md":     "# Diary\n",
		"Shared/pic.png":       "png",
	}
	for id, recorded := range map[string]string{
		"11111111-1111-4111-8111-111111111111": "/Journal/old.png",
		"22222222-2222-4222-8222-222222222222": "Journal//old.png",
		"33333333-3333-4333-8333-333333333333": " Journal/old.png",
		"44444444-4444-4444-8444-444444444444": "Shared/kept.png",
	} {
		data, _ := json.Marshal(map[string]string{"path": recorded, "name": "old.png", "deletedAt": "2026-01-01T00:00:00.000Z"})
		files[".zennotes/deleted-assets/"+id+"/.zn-deleted.json"] = string(data)
		files[".zennotes/deleted-assets/"+id+"/old.png"] = "old"
	}
	root := seedFiles(t, files)
	cs := runScoped(t, root, testScope(t, "write", nil, []string{"Journal"}))
	text := mustCall(t, cs, "list_assets", map[string]any{"deleted": true})
	if strings.Contains(text, "Journal") || !strings.Contains(text, "Shared/kept.png") {
		t.Fatalf("%s", text)
	}
	for _, token := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"} {
		if got := mustFail(t, cs, "restore_asset", map[string]any{"undoToken": token}); !strings.Contains(got, "Deleted asset not found") {
			t.Errorf("%s: %s", token, got)
		}
	}
	if exists(root, "Journal/old.png") {
		t.Fatal("restored into a hidden folder")
	}
}

func TestAWikilinkWithAnAnchorNamesTheNote(t *testing.T) {
	body := "![[Report#Heading]] [[Report^block]] [[Report|alias]]\n"
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"Report.md":            "# Report\n",
		"files/Report":         "a file of that name",
		"n.md":                 body,
	})
	mustCall(t, runScoped(t, root, nil), "move_asset", map[string]any{"path": "files/Report", "targetDir": "elsewhere"})
	if got := readFile(t, root, "n.md"); got != body {
		t.Fatalf("a link to the note was rewritten: %q", got)
	}
}

func TestTaskNoteEditsKeepAWindowsNotesLineEndings(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"t.md":                 "---\r\ntags: [task]\r\nstatus: open\r\n---\r\n# T\r\n\r\nbody\r\n",
		"u.md":                 "---\r\ntags: [task]\r\nstatus: open\r\n---\r\n# U\r\n",
	})
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "update_task", map[string]any{"id": "t.md#task", "due": "2026-02-02"})
	if got := readFile(t, root, "t.md"); got != "---\r\ntags: [task]\r\nstatus: open\r\ndue: 2026-02-02\r\n---\r\n# T\r\n\r\nbody\r\n" {
		t.Fatalf("%q", got)
	}
	mustCall(t, cs, "toggle_task", map[string]any{"id": "u.md#task"})
	if got := readFile(t, root, "u.md"); strings.Count(got, "\n") != strings.Count(got, "\r\n") || !strings.Contains(got, "status: done") {
		t.Fatalf("%q", got)
	}
}

// What a refusal says must not depend on what is on the disk behind it.
func TestAMoveAnswersAloneDoNotDescribeHiddenPaths(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","systemFolderPaths":{"trash":"Bin"}}`,
		"Open/pic.png":         "png",
		"Journal/Diary.md":     "# Diary\n",
		"Bin/old.md":           "# old\n",
	})
	cs := runScoped(t, root, testScope(t, "write", []string{"Open"}, nil))
	want := "Error: This assistant can put files only inside: Open."
	for _, dir := range []string{"Journal/Diary.md/x", "Journal/Nope.md/x", "Journal", "Nope", "Bin", "trash", "Bin/x"} {
		got := mustFail(t, cs, "move_asset", map[string]any{"path": "Open/pic.png", "targetDir": dir})
		if got != want && got != strings.TrimPrefix(want, "Error: ") {
			t.Errorf("%q: %s", dir, got)
		}
	}
}

func TestMoveAssetKeepsARealFolderWhateverItsName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot hold these names")
	}
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json":    rootVault,
		"assets/a.png":            "png",
		"assets/b.png":            "png",
		"assets/c.png":            "png",
		"Clients/Acme Inc./x.txt": "x",
		"v1./x.txt":               "x",
		"Notes: 2026/x.txt":       "x",
	})
	cs := runScoped(t, root, nil)
	for file, dir := range map[string]string{"a.png": "Clients/Acme Inc.", "b.png": "v1.", "c.png": "Notes: 2026"} {
		mustCall(t, cs, "move_asset", map[string]any{"path": "assets/" + file, "targetDir": dir})
		if !exists(root, dir+"/"+file) {
			t.Errorf("%s did not reach %s", file, dir)
		}
	}
	// The same under a scope, once the path is known to be visible.
	scoped := runScoped(t, root, testScope(t, "write", []string{"Clients", "v1."}, nil))
	mustCall(t, scoped, "move_asset", map[string]any{"path": "Clients/Acme Inc./a.png", "targetDir": "v1."})
	if !exists(root, "v1./a.png") {
		t.Fatal("a real folder was refused under a scope")
	}
	if got := mustFail(t, scoped, "move_asset", map[string]any{"path": "v1./a.png", "targetDir": "v1./New."}); !strings.Contains(got, "can put files only inside") {
		t.Fatalf("a new one that will not hold everywhere: %s", got)
	}
}

func TestLinksInAFolderWhoseNameHoldsLinkSyntax(t *testing.T) {
	for _, folder := range []string{"Issue #5", "a?b", "a|b", "a[1]"} {
		t.Run(folder, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.ContainsAny(folder, "?|") {
				t.Skip("Windows cannot hold this name")
			}
			root := seedFiles(t, map[string]string{
				".zennotes/vault.json": rootVault,
				folder + "/pic.png":    "png",
				folder + "/n.md":       "![[pic.png]]\n",
			})
			mustCall(t, runScoped(t, root, testScope(t, "write", []string{folder}, nil)), "rename_asset", map[string]any{"path": folder + "/pic.png", "name": "new.png"})
			if got := readFile(t, root, folder+"/n.md"); got != "![[new.png]]\n" {
				t.Fatalf("under a scope a bare name stays bare here: %q", got)
			}
		})
	}
	t.Run("a move into one", func(t *testing.T) {
		root := seedFiles(t, map[string]string{
			".zennotes/vault.json": rootVault,
			"assets/pic.png":       "png",
			"assets/other.png":     "png",
			"elsewhere/pic.png":    "same name",
			"wiki.md":              "![[assets/pic.png]]\n",
			"plain.md":             "[f](assets/pic.png)\n",
			"old.md":               "![[assets/other.png]]\n",
		})
		cs := runScoped(t, root, nil)
		var got struct {
			Path            string
			LinksNotUpdated []string
		}
		if err := json.Unmarshal([]byte(mustCall(t, cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": "Issue #5"})), &got); err != nil {
			t.Fatal(err)
		}
		if got.Path != "Issue #5/pic.png" {
			t.Fatalf("%+v", got)
		}
		assets := []vault.AssetMeta{{Path: got.Path, Name: "pic.png"}}
		if plain := readFile(t, root, "plain.md"); !vault.AssetsReferencedBy(plain, assets, nil, "plain.md")[got.Path] {
			t.Fatalf("the Markdown link does not resolve: %q", plain)
		}
		// Another file has this name, so a bare name would not find it, and a full
		// path would read as a heading.
		if len(got.LinksNotUpdated) != 1 || got.LinksNotUpdated[0] != "wiki.md" || readFile(t, root, "wiki.md") != "![[assets/pic.png]]\n" {
			t.Fatalf("%+v %q", got, readFile(t, root, "wiki.md"))
		}
		// With the name unique, the bare name does.
		if err := json.Unmarshal([]byte(mustCall(t, cs, "move_asset", map[string]any{"path": "assets/other.png", "targetDir": "Issue #5"})), &got); err != nil {
			t.Fatal(err)
		}
		if body := readFile(t, root, "old.md"); body != "![[other.png]]\n" {
			t.Fatalf("%q", body)
		}
	})
}

func TestAFolderListedTwiceIsStillOneFolder(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"Projects/pic.png":     "png",
		"Projects/Plan.md":     "# Plan\n",
		"Secret/x.md":          "# x\n",
		"Other/move.png":       "png",
		"n.md":                 "![[Other/move.png]]\n",
	})
	var got struct{ Path string }
	if err := json.Unmarshal([]byte(mustCall(t, runScoped(t, root, testScope(t, "write", nil, []string{"Secret"})), "move_asset", map[string]any{"path": "Other/move.png", "targetDir": "projects"})), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "Projects/move.png" {
		t.Fatalf("%+v", got)
	}
	if body := readFile(t, root, "n.md"); body != "![[Projects/move.png]]\n" {
		t.Fatalf("%q", body)
	}
}

func TestRestoreAssetUndoesANumberThatLandedOnADenyEntry(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"assets/cafe.png":      "taken",
		"assets/Café 2.png":    "denied",
		".zennotes/deleted-assets/11111111-1111-4111-8111-111111111111/.zn-deleted.json": `{"path":"assets/cafe.png","name":"cafe.png","deletedAt":"2026-01-01T00:00:00.000Z"}`,
		".zennotes/deleted-assets/11111111-1111-4111-8111-111111111111/cafe.png":         "old",
	})
	cs := runScoped(t, root, testScope(t, "write", nil, []string{"assets/Café 2.png"}))
	if got := mustFail(t, cs, "restore_asset", map[string]any{"undoToken": "11111111-1111-4111-8111-111111111111"}); !strings.Contains(got, "Deleted asset not found") {
		t.Fatalf("%s", got)
	}
	if exists(root, "assets/cafe 2.png") {
		t.Fatal("the restored file stayed")
	}
}

func TestADatabasePairIsFoundInAnyCase(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json":  rootVault,
		"Lists/T.csv":           "a\n",
		"Lists/t.csv.base.json": "{}",
	})
	if got := mustFail(t, runScoped(t, root, nil), "delete_asset", map[string]any{"path": "Lists/T.csv"}); !strings.Contains(got, "belongs to a ZenNotes database") {
		t.Fatalf("%s", got)
	}
}

// Whether a hidden note has that title must not show in what the client's note
// ends up saying.
func TestABareLinkDoesNotShowAHiddenNotesTitle(t *testing.T) {
	run := func(extra map[string]string) string {
		files := map[string]string{
			".zennotes/vault.json": rootVault,
			"Shared/Report":        "a file",
			"Shared/n.md":          "![[Report]]\n",
			"Hidden/keep.md":       "# keep\n",
		}
		for k, v := range extra {
			files[k] = v
		}
		root := seedFiles(t, files)
		mustCall(t, runScoped(t, root, testScope(t, "write", []string{"Shared"}, nil)), "rename_asset", map[string]any{"path": "Shared/Report", "name": "Summary"})
		return readFile(t, root, "Shared/n.md")
	}
	without, with := run(nil), run(map[string]string{"Hidden/Report.md": "# Report\n"})
	if without != with {
		t.Fatalf("%q vs %q", without, with)
	}
}

func TestDoneOnAFinishedTaskNoteKeepsItsDate(t *testing.T) {
	body := "---\ntags: [task]\nstatus: done\ncompletedDate: 2020-01-02\n---\n# T\n"
	root := seedFiles(t, map[string]string{".zennotes/vault.json": rootVault, "t.md": body})
	mustCall(t, runScoped(t, root, nil), "update_task", map[string]any{"id": "t.md#task", "state": "done"})
	if got := readFile(t, root, "t.md"); got != body {
		t.Fatalf("%q", got)
	}
}

func TestToggleKeepsAWindowsNotesLineEndings(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"plain.md":             "# T\r\n- [ ] one\r\n- [ ] two\r\n",
		"forwarded.md":         "- [>] moved\r\n- [ ] two\r\n",
	})
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "toggle_task", map[string]any{"id": "plain.md#1"})
	if got := readFile(t, root, "plain.md"); got != "# T\r\n- [ ] one\r\n- [x] two\r\n" {
		t.Fatalf("%q", got)
	}
	mustCall(t, cs, "toggle_task", map[string]any{"id": "forwarded.md#0"})
	if got := readFile(t, root, "forwarded.md"); got != "- [>] moved\r\n- [ ] two\r\n" {
		t.Fatalf("%q", got)
	}
}

func TestAMoveThroughAFolderLinkIsRefused(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","systemFolderPaths":{"trash":"Bin"}}`,
		"assets/pic.png":       "png",
		"Bin/old.md":           "# old\n",
	})
	linkFolder(t, filepath.Join(root, "assets"), filepath.Join(root, "alias"))
	linkFolder(t, filepath.Join(root, "Bin"), filepath.Join(root, "bin2"))
	cs := runScoped(t, root, nil)
	for _, dir := range []string{"alias", "bin2", "bin2/x"} {
		if got := mustFail(t, cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": dir}); !strings.Contains(got, "cannot put files there") {
			t.Errorf("%q: %s", dir, got)
		}
	}
	if !exists(root, "assets/pic.png") {
		t.Fatal("the file moved")
	}
}

// Under an allow list a spelling that is not resolved is a folder outside it.
func TestAVariantOfAFolderHoldingAnAssetIsStillThatFolder(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": rootVault,
		"Projects/pic.png":     "png",
		"Projects/Plan.md":     "# Plan\n",
		"Other/move.png":       "png",
	})
	var got struct{ Path string }
	if err := json.Unmarshal([]byte(mustCall(t, runScoped(t, root, testScope(t, "write", []string{"Projects", "Other"}, nil)), "move_asset", map[string]any{"path": "Other/move.png", "targetDir": "projects"})), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "Projects/move.png" {
		t.Fatalf("%+v", got)
	}
}

// A notice names what it found in the vault, and may land on a terminal.
func TestScopeNoticeCleansAName(t *testing.T) {
	var got string
	sb := newScopedBackend(nil, Scope{})
	sb.logf = func(format string, args ...any) { got = fmt.Sprintf(format, args...) }
	sb.notice("the folder %s is hidden", "a\x1b[31mb")
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "a␛[31mb") {
		t.Errorf("logged %q", got)
	}
}
