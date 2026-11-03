package vault

import "testing"

// The cases of the app's internal-links.test.ts, plus the reported link.
func TestResolveNoteHref(t *testing.T) {
	notes := []NoteMeta{
		{Path: "Work/Documentation/Vault CLI Cheatsheet.md", Folder: FolderInbox},
		{Path: "Work/Documentation/Another Note.md", Folder: FolderInbox},
		{Path: "Work/Projects/plan.md", Folder: FolderInbox},
		{Path: "Work/Daily/2020-03-31.md", Folder: FolderInbox},
		{Path: "Daily/2020-03-31.md", Folder: FolderInbox},
		{Path: "Work/v1.2.md", Folder: FolderInbox},
		{Path: "index.md", Folder: FolderInbox},
		{Path: "Archive/old plan.md", Folder: FolderTrash},
		{Path: "Moved/unique.md", Folder: FolderInbox},
		{Path: "One/twin.md", Folder: FolderInbox},
		{Path: "Two/twin.md", Folder: FolderInbox},
	}
	const from = "Work/Documentation/Vault CLI Cheatsheet.md"
	for _, tc := range []struct {
		name, from, href, want string
	}{
		{"same folder", from, "Another Note.md", "Work/Documentation/Another Note.md"},
		{"up a folder", from, "../Projects/plan.md", "Work/Projects/plan.md"},
		{"vault root", from, "/index.md", "index.md"},
		{"percent-encoded", from, "Another%20Note.md", "Work/Documentation/Another Note.md"},
		{"anchor", from, "Another%20Note.md#My%20Heading", "Work/Documentation/Another Note.md"},
		{"block anchor", from, "Another%20Note.md#^id", "Work/Documentation/Another Note.md"},
		{"no extension", from, "Another Note", "Work/Documentation/Another Note.md"},
		{"case-insensitive", from, "another note.MD", "Work/Documentation/Another Note.md"},
		{"reported link", "Work/Index.md", "./Daily/2020-03-31", "Work/Daily/2020-03-31.md"},
		{"relative beats the same path from the root", "Work/Index.md", "./Daily/2020-03-31.md", "Work/Daily/2020-03-31.md"},
		{"from the vault root", "Index.md", "./Daily/2020-03-31", "Daily/2020-03-31.md"},
		{"no current note", "", "./Daily/2020-03-31", "Daily/2020-03-31.md"},
		{"a dot in the name", "Work/Index.md", "./v1.2", "Work/v1.2.md"},
		{"moved note, unique name", from, "../Gone/unique", "Moved/unique.md"},
		{"moved note, ambiguous name", from, "../Gone/twin", ""},
		{"trashed", from, "../../Archive/old plan.md", ""},
		{"escapes the vault", "index.md", "../secrets.md", ""},
		{"same-note anchor", from, "#Heading", ""},
		{"url", from, "https://example.com/a.md", ""},
		{"scheme", from, "mailto:a@example.com", ""},
		{"protocol-relative", from, "//example.com/a.md", ""},
		{"empty", from, "  ", ""},
		{"no match", from, "Nope", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveNoteHref(notes, tc.from, tc.href)
			if tc.want == "" {
				if ok {
					t.Fatalf("resolved to %s", got.Path)
				}
				return
			}
			if !ok || got.Path != tc.want {
				t.Fatalf("got %q (%v), want %q", got.Path, ok, tc.want)
			}
		})
	}
}
