package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/search"
	"github.com/ZenNotes/tui/internal/vault"
)

// Attachments (ZenNotes/tui#34). Every tool finds its file in the vault's asset
// listing, whatever the scope: the listing walks real files only and leaves out
// notes, hidden and internal files, database folders and symlinks, so nothing
// else can be named through these tools. Deleting is the soft delete the app's
// Trash can undo. Purging is left out on purpose: it cannot be undone, an
// assistant never needs it to finish a task, and the user can do it in the app.

const (
	// maxUploadAssetBytes bounds upload_asset. A tool call carries the file as
	// base64 inside one JSON-RPC message, held in memory several times over.
	maxUploadAssetBytes = 16 << 20
	// maxReadAssetBytes bounds read_asset. Past this a client's context limits
	// refuse the result anyway.
	maxReadAssetBytes = 5 << 20
)

func assetNotFound(rel string) error { return fmt.Errorf("Asset not found: %s", rel) }

func deletedAssetNotFound(token string) error {
	return fmt.Errorf("Deleted asset not found: %s", token)
}

var errAssetsUnsupported = errors.New("This vault's backend cannot manage attachments.")

func assetManager(b Backend) (backend.AssetManager, error) {
	m, ok := b.(backend.AssetManager)
	if !ok {
		return nil, errAssetsUnsupported
	}
	return m, nil
}

// matchAsset finds a client's path in a listing: exact, else the one path equal
// after case and accent folding, so the vault's spelling is what gets used.
func matchAsset(all []vault.AssetMeta, raw string) (vault.AssetMeta, bool) {
	clean, ok := cleanRequest(raw)
	if !ok {
		return vault.AssetMeta{}, false
	}
	folded := search.Fold(clean)
	var match vault.AssetMeta
	count := 0
	for _, a := range all {
		if a.Path == clean {
			return a, true
		}
		if search.Fold(a.Path) == folded {
			match = a
			count++
		}
	}
	return match, count == 1
}

// shownAssets is list_assets' answer: the listing without a database's schema
// file and backups, which the app does not show as attachments either. The tools
// below keep the whole listing, since a schema file is how they know a CSV is a
// database.
func shownAssets(all []vault.AssetMeta) []vault.AssetMeta {
	out := make([]vault.AssetMeta, 0, len(all))
	for _, a := range all {
		if !database.IsLooseInternalPath(a.Path) {
			out = append(out, a)
		}
	}
	return out
}

func findAsset(ctx context.Context, b Backend, raw string) (vault.AssetMeta, []vault.AssetMeta, error) {
	all, err := b.ListAssets(ctx)
	if err != nil {
		return vault.AssetMeta{}, nil, err
	}
	meta, ok := matchAsset(all, raw)
	if !ok {
		return vault.AssetMeta{}, nil, assetNotFound(raw)
	}
	return meta, all, nil
}

// refuseDatabaseFile keeps these tools off a loose CSV database and its schema
// file: they move as a pair, which only the database code knows how to do.
func refuseDatabaseFile(meta vault.AssetMeta, all []vault.AssetMeta) error {
	refused := database.IsSidecarPath(meta.Path)
	if !refused && database.IsLooseCSVPath(meta.Path) {
		sidecar := search.Fold(meta.Path + database.SidecarSuffix)
		for _, a := range all {
			if search.Fold(a.Path) == sidecar {
				refused = true
				break
			}
		}
	}
	if refused {
		return errors.New("That file belongs to a ZenNotes database. Change it from the app.")
	}
	return nil
}

// checkAssetName refuses a name the asset listing would not show afterwards,
// since a file the listing cannot see is one no tool can reach again. Dots and
// spaces at the end of a name do not count.
func checkAssetName(name string) error {
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)
	bare := strings.TrimRight(lower, ". ")
	switch {
	case trimmed == "" || trimmed == "." || trimmed == "..":
		return errors.New("A file name is required.")
	case strings.ContainsAny(trimmed, "/\\\x00"):
		return errors.New("Use only a file name, with no folder in it.")
	case strings.HasPrefix(trimmed, "."):
		return errors.New("A file name cannot start with a dot.")
	case strings.HasSuffix(bare, ".md"):
		return errors.New("Notes are not attachments: use create_note or rename_note.")
	case strings.HasSuffix(bare, ".excalidraw"), vault.IsAtomicWriteTempPath(trimmed), database.IsLooseInternalPath(bare):
		return errors.New("That kind of file cannot be managed as an attachment.")
	}
	return nil
}

// checkUploadName also asks about the name the upload will really use, the one
// left after cleaning. It returns that name.
func checkUploadName(name string) (string, error) {
	if err := checkAssetName(name); err != nil {
		return "", err
	}
	cleaned := vault.CleanAssetFileName(name)
	if cleaned == "" {
		return strings.TrimSpace(name), nil
	}
	return cleaned, checkAssetName(cleaned)
}

// checkAssetRename adds what a rename must refuse and an upload need not, since
// an upload's name is cleaned the way the app's import cleans it. Most of these
// are read as link syntax (`#` a heading, `?` a query, `|` an alias, `]` the end
// of a wikilink, `^` a block, `%` an escape), and the rest do not survive on every
// system. Either way every link to the file would stop resolving while the rename
// reported success.
func checkAssetRename(name string) error {
	if err := checkAssetName(name); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(name)
	stem, _, _ := strings.Cut(strings.ToLower(trimmed), ":")
	if strings.HasSuffix(strings.TrimRight(stem, ". "), ".md") {
		return errors.New("Notes are not attachments: use create_note or rename_note.")
	}
	if vault.IsReservedFileName(trimmed) {
		return errors.New("That name is reserved on Windows, so choose another.")
	}
	if strings.ContainsAny(trimmed, "#?|[]^%:*\"<>`") || strings.IndexFunc(trimmed, unicode.IsControl) >= 0 || strings.HasSuffix(trimmed, ".") {
		return errors.New("A file name cannot contain # ? | [ ] ^ % : * \" < > ` or a control character, or end with a dot, since links to it would break.")
	}
	return nil
}

// checkAssetTargetDir refuses a destination whatever the scope: anything that
// is not a plain vault-relative folder, a database's insides, and the trash,
// whose contents empty_trash and the app destroy. Under a scope nothing here looks
// at the disk, and the trash answers as any other folder the client may not use,
// since what happens to be there is for assetDestination to judge once the path is
// known to be visible.
func checkAssetTargetDir(ctx context.Context, b Backend, raw string) (string, error) {
	refused := errors.New("This assistant cannot put files there.")
	clean, ok := cleanRequest(raw)
	// The move trims what it is given, so this has to be what it will use.
	if !ok || trimsDifferently(raw) {
		return "", refused
	}
	for _, seg := range strings.Split(clean, "/") {
		if strings.HasSuffix(strings.ToLower(seg), database.FormDirSuffix) {
			return "", refused
		}
	}
	settings, err := b.VaultSettings(ctx)
	if err != nil {
		return "", err
	}
	scoped, isScoped := b.(*scopedBackend)
	if insideFolded(clean, vault.ResolveFolderPath(vault.FolderTrash, settings.SystemFolderPaths)) {
		if isScoped {
			return "", scoped.destinationRefusal(ctx)
		}
		return "", refused
	}
	if !isScoped && b.Kind() == backend.KindLocal {
		if clean, ok = foldersAsListed(b.Root(), clean); !ok {
			return "", refused
		}
	}
	return clean, nil
}

// holdsEverywhere says whether a folder about to be made means the same folder on
// every system: a name ending in a dot or a space, or holding a colon, does not.
func holdsEverywhere(seg string) bool {
	return !strings.HasSuffix(seg, ".") && !strings.HasSuffix(seg, " ") && !strings.Contains(seg, ":")
}

// foldersAsListed writes each segment of rel that exists the way its parent lists
// it. It refuses a segment that is not a real folder under a name its parent lists,
// and a new one that would not hold on every system.
func foldersAsListed(root, rel string) (string, bool) {
	walk := root
	out := ""
	segments := strings.Split(rel, "/")
	for i, seg := range segments {
		next := filepath.Join(walk, seg)
		info, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			for _, fresh := range segments[i:] {
				if !holdsEverywhere(fresh) {
					return "", false
				}
			}
			return joinRel(out, strings.Join(segments[i:], "/")), true
		}
		if err != nil || !info.IsDir() {
			return "", false
		}
		listed, ok := folderNameListed(walk, seg)
		if !ok {
			return "", false
		}
		out = joinRel(out, listed)
		walk = filepath.Join(walk, listed)
	}
	return out, true
}

// folderNameListed finds name among what parent lists: exact, else equal after
// case and accent folding.
func folderNameListed(parent, name string) (string, bool) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	folded := search.Fold(name)
	match, found := "", false
	for _, e := range entries {
		if e.Name() == name {
			return name, true
		}
		if !found && search.Fold(e.Name()) == folded {
			match, found = e.Name(), true
		}
	}
	return match, found
}

// decodeAssetBase64 reads what a client plausibly sends: standard or URL-safe,
// padded or not, wrapped lines, a data: URL. It refuses by length before it
// decodes anything.
func decodeAssetBase64(raw string) ([]byte, error) {
	text := raw
	if strings.HasPrefix(text, "data:") {
		comma := strings.IndexByte(text, ',')
		if comma < 0 || !strings.HasSuffix(text[:comma], ";base64") {
			return nil, errors.New("content_base64 is a data: URL that is not base64.")
		}
		text = text[comma+1:]
	}
	tooLarge := fmt.Errorf("upload_asset takes files up to %d MiB; this one is larger. Add it from the app or copy it into the vault instead.", maxUploadAssetBytes>>20)
	// Wrapped base64 has at most a little whitespace on top; this bounds the
	// work below without refusing a file that fits.
	if len(text) > base64.StdEncoding.EncodedLen(maxUploadAssetBytes)*2 {
		return nil, tooLarge
	}
	text = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, text)
	text = strings.TrimRight(text, "=")
	if base64.RawStdEncoding.DecodedLen(len(text)) > maxUploadAssetBytes {
		return nil, tooLarge
	}
	data, err := base64.RawStdEncoding.DecodeString(text)
	if err != nil {
		return nil, errors.New("content_base64 is not valid base64.")
	}
	if len(data) == 0 {
		return nil, errors.New("content_base64 is empty.")
	}
	return data, nil
}

// assetRename and assetMove keep a note's links pointing at the file: a local
// vault rewrites them here, and a server rewrites them itself.
func assetRename(ctx context.Context, b Backend, rel, name string) (vault.AssetMeta, error) {
	if r, ok := b.(backend.AssetLinkRewriter); ok {
		return r.RenameAssetRewritingLinksIn(ctx, rel, name, nil)
	}
	m, err := assetManager(b)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	return m.RenameAsset(ctx, rel, name)
}

func assetMove(ctx context.Context, b Backend, rel, dir string) (vault.AssetMeta, error) {
	if r, ok := b.(backend.AssetLinkRewriter); ok {
		return r.MoveAssetRewritingLinksIn(ctx, rel, dir, nil)
	}
	m, err := assetManager(b)
	if err != nil {
		return vault.AssetMeta{}, err
	}
	return m.MoveAsset(ctx, rel, dir)
}

var assetImageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
}

var assetAudioTypes = map[string]string{
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".m4a": "audio/mp4", ".ogg": "audio/ogg",
}

var assetBinaryTypes = map[string]string{
	".pdf": "application/pdf", ".mp4": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
}

// assetResult is a read_asset answer: a line of facts, then the file as the MCP
// content type a client can use. A model sees an image, reads text as text, and
// anything else arrives as an embedded resource.
func assetResult(meta vault.AssetMeta, data []byte) (*sdk.CallToolResult, error) {
	ext := strings.ToLower(path.Ext(meta.Name))
	mimeType := ""
	var content sdk.Content
	if t, ok := assetImageTypes[ext]; ok {
		mimeType = t
		content = &sdk.ImageContent{Data: data, MIMEType: t}
	} else if t, ok := assetAudioTypes[ext]; ok {
		mimeType = t
		content = &sdk.AudioContent{Data: data, MIMEType: t}
	} else if _, binary := assetBinaryTypes[ext]; !binary && utf8.Valid(data) && !bytes.ContainsRune(data, 0) {
		mimeType = "text/plain"
		content = &sdk.TextContent{Text: string(data)}
	} else {
		mimeType = assetBinaryTypes[ext]
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		content = &sdk.EmbeddedResource{Resource: &sdk.ResourceContents{
			URI:      "zennotes://asset/" + url.PathEscape(meta.Path),
			MIMEType: mimeType,
			Blob:     data,
		}}
	}
	facts, err := Payload(orderedResult{{"path", meta.Path}, {"name", meta.Name}, {"size", meta.Size}, {"mimeType", mimeType}})
	if err != nil {
		return nil, err
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: facts}, content}}, nil
}

func assetTools() []toolDef {
	return []toolDef{
		{
			name:        "read_asset",
			description: "Read an attachment from list_assets: an image comes back as an image you can see, text as text, anything else (PDF, audio, video) as an embedded file. Files up to 5 MiB.",
			schema:      `{"type":"object","properties":{"path":{"type":"string","description":"Vault-relative path exactly as list_assets returned it."}},"required":["path"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				meta, _, err := findAsset(ctx, b, rel)
				if err != nil {
					return nil, err
				}
				if meta.Size > maxReadAssetBytes {
					return nil, fmt.Errorf("%s is %.1f MiB; read_asset returns files up to %d MiB.", meta.Name, float64(meta.Size)/(1<<20), maxReadAssetBytes>>20)
				}
				data, err := b.ReadAsset(ctx, meta.Path)
				if err != nil {
					return nil, err
				}
				if len(data) > maxReadAssetBytes {
					return nil, fmt.Errorf("%s is %.1f MiB; read_asset returns files up to %d MiB.", meta.Name, float64(len(data))/(1<<20), maxReadAssetBytes>>20)
				}
				return assetResult(meta, data)
			},
		},
		{
			name:        "upload_asset",
			description: "Add a file to the vault's assets folder from base64 (up to 16 MiB), never overwriting: a taken name gets a number. Returns its `path` and `markdown`, the embed or link to paste into a note with append_to_note or edit_section.",
			schema:      `{"type":"object","properties":{"filename":{"type":"string","description":"File name with its extension, such as plot.png."},"content_base64":{"type":"string","description":"The file's bytes as base64."}},"required":["filename","content_base64"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				name, err := a.requireString("filename")
				if err != nil {
					return nil, err
				}
				encoded, err := a.requireString("content_base64")
				if err != nil {
					return nil, err
				}
				cleaned, err := checkUploadName(name)
				if err != nil {
					return nil, err
				}
				m, err := assetManager(b)
				if err != nil {
					return nil, err
				}
				data, err := decodeAssetBase64(encoded)
				if err != nil {
					return nil, err
				}
				return m.ImportAsset(ctx, "", cleaned, bytes.NewReader(data))
			},
		},
		{
			name:        "rename_asset",
			description: "Rename an attachment in place. Notes that embed or link it are updated to the new name; `linksNotUpdated` lists any it could not read or write, which may still use the old name, so fix those by hand. Names holding # ? | [ ] ^ or % are refused, since links could not reach the file.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"name":{"type":"string","description":"New file name with its extension, no folder."}},"required":["path","name"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				name, err := a.requireString("name")
				if err != nil {
					return nil, err
				}
				if err := checkAssetRename(name); err != nil {
					return nil, err
				}
				meta, all, err := findAsset(ctx, b, rel)
				if err != nil {
					return nil, err
				}
				if err := refuseDatabaseFile(meta, all); err != nil {
					return nil, err
				}
				return assetRename(ctx, b, meta.Path, strings.TrimSpace(name))
			},
		},
		{
			name:        "move_asset",
			description: "Move an attachment to another folder, creating it if needed. Notes that embed or link it are updated; `linksNotUpdated` lists any it could not read or write, which may still use the old name. A taken name gets a number, so use the returned `path`.",
			schema:      `{"type":"object","properties":{"path":{"type":"string"},"targetDir":{"type":"string","description":"Vault-relative folder, such as assets/plots."}},"required":["path","targetDir"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				rawDir, err := a.requireString("targetDir")
				if err != nil {
					return nil, err
				}
				meta, all, err := findAsset(ctx, b, rel)
				if err != nil {
					return nil, err
				}
				if err := refuseDatabaseFile(meta, all); err != nil {
					return nil, err
				}
				dir, err := checkAssetTargetDir(ctx, b, rawDir)
				if err != nil {
					return nil, err
				}
				return assetMove(ctx, b, meta.Path, dir)
			},
		},
		pathTool("duplicate_asset", "Copy an attachment next to itself with a \" copy\" suffix.", func(ctx context.Context, b Backend, rel string) (any, error) {
			m, err := assetManager(b)
			if err != nil {
				return nil, err
			}
			meta, all, err := findAsset(ctx, b, rel)
			if err != nil {
				return nil, err
			}
			if err := refuseDatabaseFile(meta, all); err != nil {
				return nil, err
			}
			return m.DuplicateAsset(ctx, meta.Path)
		}),
		pathTool("delete_asset", "Soft-delete an attachment: it leaves the vault for the app's Trash and restore_asset brings it back. Keep the returned undoToken. Notes keep their links to it, so check them first with search_text.", func(ctx context.Context, b Backend, rel string) (any, error) {
			m, err := assetManager(b)
			if err != nil {
				return nil, err
			}
			meta, all, err := findAsset(ctx, b, rel)
			if err != nil {
				return nil, err
			}
			if err := refuseDatabaseFile(meta, all); err != nil {
				return nil, err
			}
			return m.DeleteAsset(ctx, meta.Path)
		}),
		{
			name:        "restore_asset",
			description: "Bring back a deleted attachment to where it was, using the undoToken from delete_asset or list_assets with deleted: true. A taken name gets a number, so use the returned `path`.",
			schema:      `{"type":"object","properties":{"undoToken":{"type":"string"}},"required":["undoToken"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				token, err := a.requireString("undoToken")
				if err != nil {
					return nil, err
				}
				m, err := assetManager(b)
				if err != nil {
					return nil, err
				}
				deleted, err := m.ListDeletedAssets(ctx)
				if err != nil {
					return nil, err
				}
				for _, d := range deleted {
					if d.UndoToken == token {
						return m.RestoreDeletedAsset(ctx, d)
					}
				}
				return nil, deletedAssetNotFound(token)
			},
		},
	}
}
