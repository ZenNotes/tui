package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/x/editor"

	"github.com/ZenNotes/zennotescli/internal/config"
)

// cmdConfig opens config.toml in $EDITOR, writing a commented starter file
// first when none exists. `--path` only prints where the file lives.
func cmdConfig(ctx context.Context, args Args) error {
	path := config.ConfigTomlPath()
	if args.Bool("path") {
		fmt.Fprintln(stdout, path)
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(config.DefaultConfigTOML), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Created %s\n", path)
	} else if err != nil {
		return err
	}
	if args.Bool("no-edit") {
		fmt.Fprintln(stdout, path)
		return nil
	}
	cmd, err := editor.CommandContext(ctx, "ZenNotes", path)
	if err != nil {
		return fmt.Errorf("no editor found: set $EDITOR or $VISUAL (the file is at %s)", path)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with an error: %w", err)
	}
	fmt.Fprintf(stdout, "Edited %s\n", path)
	return nil
}
