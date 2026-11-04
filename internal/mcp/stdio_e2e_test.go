package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The newer tools driven the way a client reaches them: the built `zn mcp`
// binary over stdio with the SDK's client, on a local vault and on a real
// ZenNotes server, under every kind of scope, checking the files on disk. It
// builds the binary, so it runs only when asked:
//
//	ZN_MCP_E2E=1 go test ./internal/mcp -run TestStdio
//	ZENNOTES_SERVER_CONTRACT_BINARY=/path/to/zennotes-server ZN_MCP_E2E=1 go test ./internal/mcp -run TestStdio
const e2eToken = "e2e-token"

func buildZN(t *testing.T) string {
	t.Helper()
	if os.Getenv("ZN_MCP_E2E") == "" {
		t.Skip("set ZN_MCP_E2E=1 to build zn and drive it over stdio")
	}
	bin := filepath.Join(t.TempDir(), "zn")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/ZenNotes/tui/cmd/zn")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build zn: %v\n%s", err, out)
	}
	return bin
}

// e2eVault is a root-mode vault the cases below act on.
func e2eVault(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root","dailyNotes":{"enabled":true,"directory":"Journal/Days"}}`,
		"Projects/Plan.md":     "# Plan\n\nIntro\n\n## Actions\n\n- [ ] ship it\n- [ ] write it !low\n\n```\n- [ ] in code\n```\n\n### Detail\n\nfine print\n\n## Files\n\n![[pic.png]] and [the report](<../assets/report.pdf>)\n",
		"Journal/Diary.md":     "# Diary\n\nsecret ![[assets/pic.png]]\n\n- [ ] call mum\n",
		"Journal/private.png":  "\x89PNG private",
		"assets/pic.png":       "\x89PNG pic",
		"assets/report.pdf":    "%PDF-1.7 report",
		"assets/words.txt":     "plain words\n",
		"assets/secret/no.png": "\x89PNG no",
	}
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type e2eBackend struct {
	name string
	root string
	args []string
}

func e2eBackends(t *testing.T) []func(t *testing.T) e2eBackend {
	out := []func(t *testing.T) e2eBackend{func(t *testing.T) e2eBackend {
		root := filepath.Join(t.TempDir(), "vault")
		e2eVault(t, root)
		return e2eBackend{name: "local", root: root, args: []string{"--vault", root}}
	}}
	if server := os.Getenv("ZENNOTES_SERVER_CONTRACT_BINARY"); server != "" {
		out = append(out, func(t *testing.T) e2eBackend {
			base := t.TempDir()
			root := filepath.Join(base, "vault")
			e2eVault(t, root)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			log, err := os.Create(filepath.Join(base, "server.log"))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(server)
			cmd.Dir = base
			cmd.Env = append(os.Environ(), "ZENNOTES_BIND="+address, "ZENNOTES_DEFAULT_VAULT_PATH="+root, "ZENNOTES_CONFIG_PATH="+filepath.Join(base, "server.json"), "ZENNOTES_BROWSE_ROOTS="+root, "ZENNOTES_AUTH_TOKEN="+e2eToken)
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); log.Close() })
			for deadline := time.Now().Add(10 * time.Second); ; {
				conn, err := net.Dial("tcp", address)
				if err == nil {
					conn.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server did not start")
				}
				time.Sleep(50 * time.Millisecond)
			}
			return e2eBackend{name: "server", root: root, args: []string{"--server", "http://" + address, "--token", e2eToken}}
		})
	}
	return out
}

func connectStdio(t *testing.T, bin string, b e2eBackend, flags ...string) *sdk.ClientSession {
	t.Helper()
	config := t.TempDir()
	cmd := exec.Command(bin, append(append([]string{"mcp"}, b.args...), flags...)...)
	cmd.Env = append(os.Environ(), "ZENNOTES_CONFIG_DIR="+config)
	cmd.Stderr = os.Stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "claude-code", Version: "0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	cs, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close(); cancel() })
	return cs
}

func e2eText(res *sdk.CallToolResult) string {
	var out strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			out.WriteString(tc.Text)
		}
	}
	return out.String()
}

func e2eOK(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("%s %v failed: %s", tool, args, e2eText(res))
	}
	return res
}

func e2eField(t *testing.T, res *sdk.CallToolResult, key string) string {
	t.Helper()
	object := map[string]any{}
	if err := json.Unmarshal([]byte(e2eText(res)), &object); err != nil {
		t.Fatalf("%v in %s", err, e2eText(res))
	}
	return fmt.Sprint(object[key])
}

func e2eFails(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any, want string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	got := ""
	switch {
	case err != nil:
		got = err.Error()
	case !res.IsError:
		t.Fatalf("%s %v should fail, got %s", tool, args, e2eText(res))
	default:
		got = e2eText(res)
	}
	if !strings.Contains(got, want) {
		t.Fatalf("%s %v:\n got %q\nwant %q", tool, args, got, want)
	}
}

func e2eDisk(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(data)
}

func e2eAbsent(t *testing.T, root, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
		t.Fatalf("%s should not exist", rel)
	}
}

// Every asset tool with full access, each checked against the disk.
func TestStdioAssetToolsAtFullAccess(t *testing.T) {
	bin := buildZN(t)
	for _, start := range e2eBackends(t) {
		b := start(t)
		t.Run(b.name, func(t *testing.T) {
			cs := connectStdio(t, bin, b)

			// upload: the bytes land, the name stays unique, the markdown embeds.
			png := []byte("\x89PNG\r\n\x1a\nbinary\x00bytes")
			up := e2eOK(t, cs, "upload_asset", map[string]any{"filename": "plot.png", "content_base64": base64.StdEncoding.EncodeToString(png)})
			if e2eField(t, up, "path") != "assets/plot.png" || e2eField(t, up, "markdown") != "![[assets/plot.png]]" || e2eField(t, up, "kind") != "image" {
				t.Fatalf("upload: %s", e2eText(up))
			}
			if !bytes.Equal([]byte(e2eDisk(t, b.root, "assets/plot.png")), png) {
				t.Fatal("upload changed the bytes")
			}
			if got := e2eField(t, e2eOK(t, cs, "upload_asset", map[string]any{"filename": "plot.png", "content_base64": "data:image/png;base64,cG5n"}), "path"); got != "assets/plot 2.png" {
				t.Fatalf("a second upload must not overwrite: %s", got)
			}

			// upload is bounded before it decodes, and the limit itself still works
			// through a real pipe.
			atLimit := base64.StdEncoding.EncodeToString(make([]byte, maxUploadAssetBytes))
			if got := e2eField(t, e2eOK(t, cs, "upload_asset", map[string]any{"filename": "big.bin", "content_base64": atLimit}), "path"); got != "assets/big.bin" {
				t.Fatalf("at the limit: %s", got)
			}
			overLimit := base64.StdEncoding.EncodeToString(make([]byte, maxUploadAssetBytes+1))
			e2eFails(t, cs, "upload_asset", map[string]any{"filename": "huge.bin", "content_base64": overLimit}, "upload_asset takes files up to 16 MiB")
			e2eAbsent(t, b.root, "assets/huge.bin")

			// read: an image is an image, text is text, a PDF is an embedded file.
			read := e2eOK(t, cs, "read_asset", map[string]any{"path": "assets/plot.png"})
			img, ok := read.Content[1].(*sdk.ImageContent)
			if !ok || img.MIMEType != "image/png" || !bytes.Equal(img.Data, png) {
				t.Fatalf("read_asset image: %#v", read.Content[1])
			}
			if text := e2eText(e2eOK(t, cs, "read_asset", map[string]any{"path": "assets/words.txt"})); !strings.Contains(text, "plain words") {
				t.Fatalf("read_asset text: %s", text)
			}
			pdf := e2eOK(t, cs, "read_asset", map[string]any{"path": "assets/report.pdf"})
			blob, ok := pdf.Content[1].(*sdk.EmbeddedResource)
			if !ok || blob.Resource.MIMEType != "application/pdf" || string(blob.Resource.Blob) != "%PDF-1.7 report" {
				t.Fatalf("read_asset pdf: %#v", pdf.Content[1])
			}
			e2eFails(t, cs, "read_asset", map[string]any{"path": "assets/big.bin"}, "read_asset returns files up to 5 MiB")

			// nothing but an asset can be named.
			for _, path := range []string{"../../../../etc/passwd", ".zennotes/vault.json", "Projects/Plan.md", "assets/../Journal/Diary.md", "/etc/passwd"} {
				e2eFails(t, cs, "read_asset", map[string]any{"path": path}, "Asset not found: "+path)
				e2eFails(t, cs, "delete_asset", map[string]any{"path": path}, "Asset not found: "+path)
			}
			e2eFails(t, cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": "../../outside"}, "cannot put files there")
			e2eFails(t, cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": ".zennotes"}, "cannot put files there")
			e2eFails(t, cs, "rename_asset", map[string]any{"path": "assets/pic.png", "name": "../../evil.png"}, "Use only a file name")
			e2eFails(t, cs, "rename_asset", map[string]any{"path": "assets/pic.png", "name": "note.md"}, "Notes are not attachments")

			// rename and move keep every note's links pointing at the file.
			if got := e2eField(t, e2eOK(t, cs, "rename_asset", map[string]any{"path": "assets/pic.png", "name": "photo.png"}), "path"); got != "assets/photo.png" {
				t.Fatalf("rename: %s", got)
			}
			if plan := e2eDisk(t, b.root, "Projects/Plan.md"); !strings.Contains(plan, "![[photo.png]]") {
				t.Fatalf("rename left a bare embed behind:\n%s", plan)
			}
			if diary := e2eDisk(t, b.root, "Journal/Diary.md"); !strings.Contains(diary, "![[assets/photo.png]]") {
				t.Fatalf("rename left a rooted embed behind:\n%s", diary)
			}
			if got := e2eField(t, e2eOK(t, cs, "move_asset", map[string]any{"path": "assets/report.pdf", "targetDir": "Projects/Files"}), "path"); got != "Projects/Files/report.pdf" {
				t.Fatalf("move: %s", got)
			}
			if plan := e2eDisk(t, b.root, "Projects/Plan.md"); !strings.Contains(plan, "(<Files/report.pdf>)") {
				t.Fatalf("move left a relative link behind:\n%s", plan)
			}
			e2eAbsent(t, b.root, "assets/report.pdf")

			// duplicate, then delete and restore through the token and the listing.
			if got := e2eField(t, e2eOK(t, cs, "duplicate_asset", map[string]any{"path": "assets/words.txt"}), "path"); got != "assets/words copy.txt" {
				t.Fatalf("duplicate: %s", got)
			}
			deleted := e2eOK(t, cs, "delete_asset", map[string]any{"path": "assets/words copy.txt"})
			token := e2eField(t, deleted, "undoToken")
			e2eAbsent(t, b.root, "assets/words copy.txt")
			if listed := e2eText(e2eOK(t, cs, "list_assets", map[string]any{"deleted": true})); !strings.Contains(listed, token) {
				t.Fatalf("list_assets deleted lacks the token: %s", listed)
			}
			e2eFails(t, cs, "restore_asset", map[string]any{"undoToken": "not-a-token"}, "Deleted asset not found: not-a-token")
			if got := e2eField(t, e2eOK(t, cs, "restore_asset", map[string]any{"undoToken": token}), "path"); got != "assets/words copy.txt" {
				t.Fatalf("restore: %s", got)
			}
			if e2eDisk(t, b.root, "assets/words copy.txt") != "plain words\n" {
				t.Fatal("restore changed the bytes")
			}
		})
	}
}

// update_task, note_sections, edit_section and daily_note with full access.
func TestStdioWorkToolsAtFullAccess(t *testing.T) {
	bin := buildZN(t)
	for _, start := range e2eBackends(t) {
		b := start(t)
		t.Run(b.name, func(t *testing.T) {
			cs := connectStdio(t, bin, b)

			task := e2eOK(t, cs, "update_task", map[string]any{"id": "Projects/Plan.md#0", "due": "2026-12-01", "priority": "high", "status": "doing"})
			if e2eField(t, task, "due") != "2026-12-01" || e2eField(t, task, "priority") != "high" || e2eField(t, task, "status") != "doing" {
				t.Fatalf("update_task: %s", e2eText(task))
			}
			e2eOK(t, cs, "update_task", map[string]any{"id": "Projects/Plan.md#1", "priority": "", "state": "done", "waiting": true})
			plan := e2eDisk(t, b.root, "Projects/Plan.md")
			if !strings.Contains(plan, "- [ ] ship it due:2026-12-01 !high @status:doing\n- [x] write it @waiting\n") || !strings.Contains(plan, "```\n- [ ] in code\n```") {
				t.Fatalf("update_task edited the wrong bytes:\n%s", plan)
			}
			if text := e2eText(e2eOK(t, cs, "update_task", map[string]any{"id": "Projects/Plan.md#9", "due": "2026-12-01"})); text != "null" {
				t.Fatalf("a drifted id answers null, got %s", text)
			}

			outline := e2eText(e2eOK(t, cs, "note_sections", map[string]any{"path": "Projects/Plan.md"}))
			for _, want := range []string{`"Actions"`, `"Detail"`, `"lineNumber"`, `"endLineNumber"`} {
				if !strings.Contains(outline, want) {
					t.Fatalf("outline lacks %s: %s", want, outline)
				}
			}
			detail := e2eOK(t, cs, "note_sections", map[string]any{"path": "Projects/Plan.md", "heading": "detail"})
			if e2eField(t, detail, "body") != "fine print" {
				t.Fatalf("section: %s", e2eText(detail))
			}

			e2eOK(t, cs, "edit_section", map[string]any{"path": "Projects/Plan.md", "heading": "Actions", "mode": "append", "text": "- [ ] added"})
			e2eOK(t, cs, "edit_section", map[string]any{"path": "Projects/Plan.md", "heading": "Detail", "mode": "replace", "text": "new print"})
			plan = e2eDisk(t, b.root, "Projects/Plan.md")
			if !strings.Contains(plan, "```\n\n- [ ] added\n\n### Detail\n\nnew print\n\n## Files") {
				t.Fatalf("edit_section:\n%s", plan)
			}
			e2eFails(t, cs, "edit_section", map[string]any{"path": "Projects/Plan.md", "heading": "Nope", "mode": "append", "text": "x"}, "Headings: Plan | Actions | Detail | Files.")

			daily := e2eOK(t, cs, "daily_note", map[string]any{"date": "2026-02-03"})
			if e2eField(t, daily, "exists") != "false" {
				t.Fatalf("daily_note: %s", e2eText(daily))
			}
			var parsed struct {
				CreateWith map[string]any `json:"createWith"`
			}
			if err := json.Unmarshal([]byte(e2eText(daily)), &parsed); err != nil {
				t.Fatal(err)
			}
			created := e2eOK(t, cs, "create_note", parsed.CreateWith)
			if got := e2eField(t, created, "path"); got != "Journal/Days/2026-02-03.md" {
				t.Fatalf("create_note from createWith: %s", got)
			}
			again := e2eOK(t, cs, "daily_note", map[string]any{"date": "2026-02-03"})
			if e2eField(t, again, "exists") != "true" || e2eField(t, again, "path") != "Journal/Days/2026-02-03.md" {
				t.Fatalf("daily_note after creating: %s", e2eText(again))
			}
		})
	}
}

// A file that gains a space keeps its plain Markdown links working. A server
// rewrites links itself, so this is the local vault's guarantee; its own copy of
// the rewrite gets the same fix separately.
func TestStdioASpacedNameKeepsPlainLinksWorking(t *testing.T) {
	bin := buildZN(t)
	root := filepath.Join(t.TempDir(), "vault")
	e2eVault(t, root)
	note := filepath.Join(root, "Projects", "Links.md")
	if err := os.WriteFile(note, []byte("[w](../assets/words.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connectStdio(t, bin, e2eBackend{name: "local", root: root, args: []string{"--vault", root}})
	e2eOK(t, cs, "rename_asset", map[string]any{"path": "assets/words.txt", "name": "new words.txt"})
	if got := e2eDisk(t, root, "Projects/Links.md"); got != "[w](<../assets/new words.txt>)\n" {
		t.Fatalf("%q", got)
	}
	e2eOK(t, cs, "move_asset", map[string]any{"path": "assets/new words.txt", "targetDir": "Projects"})
	if got := e2eDisk(t, root, "Projects/Links.md"); got != "[w](<new words.txt>)\n" {
		t.Fatalf("%q", got)
	}
}

// A denied call under each scope, with the disk unchanged afterwards.
func TestStdioEveryScopeFailsClosed(t *testing.T) {
	bin := buildZN(t)
	writes := []struct {
		tool string
		args map[string]any
	}{
		{"upload_asset", map[string]any{"filename": "x.png", "content_base64": "cG5n"}},
		{"rename_asset", map[string]any{"path": "assets/words.txt", "name": "y.txt"}},
		{"move_asset", map[string]any{"path": "assets/words.txt", "targetDir": "Projects"}},
		{"duplicate_asset", map[string]any{"path": "assets/words.txt"}},
		{"delete_asset", map[string]any{"path": "assets/words.txt"}},
		{"restore_asset", map[string]any{"undoToken": "11111111-1111-4111-8111-111111111111"}},
		{"update_task", map[string]any{"id": "Projects/Plan.md#0", "due": "2026-12-01"}},
		{"edit_section", map[string]any{"path": "Projects/Plan.md", "heading": "Actions", "mode": "append", "text": "x"}},
	}
	for _, start := range e2eBackends(t) {
		b := start(t)
		t.Run(b.name, func(t *testing.T) {
			plan := e2eDisk(t, b.root, "Projects/Plan.md")
			for _, flags := range [][]string{{"--read-only"}, {"--access", "read"}, {"--access", "comment"}} {
				cs := connectStdio(t, bin, b, flags...)
				tools, err := cs.ListTools(context.Background(), nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, write := range writes {
					for _, tool := range tools.Tools {
						if tool.Name == write.tool {
							t.Fatalf("%v lists %s", flags, write.tool)
						}
					}
					e2eFails(t, cs, write.tool, write.args, write.tool)
				}
				// Reading still works at these levels.
				if text := e2eText(e2eOK(t, cs, "read_asset", map[string]any{"path": "assets/words.txt"})); !strings.Contains(text, "plain words") {
					t.Fatalf("%v read_asset: %s", flags, text)
				}
				e2eOK(t, cs, "note_sections", map[string]any{"path": "Projects/Plan.md"})
				e2eOK(t, cs, "daily_note", nil)
			}
			if e2eDisk(t, b.root, "Projects/Plan.md") != plan || e2eDisk(t, b.root, "assets/words.txt") != "plain words\n" {
				t.Fatal("a read-only scope changed the vault")
			}
			e2eAbsent(t, b.root, "assets/x.png")

			// A path scope hides, in the same bytes as missing, on every new tool.
			cs := connectStdio(t, bin, b, "--allow-path", "assets", "--allow-path", "Projects", "--deny-path", "assets/secret")
			if listed := e2eText(e2eOK(t, cs, "list_assets", nil)); strings.Contains(listed, "secret") || strings.Contains(listed, "private") {
				t.Fatalf("list_assets shows hidden files: %s", listed)
			}
			for _, hidden := range []string{"assets/secret/no.png", "Journal/private.png"} {
				for _, tool := range []string{"read_asset", "delete_asset", "duplicate_asset"} {
					e2eFails(t, cs, tool, map[string]any{"path": hidden}, "Asset not found: "+hidden)
				}
				e2eFails(t, cs, "rename_asset", map[string]any{"path": hidden, "name": "a.png"}, "Asset not found: "+hidden)
				e2eFails(t, cs, "move_asset", map[string]any{"path": hidden, "targetDir": "assets"}, "Asset not found: "+hidden)
			}
			e2eFails(t, cs, "move_asset", map[string]any{"path": "assets/words.txt", "targetDir": "Journal"}, "This assistant can put files only inside: assets, Projects.")
			e2eFails(t, cs, "note_sections", map[string]any{"path": "Journal/Diary.md"}, "Note not found: Journal/Diary.md")
			e2eFails(t, cs, "edit_section", map[string]any{"path": "Journal/Diary.md", "heading": "Diary", "mode": "append", "text": "x"}, "Note not found: Journal/Diary.md")
			e2eFails(t, cs, "update_task", map[string]any{"id": "Journal/Diary.md#0", "due": "2026-12-01"}, "Note not found: Journal/Diary.md")

			// A path scope on a server gets no file contents at all.
			if b.name == "server" {
				for _, tool := range []string{"read_asset", "duplicate_asset"} {
					e2eFails(t, cs, tool, map[string]any{"path": "assets/words.txt"}, "not available to an assistant that sees only part of the vault")
				}
				e2eAbsent(t, b.root, "assets/words copy.txt")
			} else {
				e2eOK(t, cs, "read_asset", map[string]any{"path": "assets/words.txt"})
			}

			// A rename under a path scope touches only what the client sees, and a
			// server, which rewrites every note itself, refuses it.
			rename := map[string]any{"path": "assets/pic.png", "name": "photo.png"}
			if b.name == "local" {
				e2eOK(t, cs, "rename_asset", rename)
				if plan := e2eDisk(t, b.root, "Projects/Plan.md"); !strings.Contains(plan, "![[photo.png]]") {
					t.Fatalf("a visible note keeps its link:\n%s", plan)
				}
				if diary := e2eDisk(t, b.root, "Journal/Diary.md"); !strings.Contains(diary, "![[assets/pic.png]]") {
					t.Fatalf("a hidden note was rewritten:\n%s", diary)
				}
			} else {
				e2eFails(t, cs, "rename_asset", rename, "not available to an assistant that sees only part of the vault")
				e2eFails(t, cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": "Projects"}, "not available to an assistant that sees only part of the vault")
				e2eAbsent(t, b.root, "assets/photo.png")
			}
			// Upload, duplicate, delete and restore still work inside the scope.
			if got := e2eField(t, e2eOK(t, cs, "upload_asset", map[string]any{"filename": "in.png", "content_base64": "cG5n"}), "path"); got != "assets/in.png" {
				t.Fatalf("upload inside the scope: %s", got)
			}
			token := e2eField(t, e2eOK(t, cs, "delete_asset", map[string]any{"path": "assets/in.png"}), "undoToken")
			e2eOK(t, cs, "restore_asset", map[string]any{"undoToken": token})

			// Without the assets folder shared, there is nowhere to upload to.
			narrow := connectStdio(t, bin, b, "--allow-path", "Projects")
			e2eFails(t, narrow, "upload_asset", map[string]any{"filename": "n.png", "content_base64": "cG5n"}, "the vault's assets folder is not shared")
			e2eFails(t, narrow, "restore_asset", map[string]any{"undoToken": token}, "Deleted asset not found")
			e2eAbsent(t, b.root, "assets/n.png")
		})
	}
}
