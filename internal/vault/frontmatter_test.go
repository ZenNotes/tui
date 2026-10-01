package vault

import "testing"

// An empty block is still a block: record pages without properties carry
// one, and treating it as body text made every re-mirror add another.
func TestFrontmatterRecognisesAnEmptyBlock(t *testing.T) {
	cases := []struct {
		body, block, rest string
		ok                bool
	}{
		{"---\n---\n# Title\n\nbody\n", "", "# Title\n\nbody\n", true},
		{"---\r\n---\r\n# T\n", "", "# T\n", true},
		{"---\n---", "", "", true},
		{"---\ntitle: x\n---\n# T\n", "title: x", "# T\n", true},
		{"---\ntitle: x\ntags: [a]\n---\nbody", "title: x\ntags: [a]", "body", true},
		// A later horizontal rule is body, not the closing fence.
		{"---\n---\n# T\n\n---\n\nmore\n", "", "# T\n\n---\n\nmore\n", true},
		{"---\n----- not a fence\ntitle: x\n---\nbody", "----- not a fence\ntitle: x", "body", true},
		{"# No frontmatter\n---\n---\n", "", "# No frontmatter\n---\n---\n", false},
		{"---\nunterminated\n", "", "---\nunterminated\n", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		block, rest, ok := Frontmatter(c.body)
		if ok != c.ok || block != c.block || rest != c.rest {
			t.Errorf("Frontmatter(%q) = (%q, %q, %v), want (%q, %q, %v)", c.body, block, rest, ok, c.block, c.rest, c.ok)
		}
	}
}

func TestPrependKeepsAnEmptyFrontmatterBlockOnTop(t *testing.T) {
	got := PrependToBody("---\n---\n# T\n", "new line")
	if got != "---\n---\nnew line\n\n# T\n" {
		t.Fatalf("prepend: %q", got)
	}
	if excerpt := BuildExcerpt("---\n---\n# Real title\n\nThe body.\n"); excerpt != "Real title The body." {
		t.Fatalf("excerpt after empty block: %q", excerpt)
	}
}
