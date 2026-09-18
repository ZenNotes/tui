package cli

import (
	"reflect"
	"testing"
)

// A switch written before a positional used to swallow it: `zn open
// --new-window ~/notes` parsed as new-window="~/notes" with no path.
func TestParseSwitchesNeverTakeTheNextToken(t *testing.T) {
	args := Parse([]string{"open", "--new-window", "/home/user/notes"})
	if !reflect.DeepEqual(args.Positionals, []string{"open", "/home/user/notes"}) {
		t.Fatalf("positionals: %v", args.Positionals)
	}
	if !args.Bool("new-window") {
		t.Fatalf("new-window should read as true: %v", args.Flags)
	}

	for name := range valuelessFlags {
		before := Parse([]string{"--" + name, "inbox/a.md"})
		if !reflect.DeepEqual(before.Positionals, []string{"inbox/a.md"}) || !before.Bool(name) {
			t.Errorf("--%s before the positional: %v %v", name, before.Positionals, before.Flags)
		}
		after := Parse([]string{"inbox/a.md", "--" + name})
		if !reflect.DeepEqual(after.Positionals, []string{"inbox/a.md"}) || !after.Bool(name) {
			t.Errorf("--%s after the positional: %v %v", name, after.Positionals, after.Flags)
		}
	}
}

func TestParseKeepsValueFlagsAndExplicitAssignments(t *testing.T) {
	args := Parse([]string{"list", "--tag", "idea", "--limit", "5"})
	if !reflect.DeepEqual(args.Positionals, []string{"list"}) || args.Str("tag") != "idea" || args.Str("limit") != "5" {
		t.Fatalf("value flags: %v %v", args.Positionals, args.Flags)
	}
	// `--flag=value` still reaches a switch.
	if Parse([]string{"--json=false"}).Bool("json") {
		t.Fatal("--json=false should read as false")
	}
	short := Parse([]string{"open", "-n", "/home/user/notes"})
	if !reflect.DeepEqual(short.Positionals, []string{"open", "/home/user/notes"}) || !short.Bool("n") {
		t.Fatalf("-n: %v %v", short.Positionals, short.Flags)
	}
}

func TestOpenLaunchArgsAndMessage(t *testing.T) {
	paths := []string{"/home/user/notes", "/home/user/todo.md"}
	if got := launchArgs(false, paths); !reflect.DeepEqual(got, paths) {
		t.Fatalf("without -n: %v", got)
	}
	if got := launchArgs(true, paths); !reflect.DeepEqual(got, []string{"--new-window", "/home/user/notes", "/home/user/todo.md"}) {
		t.Fatalf("with -n: %v", got)
	}

	folder := []openTarget{{abs: "/home/user/notes", isDirectory: true}}
	if got := openMessage(folder, true); got != "Opening folder /home/user/notes in a new ZenNotes window" {
		t.Fatalf("folder, -n: %q", got)
	}
	if got := openMessage(folder, false); got != "Opening folder /home/user/notes in ZenNotes" {
		t.Fatalf("folder: %q", got)
	}
	two := []openTarget{{abs: "/a.md"}, {abs: "/b.md"}}
	if got := openMessage(two, true); got != "Opening 2 items in a new ZenNotes window" {
		t.Fatalf("two, -n: %q", got)
	}
}
