package vim

import "testing"

func TestDisplayRowBoundariesRespectCountsAndLogicalMode(t *testing.T) {
	opts := DefaultOptions()
	opts.WrappedLineMotions = true
	ed := New("abcdefghij\nsecond", opts, Hooks{DisplayRowBounds: func(p Pos) (int, int) {
		if p.Line == 0 {
			if p.Col < 5 {
				return 0, 5
			}
			return 5, 10
		}
		return 0, 6
	}})
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 4 {
		t.Fatal(ed.Cursor())
	}
	ed.SetCursor(Pos{})
	ed.HandleKey(R('A'))
	if ed.Cursor().Col != 5 {
		t.Fatal(ed.Cursor())
	}
	ed.HandleKey(Special("esc"))
	ed.SetCursor(Pos{0, 7})
	ed.HandleKey(R('I'))
	if ed.Cursor().Col != 5 {
		t.Fatal(ed.Cursor())
	}
	ed.HandleKey(Special("esc"))
	ed.SetCursor(Pos{})
	ed.HandleKey(R('2'))
	ed.HandleKey(R('$'))
	if ed.Cursor() != (Pos{1, 5}) {
		t.Fatal(ed.Cursor())
	}
	opts.WrappedLineMotions = false
	ed.SetOptions(opts)
	ed.SetCursor(Pos{})
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 9 {
		t.Fatal(ed.Cursor())
	}
}

func TestSecondDollarOnAWrappedRowReachesTheLineEnd(t *testing.T) {
	opts := DefaultOptions()
	opts.WrappedLineMotions = true
	rows := Hooks{DisplayRowBounds: func(p Pos) (int, int) {
		if p.Col < 5 {
			return 0, 5
		}
		if p.Col < 10 {
			return 5, 10
		}
		return 10, 14
	}}
	ed := New("abcdefghijklmn", opts, rows)
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 4 {
		t.Fatalf("first $ should stop at the row end: %v", ed.Cursor())
	}
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 13 {
		t.Fatalf("second $ should reach the line end: %v", ed.Cursor())
	}
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 13 {
		t.Fatalf("$ at the line end should stay: %v", ed.Cursor())
	}
	// g$ is the row motion, so it never runs on past the row.
	ed.SetCursor(Pos{0, 4})
	ed.Feed("g$")
	if ed.Cursor().Col != 4 {
		t.Fatalf("g$ at the row end left the row: %v", ed.Cursor())
	}
	// Operators keep the visible row, even from its last character.
	ed.SetCursor(Pos{0, 4})
	ed.Feed("d$")
	if ed.Text() != "abcdfghijklmn" {
		t.Fatalf("d$ at the row end: %q", ed.Text())
	}
	// Visual mode extends to the line end the same way.
	ed = New("abcdefghijklmn", opts, rows)
	ed.Feed("v$$")
	if ed.Cursor().Col != 13 || ed.Mode() != ModeVisual {
		t.Fatalf("v$$: %v %v", ed.Cursor(), ed.Mode())
	}
}
