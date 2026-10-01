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
		if !stdinIsTTY() || args.Bool("no-input") {
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
	if source, _ := backend.ResolveWorkspaceSource(args.Str("workspace-source")); source == "app" {
		// A desktop-managed zn follows the app whatever zn's default says, so
		// "zn uses it by default now" would be wrong here: only zn tui does.
		if makeDefault {
			emitLine("zn tui opens it by default now. Other zn commands and zn mcp keep following the ZenNotes app, and use this token whenever the app is connected to " + baseURL + ".")
		} else {
			emitLine("zn commands and zn mcp use this token whenever the ZenNotes app is connected to " + baseURL + "; `--server " + baseURL + "` reaches it directly.")
		}
		return nil
	}
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
		return notSavedError(ws, name, "server")
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

// notSavedError explains why a selector matched nothing zn saved. `zn vault
// list` also shows the desktop app's entries, so a name copied from there
// gets told who owns it instead of a bare "no such name".
func notSavedError(ws config.Workspaces, selector, noun string) error {
	desktop := desktopWorkspaces()
	if v := desktop.FindVault(selector); v != nil {
		return fmt.Errorf("%q is a vault saved by the ZenNotes desktop app, not by zn; remove it in the app's vault switcher. zn only lists it.%s", v.Name, savedNamesHint(ws))
	}
	if s := desktop.FindServer(selector); s != nil {
		return fmt.Errorf("%q is a server saved by the ZenNotes desktop app, not by zn; remove it in the app's server settings. zn only lists it.%s", s.Name, savedNamesHint(ws))
	}
	hint := savedNamesHint(ws)
	if guess := containingName(selector, append(ws.Names(), desktop.Names()...)); guess != "" {
		hint = fmt.Sprintf(" Did you mean %q?%s", guess, hint)
	}
	return fmt.Errorf("No saved %s named %q.%s", noun, selector, hint)
}

// savedNamesHint names what zn itself saved, which is what `zn use`,
// `zn disconnect` and `zn vault remove` accept.
func savedNamesHint(ws config.Workspaces) string {
	names := ws.Names()
	if len(names) == 0 {
		return " zn has no saved vaults or servers of its own; `zn init`, `zn vault add` and `zn connect` save one."
	}
	return " zn's saved entries are: " + strings.Join(names, ", ") + ". A server's URL or host works too."
}

// containingName is the one saved name containing the selector as a word,
// for `zn vault remove workspace` against "workspace (notes.example.com)".
func containingName(selector string, names []string) string {
	needle := strings.ToLower(strings.TrimSpace(selector))
	if needle == "" {
		return ""
	}
	match := ""
	for _, name := range names {
		if strings.Contains(strings.ToLower(name), needle) {
			if match != "" {
				return ""
			}
			match = name
		}
	}
	return match
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
		desktop := desktopWorkspaces()
		switch {
		case backend.LooksLikeServerURL(sel):
			return cmdConnect(ctx, args)
		case desktop.FindVault(sel) != nil:
			// A vault the desktop app knows: save its folder under the same
			// name so the terminal can keep using it by that name.
			v := desktop.FindVault(sel)
			ws.AddVault(v.Name, v.Root)
			sel = v.Root
		case desktop.FindServer(sel) != nil:
			// The app keeps that server's token where zn cannot read it, so
			// this is a connect: it asks for the token once.
			s := desktop.FindServer(sel)
			args.Positionals = []string{s.URL}
			if args.Str("name") == "" {
				args.push("name", s.Name)
			}
			return cmdConnect(ctx, args)
		default:
			root, err := filepath.Abs(config.ExpandHome(sel))
			if err != nil {
				return err
			}
			if info, err := os.Stat(root); err != nil || !info.IsDir() {
				return fmt.Errorf("%v `zn use` also takes a folder path or a server URL.", notSavedError(ws, sel, "vault or server"))
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

// cmdVaultRemove forgets a saved vault or server. Note files stay; a
// server's token goes with it, as with `zn disconnect`.
func cmdVaultRemove(args Args) error {
	name := strings.TrimSpace(args.Positional(0))
	if name == "" {
		return errors.New("Usage: zn vault remove <name|folder|url>  (see `zn vault list`)")
	}
	ws := config.LoadWorkspaces()
	var kind, saved, location string
	if v := ws.FindVault(name); v != nil {
		kind, saved, location = "local", v.Name, v.Root
	} else if s := ws.FindServer(name); s != nil {
		kind, saved, location = "remote", s.Name, s.URL
	} else {
		return notSavedError(ws, name, "vault or server")
	}
	ws.Remove(saved)
	if err := config.SaveWorkspaces(ws); err != nil {
		return err
	}
	if kind == "remote" {
		_ = config.DeleteToken(location)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "name": saved, "kind": kind, "location": location})
		return nil
	}
	if kind == "remote" {
		emitOK(fmt.Sprintf("Forgot server %s (%s) and its token. Nothing on the server changed.", saved, location))
		return nil
	}
	emitOK(fmt.Sprintf("Forgot vault %s (%s). The notes are still there.", saved, location))
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
	fmt.Fprintln(stderr, "  4  Install a ZenNotes server on this machine")
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
	case "4":
		name, err := readLine("Server name", "home")
		if err != nil {
			return err
		}
		path, err := readLine("Vault folder", "~/Notes")
		if err != nil {
			return err
		}
		bind, err := readLine("Listen address", "127.0.0.1:7878")
		if err != nil {
			return err
		}
		return cmdServer(ctx, "setup", Parse([]string{name, "--vault", path, "--bind", bind}))
	}
	return fmt.Errorf("Pick 1, 2, 3 or 4.")
}

var _ = vault.FolderInbox
