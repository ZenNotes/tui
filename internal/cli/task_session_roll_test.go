package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newIDRe finds a block id that the roll made. The id `ab12cd` is the one the
// tests write by hand.
var newIDRe = regexp.MustCompile(`\^[a-z0-9]{6}\b`)

// writeVaultNotes writes notes, given by vault path, into a fresh vault.
func writeVaultNotes(t *testing.T, notes map[string]string) string {
	t.Helper()
	root := isolatedCLI(t)
	for rel, body := range notes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readVaultNote(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runToggle runs `zn task toggle` and returns its exit code and stderr.
func runToggle(t *testing.T, root, id string) (int, string) {
	t.Helper()
	var errBuf bytes.Buffer
	var code int
	captureOutput(t, func() {
		stderr = &errBuf
		code = Main([]string{"task", "toggle", id, "--vault", root})
	})
	return code, errBuf.String()
}

// masked replaces the one block id that a roll made with {ID}. It fails when
// the roll made more than one different id.
func masked(t *testing.T, text string) (string, string) {
	t.Helper()
	made := ""
	out := newIDRe.ReplaceAllStringFunc(text, func(id string) string {
		if id == "^ab12cd" {
			return id
		}
		if made != "" && made != id {
			t.Errorf("two new ids: %s and %s", made, id)
		}
		made = id
		return "^{ID}"
	})
	return out, strings.TrimPrefix(made, "^")
}

func TestTaskToggleCopiesWorkSessions(t *testing.T) {
	for _, tc := range []struct {
		name, today string
		work, plans string
		wantWork    string
		wantPlans   string
	}{
		{
			name: "repeat on named days", today: "2026-11-02",
			work:      "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n",
			plans:     "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n",
			wantWork:  "intro\n- [ ] Water plants @repeat:mon/thu ^{ID}\n- [x] Water plants ^ab12cd\noutro\n",
			wantPlans: "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n- Water plants task:[[Work#^{ID}]] when:2026-11-05T09:00-09:15\n",
		},
		{
			name: "due and session", today: "2026-11-02",
			work:      "- [ ] T due:2026-11-04T17:00-17:30 @repeat:3d ^ab12cd\n",
			plans:     "- T task:[[Work#^ab12cd]] when:2026-11-02T09:30-10:00\n",
			wantWork:  "- [ ] T due:2026-11-07T17:00-17:30 @repeat:3d ^{ID}\n- [x] T due:2026-11-04T17:00-17:30 ^ab12cd\n",
			wantPlans: "- T task:[[Work#^ab12cd]] when:2026-11-02T09:30-10:00\n- T task:[[Work#^{ID}]] when:2026-11-05T09:30-10:00\n",
		},
		{
			name: "count from completion", today: "2026-11-04",
			work:      "- [ ] T due:2026-11-03 @repeat:after-3d ^ab12cd\n",
			plans:     "- T task:[[Work#^ab12cd]] when:2026-11-02\n",
			wantWork:  "- [ ] T due:2026-11-08 @repeat:after-3d ^{ID}\n- [x] T due:2026-11-03 ^ab12cd\n",
			wantPlans: "- T task:[[Work#^ab12cd]] when:2026-11-02\n- T task:[[Work#^{ID}]] when:2026-11-07\n",
		},
		{
			name: "two sessions", today: "2026-11-02",
			work:      "- [ ] T @repeat:1w ^ab12cd\n",
			plans:     "- T task:[[Work#^ab12cd]] when:2026-11-02T09:00-10:00\n- T task:[[Work#^ab12cd]] when:2026-11-03T14:00-16:00\n",
			wantWork:  "- [ ] T @repeat:1w ^{ID}\n- [x] T ^ab12cd\n",
			wantPlans: "- T task:[[Work#^ab12cd]] when:2026-11-02T09:00-10:00\n- T task:[[Work#^{ID}]] when:2026-11-09T09:00-10:00\n- T task:[[Work#^ab12cd]] when:2026-11-03T14:00-16:00\n- T task:[[Work#^{ID}]] when:2026-11-10T14:00-16:00\n",
		},
		{
			name: "skip period", today: "2027-03-08",
			work:      "- [ ] Assignment due:2027-03-12T23:59 @repeat:1w @skip:2027-03-13/2027-03-21 ^ab12cd\n",
			plans:     "- Assignment task:[[Work#^ab12cd]] when:2027-03-08T09:00-11:00\n",
			wantWork:  "- [ ] Assignment due:2027-03-26T23:59 @repeat:1w @skip:2027-03-13/2027-03-21 ^{ID}\n- [x] Assignment due:2027-03-12T23:59 @skip:2027-03-13/2027-03-21 ^ab12cd\n",
			wantPlans: "- Assignment task:[[Work#^ab12cd]] when:2027-03-08T09:00-11:00\n- Assignment task:[[Work#^{ID}]] when:2027-03-22T09:00-11:00\n",
		},
		{
			name: "session across midnight", today: "2026-11-02",
			work:      "- [ ] T @repeat:1w ^ab12cd\n",
			plans:     "- T task:[[Work#^ab12cd]] when:2026-11-02T22:00/2026-11-03T02:00\n",
			wantWork:  "- [ ] T @repeat:1w ^{ID}\n- [x] T ^ab12cd\n",
			wantPlans: "- T task:[[Work#^ab12cd]] when:2026-11-02T22:00/2026-11-03T02:00\n- T task:[[Work#^{ID}]] when:2026-11-09T22:00/2026-11-10T02:00\n",
		},
		{
			name: "repeating and dangling sessions", today: "2026-11-02",
			work:      "- [ ] Water plants @repeat:mon/thu ^ab12cd\n",
			plans:     "- Water plants task:[[Work#^ab12cd]] when:2026-11-03T09:00-09:15\n- Water plants task:[[Work#^ab12cd]] when:2026-11-02T08:00 @repeat:1w\n- Water plants task:[[Work#^zz99zz]] when:2026-11-01T08:00\n",
			wantWork:  "- [ ] Water plants @repeat:mon/thu ^{ID}\n- [x] Water plants ^ab12cd\n",
			wantPlans: "- Water plants task:[[Work#^ab12cd]] when:2026-11-03T09:00-09:15\n- Water plants task:[[Work#^{ID}]] when:2026-11-05T09:00-09:15\n- Water plants task:[[Work#^ab12cd]] when:2026-11-02T08:00 @repeat:1w\n- Water plants task:[[Work#^zz99zz]] when:2026-11-01T08:00\n",
		},
		{
			name: "no session", today: "2026-11-02",
			work:      "- [ ] T due:2026-11-02 @repeat:1d ^keep\n  - [ ] Sub ^sub1\n",
			plans:     "- Other task:[[Work#^zz99zz]] when:2026-11-02\n",
			wantWork:  "- [ ] T due:2026-11-03 @repeat:1d\n  - [ ] Sub\n- [x] T due:2026-11-02 ^keep\n  - [-] Sub ^sub1\n",
			wantPlans: "- Other task:[[Work#^zz99zz]] when:2026-11-02\n",
		},
		{
			name: "no date and no session", today: "2026-11-02",
			work:      "- [ ] T @repeat:3d\n",
			plans:     "- Other task:[[Work#^zz99zz]] when:2026-11-02\n",
			wantWork:  "- [ ] T @repeat:3d due:2026-11-05\n- [x] T\n",
			wantPlans: "- Other task:[[Work#^zz99zz]] when:2026-11-02\n",
		},
		{
			name: "at the until date", today: "2026-12-14",
			work:      "- [ ] Lab @repeat:1w @until:2026-12-14 ^ab12cd\n",
			plans:     "- Lab task:[[Work#^ab12cd]] when:2026-12-14T09:00\n",
			wantWork:  "- [x] Lab @until:2026-12-14 ^ab12cd\n",
			wantPlans: "- Lab task:[[Work#^ab12cd]] when:2026-12-14T09:00\n",
		},
		{
			name: "copy carries no until", today: "2026-12-07",
			work:      "- [ ] Lab @repeat:1w @until:2026-12-14 ^ab12cd\n",
			plans:     "- Lab task:[[Work#^ab12cd]] when:2026-12-07T09:00 @until:2026-12-20\n",
			wantWork:  "- [ ] Lab @repeat:1w @until:2026-12-14 ^{ID}\n- [x] Lab @until:2026-12-14 ^ab12cd\n",
			wantPlans: "- Lab task:[[Work#^ab12cd]] when:2026-12-07T09:00 @until:2026-12-20\n- Lab task:[[Work#^{ID}]] when:2026-12-14T09:00\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixClock(t, tc.today)
			root := writeVaultNotes(t, map[string]string{"inbox/Work.md": tc.work, "quick/Plans.md": tc.plans})
			toggle := "inbox/Work.md#0"
			if strings.Contains(tc.work, "^ab12cd") {
				toggle = "inbox/Work.md#^ab12cd"
			}
			if code, errText := runToggle(t, root, toggle); code != 0 || errText != "" {
				t.Fatalf("exit %d, stderr %q", code, errText)
			}
			gotWork, id := masked(t, readVaultNote(t, root, "inbox/Work.md"))
			if gotWork != tc.wantWork {
				t.Fatalf("Work.md:\n%s\nwant:\n%s", gotWork, tc.wantWork)
			}
			gotPlans := strings.ReplaceAll(readVaultNote(t, root, "quick/Plans.md"), "^"+id, "^{ID}")
			if id == "" {
				gotPlans = readVaultNote(t, root, "quick/Plans.md")
			}
			if gotPlans != tc.wantPlans {
				t.Fatalf("Plans.md:\n%s\nwant:\n%s", gotPlans, tc.wantPlans)
			}
		})
	}
}

func TestTaskToggleCopiesSessionOfATaskNote(t *testing.T) {
	fixClock(t, "2026-11-02")
	root := writeVaultNotes(t, map[string]string{
		"inbox/Report.md": "---\npage-type: task\nstatus: open\ndue: 2026-11-04\nrepeat: 1w\n---\nBody\n",
		"quick/Plans.md":  "- Report task:[[Report]] when:2026-11-02T09:00\n",
	})
	if code, errText := runToggle(t, root, "inbox/Report.md#task"); code != 0 || errText != "" {
		t.Fatalf("exit %d, stderr %q", code, errText)
	}
	report := readVaultNote(t, root, "inbox/Report.md")
	for _, want := range []string{"status: open", "due: 2026-11-11", "lastCompleted: 2026-11-02"} {
		if !strings.Contains(report, want) {
			t.Errorf("Report.md lacks %q:\n%s", want, report)
		}
	}
	want := "- Report task:[[Report]] when:2026-11-02T09:00\n- Report task:[[Report]] when:2026-11-09T09:00\n"
	if got := readVaultNote(t, root, "quick/Plans.md"); got != want {
		t.Fatalf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

// A note that cannot be written gives a warning on stderr, and the roll
// stands.
func TestTaskToggleWarnsWhenCategoryNoteIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a directory that is read only")
	}
	fixClock(t, "2026-11-02")
	root := writeVaultNotes(t, map[string]string{
		"inbox/Work.md":  "- [ ] Water plants @repeat:mon/thu ^ab12cd\n",
		"quick/Plans.md": "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n",
	})
	dir := filepath.Join(root, "quick")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	code, errText := runToggle(t, root, "inbox/Work.md#^ab12cd")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errText)
	}
	if !strings.Contains(errText, "Plans.md") || !strings.Contains(errText, "warning") {
		t.Fatalf("stderr does not name Plans.md: %q", errText)
	}
	gotWork, _ := masked(t, readVaultNote(t, root, "inbox/Work.md"))
	if want := "- [ ] Water plants @repeat:mon/thu ^{ID}\n- [x] Water plants ^ab12cd\n"; gotWork != want {
		t.Fatalf("Work.md:\n%s\nwant:\n%s", gotWork, want)
	}
	if got, want := readVaultNote(t, root, "quick/Plans.md"), "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"; got != want {
		t.Fatalf("Plans.md changed:\n%s", got)
	}
}
