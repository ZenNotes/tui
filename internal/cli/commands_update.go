package cli

import (
	"context"
	"fmt"

	"github.com/ZenNotes/tui/internal/selfupdate"
)

func cmdUpdate(ctx context.Context, args Args) error {
	if len(args.Positionals) != 0 {
		return fmt.Errorf("usage: zn update [--check] [--version <version>]")
	}
	result, err := selfupdate.Update(ctx, Version, args.Str("version"), args.Bool("check"), stderr)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(result)
	} else {
		emitLine(fmt.Sprintf("Installed: %s (%s)\nRelease: %s\n%s", result.Current, result.Owner, result.Available, result.Instruction))
		if result.Updated {
			emitOK("Updated to " + result.Available)
		}
	}
	return nil
}
