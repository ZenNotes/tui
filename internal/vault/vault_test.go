package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"inbox", "quick", "archive", "trash"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestExtractTagsSkipsCodeAndReadsFrontmatter(t *testing.T) {
	body := "---\ntags: daily, work\n---\n# Title #heading-not-tag\n\nSome #idea and #проект text\n\n```c\n#include <x>\n```\n\n`#code`\n\n- nested\n    ```\n    #nested\n    ```\n"
	got := ExtractTags(body)
	want := []string{"daily", "work", "heading-not-tag", "idea", "проект"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
}

func TestExtractWikilinksAndExcerpt(t *testing.T) {
	body := "# Heading\n\nSee [[Other Note]] and [[Other Note|alias]] plus ![[image.png]] and [link](https://x).\n\n`[[not a link]]`"
	links := ExtractWikilinks(body)
	if !reflect.DeepEqual(links, []string{"Other Note", "image.png"}) {
		t.Fatalf("wikilinks = %v", links)
	}
	excerpt := BuildExcerpt(body)
	if excerpt != "Heading See Other Note and alias plus !image.png and link." {
		t.Fatalf("excerpt = %q", excerpt)
	}
}

func TestParseTasksInlineAndFile(t *testing.T) {
	body := "---\ntags: [task, home]\nstatus: in-progress\ndue: 2026-01-05\npriority: normal\n---\n# Note\n\n- [ ] Buy milk due:2026-02-01 !high #errand @waiting @project:alpha\n- [x] Done thing\n1. [/] Numbered started\n> - [-] quoted cancelled\n```\n- [ ] not a task\n```\n- [>] moved [[Elsewhere]]\n"
	tasks := ParseTasks("inbox/Note.md", "Note", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 6 {
		t.Fatalf("got %d tasks: %+v", len(tasks), tasks)
	}
	file := tasks[0]
	if file.Kind != "file" || file.ID != "inbox/Note.md#task" || !file.InProgress || file.Due != "2026-01-05" || file.Priority != "med" {
		t.Fatalf("file task = %+v", file)
	}
	if !reflect.DeepEqual(file.Tags, []string{"home"}) {
		t.Fatalf("file tags = %v", file.Tags)
	}
	first := tasks[1]
	if first.ID != "inbox/Note.md#0" || first.Due != "2026-02-01" || first.Priority != "high" || !first.Waiting {
		t.Fatalf("first = %+v", first)
	}
	if first.Content != "Buy milk #errand" || first.Fields["project"] != "alpha" || first.Fields["status"] != "in-progress" {
		t.Fatalf("first content/fields = %q %v", first.Content, first.Fields)
	}
	if !tasks[2].Checked || !tasks[3].InProgress || !tasks[4].Cancelled || !tasks[5].Forwarded {
		t.Fatalf("states wrong: %+v", tasks[2:])
	}
	if tasks[5].LineNumber != 15 {
		t.Fatalf("forwarded line = %d", tasks[5].LineNumber)
	}
}

func TestTasksModeGate(t *testing.T) {
	body := "---\ntasks: false\ntags: [task]\n---\n- [ ] hidden\n"
	if got := ParseTasks("a.md", "a", FolderInbox, body, ParseTasksOptions{}); len(got) != 0 {
		t.Fatalf("expected no tasks, got %+v", got)
	}
	if got := ParseTasks("a.md", "a", FolderInbox, body, ParseTasksOptions{IncludeExcluded: true}); len(got) != 2 {
		t.Fatalf("expected file + inline, got %+v", got)
	}
	noteOnly := "---\ntasks: note\ntags: [task]\n---\n- [ ] hidden\n"
	if got := ParseTasks("a.md", "a", FolderInbox, noteOnly, ParseTasksOptions{}); len(got) != 1 || got[0].Kind != "file" {
		t.Fatalf("expected only the file task, got %+v", got)
	}
}

func TestToggleTaskInBody(t *testing.T) {
	body := "- [ ] one\n- [/] two\n- [>] three\n- [x] four\n"
	next, ok := ToggleTaskInBody(body, 0, DialectApp)
	if !ok || !strings.HasPrefix(next, "- [x] one") {
		t.Fatalf("toggle open: %q", next)
	}
	next, _ = ToggleTaskInBody(body, 1, DialectApp)
	if !strings.Contains(next, "- [x] two") {
		t.Fatalf("in-progress should check off: %q", next)
	}
	next, _ = ToggleTaskInBody(body, 2, DialectApp)
	if next != body {
		t.Fatalf("forwarded must stay: %q", next)
	}
	next, _ = ToggleTaskInBody(body, 3, DialectApp)
	if !strings.Contains(next, "- [ ] four") {
		t.Fatalf("done should reopen: %q", next)
	}
	if _, ok := ToggleTaskInBody(body, 9, DialectApp); ok {
		t.Fatal("out of range must report not found")
	}
	if got := SetTaskChecked("- [/] a", 0, false); got != "- [/] a" {
		t.Fatalf("unchecking in-progress keeps slash: %q", got)
	}
	if got := SetTaskDue("- [ ] a due:2026-01-01 !low", 0, "2026-03-03"); got != "- [ ] a !low due:2026-03-03" {
		t.Fatalf("set due: %q", got)
	}
	if got := SetTaskPriority("- [ ] a !low", 0, ""); got != "- [ ] a" {
		t.Fatalf("clear priority: %q", got)
	}
	if got := SetTaskWaiting("- [ ] a @waiting", 0, false); got != "- [ ] a" {
		t.Fatalf("clear waiting: %q", got)
	}
}

func TestForwardTaskSubtree(t *testing.T) {
	body := "- [ ] parent\n  - [ ] child\n  - [x] done child\n\n  loose para\n- [ ] sibling\n"
	next, children := ForwardTaskSubtree(body, 0, "[[Target]]")
	if !strings.HasPrefix(next, "- [>] parent [[Target]]\n  - [>] child\n  - [x] done child") {
		t.Fatalf("forwarded body = %q", next)
	}
	if !reflect.DeepEqual(children, []string{"  - [ ] child", "  - [x] done child", "", "  loose para"}) {
		t.Fatalf("children = %q", children)
	}
	if !strings.HasSuffix(next, "- [ ] sibling\n") {
		t.Fatalf("sibling touched: %q", next)
	}
}

func TestForwardTaskLinesCarriesTheTaskItself(t *testing.T) {
	next, moved := ForwardTaskLines("- [ ] alone due:2026-09-10\n- [ ] other\n", 0, "[[Today]]")
	if !strings.HasPrefix(next, "- [>] alone due:2026-09-10 [[Today]]\n") {
		t.Fatalf("body = %q", next)
	}
	if !reflect.DeepEqual(moved, []string{"- [ ] alone due:2026-09-10"}) {
		t.Fatalf("a childless task moves as itself: %q", moved)
	}
	_, moved = ForwardTaskLines("  - [/] parent\n    - [ ] child\n", 0, "[[Today]]")
	if !reflect.DeepEqual(moved, []string{"- [/] parent", "  - [ ] child"}) {
		t.Fatalf("parent loses its indent, children keep theirs: %q", moved)
	}
	if body, moved := ForwardTaskLines("- [x] done\n", 0, "[[Today]]"); moved != nil || body != "- [x] done\n" {
		t.Fatalf("a finished task is not forwarded: %q %q", body, moved)
	}
}

func TestInsertTasksUnderTasksHeading(t *testing.T) {
	body := "# Day\n\n## Tasks\n\n- [ ] a\n\n---\n\n## Notes\n"
	got := InsertTasksUnderTasksHeading(body, []string{"- [ ] b"})
	want := "# Day\n\n## Tasks\n\n- [ ] a\n- [ ] b\n\n---\n\n## Notes\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got := InsertTasksUnderTasksHeading("text\n", []string{"- [ ] b"}); got != "text\n- [ ] b\n" {
		t.Fatalf("append fallback: %q", got)
	}
}

func TestFileTaskToggleFrontmatter(t *testing.T) {
	body := "---\ntitle: X\nstatus: open\ntags:\n  - task\n---\nbody\n"
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.Local)
	done := ToggleFileTaskInBody(body, false, now)
	if done != "---\ntitle: X\nstatus: done\ntags:\n  - task\ncompletedDate: 2026-09-08\n---\nbody\n" {
		t.Fatalf("done = %q", done)
	}
	reopened := ToggleFileTaskInBody(done, true, now)
	if reopened != "---\ntitle: X\nstatus: open\ntags:\n  - task\n---\nbody\n" {
		t.Fatalf("reopened = %q", reopened)
	}
}

func TestBodyTransforms(t *testing.T) {
	if got := AppendToBody("a\n", "b"); got != "a\n\nb\n" {
		t.Fatalf("append: %q", got)
	}
	if got := AppendToBody("", "b"); got != "b\n" {
		t.Fatalf("append empty: %q", got)
	}
	if got := PrependToBody("---\nk: v\n---\nbody", "top"); got != "---\nk: v\n---\ntop\n\nbody" {
		t.Fatalf("prepend: %q", got)
	}
	if got := InsertAtLineInBody("a\nb", 1, "x\ny"); got != "a\nx\ny\nb" {
		t.Fatalf("insert: %q", got)
	}
	next, n, _ := ReplaceInBody("foo foo", "foo", "bar", false)
	if next != "bar foo" || n != 1 {
		t.Fatalf("replace first: %q %d", next, n)
	}
	next, n, _ = ReplaceInBody("foo foo", "foo", "bar", true)
	if next != "bar bar" || n != 2 {
		t.Fatalf("replace all: %q %d", next, n)
	}
}

func TestRetitleLeadingHeading(t *testing.T) {
	if got := RetitleLeadingHeading("---\na: b\n---\n\n# Old\ntext", "New"); got != "---\na: b\n---\n\n# New\ntext" {
		t.Fatalf("got %q", got)
	}
	if got := RetitleLeadingHeading("## Deep\n", "New"); got != "## Deep\n" {
		t.Fatalf("deeper heading must stay: %q", got)
	}
	if got := RetitleLeadingHeading("prose\n# Old", "New"); got != "prose\n# Old" {
		t.Fatalf("prose first must stay: %q", got)
	}
}

func TestDeepLink(t *testing.T) {
	got := BuildOpenNoteDeepLink("inbox/My Note (draft) #1.md")
	want := "zennotes://open?path=inbox/My%20Note%20%28draft%29%20%231.md"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestVaultLifecycle(t *testing.T) {
	v := newTestVault(t)
	body := "# Hello\n\nLinks to [[World]] #tag\n\n- [ ] task one\n"
	meta, err := v.CreateNote(FolderInbox, "Hello", "", &body)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Path != "inbox/Hello.md" || meta.Folder != FolderInbox || !reflect.DeepEqual(meta.Tags, []string{"tag"}) {
		t.Fatalf("meta = %+v", meta)
	}
	world := "# World\n"
	if _, err := v.CreateNote(FolderInbox, "World", "Sub", &world); err != nil {
		t.Fatal(err)
	}
	dup, err := v.CreateNote(FolderInbox, "Hello", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Path != "inbox/Hello 2.md" {
		t.Fatalf("dedupe = %s", dup.Path)
	}
	content, err := v.ReadNote("inbox/Hello 2.md")
	if err != nil || content.Body != "# Hello 2\n\n" {
		t.Fatalf("default body = %q err=%v", content.Body, err)
	}
	notes, _ := v.ListNotes()
	if len(notes) != 3 {
		t.Fatalf("list = %d", len(notes))
	}
	folders, _ := v.ListFolders()
	if !reflect.DeepEqual(folders, []FolderEntry{{FolderInbox, "Sub"}}) {
		t.Fatalf("folders = %+v", folders)
	}
	back, _ := v.Backlinks("inbox/Sub/World.md")
	if len(back) != 1 || back[0].Path != "inbox/Hello.md" {
		t.Fatalf("backlinks = %+v", back)
	}
	renamed, err := v.RenameNote("inbox/Sub/World.md", "Earth")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Path != "inbox/Sub/Earth.md" {
		t.Fatalf("renamed = %s", renamed.Path)
	}
	earth, _ := v.ReadNote("inbox/Sub/Earth.md")
	if !strings.HasPrefix(earth.Body, "# Earth\n") {
		t.Fatalf("heading not synced: %q", earth.Body)
	}
	hello, _ := v.ReadNote("inbox/Hello.md")
	if !strings.Contains(hello.Body, "[[Earth]]") {
		t.Fatalf("wikilink not rewritten: %q", hello.Body)
	}
	tasks, _ := v.ScanTasks(ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].ID != "inbox/Hello.md#0" {
		t.Fatalf("tasks = %+v", tasks)
	}
	toggled, err := v.ToggleTask("inbox/Hello.md#0", DialectApp)
	if err != nil || toggled == nil || !toggled.Checked {
		t.Fatalf("toggle = %+v err=%v", toggled, err)
	}
	archived, err := v.ArchiveNote("inbox/Sub/Earth.md")
	if err != nil || archived.Path != "archive/Sub/Earth.md" {
		t.Fatalf("archive = %+v err=%v", archived, err)
	}
	restored, err := v.UnarchiveNote("archive/Sub/Earth.md")
	if err != nil || restored.Path != "inbox/Sub/Earth.md" {
		t.Fatalf("unarchive = %+v err=%v", restored, err)
	}
	trashed, err := v.MoveToTrash("inbox/Hello 2.md")
	if err != nil || trashed.Path != "trash/Hello 2.md" || trashed.Folder != FolderTrash {
		t.Fatalf("trash = %+v err=%v", trashed, err)
	}
	matches, _ := v.SearchText("links", 10)
	if len(matches) != 1 || matches[0].LineNumber != 3 {
		t.Fatalf("search = %+v", matches)
	}
	if _, err := v.ReadNote("../outside.md"); err == nil {
		t.Fatal("escape must fail")
	}
	moved, err := v.MoveNote("inbox/Hello.md", FolderQuick, "Deep/Er")
	if err != nil || moved.Path != "quick/Deep/Er/Hello.md" {
		t.Fatalf("move = %+v err=%v", moved, err)
	}
	if err := v.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	notes, _ = v.ListNotes()
	if len(notes) != 2 {
		t.Fatalf("after empty trash = %d", len(notes))
	}
}

func TestRootPrimaryModeAndRemappedFolders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".zennotes", "vault.json"), `{"primaryNotesLocation":"root","systemFolderPaths":{"archive":"99 - Old"}}`)
	writeFile(t, filepath.Join(root, "Loose.md"), "# Loose\n")
	writeFile(t, filepath.Join(root, "Projects", "P.md"), "# P\n")
	writeFile(t, filepath.Join(root, "99 - Old", "Gone.md"), "# Gone\n")
	writeFile(t, filepath.Join(root, "archive", "NotArchive.md"), "# user folder now\n")
	writeFile(t, filepath.Join(root, "quick", "Q.md"), "# Q\n")
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if v.PrimaryNotesLocation() != PrimaryNotesRoot {
		t.Fatal("expected root mode")
	}
	notes, _ := v.ListNotes()
	byPath := map[string]NoteFolder{}
	for _, n := range notes {
		byPath[n.Path] = n.Folder
	}
	want := map[string]NoteFolder{
		"Loose.md":              FolderInbox,
		"Projects/P.md":         FolderInbox,
		"99 - Old/Gone.md":      FolderArchive,
		"archive/NotArchive.md": FolderInbox,
		"quick/Q.md":            FolderQuick,
	}
	if !reflect.DeepEqual(byPath, want) {
		t.Fatalf("classification = %v", byPath)
	}
	created, err := v.CreateNote(FolderInbox, "New", "", nil)
	if err != nil || created.Path != "New.md" {
		t.Fatalf("root create = %+v err=%v", created, err)
	}
	archived, err := v.ArchiveNote("Projects/P.md")
	if err != nil || archived.Path != "99 - Old/Projects/P.md" {
		t.Fatalf("archive in remapped = %+v err=%v", archived, err)
	}
}

func TestSystemFolderPathsValidation(t *testing.T) {
	got := NormalizeSystemFolderPaths(map[string]string{
		"inbox":   "archive",
		"quick":   "Fast",
		"archive": "assets",
		"trash":   "Fast",
	})
	if !reflect.DeepEqual(got, map[string]string{"trash": "Fast"}) {
		t.Fatalf("got %v", got)
	}
}

func TestGroupTasks(t *testing.T) {
	today := time.Date(2026, 9, 8, 12, 0, 0, 0, time.Local)
	tasks := []Task{
		{ID: "a#0", Due: "2026-09-01"},
		{ID: "a#1"},
		{ID: "a#2", Due: "2026-09-20"},
		{ID: "a#3", Waiting: true},
		{ID: "a#4", Checked: true},
		{ID: "a#5", Forwarded: true},
		{ID: "a#6", Cancelled: true},
		{ID: "a#7", Due: "2026-09-08", Priority: "high"},
	}
	g := GroupTasks(tasks, today)
	if len(g.Today) != 3 || g.OverdueCount != 1 || len(g.Upcoming) != 1 || len(g.Waiting) != 1 || len(g.Done) != 1 || len(g.Forwarded) != 1 || len(g.Cancelled) != 1 {
		t.Fatalf("groups = %+v", g)
	}
	if g.Today[0].ID != "a#7" {
		t.Fatalf("priority first: %+v", g.Today)
	}
}

func TestUpdateFrontmatterFieldsQuoting(t *testing.T) {
	got := UpdateFrontmatterFields("body\n", []FrontmatterUpdate{{Key: "Link", Value: StrPtr("[[Note]]")}, {Key: "Plain", Value: StrPtr("hello world")}})
	if got != "---\nLink: \"[[Note]]\"\nPlain: hello world\n---\nbody\n" {
		t.Fatalf("got %q", got)
	}
}

func TestOutline(t *testing.T) {
	body := "---\ntitle: x\n---\n# One\ntext\n```\n# not\n```\nTwo\n---\n### Three\n"
	items := Outline(body)
	if len(items) != 3 || items[0].Line != 4 || items[1].Level != 2 || items[1].Text != "Two" || items[2].Level != 3 {
		t.Fatalf("outline = %+v", items)
	}
}

func TestCLIDialect(t *testing.T) {
	body := "- [ ] one @project:x\n1. [ ] numbered\n> - [ ] quoted\n- [x] two\n"
	app := ParseTasks("a.md", "a", FolderInbox, body, ParseTasksOptions{})
	cli := ParseTasks("a.md", "a", FolderInbox, body, ParseTasksOptions{Dialect: DialectCLI})
	if len(app) != 4 || len(cli) != 2 {
		t.Fatalf("app=%d cli=%d", len(app), len(cli))
	}
	if app[0].Content != "one" || cli[0].Content != "one @project:x" || cli[0].Fields != nil {
		t.Fatalf("content app=%q cli=%q fields=%v", app[0].Content, cli[0].Content, cli[0].Fields)
	}
	if cli[1].ID != "a.md#1" || !cli[1].Checked {
		t.Fatalf("cli second = %+v", cli[1])
	}
	next, ok := ToggleTaskInBody(body, 1, DialectCLI)
	if !ok || !strings.Contains(next, "- [ ] two") {
		t.Fatalf("cli toggle #1 should reopen 'two': %q", next)
	}
}
