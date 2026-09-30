package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ZenNotes/tui/internal/releases"
)

func (m *Manager) lock(name string) (func(), error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.Dir(name), 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(m.Dir(name))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("managed instance directory must be a regular directory")
	}
	path := filepath.Join(m.Dir(name), ".lock")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot lock server %s: %w; if a previous operation was interrupted, remove %s", name, err, path)
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { os.Remove(path) }, nil
}

// Install downloads a verified binary and creates private runtime configuration.
// Repeating the same installation preserves its version, credentials and vault.
func (m *Manager) Install(ctx context.Context, opts Options) (Instance, error) {
	i := Instance{Name: opts.Name, Vault: opts.Vault, Bind: opts.Bind, BasePath: opts.BasePath}
	old, err := m.Load(opts.Name)
	if err != nil && !os.IsNotExist(err) {
		return i, err
	}
	if err == nil {
		if i.Vault == "" {
			i.Vault = old.Vault
		}
		if i.Bind == "" {
			i.Bind = old.Bind
		}
		if i.BasePath == "" {
			i.BasePath = old.BasePath
		}
	}
	if i.Bind == "" {
		i.Bind = "127.0.0.1:7878"
	}
	i, err = m.validate(i)
	if err != nil {
		return i, err
	}
	if opts.Version != "" && opts.Version != "latest" {
		version, err := releases.NormalizeVersion(opts.Version)
		if err != nil {
			return i, err
		}
		if old.Version != "" && old.Version != version {
			return i, fmt.Errorf("server already uses %s; change versions with `zn server update %s --version %s`", old.Version, i.Name, version)
		}
	}
	if old.Version != "" {
		if old.Vault != i.Vault || old.Bind != i.Bind || old.BasePath != i.BasePath {
			return i, fmt.Errorf("server already exists with different settings; use `zn server config %s`", i.Name)
		}
		if _, err := m.token(old.Name); err != nil {
			return old, err
		}
		return old, m.verifyBinary(old)
	}
	unlock, err := m.lock(i.Name)
	if err != nil {
		return i, err
	}
	defer unlock()
	if _, err := m.Load(i.Name); err == nil {
		return i, fmt.Errorf("server was installed concurrently; repeat the command")
	}
	release, err := m.Releases.Lookup(ctx, releases.Server, opts.Version)
	if err != nil {
		return i, err
	}
	i.Version = release.Version()
	i.SHA256, err = m.installVersion(ctx, i, release)
	if err != nil {
		return i, err
	}
	// A fresh install generates one token. Retrying after a later failure keeps it.
	if _, err := os.Stat(m.TokenPath(i.Name)); os.IsNotExist(err) {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return i, err
		}
		if err := writeAtomic(m.TokenPath(i.Name), []byte(hex.EncodeToString(random[:])+"\n"), 0o600); err != nil {
			return i, err
		}
	} else if err != nil {
		return i, err
	}
	if err := os.MkdirAll(i.Vault, 0o700); err != nil {
		return i, err
	}
	if err := m.saveHost(i); err != nil {
		return i, err
	}
	return i, m.save(i)
}

func (m *Manager) installVersion(ctx context.Context, i Instance, release releases.Release) (string, error) {
	version, err := releases.NormalizeVersion(release.Tag)
	if err != nil {
		return "", err
	}
	i.Version = version
	bin := m.BinaryPath(i)
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		return "", err
	}
	staged, err := m.Releases.Stage(ctx, releases.Server, release, runtime.GOOS, runtime.GOARCH, filepath.Dir(bin))
	if err != nil {
		return "", err
	}
	defer os.Remove(staged)
	digest, err := fileDigest(staged)
	if err != nil {
		return "", err
	}
	if previous, err := fileDigest(bin); err == nil {
		if previous != digest {
			return "", fmt.Errorf("release %s differs from the retained executable; refusing to overwrite it", version)
		}
		return previous, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(staged, bin); err != nil {
		return "", err
	}
	return digest, nil
}

func (m *Manager) save(i Instance) error {
	return writeJSON(filepath.Join(m.Dir(i.Name), "instance.json"), i)
}

func (m *Manager) saveHost(i Instance) error {
	return writeJSON(filepath.Join(m.Dir(i.Name), "server.json"), map[string]string{
		"vaultPath": i.Vault, "bind": i.Bind, "basePath": i.BasePath,
	})
}
