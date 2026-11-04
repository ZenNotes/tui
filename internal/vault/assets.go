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
	dir := filepath.Dir(srcAbs)
	destAbs := filepath.Join(dir, cleanName)
	if destAbs != srcAbs {
		// Lstat, because a link at the destination is something there too, even
		// when what it names is missing.
		dstInfo, statErr := os.Lstat(destAbs)
		switch {
		case statErr == nil:
			// Allowed only when it is literally this file under another spelling
			// (a case-only rename on a case-insensitive volume). A hard link is the
			// same file too, but it has an entry of its own to lose.
			srcInfo, srcErr := os.Lstat(srcAbs)
			if srcErr != nil {
				return AssetMeta{}, srcErr
			}
			entries, err := dirEntryNames(dir)
			if err != nil {
				return AssetMeta{}, err
			}
			if !os.SameFile(dstInfo, srcInfo) || (entries[filepath.Base(srcAbs)] && entries[cleanName]) {
				return AssetMeta{}, fmt.Errorf("an asset named %q already exists in this folder", cleanName)
			}
			tmp, err := uniquePath(dir, filepath.Base(srcAbs)+".zenrename", ".tmp")
			if err != nil {
				return AssetMeta{}, err
			}
			if err := os.Rename(srcAbs, tmp); err != nil {
				return AssetMeta{}, err
			}
			if err := os.Rename(tmp, destAbs); err != nil {
				_ = os.Rename(tmp, srcAbs)
				return AssetMeta{}, err
			}
		case !errors.Is(statErr, os.ErrNotExist):
			return AssetMeta{}, statErr
		default:
			if err := os.Rename(srcAbs, destAbs); err != nil {
				return AssetMeta{}, err
			}
		}
	}
	return v.assetMetaForAbs(destAbs)
}

// RenameAssetRewritingLinksIn renames an asset and re-targets the notes that
// reference it, the way the desktop and the server do (#785), in the notes
// rewrite accepts (nil: every note). The plain RenameAsset leaves links to the
// caller, because `zn tui` rewrites them through its open buffers.
func (v *Vault) RenameAssetRewritingLinksIn(rel, nextName string, rewrite func(path string) bool) (AssetMeta, error) {
	// Snapshot first so references still resolve to the asset under its old name.
	assetsBefore, notesBefore, err := v.assetRewriteSnapshot()
	if err != nil {
		return AssetMeta{}, err
	}
	oldRel := vaultRelOf(v, rel)
	meta, err := v.RenameAsset(rel, nextName)
	if err != nil {
		return AssetMeta{}, err
	}
	if meta.Path != oldRel {
		meta.LinksNotUpdated = v.rewriteAssetReferences(notesBefore, assetsBefore, oldRel, meta.Path, rewrite)
	}
	return meta, nil
}

// MoveAsset moves an asset file into targetDir (vault-relative; empty means the
// unified assets/ folder), mirroring the desktop moveAsset. The filename is made
// unique in the destination. (#379)
func (v *Vault) MoveAsset(rel, targetDir string) (AssetMeta, error) {
	return v.moveAsset(rel, targetDir, nil)
}

// ErrAssetDestinationRefused is a move whose final path the caller turned down.
var ErrAssetDestinationRefused = errors.New("that destination is refused")

// moveAsset refuses before anything changes when accept turns down the final
// path, which only this point knows, since a taken name gets a number.
func (v *Vault) moveAsset(rel, targetDir string, accept func(path string) bool) (AssetMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	srcAbs, err := v.assertAssetFile(rel)
	if err != nil {
		return AssetMeta{}, err
	}
	destDir, err := v.cleanAssetTargetDir(targetDir)
	if err != nil {
		return AssetMeta{}, err
	}
	// Compared by identity: ASSETS on a case-insensitive volume, or a link to the
	// folder, is where the file already is, and would otherwise look like a taken
	// name and get a number.
	if sameDirectory(destDir, filepath.Dir(srcAbs)) {
		return v.assetMetaForAbs(srcAbs)
	}
	name := filepath.Base(srcAbs)
	ext := filepath.Ext(name)
	destAbs, err := uniquePath(destDir, strings.TrimSuffix(name, ext), ext)
	if err != nil {
		return AssetMeta{}, err
	}
	if accept != nil && !accept(v.relPosix(destAbs)) {
		return AssetMeta{}, ErrAssetDestinationRefused
	}
	if err := os.MkdirAll(destDir, v.dirMode); err != nil {
		return AssetMeta{}, err
	}
	if err := os.Rename(srcAbs, destAbs); err != nil {
		return AssetMeta{}, err
	}
	return v.assetMetaForAbs(destAbs)
}

// MoveAssetRewritingLinksIn is MoveAsset plus the link rewrite, in the notes
// rewrite accepts (nil: every note). A filter also has to accept where the file
// lands, because it is what the caller is allowed to see.
func (v *Vault) MoveAssetRewritingLinksIn(rel, targetDir string, rewrite func(path string) bool) (AssetMeta, error) {
	assetsBefore, notesBefore, err := v.assetRewriteSnapshot()
	if err != nil {
		return AssetMeta{}, err
	}
	oldRel := vaultRelOf(v, rel)
	meta, err := v.moveAsset(rel, targetDir, rewrite)
	if err != nil {
		return AssetMeta{}, err
	}
	if meta.Path != oldRel {
		meta.LinksNotUpdated = v.rewriteAssetReferences(notesBefore, assetsBefore, oldRel, meta.Path, rewrite)
	}
	return meta, nil
}

func (v *Vault) assetRewriteSnapshot() ([]AssetMeta, []NoteMeta, error) {
	assets, err := v.ListAssets()
	if err != nil {
		return nil, nil, err
	}
	notes, err := v.ListNotes()
	if err != nil {
		return nil, nil, err
	}
	return assets, notes, nil
}

func dirEntryNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names, nil
}

func sameDirectory(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	infoA, err := os.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

// vaultRelOf spells rel the way the listings do, for comparing against them.
func vaultRelOf(v *Vault, rel string) string {
	abs, err := v.assertAssetFile(rel)
	if err != nil {
		return ""
	}
	return v.relPosix(abs)
}

// DuplicateAsset copies an asset next to itself as "<name> copy<ext>",
// mirroring the desktop duplicateAsset.
func (v *Vault) DuplicateAsset(rel string) (AssetMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	srcAbs, err := v.assertAssetFile(rel)
	if err != nil {
		return AssetMeta{}, err
	}
	ext := filepath.Ext(srcAbs)
	stem := strings.TrimSuffix(filepath.Base(srcAbs), ext)
	destAbs, err := uniquePath(filepath.Dir(srcAbs), stem+" copy", ext)
	if err != nil {
		return AssetMeta{}, err
	}
	in, err := os.Open(srcAbs)
	if err != nil {
		return AssetMeta{}, err
	}
	defer in.Close()
	out, err := os.OpenFile(destAbs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, v.fileMode)
	if err != nil {
		return AssetMeta{}, err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(destAbs)
		return AssetMeta{}, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(destAbs)
		return AssetMeta{}, err
	}
	return v.assetMetaForAbs(destAbs)
}

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
func (v *Vault) cleanAssetTargetDir(targetDir string) (string, error) {
	normalized := strings.Trim(strings.TrimSpace(filepath.ToSlash(targetDir)), "/")
	if normalized == "" {
		return SafeJoin(v.root, AssetsDir)
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == internalVaultDir {
			return "", errors.New("cannot move assets into internal ZenNotes files")
		}
	}
	return SafeJoin(v.root, normalized)
}

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
