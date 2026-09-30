package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"golang.org/x/term"
)

// Handler runs one command against a resolved backend.
type Handler func(ctx context.Context, b backend.Backend, args Args) error

func dispatchTable() map[string]Handler {
	return map[string]Handler{
		"list":            cmdList,
		"read":            cmdRead,
		"create":          cmdCreate,
		"write":           cmdWrite,
		"append":          cmdAppend,
		"prepend":         cmdPrepend,
		"rename":          cmdRename,
		"move":            cmdMove,
		"archive":         cmdArchive,
		"unarchive":       cmdUnarchive,
		"trash":           cmdTrash,
		"restore":         cmdRestore,
		"delete":          cmdDelete,
		"duplicate":       cmdDuplicate,
		"search":          cmdSearch,
		"search-title":    cmdSearchTitle,
		"backlinks":       cmdBacklinks,
		"folder list":     cmdFolderList,
		"folder create":   cmdFolderCreate,
		"folder rename":   cmdFolderRename,
		"folder delete":   cmdFolderDelete,
		"tag list":        cmdTagList,
		"tag find":        cmdTagFind,
		"task list":       cmdTaskList,
		"task toggle":     cmdTaskToggle,
		"comment list":    cmdCommentList,
		"comment add":     cmdCommentAdd,
		"comment reply":   cmdCommentReply,
		"comment resolve": cmdCommentResolve,
		"vault info":      cmdVaultInfo,
		"vault mode":      cmdVaultMode,
		"base list":       cmdBaseList,
		"base create":     cmdBaseCreate,
		"base rows":       cmdBaseRows,
		"base get":        cmdBaseGet,
		"base add":        cmdBaseAdd,
		"base set":        cmdBaseSet,
		"base convert":    cmdBaseConvert,
		"capture":         cmdCapture,
	}
}

// Main runs the CLI and returns the exit code.
func Main(argv []string) int {
	code, err := run(argv)
	if err != nil {
		var invocation *invocationError
		if errors.Is(err, context.Canceled) {
			code = 130
		}
		if errors.As(err, &invocation) && invocation.json {
			kind := "error"
			if code == 2 {
				kind = "usage"
			} else if code == 130 {
				kind = "cancelled"
			}
			_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]string{"code": kind, "message": err.Error()}})
		} else {
			emitError(err.Error())
		}
		if code == 0 {
			code = 1
		}
	}
	return code
}

type invocationError struct {
	err  error
	json bool
}

func (e *invocationError) Error() string { return e.err.Error() }
func (e *invocationError) Unwrap() error { return e.err }

// ResolveTargetFromArgs picks the vault for an invocation from the global
// flags.
func ResolveTargetFromArgs(args Args) (backend.Target, error) {
	return backend.ResolveTargetWithSource(args.Str("vault"), args.Str("server"), args.Str("token"), args.Str("workspace-source"))
}

// OpenBackend binds a target with the user's portable preferences applied.
func OpenBackend(target backend.Target) (backend.Backend, error) {
	prefs, _, _ := config.LoadPrefs()
	return backend.New(target, backend.Options{SyncTitleHeading: prefs.SyncTitleHeadingOnRename})
}

func run(argv []string) (code int, err error) {
	var args Args
	defer func() {
		if err != nil {
			err = &invocationError{err: err, json: requestedJSON(argv)}
		}
	}()
	if len(argv) == 1 && argv[0] == "--desktop-integration" {
		emitJSON(map[string]any{"protocol": 1, "version": Version})
		return 0, nil
	}
	key, parsed, parseErr := parseCommand(argv)
	args = parsed
	if parseErr != nil {
		return 2, parseErr
	}
	if key == "version" || args.Bool("version") && !commandSpecs()[key].Flags["version"] {
		if args.Bool("json") {
			emitJSON(map[string]string{"version": Version})
		} else {
			fmt.Fprint(stdout, RenderVersion(argv))
		}
		return 0, nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if args.Bool("help") {
		fmt.Fprint(stdout, RenderScopedHelp(key, argv))
		return 0, nil
	}
	if key == "" {
		if len(argv) == 0 && stdinIsTTY() && term.IsTerminal(int(os.Stdout.Fd())) {
			return 0, cmdTUI(ctx, args)
		}
		fmt.Fprint(stdout, RenderHelp(argv))
		return 0, nil
	}
	command, _, _ := strings.Cut(key, " ")

	switch command {
	case "mcp":
		return 0, cmdMCP(ctx, args)
	case "tui":
		return 0, cmdTUI(ctx, args)
	case "config":
		return 0, cmdConfigAction(ctx, args, strings.TrimSpace(strings.TrimPrefix(key, "config")))
	case "status":
		return 0, cmdStatus(args)
	case "doctor":
		return 0, cmdDoctor(ctx, args)
	case "completion":
		return 0, cmdCompletion(args)
	case "server":
		return 0, cmdServer(ctx, strings.TrimPrefix(key, "server "), args)
	case "update":
		return 0, cmdUpdate(ctx, args)
	case "__complete":
		for _, value := range completionCandidates(args.Positionals) {
			emitLine(value)
		}
		return 0, nil
	case "connect":
		return 0, cmdConnect(ctx, args)
	case "disconnect":
		return 0, cmdDisconnect(args)
	case "use":
		return 0, cmdUse(ctx, args)
	case "init":
		return 0, cmdInit(args)
	case "setup":
		if args.Bool("no-input") {
			return 2, errors.New("setup needs a terminal. Use `zn init <folder>`, `zn vault add <folder>` or `zn connect <url>` for automation")
		}
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
