package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

var prefsWriteMu sync.Mutex

// SetValue edits one portable preference while retaining every other table,
// including settings from newer desktop versions. nil removes a map entry.
func SetValue(section, key string, value any) error {
	return UpdateConfig(func(doc map[string]any) error {
		table, ok := doc[section].(map[string]any)
		if !ok {
			if _, exists := doc[section]; exists {
				return fmt.Errorf("%s is not a config table", section)
			}
			table = map[string]any{}
			doc[section] = table
		}
		if value == nil {
			delete(table, key)
		} else {
			table[key] = value
		}
		return nil
	})
}

// SetViewModeRule adds or changes one [terminal.view_mode_rules] entry,
// keeping every other key and table. Rules already in the file that are
// malformed never block the write.
func SetViewModeRule(key, mode string) error {
	if err := ValidateViewModeRule(key, mode); err != nil {
		return err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	return UpdateConfig(func(doc map[string]any) error {
		terminal, rules, err := viewModeTables(doc, true)
		if err != nil {
			return err
		}
		rules[key] = mode
		terminal["view_mode_rules"] = rules
		doc["terminal"] = terminal
		return nil
	})
}

// RemoveViewModeRule deletes the entry stored under the raw key exactly as
// written, so malformed keys can be removed too. Removing the last rule
// drops the empty table.
func RemoveViewModeRule(key string) error {
	return UpdateConfig(func(doc map[string]any) error {
		terminal, rules, err := viewModeTables(doc, false)
		if err != nil || rules == nil {
			return err
		}
		delete(rules, key)
		if len(rules) == 0 {
			delete(terminal, "view_mode_rules")
		}
		return nil
	})
}

// viewModeTables returns [terminal] and its rules sub-table, creating them
// when create is set; otherwise missing tables come back nil.
func viewModeTables(doc map[string]any, create bool) (terminal, rules map[string]any, err error) {
	raw, exists := doc["terminal"]
	terminal, ok := raw.(map[string]any)
	if exists && !ok {
		return nil, nil, fmt.Errorf("terminal is not a config table")
	}
	if !exists {
		if !create {
			return nil, nil, nil
		}
		terminal = map[string]any{}
	}
	raw, exists = terminal["view_mode_rules"]
	rules, ok = raw.(map[string]any)
	if exists && !ok {
		return nil, nil, fmt.Errorf("terminal.view_mode_rules is not a config table")
	}
	if !exists {
		if !create {
			return terminal, nil, nil
		}
		rules = map[string]any{}
	}
	return terminal, rules, nil
}

// UpdateConfig applies a targeted edit to the latest file. A parse failure or
// a concurrent external edit leaves the original file untouched.
func UpdateConfig(edit func(map[string]any) error) error {
	prefsWriteMu.Lock()
	defer prefsWriteMu.Unlock()
	path := ConfigTomlPath()
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if !os.IsNotExist(err) {
		return err
	} else if info, e := os.Lstat(path); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config symlink target is missing: %s", path)
	}
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	input := before
	if os.IsNotExist(err) {
		input = nil
	}
	doc := map[string]any{}
	if _, err := toml.Decode(string(input), &doc); err != nil {
		return fmt.Errorf("config.toml: %w", err)
	}
	if err := edit(doc); err != nil {
		return err
	}
	if err := validateKnownValues(doc, true); err != nil {
		return err
	}
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(out.Bytes())
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	latest, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(before, latest) {
		return fmt.Errorf("config.toml changed while saving; retry the change")
	}
	return os.Rename(tmp.Name(), path)
}
