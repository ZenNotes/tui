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
	DeleteAsset(context.Context, string) (vault.DeletedAsset, error)
	ListDeletedAssets(context.Context) ([]vault.DeletedAsset, error)
	RestoreDeletedAsset(context.Context, vault.DeletedAsset) (vault.AssetMeta, error)
}

func (l *Local) ImportAsset(_ context.Context, note, name string, r io.Reader) (vault.ImportedAsset, error) {
	return l.vault.ImportAsset(note, name, r)
}
func (l *Local) RenameAsset(_ context.Context, p, n string) (vault.AssetMeta, error) {
	return l.vault.RenameAsset(p, n)
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
func (r *Remote) DeleteAsset(ctx context.Context, p string) (vault.DeletedAsset, error) {
	var out vault.DeletedAsset
	err := r.client.Post(ctx, "/api/assets/delete", map[string]string{"path": p}, &out)
	return out, err
}
func (r *Remote) ListDeletedAssets(ctx context.Context) ([]vault.DeletedAsset, error) {
	var out []vault.DeletedAsset
	err := r.client.Get(ctx, "/api/assets/deleted", &out)
	return out, err
}
func (r *Remote) RestoreDeletedAsset(ctx context.Context, d vault.DeletedAsset) (vault.AssetMeta, error) {
	var out vault.AssetMeta
	err := r.client.Post(ctx, "/api/assets/restore", d, &out)
	return out, err
}
