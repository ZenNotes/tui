package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDailyNoteDateNeedsAllThreeConditions(t *testing.T) {
	for _, tc := range []struct {
		name, settings, rel, title, want string
	}{
		{"all three hold", `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":true}}`,
			"inbox/Daily Notes/2026-11-06.md", "2026-11-06", "2026-11-06"},
		{"daily notes off", `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":false}}`,
			"inbox/Daily Notes/2026-11-06.md", "2026-11-06", ""},
		{"other folder", `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":true}}`,
			"inbox/Projects/2026-11-06.md", "2026-11-06", ""},
		{"title off pattern", `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":true}}`,
			"inbox/Daily Notes/Plans.md", "Plans", ""},
		{"archive folder", `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":true}}`,
			"archive/Daily Notes/2026-11-06.md", "2026-11-06", ""},
		{"setting for due on note date is off", `{"primaryNotesLocation":"inbox","dailyNotes":{"enabled":true,"tasksDueOnNoteDate":false}}`,
			"inbox/Daily Notes/2026-11-06.md", "2026-11-06", "2026-11-06"},
		{"custom folder and title", `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"Days","titlePattern":"yyyy.MM.dd"}}`,
			"Days/2026.11.06.md", "2026.11.06", "2026-11-06"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".zennotes"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".zennotes", "vault.json"), []byte(tc.settings), 0o644); err != nil {
				t.Fatal(err)
			}
			b, err := New(Target{Kind: KindLocal, Root: root}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			fn := b.(*Local).Vault().DailyNoteDate
			if fn == nil {
				t.Fatal("DailyNoteDate is not set")
			}
			got, ok := fn(tc.rel, tc.title)
			if ok != (tc.want != "") || got != tc.want {
				t.Fatalf("DailyNoteDate(%q, %q) = %q, %v; want %q", tc.rel, tc.title, got, ok, tc.want)
			}
		})
	}
}
