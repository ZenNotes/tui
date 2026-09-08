// Package config reads what the desktop app writes: the runtime config
// (`zennotes.config.json`, where the known vaults and saved servers live)
// and the portable preferences (`config.toml`). It never writes the former;
// zn follows the app, it does not steer it.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// UserDataDir mirrors Electron's `app.getPath('userData')` for the product
// name "ZenNotes". ZENNOTES_CONFIG_DIR overrides it, for tests and automation.
func UserDataDir() string {
	if override := strings.TrimSpace(os.Getenv("ZENNOTES_CONFIG_DIR")); override != "" {
		abs, err := filepath.Abs(override)
		if err == nil {
			return abs
		}
		return override
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ZenNotes")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "ZenNotes")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "ZenNotes")
		}
		return filepath.Join(home, ".config", "ZenNotes")
	}
}

// PortableConfigDir is where `config.toml` lives: an XDG-style location the
// user can sync. Deliberately NOT UserDataDir.
func PortableConfigDir() string {
	if explicit := strings.TrimSpace(os.Getenv("ZENNOTES_CONFIG_DIR")); explicit != "" {
		return explicit
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "zennotes")
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "zennotes")
	}
	return filepath.Join(home, ".config", "zennotes")
}

// ConfigTomlPath is the portable preferences file.
func ConfigTomlPath() string {
	return filepath.Join(PortableConfigDir(), "config.toml")
}

// ExpandHome turns `~` and `~/x` into absolute paths.
func ExpandHome(target string) string {
	if target == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(target, "~/") || strings.HasPrefix(target, "~"+string(filepath.Separator)) {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, target[2:])
	}
	return target
}
