package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/selfupdate"
	"github.com/ZenNotes/tui/internal/server"
)

type workspaceStatus struct {
	Version           string       `json:"version"`
	Executable        string       `json:"executable"`
	Platform          string       `json:"platform"`
	ConfigPath        string       `json:"configPath"`
	Source            string       `json:"workspaceSource"`
	SelectedBy        string       `json:"selectedBy"`
	Kind              backend.Kind `json:"kind,omitempty"`
	Name              string       `json:"name,omitempty"`
	Location          string       `json:"location,omitempty"`
	AuthConfigured    bool         `json:"authConfigured"`
	Problem           string       `json:"problem,omitempty"`
	InstallationOwner string       `json:"installationOwner,omitempty"`
}

func currentStatus(args Args) (workspaceStatus, error) {
	source, err := backend.ResolveWorkspaceSource(args.Str("workspace-source"))
	if err != nil {
		return workspaceStatus{}, err
	}
	exe, _ := os.Executable()
	st := workspaceStatus{Version: Version, Executable: exe, Platform: runtime.GOOS + "/" + runtime.GOARCH, ConfigPath: config.ConfigTomlPath(), Source: source}
	if install, err := selfupdate.Current(); err == nil {
		st.InstallationOwner = install.Owner
	}
	switch {
	case args.Str("server") != "":
		st.SelectedBy = "--server"
	case args.Str("vault") != "":
		st.SelectedBy = "--vault"
	case os.Getenv("ZENNOTES_SERVER") != "":
		st.SelectedBy = "ZENNOTES_SERVER"
	case os.Getenv("ZENNOTES_VAULT") != "":
		st.SelectedBy = "ZENNOTES_VAULT"
	case source == "terminal" && config.LoadWorkspaces().Default != "":
		st.SelectedBy = "terminal default"
	default:
		st.SelectedBy = "desktop workspace"
	}
	target, err := ResolveTargetFromArgs(args)
	if err != nil {
		st.Problem = err.Error()
		return st, nil
	}
	st.Kind, st.Name, st.Location = target.Kind, target.Name, target.Root
	if target.Kind == backend.KindRemote {
		st.Location = redactedURL(target.BaseURL)
	}
	st.AuthConfigured = target.AuthToken != ""
	return st, nil
}

func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid server URL"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

func cmdStatus(args Args) error {
	st, err := currentStatus(args)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(st)
		return nil
	}
	emitLine("zn " + st.Version + " (" + st.Platform + ")")
	emitLine("Executable: " + st.Executable)
	emitLine("Installation owner: " + st.InstallationOwner)
	emitLine("Config: " + st.ConfigPath)
	emitLine("Workspace source: " + st.Source + " · selected by " + st.SelectedBy)
	if st.Problem != "" {
		emitLine(st.Problem)
	} else {
		emitLine(fmt.Sprintf("Workspace: %s %s (%s)", st.Name, st.Location, st.Kind))
		if st.Kind == backend.KindRemote {
			emitLine(fmt.Sprintf("Credentials configured: %t", st.AuthConfigured))
		}
	}
	return nil
}

type diagnosticCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func cmdDoctor(ctx context.Context, args Args) error {
	checks := []diagnosticCheck{}
	add := func(name, good string, err error) {
		message := good
		if err != nil {
			message = err.Error()
		}
		checks = append(checks, diagnosticCheck{Name: name, OK: err == nil, Message: message})
	}
	add("configuration", config.ConfigTomlPath(), config.ValidateConfig())
	if installation, err := selfupdate.Current(); err == nil {
		add("installation", installation.Owner+": "+installation.Instruction, nil)
	}
	if editor := strings.TrimSpace(os.Getenv("VISUAL")); editor != "" {
		_, err := exec.LookPath(strings.Fields(editor)[0])
		add("editor", "VISUAL is available", err)
	} else if editor = strings.TrimSpace(os.Getenv("EDITOR")); editor != "" {
		_, err := exec.LookPath(strings.Fields(editor)[0])
		add("editor", "EDITOR is available", err)
	} else {
		add("editor", "VISUAL/EDITOR unset; config edit uses the platform fallback", nil)
	}
	if info, err := os.Stat(config.CredentialsPath()); err == nil {
		var permissionErr error
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			permissionErr = fmt.Errorf("credentials.toml must be private (mode 0600)")
		}
		add("credentials", "Stored credentials are private", permissionErr)
	}
	target, err := ResolveTargetFromArgs(args)
	add("workspace", "Workspace selected", err)
	if err == nil {
		b, err := OpenBackend(target)
		if err == nil {
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err = b.Describe(probe)
			cancel()
		}
		add("access", "Workspace is accessible", err)
	}
	m := server.DefaultManager()
	instances, instanceErr := m.List()
	add("managed servers", fmt.Sprintf("%d local instances", len(instances)), instanceErr)
	for _, i := range instances {
		status, err := m.Status(ctx, i.Name)
		if err == nil && status.Active && !status.Healthy {
			err = fmt.Errorf("%s", status.Error)
		}
		message := "Stopped"
		if status.Healthy {
			message = "Authenticated and healthy"
		}
		add("server "+i.Name, message, err)
	}
	failed := 0
	for _, check := range checks {
		if !check.OK {
			failed++
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": failed == 0, "checks": checks})
	} else {
		for _, check := range checks {
			state := "OK"
			if !check.OK {
				state = "FAIL"
			}
			emitLine(fmt.Sprintf("%s  %s: %s", state, check.Name, check.Message))
		}
	}
	if failed != 0 {
		return fmt.Errorf("%d checks failed; resolve the reported issues and run `zn doctor` again", failed)
	}
	return nil
}
