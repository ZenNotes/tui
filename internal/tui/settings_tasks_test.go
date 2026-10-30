package tui

import (
	"reflect"
	"strings"
	"testing"
)

// #962: the TUI writes the Tasks folder lists where every runtime reads them,
// under `tasks`, and keeps the other list as it is.
func TestSetTasksFolderList(t *testing.T) {
	raw := map[string]any{"tasks": map[string]any{"excludedFolders": []string{"inbox/Books"}}}
	setTasksFolderList(raw, "includedFolders", " Tasks , inbox/Work ,")
	want := map[string]any{"tasks": map[string]any{
		"excludedFolders": []string{"inbox/Books"},
		"includedFolders": []string{"Tasks", "inbox/Work"},
	}}
	if !reflect.DeepEqual(raw, want) {
		t.Fatalf("after adding Task folders: %#v", raw)
	}
	if _, stray := raw["tasksExcludedFolders"]; stray {
		t.Fatal("wrote the old top-level tasksExcludedFolders key")
	}

	setTasksFolderList(raw, "excludedFolders", "")
	setTasksFolderList(raw, "includedFolders", "  ")
	if _, ok := raw["tasks"]; ok {
		t.Fatalf("emptying both lists should drop tasks, got %#v", raw)
	}
}

func TestSettingsDisplayGroupsUserAndVaultByPrefix(t *testing.T) {
	v := &settingsView{settings: []settingRow{
		{"vim", "a", 1, false}, {"vim", "b", 2, false}, {"editor", "c", 3, false},
		{"dailyNotes", "enabled", true, true}, {"tasks", "includedFolders", "x", true},
	}}
	disp, cur := v.display(40)
	var got []string
	for _, d := range disp {
		got = append(got, strings.TrimSpace(d.text))
	}
	want := []string{"USER", "vim", "a = 1", "b = 2", "editor", "c = 3", "", "VAULT", "dailyNotes", "enabled = true", "tasks", "includedFolders = x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("layout = %q", got)
	}
	if cur != 2 || disp[cur].idx != 0 {
		t.Fatalf("cursor row = %d", cur)
	}
}
