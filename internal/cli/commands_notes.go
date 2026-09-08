package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

var validFolders = []vault.NoteFolder{vault.FolderInbox, vault.FolderQuick, vault.FolderArchive, vault.FolderTrash}

func parseFolderFlag(value string, present bool) (vault.NoteFolder, bool, error) {
	if !present {
		return "", false, nil
	}
	for _, f := range validFolders {
		if string(f) == value {
			return f, true, nil
		}
	}
	return "", false, fmt.Errorf("--folder must be one of inbox, quick, archive, trash (got %q)", value)
}

func requirePath(args Args) (string, error) {
	if v, ok := args.String("path"); ok && v != "" {
		return v, nil
	}
	if p := args.Positional(0); p != "" {
		return p, nil
	}
	return "", errors.New("A note path is required.")
}

func sortByUpdated(notes []vault.NoteMeta) {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].UpdatedAt > notes[j].UpdatedAt })
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.ToLower(t) == tag {
			return true
		}
	}
	return false
}

func cmdList(ctx context.Context, b backend.Backend, args Args) error {
	folder, hasFolder, err := parseFolderFlag(args.String("folder"))
	if err != nil {
		return err
	}
	tag := strings.ToLower(strings.TrimPrefix(args.Str("tag"), "#"))
	limit := args.IntOr("limit", 50)
	all, err := b.ListNotes(ctx)
	if err != nil {
		return err
	}
	notes := []vault.NoteMeta{}
	for _, n := range all {
		if hasFolder {
			if n.Folder != folder {
				continue
			}
		} else if n.Folder == vault.FolderTrash {
			continue
		}
		if tag != "" && !hasTag(n.Tags, tag) {
			continue
		}
		notes = append(notes, n)
	}
	sortByUpdated(notes)
	if limit >= 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	if args.Bool("json") {
		emitJSON(notes)
		return nil
	}
	if len(notes) == 0 {
		emitLine("No notes found.")
		return nil
	}
	widest := 0
	for _, n := range notes {
		widest = max(widest, len(n.Folder))
	}
	for _, n := range notes {
		emitLine(fmt.Sprintf("%s  %s  %s", pad(formatRelativeAge(n.UpdatedAt), 10), pad(string(n.Folder), widest), n.Path))
	}
	return nil
}

func cmdRead(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	note, err := b.ReadNote(ctx, rel)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(note)
		return nil
	}
	if args.Bool("meta") {
		emitJSON(note.NoteMeta)
		return nil
	}
	if args.Bool("pretty") || args.Str("style") != "" {
		out, err := renderPretty(note.Body, args.Str("style"))
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, out)
		return nil
	}
	fmt.Fprint(stdout, note.Body)
	if !strings.HasSuffix(note.Body, "\n") {
		fmt.Fprint(stdout, "\n")
	}
	return nil
}

func stripTagPrefix(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, strings.TrimPrefix(t, "#"))
	}
	return out
}

func composeCreateBody(title string, body *string, tags []string) *string {
	if body == nil && len(tags) == 0 {
		return nil
	}
	heading := ""
	if title != "" {
		heading = "# " + title + "\n\n"
	}
	tagLine := ""
	if len(tags) > 0 {
		parts := make([]string, len(tags))
		for i, t := range tags {
			parts[i] = "#" + t
		}
		tagLine = strings.Join(parts, " ") + "\n\n"
	}
	bodyText := ""
	if body != nil {
		bodyText = *body
	}
	out := heading + tagLine + bodyText
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return &out
}

func cmdCreate(ctx context.Context, b backend.Backend, args Args) error {
	folder, hasFolder, err := parseFolderFlag(args.String("folder"))
	if err != nil {
		return err
	}
	if !hasFolder {
		folder = vault.FolderInbox
	}
	if folder == vault.FolderTrash {
		return errors.New("Refusing to create a note directly in trash.")
	}
	title := args.Str("title")
	if title == "" {
		title = args.Positional(0)
	}
	subpath := args.Str("subpath")
	tags := stripTagPrefix(args.Many("tag"))
	input := args.ResolveBody("")
	composed := composeCreateBody(title, input, tags)
	meta, err := b.CreateNote(ctx, folder, title, subpath, composed)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(meta)
		return nil
	}
	emitOK("Created " + meta.Path)
	if meta.Excerpt != "" {
		emitLine("  " + truncate(meta.Excerpt, 80))
	}
	return nil
}

func emitWritten(meta vault.NoteMeta, args Args, verb string) {
	if args.Bool("json") {
		emitJSON(meta)
		return
	}
	emitOK(verb + " " + meta.Path)
}

func cmdWrite(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	body := args.ResolveBody("")
	if body == nil {
		return errors.New("zn write requires --body, a positional body, or piped stdin.")
	}
	meta, err := b.WriteNote(ctx, rel, *body)
	if err != nil {
		return err
	}
	emitWritten(meta, args, "Updated")
	return nil
}

func cmdAppend(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	body := args.ResolveBody("")
	if body == nil || strings.TrimSpace(*body) == "" {
		return errors.New("zn append requires --body, a positional body, or piped stdin.")
	}
	meta, err := b.AppendToNote(ctx, rel, *body)
	if err != nil {
		return err
	}
	emitWritten(meta, args, "Updated")
	return nil
}

func cmdPrepend(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	body := args.ResolveBody("")
	if body == nil || strings.TrimSpace(*body) == "" {
		return errors.New("zn prepend requires --body, a positional body, or piped stdin.")
	}
	meta, err := b.PrependToNote(ctx, rel, *body)
	if err != nil {
		return err
	}
	emitWritten(meta, args, "Updated")
	return nil
}

func cmdRename(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	to := args.Str("to")
	if to == "" {
		return errors.New("zn rename requires --to <new title>.")
	}
	meta, err := b.RenameNote(ctx, rel, to)
	if err != nil {
		return err
	}
	emitWritten(meta, args, "Renamed")
	return nil
}

func cmdMove(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	folder, hasFolder, err := parseFolderFlag(args.String("folder"))
	if err != nil {
		return err
	}
	if !hasFolder {
		return errors.New("zn move requires --folder <inbox|quick|archive|trash>.")
	}
	meta, err := b.MoveNote(ctx, rel, folder, args.Str("subpath"))
	if err != nil {
		return err
	}
	emitWritten(meta, args, "Moved")
	return nil
}

func simpleNoteCommand(op func(context.Context, string) (vault.NoteMeta, error), verb string) Handler {
	return func(ctx context.Context, b backend.Backend, args Args) error {
		rel, err := requirePath(args)
		if err != nil {
			return err
		}
		meta, err := op(ctx, rel)
		if err != nil {
			return err
		}
		emitWritten(meta, args, verb)
		return nil
	}
}

func cmdArchive(ctx context.Context, b backend.Backend, args Args) error {
	return simpleNoteCommand(b.ArchiveNote, "Archived")(ctx, b, args)
}

func cmdUnarchive(ctx context.Context, b backend.Backend, args Args) error {
	return simpleNoteCommand(b.UnarchiveNote, "Unarchived")(ctx, b, args)
}

func cmdTrash(ctx context.Context, b backend.Backend, args Args) error {
	return simpleNoteCommand(b.MoveToTrash, "Moved to trash")(ctx, b, args)
}

func cmdRestore(ctx context.Context, b backend.Backend, args Args) error {
	return simpleNoteCommand(b.RestoreFromTrash, "Restored")(ctx, b, args)
}

func cmdDuplicate(ctx context.Context, b backend.Backend, args Args) error {
	return simpleNoteCommand(b.DuplicateNote, "Duplicated")(ctx, b, args)
}

func cmdDelete(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := requirePath(args)
	if err != nil {
		return err
	}
	if !args.Bool("yes") {
		return errors.New("zn delete is permanent. Re-run with --yes to confirm, or use `zn trash` for the reversible alternative.")
	}
	if err := b.DeleteNote(ctx, rel); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": rel})
		return nil
	}
	emitOK("Deleted " + rel)
	return nil
}
