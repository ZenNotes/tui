package vault

import "testing"

func TestAnchorLineHeadings(t *testing.T) {
	body := "---\ntitle: x\n---\n# Top\n\n## Section Three\n\n```\n## Not a heading\n```\n\n### C++ & Go!\n\nSetext\n---\n"
	for _, tc := range []struct {
		name, target string
		want         int
	}{
		{"by text", "Doc#Section Three", 6},
		{"case-insensitive", "Doc#section three", 6},
		{"percent-encoded", "#Section%20Three", 6},
		{"slug", "#section-three", 6},
		{"punctuation in the slug", "#c--go", 12},
		{"setext", "#Setext", 14},
		{"inside a fence", "#Not a heading", 0},
		{"missing", "Doc#Nope", 0},
		{"no anchor", "Doc", 0},
		{"empty anchor", "Doc#", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AnchorLine(body, tc.target)
			if tc.want == 0 {
				if ok {
					t.Fatalf("found line %d", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Fatalf("got %d (%v), want %d", got, ok, tc.want)
			}
		})
	}
}

// An exact heading beats a slug that collides with it, wherever it sits.
func TestAnchorLineExactBeatsSlug(t *testing.T) {
	body := "# Foo Bar\n\n# foo-bar\n"
	if got, ok := AnchorLine(body, "#foo-bar"); !ok || got != 3 {
		t.Fatalf("got %d (%v), want 3", got, ok)
	}
}

func TestAnchorLineBlocks(t *testing.T) {
	body := "---\nid: ^front\n---\n# Doc\n\nFirst para ^para-one\n\n- item ^LIST\n\nabove one\nabove two\n\n^standalone\n\n```\ncode ^fenced\n```\n\n^after-fence\n\n2^3 and x ^ y\n"
	for _, tc := range []struct {
		name, target string
		want         int
	}{
		{"caret form", "Doc^para-one", 6},
		{"Obsidian form", "Doc#^para-one", 6},
		{"same note", "^para-one", 6},
		{"case-insensitive", "#^list", 8},
		{"standalone marks the block above", "^standalone", 10},
		{"standalone marks the fence above", "^after-fence", 15},
		{"inside a fence", "^fenced", 0},
		{"inside frontmatter", "^front", 0},
		{"an operator is no marker", "^3", 0},
		{"missing", "^nope", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AnchorLine(body, tc.target)
			if tc.want == 0 {
				if ok {
					t.Fatalf("found line %d", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Fatalf("got %d (%v), want %d", got, ok, tc.want)
			}
		})
	}
}

// Whichever marker comes first owns the anchor: `Doc#Heading^tail` is a heading
// link, so a block of that id is not where it goes.
func TestAnchorKindIsDecidedByTheFirstMarker(t *testing.T) {
	body := "# Heading^tail\n\nprose ^tail\n"
	if got, ok := AnchorLine(body, "Doc#Heading^tail"); !ok || got != 1 {
		t.Fatalf("heading link: got %d (%v), want 1", got, ok)
	}
	if got, ok := AnchorLine(body, "Doc^tail#x"); ok {
		t.Fatalf("block id with a hash in it found line %d", got)
	}
}

func TestIsSameNoteAnchor(t *testing.T) {
	for target, want := range map[string]bool{
		"#Heading": true,
		"^id":      true,
		"#^id":     true,
		"Doc":      false,
		"Doc#H":    false,
		"Doc^id":   false,
		"#":        false,
		"":         false,
	} {
		if got := IsSameNoteAnchor(target); got != want {
			t.Errorf("%q: got %v, want %v", target, got, want)
		}
	}
}
