package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/selfupdate"
	"github.com/ZenNotes/tui/internal/vim"
)

type versionView struct {
	textReader
	loading        bool
	copyWhenReady  bool
	cancel         context.CancelFunc
	writeClipboard func(string) error
}

type versionDetailsMsg struct {
	view *versionView
	text string
}

func (a *App) showVersion(cmd vim.ExCommand) error {
	if arg := strings.TrimSpace(cmd.Args); arg != "" && arg != "copy" {
		return fmt.Errorf("usage: :version [copy] or :version!")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
	v := &versionView{
		textReader:     textReader{title: "Version details", body: "Collecting version details…"},
		loading:        true,
		copyWhenReady:  cmd.Bang || strings.TrimSpace(cmd.Args) == "copy",
		cancel:         cancel,
		writeClipboard: clipboard.WriteAll,
	}
	a.overlay = v
	version, source, target := a.opts.Version, a.opts.WorkspaceSource, a.opts.Target
	a.queue(func() tea.Msg {
		defer cancel()
		return versionDetailsMsg{view: v, text: collectVersionDetails(ctx, version, source, target)}
	})
	return nil
}

func (a *App) finishVersionDetails(msg versionDetailsMsg) {
	if a.overlay != msg.view {
		return
	}
	msg.view.body, msg.view.loading = msg.text, false
	if msg.view.copyWhenReady {
		msg.view.copy(a)
	}
}

func (v *versionView) copy(a *App) {
	if v.loading {
		v.copyWhenReady = true
		return
	}
	if err := v.writeClipboard(v.body); err != nil {
		a.notifyError("Clipboard unavailable: " + err.Error())
		return
	}
	a.notify("Copied version details")
}

func (v *versionView) handleKey(a *App, k vim.Key) {
	if k.IsRune('c') || k.IsRune('y') {
		v.copy(a)
		return
	}
	if k.Is("esc") || k.IsCtrl('c') || k.IsRune('q') {
		v.cancel()
	}
	v.textReader.handleKey(a, k)
}

func (v *versionView) render(a *App, w, h int) string {
	return v.renderWithHint(a, w, h, "↑/↓ scroll · c copy details · Esc close")
}

func collectVersionDetails(ctx context.Context, version, source string, target backend.Target) string {
	if version == "" {
		version = "development"
	}
	lines := []string{
		"ZenNotes TUI " + versionValue(version),
		"OS: " + systemVersion(ctx),
		"Architecture: " + runtime.GOARCH,
		"Go: " + runtime.Version(),
		"Build: " + buildRevision(),
	}
	owner := "unknown"
	if installation, err := selfupdate.Current(); err == nil {
		owner = installation.Owner
	}
	lines = append(lines, "Installation: "+owner)
	terminal := []string{}
	for _, key := range []string{"TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TERM", "COLORTERM"} {
		if value := versionValue(os.Getenv(key)); value != "" {
			terminal = append(terminal, key+"="+value)
		}
	}
	if len(terminal) == 0 {
		terminal = append(terminal, "not reported")
	}
	lines = append(lines, "Terminal: "+strings.Join(terminal, "; "))
	if source == "" {
		source = "terminal"
	}
	lines = append(lines, "Workspace: "+versionValue(string(target.Kind)), "Workspace source: "+versionValue(source))
	if target.Kind == backend.KindRemote {
		address := versionServerURL(target.BaseURL)
		if address == "" {
			lines = append(lines, "Server: unavailable (invalid server URL)")
		} else {
			lines = append(lines, "Server: "+connectedServerVersion(ctx, address, target.AuthToken), "Server URL: "+address)
		}
	}
	return strings.Join(lines, "\n")
}

// Reports are intended for pasting into public issues. Never interpolate a
// backend label, request error, local path, or arbitrary server response body.
func versionServerURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment = nil, "", "", ""
	u.ForceQuery = false
	return strings.TrimRight(u.String(), "/")
}

func connectedServerVersion(ctx context.Context, address, token string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/api/version", nil)
	if err != nil {
		return "unavailable (invalid request)"
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return "unavailable (connection failed or request cancelled)"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Sprintf("unavailable (HTTP %d)", response.StatusCode)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return "unavailable (invalid version response)"
	}
	version := versionValue(payload.Version)
	if version == "" {
		return "not reported"
	}
	return version
}

func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "not embedded"
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = versionValue(setting.Value)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "not embedded"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += " (modified)"
	}
	return revision
}

func versionValue(raw string) string {
	value := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, stripAnsi(raw))
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return value
}

func systemVersion(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		return strings.TrimSpace("macOS " + versionCommand(ctx, "/usr/bin/sw_vers", "-productVersion"))
	case "linux":
		name := "Linux"
		if f, err := os.Open("/etc/os-release"); err == nil {
			data, _ := io.ReadAll(io.LimitReader(f, 64<<10))
			f.Close()
			if pretty := linuxDistribution(string(data)); pretty != "" {
				name = pretty
			}
		}
		if kernel := versionCommand(ctx, "uname", "-r"); kernel != "" {
			name += " (kernel " + kernel + ")"
		}
		return name
	case "windows":
		if version := versionCommand(ctx, "cmd", "/c", "ver"); version != "" {
			return version
		}
		return "Windows"
	default:
		return runtime.GOOS
	}
}

func versionCommand(ctx context.Context, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return versionValue(string(output))
}

func linuxDistribution(data string) string {
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key != "PRETTY_NAME" {
			continue
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, "\"'")
		}
		return versionValue(value)
	}
	return ""
}
