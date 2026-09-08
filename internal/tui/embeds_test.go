package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
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
	spans := highlightFences(lines, "gruvbox")
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
	if len(highlightFences([]string{"```", "x", "```"}, "gruvbox")) != 0 {
		t.Fatal("a fence without a language stays plain")
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
	blocks := splitBlocks([]string{"text", "$$", "x^2", "$$", "$$ y $$", "more"})
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
