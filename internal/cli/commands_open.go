package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var markdownExtensions = []string{".md", ".markdown"}

func isMarkdownFilePath(candidate string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(candidate)))
	for _, e := range markdownExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

type openTarget struct {
	abs         string
	isDirectory bool
}

// resolveOpenTarget tries a path against the current directory, then the
// vault root, so the vault-relative paths `zn list` prints open from anywhere.
func resolveOpenTarget(vaultRoot, target string) *openTarget {
	candidates := []string{}
	if abs, err := filepath.Abs(target); err == nil {
		candidates = append(candidates, abs)
	}
	if vaultRoot != "" {
		candidates = append(candidates, filepath.Join(vaultRoot, target))
	}
	for _, c := range candidates {
		info, err := os.Stat(c)
		if err != nil {
			continue
		}
		if info.IsDir() || info.Mode().IsRegular() {
			return &openTarget{abs: c, isDirectory: info.IsDir()}
		}
	}
	return nil
}

// FindDesktopApp locates the ZenNotes desktop executable. ZENNOTES_APP_PATH
// wins; then the usual install locations per platform; then PATH.
func FindDesktopApp() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("ZENNOTES_APP_PATH")); explicit != "" {
		if resolved, ok := executableInside(explicit); ok {
			return resolved, nil
		}
		return "", fmt.Errorf("ZENNOTES_APP_PATH points at %s, which is not a ZenNotes executable or .app bundle.", explicit)
	}
	home, _ := os.UserHomeDir()
	candidates := []string{}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/ZenNotes.app",
			filepath.Join(home, "Applications", "ZenNotes.app"),
		)
		if out, err := exec.Command("mdfind", "kMDItemCFBundleIdentifier == 'com.adibhanna.zennotes'").Output(); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					candidates = append(candidates, line)
				}
			}
		}
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		candidates = append(candidates,
			filepath.Join(local, "Programs", "ZenNotes", "ZenNotes.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "ZenNotes", "ZenNotes.exe"),
		)
	default:
		candidates = append(candidates,
			"/opt/ZenNotes/zennotes",
			"/opt/zennotes/zennotes",
			"/usr/lib/zennotes/zennotes",
			"/usr/bin/zennotes",
			"/usr/local/bin/zennotes",
			filepath.Join(home, ".local", "bin", "zennotes"),
			filepath.Join(home, "Applications", "ZenNotes.AppImage"),
		)
	}
	for _, c := range candidates {
		if resolved, ok := executableInside(c); ok {
			return resolved, nil
		}
	}
	for _, name := range []string{"zennotes", "ZenNotes", "zen-notes"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("Could not find the ZenNotes desktop app. Install it, or point ZENNOTES_APP_PATH at the app (the .app bundle on macOS, the executable elsewhere).")
}

// executableInside resolves a `.app` bundle to its main executable and
// verifies a plain path is runnable.
func executableInside(p string) (string, bool) {
	info, err := os.Stat(p)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		if strings.HasSuffix(strings.ToLower(p), ".app") {
			macos := filepath.Join(p, "Contents", "MacOS")
			entries, err := os.ReadDir(macos)
			if err != nil {
				return "", false
			}
			for _, e := range entries {
				full := filepath.Join(macos, e.Name())
				if fi, err := os.Stat(full); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
					return full, true
				}
			}
		}
		return "", false
	}
	if runtime.GOOS == "windows" || info.Mode()&0o111 != 0 {
		return p, true
	}
	return "", false
}

// launchGrace is how long a cold start gets to crash before the launch is
// called good.
const launchGrace = 500 * time.Millisecond

func cmdOpen(vaultRoot string, args Args) error {
	if len(args.Positionals) == 0 {
		return errors.New("zn open needs a path. Usage: zn open <file.md | folder> [more ...]")
	}
	resolved := []openTarget{}
	unresolved := ""
	for _, target := range args.Positionals {
		hit := resolveOpenTarget(vaultRoot, target)
		if hit == nil {
			unresolved = target
			break
		}
		resolved = append(resolved, *hit)
	}
	if unresolved != "" {
		// An unquoted path with spaces arrives as several tokens: re-join and
		// try once more as a single path before giving up.
		joined := strings.Join(args.Positionals, " ")
		var hit *openTarget
		if len(args.Positionals) > 1 {
			hit = resolveOpenTarget(vaultRoot, joined)
		}
		if hit == nil {
			locations := []string{}
			if abs, err := filepath.Abs(unresolved); err == nil {
				locations = append(locations, abs)
			}
			if vaultRoot != "" {
				locations = append(locations, filepath.Join(vaultRoot, unresolved))
			}
			hint := ""
			if len(args.Positionals) > 1 {
				hint = fmt.Sprintf(" (also tried as one path: %q)", joined)
			}
			return fmt.Errorf("No such file or folder: %s (looked in %s)%s", unresolved, strings.Join(locations, " or "), hint)
		}
		resolved = []openTarget{*hit}
	}
	for _, t := range resolved {
		if !t.isDirectory && !isMarkdownFilePath(t.abs) {
			return fmt.Errorf("zn open only supports markdown files (.md, .markdown): %s", t.abs)
		}
	}
	app, err := FindDesktopApp()
	if err != nil {
		return err
	}
	paths := make([]string, len(resolved))
	for i, t := range resolved {
		paths[i] = t.abs
	}
	// The app's single-instance handling routes the paths to a running
	// ZenNotes (or starts one), where the open logic decides whether each is
	// a vault note, a standalone file, or a folder session.
	cmd := exec.Command(app, paths...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Could not launch ZenNotes: %v", err)
	}
	// The child is detached and silent, so the only honest success signal is
	// the absence of an immediate failure: a warm hand-off exits 0 almost
	// instantly, which is success; past the grace window we stop waiting.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("ZenNotes exited before it could open anything (%v).", err)
		}
	case <-time.After(launchGrace):
	}
	if len(resolved) == 1 {
		prefix := ""
		if resolved[0].isDirectory {
			prefix = "folder "
		}
		emitOK(fmt.Sprintf("Opening %s%s in ZenNotes", prefix, resolved[0].abs))
	} else {
		emitOK(fmt.Sprintf("Opening %d items in ZenNotes", len(resolved)))
	}
	return nil
}
