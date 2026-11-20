package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

// assetOnlyBackend hides Local's link rewriter, the shape of a server's backend.
type assetOnlyBackend struct {
	Backend
}

func (a *assetOnlyBackend) ImportAsset(ctx context.Context, note, name string, r io.Reader) (vault.ImportedAsset, error) {
	return a.Backend.(backend.AssetManager).ImportAsset(ctx, note, name, r)
}

func mustLocalBackend(t *testing.T) Backend {
	t.Helper()
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"assets/pic.png":       "png",
	})
	b, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDailyNoteStartsFromTheVaultsTemplate(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json":       `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"Days","titlePattern":"yyyy-MM-dd","templateId":"custom:day"}}`,
		".zennotes/templates/day.md": "---\nname: Day\n---\n# {{title}}\n\n## Log\n",
		"Days/2026-03-04.md":         "# old\n",
		"Elsewhere/Plan.md":          "# Plan\n",
	})
	full := runScoped(t, root, nil)
	var result struct {
		Exists     bool
		Enabled    bool
		Path       string
		CreateWith map[string]any
	}
	if err := json.Unmarshal([]byte(mustCall(t, full, "daily_note", map[string]any{"date": "2026-03-05"})), &result); err != nil {
		t.Fatal(err)
	}
	if result.Exists || !result.Enabled || result.CreateWith["subpath"] != "Days" || result.CreateWith["title"] != "2026-03-05" {
		t.Fatalf("%+v", result)
	}
	if body, _ := result.CreateWith["body"].(string); !strings.Contains(body, "# 2026-03-05") || !strings.Contains(body, "## Log") {
		t.Fatalf("the template is not applied: %q", body)
	}
	found := mustCall(t, full, "daily_note", map[string]any{"date": "2026-03-04"})
	if !strings.Contains(found, `"exists": true`) || !strings.Contains(found, `"path": "Days/2026-03-04.md"`) {
		t.Fatalf("an existing note: %s", found)
	}

	// A client that sees part of the vault gets no template, and a hidden daily
	// note reads as absent, in the same shape.
	narrow := runScoped(t, root, testScope(t, "read", []string{"Elsewhere"}, nil))
	text := mustCall(t, narrow, "daily_note", map[string]any{"date": "2026-03-04"})
	if !strings.Contains(text, `"exists": false`) || strings.Contains(text, "## Log") {
		t.Fatalf("a path scope saw the template or a hidden note: %s", text)
	}

	// A read-only whole-vault client changes nothing and still gets the template.
	read := runScoped(t, root, testScope(t, "read", nil, nil))
	if text := mustCall(t, read, "daily_note", map[string]any{"date": "2026-03-05"}); !strings.Contains(text, "## Log") {
		t.Fatalf("read access: %s", text)
	}
	if _, err := os.Stat(filepath.Join(root, "Days", "2026-03-05.md")); err == nil {
		t.Fatal("daily_note created a note")
	}
}

func TestDailyNoteFindsANoteUnderAnOlderPattern(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"New","titlePattern":"yyyy-MM-dd","legacyPatterns":[{"directory":"Old","titlePattern":"yyyy.MM.dd"}]}}`,
		"Old/2026.03.04.md":    "# old pattern\n",
	})
	text := mustCall(t, runScoped(t, root, nil), "daily_note", map[string]any{"date": "2026-03-04"})
	if !strings.Contains(text, `"path": "Old/2026.03.04.md"`) {
		t.Fatalf("%s", text)
	}
}

func TestEditSectionWhenAHeadingIsAmbiguous(t *testing.T) {
	root := seedFiles(t, map[string]string{"inbox/N.md": "# N\n\n## A\n\n## A\n"})
	cs := runScoped(t, root, nil)
	if got := mustFail(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "A", "mode": "append", "text": "x"}); !strings.Contains(got, "more than once, on lines 2, 4") {
		t.Fatalf("%s", got)
	}
	mustCall(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "A", "lineNumber": float64(4), "mode": "append", "text": "x"})
	data, _ := os.ReadFile(filepath.Join(root, "inbox", "N.md"))
	if string(data) != "# N\n\n## A\n\n## A\n\nx\n" {
		t.Fatalf("%q", data)
	}
	if got := mustFail(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "N", "mode": "append", "text": " "}); !strings.Contains(got, "only replace can clear") {
		t.Fatalf("%s", got)
	}
	if got := mustFail(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "N", "mode": "rewrite", "text": "x"}); !strings.Contains(got, "mode must be") {
		t.Fatalf("%s", got)
	}
}

// rollSessionAt is a local session whose clock stands at a fixed day.
func rollSessionAt(t *testing.T, root string, day time.Time) *sdk.ClientSession {
	t.Helper()
	return runServerWith(t, backend.Options{Now: func() time.Time { return day.Add(12 * time.Hour) }}, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})
}

func mondayNoon() time.Time { return time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local) }

// update_task with state done rolls a repeating task exactly as toggle_task
// does: the same sessions, times, skip periods, until date and daily-note base.
func TestUpdateTaskRollsWithSessions(t *testing.T) {
	session := "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"
	for _, tc := range []struct {
		name  string
		notes map[string]string
		id    string
		want  string
	}{
		{"sessions are copied", map[string]string{
			"inbox/Work.md":  "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n",
			"quick/Plans.md": session,
		}, "inbox/Work.md#0", "when:2026-11-05T09:00-09:15"},
		{"a time on due: is kept", map[string]string{
			"inbox/Work.md": "- [ ] Water due:2026-11-02T09:00 @repeat:weekly\n",
		}, "inbox/Work.md#0", "due:2026-11-09T09:00"},
		{"a skip period is passed over", map[string]string{
			"inbox/Work.md": "- [ ] Water due:2026-11-02 @repeat:daily @skip:2026-11-03/2026-11-04\n",
		}, "inbox/Work.md#0", "due:2026-11-05"},
		{"the until date ends the series", map[string]string{
			"inbox/Work.md": "- [ ] Water due:2026-11-02 @repeat:daily @until:2026-11-02\n",
		}, "inbox/Work.md#0", "- [x] Water due:2026-11-02 @until:2026-11-02\n"},
		{"a daily note counts from its own date", map[string]string{
			".zennotes/vault.json":            `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":true}}`,
			"inbox/Daily Notes/2026-11-06.md": "- [ ] Water @repeat:monthly\n",
		}, "inbox/Daily Notes/2026-11-06.md#0", "due:2026-12-06"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viaUpdate, viaToggle := t.TempDir(), t.TempDir()
			writeNotes(t, viaUpdate, tc.notes)
			writeNotes(t, viaToggle, tc.notes)
			if text, isErr := callTool(t, rollSessionAt(t, viaUpdate, mondayNoon()), "update_task", map[string]any{"id": tc.id, "state": "done"}); isErr {
				t.Fatalf("update_task: %s", text)
			} else if !strings.Contains(text, `"checked": true`) {
				t.Fatalf("the result is not the finished task: %s", text)
			}
			if text, isErr := callTool(t, rollSessionAt(t, viaToggle, mondayNoon()), "toggle_task", map[string]any{"id": tc.id}); isErr {
				t.Fatalf("toggle_task: %s", text)
			}
			joined := ""
			for rel := range tc.notes {
				if strings.HasSuffix(rel, ".md") {
					got, want := withoutNewID(readNote(t, viaUpdate, rel)), withoutNewID(readNote(t, viaToggle, rel))
					if got != want {
						t.Fatalf("%s:\n%q\nwant what toggle_task writes:\n%q", rel, got, want)
					}
					joined += got
				}
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("no %q in:\n%s", tc.want, joined)
			}
		})
	}
}

// The expectedText check refuses before any roll and writes nothing.
func TestUpdateTaskExpectedTextStillRefuses(t *testing.T) {
	root := t.TempDir()
	body := "- [ ] first @repeat:weekly due:2026-11-02\n- [ ] second\n"
	writeNotes(t, root, map[string]string{"inbox/N.md": body})
	cs := rollSessionAt(t, root, mondayNoon())
	text, isErr := callTool(t, cs, "update_task", map[string]any{"id": "inbox/N.md#0", "state": "done", "expectedText": "second"})
	if !isErr || !strings.Contains(text, `now reads "first @repeat:weekly", not "second"`) {
		t.Fatalf("%v %s", isErr, text)
	}
	if got := readNote(t, root, "inbox/N.md"); got != body {
		t.Fatalf("a refused update wrote %q", got)
	}
}

// state cancelled on an open repeating task writes its next occurrence and a
// cancelled record; a task with no rule is only cancelled.
func TestUpdateTaskCancelledRolls(t *testing.T) {
	root := t.TempDir()
	writeNotes(t, root, map[string]string{
		"inbox/Work.md":  "- [ ] Water due:2026-11-02 @repeat:weekly\n  - [ ] Check the pots\n",
		"inbox/Plain.md": "- [ ] Once due:2026-11-02\n",
	})
	cs := rollSessionAt(t, root, mondayNoon())
	text, isErr := callTool(t, cs, "update_task", map[string]any{"id": "inbox/Work.md#0", "state": "cancelled"})
	if isErr || !strings.Contains(text, `"cancelled": true`) {
		t.Fatalf("%v %s", isErr, text)
	}
	want := "- [ ] Water due:2026-11-09 @repeat:weekly\n  - [ ] Check the pots\n" +
		"- [-] Water due:2026-11-02\n  - [-] Check the pots\n"
	if got := readNote(t, root, "inbox/Work.md"); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
	if _, isErr := callTool(t, cs, "update_task", map[string]any{"id": "inbox/Plain.md#0", "state": "cancelled"}); isErr {
		t.Fatal("update_task failed")
	}
	if got := readNote(t, root, "inbox/Plain.md"); got != "- [-] Once due:2026-11-02\n" {
		t.Fatalf("%q", got)
	}
}

// A task that is finished already does not roll again when state done is asked.
func TestUpdateTaskDoneOnFinishedTaskDoesNotRoll(t *testing.T) {
	root := t.TempDir()
	body := "- [x] Water due:2026-11-02 @repeat:weekly\n"
	writeNotes(t, root, map[string]string{"inbox/N.md": body})
	cs := rollSessionAt(t, root, mondayNoon())
	if text, isErr := callTool(t, cs, "update_task", map[string]any{"id": "inbox/N.md#0", "state": "done"}); isErr {
		t.Fatal(text)
	}
	if got := readNote(t, root, "inbox/N.md"); got != body {
		t.Fatalf("%q", got)
	}
}

// Each case of the shared recurrence table gives the same note through
// update_task as it gives through toggle_task.
func TestUpdateTaskRunsUpstreamCases(t *testing.T) {
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
			cs := rollSessionAt(t, root, day)
			text, isErr := callTool(t, cs, "update_task", map[string]any{"id": "inbox/note.md#" + strconv.Itoa(c.TaskIndex), "state": "done"})
			got := readNote(t, root, "inbox/note.md")
			if isErr {
				// A forwarded record keeps its marker: the call is refused.
				if c.Expected != nil || got != c.Body {
					t.Fatalf("update_task: %s\n%q", text, got)
				}
				return
			}
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

// Against a server, update_task keeps the roll of the released version: a rule
// the desktop app reads rolls, another rule is a plain tick, and nothing else
// is written. The server has no clock, so the next date counts from today.
func TestUpdateTaskOnRemoteKeepsReleasedRoll(t *testing.T) {
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
			text, isErr := callTool(t, cs, "update_task", map[string]any{"id": tc.id, "state": "done"})
			after := time.Now()
			if isErr {
				t.Fatalf("update_task: %s", text)
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

// A scoped client's update_task copies a session only into a note that the
// client may write.
func TestScopedUpdateTaskWritesOnlyNotesInScope(t *testing.T) {
	root := t.TempDir()
	session := "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"
	writeNotes(t, root, map[string]string{
		".zennotes/vault.json":     `{"primaryNotesLocation":"root"}`,
		"Projects/Work.md":         "- [ ] Water plants @repeat:mon/thu ^ab12cd\n",
		"Projects/Plans.md":        session,
		"Projects/Clients/Plan.md": session,
		"Journal/Plan.md":          session,
	})
	scope := testScope(t, "write", []string{"Projects"}, []string{"Projects/Clients"})
	cs := runScopedWith(t, root, scope, backend.Options{Now: func() time.Time { return mondayNoon().Add(12 * time.Hour) }})
	text, isErr := callTool(t, cs, "update_task", map[string]any{"id": "Projects/Work.md#0", "state": "done"})
	if isErr {
		t.Fatalf("update_task: %s", text)
	}
	for _, leak := range []string{"Clients", "Journal", "warning", "Warning"} {
		if strings.Contains(text, leak) {
			t.Fatalf("result mentions %q: %s", leak, text)
		}
	}
	if got := withoutNewID(readNote(t, root, "Projects/Plans.md")); got != session+"- Water plants task:[[Work#^{ID}]] when:2026-11-05T09:00-09:15\n" {
		t.Fatalf("Plans.md:\n%s", got)
	}
	for _, rel := range []string{"Projects/Clients/Plan.md", "Journal/Plan.md"} {
		if got := readNote(t, root, rel); got != session {
			t.Fatalf("%s was written:\n%s", rel, got)
		}
	}
}
