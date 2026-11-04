package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/vault"
)

var _ backend.AssetManager = (*scopedBackend)(nil)

// The asset half of scopedBackend. An asset is visible when its path is, and a
// destination must be, so it holds no second idea of what a scope means. As
// with notes, a hidden file answers exactly like a missing one.

func (sb *scopedBackend) assetsInner() (backend.AssetManager, error) {
	m, ok := sb.inner.(backend.AssetManager)
	if !ok {
		return nil, errAssetsUnsupported
	}
	return m, nil
}

// visibleAssets is the asset listing this call sees, read once.
func (sb *scopedBackend) visibleAssets(ctx context.Context) ([]vault.AssetMeta, error) {
	sb.assetsOnce.Do(func() {
		v, err := sb.load(ctx)
		if err != nil {
			sb.assetsErr = err
			return
		}
		all, err := sb.inner.ListAssets(ctx)
		if err != nil {
			sb.assetsErr = err
			return
		}
		sb.assets = []vault.AssetMeta{}
		for _, a := range all {
			// A server lists a link to a file, whose contents are whatever it
			// names. A local listing never holds one. As for notes, a client with
			// no paths in its scope sees what an unscoped one does.
			if a.Symlink && sb.scope.Restricted() {
				continue
			}
			if v.visible(a.Path) {
				sb.assets = append(sb.assets, a)
			}
		}
	})
	return sb.assets, sb.assetsErr
}

// trimsDifferently says whether the asset routes would use another path than
// this one: they trim surrounding whitespace, on a vault here and on a server, so
// " Journal" would pass the scope check and reach "Journal".
func trimsDifferently(raw string) bool { return strings.TrimSpace(raw) != raw }

// assetFor resolves a client's path to a visible listed asset, the first step
// of every per-asset operation.
func (sb *scopedBackend) assetFor(ctx context.Context, raw string, a Access) (vault.AssetMeta, *scopeView, error) {
	if err := sb.need(a); err != nil {
		return vault.AssetMeta{}, nil, err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return vault.AssetMeta{}, nil, err
	}
	all, err := sb.visibleAssets(ctx)
	if err != nil {
		return vault.AssetMeta{}, nil, err
	}
	if trimsDifferently(raw) {
		return vault.AssetMeta{}, nil, assetNotFound(raw)
	}
	meta, ok := matchAsset(all, raw)
	if !ok {
		return vault.AssetMeta{}, nil, assetNotFound(raw)
	}
	if a == AccessWrite {
		if err := sb.refuseDatabaseFile(ctx, meta); err != nil {
			return vault.AssetMeta{}, nil, err
		}
	}
	return meta, v, nil
}

// refuseDatabaseFile asks about the whole listing. A CSV whose schema file is
// hidden from this client is still half of a database, and changing it alone
// would split the pair.
func (sb *scopedBackend) refuseDatabaseFile(ctx context.Context, meta vault.AssetMeta) error {
	if !database.IsLooseCSVPath(meta.Path) {
		return refuseDatabaseFile(meta, nil)
	}
	all, err := sb.inner.ListAssets(ctx)
	if err != nil {
		return err
	}
	return refuseDatabaseFile(meta, all)
}

// assetRefusal is the one answer for every folder a client may not put a file
// in, hidden or not.
func (v *scopeView) assetRefusal() error {
	if len(v.allow) > 0 {
		return fmt.Errorf("This assistant can put files only inside: %s.", strings.Join(v.allow, ", "))
	}
	return errors.New("This assistant cannot put files there.")
}

// knownAssetDirs are the folders the listings prove are real, plus the
// attachment folders, which listings hide. Whether one of those is itself a link
// cannot be told on a server, since a read answers the same for a real folder,
// so the server refuses that write itself.
func (sb *scopedBackend) knownAssetDirs(ctx context.Context, v *scopeView) []string {
	known := append(append([]string{}, v.dirs...), vault.AttachmentDirs()...)
	if all, err := sb.visibleAssets(ctx); err == nil {
		for _, a := range all {
			for dir := path.Dir(a.Path); dir != "." && dir != "/"; dir = path.Dir(dir) {
				known = append(known, dir)
			}
		}
	}
	return known
}

// realDirs says whether every segment of rel that exists is a real directory
// under the name its parent lists, so a write lands in the folder that was checked.
func (sb *scopedBackend) realDirs(ctx context.Context, v *scopeView, rel string) bool {
	// With no paths to keep a file inside, this has nothing to protect, and a server
	// refuses a write through a link itself. It cannot tell a subfolder its last file left
	// from a link, so it would refuse a move back into one.
	if rel == "" || rel == "." || !sb.scope.Restricted() {
		return true
	}
	if v.root == "" {
		known := sb.knownAssetDirs(ctx, v)
		walked := ""
		for _, seg := range strings.Split(rel, "/") {
			walked = joinRel(walked, seg)
			if _, ok := spell(walked, known); !ok {
				return !v.occupied(walked)
			}
		}
		return true
	}
	walk := v.root
	for _, seg := range strings.Split(rel, "/") {
		parent := walk
		walk = filepath.Join(walk, seg)
		info, err := os.Lstat(walk)
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		if err != nil || !info.IsDir() {
			return false
		}
		if _, listed := folderNameListed(parent, seg); !listed {
			return false
		}
	}
	return true
}

// assetDestination resolves a folder a file may be moved to, in the vault's
// spelling, or refuses.
func (sb *scopedBackend) assetDestination(ctx context.Context, v *scopeView, raw string) (string, error) {
	clean, ok := cleanRequest(raw)
	if !ok || trimsDifferently(raw) {
		return "", v.assetRefusal()
	}
	known := sb.knownAssetDirs(ctx, v)
	spelled := ""
	for _, seg := range strings.Split(clean, "/") {
		next := joinRel(spelled, seg)
		if s, ok := spell(next, known); ok {
			next = s
		}
		spelled = next
	}
	if !v.visible(spelled) || !sb.realDirs(ctx, v, spelled) {
		return "", v.assetRefusal()
	}
	if v.root != "" {
		listed, ok := foldersAsListed(v.root, spelled)
		if !ok || !v.visible(listed) {
			return "", v.assetRefusal()
		}
		spelled = listed
	}
	return spelled, nil
}

// destinationRefusal is assetRefusal for a caller without the view.
func (sb *scopedBackend) destinationRefusal(ctx context.Context) error {
	v, err := sb.load(ctx)
	if err != nil {
		return err
	}
	return v.assetRefusal()
}

// errAssetBytesOnAServer is the refusal for handing a file's contents to a
// client that sees only part of a server's vault. It holds until a server can
// say enough about a file for this check to be made here.
var errAssetBytesOnAServer = errors.New("Reading or copying attachments on a ZenNotes server is not available to an assistant that sees only part of the vault.")

func (sb *scopedBackend) refuseAssetBytes() error {
	if sb.scope.Restricted() && sb.inner.Kind() == backend.KindRemote {
		return errAssetBytesOnAServer
	}
	return nil
}

func (sb *scopedBackend) ReadAsset(ctx context.Context, rel string) ([]byte, error) {
	meta, _, err := sb.assetFor(ctx, rel, AccessRead)
	if err != nil {
		return nil, err
	}
	if err := sb.refuseAssetBytes(); err != nil {
		return nil, err
	}
	return sb.inner.ReadAsset(ctx, meta.Path)
}

// ImportAsset only ever writes to the vault's assets folder, so that folder has
// to be shared. The name comes back from the backend, and a name that lands on
// a deny entry is undone rather than left where the client cannot see it.
func (sb *scopedBackend) ImportAsset(ctx context.Context, note, name string, r io.Reader) (vault.ImportedAsset, error) {
	if err := sb.need(AccessWrite); err != nil {
		return vault.ImportedAsset{}, err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	m, err := sb.assetsInner()
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	refused := errors.New("This assistant cannot add files: the vault's assets folder is not shared with it.")
	if !v.visible(vault.AssetsDir) || !sb.realDirs(ctx, v, vault.AssetsDir) {
		return vault.ImportedAsset{}, refused
	}
	got, err := m.ImportAsset(ctx, note, name, r)
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	if !v.visible(got.Path) {
		_, _ = m.DeleteAsset(ctx, got.Path)
		return vault.ImportedAsset{}, refused
	}
	return got, nil
}

func (sb *scopedBackend) RenameAsset(ctx context.Context, rel, name string) (vault.AssetMeta, error) {
	meta, v, err := sb.assetFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	if !v.visible(path.Join(path.Dir(meta.Path), name)) {
		return vault.AssetMeta{}, errors.New("This assistant cannot give the file that name.")
	}
	if !sb.scope.Restricted() {
		return assetRename(ctx, sb.inner, meta.Path, name)
	}
	r, ok := sb.inner.(backend.AssetLinkRewriter)
	if !ok {
		return vault.AssetMeta{}, errAssetLinksEverywhere
	}
	return r.RenameAssetRewritingLinksIn(ctx, meta.Path, name, v.visible)
}

func (sb *scopedBackend) MoveAsset(ctx context.Context, rel, targetDir string) (vault.AssetMeta, error) {
	meta, v, err := sb.assetFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	dir, err := sb.assetDestination(ctx, v, targetDir)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	if !sb.scope.Restricted() {
		return assetMove(ctx, sb.inner, meta.Path, dir)
	}
	r, ok := sb.inner.(backend.AssetLinkRewriter)
	if !ok {
		return vault.AssetMeta{}, errAssetLinksEverywhere
	}
	// The filter also decides where the file may land: the folder being visible
	// says nothing about the final name, which only the move itself knows.
	moved, err := r.MoveAssetRewritingLinksIn(ctx, meta.Path, dir, v.visible)
	if errors.Is(err, vault.ErrAssetDestinationRefused) {
		return vault.AssetMeta{}, v.assetRefusal()
	}
	return moved, err
}

// errAssetLinksEverywhere is the refusal for a server: it rewrites links in
// every note, including ones the client cannot see, as with rename_note.
var errAssetLinksEverywhere = errors.New("Renaming or moving attachments on a ZenNotes server is not available to an assistant that sees only part of the vault: the server rewrites links in every note, including ones this assistant cannot see.")

// sharesPath says whether this client sees a path at all, for a tool about to
// name one it did not get from a listing. If that cannot be told, it does not.
// The vault's root is shared unless an allow list narrows it.
func (sb *scopedBackend) sharesPath(ctx context.Context, rel string) bool {
	v, err := sb.load(ctx)
	if err != nil {
		return false
	}
	if rel == "." || rel == "" {
		return len(v.allow) == 0
	}
	return v.visible(rel)
}

func (sb *scopedBackend) DuplicateAsset(ctx context.Context, rel string) (vault.AssetMeta, error) {
	meta, v, err := sb.assetFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	if err := sb.refuseAssetBytes(); err != nil {
		return vault.AssetMeta{}, err
	}
	m, err := sb.assetsInner()
	if err != nil {
		return vault.AssetMeta{}, err
	}
	ext := path.Ext(meta.Name)
	if !v.visible(path.Join(path.Dir(meta.Path), strings.TrimSuffix(meta.Name, ext)+" copy"+ext)) {
		return vault.AssetMeta{}, errors.New("This assistant cannot give the file that name.")
	}
	copied, err := m.DuplicateAsset(ctx, meta.Path)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	// A taken name gets a number, and only the copy can say which. One that landed
	// on a deny entry is undone rather than left where the client cannot see it.
	if !v.visible(copied.Path) {
		_, _ = m.DeleteAsset(ctx, copied.Path)
		return vault.AssetMeta{}, errors.New("This assistant cannot give the file that name.")
	}
	return copied, nil
}

func (sb *scopedBackend) DeleteAsset(ctx context.Context, rel string) (vault.DeletedAsset, error) {
	meta, _, err := sb.assetFor(ctx, rel, AccessWrite)
	if err != nil {
		return vault.DeletedAsset{}, err
	}
	m, err := sb.assetsInner()
	if err != nil {
		return vault.DeletedAsset{}, err
	}
	return m.DeleteAsset(ctx, meta.Path)
}

// ListDeletedAssets shows what was deleted from where the client can see. The
// store itself is internal; only its entries' original paths decide, read the way
// any other path is, since a record can say anything.
func (sb *scopedBackend) ListDeletedAssets(ctx context.Context) ([]vault.DeletedAsset, error) {
	v, err := sb.load(ctx)
	if err != nil {
		return nil, err
	}
	m, err := sb.assetsInner()
	if err != nil {
		return nil, err
	}
	all, err := m.ListDeletedAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := []vault.DeletedAsset{}
	for _, d := range all {
		if clean, ok := cleanRequest(d.Path); ok && !trimsDifferently(d.Path) && v.visible(clean) {
			out = append(out, d)
		}
	}
	return out, nil
}

// RestoreDeletedAsset takes only an entry this client can list, and the folder
// it returns to must still be a real one.
func (sb *scopedBackend) RestoreDeletedAsset(ctx context.Context, d vault.DeletedAsset) (vault.AssetMeta, error) {
	if err := sb.need(AccessWrite); err != nil {
		return vault.AssetMeta{}, err
	}
	v, err := sb.load(ctx)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	m, err := sb.assetsInner()
	if err != nil {
		return vault.AssetMeta{}, err
	}
	visible, err := sb.ListDeletedAssets(ctx)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	for _, entry := range visible {
		if entry.UndoToken == d.UndoToken {
			clean, _ := cleanRequest(entry.Path)
			if !sb.realDirs(ctx, v, path.Dir(clean)) {
				return vault.AssetMeta{}, deletedAssetNotFound(d.UndoToken)
			}
			restored, err := m.RestoreDeletedAsset(ctx, entry)
			if err != nil {
				return vault.AssetMeta{}, err
			}
			// As with a copy: a number that landed on a deny entry is undone.
			if !v.visible(restored.Path) {
				_, _ = m.DeleteAsset(ctx, restored.Path)
				return vault.AssetMeta{}, deletedAssetNotFound(d.UndoToken)
			}
			return restored, nil
		}
	}
	return vault.AssetMeta{}, deletedAssetNotFound(d.UndoToken)
}
