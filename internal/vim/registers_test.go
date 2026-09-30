package vim

import "testing"

func TestSharedRegistersPreservePasteShape(t *testing.T) {
	for _, tc := range []struct{ name, source, yank, target, want string }{
		{"characters", "alpha beta", "yiw", "123", "1alpha23"},
		{"lines", "alpha\nsecond", "yy", "target", "target\nalpha"},
		{"block", "abXY\ncdZW", "<C-v>ljy", "12\n34", "1ab2\n3cd4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var store RegisterStore
			source := NewWithRegisters(tc.source, DefaultOptions(), Hooks{}, &store)
			target := NewWithRegisters(tc.target, DefaultOptions(), Hooks{}, &store)
			source.Feed(tc.yank)
			target.Feed("p")
			if got := target.Text(); got != tc.want {
				t.Fatalf("paste: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSharedNamedAppendAndNumberedDeleteRegisters(t *testing.T) {
	var store RegisterStore
	first := NewWithRegisters("alpha\nkeep", DefaultOptions(), Hooks{}, &store)
	second := NewWithRegisters("beta\nstay", DefaultOptions(), Hooks{}, &store)
	first.Feed(`"ayy`)
	second.Feed(`"Ayy`)
	target := NewWithRegisters("target", DefaultOptions(), Hooks{}, &store)
	target.Feed(`"ap`)
	if got := target.Text(); got != "target\nalpha\nbeta" {
		t.Fatalf("named append across notes: %q", got)
	}
	first.Feed("dd")
	second.Feed("dd")
	target.Feed(`gg"2p`)
	if got := target.Text(); got != "target\nalpha\nalpha\nbeta" {
		t.Fatalf("numbered delete history across notes: %q", got)
	}
}

func TestSharedRegistersRespectBlackHoleAndLocalUndo(t *testing.T) {
	var store RegisterStore
	first := NewWithRegisters("alpha", DefaultOptions(), Hooks{}, &store)
	second := NewWithRegisters("beta\nstay", DefaultOptions(), Hooks{}, &store)
	first.Feed("yy")
	second.Feed(`"_ddp`)
	if got := second.Text(); got != "stay\nalpha" {
		t.Fatalf("black-hole delete replaced shared yank: %q", got)
	}
	first.Feed("u")
	if first.Text() != "alpha" || second.Text() != "stay\nalpha" {
		t.Fatal("an editor undid another editor's change")
	}
}

func TestSharedBlockYankRetainsItsShapeWithClipboardEnabled(t *testing.T) {
	var store RegisterStore
	clipboard := ""
	hooks := Hooks{ReadClipboard: func() (string, bool) { return clipboard, true }, WriteClipboard: func(text string) bool { clipboard = text; return true }}
	opts := DefaultOptions()
	opts.YankToClipboard = true
	first := NewWithRegisters("abXY\ncdZW", opts, hooks, &store)
	second := NewWithRegisters("12\n34", opts, hooks, &store)
	first.Feed("<C-v>ljy")
	second.Feed("p")
	if got := second.Text(); got != "1ab2\n3cd4" {
		t.Fatalf("clipboard lost shared block metadata: %q", got)
	}
}

func TestPrivateEditorsDoNotShareRegisters(t *testing.T) {
	first := New("alpha", DefaultOptions(), Hooks{})
	second := New("beta", DefaultOptions(), Hooks{})
	first.Feed("yy")
	second.Feed("p")
	if second.Text() != "beta" {
		t.Fatal("standalone editors unexpectedly shared registers")
	}
}
