// Package backend is the seam that lets every zn command and the terminal UI
// work against a vault on this machine or one behind a self-hosted ZenNotes
// server: the same operations, bound either to a root on disk or to a server
// over HTTP.
package backend

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/remote"
)

// Kind says where a vault lives.
type Kind string

const (
	KindLocal  Kind = "local"
	KindRemote Kind = "remote"
)

// Target is which vault a command is about.
type Target struct {
	Kind Kind
	// Root is the local vault directory.
	Root string
	// Name is the saved server profile's name, "" for a bare URL.
	Name      string
	BaseURL   string
	AuthToken string
}

// RemoteTokenEnv carries a server token for CI and headless use.
const RemoteTokenEnv = "ZENNOTES_REMOTE_TOKEN"

// ResolveAuthToken picks the token, loudest first: an explicit `--token`,
// then the environment, then whatever the saved profile carries.
func ResolveAuthToken(flagToken, profileToken string) string {
	return ResolveAuthTokenFor("", flagToken, profileToken)
}

// ResolveAuthTokenFor is ResolveAuthToken with zn's own credential store in
// the chain: flag, environment, the token `zn connect` saved for the URL,
// then the desktop profile's token.
func ResolveAuthTokenFor(baseURL, flagToken, profileToken string) string {
	if explicit := strings.TrimSpace(flagToken); explicit != "" {
		return explicit
	}
	if fromEnv := strings.TrimSpace(os.Getenv(RemoteTokenEnv)); fromEnv != "" {
		return fromEnv
	}
	if baseURL != "" {
		if stored := config.LoadToken(baseURL); stored != "" {
			return stored
		}
	}
	return strings.TrimSpace(profileToken)
}

// TargetForWorkspace resolves one of zn's own saved workspaces by name.
func TargetForWorkspace(ws config.Workspaces, name, flagToken string) (Target, bool) {
	if v := ws.FindVault(name); v != nil {
		return Target{Kind: KindLocal, Root: v.Root}, true
	}
	if s := ws.FindServer(name); s != nil {
		return Target{Kind: KindRemote, Name: s.Name, BaseURL: s.URL, AuthToken: ResolveAuthTokenFor(s.URL, flagToken, "")}, true
	}
	return Target{}, false
}

var hostPortRe = regexp.MustCompile(`^[\w.-]+:\d+(/|$)`)
var httpSchemeRe = regexp.MustCompile(`(?i)^https?://`)

// LooksLikeServerURL is true for a bare URL rather than a profile name: it
// has a scheme, or looks like `host:port`.
func LooksLikeServerURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	return httpSchemeRe.MatchString(trimmed) || hostPortRe.MatchString(trimmed)
}

func findProfile(profiles []config.RemoteProfile, selector string) *config.RemoteProfile {
	needle := strings.ToLower(strings.TrimSpace(selector))
	for i := range profiles {
		if strings.ToLower(profiles[i].Name) == needle {
			return &profiles[i]
		}
	}
	return nil
}

// ResolveServerTarget resolves `--server <name|url>`.
func ResolveServerTarget(selector, flagToken string) (Target, error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" || trimmed == "true" {
		return Target{}, errors.New("--server needs a server name or URL, e.g. `--server home` or `--server localhost:7878`.")
	}
	ws := config.LoadWorkspaces()
	if saved := ws.FindServer(trimmed); saved != nil {
		return Target{Kind: KindRemote, Name: saved.Name, BaseURL: saved.URL, AuthToken: ResolveAuthTokenFor(saved.URL, flagToken, "")}, nil
	}
	profiles := config.RemoteProfiles()
	if profile := findProfile(profiles, trimmed); profile != nil {
		return Target{
			Kind:      KindRemote,
			Name:      profile.Name,
			BaseURL:   profile.BaseURL,
			AuthToken: ResolveAuthTokenFor(profile.BaseURL, flagToken, profile.AuthToken),
		}, nil
	}
	if LooksLikeServerURL(trimmed) {
		base := remote.NormalizeBaseURL(trimmed)
		return Target{
			Kind:      KindRemote,
			BaseURL:   base,
			AuthToken: ResolveAuthTokenFor(base, flagToken, ""),
		}, nil
	}
	names := []string{}
	for _, s := range ws.Servers {
		names = append(names, s.Name)
	}
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	if len(names) > 0 {
		return Target{}, fmt.Errorf("No server named %q. Known servers: %s. You can also pass a URL like localhost:7878, or run `zn connect <url>` to save one.", trimmed, strings.Join(names, ", "))
	}
	return Target{}, fmt.Errorf("No server named %q and none is saved yet. Run `zn connect <url>` once (it asks for the token), or pass a URL like localhost:7878.", trimmed)
}

// ResolveVaultTarget resolves `--vault <name|path>`: a local vault name, then
// a server profile name, then a directory path.
func ResolveVaultTarget(selector, flagToken string) (Target, error) {
	trimmed := strings.TrimSpace(selector)
	if t, ok := TargetForWorkspace(config.LoadWorkspaces(), trimmed, flagToken); ok {
		return t, nil
	}
	known := config.KnownVaults()
	profiles := config.RemoteProfiles()
	localMatches := []config.KnownVault{}
	for _, v := range known {
		if strings.EqualFold(v.Name, trimmed) {
			localMatches = append(localMatches, v)
		}
	}
	profile := findProfile(profiles, trimmed)
	if len(localMatches) > 0 && profile != nil {
		return Target{}, fmt.Errorf("%q names both a local vault (%s) and a server (%s). Use --server %s for the server, or pass the local vault's path.", trimmed, localMatches[0].Root, profile.BaseURL, trimmed)
	}
	if profile != nil {
		return Target{
			Kind:      KindRemote,
			Name:      profile.Name,
			BaseURL:   profile.BaseURL,
			AuthToken: ResolveAuthToken(flagToken, profile.AuthToken),
		}, nil
	}
	root, err := config.ResolveVaultRoot(trimmed)
	if err != nil {
		return Target{}, err
	}
	return Target{Kind: KindLocal, Root: root}, nil
}

// ResolveDefaultTarget is the target when nothing named one: ZENNOTES_SERVER,
// then ZENNOTES_VAULT, then whatever the desktop app has open, a connected
// server included.
func ResolveDefaultTarget(flagToken string) (Target, error) {
	if envServer := strings.TrimSpace(os.Getenv("ZENNOTES_SERVER")); envServer != "" {
		return ResolveServerTarget(envServer, flagToken)
	}
	if strings.TrimSpace(os.Getenv("ZENNOTES_VAULT")) != "" {
		root, err := config.ResolveVaultRoot("")
		if err != nil {
			return Target{}, err
		}
		return Target{Kind: KindLocal, Root: root}, nil
	}
	// zn's own default, set by `zn connect`, `zn init`, `zn use` or a switch
	// in the TUI, comes before whatever the desktop app has open.
	if ws := config.LoadWorkspaces(); strings.TrimSpace(ws.Default) != "" {
		if t, ok := TargetForWorkspace(ws, ws.Default, flagToken); ok {
			return t, nil
		}
	}
	active := config.ActiveWorkspaceFromConfig()
	if active.Kind == "remote" {
		return Target{
			Kind:      KindRemote,
			Name:      active.Name,
			BaseURL:   active.BaseURL,
			AuthToken: ResolveAuthTokenFor(active.BaseURL, flagToken, active.AuthToken),
		}, nil
	}
	root, err := config.ResolveVaultRoot("")
	if err != nil {
		return Target{}, err
	}
	return Target{Kind: KindLocal, Root: root}, nil
}

// ResolveTarget is the target for one invocation. `--server` wins over
// `--vault`; with neither, the environment and then the app decide.
func ResolveTarget(vaultSelector, serverSelector, flagToken string) (Target, error) {
	if strings.TrimSpace(serverSelector) != "" {
		return ResolveServerTarget(serverSelector, flagToken)
	}
	if strings.TrimSpace(vaultSelector) != "" {
		return ResolveVaultTarget(vaultSelector, flagToken)
	}
	return ResolveDefaultTarget(flagToken)
}

// RememberTarget makes a target zn's default for next time, saving a path
// or URL that was typed rather than picked from the list.
func RememberTarget(t Target) error {
	ws := config.LoadWorkspaces()
	switch t.Kind {
	case KindLocal:
		entry := ws.AddVault("", t.Root)
		ws.Default = entry.Name
	case KindRemote:
		entry := ws.AddServer(t.Name, t.BaseURL)
		ws.Default = entry.Name
		if t.AuthToken != "" && config.LoadToken(t.BaseURL) == "" {
			_ = config.SaveToken(t.BaseURL, t.AuthToken)
		}
	default:
		return nil
	}
	return config.SaveWorkspaces(ws)
}
