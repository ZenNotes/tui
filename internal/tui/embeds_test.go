package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestPlaceholderRowsEncodeIdRowAndColumn(t *testing.T) {
	rows := placeholderRows(0x0102A3, 3, 2)
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	if !strings.HasPrefix(rows[0], "\x1b[38;2;1;2;163m") {
		t.Fatalf("id goes into the foreground color: %q", rows[0])
	}
	cells := strings.Count(rows[1], "\U0010EEEE")
	if cells != 3 {
		t.Fatalf("one placeholder per column: %d", cells)
	}
	if !strings.Contains(rows[1], "\U0010EEEE"+string(kittyDiacritics[1])+string(kittyDiacritics[2])) {
		t.Fatalf("row 1 col 2 diacritics missing: %q", rows[1])
	}
	if len(kittyDiacritics) != 297 {
		t.Fatalf("diacritics table has %d entries", len(kittyDiacritics))
	}
}

func TestKittyTransmitChunksAndTmuxWrap(t *testing.T) {
	var buf bytes.Buffer
	data := bytes.Repeat([]byte{7}, 5000)
	if err := kittyTransmit(&buf, 12, 10, 5, data, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100,t=d,q=2,U=1,i=12,c=10,r=5,m=1;") {
		t.Fatalf("first chunk header: %q", out[:60])
	}
	if strings.Count(out, "\x1b_G") != 2 || !strings.Contains(out, "\x1b_Gm=0;") {
		t.Fatalf("two chunks, the last with m=0: %d", strings.Count(out, "\x1b_G"))
	}
	wrapped := kittyAPC("a=d,d=I,i=1", true)
	if !strings.HasPrefix(wrapped, "\x1bPtmux;\x1b\x1b_G") || !strings.HasSuffix(wrapped, "\x1b\x1b\\\x1b\\") {
		t.Fatalf("tmux passthrough: %q", wrapped)
	}
}

func TestToPNGKeepsPNGAndConvertsJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 12), 90, 255})
		}
	}
	var pngBuf, jpgBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	_ = jpeg.Encode(&jpgBuf, img, nil)
	out, w, h, err := toPNG(pngBuf.Bytes(), 1600)
	if err != nil || w != 40 || h != 20 || !bytes.Equal(out, pngBuf.Bytes()) {
		t.Fatalf("png passes through: %v %d %d", err, w, h)
	}
	out, w, h, err = toPNG(jpgBuf.Bytes(), 1600)
	if err != nil || w != 40 || h != 20 || !bytes.HasPrefix(out, []byte("\x89PNG")) {
		t.Fatalf("jpeg becomes png: %v %d %d", err, w, h)
	}
	out, w, h, err = toPNG(pngBuf.Bytes(), 20)
	if err != nil || w != 20 || h != 10 {
		t.Fatalf("wide pictures scale down: %v %d %d", err, w, h)
	}
	_ = out
}

func TestHighlightFencesColorsGoCode(t *testing.T) {
	lines := []string{"# Title", "```go", "func main() { fmt.Println(\"hi\") }", "```", "plain"}
	spans := highlightFences(lines, "gruvbox", "typst")
	if len(spans[2]) == 0 {
		t.Fatal("the code line gets spans")
	}
	if _, ok := spans[0]; ok {
		t.Fatal("prose is untouched")
	}
	first, ok := codeSpanAt(spans[2], 0)
	if !ok || first.fg == "" {
		t.Fatalf("func keyword colored: %+v", first)
	}
	if len(highlightFences([]string{"```", "x", "```"}, "gruvbox", "typst")) != 0 {
		t.Fatal("a fence without a language stays plain")
	}
}

// chromaColumns tokenises text with chroma's own lexer and returns, per line,
// the style of every column ("" where the style sets nothing), the way the
// editor paints code.
func chromaColumns(t *testing.T, lang, text string) [][]string {
	t.Helper()
	lexer := lexers.Get(lang)
	if lexer == nil {
		t.Fatalf("chroma has no %s lexer", lang)
	}
	iter, err := chroma.Coalesce(lexer).Tokenise(nil, text+"\n")
	if err != nil {
		t.Fatal(err)
	}
	style := styles.Get("gruvbox")
	out := [][]string{nil}
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		e := style.Get(tok.Type)
		key := ""
		if e.Colour.IsSet() || e.Bold == chroma.Yes || e.Italic == chroma.Yes {
			key = fmt.Sprintf("%s/%v/%v", e.Colour, e.Bold == chroma.Yes, e.Italic == chroma.Yes)
			if !e.Colour.IsSet() {
				key = fmt.Sprintf("/%v/%v", e.Bold == chroma.Yes, e.Italic == chroma.Yes)
			}
		}
		for _, r := range tok.Value {
			if r == '\n' {
				out = append(out, nil)
				continue
			}
			out[len(out)-1] = append(out[len(out)-1], key)
		}
	}
	return out
}

// spanColumns is the style of every column of a line as the editor paints it.
func spanColumns(spans []codeSpan, line string) []string {
	var out []string
	for col := range []rune(line) {
		key := ""
		if s, ok := codeSpanAt(spans, col); ok {
			key = fmt.Sprintf("%s/%v/%v", s.fg, s.bold, s.italic)
		}
		out = append(out, key)
	}
	return out
}

// assertMathBodyLexed checks that the body lines of the block at
// lines[open..close] are painted exactly as chroma's lang lexer paints the
// body, lexed alone or together with its delimiter lines. It also checks
// that the other engine's lexer paints this body differently, so the test
// tells the two engines apart.
func assertMathBodyLexed(t *testing.T, spans map[int][]codeSpan, lines []string, open, close int, lang, other string) {
	t.Helper()
	body := lines[open+1 : close]
	block := lines[open : close+1]
	alone := chromaColumns(t, lang, strings.Join(body, "\n"))
	whole := chromaColumns(t, lang, strings.Join(block, "\n"))[1:]
	otherAlone := chromaColumns(t, other, strings.Join(body, "\n"))
	otherWhole := chromaColumns(t, other, strings.Join(block, "\n"))[1:]
	matches := func(want [][]string) bool {
		for k, line := range body {
			if fmt.Sprint(spanColumns(spans[open+1+k], line)) != fmt.Sprint(want[k]) {
				return false
			}
		}
		return true
	}
	if matches(otherAlone) || matches(otherWhole) {
		t.Fatalf("the body is painted as the %s lexer paints it, want %s", other, lang)
	}
	if !matches(alone) && !matches(whole) {
		var got []string
		for k, line := range body {
			got = append(got, fmt.Sprint(spanColumns(spans[open+1+k], line)))
		}
		t.Fatalf("the body is not painted by chroma's %s lexer\n got: %v\nwant: %v\n  or: %v", lang, got, alone[:len(body)], whole[:len(body)])
	}
}

func TestHighlightDollarBodyWithTexUnderLatex(t *testing.T) {
	lines := []string{"Some prose", "", "$$", `\frac{a}{b} + x_1 = 2`, "$$", "", "after"}
	spans := highlightFences(lines, "gruvbox", "latex")
	assertMathBodyLexed(t, spans, lines, 2, 4, "tex", "typst")
	if _, ok := spans[0]; ok {
		t.Fatal("prose is untouched")
	}
}

func TestHighlightBracketBlockWithTex(t *testing.T) {
	lines := []string{"Some prose", "", `\[`, `\alpha^2 + \beta = 3`, `\]`, "", "after"}
	spans := highlightFences(lines, "gruvbox", "latex")
	assertMathBodyLexed(t, spans, lines, 2, 4, "tex", "typst")
	if _, ok := spans[0]; ok {
		t.Fatal("prose is untouched")
	}
}

func TestHighlightEnvironmentWithTex(t *testing.T) {
	lines := []string{"Some prose", "", `\begin{align}`, `a &= \sqrt{b} \\`, `c &= \frac{d}{e}`, `\end{align}`, "", "after"}
	spans := highlightFences(lines, "gruvbox", "latex")
	assertMathBodyLexed(t, spans, lines, 2, 5, "tex", "typst")
	if _, ok := spans[0]; ok {
		t.Fatal("prose is untouched")
	}
}

// $$ holds the desktop's KaTeX, so typst does not change how it is lexed:
// chroma's typst lexer would paint every backslash as an error.
func TestHighlightDollarBodyWithTexUnderTypst(t *testing.T) {
	lines := []string{"Some prose", "", "$$", `x = \frac{-b \pm \sqrt{b^2-4ac}}{2a}`, "$$", "", "after"}
	spans := highlightFences(lines, "gruvbox", "typst")
	assertMathBodyLexed(t, spans, lines, 2, 4, "tex", "typst")
	if _, ok := spans[0]; ok {
		t.Fatal("prose is untouched")
	}
}

func TestHighlightTikzFenceWithTex(t *testing.T) {
	lines := []string{"```tikz", `\begin{tikzpicture}`, `\draw (0,0) -- (1,1);`, `\end{tikzpicture}`, "```"}
	for _, engine := range []string{"typst", "latex"} {
		assertMathBodyLexed(t, highlightFences(lines, "gruvbox", engine), lines, 0, 4, "tex", "typst")
	}
}

func TestCodeSpansForRehighlightsWhenEngineChanges(t *testing.T) {
	a := &App{theme: buildTheme(true), prefs: prefsView{Prefs: config.Prefs{TerminalMath: "typst"}}}
	// \[ \] is display math only under latex, so its spans change with the
	// engine; $$ is lexed as tex under both.
	text := "\\[\n\\frac{a}{b}\n\\]"
	buf := &noteBuffer{ed: vim.New(text, vim.Options{}, vim.Hooks{})}
	lines := strings.Split(text, "\n")
	typst := fmt.Sprint(a.codeSpansFor(buf, lines))
	a.prefs.TerminalMath = "latex"
	latex := fmt.Sprint(a.codeSpansFor(buf, lines))
	if latex != fmt.Sprint(highlightFences(lines, a.theme.ChromaStyle, "latex")) || latex == typst {
		t.Fatalf("switching the engine keeps the cached spans\ntypst: %s\nlatex: %s", typst, latex)
	}
}

func TestAssetKindAndProviders(t *testing.T) {
	cases := map[string]string{"pic.PNG": "image", "deck.pdf": "pdf", "song.mp3": "audio", "clip.mov": "video", "sketch.excalidraw": "drawing", "Some Note": "note", "data.csv": "file"}
	for in, want := range cases {
		if got := assetKind(in); got != want {
			t.Errorf("%s: %s want %s", in, got, want)
		}
	}
	if p, ok := videoProvider("https://youtu.be/abc"); !ok || p != "YouTube" {
		t.Fatal("youtube")
	}
	if p, ok := videoProvider("https://vimeo.com/123"); !ok || p != "Vimeo" {
		t.Fatal("vimeo")
	}
	if _, ok := videoProvider("https://example.com/x.mp4"); ok {
		t.Fatal("other hosts are not video providers")
	}
}

func TestSplitBlocksFindsMath(t *testing.T) {
	blocks := splitBlocks([]string{"text", "$$", "x^2", "$$", "$$ y $$", "more"}, false)
	kinds := []string{}
	for _, b := range blocks {
		kinds = append(kinds, b.kind)
	}
	if strings.Join(kinds, ",") != "para,math,math,para" {
		t.Fatalf("kinds: %v", kinds)
	}
}

func TestPreviewRendersEmbedsAsCardsAndDiagrams(t *testing.T) {
	a, root := newDatabaseTestApp(t)
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	t.Setenv("TERM", "xterm-256color")
	if err := os.MkdirAll(filepath.Join(root, "attachements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "attachements", "deck.pdf"), []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Other.md"), []byte("# Other\n\nembedded body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshIndex()
	a.idx = &index{notes: []vault.NoteMeta{{Path: "Other.md", Title: "Other"}}, byPath: map[string]vault.NoteMeta{"Other.md": {Path: "Other.md", Title: "Other"}}}
	body := "# Doc\n\n![[deck.pdf]]\n\nhttps://youtu.be/abc123\n\n```mermaid\ngraph LR\n  A --> B\n```\n\n$$\nx^2 + y^2\n$$\n\n![[Other]]\n\n![[missing.png]]\n"
	lines, err := a.renderGlamourLinesFrom(body, 80, "Doc.md")
	if err != nil {
		t.Fatal(err)
	}
	plain := []string{}
	for _, l := range lines {
		plain = append(plain, stripAnsi(l.text))
	}
	all := strings.Join(plain, "\n")
	for _, want := range []string{"deck.pdf", "PDF", "YouTube video", "A", "B", "x^2 + y^2", "⟦ Other ⟧", "embedded body", "missing.png"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in:\n%s", want, all)
		}
	}
	var fileLink, urlLink bool
	for _, l := range lines {
		for _, ln := range l.links {
			if ln.kind == "file" && ln.target == "attachements/deck.pdf" {
				fileLink = true
			}
			if ln.kind == "url" && ln.target == "https://youtu.be/abc123" {
				urlLink = true
			}
		}
	}
	if !fileLink || !urlLink {
		t.Fatalf("cards carry links: file %v url %v", fileLink, urlLink)
	}
}

// --- shared runner with timeout and the shared render pool ---

// TestHelperSleep is the child process for TestRunToolKillsProcessAfterTimeout.
// With ZN_HELPER_SLEEP=1 it starts a grandchild that holds its output pipe
// (as Chromium does under mmdc) and then hangs; with 2 it only sleeps.
func TestHelperSleep(t *testing.T) {
	switch os.Getenv("ZN_HELPER_SLEEP") {
	case "1":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
		child.Env = append(os.Environ(), "ZN_HELPER_SLEEP=2")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		_ = child.Start()
		time.Sleep(time.Minute)
		os.Exit(0)
	case "2":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}

func TestRunToolKillsProcessAfterTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	// os.TempDir, not t.TempDir: Windows does not kill the helper's child,
	// which keeps its working directory busy until it exits.
	_, err := runTool(ctx, os.TempDir(), []string{"ZN_HELPER_SLEEP=1"}, os.Args[0], "-test.run=^TestHelperSleep$")
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("runTool returned after %v, want < 3s", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timed out error, got %v", err)
	}
	// A timeout must be told apart from a tool error.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error %v does not match context.DeadlineExceeded", err)
	}
}

type toolCall struct {
	name     string
	args     []string
	deadline time.Duration // time left on ctx when the call started; 0 when none
	input    string        // content of the .typ input, when any
}

// fakeTools swaps runTool for fn, which may block; every call is recorded.
// Any argument ending in .png is written as a small PNG, as the real tools do.
func fakeTools(t *testing.T, fn func(ctx context.Context) ([]byte, error)) func() []toolCall {
	t.Helper()
	var mu sync.Mutex
	calls := []toolCall{}
	inDir := func(dir, p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	prev := runTool
	runTool = func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
		c := toolCall{name: filepath.Base(name), args: append([]string(nil), args...)}
		if dl, ok := ctx.Deadline(); ok {
			c.deadline = time.Until(dl)
		}
		for _, arg := range args {
			if strings.HasSuffix(arg, ".typ") {
				b, _ := os.ReadFile(inDir(dir, arg))
				c.input = string(b)
			}
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		out, err := fn(ctx)
		if err != nil {
			return out, err
		}
		for _, arg := range args {
			if strings.HasSuffix(arg, ".png") {
				if err := os.WriteFile(inDir(dir, arg), tinyPNG(t), 0o644); err != nil {
					return nil, err
				}
			}
		}
		return out, nil
	}
	t.Cleanup(func() { runTool = prev })
	return func() []toolCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]toolCall(nil), calls...)
	}
}

func tinyPNG(t *testing.T) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 20))); err != nil {
		t.Error(err)
	}
	return buf.Bytes()
}

// renderWatch is a program model that reports each finished background render.
type renderWatch struct{ done chan string }

func (m renderWatch) Init() tea.Cmd { return nil }
func (m renderWatch) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if r, ok := msg.(embedRenderedMsg); ok {
		m.done <- r.key
	}
	return m, nil
}
func (m renderWatch) View() string { return "" }

// newRenderTestApp is an App with Kitty pictures on, the given prefs, the
// named tools present on PATH as stubs, and a program that reports renders.
func newRenderTestApp(t *testing.T, prefs config.Prefs, tools ...string) (*App, chan string) {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
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
	prefs.TerminalImages = "kitty"
	a := &App{
		theme:      buildTheme(true),
		prefs:      prefsView{Prefs: prefs},
		imageStore: &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard},
	}
	return a, watchRenders(t, a)
}

// watchRenders gives a a program that reports each finished background
// render on the returned channel.
func watchRenders(t *testing.T, a *App) chan string {
	t.Helper()
	done := make(chan string, 64)
	p := tea.NewProgram(renderWatch{done: done}, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	go func() { _, _ = p.Run() }()
	t.Cleanup(p.Kill)
	a.program = p
	return done
}

func waitRenders(t *testing.T, done chan string, n int, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-deadline:
			t.Fatalf("%d of %d renders finished within %v", i, n, within)
		}
	}
}

func joinPreview(lines []previewLine) string {
	plain := []string{}
	for _, l := range lines {
		plain = append(plain, stripAnsi(l.text))
	}
	return strings.Join(plain, "\n")
}

func isPicture(lines []previewLine) bool {
	return strings.Contains(joinPreview(lines), "\U0010EEEE")
}

func TestTypstRenderUsesUnchangedArgs(t *testing.T) {
	prefs := config.DefaultPrefs()
	prefs.TerminalMath = "typst"
	a, done := newRenderTestApp(t, prefs, "typst")
	calls := fakeTools(t, func(context.Context) ([]byte, error) { return nil, nil })
	a.mathLines("$$\nx^2\n$$", 0, 80)
	waitRenders(t, done, 1, 5*time.Second)
	got := calls()
	if len(got) != 1 || got[0].name != "typst" {
		t.Fatalf("want one typst call, got %+v", got)
	}
	args := got[0].args
	if len(args) != 7 || strings.Join(args[:5], " ") != "compile --format png --ppi 144" || !strings.HasSuffix(args[5], ".typ") || !strings.HasSuffix(args[6], ".png") {
		t.Fatalf("typst args changed: %q", args)
	}
	want := "#set page(width: auto, height: auto, margin: 8pt, fill: none)\n#set text(size: 18pt, fill: white)\n$ x^2 $\n"
	if got[0].input != want {
		t.Fatalf("typst document changed:\n%q\nwant\n%q", got[0].input, want)
	}
	if lines := a.mathLines("$$\nx^2\n$$", 0, 80); !isPicture(lines) {
		t.Fatalf("rendered block is a picture: %s", joinPreview(lines))
	}
}

func TestTypstTimeoutFallsBackToRawSource(t *testing.T) {
	prefs := config.DefaultPrefs()
	prefs.TerminalMath = "typst"
	prefs.RenderTimeout = 1
	a, done := newRenderTestApp(t, prefs, "typst")
	calls := fakeTools(t, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	a.mathLines("$$\nx^2 + y^2\n$$", 0, 80)
	waitRenders(t, done, 1, 5*time.Second)
	got := calls()
	if len(got) != 1 {
		t.Fatalf("want one typst call, got %+v", got)
	}
	if d := got[0].deadline; d <= 500*time.Millisecond || d > 1500*time.Millisecond {
		t.Fatalf("ctx deadline %v, want about render_timeout = 1s", d)
	}
	lines := a.mathLines("$$\nx^2 + y^2\n$$", 0, 80)
	if isPicture(lines) || !strings.Contains(joinPreview(lines), "x^2 + y^2") {
		t.Fatalf("timed out block shows raw source: %s", joinPreview(lines))
	}
}

func TestMermaidTimeoutFallsBackToText(t *testing.T) {
	prefs := config.DefaultPrefs()
	prefs.RenderTimeout = 1
	a, done := newRenderTestApp(t, prefs, "mmdc")
	calls := fakeTools(t, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	src := "graph LR\n  Alpha --> Beta"
	a.mermaidLines(src, 0, 80)
	waitRenders(t, done, 1, 5*time.Second)
	got := calls()
	if len(got) != 1 || got[0].name != "mmdc" {
		t.Fatalf("want one mmdc call, got %+v", got)
	}
	if d := got[0].deadline; d <= 500*time.Millisecond || d > 1500*time.Millisecond {
		t.Fatalf("ctx deadline %v, want about render_timeout = 1s", d)
	}
	lines := a.mermaidLines(src, 0, 80)
	all := joinPreview(lines)
	if isPicture(lines) || !strings.Contains(all, "Alpha") || !strings.Contains(all, "Beta") {
		t.Fatalf("timed out mermaid shows the text diagram: %s", all)
	}
}

func TestTypstErrorShowsRawSourceOnly(t *testing.T) {
	src := "$$\nx^2 + \\foo\n$$"
	t.Run("compile error", func(t *testing.T) {
		prefs := config.DefaultPrefs()
		prefs.TerminalMath = "typst"
		a, done := newRenderTestApp(t, prefs, "typst")
		fakeTools(t, func(context.Context) ([]byte, error) {
			return []byte("error: unknown variable: foo"), errors.New("exit status 1")
		})
		a.mathLines(src, 0, 80)
		waitRenders(t, done, 1, 5*time.Second)
		lines := a.mathLines(src, 0, 80)
		all := joinPreview(lines)
		if isPicture(lines) || len(lines) != 1 || !strings.Contains(all, "x^2 + \\foo") || strings.Contains(all, "typst") || strings.Contains(all, "error") {
			t.Fatalf("failed block shows only its raw source: %q", all)
		}
	})
	t.Run("typst not on PATH", func(t *testing.T) {
		prefs := config.DefaultPrefs()
		prefs.TerminalMath = "typst"
		a, _ := newRenderTestApp(t, prefs)
		calls := fakeTools(t, func(context.Context) ([]byte, error) { return nil, nil })
		for i := 0; i < 2; i++ {
			lines := a.mathLines(src, 0, 80)
			all := joinPreview(lines)
			if isPicture(lines) || len(lines) != 1 || !strings.Contains(all, "x^2 + \\foo") || strings.Contains(all, "typst") {
				t.Fatalf("block without typst shows only its raw source: %q", all)
			}
		}
		if n := len(calls()); n != 0 {
			t.Fatalf("no tool runs without typst on PATH: %d calls", n)
		}
	})
}

// inFlight is a fake tool body that holds each call for a while and
// records the most calls running at once.
func inFlight() (func(context.Context) ([]byte, error), func() int) {
	var mu sync.Mutex
	cur, peak := 0, 0
	body := func(context.Context) ([]byte, error) {
		mu.Lock()
		cur++
		peak = max(peak, cur)
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
		return nil, nil
	}
	return body, func() int {
		mu.Lock()
		defer mu.Unlock()
		return peak
	}
}

func TestRenderPoolCapsConcurrency(t *testing.T) {
	for _, tc := range []struct {
		name string
		cap  int // 0 keeps the default
		want int
	}{{"default 2", 0, 2}, {"configured 4", 4, 4}} {
		t.Run(tc.name, func(t *testing.T) {
			prefs := config.DefaultPrefs()
			prefs.TerminalMath = "typst"
			if tc.cap > 0 {
				prefs.RenderConcurrency = tc.cap
			}
			a, done := newRenderTestApp(t, prefs, "typst")
			body, peak := inFlight()
			fakeTools(t, body)
			for i := 0; i < 8; i++ {
				a.mathLines(fmt.Sprintf("$$\nx^%d\n$$", i), 0, 80)
			}
			waitRenders(t, done, 8, 10*time.Second)
			if got := peak(); got != tc.want {
				t.Fatalf("at most %d renders run at once, saw %d", tc.want, got)
			}
		})
	}
}

func TestRenderPoolSharedByMermaidAndMath(t *testing.T) {
	prefs := config.DefaultPrefs()
	prefs.TerminalMath = "typst"
	prefs.RenderConcurrency = 1
	a, done := newRenderTestApp(t, prefs, "typst", "mmdc")
	body, peak := inFlight()
	calls := fakeTools(t, body)
	a.mermaidLines("graph LR\n  A --> B", 0, 80)
	a.mathLines("$$\nx^2\n$$", 0, 80)
	waitRenders(t, done, 2, 10*time.Second)
	if n := len(calls()); n != 2 {
		t.Fatalf("want a mermaid and a math render, got %+v", calls())
	}
	if got := peak(); got != 1 {
		t.Fatalf("mermaid and math share one pool of 1, saw %d at once", got)
	}
}

// --- the math engine wired into the reading view ---
//
// These drive `$$` blocks through mathLines (and whole notes through the
// preview renderers), with pdftex and pdftocairo faked by fakeTeX and typst
// by fakeTypst. Block detection for \[…\] and environments is tested further down.

// fakeTypst wraps the current runTool: typst runs succeed and write their
// PNG, every other tool goes to the wrapped fake. It returns the typst run
// count.
func fakeTypst(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	n := 0
	prev := runTool
	runTool = func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
		if strings.TrimSuffix(filepath.Base(name), ".exe") != "typst" {
			return prev(ctx, dir, env, name, args...)
		}
		mu.Lock()
		n++
		mu.Unlock()
		for _, arg := range args {
			if strings.HasSuffix(arg, ".png") {
				p := arg
				if !filepath.IsAbs(p) {
					p = filepath.Join(dir, p)
				}
				if err := os.WriteFile(p, tinyPNG(t), 0o644); err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	}
	t.Cleanup(func() { runTool = prev })
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// toolPath is a stub executable's path in the single PATH dir latexEnv set.
func toolPath(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(os.Getenv("PATH"), name)
}

func addTool(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(toolPath(name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func removeTool(t *testing.T, name string) {
	t.Helper()
	if err := os.Remove(toolPath(name)); err != nil {
		t.Fatal(err)
	}
}

// newMathApp is an App with the given prefs, a dark theme and a program
// that reports finished renders.
func newMathApp(t *testing.T, prefs config.Prefs) (*App, chan string) {
	t.Helper()
	a := &App{
		theme:      buildTheme(true),
		prefs:      prefsView{Prefs: prefs},
		imageStore: &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard},
	}
	return a, watchRenders(t, a)
}

func mathPrefs(engine, images string) config.Prefs {
	p := config.DefaultPrefs()
	p.TerminalMath = engine
	p.TerminalImages = images
	return p
}

// showMath shows a block, waits up to wait for a background render, and
// shows it again: what the reader sees once the render is done.
func showMath(a *App, done chan string, body string, wait time.Duration) []previewLine {
	a.mathLines(body, 0, 80)
	nextRender(done, wait)
	return a.mathLines(body, 0, 80)
}

func rowIndex(lines []previewLine, sub string) int {
	for i, l := range lines {
		if strings.Contains(stripAnsi(l.text), sub) {
			return i
		}
	}
	return -1
}

func preambleExists() bool {
	_, err := os.Stat(config.PreamblePath())
	return err == nil
}

func TestMathEngineRouting(t *testing.T) {
	for _, tc := range []struct {
		name, terminal string
		latex          bool
	}{
		{"no math key", "images = \"kitty\"\n", false},
		{"typst", "math = \"typst\"\nimages = \"kitty\"\n", false},
		{"latex", "math = \"latex\"\nimages = \"kitty\"\n", true},
		{"unknown foo", "math = \"foo\"\nimages = \"kitty\"\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			latexEnv(t)
			addTool(t, "typst")
			if err := os.WriteFile(config.ConfigTomlPath(), []byte("[terminal]\n"+tc.terminal), 0o644); err != nil {
				t.Fatal(err)
			}
			prefs, _, err := config.LoadPrefs()
			if err != nil {
				t.Fatalf("LoadPrefs: %v", err)
			}
			calls := fakeTeX(t, texFake{})
			typst := fakeTypst(t)
			a, done := newMathApp(t, prefs)
			body := "$$\nx^2 + y^2\n$$"
			a.mathLines(body, 0, 80)
			if !nextRender(done, 5*time.Second) {
				t.Fatalf("no background render finished; typst runs %d, tex calls %+v", typst(), calls())
			}
			lines := a.mathLines(body, 0, 80)
			if tc.latex {
				if n := typst(); n != 0 {
					t.Fatalf("math = latex ran typst %d times, want 0", n)
				}
				if n := len(renderCalls(calls())); n != 1 {
					t.Fatalf("math = latex: %d pdftex renders, want 1; calls %+v", n, calls())
				}
			} else {
				if n := typst(); n != 1 {
					t.Fatalf("typst runs %d, want 1", n)
				}
				if got := calls(); len(got) != 0 {
					t.Fatalf("typst engine ran pdftex or pdftocairo: %+v", got)
				}
				if strings.Contains(joinPreview(lines), "latex") {
					t.Fatalf("typst engine shows a latex message: %s", joinPreview(lines))
				}
			}
			if !isPicture(lines) {
				t.Fatalf("rendered block is not a picture: %s", joinPreview(lines))
			}
		})
	}
}

func TestLatexImagesOffShowsSourceWithoutPdftex(t *testing.T) {
	for _, images := range []string{"off", "auto"} {
		t.Run(images, func(t *testing.T) {
			latexEnv(t)
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("KITTY_WINDOW_ID", "")
			t.Setenv("GHOSTTY_RESOURCES_DIR", "")
			t.Setenv("TERM", "xterm-256color")
			calls := fakeTeX(t, texFake{})
			a, done := newMathApp(t, mathPrefs("latex", images))
			lines := showMath(a, done, "$$\nx^2 + y^2\n$$", 300*time.Millisecond)
			if got := calls(); len(got) != 0 {
				t.Fatalf("pdftex ran with Kitty graphics off: %+v", got)
			}
			if isPicture(lines) || !strings.Contains(joinPreview(lines), "x^2 + y^2") {
				t.Fatalf("block is not shown as its raw source: %s", joinPreview(lines))
			}
			if preambleExists() {
				t.Fatalf("preamble.tex created with Kitty graphics off")
			}
		})
	}
}

func TestNoPreambleWithoutDisplayMath(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	a, _ := newDatabaseTestApp(t)
	a.prefs = prefsView{Prefs: mathPrefs("latex", "kitty")}
	a.imageStore = &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard}
	done := watchRenders(t, a)
	body := "# Doc\n\nInline $x^2$ only.\n\n- a list item\n\n```go\nfunc main() {}\n```\n"
	if _, err := a.renderGlamourLinesFrom(body, 80, "Doc.md"); err != nil {
		t.Fatal(err)
	}
	nextRender(done, 300*time.Millisecond)
	if preambleExists() {
		t.Fatalf("preamble.tex created for a note with no display math")
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("tools ran for a note with no display math: %+v", got)
	}
}

func TestNoPreambleUnderTypst(t *testing.T) {
	latexEnv(t)
	addTool(t, "typst")
	fakeTeX(t, texFake{})
	typst := fakeTypst(t)
	a, done := newMathApp(t, mathPrefs("typst", "kitty"))
	lines := showMath(a, done, "$$\nx^2\n$$", 5*time.Second)
	if typst() != 1 || !isPicture(lines) {
		t.Fatalf("typst did not render the block: %d runs, %s", typst(), joinPreview(lines))
	}
	if preambleExists() {
		t.Fatalf("preamble.tex created under math = typst")
	}
}

func TestMathKeyChangesWithEngineThemeAndPreamble(t *testing.T) {
	body := "$$\nx^2\n$$"
	t.Run("engine", func(t *testing.T) {
		latexEnv(t)
		addTool(t, "typst")
		calls := fakeTeX(t, texFake{})
		typst := fakeTypst(t)
		a, done := newMathApp(t, mathPrefs("typst", "kitty"))
		if lines := showMath(a, done, body, 5*time.Second); typst() != 1 || !isPicture(lines) {
			t.Fatalf("typst did not render the block: %d runs", typst())
		}
		a.prefs.TerminalMath = "latex"
		showMath(a, done, body, 5*time.Second)
		if n := len(renderCalls(calls())); n != 1 {
			t.Fatalf("the same block under latex after typst: %d pdftex renders, want 1 (typst entry reused)", n)
		}
	})
	t.Run("theme", func(t *testing.T) {
		latexEnv(t)
		calls := fakeTeX(t, texFake{})
		a, done := newMathApp(t, mathPrefs("latex", "kitty"))
		if lines := showMath(a, done, body, 5*time.Second); !isPicture(lines) {
			t.Fatalf("first show is not a picture; tex calls %+v", calls())
		}
		light := buildTheme(false)
		if strings.EqualFold(string(light.Fg), string(a.theme.Fg)) {
			t.Fatalf("light and dark themes share fg %s", light.Fg)
		}
		a.theme = light
		showMath(a, done, body, 5*time.Second)
		r := renderCalls(calls())
		if len(r) != 2 {
			t.Fatalf("after a theme change: %d pdftex renders in all, want 2", len(r))
		}
		hex := strings.ToLower(strings.TrimPrefix(string(light.Fg), "#"))
		if !strings.Contains(strings.ToLower(r[1].allTeX()), hex) {
			t.Fatalf("second render does not use the new theme colour %s:\n%s", hex, r[1].allTeX())
		}
	})
	t.Run("preamble", func(t *testing.T) {
		latexEnv(t)
		calls := fakeTeX(t, texFake{})
		a, done := newMathApp(t, mathPrefs("latex", "kitty"))
		if lines := showMath(a, done, body, 5*time.Second); !isPicture(lines) {
			t.Fatalf("first show is not a picture; tex calls %+v", calls())
		}
		pre, err := os.ReadFile(config.PreamblePath())
		if err != nil {
			t.Fatalf("preamble.tex after the first render: %v", err)
		}
		if err := os.WriteFile(config.PreamblePath(), append(pre, []byte("\n\\usepackage{xspace}\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		showMath(a, done, body, 5*time.Second)
		if n := len(renderCalls(calls())); n != 2 {
			t.Fatalf("after preamble.tex changed: %d pdftex renders in all, want 2", n)
		}
	})
}

func TestTypstWritesNothingUnderTexCache(t *testing.T) {
	_, texDir := latexEnv(t)
	addTool(t, "typst")
	fakeTeX(t, texFake{})
	typst := fakeTypst(t)
	a, done := newMathApp(t, mathPrefs("typst", "kitty"))
	if lines := showMath(a, done, "$$\nx^2\n$$", 5*time.Second); typst() != 1 || !isPicture(lines) {
		t.Fatalf("typst did not render the block: %d runs", typst())
	}
	written := []string{}
	_ = filepath.Walk(texDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			written = append(written, p)
		}
		return nil
	})
	if len(written) != 0 {
		t.Fatalf("typst render wrote under %s: %v", texDir, written)
	}
}

func TestLatexMissingToolsFallBack(t *testing.T) {
	for _, missing := range []string{"pdftex", "pdftocairo"} {
		t.Run("no "+missing, func(t *testing.T) {
			latexEnv(t)
			removeTool(t, missing)
			fakeTeX(t, texFake{})
			a, done := newMathApp(t, mathPrefs("latex", "kitty"))
			for i := 0; i < 2; i++ {
				lines := showMath(a, done, "$$\nx^2 + y^2\n$$", 500*time.Millisecond)
				all := joinPreview(lines)
				if isPicture(lines) || !strings.Contains(all, "x^2 + y^2") || strings.Contains(all, "latex:") {
					t.Fatalf("without %s the block shows only its raw source: %q", missing, all)
				}
			}
		})
	}
}

func TestLatexErrorRowAboveSource(t *testing.T) {
	// wantRow checks that the block is not a picture and that a row holding
	// msg sits above the row holding the block's source.
	wantRow := func(t *testing.T, lines []previewLine, msg, src string) {
		t.Helper()
		all := joinPreview(lines)
		if isPicture(lines) {
			t.Fatalf("failed block is a picture: %s", all)
		}
		e, s := rowIndex(lines, msg), rowIndex(lines, src)
		if e < 0 || s < 0 || e >= s {
			t.Fatalf("want a %q row above the source %q, got:\n%s", msg, src, all)
		}
	}
	t.Run("LaTeX error", func(t *testing.T) {
		latexEnv(t)
		log := "This is pdfTeX, Version 3.141592653\n! Undefined control sequence.\nl.3 \\nosuchmacro\n! Emergency stop.\n"
		fakeTeX(t, texFake{render: func(_ context.Context, base string) ([]byte, error) {
			_ = os.WriteFile(base+".log", []byte(log), 0o644)
			return []byte(log), errors.New("exit status 1")
		}})
		a, done := newMathApp(t, mathPrefs("latex", "kitty"))
		lines := showMath(a, done, "$$\n\\nosuchmacro\n$$", 5*time.Second)
		wantRow(t, lines, "latex: ! Undefined control sequence.", "\\nosuchmacro")
		if strings.Contains(joinPreview(lines), "Emergency stop") {
			t.Fatalf("error row is not only the first TeX error line: %s", joinPreview(lines))
		}
	})
	t.Run("broken preamble", func(t *testing.T) {
		latexEnv(t)
		if err := os.WriteFile(config.PreamblePath(), []byte("\\usepackage{amsmath}\n\\badmacro\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		log := "This is pdfTeX, Version 3.141592653\n! Undefined control sequence.\nl.2 \\badmacro\n! Emergency stop.\n"
		fakeTeX(t, texFake{build: func(context.Context) ([]byte, error) {
			return []byte(log), errors.New("exit status 1")
		}})
		a, done := newMathApp(t, mathPrefs("latex", "kitty"))
		for _, b := range []struct{ body, src string }{
			{"$$\nalpha_1\n$$", "alpha_1"},
			{"$$\nbeta_2\n$$", "beta_2"},
			{"$$ gamma_3 $$", "gamma_3"},
		} {
			wantRow(t, showMath(a, done, b.body, 5*time.Second), "! Undefined control sequence.", b.src)
		}
	})
	t.Run("unwritable config dir", func(t *testing.T) {
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
		a, done := newMathApp(t, mathPrefs("latex", "kitty"))
		wantRow(t, showMath(a, done, "$$\nx^2 + y^2\n$$", 5*time.Second), "permission denied", "x^2 + y^2")
	})
}

func TestZenStyleLeavesDollarBlockUnrendered(t *testing.T) {
	body := "# Doc\n\nBefore.\n\n$$\nx^2 + y^2\n$$\n\nAfter.\n"
	for _, engine := range []string{"typst", "latex"} {
		t.Run(engine, func(t *testing.T) {
			latexEnv(t)
			addTool(t, "typst")
			calls := fakeTeX(t, texFake{})
			typst := fakeTypst(t)
			a, _ := newDatabaseTestApp(t)
			prefs := mathPrefs(engine, "kitty")
			prefs.PreviewStyle = "zen"
			a.prefs = prefsView{Prefs: prefs}
			a.imageStore = &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard}
			done := watchRenders(t, a)
			a.renderPreviewLinesFrom(body, 80, "Doc.md")
			nextRender(done, 300*time.Millisecond)
			got := a.renderPreviewLinesFrom(body, 80, "Doc.md")
			if n := typst(); n != 0 || len(calls()) != 0 {
				t.Fatalf("zen style ran tools: typst %d, tex %+v", n, calls())
			}
			// Before this change: zen, typst, no pictures.
			b, _ := newDatabaseTestApp(t)
			base := mathPrefs("typst", "off")
			base.PreviewStyle = "zen"
			b.prefs = prefsView{Prefs: base}
			want := b.renderPreviewLines(body, 80)
			if len(got) != len(want) {
				t.Fatalf("zen output changed:\n%s\nwant\n%s", joinPreview(got), joinPreview(want))
			}
			for i := range got {
				if got[i].text != want[i].text {
					t.Fatalf("zen line %d changed: %q, want %q", i, got[i].text, want[i].text)
				}
			}
			if !strings.Contains(joinPreview(got), "x^2 + y^2") {
				t.Fatalf("zen output lacks the raw block: %s", joinPreview(got))
			}
		})
	}
}

// --- block detection for \[…\] and the display environments ---

// blockKinds is splitBlocks' result as "kind[start,end)" entries.
func blockKinds(src string, latex bool) []string {
	out := []string{}
	for _, b := range splitBlocks(strings.Split(src, "\n"), latex) {
		out = append(out, fmt.Sprintf("%s[%d,%d)", b.kind, b.start, b.end))
	}
	return out
}

func hasTexBlock(src string, latex bool) bool {
	for _, b := range splitBlocks(strings.Split(src, "\n"), latex) {
		if b.kind == "tex" {
			return true
		}
	}
	return false
}

// newNoteApp is a glamour-preview App with the given engine and images
// mode and a program that reports finished renders. Call latexEnv first.
func newNoteApp(t *testing.T, engine, images string) (*App, chan string) {
	t.Helper()
	a, _ := newDatabaseTestApp(t)
	a.prefs = prefsView{Prefs: mathPrefs(engine, images)}
	a.imageStore = &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard}
	return a, watchRenders(t, a)
}

// showNote shows a note, waits up to wait for one background render, and
// shows it again: what the reader sees once the render is done.
func showNote(a *App, done chan string, body string, wait time.Duration) []previewLine {
	a.renderPreviewLinesFrom(body, 80, "Doc.md")
	nextRender(done, wait)
	return a.renderPreviewLinesFrom(body, 80, "Doc.md")
}

// beforeChange is the note as shown before this change: typst, no pictures.
func beforeChange(t *testing.T, body string) []previewLine {
	t.Helper()
	b, _ := newDatabaseTestApp(t)
	b.prefs = prefsView{Prefs: mathPrefs("typst", "off")}
	return b.renderPreviewLinesFrom(body, 80, "Doc.md")
}

func sameLines(t *testing.T, got, want []previewLine) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("output changed:\n%s\nwant\n%s", joinPreview(got), joinPreview(want))
	}
	for i := range got {
		if got[i].text != want[i].text {
			t.Fatalf("line %d changed: %q, want %q", i, got[i].text, want[i].text)
		}
	}
}

// latexDisplaySources is one block-level source per detected form.
func latexDisplaySources() map[string]string {
	srcs := map[string]string{"\\[ \\]": "\\[\nx^2 + y^2\n\\]"}
	for _, env := range []string{"equation", "align", "gather", "multline", "alignat", "flalign", "eqnarray"} {
		for _, name := range []string{env, env + "*"} {
			begin := "\\begin{" + name + "}"
			if env == "alignat" {
				begin += "{2}"
			}
			srcs[name+""] = begin + "\na &= b + c\n\\end{" + name + "}"
		}
	}
	srcs["tikzpicture"] = "\\begin{tikzpicture}\n\\draw (0,0) -- (1,1);\n\\end{tikzpicture}"
	return srcs
}

func TestSplitBlocksLatexDisplay(t *testing.T) {
	for name, src := range latexDisplaySources() {
		t.Run(name, func(t *testing.T) {
			n := strings.Count(src, "\n") + 1
			note := "Before.\n\n" + src + "\n\nAfter."
			want := fmt.Sprintf("para[0,1) blank[1,2) tex[2,%d) blank[%d,%d) para[%d,%d)", 2+n, 2+n, 3+n, 3+n, 4+n)
			if got := strings.Join(blockKinds(note, true), " "); got != want {
				t.Fatalf("blocks %q, want %q", got, want)
			}
			// A paragraph also breaks before the block.
			tight := "Before.\n" + src + "\n\nAfter."
			wantTight := fmt.Sprintf("para[0,1) tex[1,%d) blank[%d,%d) para[%d,%d)", 1+n, 1+n, 2+n, 2+n, 3+n)
			if got := strings.Join(blockKinds(tight, true), " "); got != wantTight {
				t.Fatalf("after a paragraph line: blocks %q, want %q", got, wantTight)
			}

			latexEnv(t)
			calls := fakeTeX(t, texFake{})
			a, done := newNoteApp(t, "latex", "kitty")
			lines := showNote(a, done, note, 5*time.Second)
			if !isPicture(lines) {
				t.Fatalf("block is not rendered as a picture: %s; calls %+v", joinPreview(lines), calls())
			}
			if r := renderCalls(calls()); len(r) != 1 {
				t.Fatalf("%d pdftex renders, want 1; calls %+v", len(r), calls())
			}
			if in := renderInput(t, calls()); !strings.Contains(in, src) {
				t.Fatalf("pdftex input lacks the block source verbatim:\n%s", in)
			}
		})
	}
}

func TestSplitBlocksLatexInsideFenceIsCode(t *testing.T) {
	for name, src := range latexDisplaySources() {
		t.Run(name, func(t *testing.T) {
			note := "Before.\n\n```latex\n" + src + "\n```\n\nAfter."
			if hasTexBlock(note, true) {
				t.Fatalf("source inside a code fence detected as a tex block: %v", blockKinds(note, true))
			}
			latexEnv(t)
			calls := fakeTeX(t, texFake{})
			a, done := newNoteApp(t, "latex", "kitty")
			lines := showNote(a, done, note, 300*time.Millisecond)
			if got := calls(); len(got) != 0 {
				t.Fatalf("code fence content was rendered: %+v", got)
			}
			sameLines(t, lines, beforeChange(t, note))
		})
	}
}

func TestSplitBlocksDollarBlockOwnsEnvironment(t *testing.T) {
	for _, env := range []string{"align", "equation*", "tikzpicture"} {
		t.Run(env, func(t *testing.T) {
			note := "$$\n\\begin{" + env + "}\na &= b\n\\end{" + env + "}\n$$"
			if got, want := strings.Join(blockKinds(note, true), " "), "math[0,5)"; got != want {
				t.Fatalf("blocks %q, want %q", got, want)
			}
			latexEnv(t)
			calls := fakeTeX(t, texFake{})
			a, done := newNoteApp(t, "latex", "kitty")
			showNote(a, done, note, 5*time.Second)
			nextRender(done, 300*time.Millisecond)
			if r := renderCalls(calls()); len(r) != 1 {
				t.Fatalf("%d pdftex renders for one $$ block, want 1; calls %+v", len(r), calls())
			}
		})
	}
}

func TestSplitBlocksLatexNestedInListQuoteCallout(t *testing.T) {
	nest := map[string]func(src string) string{
		"list": func(src string) string {
			return "- item\n  " + strings.ReplaceAll(src, "\n", "\n  ")
		},
		"quote": func(src string) string {
			return "> " + strings.ReplaceAll(src, "\n", "\n> ")
		},
		"callout": func(src string) string {
			return "> [!note] Title\n> " + strings.ReplaceAll(src, "\n", "\n> ")
		},
	}
	srcs := latexDisplaySources()
	for _, name := range []string{"\\[ \\]", "align*", "tikzpicture"} {
		for where, wrap := range nest {
			t.Run(where+" "+name, func(t *testing.T) {
				note := "Before.\n\n" + wrap(srcs[name]) + "\n\nAfter."
				if hasTexBlock(note, true) {
					t.Fatalf("nested source detected as a tex block: %v", blockKinds(note, true))
				}
				latexEnv(t)
				calls := fakeTeX(t, texFake{})
				a, done := newNoteApp(t, "latex", "kitty")
				lines := showNote(a, done, note, 300*time.Millisecond)
				if got := calls(); len(got) != 0 {
					t.Fatalf("nested source was rendered: %+v", got)
				}
				sameLines(t, lines, beforeChange(t, note))
			})
		}
	}
}

func TestSplitBlocksSingleLineBracket(t *testing.T) {
	for _, tc := range []struct{ note, want string }{
		{"text\n\\[ x^2 \\]\nmore", "para[0,1) tex[1,2) para[2,3)"},
		{"text\n\n\\[ x^2 \\]\n\nmore", "para[0,1) blank[1,2) tex[2,3) blank[3,4) para[4,5)"},
		{"\\[ x^2 \\]", "tex[0,1)"},
	} {
		if got := strings.Join(blockKinds(tc.note, true), " "); got != tc.want {
			t.Fatalf("latex blocks for %q: %q, want %q", tc.note, got, tc.want)
		}
		if hasTexBlock(tc.note, false) {
			t.Fatalf("typst detects a tex block in %q: %v", tc.note, blockKinds(tc.note, false))
		}
	}
	for name, note := range map[string]string{
		"fence":     "text\n\n```latex\n\\[ x^2 \\]\n```\n\nmore",
		"list item": "- item\n  \\[ x^2 \\]\n\nmore",
		"list line": "- \\[ x^2 \\]\n\nmore",
	} {
		if hasTexBlock(note, true) {
			t.Fatalf("%s: one-line \\[ \\] detected as a tex block: %v", name, blockKinds(note, true))
		}
	}
}

func TestSplitBlocksTypstLeavesLatexAsText(t *testing.T) {
	// Block kinds as splitBlocks returned them before this change.
	for _, tc := range []struct{ note, want string }{
		{"Before.\n\n\\[\nx^2\n\\]\n\nAfter.", "para[0,1) blank[1,2) para[2,5) blank[5,6) para[6,7)"},
		{"Before.\n\\[\nx^2\n\\]\nAfter.", "para[0,5)"},
		{"Before.\n\n\\begin{align*}\na &= b\n\\end{align*}\n\nAfter.", "para[0,1) blank[1,2) para[2,5) blank[5,6) para[6,7)"},
		{"\\begin{tikzpicture}\n\\draw (0,0) -- (1,1);\n\\end{tikzpicture}", "para[0,3)"},
	} {
		if got := strings.Join(blockKinds(tc.note, false), " "); got != tc.want {
			t.Fatalf("typst blocks for %q: %q, want %q", tc.note, got, tc.want)
		}
	}
	for name, src := range latexDisplaySources() {
		t.Run(name, func(t *testing.T) {
			note := "Before.\n\n" + src + "\n\nAfter."
			if hasTexBlock(note, false) {
				t.Fatalf("typst detects a tex block: %v", blockKinds(note, false))
			}
			latexEnv(t)
			addTool(t, "typst")
			calls := fakeTeX(t, texFake{})
			typst := fakeTypst(t)
			a, done := newNoteApp(t, "typst", "kitty")
			lines := showNote(a, done, note, 300*time.Millisecond)
			if n := typst(); n != 0 || len(calls()) != 0 {
				t.Fatalf("typst engine rendered latex source: typst %d, tex %+v", n, calls())
			}
			sameLines(t, lines, beforeChange(t, note))
		})
	}
}

// Under latex a ```tikz fence renders through pdftex instead, as on the
// desktop (TestTikzFenceRendersThroughLatex); it is a fence block either way.
func TestTikzFenceStaysCodeBlockUnderTypst(t *testing.T) {
	note := "Before.\n\n```tikz\n\\begin{tikzpicture}\n\\draw (0,0) -- (1,1);\n\\end{tikzpicture}\n```\n\nAfter."
	if got, want := strings.Join(blockKinds(note, true), " "), "para[0,1) blank[1,2) fence[2,7) blank[7,8) para[8,9)"; got != want {
		t.Fatalf("latex blocks %q, want %q", got, want)
	}
	if got, want := strings.Join(blockKinds(note, false), " "), "para[0,1) blank[1,2) fence[2,7) blank[7,8) para[8,9)"; got != want {
		t.Fatalf("typst blocks %q, want %q", got, want)
	}
	latexEnv(t)
	addTool(t, "typst")
	calls := fakeTeX(t, texFake{})
	typst := fakeTypst(t)
	a, done := newNoteApp(t, "typst", "kitty")
	lines := showNote(a, done, note, 300*time.Millisecond)
	if n := typst(); n != 0 || len(calls()) != 0 {
		t.Fatalf("tikz fence was rendered: typst %d, tex %+v", n, calls())
	}
	if isPicture(lines) || !strings.Contains(joinPreview(lines), "\\draw (0,0) -- (1,1);") {
		t.Fatalf("tikz fence is not shown as a code block: %s", joinPreview(lines))
	}
	sameLines(t, lines, beforeChange(t, note))
}

func TestInlineMathUnchangedUnderLatex(t *testing.T) {
	note := "Inline $x^2$ and $\\alpha + \\beta$ in a line.\n\nAnd \\(y\\) too.\n"
	if hasTexBlock(note, true) {
		t.Fatalf("inline math detected as a tex block: %v", blockKinds(note, true))
	}
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	a, done := newNoteApp(t, "latex", "kitty")
	lines := showNote(a, done, note, 300*time.Millisecond)
	if got := calls(); len(got) != 0 {
		t.Fatalf("inline math was rendered: %+v", got)
	}
	sameLines(t, lines, beforeChange(t, note))
}

func tikzFence() ([]string, mdBlock) {
	lines := []string{"```tikz", "\\begin{tikzpicture}", "\\draw (0,0) -- (1,1);", "\\end{tikzpicture}", "```"}
	return lines, mdBlock{kind: "fence", start: 0, end: len(lines)}
}

func TestTikzFenceRendersThroughLatex(t *testing.T) {
	latexEnv(t)
	calls := fakeTeX(t, texFake{})
	a, done := newMathApp(t, mathPrefs("latex", "kitty"))
	lines, b := tikzFence()
	if _, ok := a.embedBlockLines(b, lines, 80, ""); ok {
		t.Fatal("the fence is a picture before its render finished")
	}
	if !nextRender(done, 5*time.Second) {
		t.Fatalf("no render finished; calls %+v", calls())
	}
	out, ok := a.embedBlockLines(b, lines, 80, "")
	if !ok || !isPicture(out) {
		t.Fatalf("the ```tikz fence is not a picture under latex:\n%s", joinPreview(out))
	}
	doc := renderInput(t, calls())
	if !strings.Contains(doc, "\\begin{document}") || !strings.Contains(doc, "\\draw (0,0) -- (1,1);") || strings.Contains(doc, "```") {
		t.Fatalf("pdftex did not get the fence body as a document:\n%s", doc)
	}
}

func TestFailedTikzFenceShowsTheErrorAboveTheCode(t *testing.T) {
	latexEnv(t)
	fakeTeX(t, texFake{render: func(ctx context.Context, base string) ([]byte, error) {
		_ = os.WriteFile(base+".log", []byte("! Undefined control sequence.\n"), 0o644)
		return []byte("! Undefined control sequence."), errors.New("exit status 1")
	}})
	a, _ := newDatabaseTestApp(t)
	a.prefs = prefsView{Prefs: mathPrefs("latex", "kitty")}
	a.imageStore = &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: io.Discard}
	done := watchRenders(t, a)
	body := strings.Join(func() []string { l, _ := tikzFence(); return l }(), "\n")
	if _, err := a.renderGlamourLinesFrom(body, 80, "Doc.md"); err != nil {
		t.Fatal(err)
	}
	if !nextRender(done, 5*time.Second) {
		t.Fatal("no render finished")
	}
	out, err := a.renderGlamourLinesFrom(body, 80, "Doc.md")
	if err != nil {
		t.Fatal(err)
	}
	errRow, codeRow := rowIndex(out, "latex: ! Undefined control sequence."), rowIndex(out, "\\draw (0,0)")
	if errRow < 0 || codeRow < 0 || errRow > codeRow {
		t.Fatalf("want the TeX error above the fence's code (error row %d, code row %d):\n%s", errRow, codeRow, joinPreview(out))
	}
}
