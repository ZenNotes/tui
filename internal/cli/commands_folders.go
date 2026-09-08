package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

var topFolders = []vault.NoteFolder{vault.FolderInbox, vault.FolderQuick, vault.FolderArchive}

type folderRef struct {
	folder  vault.NoteFolder
	subpath string
}

// splitFolderPath reads `folder/sub` references, the CLI's canonical form.
func splitFolderPath(spec string) (folderRef, error) {
	trimmed := strings.Trim(spec, "/")
	if trimmed == "" {
		return folderRef{}, errors.New("Folder path must not be empty.")
	}
	parts := strings.Split(trimmed, "/")
	head := parts[0]
	for _, f := range topFolders {
		if string(f) == head {
			return folderRef{folder: f, subpath: strings.Join(parts[1:], "/")}, nil
		}
	}
	return folderRef{}, fmt.Errorf("Folder path must start with one of inbox, quick, archive (got %q).", head)
}

func folderTarget(args Args) string {
	if p := args.Positional(0); p != "" {
		return p
	}
	return args.Str("path")
}

func cmdFolderList(ctx context.Context, b backend.Backend, args Args) error {
	folders, err := b.ListFolders(ctx)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(folders)
		return nil
	}
	if len(folders) == 0 {
		emitLine("No subfolders.")
		return nil
	}
	for _, f := range folders {
		emitLine(fmt.Sprintf("%s/%s", f.Folder, f.Subpath))
	}
	return nil
}

func cmdFolderCreate(ctx context.Context, b backend.Backend, args Args) error {
	target := folderTarget(args)
	if target == "" {
		return errors.New("zn folder create requires a folder path like inbox/Work.")
	}
	ref, err := splitFolderPath(target)
	if err != nil {
		return err
	}
	if ref.subpath == "" {
		return errors.New("Cannot create the top-level folder; pick a subpath.")
	}
	if err := b.CreateFolder(ctx, ref.folder, ref.subpath); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "folder": ref.folder, "subpath": ref.subpath})
		return nil
	}
	emitOK(fmt.Sprintf("Created %s/%s", ref.folder, ref.subpath))
	return nil
}

func cmdFolderRename(ctx context.Context, b backend.Backend, args Args) error {
	oldPath := folderTarget(args)
	to := args.Str("to")
	if oldPath == "" {
		return errors.New("zn folder rename requires a folder path.")
	}
	if to == "" {
		return errors.New("zn folder rename requires --to <newPath>.")
	}
	oldRef, err := splitFolderPath(oldPath)
	if err != nil {
		return err
	}
	newRef, err := splitFolderPath(to)
	if err != nil {
		return err
	}
	if oldRef.folder != newRef.folder {
		return errors.New("Renaming across top-level folders is not supported. Use `zn move` for individual notes.")
	}
	if oldRef.subpath == "" || newRef.subpath == "" {
		return errors.New("Both old and new folder paths must include a subpath.")
	}
	next, err := b.RenameFolder(ctx, oldRef.folder, oldRef.subpath, newRef.subpath)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "folder": oldRef.folder, "subpath": next})
		return nil
	}
	emitOK(fmt.Sprintf("Renamed to %s/%s", oldRef.folder, next))
	return nil
}

func cmdFolderDelete(ctx context.Context, b backend.Backend, args Args) error {
	target := folderTarget(args)
	if target == "" {
		return errors.New("zn folder delete requires a folder path.")
	}
	if !args.Bool("yes") {
		return errors.New("zn folder delete is destructive. Re-run with --yes to confirm.")
	}
	ref, err := splitFolderPath(target)
	if err != nil {
		return err
	}
	if ref.subpath == "" {
		return errors.New("Cannot delete a top-level folder.")
	}
	if err := b.DeleteFolder(ctx, ref.folder, ref.subpath); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "folder": ref.folder, "subpath": ref.subpath})
		return nil
	}
	emitOK(fmt.Sprintf("Deleted %s/%s", ref.folder, ref.subpath))
	return nil
}
