package tui

import (
	"bytes"
	"image"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fullPNG is a w x h picture inked edge to edge, so cropToInk keeps its size.
func fullPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return inkPNG(t, w, h, image.Rect(0, 0, w, h))
}

func testStore() (*imageStore, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &imageStore{byKey: map[string]*termImage{}, nextID: 1000, cellW: 10, cellH: 20, out: out}, out
}

// sends counts the pictures transmitted to out since the last call.
func sends(out *bytes.Buffer) int {
	n := strings.Count(out.String(), "a=T,")
	out.Reset()
	return n
}

func TestPictureGrowsAndShrinksWithThePane(t *testing.T) {
	s, out := testStore()
	data := fullPNG(t, 1552, 122) // 156 cells wide at 10 px a cell
	load := func() ([]byte, error) { return data, nil }

	narrow := s.imageFor("wide", 30, 40, load)
	if narrow.cols != 30 || sends(out) != 1 {
		t.Fatalf("narrow pane: %d cols, want 30, sent once", narrow.cols)
	}
	wide := s.imageFor("wide", 120, 40, func() ([]byte, error) { t.Fatal("loaded again"); return nil, nil })
	if wide.cols != 120 || wide.id == narrow.id || sends(out) != 1 {
		t.Fatalf("wider pane: %d cols (id %d, was %d), want 120 under a new id, sent once", wide.cols, wide.id, narrow.id)
	}
	if wide.rows < narrow.rows {
		t.Fatalf("height shrank as the picture grew: %d rows from %d", wide.rows, narrow.rows)
	}
	again := s.imageFor("wide", 30, 40, load)
	if again.id != narrow.id || sends(out) != 0 {
		t.Fatalf("back to the narrow pane sent a new picture (id %d, was %d)", again.id, narrow.id)
	}
}

func TestPictureAtItsOwnSizeIsSentOnce(t *testing.T) {
	s, out := testStore()
	data := fullPNG(t, 120, 40) // 12 cells wide
	load := func() ([]byte, error) { return data, nil }
	first := s.imageFor("small", 30, 40, load)
	for _, cols := range []int{60, 120, 13} {
		if img := s.imageFor("small", cols, 40, load); img.id != first.id || img.cols != 12 {
			t.Fatalf("at %d cols: id %d cols %d, want id %d at its own 12 cols", cols, img.id, img.cols, first.id)
		}
	}
	if n := sends(out); n != 1 {
		t.Fatalf("a picture that fits every pane was sent %d times, want 1", n)
	}
}

func TestOldestPictureSizeIsFreed(t *testing.T) {
	s, out := testStore()
	data := fullPNG(t, 1552, 122)
	load := func() ([]byte, error) { return data, nil }
	first := s.imageFor("wide", 20, 40, load)
	for _, cols := range []int{40, 60, 80} {
		s.imageFor("wide", cols, 40, load)
	}
	if !strings.Contains(out.String(), "a=d,d=I,i="+strconv.FormatUint(uint64(first.id), 10)+",") {
		t.Fatalf("the fourth size did not free the first (id %d):\n%q", first.id, out.String())
	}
	out.Reset()
	if img := s.imageFor("wide", 20, 40, load); img.id == first.id || sends(out) != 1 {
		t.Fatalf("a freed size came back under its old id %d", first.id)
	}
}

func TestKeptPictureBytesAreCapped(t *testing.T) {
	prev := maxPicBytes
	t.Cleanup(func() { maxPicBytes = prev })
	s, _ := testStore()
	data := fullPNG(t, 400, 100)
	maxPicBytes = 2*len(data) + len(data)/2
	for _, k := range []string{"a", "b", "c"} {
		s.imageFor(k, 80, 40, func() ([]byte, error) { return data, nil })
	}
	if _, ok := s.pics["a"]; ok || len(s.pics) != 2 {
		t.Fatalf("kept %d pictures, want the 2 most recent: %v", len(s.pics), s.pics)
	}
	if s.refit("a", 20, 40) != nil {
		t.Fatal("refit of a dropped picture did not report it")
	}
	if img := s.byKey["a"]; img == nil || img.err != nil {
		t.Fatalf("dropping the bytes broke the picture on screen: %+v", img)
	}
}

// placeholderCols is how many placeholder cells the first picture row of a
// preview has.
func placeholderCols(lines []previewLine) int {
	for _, l := range lines {
		if n := strings.Count(l.text, "\U0010EEEE"); n > 0 {
			return n
		}
	}
	return 0
}

func TestLatexPictureRefitsWhenThePreviewWidens(t *testing.T) {
	latexEnv(t)
	fakeTeX(t, texFake{png: fullPNG(t, 1552, 122)})
	a, done := newMathApp(t, mathPrefs("latex", "kitty"))
	body := "\\[ a_1 + a_2 + \\dots + a_{20} \\]"
	a.mathLines(body, 0, 32)
	if !nextRender(done, 5*time.Second) {
		t.Fatal("no render finished")
	}
	narrow := placeholderCols(a.mathLines(body, 0, 32))
	wide := placeholderCols(a.mathLines(body, 0, 122))
	if narrow != 30 || wide != 120 {
		t.Fatalf("picture is %d cells in a 32-cell preview and %d in a 122-cell one, want 30 then 120", narrow, wide)
	}
	if back := placeholderCols(a.mathLines(body, 0, 32)); back != 30 {
		t.Fatalf("narrowed again, the picture is %d cells, want 30", back)
	}
}
