package vault

import "testing"

// The cases of the app's internal-links.test.ts for externalLinkUrl.
func TestExternalLinkURL(t *testing.T) {
	for href, want := range map[string]string{
		"https://example.com":    "https://example.com",
		"HTTP://example.com":     "HTTP://example.com",
		"mailto:a@example.com":   "mailto:a@example.com",
		"tel:+15551234":          "tel:+15551234",
		"google.com":             "https://google.com",
		"example.com/path":       "https://example.com/path",
		"example.com?q=1":        "https://example.com?q=1",
		"sub.example.co.uk#frag": "https://sub.example.co.uk#frag",
		"  google.com  ":         "https://google.com",
		"Note.md":                "",
		"Note.md#Heading":        "",
		"photo.png":              "",
		"docs.pdf":               "",
		"My Note.md":             "",
		"My Note":                "",
		"Note":                   "",
		"./google.com":           "",
		"../Projects/plan.md":    "",
		"/index.md":              "",
		"//example.com":          "",
		"#heading":               "",
		"zotero:abc":             "",
		"":                       "",
	} {
		got, ok := ExternalLinkURL(href)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q (%v), want %q", href, got, ok, want)
		}
	}
}

func TestUnwrapMarkdownDest(t *testing.T) {
	for dest, want := range map[string]string{
		"Note.md":              "Note.md",
		"  Note.md  ":          "Note.md",
		"<./My Note>":          "./My Note",
		"<a) b.md>":            "a) b.md",
		`Note.md "Title"`:      "Note.md",
		`Note.md 'Title'`:      "Note.md",
		`<My Note.md> "Title"`: "My Note.md",
		"My Note.md":           "My Note.md",
		"<":                    "<",
	} {
		if got := UnwrapMarkdownDest(dest); got != want {
			t.Errorf("%q: got %q, want %q", dest, got, want)
		}
	}
}

func TestHrefFragment(t *testing.T) {
	for href, want := range map[string]string{
		"Note.md":              "",
		"Note.md#":             "",
		"Note.md#Heading":      "Heading",
		"Note.md#My%20Heading": "My Heading",
		"Note.md#^id":          "^id",
		"Note.md#%":            "%",
		"Note.md#a#b":          "a#b",
	} {
		if got := HrefFragment(href); got != want {
			t.Errorf("%q: got %q, want %q", href, got, want)
		}
	}
}

func TestHrefNamesFile(t *testing.T) {
	for href, want := range map[string]bool{
		"./photo.png":      true,
		"docs/a.PDF":       true,
		"./notes.txt":      true,
		"~":                true,
		"~/bin/build.sh":   true,
		"/usr/bin/tool.sh": true,
		"./photo.png#frag": true,
		"./My Note":        false,
		"./My Note.md":     false,
		"./v1.2":           false,
		"/notes/plan":      false,
		"/notes/plan.md":   false,
	} {
		if got := HrefNamesFile(href); got != want {
			t.Errorf("%q: got %v, want %v", href, got, want)
		}
	}
}

func TestNoteHrefPathIgnoresWhetherTheNoteExists(t *testing.T) {
	for _, tc := range []struct{ from, href, want string }{
		{"Work/Index.md", "./My Note", "Work/My Note"},
		{"Work/Index.md", "../Other/New.md#Heading", "Other/New.md"},
		{"Work/Index.md", "My%20Note", "Work/My Note"},
		{"Work/Index.md", "/Top/New", "Top/New"},
		{"Index.md", "../escape", ""},
		{"Index.md", "#heading", ""},
		{"Index.md", "https://example.com", ""},
	} {
		got, ok := NoteHrefPath(tc.from, tc.href)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s from %s: got %q (%v), want %q", tc.href, tc.from, got, ok, tc.want)
		}
	}
}
