package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
	"github.com/charmbracelet/x/ansi"
)

// linkTestApp is an app over a vault holding the given notes, by relative path.
func linkTestApp(t *testing.T, notes map[string]string) (*App, string) {
	t.Helper()
	a, root := newFeatureTestApp(t)
	for rel, body := range notes {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := a.loadIndexCmd()().(indexLoadedMsg)
	if m.err != nil {
		t.Fatal(m.err)
	}
	a.idx = m.idx
	return a, root
}

// gdOn opens a note and presses `gd` on the first occurrence of needle in it.
func gdOn(t *testing.T, a *App, notePath, needle string) {
	t.Helper()
	a.openNote(notePath, true)
	buf := a.activeBuffer()
	if buf == nil || buf.path != notePath {
		t.Fatalf("did not open %s", notePath)
	}
	for i := 0; i < buf.ed.LineCount(); i++ {
		if col := strings.Index(buf.ed.Line(i), needle); col >= 0 {
			buf.ed.SetCursor(vim.Pos{Line: i, Col: len([]rune(buf.ed.Line(i)[:col]))})
			a.followLinkAtCursor(buf)
			return
		}
	}
	t.Fatalf("%q is not in %s", needle, notePath)
}

func lastToast(a *App) string {
	if len(a.toasts) == 0 {
		return ""
	}
	return a.toasts[len(a.toasts)-1].text
}

func TestLinkAtReadsDestinationsLikeTheDesktop(t *testing.T) {
	for _, tc := range []struct {
		name, line, kind, target string
	}{
		{"angle brackets with spaces", "[a](<./My Note>)", "path", "./My Note"},
		{"angle brackets holding a parenthesis", "[a](<./My (old) Note>)", "path", "./My (old) Note"},
		{"angle brackets and a title", `[a](<./My Note> "Title")`, "path", "./My Note"},
		{"spaces without brackets", "[a](My Note.md)", "path", "My Note.md"},
		{"a double-quoted title", `[a](Note.md "Title")`, "path", "Note.md"},
		{"a single-quoted title", `[a](Note.md 'Title')`, "path", "Note.md"},
		{"an anchor", "[a](Note.md#Heading)", "path", "Note.md#Heading"},
		{"same-note anchor", "[a](#Heading)", "path", "#Heading"},
		{"bare domain", "[a](google.com)", "url", "https://google.com"},
		{"bare domain and path", "[a](example.com/x?y=1)", "url", "https://example.com/x?y=1"},
		{"a file is no domain", "[a](photo.png)", "path", "photo.png"},
		{"a note is no domain", "[a](Note.md)", "path", "Note.md"},
		{"a relative path is no domain", "[a](./google.com)", "path", "./google.com"},
		{"explicit url", "[a](https://example.com)", "url", "https://example.com"},
		{"tel", "[a](tel:+15551234)", "url", "tel:+15551234"},
		{"mailto", "[a](mailto:a@example.com)", "url", "mailto:a@example.com"},
		{"wikilink keeps its heading", "[[Doc#Heading|alias]]", "wikilink", "Doc#Heading"},
		{"wikilink keeps its block", "[[Doc^id]]", "wikilink", "Doc^id"},
		{"wikilink same-note heading", "[[#Heading]]", "wikilink", "#Heading"},
		{"wikilink naming a domain", "[[2024.01.15]]", "wikilink", "2024.01.15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := linkAt(tc.line, 1)
			if !ok || got.kind != tc.kind || got.target != tc.target {
				t.Fatalf("linkAt: %+v (%v), want %s %q", got, ok, tc.kind, tc.target)
			}
			links := linksOnLine(tc.line)
			if len(links) != 1 || links[0].kind != tc.kind || links[0].target != tc.target {
				t.Fatalf("linksOnLine: %+v, want one %s %q", links, tc.kind, tc.target)
			}
		})
	}
}

func TestGdFollowsAngleBracketLinks(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{
		"Work/Index.md":   "See [a](<./My Note>) here\n",
		"Work/My Note.md": "# My Note\n",
	})
	gdOn(t, a, "Work/Index.md", "[a]")
	if got := a.activeBuffer().path; got != "Work/My Note.md" {
		t.Fatalf("opened %s", got)
	}
}

func TestGdLandsOnTheAnchor(t *testing.T) {
	target := "---\ntitle: x\n---\n# Target\n\nintro\n\n## Second Part\n\nprose ^marked\n"
	const headingLine, blockLine = 7, 9
	for _, tc := range []struct {
		name, link string
		line       int
	}{
		{"heading", "[a](Target.md#Second%20Part)", headingLine},
		{"heading in angle brackets", "[a](<Target.md#Second Part>)", headingLine},
		{"heading slug", "[a](Target.md#second-part)", headingLine},
		{"block", "[a](Target.md#^marked)", blockLine},
		{"no extension", "[a](./Target#^marked)", blockLine},
		{"missing heading stays at the top", "[a](Target.md#Gone)", 0},
		{"wikilink heading", "[[Target#Second Part]]", headingLine},
		{"wikilink block", "[[Target^marked]]", blockLine},
		{"wikilink Obsidian block", "[[Target#^marked]]", blockLine},
		{"wikilink with alias", "[[Target#Second Part|there]]", headingLine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := linkTestApp(t, map[string]string{
				"Index.md":  tc.link + "\n",
				"Target.md": target,
			})
			gdOn(t, a, "Index.md", tc.link[:3])
			buf := a.activeBuffer()
			if buf.path != "Target.md" {
				t.Fatalf("opened %s", buf.path)
			}
			if got := buf.ed.Cursor().Line; got != tc.line {
				t.Fatalf("cursor on line %d, want %d", got, tc.line)
			}
		})
	}
}

func TestGdLandsOnASameNoteAnchor(t *testing.T) {
	body := "[h](#Later)\n[[#Later]]\n[[^mark]]\n[e](#Section%20Two)\n\n# Later\n\nblock ^mark\n\n# Section Two\n"
	for _, tc := range []struct {
		needle string
		line   int
	}{
		{"[h]", 5},
		{"[[#Later", 5},
		{"[[^mark", 7},
		{"[e]", 9},
	} {
		t.Run(tc.needle, func(t *testing.T) {
			a, _ := linkTestApp(t, map[string]string{"Self.md": body})
			gdOn(t, a, "Self.md", tc.needle)
			if a.overlay != nil {
				t.Fatal("a same-note anchor offered to create a note")
			}
			buf := a.activeBuffer()
			if buf.path != "Self.md" || buf.ed.Cursor().Line != tc.line {
				t.Fatalf("at %s:%d, want Self.md:%d", buf.path, buf.ed.Cursor().Line, tc.line)
			}
		})
	}
}

// Reading mode has its own cursor, so the anchor has to reach it too.
func TestAnchorScrollsReadingMode(t *testing.T) {
	var body strings.Builder
	body.WriteString("# Target\n\n")
	for i := 0; i < 80; i++ {
		body.WriteString("filler\n\n")
	}
	body.WriteString("## Far Down\n\nend\n")
	a, _ := linkTestApp(t, map[string]string{
		"Index.md":  "[a](Target.md#Far%20Down)\n",
		"Target.md": body.String(),
	})
	a.prefs.DefaultPaneMode = string(modePreview)
	gdOn(t, a, "Index.md", "[a]")
	tb := a.activeTab()
	if tb == nil || tb.path != "Target.md" || tb.mode != modePreview {
		t.Fatalf("tab %+v", tb)
	}
	buf := a.activeBuffer()
	out := a.renderPreview(tb, buf, 80, 20, true)
	if !strings.Contains(ansi.Strip(out), "Far Down") {
		t.Fatalf("heading not on screen:\n%s", ansi.Strip(out))
	}
	if tb.preview.scroll == 0 || tb.preview.jumpPending {
		t.Fatalf("preview did not jump: %+v", tb.preview)
	}
	row := tb.preview.cache.lines[tb.preview.cursor]
	if row.srcLine != buf.ed.Cursor().Line {
		t.Fatalf("preview cursor on source line %d, editor on %d", row.srcLine, buf.ed.Cursor().Line)
	}
}

func TestDeadRelativeLinkOffersTheNote(t *testing.T) {
	for _, tc := range []struct {
		name, link, want, message string
	}{
		{"relative", "[a](./Daily/2020-03-31)", "Work/Daily/2020-03-31.md", "Work/Daily/2020-03-31"},
		{"angle brackets", "[a](<./My Note>)", "Work/My Note.md", "Work/My Note"},
		{"bare name", "[a](New.md)", "Work/New.md", "Work/New"},
		{"percent-encoded", "[a](My%20Note)", "Work/My Note.md", "Work/My Note"},
		{"anchor dropped", "[a](./New#Heading)", "Work/New.md", "Work/New"},
		{"up a folder", "[a](../Other/New)", "Other/New.md", "Other/New"},
		{"a dot in the name", "[a](./v1.2)", "Work/v1.2.md", "Work/v1.2"},
		{"from the vault root", "[a](/Top/New)", "Top/New.md", "Top/New"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, root := linkTestApp(t, map[string]string{"Work/Index.md": tc.link + "\n"})
			gdOn(t, a, "Work/Index.md", "[a]")
			dialog, ok := a.overlay.(*confirmDialog)
			if !ok {
				t.Fatalf("no confirm, toast %q", lastToast(a))
			}
			if !strings.Contains(dialog.message, `"`+tc.message+`"`) {
				t.Fatalf("message %q, want %q", dialog.message, tc.message)
			}
			if _, err := os.Stat(filepath.Join(root, tc.want)); !os.IsNotExist(err) {
				t.Fatal("created before the answer")
			}
			a.overlay.handleKey(a, enterKey())
			if _, err := os.Stat(filepath.Join(root, tc.want)); err != nil {
				t.Fatalf("not created: %v", err)
			}
			if got := a.activeBuffer().path; got != tc.want {
				t.Fatalf("opened %s, want %s", got, tc.want)
			}
		})
	}
}

// Declining leaves the vault alone, and the note made on yes is what the link
// then resolves to.
func TestDeadLinkDeclinedThenAccepted(t *testing.T) {
	a, root := linkTestApp(t, map[string]string{"Work/Index.md": "[a](<./My Note>)\n"})
	gdOn(t, a, "Work/Index.md", "[a]")
	a.overlay.handleKey(a, vim.R('n'))
	if a.overlay != nil || a.activeBuffer().path != "Work/Index.md" {
		t.Fatal("declining did not stay put")
	}
	if _, err := os.Stat(filepath.Join(root, "Work/My Note.md")); !os.IsNotExist(err) {
		t.Fatal("declining created the note")
	}

	gdOn(t, a, "Work/Index.md", "[a]")
	a.overlay.handleKey(a, vim.R('y'))
	a.openNote("Work/Index.md", true)
	buf := a.activeBuffer()
	buf.ed.SetCursor(vim.Pos{})
	a.followLinkAtCursor(buf)
	if a.overlay != nil {
		t.Fatal("the created note did not resolve")
	}
	if got := a.activeBuffer().path; got != "Work/My Note.md" {
		t.Fatalf("opened %s", got)
	}
}

func TestDeadLinkThatCannotBeANoteIsNotOffered(t *testing.T) {
	for _, tc := range []struct{ name, link, toast string }{
		{"image", "[a](./photo.png)", "Not a note: photo.png"},
		{"image with a fragment", "[a](./photo.png#x)", "Not a note: photo.png"},
		{"pdf", "[a](docs/a.pdf)", "Not a note: docs/a.pdf"},
		{"home path", "[a](~/bin/build.sh)", "Not a note: ~/bin/build.sh"},
		{"absolute file", "[a](/usr/bin/tool.sh)", "Not a note: /usr/bin/tool.sh"},
		{"escapes the vault", "[a](../../secrets)", "Not a note: ../../secrets"},
		{"other scheme", "[a](zotero:abc)", "Not a note: zotero:abc"},
		{"unsafe name", "[a](./what?)", "Not a note: what?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := linkTestApp(t, map[string]string{"Work/Index.md": tc.link + "\n"})
			gdOn(t, a, "Work/Index.md", "[a]")
			if a.overlay != nil {
				t.Fatal("offered to create a note")
			}
			if got := lastToast(a); got != tc.toast {
				t.Fatalf("toast %q, want %q", got, tc.toast)
			}
		})
	}
}

// A vault keeping notes under inbox/ reads a leading slash as Inbox-relative.
func TestDeadLinkInAnInboxVaultStaysInTheInbox(t *testing.T) {
	root := newSessionParityVault(t)
	for rel, body := range map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"inbox"}`,
		"inbox/Work/Index.md":  "[r](./New)\n[s](/Top/Other)\n[q](../../quick/Fast)\n",
	} {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := newSessionParityApp(t, root)
	if a.primaryAtRoot() {
		t.Fatal("this vault keeps notes at the root")
	}
	for needle, want := range map[string]string{
		"[r]": "inbox/Work/New.md",
		"[s]": "inbox/Top/Other.md",
		"[q]": "quick/Fast.md",
	} {
		gdOn(t, a, "inbox/Work/Index.md", needle)
		if a.overlay == nil {
			t.Fatalf("%s: no confirm, toast %q", needle, lastToast(a))
		}
		a.overlay.handleKey(a, enterKey())
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Fatalf("%s: %v", needle, err)
		}
	}
}

func TestDeadWikilinkOffersTheNoteWithoutItsAnchor(t *testing.T) {
	a, root := linkTestApp(t, map[string]string{"Index.md": "[[Fresh#Heading]]\n"})
	gdOn(t, a, "Index.md", "[[")
	dialog, ok := a.overlay.(*confirmDialog)
	if !ok {
		t.Fatal("no confirm")
	}
	if dialog.message != `Create note "Fresh"?` {
		t.Fatalf("message %q", dialog.message)
	}
	a.overlay.handleKey(a, enterKey())
	if _, err := os.Stat(filepath.Join(root, "Fresh.md")); err != nil {
		t.Fatal(err)
	}
}

// Reading mode only draws what the desktop's renderer treats as a link, but
// follows those the same way as the editor.
func TestPreviewLinksMatchTheRenderer(t *testing.T) {
	a, _ := linkTestApp(t, nil)
	a.openNote("note.md", true)
	lines := a.paragraph("[a](<./My Note>) [b](google.com) [c](not a link) [[Doc#Heading|d]]", 200, nil, "", "", 0)
	got := []linkTarget{}
	for _, l := range lines {
		got = append(got, l.links...)
	}
	want := []linkTarget{
		{kind: "path", target: "./My Note", alias: "a"},
		{kind: "url", target: "https://google.com", alias: "b"},
		{kind: "wikilink", target: "Doc#Heading", alias: "d"},
	}
	if len(got) != len(want) {
		t.Fatalf("links %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("link %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}
