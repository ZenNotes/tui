package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

const (
	schedWork    = "# Work\n\n- [ ] Write report due:2026-11-06\n- [ ] Other\n"
	schedPlans   = "---\npage-type: event-list\n---\n\n# Plans\n"
	schedMedical = "---\npage-type: event-list\n---\n\n# Medical\n"
)

var schedIDRe = regexp.MustCompile(` \^([a-z0-9]{6})$`)

// scheduleApp opens a vault that holds the notes, with the Tasks view open
// and the clock at noon on 2026-11-02.
func scheduleApp(t *testing.T, notes map[string]string) (*App, string) {
	t.Helper()
	a, root := sessionRollApp(t, notes, "2026-11-02")
	a.openTasks()
	return a, root
}

// selectTask moves the cursor of the Tasks view onto the task with the text.
func selectTask(t *testing.T, a *App, text string) *tasksView {
	t.Helper()
	reloadIndex(t, a)
	v := a.activeTab().view.(*tasksView)
	v.refresh(a)
	for i, row := range v.rows {
		if row.task != nil && row.task.Content == text {
			v.list.cursor = i
			return v
		}
	}
	t.Fatalf("no row for %q", text)
	return nil
}

func schedPrompt(t *testing.T, a *App) *prompt {
	t.Helper()
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatalf("overlay is %T, want a prompt", a.overlay)
	}
	return p
}

func schedMenu(t *testing.T, a *App) *menu {
	t.Helper()
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatalf("overlay is %T, want a menu", a.overlay)
	}
	return m
}

// answer types text into the open prompt and presses Enter.
func answer(t *testing.T, a *App, text string) {
	t.Helper()
	p := schedPrompt(t, a)
	p.input = newTextInput(text)
	a.handleKey(vim.Key{Name: "enter"})
}

func menuLabels(m *menu) []string {
	var out []string
	for _, it := range m.items {
		out = append(out, it.label)
	}
	return out
}

// pickLabel chooses the entry of the open menu with the label.
func pickLabel(t *testing.T, a *App, label string) {
	t.Helper()
	m := schedMenu(t, a)
	for i, it := range m.items {
		if it.label == label {
			m.cursor = i
			a.handleKey(vim.Key{Name: "enter"})
			return
		}
	}
	t.Fatalf("no menu entry %q in %v", label, menuLabels(m))
}

// schedule runs W on the task, answers the prompt and picks the note.
func schedule(t *testing.T, a *App, task, when, note string) {
	t.Helper()
	selectTask(t, a, task)
	a.handleKey(vim.R('W'))
	answer(t, a, when)
	pickLabel(t, a, note)
}

func TestScheduleWritesBlockIdAndSessionLine(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans})
	schedule(t, a, "Write report", "2026-11-03T14:00-15:30", "Plans")
	work := readRollNote(t, root, "inbox/Work.md")
	m := regexp.MustCompile(`(?m)^- \[ \] Write report due:2026-11-06 \^([a-z0-9]{6})$`).FindStringSubmatch(work)
	if m == nil {
		t.Fatalf("Work.md has no id on the task line:\n%s", work)
	}
	if want := "# Work\n\n- [ ] Write report due:2026-11-06 ^" + m[1] + "\n- [ ] Other\n"; work != want {
		t.Errorf("Work.md:\n%s\nwant:\n%s", work, want)
	}
	plans := readRollNote(t, root, "inbox/Plans.md")
	want := schedPlans + "- Write report task:[[Work#^" + m[1] + "]] when:2026-11-03T14:00-15:30\n"
	if plans != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", plans, want)
	}
	if strings.Contains(work+plans, "scheduled") {
		t.Error("a note holds the word scheduled")
	}
}

func TestScheduleReusesExistingBlockId(t *testing.T) {
	work := "- [ ] Write report ^draft-1\n"
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": work, "inbox/Plans.md": schedPlans})
	schedule(t, a, "Write report", "2026-11-03", "Plans")
	if got := readRollNote(t, root, "inbox/Work.md"); got != work {
		t.Errorf("Work.md changed:\n%s", got)
	}
	want := schedPlans + "- Write report task:[[Work#^draft-1]] when:2026-11-03\n"
	if got := readRollNote(t, root, "inbox/Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestScheduleMakesUniqueBlockId(t *testing.T) {
	for i := 0; i < 20; i++ {
		other := "- [ ] Other ^aaaaaa\n- [ ] Another ^bbbbbb\n"
		a, root := scheduleApp(t, map[string]string{"inbox/Work.md": "- [ ] Write report\n", "inbox/Other.md": other, "inbox/Plans.md": schedPlans})
		schedule(t, a, "Write report", "2026-11-03", "Plans")
		m := schedIDRe.FindStringSubmatch(strings.TrimSuffix(readRollNote(t, root, "inbox/Work.md"), "\n"))
		if m == nil {
			t.Fatal("the task line has no new id")
		}
		if m[1] == "aaaaaa" || m[1] == "bbbbbb" {
			t.Fatalf("the new id %s is in use", m[1])
		}
	}
}

func TestSchedulePromptPrefillAndAnswers(t *testing.T) {
	a, _ := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans})
	selectTask(t, a, "Write report")
	a.handleKey(vim.R('W'))
	if got := schedPrompt(t, a).input.String(); got != "2026-11-02T13:00-14:00" {
		t.Fatalf("prefill %q", got)
	}
	a.handleKey(vim.Key{Name: "esc"})

	good := map[string]string{
		"2026-11-03T14:00-15:30":            "2026-11-03T14:00-15:30",
		"2026-11-03":                        "2026-11-03",
		"2026-11-03T22:00/2026-11-04T02:00": "2026-11-03T22:00/2026-11-04T02:00",
		"tomorrow":                          "2026-11-03",
		"+3":                                "2026-11-05",
		"fri":                               "2026-11-06",
	}
	for in, want := range good {
		a, root := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans})
		schedule(t, a, "Write report", in, "Plans")
		if got := readRollNote(t, root, "inbox/Plans.md"); !strings.HasSuffix(got, " when:"+want+"\n") {
			t.Errorf("answer %q: Plans.md:\n%s\nwant when:%s", in, got, want)
		}
	}
	for _, in := range []string{"2026-02-30", "soon", ""} {
		a, root := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans})
		selectTask(t, a, "Write report")
		a.handleKey(vim.R('W'))
		answer(t, a, in)
		if _, ok := a.overlay.(*prompt); !ok || !a.messageErr {
			t.Errorf("answer %q: the prompt did not stay open with an error", in)
		}
		if readRollNote(t, root, "inbox/Work.md") != schedWork || readRollNote(t, root, "inbox/Plans.md") != schedPlans {
			t.Errorf("answer %q: a note changed", in)
		}
	}
}

func TestCategoryPickerOrderAndDefault(t *testing.T) {
	notes := map[string]string{
		"inbox/Work.md":    schedWork,
		"inbox/Plans.md":   schedPlans,
		"inbox/Medical.md": schedMedical,
		"inbox/Quoted.md":  "---\npage-type: \"event-list\"\n---\n",
		"inbox/List.md":    "---\npage-type: [event-list]\n---\n",
		"inbox/Plural.md":  "---\npage-type: events\n---\n",
		"inbox/Upper.md":   "---\npage-type: Event-List\n---\n",
		"inbox/Both.md":    "---\npage-type: [event-list, uni]\n---\n",
		"inbox/Key.md":     "---\nPage-Type: event-list\n---\n",
	}
	a, _ := scheduleApp(t, notes)
	selectTask(t, a, "Write report")
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-03")
	m := schedMenu(t, a)
	want := []string{"List", "Medical", "Plans", "Quoted", "New category note…"}
	if got := menuLabels(m); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("entries %v, want %v", got, want)
	}
	if m.cursor != 0 {
		t.Errorf("cursor %d, want 0", m.cursor)
	}

	a, _ = scheduleApp(t, map[string]string{"inbox/Work.md": schedWork})
	selectTask(t, a, "Write report")
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-03")
	m = schedMenu(t, a)
	if got := menuLabels(m); len(got) != 1 || got[0] != "New category note…" || m.cursor != 0 {
		t.Errorf("entries %v cursor %d", got, m.cursor)
	}
}

func TestNewCategoryNote(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": "- [ ] Write report ^ab12cd\n"})
	selectTask(t, a, "Write report")
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-02T15:00-16:00")
	pickLabel(t, a, "New category note…")
	answer(t, a, "Uni")
	want := "---\npage-type: event-list\n---\n\n# Uni\n\n- Write report task:[[Work#^ab12cd]] when:2026-11-02T15:00-16:00\n"
	if got := readRollNote(t, root, "inbox/Uni.md"); got != want {
		t.Errorf("Uni.md:\n%q\nwant:\n%q", got, want)
	}
	if got := a.lastCategoryNote(); got != "inbox/Uni.md" {
		t.Errorf("last used %q", got)
	}

	// The next new note goes in the folder of the last-used note.
	writeRollNotes(t, root, map[string]string{"inbox/Deep/Base.md": schedPlans})
	reloadIndex(t, a)
	selectTask(t, a, "Write report")
	a.rememberCategoryNote("inbox/Deep/Base.md")
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-03")
	pickLabel(t, a, "New category note…")
	answer(t, a, "Uni")
	if a.overlay != nil {
		t.Fatal("an overlay stays open")
	}
	if _, err := os.Stat(filepath.Join(root, "inbox", "Deep", "Uni.md")); err != nil {
		t.Errorf("no note in the folder of the last-used note: %v", err)
	}
}

func TestNewCategoryNoteExistingNameKeepsPrompt(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Uni.md": "# Uni\n"})
	selectTask(t, a, "Write report")
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-03")
	pickLabel(t, a, "New category note…")
	answer(t, a, "Uni")
	if _, ok := a.overlay.(*prompt); !ok || !a.messageErr {
		t.Fatal("the title prompt did not stay open with an error")
	}
	if got := readRollNote(t, root, "inbox/Uni.md"); got != "# Uni\n" {
		t.Errorf("Uni.md changed: %q", got)
	}
	if got := readRollNote(t, root, "inbox/Work.md"); got != schedWork {
		t.Errorf("Work.md changed: %q", got)
	}
}

func TestLastUsedCategoryNotePerVault(t *testing.T) {
	notes := map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans, "inbox/Medical.md": schedMedical}
	a, root := scheduleApp(t, notes)
	schedule(t, a, "Write report", "2026-11-03", "Medical")

	// A new App on the same vault reads the choice from the session file.
	b := newSessionParityApp(t, root)
	b.openTasks()
	selectTask(t, b, "Write report")
	b.handleKey(vim.R('W'))
	answer(t, b, "2026-11-04")
	m := schedMenu(t, b)
	if m.items[m.cursor].label != "Medical" {
		t.Errorf("selected %q, want Medical", m.items[m.cursor].label)
	}

	// A note that is gone gives the default.
	if err := os.Remove(filepath.Join(root, "inbox", "Medical.md")); err != nil {
		t.Fatal(err)
	}
	reloadIndex(t, b)
	b.handleKey(vim.Key{Name: "esc"})
	b.handleKey(vim.R('W'))
	answer(t, b, "2026-11-04")
	m = schedMenu(t, b)
	if m.items[m.cursor].label != "Plans" {
		t.Errorf("selected %q, want Plans", m.items[m.cursor].label)
	}
}

func TestLastUsedCategoryNoteKeepsOneEntryPerSavedVault(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans})
	saved := t.TempDir()
	gone := t.TempDir()
	var ws config.Workspaces
	ws.AddVault("main", root)
	ws.AddVault("other", saved)
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	f := readSessionFile()
	f.LastCategory = map[string]string{saved: "inbox/Other.md", gone: "inbox/Gone.md"}
	writeSessionFileForTest(t, f)

	schedule(t, a, "Write report", "2026-11-03", "Plans")
	f = readSessionFile()
	if len(f.LastCategory) != 2 || f.LastCategory[root] != "inbox/Plans.md" || f.LastCategory[saved] != "inbox/Other.md" {
		t.Errorf("entries %v", f.LastCategory)
	}
	if _, ok := f.LastCategory[gone]; ok {
		t.Error("the entry of a vault that is not saved stays")
	}
}

func writeSessionFileForTest(t *testing.T, f sessionFile) {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sessionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScheduleEscapeWritesNothing(t *testing.T) {
	notes := map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans}
	a, root := scheduleApp(t, notes)
	selectTask(t, a, "Write report")
	a.handleKey(vim.R('W'))
	a.handleKey(vim.Key{Name: "esc"})
	if a.overlay != nil {
		t.Fatal("the prompt stays open")
	}
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-03")
	schedMenu(t, a)
	a.handleKey(vim.Key{Name: "esc"})
	if a.overlay != nil {
		t.Fatal("the picker stays open")
	}
	if readRollNote(t, root, "inbox/Work.md") != schedWork || readRollNote(t, root, "inbox/Plans.md") != schedPlans {
		t.Error("a note changed")
	}
	if got := a.lastCategoryNote(); got != "" {
		t.Errorf("last used %q", got)
	}
}

func TestScheduleFromBoardCard(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": schedWork, "inbox/Plans.md": schedPlans})
	reloadIndex(t, a)
	v := a.activeTab().view.(*tasksView)
	v.mode = "kanban"
	v.refresh(a)
	found := false
	for c, col := range v.columns {
		for i, card := range col.cards {
			if card.Content == "Write report" {
				v.col, v.card, found = c, i, true
			}
		}
	}
	if !found {
		t.Fatal("no card")
	}
	a.handleKey(vim.R('W'))
	answer(t, a, "2026-11-03T14:00-15:30")
	pickLabel(t, a, "Plans")
	if got := readRollNote(t, root, "inbox/Plans.md"); !strings.Contains(got, "- Write report task:[[Work#^") {
		t.Errorf("Plans.md:\n%s", got)
	}
}

func TestScheduleAddsNewlineBeforeAppending(t *testing.T) {
	plans := "---\npage-type: event-list\n---\n- Old task:[[Work]] when:2026-11-01"
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": "- [ ] Write report ^ab12cd\n", "inbox/Plans.md": plans})
	schedule(t, a, "Write report", "2026-11-03", "Plans")
	want := plans + "\n- Write report task:[[Work#^ab12cd]] when:2026-11-03\n"
	if got := readRollNote(t, root, "inbox/Plans.md"); got != want {
		t.Errorf("Plans.md:\n%q\nwant:\n%q", got, want)
	}
}

func TestScheduleTaskNoteLinksTitle(t *testing.T) {
	task := "---\npage-type: task\n---\n\n# Buy milk\n"
	a, root := scheduleApp(t, map[string]string{"inbox/Buy milk.md": task, "inbox/Plans.md": schedPlans})
	schedule(t, a, "Buy milk", "2026-11-03T09:00", "Plans")
	if got := readRollNote(t, root, "inbox/Buy milk.md"); got != task {
		t.Errorf("the task note changed:\n%s", got)
	}
	want := schedPlans + "- Buy milk task:[[Buy milk]] when:2026-11-03T09:00\n"
	if got := readRollNote(t, root, "inbox/Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestScheduleKeepsSpacesInNoteName(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Big plan.md": "- [ ] Write report ^ab12cd\n", "inbox/Plans.md": schedPlans})
	schedule(t, a, "Write report", "2026-11-03", "Plans")
	want := schedPlans + "- Write report task:[[Big plan#^ab12cd]] when:2026-11-03\n"
	if got := readRollNote(t, root, "inbox/Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestScheduleAgainAddsSecondLine(t *testing.T) {
	a, root := scheduleApp(t, map[string]string{"inbox/Work.md": "- [ ] Write report ^ab12cd\n", "inbox/Plans.md": schedPlans})
	schedule(t, a, "Write report", "2026-11-03T14:00", "Plans")
	a.handleKey(vim.Key{Name: "esc"})
	schedule(t, a, "Write report", "2026-11-05T09:00", "Plans")
	want := schedPlans + "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00\n- Write report task:[[Work#^ab12cd]] when:2026-11-05T09:00\n"
	if got := readRollNote(t, root, "inbox/Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestScheduleRefusesOnRemoteVault(t *testing.T) {
	a := &App{idx: &index{}}
	a.opts.Target.Kind = "remote"
	a.opts.Target.BaseURL = "http://example.test"
	a.scheduleSession(vault.Task{Content: "Write report", SourcePath: "inbox/Work.md"})
	if a.overlay != nil {
		t.Fatal("a prompt opened")
	}
	if a.message != "Events need a local vault" {
		t.Errorf("message %q", a.message)
	}
}

// reloadIndex reads the vault again, as the app does after a change on disk.
func reloadIndex(t *testing.T, a *App) {
	t.Helper()
	loaded := a.loadIndexCmd()().(indexLoadedMsg)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	a.idx = loaded.idx
	msg := a.loadTasksCmd()().(tasksLoadedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	a.idx.tasks, a.idx.events = msg.tasks, msg.events
}
