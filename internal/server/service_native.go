package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type nativeService struct{}

func unitPath(s ServiceSpec) string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "LaunchAgents", s.Label+".plist")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", s.Label+".service")
}

func serviceCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func launchTarget(s ServiceSpec) string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), s.Label)
}

func (nativeService) Active(ctx context.Context, s ServiceSpec) (bool, error) {
	if _, err := os.Stat(unitPath(s)); os.IsNotExist(err) {
		return false, nil
	}
	var out []byte
	var err error
	switch runtime.GOOS {
	case "darwin":
		out, err = serviceCommand(ctx, "launchctl", "print", launchTarget(s))
		if err == nil {
			return bytes.Contains(out, []byte("state = running")), nil
		}
	case "linux":
		_, err = serviceCommand(ctx, "systemctl", "--user", "is-active", "--quiet", s.Label+".service")
		if err == nil {
			return true, nil
		}
	default:
		return false, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 4 || exit.ExitCode() == 113) {
		return false, nil
	}
	return false, err
}

func (n nativeService) Start(ctx context.Context, s ServiceSpec) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("background services require macOS or Linux; use `zn server run <name>` on this platform")
	}
	if err := n.Stop(ctx, s); err != nil {
		return err
	}
	content := systemdUnit(s)
	if runtime.GOOS == "darwin" {
		content = launchdUnit(s)
	}
	if err := writeAtomic(unitPath(s), []byte(content), 0o644); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		_, err := serviceCommand(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), unitPath(s))
		return err
	}
	if _, err := serviceCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	_, err := serviceCommand(ctx, "systemctl", "--user", "enable", "--now", s.Label+".service")
	return err
}

func (nativeService) Stop(ctx context.Context, s ServiceSpec) error {
	if runtime.GOOS == "darwin" {
		if _, err := serviceCommand(ctx, "launchctl", "print", launchTarget(s)); err == nil {
			if _, err := serviceCommand(ctx, "launchctl", "bootout", launchTarget(s)); err != nil {
				return err
			}
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 113 {
				return err
			}
		}
	} else if runtime.GOOS == "linux" {
		if _, err := os.Stat(unitPath(s)); os.IsNotExist(err) {
			return nil
		}
		if _, err := serviceCommand(ctx, "systemctl", "--user", "disable", "--now", s.Label+".service"); err != nil {
			return err
		}
	} else {
		return nil
	}
	if err := os.Remove(unitPath(s)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS == "linux" {
		_, err := serviceCommand(ctx, "systemctl", "--user", "daemon-reload")
		return err
	}
	return nil
}

func envKeys(s ServiceSpec) []string {
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func systemdUnit(s ServiceSpec) string {
	escape := func(value string) string { return strings.ReplaceAll(value, "%", "%%") }
	quote := func(value string) string { return strconv.Quote(escape(value)) }
	var b strings.Builder
	// WorkingDirectory and output destinations are scalar paths, unlike the
	// word-parsed ExecStart/Environment directives: quotes there are literal.
	fmt.Fprintf(&b, "[Unit]\nDescription=ZenNotes server\n\n[Service]\nType=exec\nExecStart=:%s\nWorkingDirectory=%s\n", quote(s.Executable), escape(s.Dir))
	for _, k := range envKeys(s) {
		fmt.Fprintf(&b, "Environment=%s\n", quote(k+"="+s.Env[k]))
	}
	fmt.Fprintf(&b, "Restart=on-failure\nRestartSec=3\nTimeoutStopSec=15\nUMask=0077\nStandardOutput=%s\nStandardError=%s\n\n[Install]\nWantedBy=default.target\n", "append:"+escape(s.LogPath), "append:"+escape(s.LogPath))
	return b.String()
}

func launchdUnit(s ServiceSpec) string {
	x := func(v string) string {
		var b bytes.Buffer
		xml.EscapeText(&b, []byte(v))
		return b.String()
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>`)
	fmt.Fprintf(&b, "<key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string></array><key>WorkingDirectory</key><string>%s</string>", x(s.Label), x(s.Executable), x(s.Dir))
	b.WriteString("<key>EnvironmentVariables</key><dict>")
	for _, k := range envKeys(s) {
		fmt.Fprintf(&b, "<key>%s</key><string>%s</string>", x(k), x(s.Env[k]))
	}
	fmt.Fprintf(&b, "</dict><key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict><key>ThrottleInterval</key><integer>3</integer><key>Umask</key><integer>63</integer><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string></dict></plist>\n", x(s.LogPath), x(s.LogPath))
	return b.String()
}
