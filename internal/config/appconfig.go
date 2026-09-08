package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/remote"
)

// AppConfigFile is the runtime config the desktop app maintains.
const AppConfigFile = "zennotes.config.json"

func readAppConfig() map[string]any {
	raw, err := os.ReadFile(filepath.Join(UserDataDir(), AppConfigFile))
	if err != nil {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	return parsed
}

// VaultRootFromConfig is the vault the app last opened, or "".
func VaultRootFromConfig() string {
	parsed := readAppConfig()
	if root, ok := parsed["vaultRoot"].(string); ok && strings.TrimSpace(root) != "" {
		return root
	}
	return ""
}

// KnownVault is one local vault the app knows about.
type KnownVault struct {
	Root         string `json:"root"`
	Name         string `json:"name"`
	LastOpenedAt *int64 `json:"lastOpenedAt"`
}

// KnownVaults lists every local vault the app knows: its `localVaults` list
// plus the active `vaultRoot` when it is not listed. Most recently opened
// first.
func KnownVaults() []KnownVault {
	parsed := readAppConfig()
	seen := map[string]struct{}{}
	out := []KnownVault{}
	if list, ok := parsed["localVaults"].([]any); ok {
		for _, entry := range list {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			root, _ := m["root"].(string)
			if strings.TrimSpace(root) == "" {
				continue
			}
			resolved, err := filepath.Abs(root)
			if err != nil {
				resolved = root
			}
			if _, dup := seen[resolved]; dup {
				continue
			}
			seen[resolved] = struct{}{}
			name, _ := m["name"].(string)
			if strings.TrimSpace(name) == "" {
				name = filepath.Base(resolved)
			}
			var last *int64
			if n, ok := m["lastOpenedAt"].(float64); ok {
				v := int64(n)
				last = &v
			}
			out = append(out, KnownVault{Root: resolved, Name: name, LastOpenedAt: last})
		}
	}
	if active := VaultRootFromConfig(); active != "" {
		resolved, err := filepath.Abs(active)
		if err != nil {
			resolved = active
		}
		if _, dup := seen[resolved]; !dup {
			out = append(out, KnownVault{Root: resolved, Name: filepath.Base(resolved)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return derefTime(out[i].LastOpenedAt) > derefTime(out[j].LastOpenedAt)
	})
	return out
}

func derefTime(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// RemoteProfile is a saved ZenNotes server.
type RemoteProfile struct {
	ID              string
	Name            string
	BaseURL         string
	AuthToken       string
	LastConnectedAt *int64
}

// RemoteProfiles lists every server the app has been connected to, newest
// first, the legacy single-server `remoteWorkspace` key folded in.
func RemoteProfiles() []RemoteProfile {
	parsed := readAppConfig()
	out := []RemoteProfile{}
	seen := map[string]struct{}{}
	if list, ok := parsed["remoteWorkspaceProfiles"].([]any); ok {
		for _, entry := range list {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			baseURL, _ := m["baseUrl"].(string)
			name, _ := m["name"].(string)
			if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(name) == "" {
				continue
			}
			normalized := remote.NormalizeBaseURL(baseURL)
			seen[normalized] = struct{}{}
			id, _ := m["id"].(string)
			if strings.TrimSpace(id) == "" {
				id = normalized
			}
			token, _ := m["authToken"].(string)
			var last *int64
			if n, ok := m["lastConnectedAt"].(float64); ok {
				v := int64(n)
				last = &v
			}
			out = append(out, RemoteProfile{
				ID:              id,
				Name:            strings.TrimSpace(name),
				BaseURL:         normalized,
				AuthToken:       strings.TrimSpace(token),
				LastConnectedAt: last,
			})
		}
	}
	if legacy, ok := parsed["remoteWorkspace"].(map[string]any); ok {
		baseURL, _ := legacy["baseUrl"].(string)
		if strings.TrimSpace(baseURL) != "" {
			normalized := remote.NormalizeBaseURL(baseURL)
			if _, dup := seen[normalized]; !dup {
				token, _ := legacy["authToken"].(string)
				out = append(out, RemoteProfile{
					ID:        normalized,
					Name:      "ZenNotes Server",
					BaseURL:   normalized,
					AuthToken: strings.TrimSpace(token),
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return derefTime(out[i].LastConnectedAt) > derefTime(out[j].LastConnectedAt)
	})
	return out
}

// ActiveWorkspace is what the desktop app currently has open.
type ActiveWorkspace struct {
	// Kind is "local" or "remote".
	Kind      string
	Root      string
	BaseURL   string
	Name      string
	ProfileID string
	AuthToken string
}

// ActiveWorkspaceFromConfig reads the workspace the app has open. The app
// keeps server tokens in the OS secret store, so the token here is only a
// legacy plaintext one; callers layer ZENNOTES_REMOTE_TOKEN on top.
func ActiveWorkspaceFromConfig() ActiveWorkspace {
	parsed := readAppConfig()
	root := VaultRootFromConfig()
	if mode, _ := parsed["workspaceMode"].(string); mode != "remote" {
		return ActiveWorkspace{Kind: "local", Root: root}
	}
	remoteRaw, _ := parsed["remoteWorkspace"].(map[string]any)
	rawBase, _ := remoteRaw["baseUrl"].(string)
	if strings.TrimSpace(rawBase) == "" {
		return ActiveWorkspace{Kind: "local", Root: root}
	}
	baseURL := remote.NormalizeBaseURL(rawBase)
	configuredID, _ := parsed["remoteWorkspaceProfileId"].(string)
	configuredID = strings.TrimSpace(configuredID)
	profiles := RemoteProfiles()
	var profile *RemoteProfile
	if configuredID != "" {
		for i := range profiles {
			if profiles[i].ID == configuredID {
				profile = &profiles[i]
				break
			}
		}
	}
	if profile == nil {
		for i := range profiles {
			if profiles[i].BaseURL == baseURL {
				profile = &profiles[i]
				break
			}
		}
	}
	ws := ActiveWorkspace{Kind: "remote", BaseURL: baseURL, ProfileID: configuredID}
	legacyToken, _ := remoteRaw["authToken"].(string)
	if profile != nil {
		ws.Name = profile.Name
		ws.ProfileID = profile.ID
		ws.AuthToken = profile.AuthToken
	}
	if ws.AuthToken == "" {
		ws.AuthToken = strings.TrimSpace(legacyToken)
	}
	return ws
}

// ResolveVaultSelector resolves a `--vault` selector: a known vault name
// first (case-insensitive), then a directory path. Errors name the
// available vaults so a typo is self-correcting.
func ResolveVaultSelector(selector string) (string, error) {
	trimmed := strings.TrimSpace(selector)
	known := KnownVaults()
	byName := []KnownVault{}
	for _, v := range known {
		if strings.EqualFold(v.Name, trimmed) {
			byName = append(byName, v)
		}
	}
	if len(byName) == 1 {
		root := byName[0].Root
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root, nil
		}
		return "", fmt.Errorf("Vault %q points to %s, which is missing. Open it in ZenNotes again or pass a path.", byName[0].Name, root)
	}
	if len(byName) > 1 {
		roots := make([]string, 0, len(byName))
		for _, v := range byName {
			roots = append(roots, v.Root)
		}
		return "", fmt.Errorf("Multiple vaults are named %q (%s). Pass the path instead.", trimmed, strings.Join(roots, ", "))
	}
	abs, err := filepath.Abs(ExpandHome(trimmed))
	if err == nil {
		if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
			return abs, nil
		}
	}
	names := make([]string, 0, len(known))
	for _, v := range known {
		names = append(names, v.Name)
	}
	if len(names) > 0 {
		return "", fmt.Errorf("No vault named %q. Known vaults: %s. You can also pass a directory path.", trimmed, strings.Join(names, ", "))
	}
	return "", fmt.Errorf("No vault named %q and no such directory. Pass a vault directory path.", trimmed)
}

// ErrNoVault says nothing names a vault at all.
var ErrNoVault = errors.New("No vault is set up for zn yet. Run `zn setup` for a guided start, or one of: `zn init ~/Notes` (create a vault), `zn vault add <folder>` (use an existing one), `zn connect <server-url>` (a ZenNotes server). Opening a vault in the ZenNotes app works too.")

// ResolveVaultRoot picks the vault root: an explicit selector, else
// ZENNOTES_VAULT, else the app's active vault.
func ResolveVaultRoot(selector string) (string, error) {
	if strings.TrimSpace(selector) != "" {
		return ResolveVaultSelector(selector)
	}
	if fromEnv := strings.TrimSpace(os.Getenv("ZENNOTES_VAULT")); fromEnv != "" {
		return filepath.Abs(ExpandHome(fromEnv))
	}
	if fromConfig := VaultRootFromConfig(); fromConfig != "" {
		return filepath.Abs(fromConfig)
	}
	return "", ErrNoVault
}

// MCPInstructionsPath is where a user override of the MCP instructions lives.
func MCPInstructionsPath() string {
	return filepath.Join(UserDataDir(), "zennotes.mcp-instructions.md")
}
