// Package selfupdate respects the owner of the running CLI installation.
package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/releases"
)

type Installation struct {
	Executable  string `json:"executable"`
	Owner       string `json:"owner"`
	Instruction string `json:"instruction"`
}

func Detect(executable string, info *debug.BuildInfo) Installation {
	i := Installation{Executable: executable, Owner: "standalone", Instruction: "zn update"}
	path := filepath.ToSlash(executable)
	switch {
	case strings.Contains(path, "/Cellar/zn/") || strings.Contains(path, "/Cellar/zennotes/"):
		i.Owner, i.Instruction = "homebrew", "brew upgrade zn"
	case strings.Contains(path, "/cli/terminal/versions/") || strings.Contains(path, ".app/Contents/"):
		i.Owner, i.Instruction = "desktop", "Update ZenNotes desktop; it owns and verifies this CLI runtime."
	case strings.HasPrefix(path, "/nix/store/") || strings.HasPrefix(path, "/snap/") || strings.HasPrefix(path, "/usr/bin/"):
		i.Owner, i.Instruction = "package-manager", "Update zn with the package manager that installed it."
	case goInstalled(info):
		i.Owner, i.Instruction = "go", "go install github.com/ZenNotes/tui/cmd/zn@latest"
	}
	return i
}

func goInstalled(info *debug.BuildInfo) bool {
	if info == nil || info.Main.Path != "github.com/ZenNotes/tui" || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" || setting.Key == "-ldflags" && strings.Contains(setting.Value, "internal/cli.Version=") {
			return false
		}
	}
	return true
}

func Current() (Installation, error) {
	exe, err := os.Executable()
	if err != nil {
		return Installation{}, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return Installation{}, err
	}
	info, _ := debug.ReadBuildInfo()
	return Detect(exe, info), nil
}

type Result struct {
	Installation
	Current         string `json:"current"`
	Available       string `json:"available"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Updated         bool   `json:"updated"`
}

func Update(ctx context.Context, current, version string, check bool, output io.Writer) (Result, error) {
	i, err := Current()
	result := Result{Installation: i, Current: current}
	if err != nil {
		return result, err
	}
	c := releases.NewClient()
	release, err := c.Lookup(ctx, releases.CLI, version)
	if err != nil {
		return result, err
	}
	result.Available = release.Version()
	result.UpdateAvailable = releases.Newer(release.Version(), current)
	if check {
		return result, nil
	}
	if i.Owner == "desktop" || i.Owner == "package-manager" {
		return result, fmt.Errorf("%s", i.Instruction)
	}
	if current == release.Version() || (version == "" || version == "latest") && !result.UpdateAvailable {
		return result, nil
	}
	if i.Owner == "homebrew" {
		if version != "" && version != "latest" {
			return result, fmt.Errorf("Homebrew owns this installation and selects its packaged version; run `brew upgrade zn`")
		}
		cmd := exec.CommandContext(ctx, "brew", "upgrade", "zn")
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Run(); err != nil {
			return result, err
		}
		// Homebrew's versioned cellar path changes; use its stable opt path.
		parts := strings.Split(filepath.ToSlash(i.Executable), "/Cellar/zn/")
		if len(parts) != 2 {
			return result, fmt.Errorf("verify the upgraded version with `zn --version`")
		}
		err = probe(ctx, filepath.Join(parts[0], "opt", "zn", "bin", "zn"), release.Version())
	} else if i.Owner == "go" {
		cmd := exec.CommandContext(ctx, "go", "install", "github.com/ZenNotes/tui/cmd/zn@v"+release.Version())
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "GOBIN="+filepath.Dir(i.Executable))
		cmd.Stdout, cmd.Stderr = output, output
		if err = cmd.Run(); err == nil {
			err = probe(ctx, i.Executable, release.Version())
		}
	} else {
		if runtime.GOOS == "windows" {
			return result, fmt.Errorf("download %s and replace zn.exe after exiting: https://github.com/ZenNotes/tui/releases/tag/%s", release.Version(), release.Tag)
		}
		if _, err := releases.NormalizeVersion(current); err != nil {
			return result, fmt.Errorf("this is a development build; install a published release before using native self-update")
		}
		lock, lockErr := os.OpenFile(i.Executable+".update-lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if lockErr != nil {
			return result, fmt.Errorf("cannot lock installation: %w", lockErr)
		}
		lock.Close()
		defer os.Remove(i.Executable + ".update-lock")
		candidate, stageErr := c.Stage(ctx, releases.CLI, release, runtime.GOOS, runtime.GOARCH, filepath.Dir(i.Executable))
		if stageErr != nil {
			return result, stageErr
		}
		defer os.Remove(candidate)
		err = replace(ctx, i.Executable, candidate, release.Version(), probe)
	}
	if err != nil {
		return result, err
	}
	result.Updated = true
	return result, nil
}

func probe(ctx context.Context, candidate, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, candidate, "--desktop-integration")
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("candidate executable failed its integration probe: %w", err)
	}
	var info struct {
		Protocol int    `json:"protocol"`
		Version  string `json:"version"`
	}
	if err := json.Unmarshal(output.Bytes(), &info); err != nil || info.Protocol != 1 || strings.TrimPrefix(info.Version, "v") != version {
		return fmt.Errorf("candidate protocol or version did not match release %s", version)
	}
	return nil
}

func replace(ctx context.Context, executable, candidate, version string, check func(context.Context, string, string) error) error {
	if err := check(ctx, candidate, version); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("installed executable is not a regular file")
	}
	old, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer old.Close()
	backup, err := os.CreateTemp(filepath.Dir(executable), ".zn-backup-*")
	if err != nil {
		return err
	}
	defer backup.Close()
	defer os.Remove(backup.Name())
	if _, err := io.Copy(backup, old); err != nil {
		return err
	}
	if err := old.Close(); err != nil {
		return err
	}
	if err := backup.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := backup.Sync(); err != nil {
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	if err := os.Chmod(candidate, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(backup.Name(), executable+".previous"); err != nil {
		return err
	}
	return os.Rename(candidate, executable)
}
