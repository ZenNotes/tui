package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/releases"
)

func (m *Manager) Setup(ctx context.Context, opts Options) (Instance, error) {
	i, err := m.Install(ctx, opts)
	if err != nil || opts.NoStart {
		return i, err
	}
	if err := m.Start(ctx, i.Name); err != nil {
		return i, err
	}
	return i, m.Register(i, !opts.NoDefault)
}

// Register persists a terminal profile only after authenticated verification.
func (m *Manager) Register(i Instance, makeDefault bool) error {
	token, err := m.token(i.Name)
	if err != nil {
		return err
	}
	ws := config.LoadWorkspaces()
	for _, s := range ws.Servers {
		if strings.EqualFold(s.Name, i.Name) && s.URL != i.URL() {
			return fmt.Errorf("workspace %q already points to %s; choose a different managed server name", i.Name, s.URL)
		}
	}
	entry := ws.AddServer(i.Name, i.URL())
	if makeDefault {
		ws.Default = entry.Name
	}
	if err := config.SaveToken(i.URL(), token); err != nil {
		return err
	}
	return config.SaveWorkspaces(ws)
}

func (m *Manager) Start(ctx context.Context, name string) error {
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	i, err := m.Load(name)
	if err != nil {
		return err
	}
	if err := m.verifyBinary(i); err != nil {
		return err
	}
	if _, err := m.token(name); err != nil {
		return err
	}
	active, err := m.Service.Active(ctx, m.spec(i))
	if err != nil {
		return err
	}
	if active {
		return m.waitHealthy(ctx, i)
	}
	if err := m.saveHost(i); err != nil {
		return err
	}
	err = m.Service.Start(ctx, m.spec(i))
	if err == nil {
		err = m.waitHealthy(ctx, i)
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return errors.Join(err, m.Service.Stop(cleanup, m.spec(i)))
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context, name string) error {
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	i, err := m.Load(name)
	if err != nil {
		return err
	}
	return m.Service.Stop(ctx, m.spec(i))
}

type Status struct {
	Instance
	URL     string `json:"url"`
	Active  bool   `json:"active"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

func (m *Manager) Status(ctx context.Context, name string) (Status, error) {
	i, err := m.Load(name)
	if err != nil {
		return Status{}, err
	}
	s := Status{Instance: i, URL: i.URL()}
	s.Active, err = m.Service.Active(ctx, m.spec(i))
	if err != nil {
		s.Error = err.Error()
		return s, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := m.Check(checkCtx, i); err != nil {
		s.Error = err.Error()
	} else {
		s.Healthy = true
	}
	return s, nil
}

func (m *Manager) Update(ctx context.Context, name, version string) (Instance, error) {
	unlock, err := m.lock(name)
	if err != nil {
		return Instance{}, err
	}
	defer unlock()
	old, err := m.Load(name)
	if err != nil {
		return old, err
	}
	release, err := m.Releases.Lookup(ctx, releases.Server, version)
	if err != nil {
		return old, err
	}
	if release.Version() == old.Version || (version == "" || version == "latest") && !releases.Newer(release.Version(), old.Version) {
		return old, m.verifyBinary(old)
	}
	next := old
	next.Version = release.Version()
	next.PreviousVersion, next.PreviousSHA256 = old.Version, old.SHA256
	next.SHA256, err = m.installVersion(ctx, next, release)
	if err != nil {
		return old, err
	}
	return m.activate(ctx, old, next)
}

func (m *Manager) Rollback(ctx context.Context, name string) (Instance, error) {
	unlock, err := m.lock(name)
	if err != nil {
		return Instance{}, err
	}
	defer unlock()
	old, err := m.Load(name)
	if err != nil {
		return old, err
	}
	if old.PreviousVersion == "" {
		return old, fmt.Errorf("server has no previous version to restore")
	}
	next := old
	next.Version, next.PreviousVersion = old.PreviousVersion, old.Version
	next.SHA256, next.PreviousSHA256 = old.PreviousSHA256, old.SHA256
	return m.activate(ctx, old, next)
}

// Configure changes runtime settings transactionally, retaining the same token.
func (m *Manager) Configure(ctx context.Context, name string, changes map[string]string) (Instance, error) {
	unlock, err := m.lock(name)
	if err != nil {
		return Instance{}, err
	}
	defer unlock()
	old, err := m.Load(name)
	if err != nil {
		return old, err
	}
	next := old
	for key, value := range changes {
		switch key {
		case "vault":
			next.Vault = value
		case "bind":
			next.Bind = value
		case "base-path":
			next.BasePath = value
		default:
			return old, fmt.Errorf("unsupported server setting %q", key)
		}
	}
	next, err = m.validate(next)
	if err != nil {
		return old, err
	}
	if err := os.MkdirAll(next.Vault, 0o700); err != nil {
		return old, err
	}
	result, err := m.activate(ctx, old, next)
	if err != nil {
		return result, err
	}
	if err := m.updateProfiles(old, next); err != nil {
		recovery, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		restored, restoreErr := m.activate(recovery, next, old)
		return restored, errors.Join(fmt.Errorf("could not update client profiles: %w", err), restoreErr)
	}
	return result, nil
}

func (m *Manager) updateProfiles(old, next Instance) error {
	// Preserve profile names and defaults when the URL changes.
	ws := config.LoadWorkspaces()
	changed := false
	for index, s := range ws.Servers {
		if s.URL == old.URL() {
			token, err := m.token(old.Name)
			if err != nil {
				return err
			}
			if err := config.SaveToken(next.URL(), token); err != nil {
				return err
			}
			ws.Servers[index].URL = next.URL()
			changed = true
		}
	}
	if changed {
		return config.SaveWorkspaces(ws)
	}
	return nil
}

// activate keeps the old manifest until the candidate has passed all checks.
// Recovery gets its own context so Ctrl-C cannot interrupt the rollback.
func (m *Manager) activate(ctx context.Context, old, next Instance) (result Instance, err error) {
	if err := m.verifyBinary(old); err != nil {
		return old, err
	}
	if err := m.verifyBinary(next); err != nil {
		return old, err
	}
	active, err := m.Service.Active(ctx, m.spec(old))
	if err != nil {
		return old, err
	}
	if err := m.Service.Stop(ctx, m.spec(old)); err != nil {
		return old, err
	}
	defer func() {
		if err == nil {
			return
		}
		recovery, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		restoreErr := m.Service.Stop(recovery, m.spec(next))
		restoreErr = errors.Join(restoreErr, m.saveHost(old))
		if restoreErr == nil && active {
			restoreErr = m.Service.Start(recovery, m.spec(old))
			if restoreErr == nil {
				restoreErr = m.waitHealthy(recovery, old)
			}
		}
		if restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("automatic rollback needs attention: %w", restoreErr))
		} else {
			err = fmt.Errorf("change rejected; restored version %s: %w", old.Version, err)
		}
	}()
	if err := m.saveHost(next); err != nil {
		return old, err
	}
	if err := m.Service.Start(ctx, m.spec(next)); err != nil {
		return old, err
	}
	if err := m.waitHealthy(ctx, next); err != nil {
		return old, err
	}
	if !active {
		if err := m.Service.Stop(ctx, m.spec(next)); err != nil {
			return old, err
		}
	}
	if err := m.save(next); err != nil {
		return old, err
	}
	return next, nil
}

// Run is a foreground development runtime. Holding the instance lock prevents
// another command from replacing its executable or configuration while it runs.
func (m *Manager) Run(ctx context.Context, name string, output io.Writer) error {
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	i, err := m.Load(name)
	if err != nil {
		return err
	}
	if err := m.verifyBinary(i); err != nil {
		return err
	}
	if _, err := m.token(name); err != nil {
		return err
	}
	spec := m.spec(i)
	if active, err := m.Service.Active(ctx, spec); err != nil || active {
		return errors.Join(err, fmt.Errorf("stop the background service before running in the foreground"))
	}
	if err := m.saveHost(i); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, spec.Executable)
	if runtime.GOOS != "windows" {
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 15 * time.Second
	}
	cmd.Dir = spec.Dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "ZENNOTES_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, k := range envKeys(spec) {
		cmd.Env = append(cmd.Env, k+"="+spec.Env[k])
	}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			// Ctrl-C: the operator stopped the server, which has already
			// logged its shutdown; that is not an error to report twice.
			return nil
		}
		return err
	}
	return nil
}

func (m *Manager) Logs(name string, output io.Writer) error {
	if _, err := m.Load(name); err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(m.Dir(name), "server.log"))
	if err != nil {
		return err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > 64<<10 {
		if _, err := f.Seek(-64<<10, io.SeekEnd); err != nil {
			return err
		}
	}
	_, err = io.Copy(output, io.LimitReader(f, 64<<10))
	return err
}
