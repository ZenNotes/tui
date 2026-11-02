package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZenNotes/tui/internal/config"
)

// defaultPreamble is written to preamble.tex on the first latex render when
// the file is missing. The user owns the file after that.
const defaultPreamble = `% zn LaTeX preamble: loaded before every display-math block when
% [terminal] math = "latex". Edit freely; zn never rewrites this file.
\usepackage{amsmath}
\usepackage{amssymb}
\usepackage[version=4]{mhchem}
\usepackage{tikz}
\usetikzlibrary{arrows.meta,calc,positioning,shapes,decorations.pathreplacing,intersections,patterns}
`

// formatWrapper goes around preamble.tex in the precompiled format. The
// theme colour is set per render, so it stays out of the format.
const formatWrapper = "\\documentclass[border=2pt,varwidth]{standalone}\n\\RequirePackage{xcolor}\n"

// preambleMu serialises ensurePreamble: on Windows a read or a rename
// racing another render's rename of preamble.tex fails with Access is
// denied.
var preambleMu sync.Mutex

// ensurePreamble returns preamble.tex, writing the default when it is
// missing. An existing file is never rewritten.
func ensurePreamble() ([]byte, error) {
	preambleMu.Lock()
	defer preambleMu.Unlock()
	path := config.PreamblePath()
	b, err := os.ReadFile(path)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, preambleError(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, preambleError(err)
	}
	// Write a temp file and rename it into place: parallel first renders
	// read the file meanwhile and must never see it empty or half written,
	// or they would build a format from a truncated preamble. Racing
	// writers all write the same default bytes.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".preamble-*.tex")
	if err != nil {
		return nil, preambleError(err)
	}
	_, werr := tmp.WriteString(defaultPreamble)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o644)); err != nil {
		os.Remove(tmp.Name())
		return nil, preambleError(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		// Windows refuses to rename onto a file another zn has open; that
		// zn already put the preamble in place.
		if b, rerr := os.ReadFile(path); rerr == nil {
			return b, nil
		}
		return nil, preambleError(err)
	}
	return []byte(defaultPreamble), nil
}

// preambleError drops the (temp) file path, so the preview's error row is
// short enough to show the cause: "preamble.tex: open: permission
// denied".
func preambleError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("preamble.tex: %s: %w", pe.Op, pe.Err)
	}
	return fmt.Errorf("preamble.tex: %w", err)
}

var hexColour = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

// latexDocument is the per-render source: the body of a document whose
// preamble lives in the format. $$ blocks become \[ … \]; \[…\] and
// environment blocks pass through verbatim.
func latexDocument(src, fg string) string {
	body := strings.TrimSpace(src)
	if len(body) >= 4 && strings.HasPrefix(body, "$$") && strings.HasSuffix(body, "$$") {
		body = "\\[\n" + strings.TrimSpace(body[2:len(body)-2]) + "\n\\]"
	}
	return texBody(body, fg)
}

// tikzDocument is the per-render source for a ```tikz fence, normalised the
// way the desktop's TikZ renderer does it (wrapSource in
// apps/desktop/src/main/tikz.ts), so a fence draws the same in both: a
// \documentclass line goes, and the \usepackage and \usetikzlibrary lines
// above the picture stay ahead of \begin{document}, where the format is
// still in the preamble and \usepackage is allowed.
func tikzDocument(src, fg string) string {
	s := strings.TrimSpace(docClassLineRe.ReplaceAllString(strings.TrimSpace(src), ""))
	begin, end := strings.Index(s, `\begin{document}`), strings.LastIndex(s, `\end{document}`)
	if begin >= 0 && end > begin {
		return strings.TrimSpace(s[:begin]) + "\n" + texBody(strings.TrimSpace(s[begin+len(`\begin{document}`):end]), fg)
	}
	s = strings.TrimSpace(strings.NewReplacer(`\begin{document}`, "", `\end{document}`, "").Replace(s))
	if loc := tikzBodyStartRe.FindStringIndex(s); loc != nil && loc[0] > 0 {
		return strings.TrimSpace(s[:loc[0]]) + "\n" + texBody(strings.TrimSpace(s[loc[0]:]), fg)
	}
	return texBody(s, fg)
}

var (
	docClassLineRe = regexp.MustCompile(`(?m)^\s*\\documentclass(?:\[[^\]]*\])?\{[^}]+\}\s*$`)
	// tikzBodyStartRe finds where a fence's drawing starts, as the desktop
	// does; whatever comes before it is preamble.
	tikzBodyStartRe = regexp.MustCompile(`\\begin\{[^}]+\}|\\(?:tikz|draw|path|node|coordinate|matrix|graph)\b`)
)

// texBody wraps a block's body in the document, in the theme's text colour.
func texBody(body, fg string) string {
	colour := ""
	if m := hexColour.FindStringSubmatch(fg); m != nil {
		colour = "\\color[HTML]{" + strings.ToUpper(m[1]) + "}"
	}
	return "\\begin{document}" + colour + "\n" + body + "\n\\end{document}\n"
}

// texSource is the document a latex render compiles: display math, or a
// ```tikz fence under kind "tikz".
func texSource(kind, src, fg string) string {
	if kind == "tikz" {
		return tikzDocument(src, fg)
	}
	return latexDocument(src, fg)
}

// latexKeySrc is the source part of a block's key. A ```tikz fence is keyed
// apart from display math, since the two are compiled differently.
func latexKeySrc(kind, src string) string {
	if kind == "tikz" {
		return "tikz\x00" + src
	}
	return src
}

// latexDir is <UserCacheDir>/zn/tex: formats and, later, PNGs.
func latexDir() (string, error) {
	c, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "zn", "tex"), nil
}

// texEnv sandboxes pdftex: openout_any=p keeps every write inside the
// working dir, and formats are found in the cache dir before the TeX
// defaults. openin_any=p is asked for too, but TeX Live (2026) still reads
// any file by absolute or ../ path under it, so a note can show a readable
// file in its picture; the README warns about that. Reads stay local: no
// write leaves the work dir and \write18 is off.
func texEnv(texDir string) []string {
	return []string{"openin_any=p", "openout_any=p", "TEXFORMATS=" + texDir + string(os.PathListSeparator)}
}

// texSandbox is passed to every pdftex run: no shell escape, and a TeX
// error ends the run instead of waiting for input.
var texSandbox = []string{"-no-shell-escape", "-interaction=nonstopmode", "-halt-on-error"}

// texError is the first "! " line of a TeX log, else the output's first line.
// A timeout stays recognisable with errors.Is(err, context.DeadlineExceeded).
func texError(out []byte, logPath string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("latex: pdftex: %w", err)
	}
	logs := [][]byte{out}
	if b, rerr := os.ReadFile(logPath); rerr == nil {
		logs = append(logs, b)
	}
	for _, l := range logs {
		for _, line := range strings.Split(string(l), "\n") {
			if strings.HasPrefix(line, "! ") {
				return fmt.Errorf("latex: %s", strings.TrimSpace(line))
			}
		}
	}
	return fmt.Errorf("latex: %s", firstLine(strings.TrimSpace(string(out)+" "+err.Error())))
}

// latexFormats single-flights format builds per App and memoises TeX
// failures per format name, so every block shows the same error until the
// preamble changes. A timed-out build is not remembered: the next block
// tries again.
type latexFormats struct {
	mu     sync.Mutex
	builds map[string]*fmtBuild
}

type fmtBuild struct {
	done chan struct{}
	err  error
}

// ensure makes sure <texDir>/<name>.fmt exists, building it from src at
// most once at a time. A format deleted from disk is rebuilt.
func (l *latexFormats) ensure(ctx context.Context, texDir, name, src string) error {
	path := filepath.Join(texDir, name+".fmt")
	l.mu.Lock()
	if l.builds == nil {
		l.builds = map[string]*fmtBuild{}
	}
	b := l.builds[name]
	// isClosed first: b.err is only safe to read once done is closed.
	if b != nil && isClosed(b.done) && b.err == nil && !fileExists(path) {
		b = nil
	}
	if b == nil {
		b = &fmtBuild{done: make(chan struct{})}
		l.builds[name] = b
		l.mu.Unlock()
		if !fileExists(path) {
			b.err = buildFormat(ctx, texDir, name, src)
		} else {
			touch(path)
		}
		if errors.Is(b.err, context.DeadlineExceeded) {
			l.mu.Lock()
			if l.builds[name] == b {
				delete(l.builds, name)
			}
			l.mu.Unlock()
		}
		close(b.done)
	} else {
		l.mu.Unlock()
	}
	select {
	case <-b.done:
		return b.err
	case <-ctx.Done():
		return fmt.Errorf("latex: format build: %w", ctx.Err())
	}
}

func isClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// touch marks a cached file as used now, so pruneLatexCache keeps it.
func touch(p string) {
	now := time.Now()
	_ = os.Chtimes(p, now, now)
}

// latexCacheMaxAge is how long a format or a picture may go unused before
// pruneLatexCache deletes it. Every session touches the format it uses and
// every picture it reads, so only leftovers age: the format of a preamble
// since edited (a few MB each) and the PNGs of formulas since changed.
const latexCacheMaxAge = 30 * 24 * time.Hour

var latexPruneOnce sync.Once

// pruneLatexCache deletes the formats and pictures in texDir that no render
// has used for latexCacheMaxAge, and what renders that died midway left
// behind. Another zn with another preamble keeps its format by touching it.
// A file that cannot be deleted (open in another zn on Windows) is tried
// again next session.
func pruneLatexCache(texDir string, now time.Time) {
	entries, err := os.ReadDir(texDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		name, age := e.Name(), now.Sub(info.ModTime())
		switch {
		case strings.HasSuffix(name, ".fmt") || strings.HasSuffix(name, ".png"):
			if age > latexCacheMaxAge {
				_ = os.Remove(filepath.Join(texDir, name))
			}
		case strings.HasPrefix(name, "build-") || strings.HasPrefix(name, "png-"):
			if age > time.Hour {
				_ = os.RemoveAll(filepath.Join(texDir, name))
			}
		}
	}
}

// buildFormat dumps the wrapper and preamble into a format in a temp dir
// inside texDir, then renames it into place so readers never see half a file.
func buildFormat(ctx context.Context, texDir, name, src string) error {
	if err := os.MkdirAll(texDir, 0o755); err != nil {
		return fmt.Errorf("latex: %w", err)
	}
	dir, err := os.MkdirTemp(texDir, "build-")
	if err != nil {
		return fmt.Errorf("latex: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "fmt.tex"), []byte(src+"\n\\dump\n"), 0o644); err != nil {
		return fmt.Errorf("latex: %w", err)
	}
	args := append([]string{"-ini"}, texSandbox...)
	args = append(args, "-jobname="+name, "&pdflatex", "fmt.tex")
	if out, err := runTool(ctx, dir, texEnv(texDir), "pdftex", args...); err != nil {
		return texError(out, filepath.Join(dir, name+".log"), err)
	}
	if err := os.Rename(filepath.Join(dir, name+".fmt"), filepath.Join(texDir, name+".fmt")); err != nil {
		// On Windows the rename fails while another zn has the same format
		// open; that format is the one this build made.
		if _, serr := os.Stat(filepath.Join(texDir, name+".fmt")); serr != nil {
			return fmt.Errorf("latex: %w", err)
		}
	}
	return nil
}

// overfullRe matches TeX's report of a line wider than the page.
var overfullRe = regexp.MustCompile(`Overfull \\hbox \(([0-9.]+)pt too wide\)`)

// overfullBy is how far, in pt, the widest line in a TeX log runs past the
// page, or 0 when every line fits.
func overfullBy(log []byte) float64 {
	widest := 0.0
	for _, m := range overfullRe.FindAllSubmatch(log, -1) {
		if v, err := strconv.ParseFloat(string(m[1]), 64); err == nil && v > widest {
			widest = v
		}
	}
	return widest
}

// widerPage is the preamble line that widens standalone's varwidth page by
// over pt, plus a little for rounding in the log.
func widerPage(over float64) string {
	return fmt.Sprintf("\\standaloneconfig{varwidth=\\dimexpr\\linewidth+%dpt\\relax}\n", int(over)+2)
}

// latexDPI rasterises 10pt TeX at about the size of typst's 18pt at 144 ppi.
const latexDPI = "260"

// latexCropBorder is the transparent margin, in px, kept around the ink
// when a rendered PNG is cropped. standalone sets display math at full line
// width, so without the crop a short formula is a wide mostly empty picture.
const latexCropBorder = 8

// cropToInk crops a PNG to the bounding box of its pixels with alpha > 0,
// grown by border px on each side and clamped to the image. A fully
// transparent image, or one whose box is the whole image, comes back as is.
func cropToInk(data []byte, border int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	box := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if box.Empty() {
		return data, nil
	}
	if box = box.Inset(-border).Intersect(b); box == b {
		return data, nil
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return data, nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, sub.SubImage(box)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// latexPNG renders one block (kind "latex" for display math, "tikz" for a
// ```tikz fence) to PNG bytes with the format built from preamble.tex. The
// PNG is cached on disk in <UserCacheDir>/zn/tex, named by colour, format
// and source, so a later session runs no tools for it.
func latexPNG(ctx context.Context, formats *latexFormats, kind, src, fg string) ([]byte, error) {
	preamble, err := ensurePreamble()
	if err != nil {
		return nil, err
	}
	texDir, err := latexDir()
	if err != nil {
		return nil, fmt.Errorf("latex: %w", err)
	}
	fmtSrc := formatWrapper + string(preamble)
	name := latexFormatName(preamble)
	cached := filepath.Join(texDir, strings.TrimPrefix(latexKey(fg, name, latexKeySrc(kind, src)), "math:")+".png")
	if b, err := os.ReadFile(cached); err == nil {
		touch(cached)
		return b, nil
	}
	if err := formats.ensure(ctx, texDir, name, fmtSrc); err != nil {
		return nil, err
	}
	// After ensure, which touched this session's format.
	latexPruneOnce.Do(func() { go pruneLatexCache(texDir, time.Now()) })
	dir, err := os.MkdirTemp("", "zn-latex")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "m.tex"), []byte(texSource(kind, src, fg)), 0o644); err != nil {
		return nil, err
	}
	args := append([]string{"-fmt=" + name}, texSandbox...)
	args = append(args, "m.tex")
	if out, err := runTool(ctx, dir, texEnv(texDir), "pdftex", args...); err != nil {
		return nil, texError(out, filepath.Join(dir, "m.log"), err)
	}
	// The page is \linewidth wide, and a longer display or a wider picture
	// is cut off at its right edge. TeX reports by how much it ran over, so
	// that block alone renders again on a page widened by as much; a block
	// that fits keeps the usual width, so multline and equation numbers
	// still sit where they would in a document.
	if log, err := os.ReadFile(filepath.Join(dir, "m.log")); err == nil {
		if over := overfullBy(log); over > 0 {
			if err := os.WriteFile(filepath.Join(dir, "m.tex"), []byte(widerPage(over)+texSource(kind, src, fg)), 0o644); err != nil {
				return nil, err
			}
			if out, err := runTool(ctx, dir, texEnv(texDir), "pdftex", args...); err != nil {
				return nil, texError(out, filepath.Join(dir, "m.log"), err)
			}
		}
	}
	if out, err := runTool(ctx, dir, nil, "pdftocairo", "-png", "-transp", "-singlefile", "-r", latexDPI, "m.pdf", "m"); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("latex: pdftocairo: %w", err)
		}
		return nil, fmt.Errorf("latex: pdftocairo: %s", firstLine(strings.TrimSpace(string(out)+" "+err.Error())))
	}
	png, err := os.ReadFile(filepath.Join(dir, "m.png"))
	if err != nil {
		return nil, fmt.Errorf("latex: %w", err)
	}
	if png, err = cropToInk(png, latexCropBorder); err != nil {
		return nil, fmt.Errorf("latex: crop: %w", err)
	}
	// The disk cache is best effort: a failed write only costs a re-render
	// next session, and the picture is still shown now.
	if tmp, err := os.CreateTemp(texDir, "png-"); err == nil {
		_, werr := tmp.Write(png)
		cerr := tmp.Close()
		if werr != nil || cerr != nil || os.Rename(tmp.Name(), cached) != nil {
			os.Remove(tmp.Name())
		}
	}
	return png, nil
}

// latexFormatName is zn-<sha256(wrapper+preamble)[:16]>: the format's name
// and the preamble part of every latex key.
func latexFormatName(preamble []byte) string {
	sum := sha256.Sum256([]byte(formatWrapper + string(preamble)))
	return "zn-" + hex.EncodeToString(sum[:])[:16]
}

// currentLatexFormat is the format name for preamble.tex as it is now. It
// only reads: a missing file hashes as the default the render will write.
func currentLatexFormat() string {
	b, err := os.ReadFile(config.PreamblePath())
	if err != nil {
		b = []byte(defaultPreamble)
	}
	return latexFormatName(b)
}

// latexKey names a latex block in memory and, without the "math:" prefix,
// its PNG on disk: theme colour, format (wrapper + preamble hash) and source.
// The version token changes whenever the pictures do, so PNGs that an
// older zn cached are not read.
func latexKey(fg, format, src string) string {
	return contentKey("math", "latex\x00wide1\x00"+fg+"\x00"+format+"\x00"+src)
}
