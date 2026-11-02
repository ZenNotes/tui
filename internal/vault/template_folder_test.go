package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The cases of shared-domain's template-folder.test.ts and the Go server's
// template_folder_test.go, with the answers the TypeScript
// normalizeTemplateFolder gives (null is "").
func TestNormalizeTemplateFolder(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Templates", "Templates"},
		{"inbox/Templates", "inbox/Templates"},
		{"  Meta / My Templates ", "Meta/My Templates"},
		{"/Templates/", "Templates"},
		{`Meta\Templates`, "Meta/Templates"},
		{"Meta//Templates", "Meta/Templates"},
		{"", ""},
		{"   ", ""},
		{"/", ""},
		{".", ""},
		{"..", ""},
		{"../Templates", ""},
		{"Templates/..", ""},
		{".zennotes/templates", ""},
		{"Meta/.hidden", ""},
		// JavaScript's trim(), not Go's TrimSpace: NBSP, BOM and the Unicode
		// separators go; NEL (U+0085) is not JavaScript whitespace and stays.
		{"\u00a0Templates\ufeff", "Templates"},
		{"\u0085Templates", "\u0085Templates"},
		{strings.Repeat("a", 512), strings.Repeat("a", 512)},
		{strings.Repeat("a", 513), ""},
		// Counted in UTF-16 code units: 256 astral characters are 512 units.
		{strings.Repeat("😀", 256), strings.Repeat("😀", 256)},
		{strings.Repeat("😀", 257), ""},
	}
	for _, tc := range cases {
		if got := NormalizeTemplateFolder(tc.in); got != tc.want {
			t.Errorf("NormalizeTemplateFolder(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsPathInTemplateFolder(t *testing.T) {
	cases := []struct {
		path, folder string
		want         bool
	}{
		{"Templates/Meeting.md", "Templates", true},
		{"Templates/Work/Standup.md", "Templates", true},
		{"Templates Old/Meeting.md", "Templates", false},
		{"TemplatesX.md", "Templates", false},
		{"inbox/Templates/Meeting.md", "Templates", false},
		{"inbox/Templates/Meeting.md", "inbox/Templates", true},
		{"templates/Meeting.md", "Templates", false},
		{"Templates/Meeting.md", "", false},
	}
	for _, tc := range cases {
		if got := IsPathInTemplateFolder(tc.path, tc.folder); got != tc.want {
			t.Errorf("IsPathInTemplateFolder(%q, %q) = %v, want %v", tc.path, tc.folder, got, tc.want)
		}
	}
}

func TestRewriteTemplateFolderForRename(t *testing.T) {
	cases := []struct {
		folder, oldRel, newRel, want string
	}{
		{"Templates", "Templates", "Blueprints", "Blueprints"},
		{"Meta/Templates", "Meta", "Archive/Meta", "Archive/Meta/Templates"},
		{"Templates", "Templates Old", "Old", "Templates"},
		{"Templates", "Templates/Work", "Templates/Jobs", "Templates"},
		{"Templates", "Templates", "Templates", "Templates"},
		{"Templates", "Templates", ".hidden", "Templates"},
		{"", "Templates", "X", ""},
	}
	for _, tc := range cases {
		if got := rewriteTemplateFolderForRename(tc.folder, tc.oldRel, tc.newRel); got != tc.want {
			t.Errorf("rewriteTemplateFolderForRename(%q, %q, %q) = %q, want %q", tc.folder, tc.oldRel, tc.newRel, got, tc.want)
		}
	}
}

// tasksFolderListsForSettings' cases: the template folder joins the excluded
// list once, wins inside a Task folder, and an unusable one adds nothing.
func TestTasksFolderListsAddTheTemplateFolder(t *testing.T) {
	lists := func(raw string) ([]string, []string) {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return ParseSettings(m).TasksFolderLists()
	}
	excluded, included := lists(`{"tasks":{"excludedFolders":["inbox/Books"]},"templates":{"folder":"Templates"}}`)
	if !reflect.DeepEqual(excluded, []string{"inbox/Books", "Templates"}) || len(included) != 0 {
		t.Fatalf("lists = %v / %v", excluded, included)
	}
	if !IsPathExcludedFromTasks("Templates/Work/1on1.md", excluded, included) {
		t.Error("a nested template note feeds Tasks")
	}

	excluded, included = lists(`{"tasks":{"includedFolders":["Work"]},"templates":{"folder":"Work/Templates"}}`)
	if IsPathExcludedFromTasks("Work/Plan.md", excluded, included) ||
		!IsPathExcludedFromTasks("Work/Templates/Plan.md", excluded, included) {
		t.Errorf("template folder inside a Task folder: %v / %v", excluded, included)
	}

	excluded, _ = lists(`{"tasks":{"excludedFolders":["Templates"]},"templates":{"folder":"Templates"}}`)
	if !reflect.DeepEqual(excluded, []string{"Templates"}) {
		t.Errorf("listed twice: %v", excluded)
	}
	excluded, _ = lists(`{"templates":{"folder":".zennotes/templates"}}`)
	if len(excluded) != 0 {
		t.Errorf("an unusable folder was excluded: %v", excluded)
	}

	// The settings' own list is not grown in place: a second call, and the
	// excluded list zn shows in Settings, stay as vault.json has them.
	s := ParseSettings(map[string]any{
		"tasks":     map[string]any{"excludedFolders": []any{"inbox/Books"}},
		"templates": map[string]any{"folder": "Templates"},
	})
	s.TasksFolderLists()
	if !reflect.DeepEqual(s.TasksExcludedFolders, []string{"inbox/Books"}) {
		t.Errorf("TasksFolderLists grew the settings' list: %v", s.TasksExcludedFolders)
	}
}

// What vault.json can hold under `templates`, read the way the TypeScript
// reads an `unknown`: anything it takes as absent is no folder, and never an
// error for the rest of the file.
func TestTemplateFolderSettingMatchesTypeScript(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"absent", `{}`, ""},
		{"folder", `{"templates":{"folder":"Templates"}}`, "Templates"},
		{"cleaned", `{"templates":{"folder":" /Templates/ "}}`, "Templates"},
		{"empty", `{"templates":{"folder":""}}`, ""},
		{"traversal", `{"templates":{"folder":"../x"}}`, ""},
		{"hidden", `{"templates":{"folder":".zennotes"}}`, ""},
		{"not a string", `{"templates":{"folder":42}}`, ""},
		{"not an object", `{"templates":"Templates"}`, ""},
		{"an array", `{"templates":["Templates"]}`, ""},
		{"null", `{"templates":null}`, ""},
		{"keys match exactly", `{"templates":{"Folder":"Templates"}}`, ""},
		{"a stray key", `{"templates":{"folder":"Templates","extra":1}}`, "Templates"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &raw); err != nil {
				t.Fatal(err)
			}
			if got := ParseSettings(raw).TemplateFolder; got != tc.want {
				t.Errorf("TemplateFolder = %q, want %q", got, tc.want)
			}
		})
	}
}

func openTemplateFolderVault(t *testing.T, settings string, files map[string]string) *Vault {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".zennotes", "vault.json"), settings)
	for rel, body := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func taskSources(tasks []Task) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, tk := range tasks {
		if !seen[tk.SourcePath] {
			seen[tk.SourcePath] = true
			out = append(out, tk.SourcePath)
		}
	}
	sort.Strings(out)
	return out
}

// #1024 end to end: the scan and the single-note rescan skip the template
// folder, IncludeExcluded reaches it again, and clearing the setting brings
// its tasks back.
func TestTemplateFolderStaysOutOfTasks(t *testing.T) {
	v := openTemplateFolderVault(t, `{"primaryNotesLocation":"root","templates":{"folder":"Templates"}}`, map[string]string{
		"Templates/Meeting.md":    "# {{title}}\n\n- [ ] agenda item\n",
		"Templates/Work/Retro.md": "- [ ] action item\n",
		"Templates Old/Keep.md":   "- [ ] still work\n",
		"Notes/Plan.md":           "- [ ] real work\n",
	})
	tasks, err := v.ScanTasks(ParseTasksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := taskSources(tasks), []string{"Notes/Plan.md", "Templates Old/Keep.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scan = %v, want %v", got, want)
	}
	for _, rel := range []string{"Templates/Meeting.md", "Templates/Work/Retro.md"} {
		one, err := v.ScanTasksForPath(rel, ParseTasksOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(one) != 0 {
			t.Errorf("rescan of %s = %d tasks, want none", rel, len(one))
		}
		forced, err := v.ScanTasksForPath(rel, ParseTasksOptions{IncludeExcluded: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(forced) != 1 {
			t.Errorf("IncludeExcluded rescan of %s = %d tasks, want 1", rel, len(forced))
		}
	}
	all, err := v.ScanTasks(ParseTasksOptions{IncludeExcluded: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskSources(all); len(got) != 4 {
		t.Fatalf("IncludeExcluded scan = %v, want all four notes", got)
	}

	if err := v.UpdateSettings(func(raw map[string]any) { delete(raw, "templates") }); err != nil {
		t.Fatal(err)
	}
	cleared, err := v.ScanTasks(ParseTasksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskSources(cleared); len(got) != 4 {
		t.Fatalf("scan after clearing the folder = %v, want all four notes", got)
	}
}

// The setting names the folder as it is on disk, so a remapped inbox needs
// no translation, and a template folder inside a Task folder still wins.
func TestTemplateFolderFollowsTheOnDiskPath(t *testing.T) {
	v := openTemplateFolderVault(t,
		`{"primaryNotesLocation":"inbox","systemFolderPaths":{"inbox":"Entry"},"tasks":{"includedFolders":["Entry"]},"templates":{"folder":"Entry/Templates"}}`,
		map[string]string{
			"Entry/Templates/Meeting.md": "- [ ] agenda item\n",
			"Entry/Plan.md":              "- [ ] real work\n",
		})
	tasks, err := v.ScanTasks(ParseTasksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := taskSources(tasks), []string{"Entry/Plan.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scan = %v, want %v", got, want)
	}
}

// A folder that does not exist, or one the rules refuse, leaves Tasks and
// search as they were.
func TestMissingOrUnusableTemplateFolderChangesNothing(t *testing.T) {
	for _, folder := range []string{`"Gone"`, `".zennotes/templates"`, `"../Notes"`, `42`} {
		v := openTemplateFolderVault(t, `{"primaryNotesLocation":"root","templates":{"folder":`+folder+`}}`, map[string]string{
			"Notes/Plan.md":     "- [ ] real work\n",
			"Templates/Meet.md": "- [ ] agenda item\n",
		})
		tasks, err := v.ScanTasks(ParseTasksOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := taskSources(tasks); len(got) != 2 {
			t.Errorf("folder %s: scan = %v, want both notes", folder, got)
		}
		matches, err := v.SearchText("item", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 {
			t.Errorf("folder %s: search = %v, want the template's line", folder, matches)
		}
	}
}

// Text search skips the folder before the limit, so the matches it returns
// are spent on real notes, and clearing the setting brings them back.
func TestSearchTextSkipsTheTemplateFolder(t *testing.T) {
	v := openTemplateFolderVault(t, `{"primaryNotesLocation":"root","templates":{"folder":"A Templates"}}`, map[string]string{
		"A Templates/Meeting.md":      "Agenda for the standup\n",
		"A Templates/Work/Standup.md": "standup notes go here\n",
		"B Notes/Monday.md":           "Monday standup went well\n",
	})
	matches, err := v.SearchText("standup", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Path != "B Notes/Monday.md" {
		t.Fatalf("search = %+v, want only B Notes/Monday.md", matches)
	}

	if err := v.UpdateSettings(func(raw map[string]any) { delete(raw, "templates") }); err != nil {
		t.Fatal(err)
	}
	matches, err = v.SearchText("standup", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("search after clearing the folder = %+v, want three matches", matches)
	}
}

// A folder rename or move repoints the setting, as desktop main, MCP and the
// server do, and keeps whatever else vault.json holds under `templates`.
func TestRenameFolderRepointsTheTemplateFolder(t *testing.T) {
	v := openTemplateFolderVault(t,
		`{"primaryNotesLocation":"inbox","favorites":["x"],"templates":{"folder":"inbox/Meta/Templates","later":true}}`,
		map[string]string{
			"inbox/Meta/Templates/Meeting.md": "- [ ] agenda item\n",
			"inbox/Plan.md":                   "- [ ] real work\n",
		})
	if _, err := v.RenameFolder(FolderInbox, "Meta", "Old/Meta"); err != nil {
		t.Fatal(err)
	}
	s := v.Settings()
	if s.TemplateFolder != "inbox/Old/Meta/Templates" {
		t.Fatalf("template folder after the move = %q", s.TemplateFolder)
	}
	templates, _ := s.Raw["templates"].(map[string]any)
	if templates["later"] != true || !reflect.DeepEqual(s.Favorites, []string{"x"}) {
		t.Errorf("the rewrite lost other vault.json keys: %v", s.Raw)
	}
	tasks, err := v.ScanTasks(ParseTasksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := taskSources(tasks), []string{"inbox/Plan.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scan after the move = %v, want %v", got, want)
	}

	if _, err := v.RenameFolder(FolderInbox, "Old/Meta/Templates", "Old/Meta/Blueprints"); err != nil {
		t.Fatal(err)
	}
	if got := v.Settings().TemplateFolder; got != "inbox/Old/Meta/Blueprints" {
		t.Fatalf("template folder after the rename = %q", got)
	}
}

// Renaming a folder the setting does not name leaves vault.json untouched.
func TestRenameOfAnotherFolderLeavesTheTemplateFolder(t *testing.T) {
	v := openTemplateFolderVault(t, `{"primaryNotesLocation":"root","templates":{"folder":"Templates"}}`, map[string]string{
		"Templates/Meeting.md":  "x\n",
		"Templates Old/Keep.md": "y\n",
	})
	before, err := os.ReadFile(v.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.RenameFolder(FolderInbox, "Templates Old", "Older"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(v.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("vault.json was rewritten:\n%s", after)
	}
}
