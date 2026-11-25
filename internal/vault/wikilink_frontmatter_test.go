package vault

import (
	"reflect"
	"testing"
)

func TestExtractWikilinksSkipsFrontmatter(t *testing.T) {
	body := "---\nrelated: \"[[Hidden]]\"\n---\n\nSee [[Shown]].\n"
	got := ExtractWikilinks(body)
	want := []string{"Shown"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractWikilinks = %v, want %v", got, want)
	}
}

func TestExtractWikilinksFrontmatterOnly(t *testing.T) {
	got := ExtractWikilinks("---\nup: [[Hidden]]\n---\n")
	if len(got) != 0 {
		t.Fatalf("ExtractWikilinks = %v, want none", got)
	}
}

func TestExtractWikilinksFrontmatterEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"empty block", "---\n---\nSee [[Shown]].\n", []string{"Shown"}},
		{"no closing fence", "---\n[[A]]\n", []string{"A"}},
		{"code span after frontmatter", "---\nup: [[Hidden]]\n---\n`[[Code]]` and [[Shown]]\n", []string{"Shown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractWikilinks(tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractWikilinks = %v, want %v", got, tc.want)
			}
		})
	}
}
