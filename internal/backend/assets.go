package backend

import (
	"context"
	"github.com/ZenNotes/tui/internal/vault"
	"io"
)

// AssetManager is optional so older backend adapters remain usable for reading.
type AssetManager interface {
	ImportAsset(context.Context, string, string, io.Reader) (vault.ImportedAsset, error)
	RenameAsset(context.Context, string, string) (vault.AssetMeta, error)
	MoveAsset(context.Context, string, string) (vault.AssetMeta, error)
	DuplicateAsset(context.Context, string) (vault.AssetMeta, error)
	DeleteAsset(context.Context, string) (vault.DeletedAsset, error)
	ListDeletedAssets(context.Context) ([]vault.DeletedAsset, error)
	RestoreDeletedAsset(context.Context, vault.DeletedAsset) (vault.AssetMeta, error)
}

// AssetLinkRewriter is a backend whose asset renames and moves can re-target
// the notes that reference the file, limited to the notes rewrite accepts (nil:
// every note). A server rewrites every note itself, so it is not one.
type AssetLinkRewriter interface {
	RenameAssetRewritingLinksIn(ctx context.Context, rel, name string, rewrite func(path string) bool) (vault.AssetMeta, error)
	MoveAssetRewritingLinksIn(ctx context.Context, rel, targetDir string, rewrite func(path string) bool) (vault.AssetMeta, error)
}

func (l *Local) ImportAsset(_ context.Context, note, name string, r io.Reader) (vault.ImportedAsset, error) {
	return l.vault.ImportAsset(note, name, r)
}
func (l *Local) RenameAsset(_ context.Context, p, n string) (vault.AssetMeta, error) {
	return l.vault.RenameAsset(p, n)
}
func (l *Local) MoveAsset(_ context.Context, p, dir string) (vault.AssetMeta, error) {
	return l.vault.MoveAsset(p, dir)
}
func (l *Local) DuplicateAsset(_ context.Context, p string) (vault.AssetMeta, error) {
	return l.vault.DuplicateAsset(p)
}
func (l *Local) RenameAssetRewritingLinksIn(_ context.Context, p, n string, rewrite func(path string) bool) (vault.AssetMeta, error) {
	return l.vault.RenameAssetRewritingLinksIn(p, n, rewrite)
}
func (l *Local) MoveAssetRewritingLinksIn(_ context.Context, p, dir string, rewrite func(path string) bool) (vault.AssetMeta, error) {
	return l.vault.MoveAssetRewritingLinksIn(p, dir, rewrite)
}
func (l *Local) DeleteAsset(_ context.Context, p string) (vault.DeletedAsset, error) {
	return l.vault.DeleteAsset(p)
}
func (l *Local) ListDeletedAssets(_ context.Context) ([]vault.DeletedAsset, error) {
	return l.vault.ListDeletedAssets()
}
func (l *Local) RestoreDeletedAsset(_ context.Context, d vault.DeletedAsset) (vault.AssetMeta, error) {
	return l.vault.RestoreDeletedAsset(d)
}
func (r *Remote) ImportAsset(ctx context.Context, note, name string, body io.Reader) (vault.ImportedAsset, error) {
	return r.client.ImportAsset(ctx, note, name, body)
}
func (r *Remote) RenameAsset(ctx context.Context, p, n string) (vault.AssetMeta, error) {
	var out vault.AssetMeta
	err := r.client.Post(ctx, "/api/assets/rename", map[string]string{"path": p, "name": n}, &out)
	return out, err
}
func (r *Remote) MoveAsset(ctx context.Context, p, dir string) (vault.AssetMeta, error) {
	var out vault.AssetMeta
	err := r.client.Post(ctx, "/api/assets/move", map[string]string{"path": p, "targetDir": dir}, &out)
	return out, err
}
func (r *Remote) DuplicateAsset(ctx context.Context, p string) (vault.AssetMeta, error) {
	var out vault.AssetMeta
	err := r.client.Post(ctx, "/api/assets/duplicate", map[string]string{"path": p}, &out)
	return out, err
}
func (r *Remote) DeleteAsset(ctx context.Context, p string) (vault.DeletedAsset, error) {
	var out vault.DeletedAsset
	err := r.client.Post(ctx, "/api/assets/delete", map[string]string{"path": p}, &out)
	return out, err
}
func (r *Remote) ListDeletedAssets(ctx context.Context) ([]vault.DeletedAsset, error) {
	var out []vault.DeletedAsset
	err := r.client.Get(ctx, "/api/assets/deleted", &out)
	if out == nil {
		out = []vault.DeletedAsset{}
	}
	return out, err
}
func (r *Remote) RestoreDeletedAsset(ctx context.Context, d vault.DeletedAsset) (vault.AssetMeta, error) {
	var out vault.AssetMeta
	err := r.client.Post(ctx, "/api/assets/restore", d, &out)
	return out, err
}
