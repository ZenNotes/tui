package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
)

// --- LaTeX pipeline, part 1: preamble, document, format, sandbox ---
//
// The pipeline is driven through the synchronous seam renderEmbed with a
// renderReq of kind "latex" whose src is the block's whole source, $$
// delimiters included. External tools are faked through runTool.

// texCall is one recorded pdftex or pdftocairo run.
type texCall struct {
	name     string
	args     []string
	env      []string
	tex      map[string]string // every .tex file in the working dir when the call started
	deadline time.Duration     // time left on ctx when the call started; 0 when none
}

func (c texCall) isBuild() bool {
	if c.name != "pdftex" {
		return false
	}
	for _, a := range c.args {
		if a == "-ini" || a == "--ini" {
			return true
		}
	}
	return false
}

func (c texCall) isRender() bool { return c.name == "pdftex" && !c.isBuild() }

// allTeX is the content of every .tex file the call saw, in name order.
func (c texCall) allTeX() string {
	names := make([]string, 0, len(c.tex))
	for n := range c.tex {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(c.tex[n])
		b.WriteString("\n")
	}
	return b.String()
}

// flagValue returns the value of -flag=value or -flag value.
func (c texCall) flagValue(flag string) (string, bool) {
	for i, a := range c.args {
		for _, p := range []string{"-" + flag, "--" + flag} {
			if strings.HasPrefix(a, p+"=") {
				return strings.TrimPrefix(a, p+"="), true
			}
			if a == p && i+1 < len(c.args) {
				return c.args[i+1], true
			}
		}
	}
	return "", false
}

func (c texCall) envValue(key string) (string, bool) {
	for _, kv := range c.env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// texFake configures the fake tools.
type texFake struct {
	// build, when set, replaces a successful format build.
	build func(ctx context.Context) ([]byte, error)
	// buildDelay holds each successful build for a while.
	buildDelay time.Duration
	// render, when set, replaces a successful pdftex render. base is the
	// render's .tex path in its working dir, without the extension.
	render func(ctx context.Context, base string) ([]byte, error)
	// cairo, when set, replaces a successful pdftocairo run.
	cairo func(ctx context.Context) ([]byte, error)
	// png, when set, is the picture pdftocairo writes instead of tinyPNG.
	png []byte
}

// fakeTeX swaps runTool for fake pdftex and pdftocairo. A format build
// writes <jobname>.fmt, a render writes <name>.pdf and <name>.log, and
// pdftocairo writes <prefix>.png, each in the call's working dir.
func fakeTeX(t *testing.T, f texFake) func() []texCall {
	t.Helper()
	var mu sync.Mutex
	calls := []texCall{}
	inDir := func(dir, p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	prev := runTool
	runTool = func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
		c := texCall{
			name: strings.TrimSuffix(filepath.Base(name), ".exe"),
			args: append([]string(nil), args...),
			env:  append([]string(nil), env...),
			tex:  map[string]string{},
		}
		if dl, ok := ctx.Deadline(); ok {
			c.deadline = time.Until(dl)
		}
		texArgs := []string{}
		for _, a := range args {
			if strings.HasSuffix(a, ".tex") {
				texArgs = append(texArgs, a)
			}
		}
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".tex") {
					b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
					c.tex[e.Name()] = string(b)
				}
			}
		}
		for _, a := range texArgs {
			if b, err := os.ReadFile(inDir(dir, a)); err == nil {
				c.tex[filepath.Base(a)] = string(b)
			}
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()

		switch {
		case c.isBuild():
			if f.build != nil {
				return f.build(ctx)
			}
			if f.buildDelay > 0 {
				select {
				case <-time.After(f.buildDelay):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			job, ok := c.flagValue("jobname")
			if !ok && len(texArgs) > 0 {
				job = strings.TrimSuffix(filepath.Base(texArgs[len(texArgs)-1]), ".tex")
			}
			if err := os.WriteFile(inDir(dir, job+".fmt"), []byte("fake format"), 0o644); err != nil {
				return nil, err
			}
			return []byte("Beginning to dump on file " + job + ".fmt\n"), nil
		case c.isRender():
			if len(texArgs) == 0 {
				return []byte("! Emergency stop."), errors.New("exit status 1")
			}
			base := strings.TrimSuffix(inDir(dir, texArgs[len(texArgs)-1]), ".tex")
			if f.render != nil {
				return f.render(ctx, base)
			}
			if err := os.WriteFile(base+".pdf", []byte("%PDF-1.5 fake"), 0o644); err != nil {
				return nil, err
			}
			_ = os.WriteFile(base+".log", []byte("This is pdfTeX\nOutput written on m.pdf\n"), 0o644)
			return []byte("Output written on m.pdf (1 page)."), nil
		case c.name == "pdftocairo":
			if f.cairo != nil {
				return f.cairo(ctx)
			}
			pic := f.png
			if pic == nil {
				pic = tinyPNG(t)
			}
			wrote := false
			for _, a := range args {
				if strings.HasSuffix(a, ".png") {
					if err := os.WriteFile(inDir(dir, a), pic, 0o644); err != nil {
						return nil, err
					}
					wrote = true
				}
			}
			if last := args[len(args)-1]; !wrote && !strings.HasPrefix(last, "-") && !strings.HasSuffix(last, ".pdf") {
				if err := os.WriteFile(inDir(dir, last)+".png", pic, 0o644); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected tool %s", c.name)
	}
	t.Cleanup(func() { runTool = prev })
	return func() []texCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]texCall(nil), calls...)
	}
}

// latexEnv points the config dir and the user cache dir at temp dirs, puts
// stub pdftex and pdftocairo on PATH, and returns the config dir and the
// expected <UserCacheDir>/zn/tex.
func latexEnv(t *testing.T) (cfgDir, texDir string) {
	t.Helper()
	cfgDir = t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", cfgDir)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	t.Setenv("LocalAppData", cache)
	ucd, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, tool := range []string{"pdftex", "pdftocairo"} {
		name := tool
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return cfgDir, filepath.Join(ucd, "zn", "tex")
}

// newLatexApp is a fresh App (a new zn session) under math = "latex" with
// Kitty pictures on and a dark theme. It reuses the current env.
func newLatexApp(t *testing.T, concurrency int) *App {
	t.Helper()
	prefs := config.DefaultPrefs()
	prefs.TerminalMath = "latex"
	prefs.TerminalImages = "kitty"
	if concurrency > 0 {
		prefs.RenderConcurrency = concurrency
	}
	return &App{
		theme:      buildTheme(true),
		prefs:      prefsView{Prefs: prefs},
		imageStore: &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard},
	}
}

func renderLatex(a *App, src string, timeout time.Duration) *termImage {
	return a.renderEmbed(contentKey("math", "latex\x00"+src), renderReq{kind: "latex", src: src, dark: true, maxCols: 80, timeout: timeout})
}

func renderCalls(calls []texCall) []texCall {
	out := []texCall{}
	for _, c := range calls {
		if c.isRender() {
			out = append(out, c)
		}
	}
	return out
}

func buildCalls(calls []texCall) []texCall {
	out := []texCall{}
	for _, c := range calls {
		if c.isBuild() {
			out = append(out, c)
		}
	}
	return out
}

// renderInput is the document pdftex rendered, or fails the test.
func renderInput(t *testing.T, calls []texCall) string {
	t.Helper()
	r := renderCalls(calls)
	if len(r) == 0 {
		t.Fatalf("pdftex rendered nothing; calls: %+v", calls)
	}
	return r[len(r)-1].allTeX()
}

func fmtFiles(t *testing.T, texDir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(texDir, "*.fmt"))
	return m
}

func failed(img *termImage) bool { return img == nil || img.err != nil }

func imgErr(img *termImage) error {
	if img == nil {
		return errors.New("<nil picture>")
	}
	return img.err
}

func TestLatexDocumentIsStandaloneWithBody(t *testing.T) {
	for _, src := range []string{
		"\\[ a + b = c \\]",
		"\\begin{align}\n  a &= b \\\\\n  c &= d\n\\end{align}",
		"\\begin{tikzpicture}\n  \\draw (0,0) -- (1,1);\n\\end{tikzpicture}",
	} {
		t.Run(firstLine(src), func(t *testing.T) {
			latexEnv(t)
			calls := fakeTeX(t, texFake{})
			renderLatex(newLatexApp(t, 0), src, 10*time.Second)
			got := calls()
			doc := renderInput(t, got)
			for _, b := range buildCalls(got) {
				doc = b.allTeX() + "\n" + doc
			}
			if !regexp.MustCompile(`\\documentclass(\[[^\]]*\])?\{standalone\}`).MatchString(doc) {
				t.Fatalf("document is not standalone-class:\n%s", doc)
			}
			body := renderInput(t, got)
			begin, end := strings.Index(body, `\begin{document}`), strings.LastIndex(body, `\end{document}`)
			at := strings.Index(body, src)
			if begin < 0 || end < 0 || at < begin || at > end {
				t.Fatalf("body is not the verbatim block source inside the document:\n%s", body)
			}
		})
	}
}

func TestLatexDollarBlockWrappedAsDisplay(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"one line", "$$ x^2 $$", `\\\[\s*x\^2\s*\\\]`},
		{"multi line", "$$\na^2\n+ b^2\n$$", `\\\[\s*a\^2\n\+ b\^2\s*\\\]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			latexEnv(t)
			calls := fakeTeX(t, texFake{})
			renderLatex(newLatexApp(t, 0), tc.src, 10*time.Second)
			body := renderInput(t, calls())
			if strings.Contains(body, "$$") {
				t.Fatalf("$$ reached pdftex:\n%s", body)
			}
			if !regexp.MustCompile(tc.want).MatchString(body) {
				t.Fatalf("inner source not wrapped as \\[ … \\]:\n%s", body)
			}
		})
	}
}

func TestLatexThemeColourAfterPreambleNotInFile(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	a := newLatexApp(t, 0)
	renderLatex(a, "\\[ x \\]", 10*time.Second)
	got := calls()
	hex := strings.ToLower(strings.TrimPrefix(string(a.theme.Fg), "#"))
	if len(hex) != 6 {
		t.Fatalf("theme fg %q is not a hex colour", a.theme.Fg)
	}
	body := strings.ToLower(renderInput(t, got))
	at := strings.Index(body, hex)
	if !strings.Contains(body, `\color`) || at < 0 {
		t.Fatalf("render document sets no \\color for theme fg %s:\n%s", hex, body)
	}
	if b := strings.Index(body, `\begin{document}`); b < 0 || at < b {
		t.Fatalf("theme colour is not set after the preamble:\n%s", body)
	}
	pre, err := os.ReadFile(config.PreamblePath())
	if err != nil {
		t.Fatalf("preamble.tex: %v", err)
	}
	if strings.Contains(string(pre), `\color`) || strings.Contains(strings.ToLower(string(pre)), hex) {
		t.Fatalf("preamble.tex holds the theme colour:\n%s", pre)
	}
}

func TestEnsurePreambleWritesDefaultPackages(t *testing.T) {
	latexEnv(t)
	fakeTeX(t, texFake{})
	if _, err := os.Stat(config.PreamblePath()); !os.IsNotExist(err) {
		t.Fatalf("preamble.tex exists before the first render: %v", err)
	}
	renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second)
	b, err := os.ReadFile(config.PreamblePath())
	if err != nil {
		t.Fatalf("first latex render creates preamble.tex: %v", err)
	}
	pre := string(b)
	for _, pkg := range []string{"amsmath", "amssymb", "mhchem", "tikz"} {
		re := regexp.MustCompile(`(?m)^[^%\n]*\\usepackage(\[[^\]]*\])?\{[^}]*\b` + pkg + `\b[^}]*\}`)
		if !re.MatchString(pre) {
			t.Errorf("preamble.tex does not load %s:\n%s", pkg, pre)
		}
	}
	// The desktop's TikZ renderer loads these libraries for every fence, so a
	// fence written there draws here without a \usetikzlibrary line.
	if !strings.Contains(pre, `\usetikzlibrary{arrows.meta,calc,positioning,shapes,decorations.pathreplacing,intersections,patterns}`) {
		t.Errorf("preamble.tex does not load the desktop's TikZ libraries:\n%s", pre)
	}
}

func TestEnsurePreambleKeepsEditedBytes(t *testing.T) {
	latexEnv(t)
	fakeTeX(t, texFake{})
	mine := []byte("% my preamble\r\n\\usepackage{amsmath}\n\\newcommand{\\R}{\\mathbb{R}}")
	if err := os.WriteFile(config.PreamblePath(), mine, 0o600); err != nil {
		t.Fatal(err)
	}
	a := newLatexApp(t, 0)
	renderLatex(a, "\\[ x \\]", 10*time.Second)
	renderLatex(a, "$$ y $$", 10*time.Second)
	got, err := os.ReadFile(config.PreamblePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(mine) {
		t.Fatalf("edited preamble.tex changed:\n%q\nwant\n%q", got, mine)
	}
}

// Parallel first renders must never read a half-written preamble.tex, or
// they build a second format from a truncated preamble.
func TestEnsurePreambleParallelFirstReadsSeeWholeFile(t *testing.T) { // risk: single-flight
	for round := 0; round < 20; round++ {
		latexEnv(t)
		var wg sync.WaitGroup
		got := make([]string, 8)
		for i := range got {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				b, err := ensurePreamble()
				if err != nil {
					got[i] = "error: " + err.Error()
					return
				}
				got[i] = string(b)
			}(i)
		}
		wg.Wait()
		for i, g := range got {
			if g != defaultPreamble {
				t.Fatalf("round %d, reader %d got %q, want the default preamble", round, i, g)
			}
		}
	}
}

func TestEnsurePreambleReportsWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directories via chmod are not enforced on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only directories")
	}
	cfg, _ := latexEnv(t)
	fakeTeX(t, texFake{})
	if err := os.Chmod(cfg, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfg, 0o755) })
	img := renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second)
	if !failed(img) {
		t.Fatalf("render without a writable preamble.tex is not a picture")
	}
	if img == nil || img.err == nil || !strings.Contains(img.err.Error(), "permission denied") {
		t.Fatalf("render error carries the preamble write error, got %v", imgErr(img))
	}
}

func TestFormatBuiltIntoCacheAndUsed(t *testing.T) {
	_, texDir := latexEnv(t)
	calls := fakeTeX(t, texFake{})
	renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second)
	got := calls()
	if n := len(buildCalls(got)); n != 1 {
		t.Fatalf("want one format build, got %d; calls %+v", n, got)
	}
	fmts := fmtFiles(t, texDir)
	if len(fmts) != 1 {
		t.Fatalf("want one .fmt in %s, got %v", texDir, fmts)
	}
	name := strings.TrimSuffix(filepath.Base(fmts[0]), ".fmt")
	r := renderCalls(got)
	if len(r) != 1 {
		t.Fatalf("want one pdftex render, got %+v", got)
	}
	f, ok := r[0].flagValue("fmt")
	if !ok || strings.TrimSuffix(filepath.Base(f), ".fmt") != name {
		t.Fatalf("render does not use format %s: args %q", name, r[0].args)
	}
	if !filepath.IsAbs(f) {
		v, _ := r[0].envValue("TEXFORMATS")
		found := false
		for _, p := range filepath.SplitList(v) {
			if filepath.Clean(p) == filepath.Clean(texDir) {
				found = true
			}
		}
		if !found {
			t.Fatalf("render cannot find the format: TEXFORMATS=%q, want %s on it", v, texDir)
		}
	}
}

func TestFormatNotRebuiltAcrossSessions(t *testing.T) {
	_, texDir := latexEnv(t)
	calls := fakeTeX(t, texFake{})
	first := newLatexApp(t, 0)
	renderLatex(first, "\\[ x \\]", 10*time.Second)
	renderLatex(first, "\\[ y \\]", 10*time.Second)
	if n := len(buildCalls(calls())); n != 1 {
		t.Fatalf("one session builds the format once, got %d builds", n)
	}
	before := fmtFiles(t, texDir)
	renderLatex(newLatexApp(t, 0), "\\[ z \\]", 10*time.Second)
	got := calls()
	if n := len(buildCalls(got)); n != 1 {
		t.Fatalf("a later session rebuilt the format: %d builds in total", n)
	}
	if len(renderCalls(got)) != 3 {
		t.Fatalf("want three renders, got %+v", got)
	}
	if after := fmtFiles(t, texDir); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("format files changed: %v -> %v", before, after)
	}
}

func TestFormatRebuiltWhenPreambleChanges(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	a := newLatexApp(t, 0)
	renderLatex(a, "\\[ x \\]", 10*time.Second)
	pre, err := os.ReadFile(config.PreamblePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.PreamblePath(), append(pre, []byte("\n\\usepackage{bm}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	renderLatex(a, "\\[ \\bm{y} \\]", 10*time.Second)
	got := calls()
	builds := buildCalls(got)
	if len(builds) != 2 {
		t.Fatalf("changed preamble rebuilds the format: %d builds", len(builds))
	}
	if !strings.Contains(builds[1].allTeX(), `\usepackage{bm}`) {
		t.Fatalf("rebuild does not use the new preamble:\n%s", builds[1].allTeX())
	}
	r := renderCalls(got)
	if len(r) != 2 {
		t.Fatalf("want two renders, got %+v", got)
	}
	f0, _ := r[0].flagValue("fmt")
	f1, _ := r[1].flagValue("fmt")
	if f0 == f1 {
		t.Fatalf("render after the change still uses format %q", f1)
	}
	// The rebuild comes before the render that needs it.
	bi, ri := -1, -1
	for i, c := range got {
		if c.isBuild() {
			bi = i
		}
		if c.isRender() {
			ri = i
		}
	}
	if bi > ri {
		t.Fatalf("format rebuilt after the render: calls %+v", got)
	}
}

func TestFormatBuiltOnceForParallelRenders(t *testing.T) { // risk: single-flight
	_, texDir := latexEnv(t)
	calls := fakeTeX(t, texFake{buildDelay: 300 * time.Millisecond})
	a := newLatexApp(t, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			renderLatex(a, fmt.Sprintf("\\[ x^%d \\]", i), 10*time.Second)
		}(i)
	}
	wg.Wait()
	got := calls()
	if n := len(buildCalls(got)); n != 1 {
		t.Fatalf("parallel first renders build the format once, got %d builds", n)
	}
	if n := len(renderCalls(got)); n != 4 {
		t.Fatalf("want four renders, got %d", n)
	}
	if fmts := fmtFiles(t, texDir); len(fmts) != 1 {
		t.Fatalf("want one .fmt in the cache, got %v", fmts)
	}
}

func TestFormatErrorFailsEveryBlockWithFirstLine(t *testing.T) {
	latexEnv(t)
	if err := os.WriteFile(config.PreamblePath(), []byte("\\usepackage{amsmath}\n\\badmacro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := "This is pdfTeX, Version 3.141592653\n" +
		"! Undefined control sequence.\n" +
		"l.2 \\badmacro\n" +
		"! Emergency stop.\n"
	fakeTeX(t, texFake{build: func(context.Context) ([]byte, error) {
		return []byte(log), errors.New("exit status 1")
	}})
	a := newLatexApp(t, 0)
	for _, src := range []string{"\\[ x \\]", "$$ y $$", "\\begin{align} a &= b \\end{align}"} {
		img := renderLatex(a, src, 10*time.Second)
		if !failed(img) {
			t.Fatalf("%q renders as a picture with a broken preamble", src)
		}
		if img == nil || img.err == nil || !strings.Contains(img.err.Error(), "! Undefined control sequence.") {
			t.Fatalf("%q: error names the first TeX error line, got %v", src, imgErr(img))
		}
		if strings.Contains(img.err.Error(), "Emergency stop") {
			t.Fatalf("%q: error is not only the first line: %v", src, img.err)
		}
	}
}

func TestFormatBuildTimeoutFailsBlocks(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{build: func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	a := newLatexApp(t, 0)
	start := time.Now()
	for _, src := range []string{"\\[ x \\]", "\\[ y \\]"} {
		if img := renderLatex(a, src, time.Second); !failed(img) {
			t.Fatalf("%q renders as a picture after the format build timed out", src)
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("timed out builds took %v", d)
	}
	builds := buildCalls(calls())
	if len(builds) == 0 {
		t.Fatalf("no format build ran")
	}
	if d := builds[0].deadline; d <= 0 || d > 1500*time.Millisecond {
		t.Fatalf("format build ctx deadline %v, want about render_timeout = 1s", d)
	}
}

func TestPdftexRunsWithNoShellEscapeAndParanoidIO(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	renderLatex(newLatexApp(t, 0), "\\[ \\immediate\\write18{touch pwned} x \\]", 10*time.Second)
	got := calls()
	if len(buildCalls(got)) == 0 || len(renderCalls(got)) == 0 {
		t.Fatalf("want a format build and a render, got %+v", got)
	}
	for _, c := range got {
		if c.name != "pdftex" {
			continue
		}
		kind := "render"
		if c.isBuild() {
			kind = "build"
		}
		args := " " + strings.Join(c.args, " ") + " "
		if !strings.Contains(args, " -no-shell-escape ") {
			t.Errorf("%s: pdftex args lack -no-shell-escape: %q", kind, c.args)
		}
		for _, bad := range []string{"-shell-escape", "--shell-escape", "-enable-write18", "--enable-write18", "-shell-restricted", "--shell-restricted"} {
			if strings.Contains(args, " "+bad+" ") {
				t.Errorf("%s: pdftex args allow shell escape (%s): %q", kind, bad, c.args)
			}
		}
		for _, key := range []string{"openin_any", "openout_any"} {
			if v, ok := c.envValue(key); !ok || v != "p" {
				t.Errorf("%s: env %s=%q, want p (env %q)", kind, key, v, c.env)
			}
		}
	}
}

// --- LaTeX pipeline, part 2: rasterise, errors, timeouts, disk cache ---

func cairoCalls(calls []texCall) []texCall {
	out := []texCall{}
	for _, c := range calls {
		if c.name == "pdftocairo" {
			out = append(out, c)
		}
	}
	return out
}

func pngFiles(texDir string) []string {
	m, _ := filepath.Glob(filepath.Join(texDir, "*.png"))
	return m
}

// mustPicture fails the test unless img is a picture with no error.
func mustPicture(t *testing.T, img *termImage) {
	t.Helper()
	if failed(img) {
		t.Fatalf("latex render is not a picture: %v", imgErr(img))
	}
}

// blockOnCtx is a fake tool run that never finishes on its own: it returns
// only once ctx is done, as a killed process would.
func blockOnCtx(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("fake tool was never killed")
	}
}

func TestPdftocairoTransparentPNG(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	img := renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second)
	got := calls()
	c := cairoCalls(got)
	if len(c) != 1 {
		t.Fatalf("want one pdftocairo run after pdftex, got calls %+v", got)
	}
	args := " " + strings.Join(c[0].args, " ") + " "
	for _, flag := range []string{"-png", "-transp"} {
		if !strings.Contains(args, " "+flag+" ") {
			t.Errorf("pdftocairo args lack %s: %q", flag, c[0].args)
		}
	}
	pdf := false
	for _, a := range c[0].args {
		if strings.HasSuffix(a, ".pdf") {
			pdf = true
		}
	}
	if !pdf {
		t.Errorf("pdftocairo is not given the PDF pdftex wrote: %q", c[0].args)
	}
	mustPicture(t, img)
}

func TestLatexErrorFirstLogLine(t *testing.T) {
	latexEnv(t)
	log := "This is pdfTeX, Version 3.141592653\n" +
		"! Undefined control sequence.\n" +
		"l.3 \\[ \\nosuchmacro\n" +
		"! Emergency stop.\n"
	calls := fakeTeX(t, texFake{render: func(_ context.Context, base string) ([]byte, error) {
		_ = os.WriteFile(base+".log", []byte(log), 0o644)
		return []byte(log), errors.New("exit status 1")
	}})
	img := renderLatex(newLatexApp(t, 0), "\\[ \\nosuchmacro \\]", 10*time.Second)
	if !failed(img) {
		t.Fatalf("a block with a LaTeX error renders as a picture")
	}
	if got, want := img.err.Error(), "latex: ! Undefined control sequence."; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if n := len(cairoCalls(calls())); n != 0 {
		t.Fatalf("pdftocairo ran after pdftex failed: %d runs", n)
	}
}

func TestLatexRenderTimeoutDefault10s(t *testing.T) {
	latexEnv(t)
	timeout := time.Duration(config.DefaultPrefs().RenderTimeout) * time.Second
	calls := fakeTeX(t, texFake{render: func(ctx context.Context, _ string) ([]byte, error) {
		// Blocking for 10s would slow the suite: the deadline is checked
		// below, and the run ends as a kill would.
		if _, ok := ctx.Deadline(); !ok {
			return blockOnCtx(ctx)
		}
		return nil, context.DeadlineExceeded
	}})
	img := renderLatex(newLatexApp(t, 0), "\\def\\x{\\x}\\x", timeout)
	if !failed(img) {
		t.Fatalf("a render that never ends is a picture")
	}
	r := renderCalls(calls())
	if len(r) != 1 {
		t.Fatalf("want one pdftex render, got %d", len(r))
	}
	if d := r[0].deadline; d < 9*time.Second || d > 10*time.Second {
		t.Fatalf("pdftex render ctx deadline %v, want about the default 10s", d)
	}
}

func TestLatexRenderTimeoutConfigured(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{render: func(ctx context.Context, _ string) ([]byte, error) {
		return blockOnCtx(ctx)
	}})
	start := time.Now()
	img := renderLatex(newLatexApp(t, 0), "\\def\\x{\\x}\\x", 2*time.Second)
	took := time.Since(start)
	if !failed(img) {
		t.Fatalf("a render that never ends is a picture")
	}
	if took > 4*time.Second {
		t.Fatalf("render_timeout = 2 killed pdftex only after %v", took)
	}
	r := renderCalls(calls())
	if len(r) != 1 {
		t.Fatalf("want one pdftex render, got %d", len(r))
	}
	if d := r[0].deadline; d <= 0 || d > 2*time.Second {
		t.Fatalf("pdftex render ctx deadline %v, want about render_timeout = 2s", d)
	}
}

func TestPdftocairoTimeoutFallsBack(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{cairo: blockOnCtx})
	start := time.Now()
	img := renderLatex(newLatexApp(t, 0), "\\[ x \\]", time.Second)
	took := time.Since(start)
	c := cairoCalls(calls())
	if len(c) != 1 {
		t.Fatalf("want one pdftocairo run, got %d", len(c))
	}
	if !failed(img) {
		t.Fatalf("a pdftocairo run that never ends gives a picture")
	}
	if took > 3*time.Second {
		t.Fatalf("render_timeout = 1 killed pdftocairo only after %v", took)
	}
	if d := c[0].deadline; d <= 0 || d > time.Second {
		t.Fatalf("pdftocairo ctx deadline %v, want at most render_timeout = 1s", d)
	}
}

func TestLatexPNGWrittenToDiskCache(t *testing.T) {
	_, texDir := latexEnv(t)
	fakeTeX(t, texFake{})
	mustPicture(t, renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second))
	if p := pngFiles(texDir); len(p) != 1 {
		t.Fatalf("want one PNG under %s after a render, got %v", texDir, p)
	}
}

func TestLatexDiskCacheHitSkipsPdftex(t *testing.T) {
	_, texDir := latexEnv(t)
	fakeTeX(t, texFake{})
	mustPicture(t, renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second))
	if p := pngFiles(texDir); len(p) == 0 {
		t.Fatalf("no PNG under %s after the first session", texDir)
	}
	calls := fakeTeX(t, texFake{}) // a restart: fresh App, fresh call log
	img := renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second)
	if got := calls(); len(got) != 0 {
		t.Fatalf("a cached block ran tools after a restart: %+v", got)
	}
	mustPicture(t, img)
}

func TestLatexRebuildsAfterCacheDeleted(t *testing.T) {
	_, texDir := latexEnv(t)
	fakeTeX(t, texFake{})
	mustPicture(t, renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second))
	if err := os.RemoveAll(texDir); err != nil {
		t.Fatal(err)
	}
	calls := fakeTeX(t, texFake{})
	img := renderLatex(newLatexApp(t, 0), "\\[ x \\]", 10*time.Second)
	got := calls()
	if len(buildCalls(got)) != 1 || len(renderCalls(got)) != 1 || len(cairoCalls(got)) != 1 {
		t.Fatalf("want a format build, a render and a pdftocairo run, got %+v", got)
	}
	mustPicture(t, img)
	if len(fmtFiles(t, texDir)) != 1 {
		t.Fatalf("format not rebuilt into %s", texDir)
	}
	if len(pngFiles(texDir)) != 1 {
		t.Fatalf("PNG not rewritten into %s", texDir)
	}
}

// --- failure memo: a timeout is retried, a TeX error is reused ---

// showLatex is one show of a latex block through the in-memory picture
// cache: nil while the background render runs, then its result.
func showLatex(a *App, src string) *termImage {
	return a.externalImage(contentKey("math", "latex\x00"+src), 80, "latex", src)
}

// nextRender waits for one background render to finish; false if none
// finishes within d.
func nextRender(done chan string, d time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestFormatTimeoutIsRetried(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{build: blockOnCtx})
	a := newLatexApp(t, 0)
	for _, src := range []string{"\\[ x \\]", "\\[ y \\]"} {
		if img := renderLatex(a, src, 300*time.Millisecond); !failed(img) {
			t.Fatalf("%q renders as a picture after the format build timed out", src)
		}
	}
	if n := len(buildCalls(calls())); n != 2 {
		t.Fatalf("a later block after a timed out format build: %d build calls, want 2", n)
	}
}

// timeoutCases are the two tools whose own render a timeout can kill.
func timeoutCases() []struct {
	tool string
	fake texFake
	runs func([]texCall) []texCall
} {
	return []struct {
		tool string
		fake texFake
		runs func([]texCall) []texCall
	}{
		{"pdftex", texFake{render: func(ctx context.Context, _ string) ([]byte, error) { return blockOnCtx(ctx) }}, renderCalls},
		{"pdftocairo", texFake{cairo: blockOnCtx}, cairoCalls},
	}
}

// timedOutNote is a note app whose one latex block, in Doc.md, has been
// shown once and timed out, then redrawn; it fails the test unless the
// redraw kept showing the raw source without rendering again.
func timedOutNote(t *testing.T, tool string, fake texFake, runs func([]texCall) []texCall, inTab bool) (*App, chan string, func() []texCall, string) {
	t.Helper()
	latexEnv(t)
	calls := fakeTeX(t, fake)
	a, done := newNoteApp(t, "latex", "kitty")
	a.prefs.RenderTimeout = 1
	src := "\\[ x \\]"
	if _, err := a.backend.WriteNote(context.Background(), "Doc.md", "Before.\n\n"+src+"\n\nAfter.\n"); err != nil {
		t.Fatal(err)
	}
	if inTab {
		a.openNote("Doc.md", true)
	}
	showLatex(a, src)
	if !nextRender(done, 5*time.Second) {
		t.Fatalf("the first show started no background render that finished; tool calls: %d", len(calls()))
	}
	if n := len(runs(calls())); n != 1 {
		t.Fatalf("first show: %d %s runs, want 1", n, tool)
	}
	// A redraw, as after another block finishes rendering.
	if img := showLatex(a, src); !failed(img) {
		t.Fatalf("redraw after a %s timeout shows a picture, want the raw source", tool)
	}
	nextRender(done, 300*time.Millisecond)
	if n := len(runs(calls())); n != 1 {
		t.Fatalf("redraw after a %s timeout: %d %s runs, want 1 (a redraw alone does not render again)", tool, n, tool)
	}
	return a, done, calls, src
}

func TestLatexRenderTimeoutIsRetried(t *testing.T) {
	for _, tc := range timeoutCases() {
		t.Run(tc.tool, func(t *testing.T) {
			a, done, calls, src := timedOutNote(t, tc.tool, tc.fake, tc.runs, false)
			a.openNote("Doc.md", false) // the note is opened again
			showLatex(a, src)
			nextRender(done, 5*time.Second)
			if n := len(tc.runs(calls())); n != 2 {
				t.Fatalf("show after reopening the note: %d %s runs, want 2 (rendered again)", n, tc.tool)
			}
		})
	}
}

func TestLatexTimeoutForgottenByOpenNoteQuiet(t *testing.T) {
	for _, tc := range timeoutCases() {
		t.Run(tc.tool, func(t *testing.T) {
			a, done, calls, src := timedOutNote(t, tc.tool, tc.fake, tc.runs, true)
			a.openNoteQuiet("Doc.md") // already in a tab
			showLatex(a, src)
			nextRender(done, 5*time.Second)
			if n := len(tc.runs(calls())); n != 2 {
				t.Fatalf("show after reopening the note in its tab: %d %s runs, want 2 (rendered again)", n, tc.tool)
			}
		})
	}
}

func TestSingleLineBracketRendersThroughPdftex(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	a, done := newNoteApp(t, "latex", "kitty")
	src := "\\[ x^2 \\]"
	lines := showNote(a, done, "Before.\n\n"+src+"\n\nAfter.", 5*time.Second)
	got := calls()
	if r := renderCalls(got); len(r) != 1 {
		t.Fatalf("%d pdftex renders for a one-line \\[ \\] block, want 1; calls %+v", len(r), got)
	}
	body := renderInput(t, got)
	// The body starts after the \begin{document} line, which also carries
	// the theme's \color.
	begin, end := strings.Index(body, `\begin{document}`), strings.LastIndex(body, `\end{document}`)
	nl := -1
	if begin >= 0 {
		nl = strings.Index(body[begin:], "\n")
	}
	if begin < 0 || end < 0 || nl < 0 || begin+nl > end || strings.TrimSpace(body[begin+nl:end]) != src {
		t.Fatalf("document body is not the verbatim %q:\n%s", src, body)
	}
	if !isPicture(lines) {
		t.Fatalf("one-line block is not rendered as a picture: %s", joinPreview(lines))
	}
}

func TestFormatTexErrorIsReused(t *testing.T) {
	latexEnv(t)
	log := "This is pdfTeX, Version 3.141592653\n" +
		"! LaTeX Error: File `nosuch.sty' not found.\n" +
		"! Emergency stop.\n"
	calls := fakeTeX(t, texFake{build: func(context.Context) ([]byte, error) {
		return []byte(log), errors.New("exit status 1")
	}})
	a := newLatexApp(t, 0)
	first := renderLatex(a, "\\[ x \\]", 10*time.Second)
	if !failed(first) || first == nil {
		t.Fatalf("a block renders as a picture with a broken preamble")
	}
	n := len(buildCalls(calls()))
	second := renderLatex(a, "\\[ y \\]", 10*time.Second)
	if !failed(second) || second == nil {
		t.Fatalf("a second block renders as a picture with a broken preamble")
	}
	if more := len(buildCalls(calls())) - n; more != 0 {
		t.Fatalf("a second block under the same preamble ran %d more format builds, want 0", more)
	}
	if first.err.Error() != second.err.Error() || !strings.Contains(second.err.Error(), "! LaTeX Error") {
		t.Fatalf("second block error %q, want the remembered %q", second.err, first.err)
	}
}

func TestLatexRenderTexErrorIsReused(t *testing.T) {
	latexEnv(t)
	log := "This is pdfTeX, Version 3.141592653\n" +
		"! Undefined control sequence.\n" +
		"! Emergency stop.\n"
	calls := fakeTeX(t, texFake{render: func(_ context.Context, base string) ([]byte, error) {
		_ = os.WriteFile(base+".log", []byte(log), 0o644)
		return []byte(log), errors.New("exit status 1")
	}})
	a := newLatexApp(t, 0)
	done := watchRenders(t, a)
	src := "\\[ \\nosuchmacro \\]"
	showLatex(a, src)
	if !nextRender(done, 5*time.Second) {
		t.Fatalf("the first show started no background render that finished; tool calls: %d", len(calls()))
	}
	const want = "latex: ! Undefined control sequence."
	for i := 0; i < 2; i++ { // the note is reopened, then the preview redraws
		if img := showLatex(a, src); img == nil || img.err == nil || img.err.Error() != want {
			t.Fatalf("show %d: want the remembered %q, got %v", i+2, want, imgErr(img))
		}
	}
	nextRender(done, 300*time.Millisecond)
	if n := len(renderCalls(calls())); n != 1 {
		t.Fatalf("showing a failed block again ran pdftex %d times in all, want 1", n)
	}
}

// --- crop display PNGs to their ink ---

// inkPNG is a w×h transparent PNG with each rect filled opaque white.
func inkPNG(t *testing.T, w, h int, ink ...image.Rectangle) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for _, r := range ink {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	return img
}

// inkBox is the bounding box of the pixels with alpha > 0, relative to
// the image's top-left corner.
func inkBox(img image.Image) image.Rectangle {
	b := img.Bounds()
	box := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				box = box.Union(image.Rect(x-b.Min.X, y-b.Min.Y, x-b.Min.X+1, y-b.Min.Y+1))
			}
		}
	}
	return box
}

func TestCropToInk(t *testing.T) {
	const border = 8
	cases := []struct {
		name         string
		w, h         int
		ink          []image.Rectangle
		wantW, wantH int
		wantInk      image.Rectangle // ink box in the result
	}{
		{
			name: "transparent_margins_removed",
			w:    200, h: 100,
			ink:   []image.Rectangle{image.Rect(90, 45, 110, 55)},
			wantW: 20 + 2*border, wantH: 10 + 2*border,
			wantInk: image.Rect(border, border, border+20, border+10),
		},
		{
			name: "border_kept_and_clamped",
			w:    200, h: 100,
			ink:   []image.Rectangle{image.Rect(3, 45, 23, 55)},
			wantW: 3 + 20 + border, wantH: 10 + 2*border,
			wantInk: image.Rect(3, border, 23, border+10),
		},
		{
			name: "fully_transparent_unchanged",
			w:    200, h: 100,
			wantW: 200, wantH: 100,
		},
		{
			name: "opaque_edges_untouched",
			w:    60, h: 40,
			ink: []image.Rectangle{
				image.Rect(0, 0, 60, 1), image.Rect(0, 39, 60, 40),
				image.Rect(0, 0, 1, 40), image.Rect(59, 0, 60, 40),
				image.Rect(20, 15, 30, 25),
			},
			wantW: 60, wantH: 40,
			wantInk: image.Rect(0, 0, 60, 40),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := inkPNG(t, tc.w, tc.h, tc.ink...)
			out, err := cropToInk(in, border)
			if err != nil {
				t.Fatalf("cropToInk: %v", err)
			}
			got := decodePNG(t, out)
			if b := got.Bounds(); b.Dx() != tc.wantW || b.Dy() != tc.wantH {
				t.Fatalf("cropped to %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.wantW, tc.wantH)
			}
			if box := inkBox(got); box != tc.wantInk {
				t.Fatalf("ink at %v in the result, want %v", box, tc.wantInk)
			}
			if tc.name == "opaque_edges_untouched" {
				orig := decodePNG(t, in)
				ob, gb := orig.Bounds(), got.Bounds()
				for y := 0; y < tc.h; y++ {
					for x := 0; x < tc.w; x++ {
						a := color.NRGBAModel.Convert(orig.At(ob.Min.X+x, ob.Min.Y+y))
						b := color.NRGBAModel.Convert(got.At(gb.Min.X+x, gb.Min.Y+y))
						if a != b {
							t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, b, a)
						}
					}
				}
			}
		})
	}
}

func TestLatexKeyVersionedForCrop(t *testing.T) {
	_, texDir := latexEnv(t)
	if _, err := ensurePreamble(); err != nil {
		t.Fatal(err)
	}
	const src, fg = "\\[ x \\]", "#cdd6f4"
	format := currentLatexFormat()
	// An uncropped PNG left by an earlier run under the pre-crop name.
	planted := inkPNG(t, 400, 100, image.Rect(190, 45, 210, 55))
	old := strings.TrimPrefix(contentKey("math", "latex\x00"+fg+"\x00"+format+"\x00"+src), "math:")
	if err := os.MkdirAll(texDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(texDir, old+".png"), planted, 0o644); err != nil {
		t.Fatal(err)
	}
	// pdftocairo's m.png is the same wide picture, written by the fake
	// render beside the PDF.
	calls := fakeTeX(t, texFake{
		render: func(_ context.Context, base string) ([]byte, error) {
			if err := os.WriteFile(base+".pdf", []byte("%PDF-1.5 fake"), 0o644); err != nil {
				return nil, err
			}
			_ = os.WriteFile(base+".log", []byte("This is pdfTeX\nOutput written on m.pdf\n"), 0o644)
			if err := os.WriteFile(filepath.Join(filepath.Dir(base), "m.png"), planted, 0o644); err != nil {
				return nil, err
			}
			return []byte("Output written on m.pdf (1 page)."), nil
		},
		cairo: func(context.Context) ([]byte, error) { return nil, nil },
	})
	a := newLatexApp(t, 0)
	img := a.renderEmbed(latexKey(fg, format, src), renderReq{kind: "latex", src: src, dark: true, fg: fg, maxCols: 80, timeout: 10 * time.Second})
	mustPicture(t, img)
	if n := len(renderCalls(calls())); n != 1 {
		t.Fatalf("the pre-crop cached PNG was served: %d pdftex renders, want 1", n)
	}
	wantW, wantH := 20+2*latexCropBorder, 10+2*latexCropBorder
	if img.width != wantW || img.height != wantH {
		t.Fatalf("picture is %dx%d px, want cropped %dx%d", img.width, img.height, wantW, wantH)
	}
	for _, p := range pngFiles(texDir) {
		if strings.TrimSuffix(filepath.Base(p), ".png") == old {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := decodePNG(t, b).Bounds(); got.Dx() != wantW || got.Dy() != wantH {
			t.Fatalf("disk cache %s is %dx%d, want cropped %dx%d", p, got.Dx(), got.Dy(), wantW, wantH)
		}
		return
	}
	t.Fatalf("no new PNG beside the pre-crop one under %s: %v", texDir, pngFiles(texDir))
}

func TestOverfullBy(t *testing.T) {
	log := []byte("This is pdfTeX\n" +
		"Overfull \\hbox (12.5pt too wide) detected at line 3\n" +
		"Overfull \\hbox (395.19788pt too wide) detected at line 2\n" +
		"Overfull \\vbox (80.0pt too high) has occurred while \\output is active\n")
	if got := overfullBy(log); got != 395.19788 {
		t.Fatalf("overfullBy = %v, want the widest hbox, 395.19788", got)
	}
	if got := overfullBy([]byte("This is pdfTeX\nOutput written on m.pdf\n")); got != 0 {
		t.Fatalf("overfullBy of a clean log = %v, want 0", got)
	}
}

func TestOverfullBlockRendersAgainOnAWiderPage(t *testing.T) {
	latexEnv(t)
	n := 0
	calls := fakeTeX(t, texFake{render: func(ctx context.Context, base string) ([]byte, error) {
		n++
		log := "This is pdfTeX\n"
		if n == 1 {
			log += "Overfull \\hbox (395.19788pt too wide) detected at line 2\n"
		}
		if err := os.WriteFile(base+".pdf", []byte("%PDF-1.5 fake"), 0o644); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(base+".log", []byte(log), 0o644)
	}})
	if img := renderLatex(newLatexApp(t, 0), `\[ a+b+c \]`, 10*time.Second); failed(img) {
		t.Fatalf("render: %v", imgErr(img))
	}
	r := renderCalls(calls())
	if len(r) != 2 {
		t.Fatalf("pdftex rendered %d times, want 2 (the second on a wider page)", len(r))
	}
	const wider = `\standaloneconfig{varwidth=\dimexpr\linewidth+397pt\relax}`
	if strings.Contains(r[0].allTeX(), wider) {
		t.Fatalf("the first render already widened the page:\n%s", r[0].allTeX())
	}
	second := r[1].allTeX()
	if !strings.HasPrefix(second, wider+"\n") || !strings.Contains(second, `a+b+c`) {
		t.Fatalf("the second render does not widen the page by the overflow:\n%s", second)
	}
}

func TestFittingBlockRendersOnce(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	if img := renderLatex(newLatexApp(t, 0), `\[ x \]`, 10*time.Second); failed(img) {
		t.Fatalf("render: %v", imgErr(img))
	}
	if r := renderCalls(calls()); len(r) != 1 {
		t.Fatalf("pdftex rendered %d times for a block that fits, want 1", len(r))
	}
}

func TestTikzDocumentNormalisesLikeTheDesktop(t *testing.T) {
	const fg = "#EBDBB2"
	body := texBody
	for _, tc := range []struct{ name, src, want string }{
		{
			"library line stays in the preamble",
			"\\usetikzlibrary{positioning}\n\\begin{tikzpicture}\n\\node (a) {A};\n\\end{tikzpicture}",
			"\\usetikzlibrary{positioning}\n" + body("\\begin{tikzpicture}\n\\node (a) {A};\n\\end{tikzpicture}", fg),
		},
		{
			"documentclass goes, a full document keeps its preamble",
			"\\documentclass[tikz]{standalone}\n\\usepackage{amsmath}\n\\begin{document}\n\\tikz \\draw (0,0) -- (1,1);\n\\end{document}",
			"\\usepackage{amsmath}\n" + body("\\tikz \\draw (0,0) -- (1,1);", fg),
		},
		{
			"bare commands are all body",
			"\\draw (0,0) -- (1,1);",
			body("\\draw (0,0) -- (1,1);", fg),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tikzDocument(tc.src, fg); got != tc.want {
				t.Fatalf("tikzDocument:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestPruneLatexCacheDropsWhatNoRenderUsed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	age := func(name string, d time.Duration, isDir bool) {
		p := filepath.Join(dir, name)
		var err error
		if isDir {
			err = os.Mkdir(p, 0o755)
		} else {
			err = os.WriteFile(p, []byte("x"), 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-d), now.Add(-d)); err != nil {
			t.Fatal(err)
		}
	}
	day := 24 * time.Hour
	age("zn-old.fmt", 40*day, false)
	age("zn-cur.fmt", time.Minute, false)
	age("old.png", 40*day, false)
	age("new.png", day, false)
	age("build-dead", 2*time.Hour, true)
	age("png-dead", 2*time.Hour, false)
	age("build-live", time.Minute, true)
	age("notes.txt", 40*day, false)
	pruneLatexCache(dir, now)
	left := []string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if got, want := strings.Join(left, " "), "build-live new.png notes.txt zn-cur.fmt"; got != want {
		t.Fatalf("left %q, want %q", got, want)
	}
}

func TestLatexRenderTouchesWhatItUses(t *testing.T) {
	_, texDir := latexEnv(t)
	fakeTeX(t, texFake{})
	if img := renderLatex(newLatexApp(t, 0), `\[ x \]`, 10*time.Second); failed(img) {
		t.Fatalf("render: %v", imgErr(img))
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	files := append(fmtFiles(t, texDir), pngFiles(texDir)...)
	if len(files) != 2 {
		t.Fatalf("want one format and one PNG, got %v", files)
	}
	for _, f := range files {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	// A new session reads the cached PNG, then renders another block on the
	// same format.
	a := newLatexApp(t, 0)
	for _, src := range []string{`\[ x \]`, `\[ y \]`} {
		if img := renderLatex(a, src, 10*time.Second); failed(img) {
			t.Fatalf("render %s: %v", src, imgErr(img))
		}
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(info.ModTime()) > time.Hour {
			t.Errorf("%s was used but not touched, so the prune would delete it", filepath.Base(f))
		}
	}
}
