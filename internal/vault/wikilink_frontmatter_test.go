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
