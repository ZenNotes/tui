package vault

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

// --- template folder (mirrors shared-domain/template-folder.ts) ---
//
// The vault's template folder (ZenNotes/zennotes#1024, desktop 2.80): a folder
// of ordinary notes whose every note is a template. vault.json stores it as
// `templates.folder`, a vault-relative directory path exactly as it exists on
// disk (the shape of the Tasks folder lists), so a remapped system folder
// needs no translation. A template's `- [ ]` lines are placeholders for the
// note it will make, so its notes stay out of Tasks and out of text search.
// The rules are a byte-for-byte mirror of template-folder.ts in the desktop
// repo and internal/vault/template_folder.go in znserver: change all three
// together.

// maxTemplateFolderLength caps the folder path, counted in UTF-16 code units
// as the TypeScript side's `String.length` counts it.
const maxTemplateFolderLength = 512

// NormalizeTemplateFolder validates a configured folder: backslashes become
// slashes, empty segments drop, each segment loses the whitespace
// JavaScript's trim() removes, and any segment starting with a dot (`.`,
// `..`, `.zennotes`, any hidden directory) refuses the whole value. Returns
// "" when the value is unusable, and the vault then has no template folder.
func NormalizeTemplateFolder(value string) string {
	parts := []string{}
	for _, seg := range strings.Split(strings.ReplaceAll(value, "\\", "/"), "/") {
		s := strings.TrimFunc(seg, isJSWhitespace)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, ".") {
			return ""
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, "/")
	if len(utf16.Encode([]rune(joined))) > maxTemplateFolderLength {
		return ""
	}
	return joined
}

// templateFolderOf reads `templates.folder` off a decoded vault.json the way
// the TypeScript reads an `unknown`: a `templates` that is not an object, or
// a folder that is not a string, is no folder. Keys match exactly.
func templateFolderOf(raw map[string]any) string {
	return NormalizeTemplateFolder(stringOf(mapOf(raw["templates"])["folder"]))
}

// IsPathInTemplateFolder reports whether a vault-relative POSIX path is inside
// the folder: a whole-segment prefix match, case-sensitive like the rest of
// the vault layer, so `Templates` never holds `Templates Old/a.md`. No folder
// matches nothing.
func IsPathInTemplateFolder(relPath, folder string) bool {
	if folder == "" {
		return false
	}
	return relPath == folder || strings.HasPrefix(relPath, folder+"/")
}

// rewriteTemplateFolderForRename follows a folder rename or move: a template
// folder named by the renamed folder, or inside it, moves with it. Anything
// else comes back unchanged.
func rewriteTemplateFolderForRename(folder, oldRel, newRel string) string {
	if folder == "" || oldRel == "" || newRel == "" || oldRel == newRel {
		return folder
	}
	if folder != oldRel && !strings.HasPrefix(folder, oldRel+"/") {
		return folder
	}
	moved := NormalizeTemplateFolder(newRel + folder[len(oldRel):])
	if moved == "" {
		return folder
	}
	return moved
}

// TasksFolderLists is the pair every task scan applies: the excluded folders
// and the Task folders, with the template folder added to the excluded ones,
// so its placeholders stay out of Tasks even inside a Task folder. Mirrors
// tasksFolderListsForSettings in tasks-excluded-folders.ts. A scan asked for
// everything (IncludeExcluded) skips the lists and so reaches the template
// folder too. The returned slice is a fresh copy.
func (s VaultSettings) TasksFolderLists() (excluded, included []string) {
	excluded = append([]string{}, s.TasksExcludedFolders...)
	included = s.TasksIncludedFolders
	if s.TemplateFolder == "" {
		return excluded, included
	}
	for _, entry := range excluded {
		if entry == s.TemplateFolder {
			return excluded, included
		}
	}
	return append(excluded, s.TemplateFolder), included
}

// isJSWhitespace is the set JavaScript's String.prototype.trim() removes:
// WhiteSpace and LineTerminator, so NEL (U+0085) is not trimmed.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x2028, 0x2029, 0xfeff:
		return true
	}
	return unicode.Is(unicode.Zs, r)
}
