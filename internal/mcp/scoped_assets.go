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
			if v.visible(a.Path) {
				sb.assets = append(sb.assets, a)
			}
		}
	})
	return sb.assets, sb.assetsErr
}

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
	meta, ok := matchAsset(all, raw)
	if !ok {
		return vault.AssetMeta{}, nil, assetNotFound(raw)
	}
	return meta, v, nil
}

// assetRefusal is the one answer for every folder a client may not put a file
// in, hidden or not.
func (v *scopeView) assetRefusal() error {
	if len(v.allow) > 0 {
		return fmt.Errorf("This assistant can put files only inside: %s.", strings.Join(v.allow, ", "))
	}
	return errors.New("This assistant cannot put files there.")
}

// realDirs says whether every segment of rel that exists on disk is a real
// directory. The listings skip symbolic links, so a folder they do not know may
// be a link into a hidden one, and a write through it would land there. A
// server's vault is left to the server's own walker, as for notes.
func (v *scopeView) realDirs(rel string) bool {
	if v.root == "" || rel == "" || rel == "." {
		return true
	}
	walk := v.root
	for _, seg := range strings.Split(rel, "/") {
		walk = filepath.Join(walk, seg)
		info, err := os.Lstat(walk)
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// assetDestination resolves a folder a file may be moved to, in the vault's
// spelling, or refuses.
func (sb *scopedBackend) assetDestination(ctx context.Context, v *scopeView, raw string) (string, error) {
	clean, ok := cleanRequest(raw)
	if !ok {
		return "", v.assetRefusal()
	}
	known := append(append([]string{}, v.dirs...), vault.AttachmentDirs()...)
	if all, err := sb.visibleAssets(ctx); err == nil {
		for _, a := range all {
			for dir := path.Dir(a.Path); dir != "." && dir != "/"; dir = path.Dir(dir) {
				known = append(known, dir)
			}
		}
	}
	spelled := ""
	for _, seg := range strings.Split(clean, "/") {
		next := joinRel(spelled, seg)
		if s, ok := spell(next, known); ok {
			next = s
		}
		spelled = next
	}
	if !v.visible(spelled) || !v.realDirs(spelled) {
		return "", v.assetRefusal()
	}
	return spelled, nil
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
	if !v.visible(vault.AssetsDir) || !v.realDirs(vault.AssetsDir) {
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
	return r.MoveAssetRewritingLinksIn(ctx, meta.Path, dir, v.visible)
}

// errAssetLinksEverywhere is the refusal for a server: it rewrites links in
// every note, including ones the client cannot see, as with rename_note.
var errAssetLinksEverywhere = errors.New("Renaming or moving attachments on a ZenNotes server is not available to an assistant that sees only part of the vault: the server rewrites links in every note, including ones this assistant cannot see.")

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
	return m.DuplicateAsset(ctx, meta.Path)
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
// store itself is internal; only its entries' original paths decide.
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
		if _, ok := cleanRequest(d.Path); ok && v.visible(d.Path) {
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
			if !v.realDirs(path.Dir(entry.Path)) {
				return vault.AssetMeta{}, deletedAssetNotFound(d.UndoToken)
			}
			return m.RestoreDeletedAsset(ctx, entry)
		}
	}
	return vault.AssetMeta{}, deletedAssetNotFound(d.UndoToken)
}
