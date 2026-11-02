package tui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/mcp"
)

// --- real pdftex and pdftocairo ---
//
// These run the real tools found on PATH. Each test skips when either tool
// is missing (CI has no TeX) or on windows. runTool is not swapped.

// requireRealTeX skips t unless both real tools are on PATH.
func requireRealTeX(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("real TeX tests do not run on windows")
	}
	for _, tool := range []string{"pdftex", "pdftocairo"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

// realTeXEnv points the config dir and the user cache dir at temp dirs and
// keeps the real PATH. It returns the config dir and <UserCacheDir>/zn/tex.
func realTeXEnv(t *testing.T) (cfgDir, texDir string) {
	t.Helper()
	requireRealTeX(t)
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
	return cfgDir, filepath.Join(ucd, "zn", "tex")
}

// realTimeout leaves room for a first format build that loads tikz.
const realTimeout = 60 * time.Second

func renderReal(a *App, src string, timeout time.Duration) *termImage {
	return a.renderEmbed(contentKey("math", "latex\x00"+src), renderReq{kind: "latex", src: src, dark: a.theme.Dark, maxCols: 80, timeout: timeout})
}

// onlyPNG decodes the single PNG in the tex cache dir.
func onlyPNG(t *testing.T, texDir string) image.Image {
	t.Helper()
	p := pngFiles(texDir)
	if len(p) != 1 {
		t.Fatalf("want one PNG under %s, got %v", texDir, p)
	}
	f, err := os.Open(p[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", p[0], err)
	}
	return img
}

func hexRGB(t *testing.T, hex string) color.NRGBA {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(strings.TrimPrefix(hex, "#")) != 6 {
		t.Fatalf("theme colour %q is not #rrggbb", hex)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

// dominantOpaque is the most frequent colour among fully opaque pixels.
func dominantOpaque(img image.Image) (color.NRGBA, int) {
	counts := map[color.NRGBA]int{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A == 255 {
				counts[c]++
			}
		}
	}
	var best color.NRGBA
	n := 0
	for c, k := range counts {
		if k > n {
			best, n = c, k
		}
	}
	return best, n
}

func near(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

// showBlock shows a note's block through embedBlockLines, waits for the
// background render, and shows it again.
func showBlock(a *App, done chan string, lines []string, b mdBlock) []previewLine {
	a.embedBlockLines(b, lines, 80, "")
	nextRender(done, realTimeout)
	out, _ := a.embedBlockLines(b, lines, 80, "")
	return out
}

func realMathApp(t *testing.T) (*App, chan string) {
	t.Helper()
	p := mathPrefs("latex", "kitty")
	p.RenderTimeout = int(realTimeout / time.Second)
	return newMathApp(t, p)
}

func TestRealDefaultPreambleRendersAmsMhchemTikz(t *testing.T) {
	realTeXEnv(t)
	a := newLatexApp(t, 0)
	align := "\\begin{align*}\na &= \\ce{H2O} \\\\\nb &\\leq \\mathbb{R}\n\\end{align*}"
	if img := renderReal(a, align, realTimeout); failed(img) {
		t.Fatalf("align with \\ce under the default preamble: %v", imgErr(img))
	}
	if !preambleExists() {
		t.Fatalf("no preamble.tex at %s after the first render", config.PreamblePath())
	}
	tikz := "\\begin{tikzpicture}\n\\draw[-{Stealth}] (0,0) -- ($(1,0)+(0,1)$);\n\\end{tikzpicture}"
	if img := renderReal(a, tikz, realTimeout); failed(img) {
		t.Fatalf("tikzpicture under the default preamble: %v", imgErr(img))
	}
}

func TestRealUserPackageInPreamble(t *testing.T) {
	realTeXEnv(t)
	src := "\\[ \\bm{x} \\]"
	if img := renderReal(newLatexApp(t, 0), src, realTimeout); !failed(img) {
		t.Fatalf("\\bm renders without bm in the preamble; the test proves nothing")
	}
	pre, err := os.ReadFile(config.PreamblePath())
	if err != nil {
		t.Fatalf("default preamble not written: %v", err)
	}
	if err := os.WriteFile(config.PreamblePath(), append(pre, []byte("\n\\usepackage{bm}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if img := renderReal(newLatexApp(t, 0), src, realTimeout); failed(img) {
		t.Fatalf("\\bm{x} with \\usepackage{bm} added to preamble.tex: %v", imgErr(img))
	}
}

func TestRealTikzStrokeFollowsTheme(t *testing.T) {
	requireRealTeX(t)
	src := "\\begin{tikzpicture}\n\\draw[line width=4pt] (0,0) rectangle (2,1);\n\\fill (3,0.5) circle (0.5);\n\\end{tikzpicture}"
	for _, dark := range []bool{true, false} {
		t.Run(map[bool]string{true: "dark", false: "light"}[dark], func(t *testing.T) {
			_, texDir := realTeXEnv(t)
			a := newLatexApp(t, 0)
			a.theme = buildTheme(dark)
			if img := renderReal(a, src, realTimeout); failed(img) {
				t.Fatalf("tikzpicture: %v", imgErr(img))
			}
			want := hexRGB(t, string(a.theme.Fg))
			got, n := dominantOpaque(onlyPNG(t, texDir))
			if n == 0 {
				t.Fatalf("picture has no opaque pixel")
			}
			if !near(got, want, 24) {
				t.Fatalf("dominant stroke colour %v, want near theme.Fg %s (%v)", got, a.theme.Fg, want)
			}
		})
	}
}

func TestRealFormatReusedAcrossApps(t *testing.T) {
	_, texDir := realTeXEnv(t)
	if img := renderReal(newLatexApp(t, 0), "\\[ x \\]", realTimeout); failed(img) {
		t.Fatalf("first render: %v", imgErr(img))
	}
	fmts := fmtFiles(t, texDir)
	if len(fmts) != 1 {
		t.Fatalf("want one format under %s, got %v", texDir, fmts)
	}
	before, err := os.Stat(fmts[0])
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	a := newLatexApp(t, 0) // a later session
	for _, src := range []string{"\\[ y^2 \\]", "\\[ \\frac{a}{b} \\]"} {
		if img := renderReal(a, src, realTimeout); failed(img) {
			t.Fatalf("render %q: %v", src, imgErr(img))
		}
	}
	after, err := os.Stat(fmts[0])
	if err != nil {
		t.Fatalf("format gone after later renders: %v", err)
	}
	// A rebuild renames a new file into place; the later session only
	// touches the one it reuses, so the mtime moves and the file does not.
	if !os.SameFile(before, after) {
		t.Fatalf("format rebuilt: %s is a new file", fmts[0])
	}
	if got := fmtFiles(t, texDir); len(got) != 1 {
		t.Fatalf("want still one format, got %v", got)
	}
}

func TestRealWrite18Blocked(t *testing.T) {
	realTeXEnv(t)
	target := filepath.Join(t.TempDir(), "pwned")
	src := "\\immediate\\write18{touch " + target + "}\n\\[ x \\]"
	img := renderReal(newLatexApp(t, 0), src, realTimeout)
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("\\write18 ran a shell command: %s exists", target)
	}
	mustPicture(t, img) // the render ran, so the check above is not vacuous
}

func TestRealSandboxBlocksWritesOutsideWorkDir(t *testing.T) {
	_, texDir := realTeXEnv(t)
	target := filepath.Join(t.TempDir(), "leak.txt")
	src := "\\newwrite\\znout\n\\immediate\\openout\\znout=" + target + "\n" +
		"\\immediate\\write\\znout{zn}\n\\immediate\\closeout\\znout\n\\[ x \\]"
	img := renderReal(newLatexApp(t, 0), src, realTimeout)
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("\\openout wrote outside the work dir: %s exists", target)
	}
	// Real pdftex under openout_any=p stops on the refused \openout, so the
	// block cannot render. The refusal error proves pdftex ran and reached
	// the write, so the check above is not vacuous.
	if !failed(img) {
		t.Fatalf("a refused \\openout still gave a picture")
	}
	if err := imgErr(img); !strings.Contains(err.Error(), "can't write on file") {
		t.Fatalf("render did not fail on the refused write: %v", err)
	}

	// Guard: reading by absolute path is accepted TeX behaviour. If
	// TeX starts refusing it, revisit the README warning.
	t.Run("absolute_read_still_happens", func(t *testing.T) {
		in := filepath.Join(t.TempDir(), "box.tex")
		if err := os.WriteFile(in, []byte("\\rule{4cm}{4cm}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(texDir); err != nil {
			t.Fatal(err)
		}
		if img := renderReal(newLatexApp(t, 0), "\\input{"+in+"}", realTimeout); failed(img) {
			t.Fatalf("\\input of an absolute path no longer renders: %v", imgErr(img))
		}
		if w := onlyPNG(t, texDir).Bounds().Dx(); w < 300 {
			t.Fatalf("PNG is %dpx wide; the 4cm rule from %s did not reach it", w, in)
		}
	})
}

func TestRealLoopKilledByTimeout(t *testing.T) {
	realTeXEnv(t)
	a := newLatexApp(t, 0)
	// Build the format first, so the 2s budget below is spent on the loop.
	if img := renderReal(a, "\\[ x \\]", realTimeout); failed(img) {
		t.Fatalf("warm-up render: %v", imgErr(img))
	}
	start := time.Now()
	img := renderReal(a, "\\def\\x{\\x}\\x", 2*time.Second)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("render_timeout = 2 returned only after %v", took)
	}
	if !failed(img) {
		t.Fatalf("a TeX loop gave a picture")
	}
}

func TestRealPNGHasTransparentBackground(t *testing.T) {
	_, texDir := realTeXEnv(t)
	if img := renderReal(newLatexApp(t, 0), "\\[ x^2 + y^2 \\]", realTimeout); failed(img) {
		t.Fatalf("render: %v", imgErr(img))
	}
	img := onlyPNG(t, texDir)
	b := img.Bounds()
	for _, p := range []image.Point{b.Min, {b.Max.X - 1, b.Min.Y}, {b.Min.X, b.Max.Y - 1}, {b.Max.X - 1, b.Max.Y - 1}} {
		if _, _, _, a := img.At(p.X, p.Y).RGBA(); a != 0 {
			t.Fatalf("corner %v alpha %d, want 0 (transparent)", p, a)
		}
	}
}

// instructionsTikz cuts the first bare tikzpicture out of the MCP
// instructions.
// instructionsTikz is the ```tikz example fence of the MCP instructions,
// fence lines included: what an agent writes into a note.
func instructionsTikz(t *testing.T) []string {
	t.Helper()
	out := []string{}
	for _, l := range strings.Split(mcp.DefaultInstructions, "\n") {
		if len(out) == 0 && l != "```tikz" {
			continue
		}
		out = append(out, l)
		if len(out) > 1 && l == "```" {
			return out
		}
	}
	t.Fatalf("no ```tikz example fence in mcp.DefaultInstructions")
	return nil
}

func TestRealInstructionsTikzExampleRenders(t *testing.T) {
	realTeXEnv(t)
	tikz := instructionsTikz(t)
	lines := append(append([]string{"A vector:", ""}, tikz...), "", "done.")
	var blk *mdBlock
	for _, b := range splitBlocks(lines, true) {
		if b.kind == "fence" {
			b := b
			blk = &b
		}
	}
	if blk == nil {
		t.Fatalf("the ```tikz example is not a fence block: %+v", splitBlocks(lines, true))
	}
	a, done := realMathApp(t)
	out := showBlock(a, done, lines, *blk)
	if rowIndex(out, "latex:") >= 0 {
		t.Fatalf("error row under the default preamble:\n%s", joinPreview(out))
	}
	if !isPicture(out) {
		t.Fatalf("the MCP TikZ example is not a picture:\n%s", joinPreview(out))
	}
}

func TestRealDisplayPNGCroppedToInk(t *testing.T) {
	_, texDir := realTeXEnv(t)
	const src = "\\[ \\frac{a}{b} \\]"
	render := func(cols int) *termImage {
		img := newLatexApp(t, 0).renderEmbed(contentKey("math", "latex\x00"+src), renderReq{kind: "latex", src: src, dark: true, maxCols: cols, timeout: realTimeout})
		if failed(img) {
			t.Fatalf("render at %d cols: %v", cols, imgErr(img))
		}
		return img
	}
	narrow := render(80)
	if w := onlyPNG(t, texDir).Bounds().Dx(); w >= 400 {
		t.Errorf("\\frac{a}{b} PNG is %d px wide, want < 400 (cropped to the formula)", w)
	}
	wide := render(120)
	if narrow.rows != wide.rows {
		t.Errorf("displayed height %d rows at 80 cols, %d at 120: it depends on the pane width", narrow.rows, wide.rows)
	}
}

func TestRealWideBlocksAreNotClipped(t *testing.T) {
	_, texDir := realTeXEnv(t)
	long := ""
	for c := 'a'; c <= 'z'; c++ {
		long += string(c) + "+" + string(c-'a'+'A') + "+"
	}
	for _, tc := range []struct {
		name, src string
		minW      int
	}{
		// 26 letter pairs run past standalone's 345pt page; whole, they
		// are well over 2000 px at latexDPI.
		{"display", "\\[ " + long + "1 \\]", 2000},
		// 20cm is about 2050 px at latexDPI; the page alone is 1256.
		{"tikz", "\\begin{tikzpicture}\\draw (0,0) rectangle (20cm,1cm);\\end{tikzpicture}", 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.RemoveAll(texDir); err != nil {
				t.Fatal(err)
			}
			if img := renderReal(newLatexApp(t, 0), tc.src, realTimeout); failed(img) {
				t.Fatalf("render: %v", imgErr(img))
			}
			if w := onlyPNG(t, texDir).Bounds().Dx(); w < tc.minW {
				t.Fatalf("PNG is %d px wide, want at least %d: the block was clipped at the page edge", w, tc.minW)
			}
		})
	}
}

func TestRealDesktopTikzFenceRenders(t *testing.T) {
	realTeXEnv(t)
	// right=of needs positioning, which the default preamble loads as the
	// desktop does; the \usepackage line above the picture is preamble.
	src := "\\usepackage{amssymb}\n\\begin{tikzpicture}\n\\node (a) {$\\mathbb{R}$};\n\\node[right=of a] (b) {B};\n\\draw[-Stealth] (a) -- (b);\n\\end{tikzpicture}"
	img := newLatexApp(t, 0).renderEmbed(contentKey("math", "tikz\x00"+src), renderReq{kind: "tikz", src: src, dark: true, maxCols: 80, timeout: realTimeout})
	if failed(img) {
		t.Fatalf("a desktop-style ```tikz fence does not render: %v", imgErr(img))
	}
}
