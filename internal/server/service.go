package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

type ServiceSpec struct {
	Label, Executable, Dir, LogPath string
	Env                             map[string]string
}

// Stop also disables the login service, so an intentionally stopped instance
// stays stopped after login or an update's temporary health check.
type Service interface {
	Start(context.Context, ServiceSpec) error
	Stop(context.Context, ServiceSpec) error
	Active(context.Context, ServiceSpec) (bool, error)
}

func (m *Manager) spec(i Instance) ServiceSpec {
	hash := sha256.Sum256([]byte(m.Root))
	return ServiceSpec{
		Label:      fmt.Sprintf("com.zennotes.server.%s.%x", i.Name, hash[:4]),
		Executable: m.BinaryPath(i), Dir: m.Dir(i.Name), LogPath: m.LogPath(i.Name),
		Env: map[string]string{
			"ZENNOTES_CONFIG_PATH":     filepath.Join(m.Dir(i.Name), "server.json"),
			"ZENNOTES_AUTH_TOKEN_FILE": m.TokenPath(i.Name),
			"ZENNOTES_VAULT_PATH":      i.Vault, "ZENNOTES_BIND": i.Bind, "ZENNOTES_BASE_PATH": i.BasePath,
			"ZENNOTES_AUTH_TOKEN": "", "ZENNOTES_DEV": "0", "ZENNOTES_ALLOW_INSECURE_NOAUTH": "0",
			"ZENNOTES_BROWSE_ROOTS": i.Vault, "ZENNOTES_ALLOW_UNSCOPED_BROWSE": "0",
		},
	}
}
