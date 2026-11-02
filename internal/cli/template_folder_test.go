package cli

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// templateFolderVault is a root-layout vault whose `Templates` folder is its
// template folder (ZenNotes/zennotes#1024), with a real note beside it.
func templateFolderVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeVaultFile(t, root, ".zennotes/vault.json", `{"primaryNotesLocation":"root","templates":{"folder":"Templates"}}`)
	writeVaultFile(t, root, "Templates/Meeting.md", "# {{title}}\n\n- [ ] Agenda item for the standup\n")
	writeVaultFile(t, root, "Templates/Work/Retro.md", "- [ ] Action item\n")
	writeVaultFile(t, root, "Notes/Monday.md", "- [ ] Book the room for the standup\n")
	return root
}

func runJSON(t *testing.T, args []string, into any) {
	t.Helper()
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	if err := json.Unmarshal([]byte(out), into); err != nil {
		t.Fatalf("zn %v: %v\n%s", args, err, out)
	}
}

// A template's checkboxes are placeholders, so `zn task list` leaves the
// template folder out, as the desktop's Tasks do; --include-excluded lists
// them again.
func TestTaskListLeavesTheTemplateFolderOut(t *testing.T) {
	isolatedCLI(t)
	root := templateFolderVault(t)
	paths := func(flags ...string) []string {
		var tasks []struct {
			Path string `json:"sourcePath"`
		}
		runJSON(t, append([]string{"task", "list", "--json", "--vault", root}, flags...), &tasks)
		out := []string{}
		for _, task := range tasks {
			out = append(out, task.Path)
		}
		slices.Sort(out)
		return out
	}
	if got := paths(); !reflect.DeepEqual(got, []string{"Notes/Monday.md"}) {
		t.Fatalf("default: %v", got)
	}
	if got := paths("--include-excluded"); !reflect.DeepEqual(got, []string{"Notes/Monday.md", "Templates/Meeting.md", "Templates/Work/Retro.md"}) {
		t.Fatalf("--include-excluded: %v", got)
	}
}

// `zn search` leaves template notes out, as the desktop's Search Text in
// Vault and the server's text search do.
func TestSearchLeavesTheTemplateFolderOut(t *testing.T) {
	isolatedCLI(t)
	root := templateFolderVault(t)
	var matches []struct {
		Path string `json:"path"`
	}
	runJSON(t, []string{"search", "standup", "--json", "--vault", root}, &matches)
	if len(matches) != 1 || matches[0].Path != "Notes/Monday.md" {
		t.Fatalf("search = %+v, want only Notes/Monday.md", matches)
	}
}

// `zn task watch` scans as `zn task list` does, so a template's checkbox
// changing is not a task changing.
func TestTaskWatchIgnoresTheTemplateFolder(t *testing.T) {
	root := templateFolderVault(t)
	output, changed, stop := watchTasks(t, root, true)
	writeVaultFile(t, root, "Templates/Meeting.md", "# {{title}}\n\n- [x] Agenda item for the standup\n")
	changed <- struct{}{}
	writeVaultFile(t, root, "Notes/Monday.md", "- [x] Book the room for the standup\n")
	changed <- struct{}{}
	waitFor(t, "the real note's change", func() bool { return strings.Contains(output.String(), "\n") })
	changed <- struct{}{}
	stop()
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"path":"Notes/Monday.md"`) {
		t.Fatalf("want one line for Notes/Monday.md, got:\n%s", output.String())
	}
}

// templateTagsVault has a template carrying a tag of its own and a tag it
// shares with a real note.
func templateTagsVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeVaultFile(t, root, ".zennotes/vault.json", `{"primaryNotesLocation":"root","templates":{"folder":"Templates"}}`)
	writeVaultFile(t, root, "Templates/Meeting.md", "---\ntags: [work, tpl-only]\n---\n# {{title}}\n")
	writeVaultFile(t, root, "Notes/Monday.md", "# Monday\n\nPlanning #work\n")
	return root
}

// A note in the template folder contributes no tags: `zn tag list` leaves its
// tags out of names and counts, and `zn tag find` and `zn list --tag` do not
// return it, as the desktop 2.80.0 tag list and tag search do. `zn list`
// without a tag still lists it, since it is an ordinary note otherwise.
func TestTagsLeaveTheTemplateFolderOut(t *testing.T) {
	isolatedCLI(t)
	root := templateTagsVault(t)
	var tags []struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	runJSON(t, []string{"tag", "list", "--json", "--vault", root}, &tags)
	if len(tags) != 1 || tags[0].Tag != "work" || tags[0].Count != 1 {
		t.Fatalf("tag list = %+v, want only work with 1 note", tags)
	}
	notePaths := func(args ...string) []string {
		var notes []struct {
			Path string `json:"path"`
		}
		runJSON(t, append(args, "--json", "--vault", root), &notes)
		out := []string{}
		for _, n := range notes {
			out = append(out, n.Path)
		}
		slices.Sort(out)
		return out
	}
	if got := notePaths("tag", "find", "work"); !reflect.DeepEqual(got, []string{"Notes/Monday.md"}) {
		t.Fatalf("tag find work = %v", got)
	}
	if got := notePaths("tag", "find", "tpl-only"); len(got) != 0 {
		t.Fatalf("tag find tpl-only = %v, want none", got)
	}
	if got := notePaths("list", "--tag", "work"); !reflect.DeepEqual(got, []string{"Notes/Monday.md"}) {
		t.Fatalf("list --tag work = %v", got)
	}
	if got := notePaths("list"); !reflect.DeepEqual(got, []string{"Notes/Monday.md", "Templates/Meeting.md"}) {
		t.Fatalf("list = %v, want both notes", got)
	}
}
