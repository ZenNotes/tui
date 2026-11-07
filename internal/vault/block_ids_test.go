package vault

import (
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func blockIDTasks(body string, dialect TaskDialect) []Task {
	return ParseTasks("n.md", "n", FolderInbox, body, ParseTasksOptions{IncludeExcluded: true, Dialect: dialect})
}

func taskJSONLines(t *testing.T, tasks []Task) []string {
	t.Helper()
	out := []string{}
	for _, task := range tasks {
		b, err := task.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

func TestParseTasksReadsTrailingBlockIdAndDropsItFromContent(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := blockIDTasks("- [ ] Write report due:2026-11-06 ^ab12cd\n- [ ] Pay rent due:2026-11-03 ^rent  \n- [ ] Plain\n", dialect)
		if len(got) != 3 {
			t.Fatalf("got %+v", got)
		}
		if got[0].BlockID != "ab12cd" || got[0].Content != "Write report" {
			t.Errorf("first: BlockID %q, Content %q", got[0].BlockID, got[0].Content)
		}
		if got[1].BlockID != "rent" || got[1].Content != "Pay rent" {
			t.Errorf("second: BlockID %q, Content %q", got[1].BlockID, got[1].Content)
		}
		if got[2].BlockID != "" {
			t.Errorf("third: BlockID %q, want none", got[2].BlockID)
		}
		if got[0].RawText != "- [ ] Write report due:2026-11-06 ^ab12cd" {
			t.Errorf("RawText = %q, want the line as written", got[0].RawText)
		}
	}
	data := taskJSONLines(t, blockIDTasks("- [ ] A ^ab12cd\n- [ ] B\n", DialectApp))
	if !strings.Contains(data[0], `"blockId":"ab12cd"`) || strings.Contains(data[1], "blockId") {
		t.Errorf("JSON: %v", data)
	}
}

func TestBlockIdMustEndTheLine(t *testing.T) {
	got := blockIDTasks("- [ ] See ^mid then act due:2026-11-06\n- [ ] Cap^x\n", DialectApp)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].BlockID != "" || got[0].Content != "See ^mid then act" {
		t.Errorf("mid-line id: BlockID %q, Content %q", got[0].BlockID, got[0].Content)
	}
	if got[1].BlockID != "" || got[1].Content != "Cap^x" {
		t.Errorf("no space before: BlockID %q, Content %q", got[1].BlockID, got[1].Content)
	}
}

func TestNewBlockIdAvoidsIdsInUse(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-z0-9]{6}$`)
	first := NewBlockID(map[string]bool{}, rand.New(rand.NewSource(1)))
	if !pattern.MatchString(first) {
		t.Fatalf("id %q does not match the pattern", first)
	}
	taken := map[string]bool{first: true}
	second := NewBlockID(taken, rand.New(rand.NewSource(1)))
	if second == first || !pattern.MatchString(second) {
		t.Errorf("second id %q, want a different id in the pattern", second)
	}
}

func TestVaultBlockIdsListsEveryLineEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "- [ ] Task ^ab12cd\nA paragraph ^par-1\nNo id here\n^notrailing mid\n"
	if err := os.WriteFile(filepath.Join(root, "inbox", "n.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := v.VaultBlockIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !ids["ab12cd"] || !ids["par-1"] {
		t.Errorf("ids = %v", ids)
	}
}

func TestRollCopiesNeverCarryBlockIds(t *testing.T) {
	in := []string{
		"- [ ] T due:2026-11-02 repeat:1d ^keep",
		"  - [ ] Sub ^sub1",
		"  - [ ] Dated due:2026-11-02 ^sub2",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	if inserted != 3 {
		t.Fatalf("inserted = %d, want 3", inserted)
	}
	want := []string{
		"- [ ] T due:2026-11-03 repeat:1d",
		"  - [ ] Sub",
		"  - [ ] Dated due:2026-11-03",
		"- [x] T due:2026-11-02 repeat:1d ^keep",
		"  - [ ] Sub ^sub1",
		"  - [ ] Dated due:2026-11-02 ^sub2",
	}
	equalLines(t, got, want)

	// A task with no due date gets one on the copy, and the id still goes.
	got, _ = rollLines(t, []string{"- [ ] T repeat:1d ^keep"}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{"- [ ] T repeat:1d due:2026-11-03", "- [x] T repeat:1d ^keep"})
}

func TestToggleTaskAcceptsBlockId(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "Work.md")
	note := "- [ ] One\n- [ ] Two\n- [ ] Write report due:2026-11-06 ^ab12cd\n- [ ] Four\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.ToggleTask("inbox/Work.md#^ab12cd", DialectCLI)
	if err != nil || got == nil || !got.Checked || got.BlockID != "ab12cd" {
		t.Fatalf("toggle = %+v, err = %v", got, err)
	}
	raw, _ := os.ReadFile(path)
	if want := "- [ ] One\n- [ ] Two\n- [x] Write report due:2026-11-06 ^ab12cd\n- [ ] Four\n"; string(raw) != want {
		t.Errorf("note:\n%s\nwant:\n%s", raw, want)
	}

	none, err := v.ToggleTask("inbox/Work.md#^zzzzzz", DialectCLI)
	if err != nil || none != nil {
		t.Errorf("unknown id: %+v, %v, want nil and no error as an unknown index gives", none, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Errorf("unknown id changed the note:\n%s", after)
	}
}
