package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/releases"
)

func (m *Manager) token(name string) (string, error) {
	info, err := os.Lstat(m.TokenPath(name))
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", fmt.Errorf("server token file is missing or invalid")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("server token file must be private (mode 0600)")
	}
	raw, err := os.ReadFile(m.TokenPath(name))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 || strings.ContainsAny(token, "\r\n") {
		return "", fmt.Errorf("server token file is empty or invalid")
	}
	return token, nil
}

func (m *Manager) verifyBinary(i Instance) error {
	digest, err := fileDigest(m.BinaryPath(i))
	if err != nil {
		return err
	}
	if digest != i.SHA256 || digest == "" {
		return fmt.Errorf("installed server binary failed integrity verification")
	}
	return nil
}

func (m *Manager) checkServer(ctx context.Context, i Instance) error {
	token, err := m.token(i.Name)
	if err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // This is a locally managed instance, never a proxy request.
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(route, auth string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.URL()+route, nil)
		if err != nil {
			return err
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if auth == "" {
			if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusForbidden {
				return fmt.Errorf("server vault is accessible without the configured token")
			}
			return nil
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("server check %s returned HTTP %d", route, res.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	var health struct {
		OK bool `json:"ok"`
	}
	if err := get("/api/healthz", token, &health); err != nil {
		return err
	}
	if !health.OK {
		return fmt.Errorf("server health check failed")
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := get("/api/version", token, &version); err != nil {
		return err
	}
	v, _ := releases.NormalizeVersion(version.Version)
	if v != i.Version {
		return fmt.Errorf("server reports version %q, expected %s", version.Version, i.Version)
	}
	var vault struct {
		Root string `json:"root"`
	}
	if err := get("/api/vault", token, &vault); err != nil {
		return err
	}
	root, err := canonicalPath(vault.Root)
	if err != nil || vault.Root == "" || root != i.Vault {
		return fmt.Errorf("server is serving a different vault")
	}
	return get("/api/vault", "", nil)
}

func (m *Manager) waitHealthy(ctx context.Context, i Instance) error {
	ctx, cancel := context.WithTimeout(ctx, m.HealthTimeout)
	defer cancel()
	for {
		err := m.Check(ctx, i)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("server did not become healthy: %w", errors.Join(err, ctx.Err()))
		case <-time.After(200 * time.Millisecond):
		}
	}
}
