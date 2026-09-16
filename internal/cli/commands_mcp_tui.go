package cli

import (
	"context"
	"errors"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/mcp"
	"github.com/ZenNotes/tui/internal/tui"
)

// cmdMCP runs the MCP stdio server for as long as the client keeps the
// pipe open. The server pins the first successfully resolved backend for the
// session and retries resolution failures, so starting before configuration
// works without letting later desktop switches redirect in-flight edits.
func cmdMCP(ctx context.Context, args Args) error {
	return mcp.Run(ctx, mcp.Options{
		ResolveBackend: func() (mcp.Backend, error) {
			target, err := ResolveTargetFromArgs(args)
			if err != nil {
				return nil, err
			}
			return OpenBackend(target)
		},
	})
}

// cmdTUI opens the terminal app on the resolved vault; a positional path
// opens that note first.
func cmdTUI(ctx context.Context, args Args) error {
	source := args.Str("workspace-source")
	if source == "" {
		source = "terminal"
	}
	resolve := func() (backend.Target, error) {
		return backend.ResolveTargetWithSource(args.Str("vault"), args.Str("server"), args.Str("token"), source)
	}
	target, err := resolve()
	if errors.Is(err, config.ErrNoVault) && args.Str("vault") == "" && args.Str("server") == "" && stdinIsTTY() {
		// First run: set a vault up right here, then carry on into the app.
		if setupErr := runSetup(ctx); setupErr != nil {
			return setupErr
		}
		target, err = resolve()
	}
	if err != nil {
		return err
	}
	b, err := OpenBackend(target)
	if err != nil {
		return err
	}
	return tui.Run(ctx, tui.Options{
		Backend:         b,
		Target:          target,
		OpenPath:        args.Positional(0),
		Version:         Version,
		WorkspaceSource: source,
	})
}
