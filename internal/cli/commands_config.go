package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/editor"

	"github.com/ZenNotes/tui/internal/config"
)

// cmdConfig opens config.toml in $EDITOR, writing a commented starter file
// first when none exists. `--path` only prints where the file lives.
func cmdConfig(ctx context.Context, args Args) error {
	path := config.ConfigTomlPath()
	if args.Bool("path") {
		if args.Bool("json") {
			emitJSON(path)
		} else {
			emitLine(path)
		}
		return nil
	}
	if args.Bool("no-input") && !args.Bool("no-edit") {
		return errors.New("opening an editor requires interactive input; use `zn config get/set/list` or --no-edit")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(config.DefaultConfigTOML), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "Created %s\n", path)
	} else if err != nil {
		return err
	}
	if args.Bool("no-edit") {
		if args.Bool("json") {
			emitJSON(path)
		} else {
			emitLine(path)
		}
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

func preferenceValues(p config.Prefs) map[string]map[string]any {
	values := config.ValueTables(p)
	for section, entries := range map[string]map[string]string{"keymaps": p.KeymapOverrides, "saved_filters": p.SavedTaskFilters, "kanban_column_titles": p.KanbanColumnTitles, "tweaks": p.ThemeTweaks} {
		values[section] = map[string]any{}
		for key, value := range entries {
			values[section][key] = value
		}
	}
	return values
}

func cmdConfigAction(ctx context.Context, args Args, action string) error {
	if action == "edit" || action == "" && (!args.Bool("json") || args.Bool("path") || args.Bool("no-edit")) {
		if len(args.Positionals) != 0 {
			return errors.New("use `zn config get <key>` or `zn config set <key> <value>`")
		}
		return cmdConfig(ctx, args)
	}
	prefs, _, err := config.LoadPrefs()
	if err != nil {
		return err
	}
	values := preferenceValues(prefs)
	key := args.Positional(0)
	if action == "list" || action == "" {
		if args.Bool("json") {
			emitJSON(values)
			return nil
		}
		var keys []string
		for section, entries := range values {
			for key := range entries {
				keys = append(keys, section+"."+key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			section, name, _ := strings.Cut(key, ".")
			emitLine(fmt.Sprintf("%s = %v", key, values[section][name]))
		}
		return nil
	}
	section, name, ok := strings.Cut(key, ".")
	if !ok || name == "" {
		return errors.New("use a section.key such as editor.word_wrap")
	}
	value, exists := values[section][name]
	if !exists {
		if action != "set" || (section != "keymaps" && section != "saved_filters" && section != "kanban_column_titles" && section != "tweaks") {
			return fmt.Errorf("unknown preference %q; `zn config list` shows supported keys", key)
		}
		value = ""
	}
	if action == "set" {
		if len(args.Positionals) != 2 {
			return errors.New("usage: zn config set <section.key> <value>")
		}
		raw := args.Positional(1)
		switch value.(type) {
		case bool:
			value, err = strconv.ParseBool(raw)
		case int:
			value, err = strconv.Atoi(raw)
		default:
			value = raw
		}
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", key, err)
		}
		if err := config.SetValue(section, name, value); err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "key": key, "value": value})
		} else {
			emitOK(fmt.Sprintf("%s = %v", key, value))
		}
		return nil
	}
	if args.Bool("json") {
		emitJSON(value)
	} else {
		emitLine(fmt.Sprint(value))
	}
	return nil
}
