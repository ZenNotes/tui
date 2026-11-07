package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

// doctorWithTools runs `zn doctor --json` with config.toml holding terminal
// under [terminal] (none when empty) and PATH holding only stubs of tools.
func doctorWithTools(t *testing.T, terminal string, tools ...string) (string, []diagnosticCheck) {
	t.Helper()
	isolatedCLI(t)
	if terminal != "" {
		if err := os.MkdirAll(filepath.Dir(config.ConfigTomlPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.ConfigTomlPath(), []byte("[terminal]\n"+terminal+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	for _, tool := range tools {
		name := tool
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	out := captureOutput(t, func() { _ = Main([]string{"doctor", "--json"}) })
	var report struct {
		Checks []diagnosticCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(out, "\n{\"error\"", 2)[0]), &report); err != nil {
		t.Fatalf("doctor json: %v %s", err, out)
	}
	return out, report.Checks
}

func latexToolsCheck(t *testing.T, checks []diagnosticCheck, out string) diagnosticCheck {
	t.Helper()
	for _, c := range checks {
		if c.Name == "latex tools" {
			return c
		}
	}
	t.Fatalf("no \"latex tools\" check under math = \"latex\":\n%s", out)
	return diagnosticCheck{}
}

// Under latex, a missing pdftex is a failing check that names it.
func TestDoctorLatexReportsMissingPdftex(t *testing.T) {
	out, checks := doctorWithTools(t, `math = "latex"`, "pdftocairo")
	c := latexToolsCheck(t, checks, out)
	if c.OK || !strings.Contains(c.Message, "pdftex") {
		t.Fatalf("latex tools check %+v", c)
	}
	if strings.Contains(c.Message, "pdftocairo") {
		t.Fatalf("pdftocairo is on PATH but reported missing: %+v", c)
	}
}

// Under latex, a missing pdftocairo is a failing check that names it.
func TestDoctorLatexReportsMissingPdftocairo(t *testing.T) {
	out, checks := doctorWithTools(t, `math = "latex"`, "pdftex")
	c := latexToolsCheck(t, checks, out)
	if c.OK || !strings.Contains(c.Message, "pdftocairo") {
		t.Fatalf("latex tools check %+v", c)
	}
	if strings.Contains(strings.ReplaceAll(c.Message, "pdftocairo", ""), "pdftex") {
		t.Fatalf("pdftex is on PATH but reported missing: %+v", c)
	}
}

// Under typst, or with no math key, doctor never mentions the TeX tools.
func TestDoctorTypstNeverMentionsTexTools(t *testing.T) {
	for _, terminal := range []string{`math = "typst"`, ``} {
		out, _ := doctorWithTools(t, terminal)
		for _, tool := range []string{"pdftex", "pdftocairo"} {
			if strings.Contains(out, tool) {
				t.Errorf("%q: doctor output mentions %s:\n%s", terminal, tool, out)
			}
		}
	}
}

// A failed latex check carries this system's install steps, and doctor
// prints them under the check.
func TestDoctorLatexPrintsInstallSteps(t *testing.T) {
	out, checks := doctorWithTools(t, `math = "latex"`)
	c := latexToolsCheck(t, checks, out)
	if c.OK || len(c.Hint) == 0 {
		t.Fatalf("missing tools gave no install steps: %+v", c)
	}
	if !strings.Contains(c.Message, "zn does not include them") {
		t.Fatalf("message does not say zn ships no TeX: %q", c.Message)
	}
	isolatedCLI(t)
	text := captureOutput(t, func() {
		_ = os.MkdirAll(filepath.Dir(config.ConfigTomlPath()), 0o755)
		_ = os.WriteFile(config.ConfigTomlPath(), []byte("[terminal]\nmath = \"latex\"\n"), 0o644)
		t.Setenv("PATH", t.TempDir())
		_ = Main([]string{"doctor"})
	})
	if !strings.Contains(text, "      "+c.Hint[0]) {
		t.Fatalf("doctor did not print the install steps under the check:\n%s", text)
	}
}

func TestDoctorLatexWithToolsHasNoInstallSteps(t *testing.T) {
	out, checks := doctorWithTools(t, `math = "latex"`, "pdftex", "pdftocairo")
	if c := latexToolsCheck(t, checks, out); !c.OK || len(c.Hint) != 0 {
		t.Fatalf("tools present, yet: %+v", c)
	}
}

func TestLatexInstallHintPerSystem(t *testing.T) {
	for _, tc := range []struct{ goos, distro, want string }{
		{"darwin", "", "brew install --cask basictex"},
		{"darwin", "", "sudo tlmgr install " + latexPackages},
		{"linux", "ubuntu debian", "sudo apt install texlive-latex-extra"},
		{"linux", "pop ubuntu debian", "sudo apt install"},
		{"linux", "debian", "poppler-utils"},
		{"linux", "fedora", "sudo dnf install texlive-scheme-basic"},
		{"linux", "rocky rhel centos fedora", "sudo dnf install"},
		{"linux", "manjaro arch", "sudo pacman -S texlive-basic"},
		{"linux", "opensuse-tumbleweed opensuse suse", "packages " + latexPackages},
		{"linux", "", "poppler for pdftocairo"},
		{"windows", "", "MiKTeX"},
		{"freebsd", "", "TeX Live"},
	} {
		if got := strings.Join(latexInstallHint(tc.goos, tc.distro), "\n"); !strings.Contains(got, tc.want) {
			t.Errorf("%s %q: hint lacks %q:\n%s", tc.goos, tc.distro, tc.want, got)
		}
	}
}

// An unknown task_note_marker fails the configuration check of zn doctor.
func TestDoctorFailsOnUnknownTaskNoteMarker(t *testing.T) {
	out, checks := doctorWithTools(t, `task_note_marker = "foo"`)
	for _, c := range checks {
		if c.Name == "configuration" {
			if c.OK {
				t.Fatalf("configuration check passed with task_note_marker = \"foo\":\n%s", out)
			}
			if !strings.Contains(c.Message, "terminal.task_note_marker") {
				t.Fatalf("the failure does not name terminal.task_note_marker:\n%s", out)
			}
			return
		}
	}
	t.Fatalf("no configuration check:\n%s", out)
}

// doctorText runs `zn doctor` on root and returns its text output and exit
// status.
func doctorText(t *testing.T, root string) (string, int) {
	t.Helper()
	code := 0
	out := captureOutput(t, func() { code = Main([]string{"doctor", "--vault", root}) })
	return out, code
}

// A bad time on a task is a warning with the file and line, and it never
// changes the exit status that a clean vault gets.
func TestDoctorWarnsAboutBadTaskTokens(t *testing.T) {
	isolatedCLI(t)
	clean := t.TempDir()
	writeVaultFile(t, clean, "ok.md", "- [ ] fine due:2026-11-03T14:00\n")
	_, cleanCode := doctorText(t, clean)

	root := t.TempDir()
	writeVaultFile(t, root, "a.md", "# A\n\n- [ ] late due:2026-11-03T25:00\n")
	writeVaultFile(t, root, "b.md", "# B\n\n\n\n- [ ] odd repeat:sometimes\n")
	out, code := doctorText(t, root)
	if !strings.Contains(out, "WARN") {
		t.Fatalf("no warning line:\n%s", out)
	}
	for _, want := range []string{"a.md:3", "due:2026-11-03T25:00", "b.md:5", "repeat:sometimes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if code != cleanCode {
		t.Fatalf("exit status %d with a warning, %d on a clean vault", code, cleanCode)
	}
}

// A single bad token is counted in the singular.
func TestDoctorCountsOneBadTokenInSingular(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	writeVaultFile(t, root, "a.md", "- [ ] odd repeat:fortnightly\n")
	out, _ := doctorText(t, root)
	if !strings.Contains(out, "1 task time, repeat rule or skip item is not valid and is ignored") {
		t.Fatalf("warning not in the singular:\n%s", out)
	}
}

// At most ten bad tokens are listed, then how many more there are.
func TestDoctorListsTenBadTokensThenCount(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	var body strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&body, "- [ ] task %d due:2026-11-03T25:%02d\n", i, i)
	}
	writeVaultFile(t, root, "many.md", body.String())
	out, _ := doctorText(t, root)
	if n := strings.Count(out, "many.md:"); n != 10 {
		t.Fatalf("%d listed lines, want 10:\n%s", n, out)
	}
	if !strings.Contains(out, "+2 more") {
		t.Fatalf("no \"+2 more\" line:\n%s", out)
	}
}

// Only the selected workspace is scanned.
func TestDoctorScansOnlySelectedWorkspace(t *testing.T) {
	isolatedCLI(t)
	selected, other := t.TempDir(), t.TempDir()
	writeVaultFile(t, selected, "ok.md", "- [ ] fine\n")
	writeVaultFile(t, other, "elsewhere.md", "- [ ] late due:2026-11-03T25:00\n")
	out, _ := doctorText(t, selected)
	if strings.Contains(out, "elsewhere.md") || strings.Contains(out, "WARN") {
		t.Fatalf("another vault was scanned:\n%s", out)
	}
}

// A reversed range and a day that does not exist are listed.
func TestDoctorListsBadSkipItems(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	writeVaultFile(t, root, "s.md", "- [ ] away repeat:1w skip:2027-03-21..2027-03-13,2027-02-30,2027-04-01\n")
	out, _ := doctorText(t, root)
	for _, want := range []string{"s.md:1", "skip:2027-03-21..2027-03-13", "skip:2027-02-30"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "2027-04-01") {
		t.Fatalf("a valid skip day was reported:\n%s", out)
	}
}

// A reserved skip name is never a warning.
func TestDoctorIgnoresReservedSkipNames(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	writeVaultFile(t, root, "s.md", "- [ ] away repeat:1w skip:@holidays,@school\n")
	out, _ := doctorText(t, root)
	if strings.Contains(out, "WARN") || strings.Contains(out, "@holidays") {
		t.Fatalf("a reserved skip name was reported:\n%s", out)
	}
}

// doctor lists a bad time on a `due:` token with its reason, and says nothing
// about a `scheduled:` token or key.
func TestDoctorIgnoresScheduled(t *testing.T) {
	root := isolatedCLI(t)
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{
		"plan.md": "- [ ] a scheduled:2026-11-05T25:99\n- [ ] b due:2026-11-06T25:00\n",
		"task.md": "---\ntags: [task]\nscheduled: 2026-11-05T25:99\n---\n",
	}
	for name, body := range notes {
		if err := os.WriteFile(filepath.Join(root, "inbox", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check, found := taskTokenCheck(root)
	if !found {
		t.Fatal("no task token problem listed")
	}
	if len(check.Hint) != 1 || !strings.Contains(check.Hint[0], "due:2026-11-06T25:00") {
		t.Fatalf("hint: %q", check.Hint)
	}
	if strings.Contains(strings.Join(check.Hint, "\n"), "scheduled") {
		t.Fatalf("scheduled reported: %q", check.Hint)
	}
	if fields := strings.Fields(check.Hint[0]); len(fields) < 3 {
		t.Fatalf("no reason in %q", check.Hint[0])
	}
}
