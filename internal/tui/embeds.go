package tui

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/yashikota/mermaigo/pkg/mermaid"

	"github.com/ZenNotes/zennotescli/internal/vault"
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
// false for ordinary Markdown, which Glamour then renders.
func (a *App) embedBlockLines(b mdBlock, lines []string, width int, fromPath string) ([]previewLine, bool) {
	switch b.kind {
	case "fence":
		lang := fenceLang(lines[b.start])
		if lang == "mermaid" && b.end-b.start > 2 {
			return a.mermaidLines(strings.Join(lines[b.start+1:b.end-1], "\n"), b.start, width), true
		}
		return nil, false
	case "math":
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
}

func (a *App) diagrams() *diagramCache {
	if a.diagramCache == nil {
		a.diagramCache = &diagramCache{text: map[string][]string{}, png: map[string]*termImage{}, inFly: map[string]bool{}}
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
		if img := a.externalImage(key, width-2, "mermaid", src); img != nil {
			return a.pictureLines(img, srcLine, width, "mermaid")
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

// mathLines renders a `$$` block: as a picture through typst when it is
// installed and the terminal shows pictures, else as the source in a
// quiet block.
func (a *App) mathLines(body string, srcLine, width int) []previewLine {
	th := a.theme
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(body), "$$"), "$$"))
	if a.imageMode() == imagesKitty {
		key := contentKey("math", inner)
		if img := a.externalImage(key, width-2, "math", inner); img != nil {
			return a.pictureLines(img, srcLine, width, "math")
		}
	}
	out := []previewLine{}
	for _, l := range strings.Split(inner, "\n") {
		out = append(out, previewLine{text: " " + th.BorderFocus.Render("▎") + " " + th.Code.Render(truncateCells(l, width-4)), srcLine: srcLine})
	}
	return out
}

// externalImage returns a rendered picture for a key once a background
// render finished; it starts the render on first call and returns nil
// until then. Renders need the tool on PATH: mmdc for mermaid, typst for
// math.
func (a *App) externalImage(key string, maxCols int, kind, src string) *termImage {
	d := a.diagrams()
	d.mu.Lock()
	if img, ok := d.png[key]; ok {
		d.mu.Unlock()
		if img.err != nil {
			return nil
		}
		return img
	}
	if d.inFly[key] || a.program == nil {
		d.mu.Unlock()
		return nil
	}
	tool := "mmdc"
	if kind == "math" {
		tool = "typst"
	}
	if _, err := exec.LookPath(tool); err != nil {
		d.png[key] = &termImage{err: err}
		d.mu.Unlock()
		return nil
	}
	d.inFly[key] = true
	d.mu.Unlock()
	dark := a.theme.Dark
	store := a.images()
	go func() {
		data, err := renderExternal(kind, src, dark)
		var img *termImage
		if err != nil {
			img = &termImage{err: err}
		} else {
			img = store.imageFor(key, maxCols, 40, func() ([]byte, error) { return data, nil })
		}
		d.mu.Lock()
		d.png[key] = img
		delete(d.inFly, key)
		d.mu.Unlock()
		a.program.Send(embedRenderedMsg{key: key})
	}()
	return nil
}

// embedRenderedMsg says a background render finished; previews redraw.
type embedRenderedMsg struct{ key string }

// renderExternal runs mermaid-cli or typst on a temp file and returns PNG
// bytes.
func renderExternal(kind, src string, dark bool) ([]byte, error) {
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
		cmd := exec.Command("mmdc", "-i", in, "-o", out, "-b", "transparent", "-t", theme, "-s", "2", "-q")
		if msg, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("mmdc: %s", firstLine(strings.TrimSpace(string(msg))+" "+err.Error()))
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
		cmd := exec.Command("typst", "compile", "--format", "png", "--ppi", "144", in, out)
		if msg, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("typst: %s", firstLine(strings.TrimSpace(string(msg))+" "+err.Error()))
		}
		return os.ReadFile(out)
	}
	return nil, fmt.Errorf("unknown renderer %q", kind)
}

var _ = lipgloss.Width
