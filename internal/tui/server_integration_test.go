package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/templates"
	"github.com/ZenNotes/tui/internal/vault"
)

// Opt in only against a disposable server vault; this exercises real writes.
func TestDisposableServerParity(t *testing.T) {
	url := os.Getenv("ZENNOTES_TEST_SERVER_URL")
	if url == "" || os.Getenv("ZENNOTES_TEST_DISPOSABLE_VAULT") != "1" {
		t.Skip("requires a disposable ZenNotes server")
	}
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	token := os.Getenv("ZENNOTES_TEST_SERVER_TOKEN")
	target := backend.Target{Kind: backend.KindRemote, BaseURL: url, AuthToken: token}
	b, err := backend.New(target, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := remote.NewClient(url, token).WatchChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	title := fmt.Sprintf("Parity smoke %d", time.Now().UnixNano())
	body := "# Smoke\n\nHello 世界 🌱\n"
	n, err := b.CreateNote(ctx, vault.FolderInbox, title, "", &body)
	if err != nil {
		t.Fatal(err)
	}
	waitForNote := func() {
		t.Helper()
		for {
			select {
			case e, ok := <-events:
				if !ok || e.Err != nil {
					t.Fatalf("watch closed: %v", e.Err)
				}
				if e.Path == n.Path {
					return
				}
			case <-ctx.Done():
				t.Fatal("no real server change event")
			}
		}
	}
	waitForNote()
	a := newApp(ctx, Options{Backend: b, Target: target}, config.DefaultPrefs(), true)
	a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
	a.width, a.height, a.ready = 120, 40, true
	a.layout()
	a.openNote(n.Path, true)
	buf := a.activeBuffer()
	updated := body + "External change\n"
	if _, err = b.WriteNote(ctx, n.Path, updated); err != nil {
		t.Fatal(err)
	}
	waitForNote()
	a.applyBufferRead(a.readBufferCmd(buf)().(bufferReadMsg))
	if buf.ed.Text() != updated {
		t.Fatal("clean remote buffer did not refresh")
	}
	buf.ed.InsertAtCursor("Local edit")
	local := buf.ed.Text()
	external := updated + "Another external change\n"
	if _, err = b.WriteNote(ctx, n.Path, external); err != nil {
		t.Fatal(err)
	}
	a.applyBufferRead(a.readBufferCmd(buf)().(bufferReadMsg))
	if !buf.diskChanged || buf.ed.Text() != local {
		t.Fatal("dirty remote edits lost")
	}
	if err = a.saveBuffer(buf); err == nil {
		t.Fatal("conflict overwritten")
	}
	persisted, err := b.ReadNote(ctx, n.Path)
	if err != nil || persisted.Body != external {
		t.Fatal("remote conflict content changed", err)
	}

	thread, err := backend.AddComment(ctx, b, backend.AddCommentInput{Path: n.Path, Body: "Comment", AnchorText: "世界 🌱"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err = backend.ReplyToComment(ctx, b, backend.ReplyInput{Path: n.Path, ID: thread.ID, Body: "Reply"})
	if err != nil || len(thread.Replies) != 1 {
		t.Fatal("remote reply", err)
	}
	if _, err = backend.ResolveComment(ctx, b, n.Path, thread.ID, true); err != nil {
		t.Fatal(err)
	}

	assets := b.(backend.AssetManager)
	as, err := assets.ImportAsset(ctx, n.Path, "smoke.txt", strings.NewReader("attachment bytes"))
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := assets.RenameAsset(ctx, as.Path, "renamed smoke.txt")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := assets.DeleteAsset(ctx, renamed.Path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := assets.ListDeletedAssets(ctx)
	if err != nil || len(list) == 0 {
		t.Fatal("remote file trash", err)
	}
	restored, err := assets.RestoreDeletedAsset(ctx, deleted)
	if err != nil {
		t.Fatal(err)
	}
	data, err := b.ReadAsset(ctx, restored.Path)
	if err != nil || string(data) != "attachment bytes" {
		t.Fatal("remote asset roundtrip", err)
	}

	custom := templates.Template{Name: title, Body: "# {{title}}\n{{cursor}}", TargetFolder: vault.FolderInbox, TargetSubpath: "projects", TitleTemplate: "{{date:yyyy-MM-dd}}"}
	file, err := b.WriteTemplate(ctx, vault.WriteTemplateInput{Slug: vault.SafeTemplateSlug(title), Raw: templateRaw(custom)})
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.ListTemplates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.SourcePath == file.SourcePath {
			got := templates.ParseCustom(f)
			found = got.TargetSubpath == "projects" && got.Body == custom.Body
		}
	}
	if !found {
		t.Fatal("remote template metadata/body roundtrip")
	}
	if err = b.DeleteTemplate(ctx, file.SourcePath); err != nil {
		t.Fatal(err)
	}
}
