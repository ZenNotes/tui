package tui

import (
	"io"
	"strings"
	"testing"
)

// noLatexTools puts an empty dir on PATH, so neither pdftex nor pdftocairo
// is found.
func noLatexTools(t *testing.T) {
	t.Helper()
	latexEnv(t)
	t.Setenv("PATH", t.TempDir())
}

func TestMissingTexShowsNoticeOnceAboveTheFirstBlock(t *testing.T) {
	noLatexTools(t)
	a, _ := newDatabaseTestApp(t)
	a.prefs = prefsView{Prefs: mathPrefs("latex", "kitty")}
	a.imageStore = &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard}
	watchRenders(t, a)
	body := "# Doc\n\n$$\nx^2\n$$\n\nBetween.\n\n\\[\ny^2\n\\]\n"
	a.latexNoticeShown = false
	out, err := a.renderGlamourLinesFrom(body, 100, "Doc.md")
	if err != nil {
		t.Fatal(err)
	}
	all := joinPreview(out)
	if n := strings.Count(all, "zn does not include them"); n != 1 {
		t.Fatalf("notice shown %d times, want once:\n%s", n, all)
	}
	for _, want := range []string{"pdftex (TeX Live or MacTeX)", "pdftocairo (poppler)", "zn doctor"} {
		if !strings.Contains(strings.Join(strings.Fields(all), " "), want) {
			t.Fatalf("notice lacks %q:\n%s", want, all)
		}
	}
	if notice, src := rowIndex(out, "LaTeX math needs"), rowIndex(out, "x^2"); notice < 0 || src < 0 || notice > src {
		t.Fatalf("notice is not above the first block (notice %d, source %d):\n%s", notice, src, all)
	}
	if !strings.Contains(all, "y^2") {
		t.Fatalf("the second block's source is gone:\n%s", all)
	}
}

func TestNoNoticeWhenTexIsInstalled(t *testing.T) {
	latexEnv(t)
	fakeTeX(t, texFake{})
	a, _ := newDatabaseTestApp(t)
	a.prefs = prefsView{Prefs: mathPrefs("latex", "kitty")}
	a.imageStore = &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard}
	watchRenders(t, a)
	out, err := a.renderGlamourLinesFrom("$$\nx^2\n$$\n", 100, "Doc.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joinPreview(out), "does not include") {
		t.Fatalf("notice shown with the tools on PATH:\n%s", joinPreview(out))
	}
}

func TestSwitchingToLatexWithoutTexWarns(t *testing.T) {
	noLatexTools(t)
	a, _ := newDatabaseTestApp(t)
	p := a.rawPref
	p.TerminalMath = "latex"
	a.applyPreferences(p)
	found := false
	for _, toast := range a.toasts {
		if toast.isErr && strings.Contains(toast.text, "zn doctor") && strings.Contains(toast.text, "pdftex") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning toast after switching to latex without TeX: %+v", a.toasts)
	}
}
