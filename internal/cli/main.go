package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/config"
)

// Handler runs one command against a resolved backend.
type Handler func(ctx context.Context, b backend.Backend, args Args) error

var subcommands = map[string][]string{
	"folder": {"list", "create", "rename", "delete"},
	"tag":    {"list", "find"},
	"task":   {"list", "toggle"},
	"vault":  {"info", "list", "mode", "add", "remove", "rm", "use"},
	"base":   {"list", "create", "rows", "get", "add", "set", "convert"},
}

func dispatchTable() map[string]Handler {
	return map[string]Handler{
		"list":          cmdList,
		"read":          cmdRead,
		"create":        cmdCreate,
		"write":         cmdWrite,
		"append":        cmdAppend,
		"prepend":       cmdPrepend,
		"rename":        cmdRename,
		"move":          cmdMove,
		"archive":       cmdArchive,
		"unarchive":     cmdUnarchive,
		"trash":         cmdTrash,
		"restore":       cmdRestore,
		"delete":        cmdDelete,
		"duplicate":     cmdDuplicate,
		"search":        cmdSearch,
		"search-title":  cmdSearchTitle,
		"backlinks":     cmdBacklinks,
		"folder list":   cmdFolderList,
		"folder create": cmdFolderCreate,
		"folder rename": cmdFolderRename,
		"folder delete": cmdFolderDelete,
		"tag list":      cmdTagList,
		"tag find":      cmdTagFind,
		"task list":     cmdTaskList,
		"task toggle":   cmdTaskToggle,
		"vault info":    cmdVaultInfo,
		"vault mode":    cmdVaultMode,
		"base list":     cmdBaseList,
		"base create":   cmdBaseCreate,
		"base rows":     cmdBaseRows,
		"base get":      cmdBaseGet,
		"base add":      cmdBaseAdd,
		"base set":      cmdBaseSet,
		"base convert":  cmdBaseConvert,
		"capture":       cmdCapture,
	}
}

// Main runs the CLI and returns the exit code.
func Main(argv []string) int {
	code, err := run(argv)
	if err != nil {
		emitError(err.Error())
		if code == 0 {
			code = 1
		}
	}
	return code
}

// ResolveTargetFromArgs picks the vault for an invocation from the global
// flags.
func ResolveTargetFromArgs(args Args) (backend.Target, error) {
	return backend.ResolveTarget(args.Str("vault"), args.Str("server"), args.Str("token"))
}

// OpenBackend binds a target with the user's portable preferences applied.
func OpenBackend(target backend.Target) (backend.Backend, error) {
	prefs, _, _ := config.LoadPrefs()
	return backend.New(target, backend.Options{SyncTitleHeading: prefs.SyncTitleHeadingOnRename})
}

func peelSubcommand(command string, rest []string) (string, Args) {
	choices, ok := subcommands[command]
	if !ok {
		return "", Parse(rest)
	}
	if len(rest) == 0 {
		return "", Parse(rest)
	}
	for _, c := range choices {
		if rest[0] == c {
			return c, Parse(rest[1:])
		}
	}
	return "", Parse(rest)
}

func run(argv []string) (int, error) {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprint(stdout, RenderHelp(argv))
		return 0, nil
	}
	if argv[0] == "--version" || argv[0] == "version" {
		fmt.Fprint(stdout, RenderVersion(argv))
		return 0, nil
	}
	// Global flags may precede the command: `zn --vault work notes list`.
	lead := []string{}
	for len(argv) > 0 && strings.HasPrefix(argv[0], "--") {
		flag := argv[0]
		if i := strings.Index(flag, "="); i >= 0 || flag == "--json" || flag == "--no-color" {
			lead = append(lead, flag)
			argv = argv[1:]
			continue
		}
		if len(argv) < 2 {
			break
		}
		lead = append(lead, flag, argv[1])
		argv = argv[2:]
	}
	if len(argv) == 0 {
		fmt.Fprint(stdout, RenderHelp(argv))
		return 0, nil
	}
	command, rest := argv[0], append(append([]string{}, argv[1:]...), lead...)
	subcommand, args := peelSubcommand(command, rest)
	ctx := context.Background()
	key := command
	if subcommand != "" {
		key = command + " " + subcommand
	}

	switch command {
	case "mcp":
		return 0, cmdMCP(ctx, args)
	case "tui":
		return 0, cmdTUI(ctx, args)
	case "config":
		return 0, cmdConfig(ctx, args)
	case "connect":
		return 0, cmdConnect(ctx, args)
	case "disconnect":
		return 0, cmdDisconnect(args)
	case "use":
		return 0, cmdUse(ctx, args)
	case "init":
		return 0, cmdInit(args)
	case "setup":
		return 0, runSetup(ctx)
	}
	switch key {
	case "vault add":
		return 0, cmdVaultAdd(args)
	case "vault remove", "vault rm":
		return 0, cmdVaultRemove(args)
	case "vault use":
		return 0, cmdUse(ctx, args)
	}

	// `vault list` enumerates every vault and server, so it must work before
	// anything is configured.
	if key == "vault list" {
		return 0, cmdVaultList(args)
	}

	// `open` hands file paths to the desktop app, so it works without a vault
	// but uses one when available so vault-relative paths open from anywhere.
	if command == "open" {
		root := ""
		if target, err := ResolveTargetFromArgs(args); err == nil {
			if target.Kind == backend.KindRemote {
				return 1, errors.New("zn open hands file paths to the desktop app, so it needs a local vault. Use `zn read <path>` to print a note from a server instead.")
			}
			root = target.Root
		}
		return 0, cmdOpen(root, args)
	}

	handler, ok := dispatchTable()[key]
	if !ok {
		return 1, fmt.Errorf("Unknown command: zn %s. Run `zn --help` for usage.", key)
	}
	// Resolved only once the command is known, so a typo reports the typo
	// rather than complaining that no vault is configured.
	target, err := ResolveTargetFromArgs(args)
	if err != nil {
		return 1, err
	}
	b, err := OpenBackend(target)
	if err != nil {
		return 1, err
	}
	if err := handler(ctx, b, args); err != nil {
		return 1, err
	}
	return 0, nil
}

func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

var _ = os.Exit
var _ = exitCodeFor
