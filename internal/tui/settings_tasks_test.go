package tui

import (
	"fmt"
	"reflect"
	"slices"
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
	rows := []settingRow{
		{"vim", "a", 1, false}, {"vim", "b", 2, false}, {"editor", "c", 3, false},
		{"dailyNotes", "enabled", true, true}, {"tasks", "includedFolders", "x", true},
	}
	v := &settingsView{settings: rows}
	for _, s := range rows {
		scope := "user"
		if s.vault {
			scope = "vault"
		}
		v.lines = append(v.lines, scope+" · "+s.section+"."+s.key+" = "+fmt.Sprint(s.value))
	}
	v.lines[1] += " (note)"
	disp, cur := v.display(40)
	var got []string
	for _, d := range disp {
		got = append(got, d.scope+strings.TrimSpace(d.text))
	}
	want := []string{"User", "vim", "a = 1", "b = 2 (note)", "editor", "c = 3", "Vault", "dailyNotes", "enabled = true", "tasks", "includedFolders = x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("layout = %q", got)
	}
	if cur != 2 || disp[cur].idx != 0 {
		t.Fatalf("cursor row = %d", cur)
	}
}

// Every row the settings page lists sits under the group it is saved in:
// config.toml rows under User, vault.json rows under Vault, each group once.
func TestSettingsRowsSitUnderTheScopeTheySaveTo(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	v := &settingsView{}
	v.refresh(a)
	disp, _ := v.display(80)
	scopes, scope := []string{}, ""
	vaultRows := 0
	for _, d := range disp {
		if d.scope != "" {
			scope = d.scope
			scopes = append(scopes, scope)
			continue
		}
		if d.idx < 0 {
			continue
		}
		s := v.settings[d.idx]
		if s.vault != (scope == "Vault") {
			t.Fatalf("%s.%s sits under %q", s.section, s.key, scope)
		}
		if s.vault {
			vaultRows++
		}
	}
	if strings.Join(scopes, ",") != "User,Vault" || vaultRows == 0 {
		t.Fatalf("groups = %v with %d vault rows", scopes, vaultRows)
	}
}

// Moving onto the first row of a group brings that group's headings on
// screen with it, from above and from below.
func TestSettingsScrollKeepsGroupHeadingsInView(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	v := &settingsView{}
	v.refresh(a)
	firstVault := slices.IndexFunc(v.settings, func(s settingRow) bool { return s.vault })
	if firstVault < 1 {
		t.Fatalf("no vault rows after user rows: %d", firstVault)
	}
	screen := func() []string { return strings.Split(stripAnsi(v.render(a, 70, 12, true)), "\n") }
	expectAbove := func(when string, lines []string, want ...string) {
		t.Helper()
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "enabled = ") && i >= len(want) {
				got := []string{}
				for _, g := range lines[i-len(want) : i] {
					got = append(got, strings.Trim(strings.TrimSpace(g), "─ "))
				}
				if strings.Join(got, "|") == strings.Join(want, "|") {
					return
				}
			}
		}
		t.Fatalf("%s: no %q above the dailyNotes row:\n%s", when, want, strings.Join(lines, "\n"))
	}

	if got := strings.Trim(strings.TrimSpace(screen()[2]), "─ "); got != "User · config.toml" {
		t.Fatalf("top heading = %q", got)
	}
	v.list.cursor = firstVault - 1
	screen()
	v.list.move(1, len(v.settings))
	expectAbove("down", screen(), "Vault · .zennotes/vault.json", "dailyNotes")

	v.list.cursor = len(v.settings) - 1
	screen()
	for v.list.cursor > firstVault {
		v.list.move(-1, len(v.settings))
		screen()
	}
	expectAbove("up", screen(), "Vault · .zennotes/vault.json", "dailyNotes")

	// A window tall enough for the whole page shows it from the top again.
	tall := strings.Split(stripAnsi(v.render(a, 70, 200, true)), "\n")
	if got := strings.Trim(strings.TrimSpace(tall[2]), "─ "); got != "User · config.toml" {
		t.Fatalf("after growing, top line = %q", got)
	}
}
