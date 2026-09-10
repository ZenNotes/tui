package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/image/draw"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/webp"
)

// Terminal images use Kitty's graphics protocol with Unicode placeholders.
// An image is transmitted once as a virtual placement (an id, a size in
// cells); the preview then prints rows of U+10EEEE placeholder cells whose
// foreground color carries the id and whose combining diacritics carry the
// row and column, and the terminal paints the picture over them. Because
// placeholders are ordinary characters, redraws, scrolling, splits and
// tmux (with allow-passthrough on) all keep working.

type imageMode int

const (
	imagesOff imageMode = iota
	imagesKitty
)

// imageMode is the preference resolved against the terminal: auto turns
// images on for Kitty and Ghostty, the terminals that implement Unicode
// placeholders, and stays off elsewhere.
func (a *App) imageMode() imageMode {
	switch strings.ToLower(strings.TrimSpace(a.prefs.TerminalImages)) {
	case "off", "false", "no", "none":
		return imagesOff
	case "kitty", "on", "true":
		return imagesKitty
	}
	if terminalSupportsKittyPlaceholders() {
		return imagesKitty
	}
	return imagesOff
}

func terminalSupportsKittyPlaceholders() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "ghostty", "kitty":
		return true
	}
	return strings.Contains(strings.ToLower(os.Getenv("TERM")), "kitty")
}

func insideTmux() bool { return os.Getenv("TMUX") != "" }

// termImage is one transmitted picture.
type termImage struct {
	id         uint32
	cols, rows int
	width      int
	height     int
	err        error
}

type imageStore struct {
	mu     sync.Mutex
	byKey  map[string]*termImage
	nextID uint32
	cellW  int
	cellH  int
	out    io.Writer
}

func newImageStore() *imageStore {
	s := &imageStore{byKey: map[string]*termImage{}, nextID: 1000, out: os.Stdout}
	s.cellW, s.cellH = cellPixelSize()
	return s
}

func (a *App) images() *imageStore {
	if a.imageStore == nil {
		a.imageStore = newImageStore()
	}
	return a.imageStore
}

// imageFor returns the transmitted image for a key, loading and sending it
// on first use. maxCols bounds the width; the height follows the picture's
// aspect ratio at the terminal's cell size, capped at maxRows.
func (s *imageStore) imageFor(key string, maxCols, maxRows int, load func() ([]byte, error)) *termImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if img, ok := s.byKey[key]; ok && (img.err != nil || img.cols <= maxCols) {
		return img
	}
	data, err := load()
	if err != nil {
		img := &termImage{err: err}
		s.byKey[key] = img
		return img
	}
	pngData, w, h, err := toPNG(data, 1600)
	if err != nil {
		img := &termImage{err: err}
		s.byKey[key] = img
		return img
	}
	cols := max(1, min(maxCols, (w+s.cellW-1)/s.cellW))
	rows := max(1, int(float64(h)*float64(cols)*float64(s.cellW)/float64(w)/float64(s.cellH)+0.5))
	if maxRows > 0 && rows > maxRows {
		rows = maxRows
		cols = max(1, int(float64(w)*float64(rows)*float64(s.cellH)/float64(h)/float64(s.cellW)+0.5))
	}
	s.nextID++
	img := &termImage{id: s.nextID, cols: cols, rows: rows, width: w, height: h}
	if err := kittyTransmit(s.out, img.id, cols, rows, pngData, insideTmux()); err != nil {
		img.err = err
	}
	s.byKey[key] = img
	return img
}

// toPNG decodes any supported picture and re-encodes it as PNG, scaled
// down when wider than maxWidth so a huge photo does not flood the terminal.
func toPNG(data []byte, maxWidth int) ([]byte, int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	if format == "png" && cfg.Width <= maxWidth {
		return data, cfg.Width, cfg.Height, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	w, h := cfg.Width, cfg.Height
	if w > maxWidth {
		h = max(1, h*maxWidth/w)
		w = maxWidth
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		src = dst
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), w, h, nil
}

// kittyTransmit sends a PNG as a virtual placement: `a=T` transmits and
// places, `U=1` asks for Unicode placeholders, `q=2` silences replies.
// Payload chunks are 4096 base64 characters, `m=1` until the last.
func kittyTransmit(w io.Writer, id uint32, cols, rows int, pngData []byte, tmux bool) error {
	encoded := base64.StdEncoding.EncodeToString(pngData)
	var out strings.Builder
	first := true
	for len(encoded) > 0 {
		n := min(4096, len(encoded))
		chunk := encoded[:n]
		encoded = encoded[n:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		var ctrl string
		if first {
			ctrl = fmt.Sprintf("a=T,f=100,t=d,q=2,U=1,i=%d,c=%d,r=%d,m=%d", id, cols, rows, more)
			first = false
		} else {
			ctrl = fmt.Sprintf("m=%d", more)
		}
		out.WriteString(kittyAPC(ctrl+";"+chunk, tmux))
	}
	_, err := io.WriteString(w, out.String())
	return err
}

// kittyDelete frees an image and its placements.
func kittyDelete(w io.Writer, id uint32, tmux bool) {
	_, _ = io.WriteString(w, kittyAPC(fmt.Sprintf("a=d,d=I,i=%d,q=2", id), tmux))
}

// kittyAPC wraps a graphics command in the APC frame, and in tmux's
// passthrough envelope when inside tmux.
func kittyAPC(payload string, tmux bool) string {
	seq := "\x1b_G" + payload + "\x1b\\"
	if tmux {
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// placeholderRows renders the placeholder cells for an image: rows of
// U+10EEEE with the row and column diacritics, the id in the foreground
// color and its high byte, when any, in a third diacritic. Each row is
// exactly cols cells wide.
func placeholderRows(id uint32, cols, rows int) []string {
	cols = min(cols, len(kittyDiacritics))
	rows = min(rows, len(kittyDiacritics))
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	high := ""
	if hb := (id >> 24) & 0xff; hb != 0 && int(hb) < len(kittyDiacritics) {
		high = string(kittyDiacritics[hb])
	}
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString(fg)
		for c := 0; c < cols; c++ {
			b.WriteRune(0x10EEEE)
			b.WriteRune(kittyDiacritics[r])
			b.WriteRune(kittyDiacritics[c])
			b.WriteString(high)
		}
		b.WriteString("\x1b[39m")
		out = append(out, b.String())
	}
	return out
}
