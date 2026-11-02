package tui

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yashikota/mermaigo/pkg/mermaid"

	"github.com/ZenNotes/tui/internal/vault"
)

// Embeds in the reading view, following what the desktop app renders:
// pictures (painted with the Kitty protocol where the terminal can, a card
// elsewhere), PDF, audio, video, drawing and other files as cards that open
// with Enter, YouTube and Vimeo links as cards, `![[Note]]` transclusions
// rendered inline, mermaid diagrams drawn as text (or as a picture when
// mermaid-cli is installed), and `$$` math rendered through typst when it
// is installed.

var (
	embedLineRe    = regexp.MustCompile(`^\s*!\[\[([^\]\n]+?)\]\]\s*$`)
	embedMDLineRe  = regexp.MustCompile(`^\s*!\[([^\]]*)\]\(([^)\s]+)[^)]*\)\s*$`)
	bareURLLineRe  = regexp.MustCompile(`^\s*<?(https?://[^\s<>]+)>?\s*$`)
	mathFenceRe    = regexp.MustCompile(`^\s*\$\$\s*$`)
	mathInlineOpen = regexp.MustCompile(`^\s*\$\$(.+)\$\$\s*$`)
)

var (
	imageExts      = map[string]bool{".apng": true, ".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".svg": true, ".webp": true}
	paintableExts  = map[string]bool{".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".webp": true}
	pdfExts        = map[string]bool{".pdf": true}
	audioExts      = map[string]bool{".aac": true, ".flac": true, ".m4a": true, ".mp3": true, ".ogg": true, ".wav": true}
	videoExts      = map[string]bool{".m4v": true, ".mov": true, ".mp4": true, ".ogv": true, ".webm": true}
	excalidrawExts = map[string]bool{".excalidraw": true}
)

// assetKind classifies an embed target the way the desktop does.
func assetKind(target string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(target)))
	switch {
	case imageExts[ext]:
		return "image"
	case pdfExts[ext]:
		return "pdf"
	case audioExts[ext]:
		return "audio"
	case videoExts[ext]:
		return "video"
	case excalidrawExts[ext] || strings.HasSuffix(strings.ToLower(target), ".excalidraw.md"):
		return "drawing"
	case ext == "" || ext == ".md":
		return "note"
	}
	return "file"
}

// embedTarget is what an embed line points at, resolved against the vault.
type embedTarget struct {
	kind  string // image, pdf, audio, video, drawing, file, note, url
	raw   string
	rel   string // vault-relative path when resolved
	title string
	url   string
}

// resolveEmbed maps an embed's text to a vault file or a note. Bare file
// names look through the attachment folders, paths resolve relative to the
// note and to the vault root.
func (a *App) resolveEmbed(raw, fromPath string) embedTarget {
	raw = strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return embedTarget{kind: "url", raw: raw, url: raw}
	}
	target := raw
	if i := strings.IndexAny(target, "|#"); i >= 0 && assetKind(target[:i]) != "note" {
		target = target[:i]
	}
	kind := assetKind(target)
	if kind == "note" {
		name := target
		if i := strings.IndexAny(name, "#|"); i >= 0 {
			name = name[:i]
		}
		if a.idx != nil {
			if meta, ok := vault.ResolveWikilink(a.idx.notes, name); ok {
				return embedTarget{kind: "note", raw: raw, rel: meta.Path, title: meta.Title}
			}
		}
		return embedTarget{kind: "note", raw: raw, title: name}
	}
	decoded := target
	if d, err := url.PathUnescape(target); err == nil {
		decoded = d
	}
	rel := a.resolveAssetPath(decoded, fromPath)
	return embedTarget{kind: kind, raw: raw, rel: rel, title: filepath.Base(decoded)}
}

// resolveAssetPath finds an attachment by relative path or bare name.
func (a *App) resolveAssetPath(target, fromPath string) string {
	clean := vault.NormalizeRelPath(strings.TrimPrefix(target, "./"))
	if strings.HasPrefix(clean, "/") {
		clean = strings.TrimLeft(clean, "/")
	}
	candidates := []string{clean}
	if dir := filepath.ToSlash(filepath.Dir(fromPath)); dir != "." && dir != "" {
		candidates = append(candidates, vault.NormalizeRelPath(dir+"/"+clean))
	}
	assets := a.assetIndex()
	for _, c := range candidates {
		if _, ok := assets[strings.ToLower(c)]; ok {
			return c
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	for path, name := range assets {
		if name == base {
			return path
		}
	}
	if a.opts.Target.Kind == "local" {
		for _, c := range candidates {
			if abs, err := vault.SafeJoin(a.backend.Root(), c); err == nil {
				if _, err := os.Stat(abs); err == nil {
					return c
				}
			}
		}
	}
	return clean
}

// assetIndex maps lower-cased vault-relative asset paths to file names.
func (a *App) assetIndex() map[string]string {
	if a.assetPaths != nil {
		return a.assetPaths
	}
	a.assetPaths = map[string]string{}
	if list, err := a.backend.ListAssets(a.ctx); err == nil {
		for _, as := range list {
			a.assetPaths[strings.ToLower(as.Path)] = strings.ToLower(as.Name)
		}
	}
	return a.assetPaths
}

// embedBlockLines renders a block when it is an embed of some kind; ok is
// false for ordinary Markdown, which Glamour then renders below any rows
// returned anyway (a ```tikz fence's TeX error).
func (a *App) embedBlockLines(b mdBlock, lines []string, width int, fromPath string) ([]previewLine, bool) {
	switch b.kind {
	case "fence":
		lang := fenceLang(lines[b.start])
		if lang == "mermaid" && b.end-b.start > 2 {
			return a.mermaidLines(strings.Join(lines[b.start+1:b.end-1], "\n"), b.start, width), true
		}
		if lang == "tikz" && a.prefs.TerminalMath == "latex" && b.end-b.start > 2 {
			return a.tikzLines(strings.Join(lines[b.start+1:b.end-1], "\n"), b.start, width)
		}
		return nil, false
	case "math", "tex":
		body := strings.Join(lines[b.start:b.end], "\n")
		return a.mathLines(body, b.start, width), true
	case "para", "list":
		if b.end-b.start != 1 {
			return nil, false
		}
		line := lines[b.start]
		if m := embedLineRe.FindStringSubmatch(line); m != nil {
			return a.embedTargetLines(a.resolveEmbed(m[1], fromPath), b.start, width, fromPath), true
		}
		if m := embedMDLineRe.FindStringSubmatch(line); m != nil {
			t := a.resolveEmbed(m[2], fromPath)
			if t.kind != "url" && strings.TrimSpace(m[1]) != "" {
				t.title = strings.TrimSpace(m[1])
			}
			return a.embedTargetLines(t, b.start, width, fromPath), true
		}
		if m := bareURLLineRe.FindStringSubmatch(line); m != nil {
			if provider, ok := videoProvider(m[1]); ok {
				return a.cardLines(cardSpec{icon: "▶", title: provider + " video", detail: m[1], action: "Enter opens in the browser", link: linkTarget{kind: "url", target: m[1]}}, b.start, width), true
			}
		}
	}
	return nil, false
}

func fenceLang(open string) string {
	lang := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(open), "`~"))
	if sp := strings.IndexAny(lang, " \t{"); sp >= 0 {
		lang = lang[:sp]
	}
	return strings.ToLower(lang)
}

// videoProvider recognizes the hosts the desktop embeds.
func videoProvider(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "youtu.be", "youtube.com", "m.youtube.com", "youtube-nocookie.com":
		return "YouTube", true
	case "vimeo.com", "player.vimeo.com":
		return "Vimeo", true
	}
	return "", false
}

// embedTargetLines renders one resolved embed.
func (a *App) embedTargetLines(t embedTarget, srcLine, width int, fromPath string) []previewLine {
	switch t.kind {
	case "note":
		return a.transclusionLines(t, srcLine, width, fromPath)
	case "url":
		if provider, ok := videoProvider(t.url); ok {
			return a.cardLines(cardSpec{icon: "▶", title: provider + " video", detail: t.url, action: "Enter opens in the browser", link: linkTarget{kind: "url", target: t.url}}, srcLine, width)
		}
		return a.cardLines(cardSpec{icon: "⌘", title: t.url, action: "Enter opens in the browser", link: linkTarget{kind: "url", target: t.url}}, srcLine, width)
	case "image":
		return a.imageLines(t, srcLine, width)
	}
	icon, label := "▣", "file"
	switch t.kind {
	case "pdf":
		icon, label = "▤", "PDF"
	case "audio":
		icon, label = "♫", "audio"
	case "video":
		icon, label = "▶", "video"
	case "drawing":
		icon, label = "✎", "drawing"
	}
	detail := label
	if size := a.assetSize(t.rel); size > 0 {
		detail += " · " + humanSize(size)
	}
	return a.cardLines(cardSpec{icon: icon, title: t.title, detail: detail, action: a.openActionLabel(), link: linkTarget{kind: "file", target: t.rel}}, srcLine, width)
}

func (a *App) openActionLabel() string {
	if a.opts.Target.Kind == "local" {
		return "Enter opens it"
	}
	return "Enter copies the path"
}

func (a *App) assetSize(rel string) int64 {
	if list, err := a.backend.ListAssets(a.ctx); err == nil {
		for _, as := range list {
			if strings.EqualFold(as.Path, rel) {
				return as.Size
			}
		}
	}
	return 0
}

type cardSpec struct {
	icon, title, detail, action string
	link                        linkTarget
}

// cardLines draws a one-line card: an accent bar, the icon and title, the
// detail and what Enter does, all fitted to the width.
func (a *App) cardLines(c cardSpec, srcLine, width int) []previewLine {
	th := a.theme
	title := truncateCells(c.title, max(8, width-8))
	text := th.BorderFocus.Render("▍") + th.KeyHint.Render(c.icon+" ") + th.Bold.Render(title)
	used := 3 + cellWidth(title)
	if c.detail != "" && used+cellWidth(c.detail)+4 < width {
		text += th.Muted.Render("  ·  " + truncateCells(c.detail, width-used-5))
		used += 5 + cellWidth(c.detail)
	}
	if c.action != "" && used+cellWidth(c.action)+4 < width {
		text += th.Dim.Render("  ·  " + c.action)
	}
	return []previewLine{{text: text, srcLine: srcLine, links: []linkTarget{c.link}}}
}

// imageLines paints a picture with placeholders, or shows a card when the
// terminal cannot show pictures or the file cannot be read.
func (a *App) imageLines(t embedTarget, srcLine, width int) []previewLine {
	link := linkTarget{kind: "file", target: t.rel}
	card := func(detail string) []previewLine {
		return a.cardLines(cardSpec{icon: "▧", title: t.title, detail: detail, action: a.openActionLabel(), link: link}, srcLine, width)
	}
	ext := strings.ToLower(filepath.Ext(t.rel))
	if a.imageMode() == imagesOff || !paintableExts[ext] {
		return card("image")
	}
	key := "asset:" + a.sessionKey() + ":" + t.rel
	img := a.images().imageFor(key, max(4, width-2), 40, func() ([]byte, error) { return a.backend.ReadAsset(a.ctx, t.rel) })
	if img.err != nil {
		return card("image · " + img.err.Error())
	}
	out := []previewLine{}
	for i, row := range placeholderRows(img.id, img.cols, img.rows) {
		pl := previewLine{text: " " + row, srcLine: srcLine}
		if i == 0 {
			pl.links = []linkTarget{link}
		}
		out = append(out, pl)
	}
	out = append(out, previewLine{text: " " + a.theme.Muted.Render(truncateCells(fmt.Sprintf("%s · %d×%d", t.title, img.width, img.height), width-2)), srcLine: srcLine})
	return out
}

// transclusionLines renders another note inline, the way `![[Note]]` does
// in the desktop app, with a bar down the side and a depth limit.
func (a *App) transclusionLines(t embedTarget, srcLine, width int, fromPath string) []previewLine {
	th := a.theme
	link := linkTarget{kind: "wikilink", target: t.title}
	if t.rel == "" {
		return a.cardLines(cardSpec{icon: "⟦", title: t.title + " ⟧", detail: "no such note", action: "Enter creates it", link: link}, srcLine, width)
	}
	if a.previewDepth >= 3 || t.rel == fromPath {
		return a.cardLines(cardSpec{icon: "⟦", title: t.title + " ⟧", detail: "embedded note", action: "Enter opens it", link: link}, srcLine, width)
	}
	body := ""
	if buf, ok := a.buffers[t.rel]; ok {
		body = buf.ed.Text()
	} else if content, err := a.backend.ReadNote(a.ctx, t.rel); err == nil {
		body = content.Body
	} else {
		return a.cardLines(cardSpec{icon: "⟦", title: t.title + " ⟧", detail: err.Error(), link: link}, srcLine, width)
	}
	if _, rest, ok := vault.Frontmatter(body); ok {
		body = rest
	}
	a.previewDepth++
	inner := a.renderPreviewLinesFrom(body, max(20, width-2), t.rel)
	a.previewDepth--
	bar := th.BorderFocus.Render("▎")
	out := []previewLine{{text: bar + " " + th.KeyHint.Render("⟦ ") + th.Bold.Render(truncateCells(t.title, max(4, width-8))) + th.KeyHint.Render(" ⟧"), srcLine: srcLine, links: []linkTarget{link}}}
	for _, pl := range inner {
		out = append(out, previewLine{text: bar + " " + pl.text, srcLine: srcLine, links: pl.links})
	}
	return out
}

// --- mermaid ---

type diagramCache struct {
	mu    sync.Mutex
	text  map[string][]string
	png   map[string]*termImage
	inFly map[string]bool
	// pool caps the external render processes running at once; mermaid,
	// typst and latex share it. Sized from render_concurrency at first use.
	pool chan struct{}
	// latex single-flights pdftex format builds for this session.
	latex latexFormats
}

// diagramsMu guards the lazy init: render goroutines may be the first to ask.
var diagramsMu sync.Mutex

func (a *App) diagrams() *diagramCache {
	diagramsMu.Lock()
	defer diagramsMu.Unlock()
	if a.diagramCache == nil {
		a.diagramCache = &diagramCache{text: map[string][]string{}, png: map[string]*termImage{}, inFly: map[string]bool{}, pool: make(chan struct{}, max(1, a.prefs.RenderConcurrency))}
	}
	return a.diagramCache
}

func contentKey(prefix, text string) string {
	sum := sha1.Sum([]byte(text))
	return prefix + ":" + hex.EncodeToString(sum[:8])
}

// mermaidLines draws a diagram. Mermaid-cli, when installed and the
// terminal shows pictures, renders it as an image in the background; the
// pure-Go text rendering is shown meanwhile and everywhere else.
func (a *App) mermaidLines(src string, srcLine, width int) []previewLine {
	th := a.theme
	key := contentKey("mermaid", src)
	if a.imageMode() == imagesKitty {
		if img := a.externalImage(key, width-2, "mermaid", src); img != nil && img.err == nil {
			return a.pictureLines(a.fitted(key, img, width-2), srcLine, width, "mermaid")
		}
	}
	d := a.diagrams()
	d.mu.Lock()
	rows, ok := d.text[key]
	d.mu.Unlock()
	if !ok {
		rendered, err := mermaid.RenderText(src, &mermaid.TextRenderOptions{})
		if err != nil {
			rows = []string{th.StatusError.Render("mermaid: " + firstLine(err.Error()))}
			for _, l := range strings.Split(src, "\n") {
				rows = append(rows, th.Code.Render(l))
			}
		} else {
			rows = strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		}
		d.mu.Lock()
		d.text[key] = rows
		d.mu.Unlock()
	}
	out := make([]previewLine, 0, len(rows)+1)
	for _, row := range rows {
		out = append(out, previewLine{text: " " + th.Base.Render(truncateCells(row, width-1)), srcLine: srcLine})
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (a *App) pictureLines(img *termImage, srcLine, width int, label string) []previewLine {
	out := []previewLine{}
	for _, row := range placeholderRows(img.id, img.cols, img.rows) {
		out = append(out, previewLine{text: " " + row, srcLine: srcLine})
	}
	return out
}

// --- math ---

// mathLines renders a `$$` block: as a picture through typst, or pdftex
// and pdftocairo under math = "latex", when the tools are installed and
// the terminal shows pictures, else as the source in a quiet block. A
// latex error is shown in a row above the source.
func (a *App) mathLines(body string, srcLine, width int) []previewLine {
	th := a.theme
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(body), "$$"), "$$"))
	out := []previewLine{}
	if a.imageMode() == imagesKitty {
		var img *termImage
		var key string
		if a.prefs.TerminalMath == "latex" {
			src := strings.TrimSpace(body)
			key = latexKey(string(th.Fg), currentLatexFormat(), src)
			img = a.externalImage(key, width-2, "latex", src)
			out = append(out, a.latexErrorRows(img, srcLine, width)...)
		} else {
			key = contentKey("math", fmt.Sprintf("typst\x00%t\x00%s", th.Dark, inner))
			img = a.externalImage(key, width-2, "math", inner)
		}
		if img != nil && img.err == nil {
			return a.pictureLines(a.fitted(key, img, width-2), srcLine, width, "math")
		}
	}
	for _, l := range strings.Split(inner, "\n") {
		out = append(out, previewLine{text: " " + th.BorderFocus.Render("▎") + " " + th.Code.Render(truncateCells(l, width-4)), srcLine: srcLine})
	}
	return out
}

// tikzLines renders a ```tikz fence through LaTeX under math = "latex", as
// the desktop draws the same fence. ok is false until the picture is ready
// and when it fails, so the fence shows as code, with a TeX error above it.
func (a *App) tikzLines(src string, srcLine, width int) ([]previewLine, bool) {
	if a.imageMode() != imagesKitty {
		return nil, false
	}
	key := latexKey(string(a.theme.Fg), currentLatexFormat(), latexKeySrc("tikz", src))
	img := a.externalImage(key, width-2, "tikz", src)
	if img != nil && img.err == nil {
		return a.pictureLines(a.fitted(key, img, width-2), srcLine, width, "tikz"), true
	}
	return a.latexErrorRows(img, srcLine, width), false
}

// fitted is a rendered picture at the size that fits maxCols now. The
// background render sends it at the pane width of the moment it finished;
// without this, a picture first shown in a narrow pane (a split, a window
// still opening) stays that small after the pane grows.
func (a *App) fitted(key string, img *termImage, maxCols int) *termImage {
	if f := a.images().refit(key, maxCols, 40); f != nil && f.err == nil {
		return f
	}
	return img
}

// latexErrorRows is the first line of a failed latex render, wrapped rather
// than truncated because the cause sits at the end of long paths. A render
// that worked or is still running has none. Missing tools get the install
// notice instead, once per note.
func (a *App) latexErrorRows(img *termImage, srcLine, width int) []previewLine {
	if img == nil || img.err == nil {
		return nil
	}
	if errors.Is(img.err, exec.ErrNotFound) {
		return a.latexToolsNotice(srcLine, width)
	}
	out := []previewLine{}
	for _, row := range wrapCommentText(firstLine(img.err.Error()), width-2) {
		out = append(out, previewLine{text: " " + a.theme.StatusError.Render(row), srcLine: srcLine})
	}
	return out
}

// missingLatexTools names the tools math = "latex" runs that are not on
// PATH, with where each comes from.
func missingLatexTools() []string {
	missing := []string{}
	for _, tool := range []struct{ name, from string }{{"pdftex", "TeX Live or MacTeX"}, {"pdftocairo", "poppler"}} {
		if _, err := exec.LookPath(tool.name); err != nil {
			missing = append(missing, tool.name+" ("+tool.from+")")
		}
	}
	return missing
}

// latexToolsNotice tells the reader, above the first LaTeX block of a note,
// that math = "latex" runs TeX installed on this machine and which part is
// missing. zn ships no TeX, and without this the blocks just show their
// source with no hint why. Later blocks of the same note stay quiet.
func (a *App) latexToolsNotice(srcLine, width int) []previewLine {
	missing := missingLatexTools()
	if a.latexNoticeShown || len(missing) == 0 {
		return nil
	}
	a.latexNoticeShown = true
	style := lipgloss.NewStyle().Foreground(a.theme.Accent)
	text := "LaTeX math needs " + strings.Join(missing, " and ") + " installed on this computer; zn does not include them. Run zn doctor for the install steps."
	out := []previewLine{}
	for _, row := range wrapCommentText(text, width-2) {
		out = append(out, previewLine{text: " " + style.Render(row), srcLine: srcLine})
	}
	return out
}

// externalImage returns a rendered picture for a key once a background
// render finished; it starts the render on first call and returns nil
// until then; a failed render is returned with its err set. Renders need
// the tools on PATH: mmdc for mermaid, typst for math, pdftex and
// pdftocairo for latex.
func (a *App) externalImage(key string, maxCols int, kind, src string) *termImage {
	d := a.diagrams()
	d.mu.Lock()
	if img, ok := d.png[key]; ok {
		d.mu.Unlock()
		return img
	}
	if d.inFly[key] || a.program == nil {
		d.mu.Unlock()
		return nil
	}
	tools := map[string][]string{"mermaid": {"mmdc"}, "math": {"typst"}, "latex": {"pdftex", "pdftocairo"}, "tikz": {"pdftex", "pdftocairo"}}[kind]
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			img := &termImage{err: err}
			d.png[key] = img
			d.mu.Unlock()
			return img
		}
	}
	d.inFly[key] = true
	d.mu.Unlock()
	req := renderReq{kind: kind, src: src, dark: a.theme.Dark, fg: string(a.theme.Fg), maxCols: maxCols, timeout: time.Duration(a.prefs.RenderTimeout) * time.Second}
	go func() {
		img := a.renderEmbed(key, req)
		d.mu.Lock()
		// A timeout is stored like any failure, so a redraw does not loop;
		// opening a note drops it via forgetEmbedTimeouts.
		d.png[key] = img
		delete(d.inFly, key)
		d.mu.Unlock()
		a.program.Send(embedRenderedMsg{key: key})
	}()
	return nil
}

// forgetEmbedTimeouts drops every timed-out render, so the next show of
// its block renders again. Called when a note is opened.
func (a *App) forgetEmbedTimeouts() {
	d := a.diagrams()
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, img := range d.png {
		if errors.Is(img.err, context.DeadlineExceeded) {
			delete(d.png, k)
		}
	}
}

// renderReq is one external render: what to draw and its limits.
type renderReq struct {
	kind, src string
	dark      bool
	fg        string // theme text colour (#rrggbb) for latex; empty = the App's theme
	maxCols   int
	timeout   time.Duration
}

// renderEmbed runs one render synchronously. It holds a pool slot for the
// whole render, and the timeout starts once it has the slot.
func (a *App) renderEmbed(key string, req renderReq) *termImage {
	d := a.diagrams()
	d.pool <- struct{}{}
	defer func() { <-d.pool }()
	if req.timeout <= 0 {
		req.timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), req.timeout)
	defer cancel()
	var data []byte
	var err error
	if req.kind == "latex" || req.kind == "tikz" {
		if req.fg == "" {
			req.fg = string(a.theme.Fg)
		}
		data, err = latexPNG(ctx, &d.latex, req.kind, req.src, req.fg)
	} else {
		data, err = renderExternal(ctx, req.kind, req.src, req.dark)
	}
	if err != nil {
		return &termImage{err: err}
	}
	return a.images().imageFor(key, req.maxCols, 40, func() ([]byte, error) { return data, nil })
}

// runTool runs an external tool in dir with extra env and returns its
// combined output. The process is killed when ctx ends; WaitDelay stops
// children that still hold the pipes (Chromium under mmdc) from hanging
// the call. Tests swap it for a fake.
var runTool = func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = time.Second
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		limit := time.Since(start)
		if dl, ok := ctx.Deadline(); ok {
			limit = dl.Sub(start)
		}
		return out, timeoutError{fmt.Sprintf("timed out after %s", limit.Round(100*time.Millisecond))}
	}
	return out, err
}

// timeoutError reads "timed out after Ns" and matches
// errors.Is(err, context.DeadlineExceeded), so a timeout, which is worth
// retrying, can be told from a tool error, which is not.
type timeoutError struct{ msg string }

func (e timeoutError) Error() string { return e.msg }
func (e timeoutError) Unwrap() error { return context.DeadlineExceeded }

// embedRenderedMsg says a background render finished; previews redraw.
type embedRenderedMsg struct{ key string }

// renderExternal runs mermaid-cli or typst on a temp file and returns PNG
// bytes.
func renderExternal(ctx context.Context, kind, src string, dark bool) ([]byte, error) {
	dir, err := os.MkdirTemp("", "zn-embed")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	switch kind {
	case "mermaid":
		in := filepath.Join(dir, "d.mmd")
		out := filepath.Join(dir, "d.png")
		if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
			return nil, err
		}
		theme := "default"
		if dark {
			theme = "dark"
		}
		if msg, err := runTool(ctx, dir, nil, "mmdc", "-i", in, "-o", out, "-b", "transparent", "-t", theme, "-s", "2", "-q"); err != nil {
			return nil, fmt.Errorf("mmdc: %s", firstLine(strings.TrimSpace(string(msg)+" "+err.Error())))
		}
		return os.ReadFile(out)
	case "math":
		in := filepath.Join(dir, "m.typ")
		out := filepath.Join(dir, "m.png")
		fill := "black"
		if dark {
			fill = "white"
		}
		doc := "#set page(width: auto, height: auto, margin: 8pt, fill: none)\n#set text(size: 18pt, fill: " + fill + ")\n$ " + src + " $\n"
		if err := os.WriteFile(in, []byte(doc), 0o644); err != nil {
			return nil, err
		}
		if msg, err := runTool(ctx, dir, nil, "typst", "compile", "--format", "png", "--ppi", "144", in, out); err != nil {
			return nil, fmt.Errorf("typst: %s", firstLine(strings.TrimSpace(string(msg)+" "+err.Error())))
		}
		return os.ReadFile(out)
	}
	return nil, fmt.Errorf("unknown renderer %q", kind)
}

var _ = lipgloss.Width
