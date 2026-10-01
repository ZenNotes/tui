package cli

import (
	"reflect"
	"strconv"
	"strings"
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

// A body that opens with a frontmatter fence starts with `---`, which is
// not a flag; it must be taken as the value, while a real flag or the `--`
// terminator after a value flag still means the value is missing.
func TestValueFlagsAcceptValuesThatStartWithDashes(t *testing.T) {
	fm := "---\ntitle: x\n---\nbody"
	name, args, err := parseCommand([]string{"write", "a.md", "--body", fm})
	if err != nil || name != "write" || args.Str("body") != fm {
		t.Fatalf("frontmatter body: %q %v %v", name, args.Flags, err)
	}
	if _, args, err := parseCommand([]string{"capture", "--title", "---", "text"}); err != nil || args.Str("title") != "---" {
		t.Fatalf("bare --- as a value: %v %v", args.Flags, err)
	}
	if _, args, err := parseCommand([]string{"write", "a.md", "--body", "-x"}); err != nil || args.Str("body") != "-x" {
		t.Fatalf("single dash value: %v %v", args.Flags, err)
	}
	for _, argv := range [][]string{
		{"create", "--title", "--json"},
		{"create", "--title", "--", "x"},
		{"create", "--title", "-h"},
		{"create", "--title"},
	} {
		if _, _, err := parseCommand(argv); err == nil || !strings.Contains(err.Error(), "needs a value") {
			t.Errorf("%v: want a missing-value error, got %v", argv, err)
		}
	}
	if got := Parse([]string{"--body", fm}); got.Str("body") != fm {
		t.Fatalf("Parse: %v", got.Flags)
	}
}

func TestSuggestionsCountSwapsAsOneEdit(t *testing.T) {
	roots := commandRoots()
	for input, want := range map[string]string{"lsit": "list", "serach": "search", "craete": "create", "sevrer": "server", "lits": "list", "statsu": "status"} {
		if got := suggestion(input, roots); got != " Did you mean "+strconv.Quote(want)+"?" {
			t.Errorf("suggestion(%q) = %q, want %q", input, got, want)
		}
	}
	if got := suggestion("zzzzzz", roots); got != "" {
		t.Errorf("far-off input must not get a suggestion: %q", got)
	}
}

// `--tag` is the flag form of tag find's positional; it must satisfy the
// positional requirement the way --path does for note commands.
func TestTagFindAcceptsTheTagFlag(t *testing.T) {
	name, args, err := parseCommand([]string{"tag", "find", "--tag", "work", "--json"})
	if err != nil || name != "tag find" || args.Str("tag") != "work" {
		t.Fatalf("tag find --tag: %q %v %v", name, args.Flags, err)
	}
	if _, _, err := parseCommand([]string{"tag", "find"}); err == nil {
		t.Fatal("tag find without a tag must be a usage error")
	}
}
