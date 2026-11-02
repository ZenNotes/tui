package cli

import (
	"encoding/json"
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
