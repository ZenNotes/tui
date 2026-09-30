// Package server owns locally managed znserver instances. It is shared by the
// command line and terminal app; neither frontend runs lifecycle shell snippets.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/releases"
)

type ReleaseSource interface {
	Lookup(context.Context, releases.Product, string) (releases.Release, error)
	Stage(context.Context, releases.Product, releases.Release, string, string, string) (string, error)
}

type Manager struct {
	Root          string
	Releases      ReleaseSource
	Service       Service
	Check         func(context.Context, Instance) error
	HealthTimeout time.Duration
}

type Instance struct {
	Name            string `json:"name"`
	Vault           string `json:"vault"`
	Bind            string `json:"bind"`
	BasePath        string `json:"basePath,omitempty"`
	Version         string `json:"version"`
	SHA256          string `json:"sha256"`
	PreviousVersion string `json:"previousVersion,omitempty"`
	PreviousSHA256  string `json:"previousSha256,omitempty"`
}

type Options struct {
	Name, Vault, Bind, BasePath, Version string
	NoStart, NoDefault                   bool
}

func NewManager(root string) *Manager {
	if abs, err := canonicalPath(root); err == nil {
		root = abs
	}
	m := &Manager{Root: root, Releases: releases.NewClient(), Service: nativeService{}, HealthTimeout: 15 * time.Second}
	m.Check = m.checkServer
	return m
}

func DefaultManager() *Manager {
	return NewManager(filepath.Join(config.UserDataDir(), "servers"))
}

func (m *Manager) Dir(name string) string       { return filepath.Join(m.Root, name) }
func (m *Manager) TokenPath(name string) string { return filepath.Join(m.Dir(name), "token") }
func (m *Manager) LogPath(name string) string   { return filepath.Join(m.Dir(name), "server.log") }
func (m *Manager) BinaryPath(i Instance) string {
	name := "zennotes-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(m.Dir(i.Name), "versions", i.Version, name)
}

func (i Instance) URL() string {
	host, port, _ := net.SplitHostPort(i.Bind)
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + i.BasePath
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func checkName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("server name must start with a lowercase letter and contain at most 48 letters, digits, hyphens or underscores")
	}
	return nil
}

func canonicalPath(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("path contains a control character")
	}
	abs, err := filepath.Abs(config.ExpandHome(value))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	resolved, err := canonicalPath(parent)
	return filepath.Join(resolved, filepath.Base(abs)), err
}

func contains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *Manager) validate(i Instance) (Instance, error) {
	if err := checkName(i.Name); err != nil {
		return i, err
	}
	if i.Vault == "" {
		return i, fmt.Errorf("a vault folder is required; pass --vault <folder>")
	}
	root, err := canonicalPath(m.Root)
	if err != nil {
		return i, err
	}
	i.Vault, err = canonicalPath(i.Vault)
	if err != nil {
		return i, err
	}
	if contains(i.Vault, root) || contains(root, i.Vault) {
		return i, fmt.Errorf("the vault and managed server directory must be separate (tokens and binaries stay outside the vault)")
	}
	credentials, err := canonicalPath(config.CredentialsPath())
	if err != nil || contains(i.Vault, credentials) {
		return i, fmt.Errorf("the client credentials file must remain outside the served vault")
	}
	if info, err := os.Stat(i.Vault); err == nil && !info.IsDir() {
		return i, fmt.Errorf("vault is not a directory")
	} else if err != nil && !os.IsNotExist(err) {
		return i, err
	}
	host, port, err := net.SplitHostPort(i.Bind)
	n, numErr := strconv.Atoi(port)
	if err != nil || numErr != nil || n < 1 || n > 65535 || host != "localhost" && net.ParseIP(host) == nil {
		return i, fmt.Errorf("bind must be an IP address and port, e.g. 127.0.0.1:7878")
	}
	if i.BasePath != "" && (path.Clean(i.BasePath) != i.BasePath || !strings.HasPrefix(i.BasePath, "/") || strings.ContainsAny(i.BasePath, "?#\\\r\n\x00% ")) {
		return i, fmt.Errorf("base path must be empty or a clean URL path such as /notes")
	}
	if i.BasePath == "/" {
		i.BasePath = ""
	}
	if i.Version != "" {
		i.Version, err = releases.NormalizeVersion(i.Version)
		if err != nil {
			return i, err
		}
	}
	if i.PreviousVersion != "" {
		i.PreviousVersion, err = releases.NormalizeVersion(i.PreviousVersion)
	}
	return i, err
}

func (m *Manager) Load(name string) (Instance, error) {
	var i Instance
	if err := checkName(name); err != nil {
		return i, err
	}
	if info, err := os.Lstat(m.Dir(name)); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return i, fmt.Errorf("managed instance directory cannot be a symlink")
	}
	raw, err := os.ReadFile(filepath.Join(m.Dir(name), "instance.json"))
	if err != nil {
		return i, err
	}
	if err := json.Unmarshal(raw, &i); err != nil {
		return i, fmt.Errorf("invalid instance configuration: %w", err)
	}
	if i.Name != name || i.Version == "" {
		return i, fmt.Errorf("invalid instance identity or version")
	}
	return m.validate(i)
}

func (m *Manager) List() ([]Instance, error) {
	items := []Instance{}
	entries, err := os.ReadDir(m.Root)
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validName.MatchString(entry.Name()) {
			continue
		}
		i, err := m.Load(entry.Name())
		if os.IsNotExist(err) {
			continue // An interrupted first install can leave an empty directory.
		}
		if err != nil {
			return nil, fmt.Errorf("server %s: %w", entry.Name(), err)
		}
		items = append(items, i)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func writeJSON(target string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(target, append(raw, '\n'), 0o600)
}

func writeAtomic(target string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
