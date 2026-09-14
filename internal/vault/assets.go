package vault

// Asset operations follow the sibling ZenNotes server's on-disk/API contract.
import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const internalVaultDir = ".zennotes"
const maxImportedAssetBytes int64 = 64 << 20

var ErrAssetTooLarge = errors.New("asset exceeds 64 MiB")

type ImportedAsset struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
	Kind     string `json:"kind"`
}
type DeletedAsset struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	UndoToken string `json:"undoToken"`
	DeletedAt string `json:"deletedAt"`
}

func (v *Vault) ImportAsset(notePath, filename string, body io.Reader) (ImportedAsset, error) {
	_ = notePath
	v.mu.Lock()
	defer v.mu.Unlock()
	assetsAbs, err := SafeJoin(v.root, AssetsDir)
	if err != nil {
		return ImportedAsset{}, err
	}
	if err := os.MkdirAll(assetsAbs, v.dirMode); err != nil {
		return ImportedAsset{}, err
	}
	safeName := sanitizeFileName(filename)
	if safeName == "" {
		safeName = "file"
	}
	ext := filepath.Ext(safeName)
	stem := strings.TrimSuffix(safeName, ext)
	abs, err := uniquePath(assetsAbs, stem, ext)
	if err != nil {
		return ImportedAsset{}, err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, v.fileMode)
	if err != nil {
		return ImportedAsset{}, err
	}
	cleanupPartial := func() {
		_ = f.Close()
		_ = os.Remove(abs)
	}
	limited := io.LimitReader(body, maxImportedAssetBytes+1)
	written, err := io.Copy(f, limited)
	if err != nil {
		cleanupPartial()
		return ImportedAsset{}, err
	}
	if written > maxImportedAssetBytes {
		cleanupPartial()
		return ImportedAsset{}, ErrAssetTooLarge
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(abs)
		return ImportedAsset{}, err
	}
	relFromRoot, err := filepath.Rel(v.root, abs)
	if err != nil {
		return ImportedAsset{}, err
	}
	rel := filepath.ToSlash(relFromRoot)
	kind := kindForExt(strings.ToLower(filepath.Ext(abs)))
	markdown := makeAssetMarkdown(rel, kind, filepath.Base(abs))
	return ImportedAsset{
		Name:     filepath.Base(abs),
		Path:     rel,
		Markdown: markdown,
		Kind:     kind,
	}, nil
}

func (v *Vault) RenameAsset(rel, nextName string) (AssetMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	srcAbs, err := v.assertAssetFile(rel)
	if err != nil {
		return AssetMeta{}, err
	}
	cleanName, err := cleanAssetFilename(nextName)
	if err != nil {
		return AssetMeta{}, err
	}
	destAbs := filepath.Join(filepath.Dir(srcAbs), cleanName)
	if destAbs != srcAbs {
		if dstInfo, statErr := os.Stat(destAbs); statErr == nil {
			// Something is already at the destination. Allow it only when it is
			// literally the same file (case-only rename on a case-insensitive
			// filesystem), routing through a temp name; otherwise it collides.
			srcInfo, srcErr := os.Stat(srcAbs)
			if srcErr != nil {
				return AssetMeta{}, srcErr
			}
			if !os.SameFile(dstInfo, srcInfo) {
				return AssetMeta{}, fmt.Errorf("an asset named %q already exists in this folder", cleanName)
			}
			tmp := srcAbs + ".zenrename.tmp"
			if err := os.Rename(srcAbs, tmp); err != nil {
				return AssetMeta{}, err
			}
			if err := os.Rename(tmp, destAbs); err != nil {
				return AssetMeta{}, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return AssetMeta{}, statErr
		} else if err := os.Rename(srcAbs, destAbs); err != nil {
			return AssetMeta{}, err
		}
	}
	return v.assetMetaForAbs(destAbs)
}

// MoveAsset moves an asset file into targetDir (vault-relative; empty means the
// unified assets/ folder), mirroring the desktop moveAsset. The filename is made
// unique in the destination. (#379)
func (v *Vault) assertAssetFile(rel string) (string, error) {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
	if trimmed == "" {
		return "", errors.New("asset path is required")
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == internalVaultDir {
			return "", errors.New("cannot modify internal ZenNotes files")
		}
	}
	if strings.EqualFold(filepath.Ext(trimmed), ".md") {
		return "", errors.New("use note actions to modify markdown notes")
	}
	abs, err := SafeJoin(v.root, trimmed)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("asset path is not a file")
	}
	return abs, nil
}

// cleanAssetTargetDir resolves a vault-relative destination directory for a
// move. Empty resolves to the unified assets/ folder. Assumes caller holds v.mu.
func (v *Vault) assetMetaForAbs(abs string) (AssetMeta, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return AssetMeta{}, err
	}
	rel, err := filepath.Rel(v.root, abs)
	if err != nil {
		return AssetMeta{}, err
	}
	name := filepath.Base(abs)
	return AssetMeta{
		Path:      filepath.ToSlash(rel),
		Name:      name,
		Size:      info.Size(),
		UpdatedAt: info.ModTime().UnixMilli(),
	}, nil
}

func cleanAssetFilename(name string) (string, error) {
	raw := strings.TrimSpace(name)
	if strings.ContainsAny(raw, "/\\") {
		return "", errors.New("use only a file name")
	}
	trimmed := filepath.Base(raw)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return "", errors.New("asset name is required")
	}
	if strings.EqualFold(filepath.Ext(trimmed), ".md") {
		return "", errors.New("use note actions for markdown notes")
	}
	return trimmed, nil
}

// makeAssetMarkdown mirrors the desktop markdownForImportedAsset: everything is
// linked by VAULT-relative path, an image as a wikilink and anything else as a
// markdown link, which is the single form every client now writes. The link
// used to be relative to the note, so it broke as soon as the note moved to
// another depth (nothing rewrites relative asset paths on move).
func makeAssetMarkdown(vaultRelPath, kind, name string) string {
	if kind == "image" {
		return "![[" + vaultRelPath + "]]"
	}
	dest := "<" + strings.ReplaceAll(vaultRelPath, ">", "%3E") + ">"
	return "[" + name + "](" + dest + ")"
}

// --- Misc helpers ---

var forbiddenFilenameChars = []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}

func sanitizeFileName(name string) string {
	leaf := filepath.Base(name)
	safe := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune("\\/:%*?\"<>|[]#^", r) {
			return '-'
		}
		return r
	}, leaf)
	safe = strings.Join(strings.Fields(safe), " ")
	if safe == "." || safe == ".." {
		return ""
	}
	return safe
}

func uniquePath(dir, stem, ext string) (string, error) {
	candidate := filepath.Join(dir, stem+ext)
	for i := 2; ; i++ {
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s %d%s", stem, i, ext))
	}
}

func kindForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif", ".bmp":
		return "image"
	case ".pdf":
		return "pdf"
	case ".mp3", ".wav", ".m4a", ".ogg":
		return "audio"
	case ".mp4", ".mov", ".webm":
		return "video"
	}
	return "file"
}
