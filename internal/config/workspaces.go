package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// zn keeps its own list of vaults and servers next to config.toml, so a
// terminal user connects once and never has to open the desktop app. The
// desktop's zennotes.config.json is still read and never written; when zn
// has a default of its own, that default wins over the app's workspace.
// Server tokens live apart from the list, in a file only the user can read.

// LocalWorkspace is a vault directory zn remembers.
type LocalWorkspace struct {
	Name string `toml:"name"`
	Root string `toml:"root"`
}

// ServerWorkspace is a ZenNotes server zn remembers; its token is stored
// separately, keyed by URL.
type ServerWorkspace struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

// Workspaces is the content of workspaces.toml.
type Workspaces struct {
	// Default names the workspace zn uses when no flag or environment
	// variable picks one; empty means follow the desktop app.
	Default string            `toml:"default"`
	Vaults  []LocalWorkspace  `toml:"vault"`
	Servers []ServerWorkspace `toml:"server"`
}

// WorkspacesPath is where zn's vault and server list lives.
func WorkspacesPath() string { return filepath.Join(PortableConfigDir(), "workspaces.toml") }

// CredentialsPath is where server tokens live (mode 0600).
func CredentialsPath() string { return filepath.Join(PortableConfigDir(), "credentials.toml") }

// LoadWorkspaces reads the list; a missing file is an empty list.
func LoadWorkspaces() Workspaces {
	var ws Workspaces
	raw, err := os.ReadFile(WorkspacesPath())
	if err != nil {
		return ws
	}
	_, _ = toml.Decode(string(raw), &ws)
	return ws
}

// SaveWorkspaces writes the list atomically.
func SaveWorkspaces(ws Workspaces) error {
	var buf bytes.Buffer
	buf.WriteString("# Vaults and servers zn knows. `zn connect`, `zn vault add` and `zn use` edit it.\n")
	if err := toml.NewEncoder(&buf).Encode(ws); err != nil {
		return err
	}
	return writeFileAtomic(WorkspacesPath(), buf.Bytes(), 0o644)
}

type credentials struct {
	Tokens map[string]string `toml:"tokens"`
}

func loadCredentials() credentials {
	c := credentials{Tokens: map[string]string{}}
	raw, err := os.ReadFile(CredentialsPath())
	if err != nil {
		return c
	}
	_, _ = toml.Decode(string(raw), &c)
	if c.Tokens == nil {
		c.Tokens = map[string]string{}
	}
	return c
}

func saveCredentials(c credentials) error {
	var buf bytes.Buffer
	buf.WriteString("# Server tokens for zn, keyed by URL. Keep this file private.\n")
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	return writeFileAtomic(CredentialsPath(), buf.Bytes(), 0o600)
}

// LoadToken is the stored token for a server URL, or "".
func LoadToken(baseURL string) string {
	return strings.TrimSpace(loadCredentials().Tokens[tokenKey(baseURL)])
}

// SaveToken stores a server token, replacing any previous one.
func SaveToken(baseURL, token string) error {
	c := loadCredentials()
	c.Tokens[tokenKey(baseURL)] = strings.TrimSpace(token)
	return saveCredentials(c)
}

// DeleteToken forgets a server token.
func DeleteToken(baseURL string) error {
	c := loadCredentials()
	if _, ok := c.Tokens[tokenKey(baseURL)]; !ok {
		return nil
	}
	delete(c.Tokens, tokenKey(baseURL))
	return saveCredentials(c)
}

func tokenKey(baseURL string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// FindVault matches a saved vault by name (case-insensitive) or root.
func (ws Workspaces) FindVault(selector string) *LocalWorkspace {
	needle := strings.TrimSpace(selector)
	if needle == "" {
		return nil
	}
	abs, _ := filepath.Abs(ExpandHome(needle))
	for i := range ws.Vaults {
		v := &ws.Vaults[i]
		if strings.EqualFold(v.Name, needle) || (abs != "" && filepath.Clean(v.Root) == filepath.Clean(abs)) {
			return v
		}
	}
	return nil
}

// FindServer matches a saved server by name (case-insensitive) or URL.
func (ws Workspaces) FindServer(selector string) *ServerWorkspace {
	needle := strings.TrimSpace(selector)
	if needle == "" {
		return nil
	}
	for i := range ws.Servers {
		s := &ws.Servers[i]
		if strings.EqualFold(s.Name, needle) || tokenKey(s.URL) == tokenKey(needle) || tokenKey(s.URL) == tokenKey("http://"+needle) || tokenKey(s.URL) == tokenKey("https://"+needle) {
			return s
		}
		// A bare host names the server too: `zn use notes.example.com`.
		if u, err := url.Parse(s.URL); err == nil && (strings.EqualFold(u.Host, needle) || strings.EqualFold(u.Hostname(), needle)) {
			return s
		}
	}
	return nil
}

// Names lists every saved workspace name, vaults first.
func (ws Workspaces) Names() []string {
	out := []string{}
	for _, v := range ws.Vaults {
		out = append(out, v.Name)
	}
	for _, s := range ws.Servers {
		out = append(out, s.Name)
	}
	return out
}

// UniqueName appends 2, 3… to a name until no saved workspace carries it.
func (ws Workspaces) UniqueName(base string) string {
	taken := map[string]bool{}
	for _, n := range ws.Names() {
		taken[strings.ToLower(n)] = true
	}
	if !taken[strings.ToLower(base)] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// AddVault remembers a vault directory; the same root replaces its entry.
func (ws *Workspaces) AddVault(name, root string) LocalWorkspace {
	abs, err := filepath.Abs(ExpandHome(root))
	if err == nil {
		root = abs
	}
	name = strings.TrimSpace(name)
	for i := range ws.Vaults {
		if filepath.Clean(ws.Vaults[i].Root) == filepath.Clean(root) {
			if name != "" {
				ws.rename(&ws.Vaults[i].Name, name)
			}
			return ws.Vaults[i]
		}
	}
	if name == "" {
		name = filepath.Base(root)
	}
	entry := LocalWorkspace{Name: ws.UniqueName(name), Root: root}
	ws.Vaults = append(ws.Vaults, entry)
	return entry
}

// AddServer remembers a server; the same URL replaces its entry.
func (ws *Workspaces) AddServer(name, baseURL string) ServerWorkspace {
	name = strings.TrimSpace(name)
	for i := range ws.Servers {
		if tokenKey(ws.Servers[i].URL) == tokenKey(baseURL) {
			if name != "" {
				ws.rename(&ws.Servers[i].Name, name)
			}
			return ws.Servers[i]
		}
	}
	if name == "" {
		name = DefaultServerName(baseURL)
	}
	entry := ServerWorkspace{Name: ws.UniqueName(name), URL: strings.TrimRight(baseURL, "/")}
	ws.Servers = append(ws.Servers, entry)
	return entry
}

// rename changes an entry's name and keeps the default pointing at it.
func (ws *Workspaces) rename(field *string, name string) {
	if strings.EqualFold(ws.Default, *field) {
		ws.Default = name
	}
	*field = name
}

// Remove forgets a workspace by name; it reports the kind removed.
func (ws *Workspaces) Remove(name string) (string, bool) {
	for i := range ws.Vaults {
		if strings.EqualFold(ws.Vaults[i].Name, name) {
			ws.Vaults = append(ws.Vaults[:i], ws.Vaults[i+1:]...)
			if strings.EqualFold(ws.Default, name) {
				ws.Default = ""
			}
			return "local", true
		}
	}
	for i := range ws.Servers {
		if strings.EqualFold(ws.Servers[i].Name, name) {
			ws.Servers = append(ws.Servers[:i], ws.Servers[i+1:]...)
			if strings.EqualFold(ws.Default, name) {
				ws.Default = ""
			}
			return "remote", true
		}
	}
	return "", false
}

// DefaultServerName names a server after its host: notes.example.com,
// or localhost for a local address.
func DefaultServerName(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Hostname() == "" {
		return "server"
	}
	return u.Hostname()
}

// ErrNoWorkspace says a name matches nothing zn saved.
var ErrNoWorkspace = errors.New("no saved vault or server has that name")

// SortedVaults returns the vaults by name, for stable listings.
func (ws Workspaces) SortedVaults() []LocalWorkspace {
	out := append([]LocalWorkspace{}, ws.Vaults...)
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
