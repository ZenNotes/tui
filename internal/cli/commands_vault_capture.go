package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/config"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

func cmdVaultInfo(ctx context.Context, b backend.Backend, args Args) error {
	notes, err := b.ListNotes(ctx)
	if err != nil {
		return err
	}
	subfolders, err := b.ListFolders(ctx)
	if err != nil {
		return err
	}
	type folderCounts struct {
		Inbox   int `json:"inbox"`
		Quick   int `json:"quick"`
		Archive int `json:"archive"`
		Trash   int `json:"trash"`
	}
	var counts folderCounts
	for _, n := range notes {
		switch n.Folder {
		case vault.FolderInbox:
			counts.Inbox++
		case vault.FolderQuick:
			counts.Quick++
		case vault.FolderArchive:
			counts.Archive++
		case vault.FolderTrash:
			counts.Trash++
		}
	}
	if args.Bool("json") {
		emitJSON(struct {
			VaultRoot      string       `json:"vaultRoot"`
			Kind           string       `json:"kind"`
			Counts         folderCounts `json:"counts"`
			SubfolderCount int          `json:"subfolderCount"`
		}{b.Label(), string(b.Kind()), counts, len(subfolders)})
		return nil
	}
	label := "Vault"
	if b.Kind() == backend.KindRemote {
		label = "Server"
	}
	emitLine(fmt.Sprintf("%s: %s", label, b.Label()))
	emitLine(fmt.Sprintf("  inbox:   %d", counts.Inbox))
	emitLine(fmt.Sprintf("  quick:   %d", counts.Quick))
	emitLine(fmt.Sprintf("  archive: %d", counts.Archive))
	emitLine(fmt.Sprintf("  trash:   %d", counts.Trash))
	emitLine(fmt.Sprintf("  subfolders: %d", len(subfolders)))
	return nil
}

// vaultListEntry keeps `root` for local entries so scripts reading the JSON
// keep working now that servers appear in the same list.
type vaultListEntry struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Root         string `json:"root,omitempty"`
	BaseURL      string `json:"baseUrl,omitempty"`
	LastOpenedAt *int64 `json:"lastOpenedAt"`
	IsDefault    bool   `json:"isDefault"`
}

func cmdVaultList(args Args) error {
	ws := config.LoadWorkspaces()
	defaultTarget, _ := backend.ResolveDefaultTarget("")
	isDefault := func(kind, root, baseURL string) bool {
		if kind == "local" {
			return defaultTarget.Kind == backend.KindLocal && root != "" && filepath.Clean(root) == filepath.Clean(defaultTarget.Root)
		}
		return defaultTarget.Kind == backend.KindRemote && strings.EqualFold(strings.TrimRight(baseURL, "/"), strings.TrimRight(defaultTarget.BaseURL, "/"))
	}
	entries := []vaultListEntry{}
	seenRoot := map[string]bool{}
	seenURL := map[string]bool{}
	for _, v := range ws.Vaults {
		seenRoot[filepath.Clean(v.Root)] = true
		entries = append(entries, vaultListEntry{Name: v.Name, Kind: "local", Root: v.Root, IsDefault: isDefault("local", v.Root, "")})
	}
	for _, s := range ws.Servers {
		seenURL[strings.ToLower(s.URL)] = true
		entries = append(entries, vaultListEntry{Name: s.Name, Kind: "remote", BaseURL: s.URL, IsDefault: isDefault("remote", "", s.URL)})
	}
	for _, v := range config.KnownVaults() {
		if seenRoot[filepath.Clean(v.Root)] {
			continue
		}
		entries = append(entries, vaultListEntry{Name: v.Name, Kind: "local", Root: v.Root, LastOpenedAt: v.LastOpenedAt, IsDefault: isDefault("local", v.Root, "")})
	}
	for _, p := range config.RemoteProfiles() {
		if seenURL[strings.ToLower(p.BaseURL)] {
			continue
		}
		entries = append(entries, vaultListEntry{Name: p.Name, Kind: "remote", BaseURL: p.BaseURL, LastOpenedAt: p.LastConnectedAt, IsDefault: isDefault("remote", "", p.BaseURL)})
	}
	if args.Bool("json") {
		emitJSON(entries)
		return nil
	}
	if len(entries) == 0 {
		emitLine("No vaults yet. `zn setup` walks you through it; or `zn init ~/Notes`, `zn vault add <folder>`, `zn connect <url>`.")
		return nil
	}
	nameWidth := 4
	kindWidth := 0
	for _, e := range entries {
		nameWidth = max(nameWidth, len([]rune(e.Name)))
		if e.Kind == "remote" {
			kindWidth = 6
		}
	}
	for _, e := range entries {
		marker := " "
		if e.IsDefault {
			marker = "*"
		}
		age := ""
		if e.LastOpenedAt != nil {
			age = formatRelativeAge(*e.LastOpenedAt)
		}
		kind := ""
		if kindWidth > 0 {
			kind = pad(e.Kind, kindWidth) + "  "
		}
		location := e.Root
		if location == "" {
			location = e.BaseURL
		}
		emitLine(fmt.Sprintf("%s %s  %s%s  %s", marker, pad(e.Name, nameWidth), kind, pad(age, 8), location))
	}
	return nil
}

var (
	leadingHashesRe  = regexp.MustCompile(`^#+\s*`)
	leadingBulletRe  = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)
	leadingCheckboxR = regexp.MustCompile(`^\[[ xX]\]\s*`)
	wsRunRe          = regexp.MustCompile(`\s+`)
)

func stripLeadingMarker(line string) string {
	line = leadingHashesRe.ReplaceAllString(line, "")
	line = leadingBulletRe.ReplaceAllString(line, "")
	line = leadingCheckboxR.ReplaceAllString(line, "")
	return strings.TrimSpace(line)
}

// DeriveTitle is the first non-empty line, stripped of its leading marker
// and capped at 60 characters, so a captured `- [ ] buy milk` titles the note
// "buy milk" while the marker stays in the body.
func DeriveTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if t := stripLeadingMarker(strings.TrimSpace(line)); t != "" {
			runes := []rune(t)
			if len(runes) > 60 {
				return string(runes[:60])
			}
			return t
		}
	}
	return time.Now().UTC().Format("2006-01-02-15-04-05")
}

func composeCaptureBody(title, body string, tags []string) string {
	heading := "# " + title + "\n\n"
	tagLine := ""
	if len(tags) > 0 {
		parts := make([]string, len(tags))
		for i, t := range tags {
			parts[i] = "#" + t
		}
		tagLine = strings.Join(parts, " ") + "\n\n"
	}
	return heading + tagLine + strings.TrimRight(body, "\n") + "\n"
}

func cmdCapture(ctx context.Context, b backend.Backend, args Args) error {
	positional := strings.TrimSpace(strings.Join(args.Positionals, " "))
	body := positional
	if body == "" {
		body = strings.TrimSpace(ReadStdin())
	}
	if body == "" {
		return errors.New("zn capture needs text. Pass it as a positional argument or pipe via stdin.")
	}
	folder := vault.FolderInbox
	if f, ok := args.String("folder"); ok {
		switch f {
		case "inbox", "quick", "archive":
			folder = vault.NoteFolder(f)
		default:
			return errors.New("zn capture --folder must be inbox, quick, or archive.")
		}
	}
	tags := stripTagPrefix(args.Many("tag"))
	title := args.Str("title")
	if _, ok := args.String("title"); !ok {
		title = DeriveTitle(body)
	}
	composed := composeCaptureBody(title, body, tags)
	meta, err := b.CreateNote(ctx, folder, title, "", &composed)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(meta)
		return nil
	}
	emitOK("Captured " + meta.Path)
	emitLine("  " + truncate(strings.TrimSpace(wsRunRe.ReplaceAllString(body, " ")), 80))
	return nil
}

// cmdVaultMode shows or switches the notes layout: `inbox` keeps notes
// under the inbox directory, `root` puts them at the vault root.
func cmdVaultMode(ctx context.Context, b backend.Backend, args Args) error {
	local, ok := b.(*backend.Local)
	if !ok {
		return errors.New("vault mode works on a local vault; a server owns its own layout")
	}
	v := local.Vault()
	target := strings.ToLower(strings.TrimSpace(args.Positional(0)))
	current := v.PrimaryNotesLocation()
	if target == "" {
		if args.Bool("json") {
			emitJSON(struct {
				Mode string `json:"mode"`
			}{string(current)})
			return nil
		}
		emitLine(fmt.Sprintf("%s (notes %s)", current, map[vault.PrimaryNotesLocation]string{vault.PrimaryNotesRoot: "at the vault root", vault.PrimaryNotesInbox: "under the inbox folder"}[current]))
		return nil
	}
	var mode vault.PrimaryNotesLocation
	switch target {
	case "root":
		mode = vault.PrimaryNotesRoot
	case "inbox":
		mode = vault.PrimaryNotesInbox
	default:
		return fmt.Errorf("unknown mode %q: use root or inbox", target)
	}
	moved, err := v.SwitchPrimaryMode(mode)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(struct {
			Mode  string   `json:"mode"`
			Moved []string `json:"moved"`
		}{string(mode), moved})
		return nil
	}
	if len(moved) == 0 && current == mode {
		emitLine(fmt.Sprintf("Already in %s mode", mode))
		return nil
	}
	emitLine(fmt.Sprintf("Switched to %s mode, moved %d entries: %s", mode, len(moved), strings.Join(moved, ", ")))
	return nil
}
