package cli

import (
	"context"
	"errors"

	"github.com/ZenNotes/zennotescli/internal/config"
	"github.com/ZenNotes/zennotescli/internal/mcp"
	"github.com/ZenNotes/zennotescli/internal/tui"
)

// cmdMCP runs the MCP stdio server for as long as the client keeps the
// pipe open. The vault is resolved lazily, per call, so a client that starts
// before anything is configured still gets a consistent tool surface.
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
	target, err := ResolveTargetFromArgs(args)
	if errors.Is(err, config.ErrNoVault) && args.Str("vault") == "" && args.Str("server") == "" && stdinIsTTY() {
		// First run: set a vault up right here, then carry on into the app.
		if setupErr := runSetup(ctx); setupErr != nil {
			return setupErr
		}
		target, err = ResolveTargetFromArgs(args)
	}
	if err != nil {
		return err
	}
	b, err := OpenBackend(target)
	if err != nil {
		return err
	}
	return tui.Run(ctx, tui.Options{
		Backend:  b,
		Target:   target,
		OpenPath: args.Positional(0),
		Version:  Version,
	})
}
