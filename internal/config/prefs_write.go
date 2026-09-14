package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
	if err := validateKnownValues(doc); err != nil {
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
