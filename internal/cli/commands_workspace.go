package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/vault"
)

// Getting a vault in front of zn: create one, point at a folder, or
// connect to a server. Each command remembers its result in zn's own
// workspaces file and makes it the default, so the next `zn` or `zn tui`
// needs no flags. None of them needs a backend to exist yet.

// stdinIsTTY is true for a real terminal, unlike stdinIsTerminal, which
// also says yes to /dev/null; the wizard must not wait on the latter.
func stdinIsTTY() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// readLine prompts on stderr and reads one line from stdin.
func readLine(prompt, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(stderr, "%s [%s]: ", prompt, fallback)
	} else {
		fmt.Fprintf(stderr, "%s: ", prompt)
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback, nil
	}
	return line, nil
}

// readSecret prompts on stderr and reads a token without echo.
func readSecret(prompt string) (string, error) {
	fmt.Fprintf(stderr, "%s: ", prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// verifyServer talks to a server once so a bad URL or token fails here,
// with a plain message, rather than in the first real command.
func verifyServer(ctx context.Context, baseURL, token string) error {
	client := remote.NewClient(baseURL, token)
	if _, err := client.GetCurrentVault(ctx); err != nil {
		switch remote.StatusOf(err) {
		case 401, 403:
			return fmt.Errorf("%s rejected the token. It must match ZENNOTES_AUTH_TOKEN on the server.", baseURL)
		case 0:
			return errors.New(remote.ConnectionErrorMessage(baseURL, err))
		}
		return err
	}
	return nil
}

// cmdConnect saves a server, verifies the token and makes it the default.
func cmdConnect(ctx context.Context, args Args) error {
	raw := strings.TrimSpace(args.Positional(0))
	if raw == "" {
		return errors.New("Usage: zn connect <url|saved name> [--name <name>] [--token <token>] [--no-default]")
	}
	ws := config.LoadWorkspaces()
	baseURL := ""
	name := strings.TrimSpace(args.Str("name"))
	if saved := ws.FindServer(raw); saved != nil {
		baseURL = saved.URL
		if name == "" {
			name = saved.Name
		}
	} else {
		baseURL = remote.NormalizeBaseURL(raw)
	}
	token := backend.ResolveAuthTokenFor(baseURL, args.Str("token"), "")
	if token == "" {
		if !stdinIsTTY() {
			return fmt.Errorf("No token for %s. Pass --token <token> or set %s (the server's ZENNOTES_AUTH_TOKEN).", baseURL, backend.RemoteTokenEnv)
		}
		secret, err := readSecret("Token for " + baseURL + " (the server's ZENNOTES_AUTH_TOKEN)")
		if err != nil {
			return err
		}
		token = secret
	}
	if token == "" {
		return errors.New("A token is required.")
	}
	if err := verifyServer(ctx, baseURL, token); err != nil {
		return err
	}
	entry := ws.AddServer(name, baseURL)
	makeDefault := !args.Bool("no-default")
	if makeDefault {
		ws.Default = entry.Name
	}
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	if err := config.SaveToken(baseURL, token); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "name": entry.Name, "url": baseURL, "default": makeDefault})
		return nil
	}
	emitOK(fmt.Sprintf("Connected to %s (%s)", entry.Name, baseURL))
	if makeDefault {
		emitLine("zn and zn tui use it by default now. `zn use <name>` switches, `zn disconnect " + entry.Name + "` forgets it.")
	} else {
		emitLine("Use it with --server " + entry.Name + ", or `zn use " + entry.Name + "` to make it the default.")
	}
	return nil
}

// cmdDisconnect forgets a saved server and its token.
func cmdDisconnect(args Args) error {
	name := strings.TrimSpace(args.Positional(0))
	ws := config.LoadWorkspaces()
	if name == "" {
		if len(ws.Servers) == 1 {
			name = ws.Servers[0].Name
		} else {
			return errors.New("Usage: zn disconnect <name>  (see `zn vault list`)")
		}
	}
	saved := ws.FindServer(name)
	if saved == nil {
		return fmt.Errorf("No saved server named %q. `zn vault list` shows them.", name)
	}
	url := saved.URL
	ws.Remove(saved.Name)
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	_ = config.DeleteToken(url)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "name": name, "url": url})
		return nil
	}
	emitOK(fmt.Sprintf("Forgot %s (%s)", saved.Name, url))
	return nil
}

// cmdUse makes a saved vault or server the default; `app` follows the
// desktop app again; a path or URL is saved first.
func cmdUse(ctx context.Context, args Args) error {
	sel := strings.TrimSpace(args.Positional(0))
	if sel == "" {
		return cmdVaultList(args)
	}
	ws := config.LoadWorkspaces()
	if strings.EqualFold(sel, "app") || strings.EqualFold(sel, "desktop") {
		ws.Default = ""
		if err := config.SaveWorkspaces(ws); err != nil {
			return err
		}
		emitOK("zn follows the ZenNotes app's vault again")
		return nil
	}
	if _, ok := backend.TargetForWorkspace(ws, sel, ""); !ok {
		switch {
		case backend.LooksLikeServerURL(sel):
			return cmdConnect(ctx, args)
		default:
			root, err := filepath.Abs(config.ExpandHome(sel))
			if err != nil {
				return err
			}
			if info, err := os.Stat(root); err != nil || !info.IsDir() {
				return fmt.Errorf("%q is neither a saved name nor a folder. `zn vault list` shows the names.", sel)
			}
			ws.AddVault("", root)
		}
	}
	target, _ := backend.TargetForWorkspace(ws, sel, "")
	if target.Kind == backend.KindLocal {
		ws.Default = ws.FindVault(sel).Name
	} else {
		ws.Default = ws.FindServer(sel).Name
	}
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "default": ws.Default})
		return nil
	}
	emitOK("Default is now " + ws.Default)
	return nil
}

// cmdVaultAdd remembers an existing folder as a vault.
func cmdVaultAdd(args Args) error {
	raw := strings.TrimSpace(args.Positional(0))
	if raw == "" {
		return errors.New("Usage: zn vault add <folder> [--name <name>] [--no-default]")
	}
	root, err := filepath.Abs(config.ExpandHome(raw))
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a folder. `zn init %s` creates a vault there.", root, raw)
	}
	ws := config.LoadWorkspaces()
	entry := ws.AddVault(args.Str("name"), root)
	if !args.Bool("no-default") {
		ws.Default = entry.Name
	}
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "name": entry.Name, "root": entry.Root, "default": ws.Default == entry.Name})
		return nil
	}
	emitOK(fmt.Sprintf("Added %s (%s)", entry.Name, entry.Root))
	if ws.Default == entry.Name {
		emitLine("It is the default now; `zn tui` opens it.")
	}
	return nil
}

// cmdVaultRemove forgets a saved vault or server (files stay untouched).
func cmdVaultRemove(args Args) error {
	name := strings.TrimSpace(args.Positional(0))
	if name == "" {
		return errors.New("Usage: zn vault remove <name>")
	}
	ws := config.LoadWorkspaces()
	kind, ok := ws.Remove(name)
	if !ok {
		if v := ws.FindVault(name); v != nil {
			kind, _ = ws.Remove(v.Name)
			ok = true
		} else if s := ws.FindServer(name); s != nil {
			kind, _ = ws.Remove(s.Name)
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("No saved vault or server named %q. `zn vault list` shows them.", name)
	}
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	emitOK(fmt.Sprintf("Forgot %s (%s). Nothing on disk changed.", name, kind))
	return nil
}

// cmdInit creates a vault folder with its settings file and a first note,
// remembers it, and makes it the default.
func cmdInit(args Args) error {
	raw := strings.TrimSpace(args.Positional(0))
	if raw == "" {
		raw = "~/Notes"
	}
	root, err := initVault(raw)
	if err != nil {
		return err
	}
	ws := config.LoadWorkspaces()
	entry := ws.AddVault(args.Str("name"), root)
	if !args.Bool("no-default") {
		ws.Default = entry.Name
	}
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "name": entry.Name, "root": root, "default": ws.Default == entry.Name})
		return nil
	}
	emitOK(fmt.Sprintf("Created vault %s at %s", entry.Name, root))
	emitLine("Next: `zn tui` opens it; `zn create --title \"First note\"` adds a note; the ZenNotes app can open the same folder.")
	return nil
}

// initVault lays down a root-mode vault: the settings file and a welcome
// note. An existing folder is adopted, not overwritten.
func initVault(raw string) (string, error) {
	root, err := filepath.Abs(config.ExpandHome(raw))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, ".zennotes"), 0o755); err != nil {
		return "", err
	}
	settings := filepath.Join(root, ".zennotes", "vault.json")
	if _, err := os.Stat(settings); os.IsNotExist(err) {
		body := "{\n  \"primaryNotesLocation\": \"root\",\n  \"dailyNotes\": {\n    \"enabled\": true,\n    \"directory\": \"Daily\",\n    \"titlePattern\": \"yyyy-MM-dd\",\n    \"tasksDueOnNoteDate\": true\n  }\n}\n"
		if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	welcome := filepath.Join(root, "Welcome.md")
	if _, err := os.Stat(welcome); os.IsNotExist(err) {
		entries, _ := os.ReadDir(root)
		hasNotes := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				hasNotes = true
				break
			}
		}
		if !hasNotes {
			body := "# Welcome\n\nThis folder is a ZenNotes vault: every note is a plain Markdown file.\n\n- [ ] Press `Space` in `zn tui` to see every command\n- [ ] `Space d` opens today's daily note\n- [ ] `Space x` lists your tasks, `Space k` shows them as a board\n\nLink notes with [[wikilinks]], tag them with #tags, and add `due:2026-01-31` or `!high` to a task line.\n"
			_ = os.WriteFile(welcome, []byte(body), 0o644)
		}
	}
	return root, nil
}

// runSetup is the guided first run: `zn setup`, and what `zn tui` offers
// when nothing names a vault yet.
func runSetup(ctx context.Context) error {
	if !stdinIsTTY() {
		return config.ErrNoVault
	}
	fmt.Fprintln(stderr, "No vault is set up for zn yet.")
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "  1  Create a new vault")
	fmt.Fprintln(stderr, "  2  Use an existing folder of Markdown notes")
	fmt.Fprintln(stderr, "  3  Connect to a ZenNotes server")
	fmt.Fprintln(stderr)
	choice, err := readLine("Pick one", "1")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(choice) {
	case "1", "":
		path, err := readLine("Where should the vault live", "~/Notes")
		if err != nil {
			return err
		}
		return cmdInit(Parse([]string{path}))
	case "2":
		path, err := readLine("Folder", "")
		if err != nil {
			return err
		}
		if path == "" {
			return errors.New("A folder is required.")
		}
		return cmdVaultAdd(Parse([]string{path}))
	case "3":
		url, err := readLine("Server URL (like https://notes.example.com or localhost:7878)", "")
		if err != nil {
			return err
		}
		if url == "" {
			return errors.New("A URL is required.")
		}
		return cmdConnect(ctx, Parse([]string{url}))
	}
	return fmt.Errorf("Pick 1, 2 or 3.")
}

var _ = vault.FolderInbox
