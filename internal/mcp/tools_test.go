package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
)

// The desktop MCP server's tool names; the Go server must offer them all.
var desktopTools = []string{
	"add_comment", "append_to_note", "archive_note", "backlinks", "create_folder", "create_note", "delete_folder", "delete_note",
	"duplicate_note", "empty_trash", "insert_at_line", "list_assets", "list_comments", "list_folders", "list_notes", "list_tags",
	"list_tasks", "move_note", "move_to_trash", "prepend_to_note", "read_note", "rename_folder", "rename_note", "replace_in_note",
	"reply_to_comment", "resolve_comment", "restore_from_trash", "search_by_tag", "search_by_title", "search_text", "toggle_task",
	"unarchive_note", "vault_info", "write_note",
}

func TestToolsMatchTheDesktopServer(t *testing.T) {
	have := map[string]bool{}
	for _, tool := range tools() {
		have[tool.name] = true
	}
	for _, name := range desktopTools {
		if !have[name] {
			t.Errorf("missing tool %s", name)
		}
	}
}

// A repeated key in a schema is valid JSON, and some clients refuse it.
func TestToolSchemasRepeatNoKey(t *testing.T) {
	for _, def := range tools() {
		decoder := json.NewDecoder(strings.NewReader(def.schema))
		var walk func(string)
		walk = func(where string) {
			token, err := decoder.Token()
			if err != nil {
				t.Fatalf("%s: %v", def.name, err)
			}
			delim, isDelim := token.(json.Delim)
			if !isDelim {
				return
			}
			seen := map[string]bool{}
			for decoder.More() {
				if delim == '{' {
					key, err := decoder.Token()
					if err != nil {
						t.Fatalf("%s: %v", def.name, err)
					}
					if seen[key.(string)] {
						t.Errorf("%s repeats %q at %s", def.name, key, where)
					}
					seen[key.(string)] = true
					walk(where + "." + key.(string))
				} else {
					walk(where + "[]")
				}
			}
			if _, err := decoder.Token(); err != nil {
				t.Fatalf("%s: %v", def.name, err)
			}
		}
		walk(def.name)
	}
}

// The size is what decides whether an answer goes out, so it must not come in under the
// real thing, and it is what an ordinary answer should be charged.
func TestEncodedStringSize(t *testing.T) {
	for _, text := range []string{
		"plain words", `"quoted"`, `back\\slash`, "line\nbreak\r\ttab", "<b>&amp;</b>", "\x01\x1f",
		"caf\u00e9 \u6f22\u5b57 \U0001f600", "\u2028\u2029", "",
	} {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(text); err != nil {
			t.Fatal(err)
		}
		// The quotes around it and the newline after it do not count.
		real := int64(buf.Len() - 3)
		if got := encodedStringSize(text); got != real {
			t.Errorf("%q: %d, want %d", text, got, real)
		}
	}
	// An encoder may write a bad byte as a character or an escape. The larger is counted.
	if got := encodedStringSize("bad\xff"); got != 9 {
		t.Errorf("a bad byte: %d", got)
	}
}

// toggle_task rolls a repeating task the way `zn task toggle` does: the open
// copy for the next date goes above the ticked line.
func TestToggleTaskToolInsertsNextOccurrence(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "plants.md")
	note := "intro\n- [ ] Water plants due:2026-11-02 @repeat:mon/thu\noutro\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/plants.md#0"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	want := "intro\n- [ ] Water plants due:2026-11-05 @repeat:mon/thu\n" +
		"- [x] Water plants due:2026-11-02\noutro\n"
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("note (%v):\n%s\nwant:\n%s", err, got, want)
	}
}

// toggle_task ticks a repeating task on its last date and adds no copy.
func TestToggleTaskToolStopsAtUntil(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "plants.md")
	note := "- [ ] Water plants due:2026-11-03 @repeat:1d @until:2026-11-03\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/plants.md#0"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	want := "- [x] Water plants due:2026-11-03 @until:2026-11-03\n"
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("note (%v):\n%s\nwant:\n%s", err, got, want)
	}
}

// toggle_task takes a block id in place of an index.
func TestToggleTaskToolAcceptsBlockId(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "Work.md")
	note := "- [ ] One\n- [ ] Two\n- [ ] Write report due:2026-11-06 ^ab12cd\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runServerWith(t, backend.Options{}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/Work.md#^ab12cd"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	want := "- [ ] One\n- [ ] Two\n- [x] Write report due:2026-11-06 ^ab12cd\n"
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("note (%v):\n%s\nwant:\n%s", err, got, want)
	}
}

// toggle_task passes over the days of the event note that a skip name stands
// for.
func TestToggleTaskToolResolvesNamedSkip(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	event := "---\npage-type: event\nwhen: 2027-03-13/2027-03-21\n---\n"
	if err := os.WriteFile(filepath.Join(root, "inbox", "Spring Break.md"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "work.md")
	note := "- [ ] Assignment due:2027-03-08T23:59 @repeat:1w @skip:spring-break\n"
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2027, 3, 8, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/work.md#0"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	want := "- [ ] Assignment due:2027-03-22T23:59 @repeat:1w @skip:spring-break\n" +
		"- [x] Assignment due:2027-03-08T23:59 @skip:spring-break\n"
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("note (%v):\n%s\nwant:\n%s", err, got, want)
	}
}

// writeNotes writes notes, given by vault path, below root.
func writeNotes(t *testing.T, root string, notes map[string]string) {
	t.Helper()
	for rel, body := range notes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readNote(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var madeIDRe = regexp.MustCompile(`\^[a-z0-9]{6}\b`)

// withoutNewID replaces the block id that a roll made with {ID}. The id
// ab12cd is the one the tests write.
func withoutNewID(text string) string {
	return madeIDRe.ReplaceAllStringFunc(text, func(id string) string {
		if id == "^ab12cd" {
			return id
		}
		return "^{ID}"
	})
}

// toggle_task copies the work sessions of a repeating task, and its result is
// the toggled task as before.
func TestToggleTaskToolCopiesWorkSessions(t *testing.T) {
	root := t.TempDir()
	writeNotes(t, root, map[string]string{
		"inbox/Work.md":  "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n",
		"quick/Plans.md": "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n",
	})
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/Work.md#^ab12cd"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	if strings.Contains(strings.ToLower(text), "warning") || !strings.Contains(text, `"checked": true`) {
		t.Fatalf("result is not the toggled task: %s", text)
	}
	wantWork := "intro\n- [ ] Water plants @repeat:mon/thu ^{ID}\n- [x] Water plants ^ab12cd\noutro\n"
	if got := withoutNewID(readNote(t, root, "inbox/Work.md")); got != wantWork {
		t.Fatalf("Work.md:\n%s\nwant:\n%s", got, wantWork)
	}
	wantPlans := "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n" +
		"- Water plants task:[[Work#^{ID}]] when:2026-11-05T09:00-09:15\n"
	if got := withoutNewID(readNote(t, root, "quick/Plans.md")); got != wantPlans {
		t.Fatalf("Plans.md:\n%s\nwant:\n%s", got, wantPlans)
	}
}

// A scoped client's roll writes session copies only into notes that the
// client may write. The other notes stay as they were, and the result is the
// toggled task, with no warning and no path.
func TestScopedToggleWritesOnlyNotesInScope(t *testing.T) {
	root := t.TempDir()
	session := "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"
	writeNotes(t, root, map[string]string{
		".zennotes/vault.json":     `{"primaryNotesLocation":"root"}`,
		"Projects/Work.md":         "- [ ] Water plants @repeat:mon/thu ^ab12cd\n",
		"Projects/Plans.md":        session,
		"Projects/Clients/Plan.md": session,
		"Journal/Plan.md":          session,
	})
	today := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	scope := testScope(t, "write", []string{"Projects"}, []string{"Projects/Clients"})
	cs := runScopedWith(t, root, scope, backend.Options{Now: func() time.Time { return today }})
	text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "Projects/Work.md#^ab12cd"})
	if isErr {
		t.Fatalf("toggle_task: %s", text)
	}
	for _, leak := range []string{"Clients", "Journal", "warning", "Warning"} {
		if strings.Contains(text, leak) {
			t.Fatalf("result mentions %q: %s", leak, text)
		}
	}
	wantWork := "- [ ] Water plants @repeat:mon/thu ^{ID}\n- [x] Water plants ^ab12cd\n"
	if got := withoutNewID(readNote(t, root, "Projects/Work.md")); got != wantWork {
		t.Fatalf("Work.md:\n%s", got)
	}
	wantPlans := session + "- Water plants task:[[Work#^{ID}]] when:2026-11-05T09:00-09:15\n"
	if got := withoutNewID(readNote(t, root, "Projects/Plans.md")); got != wantPlans {
		t.Fatalf("Plans.md:\n%s\nwant:\n%s", got, wantPlans)
	}
	for _, rel := range []string{"Projects/Clients/Plan.md", "Journal/Plan.md"} {
		if got := readNote(t, root, rel); got != session {
			t.Fatalf("%s changed:\n%s", rel, got)
		}
	}
}

func remoteSession(t *testing.T, f *remoteFixture) *sdk.ClientSession {
	t.Helper()
	return runServerWith(t, backend.Options{}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindRemote, BaseURL: f.URL, AuthToken: "token"}, nil
	})
}

// Against a server, toggle_task rolls the values that v0.17.1 rolls and ticks
// every other task without a copy or a write to another note. The server has
// no clock, so the next date of the rolled task counts from today.
func TestRemoteToggleTaskToolRollsAsReleased(t *testing.T) {
	const (
		head    = "inbox/Work.md\n# Work\n\n"
		water   = "- [ ] Water plants repeat:mon,thu skip:@Holiday until:2026-12-31 ^ab12cd\n"
		invoice = "- [ ] Send the invoice due:2026-11-12\n"
		taskA   = "- [ ] A due:2027-03-08T23:59 @repeat:weekly @until:2027-03-10 @skip:spring-break\n"
		taskB   = "- [ ] B due:2027-03-08 @repeat:monthly-15\n"
	)
	for _, tc := range []struct {
		name, id string
		want     func(next string) string
	}{
		{"a bare repeat token is a plain tick", "inbox/Work.md#0", func(string) string {
			return head + "- [x] Water plants repeat:mon,thu skip:@Holiday until:2026-12-31 ^ab12cd\n" + invoice + taskA + taskB
		}},
		{"a rule the desktop app reads rolls", "inbox/Work.md#2", func(next string) string {
			return head + water + invoice +
				"- [ ] A due:" + next + " @repeat:weekly @until:2027-03-10 @skip:spring-break\n" +
				"- [x] A due:2027-03-08T23:59 @until:2027-03-10 @skip:spring-break\n" + taskB
		}},
		{"a rule only zn reads is a plain tick", "inbox/Work.md#3", func(string) string {
			return head + water + invoice + taskA + "- [x] B due:2027-03-08 @repeat:monthly-15\n"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRemoteFixture(t)
			cs := remoteSession(t, f)
			before := time.Now()
			text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": tc.id})
			after := time.Now()
			if isErr {
				t.Fatalf("toggle_task: %s", text)
			}
			got := f.Writes()
			var wants []string
			for _, day := range []time.Time{before, after} {
				wants = append(wants, tc.want(day.AddDate(0, 0, 7).Format("2006-01-02")))
			}
			if len(got) != 1 || (got[0] != wants[0] && got[0] != wants[1]) {
				t.Fatalf("writes:\n%q\nwant:\n%q", got, wants[0])
			}
		})
	}
}

// Against a server, list_tasks returns the same text as zn v0.17.1, with the
// `scheduled` value of the server kept.
func TestListTasksAgainstRemoteMatchesReleased(t *testing.T) {
	f := newRemoteFixture(t)
	cs := remoteSession(t, f)
	for _, tc := range []struct {
		golden string
		args   map[string]any
	}{
		{"remote-list-tasks.json", nil},
		{"remote-list-tasks-all.json", map[string]any{"status": "all"}},
	} {
		want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
		if err != nil {
			t.Fatal(err)
		}
		text, isErr := callTool(t, cs, "list_tasks", tc.args)
		if isErr {
			t.Fatalf("list_tasks: %s", text)
		}
		if text != string(want) {
			t.Errorf("list_tasks %v differs from testdata/%s:\n%s", tc.args, tc.golden, text)
		}
	}
}

// toggle_task writes the same note as the shared table of check-off results,
// for every case that one toggle can reach. A case that expects null is a
// plain tick, and the note keeps its number of lines.
func TestToggleTaskToolRunsUpstreamCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "vault", "testdata", "task-recurrence.cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Name        string  `json:"name"`
			Body        string  `json:"body"`
			TaskIndex   int     `json:"taskIndex"`
			Today       string  `json:"today"`
			FallbackDue string  `json:"fallbackDue"`
			Skip        bool    `json:"skip"`
			Expected    *string `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, c := range table.Cases {
		if c.FallbackDue != "" || c.Skip {
			continue
		}
		ran++
		t.Run(c.Name, func(t *testing.T) {
			root := t.TempDir()
			writeNotes(t, root, map[string]string{"inbox/note.md": c.Body})
			day, err := time.ParseInLocation("2006-01-02", c.Today, time.Local)
			if err != nil {
				t.Fatal(err)
			}
			today := day.Add(12 * time.Hour)
			cs := runServerWith(t, backend.Options{Now: func() time.Time { return today }}, func() (backend.Target, error) {
				return backend.Target{Kind: backend.KindLocal, Root: root}, nil
			})
			text, isErr := callTool(t, cs, "toggle_task", map[string]any{"id": "inbox/note.md#" + strconv.Itoa(c.TaskIndex)})
			if isErr {
				t.Fatalf("toggle_task: %s", text)
			}
			got := readNote(t, root, "inbox/note.md")
			if c.Expected == nil {
				if strings.Count(got, "\n") != strings.Count(c.Body, "\n") {
					t.Fatalf("a line was inserted:\n%q", got)
				}
				return
			}
			if got != *c.Expected {
				t.Fatalf("got:\n%q\nwant:\n%q", got, *c.Expected)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no cases")
	}
}
