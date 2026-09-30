package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/releases"
)

type fixtureReleases struct{}

func (fixtureReleases) Lookup(_ context.Context, _ releases.Product, version string) (releases.Release, error) {
	if version == "" || version == "latest" {
		version = "2.56.0"
	}
	return releases.Release{Tag: "v" + version}, nil
}
func (fixtureReleases) Stage(_ context.Context, _ releases.Product, r releases.Release, _, _, dir string) (string, error) {
	f, err := os.CreateTemp(dir, "candidate-*")
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(r.Version())
	f.Close()
	return f.Name(), err
}

func fixtureManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", filepath.Join(root, "config"))
	m := NewManager(filepath.Join(root, "managed"))
	m.Releases = fixtureReleases{}
	return m
}

func TestInstallIsIdempotentAndSeparatesSecrets(t *testing.T) {
	m := fixtureManager(t)
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "Keep.md"), []byte("original note"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{Name: "home", Vault: vault}
	i, err := m.Install(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(m.TokenPath(i.Name))
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		t.Fatalf("missing generated token: %v", err)
	}
	i2, err := m.Install(context.Background(), opts)
	if err != nil || i2 != i {
		t.Fatalf("repeat installation changed state: %+v %v", i2, err)
	}
	secondToken, _ := os.ReadFile(m.TokenPath(i.Name))
	if string(token) != string(secondToken) {
		t.Fatal("repeated installation rotated the token")
	}
	encoded, _ := json.Marshal(i)
	if strings.Contains(string(encoded), strings.TrimSpace(string(token))) {
		t.Fatal("instance output exposes credentials")
	}
	entries, _ := os.ReadDir(vault)
	if len(entries) != 1 || entries[0].Name() != "Keep.md" {
		t.Fatalf("installation changed vault contents: %v", entries)
	}
	if _, err := m.Install(context.Background(), Options{Name: "home", Vault: t.TempDir()}); err == nil {
		t.Fatal("existing instance silently repointed at another vault")
	}
}

func TestInstallRejectsInvalidInputsBeforeWriting(t *testing.T) {
	for _, tc := range []Options{
		{Name: "../escape", Vault: "/tmp/notes"},
		{Name: "bad/name", Vault: "/tmp/notes"},
		{Name: "home", Vault: "/tmp/notes", Bind: "127.0.0.1:99999"},
		{Name: "home", Vault: "/tmp/notes", BasePath: "/../api"},
	} {
		m := fixtureManager(t)
		if _, err := m.Install(context.Background(), tc); err == nil {
			t.Errorf("accepted %+v", tc)
		}
		if _, err := os.Stat(m.Root); !os.IsNotExist(err) {
			t.Fatal("invalid installation wrote its root")
		}
	}
	m := fixtureManager(t)
	if _, err := m.Install(context.Background(), Options{Name: "home", Vault: filepath.Dir(m.Root)}); err == nil {
		t.Fatal("accepted secrets directory inside served vault")
	}
}

type fixtureService struct {
	active bool
	spec   ServiceSpec
}

func (s *fixtureService) Start(_ context.Context, spec ServiceSpec) error {
	s.active, s.spec = true, spec
	return nil
}
func (s *fixtureService) Stop(_ context.Context, _ ServiceSpec) error {
	s.active = false
	return nil
}
func (s *fixtureService) Active(_ context.Context, _ ServiceSpec) (bool, error) {
	return s.active, nil
}

func TestUnhealthyUpgradeRestoresWorkingServerAndToken(t *testing.T) {
	m := fixtureManager(t)
	driver := &fixtureService{}
	m.Service = driver
	m.HealthTimeout = time.Millisecond
	m.Check = func(_ context.Context, i Instance) error {
		if i.Version == "2.57.0" {
			return errors.New("candidate is unhealthy")
		}
		return nil
	}
	i, err := m.Setup(context.Background(), Options{Name: "home", Vault: t.TempDir(), NoDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := os.ReadFile(m.TokenPath(i.Name))
	if _, err := m.Update(context.Background(), i.Name, "2.57.0"); err == nil {
		t.Fatal("unhealthy update succeeded")
	}
	got, err := m.Load(i.Name)
	if err != nil || got.Version != "2.56.0" || !driver.active || driver.spec.Executable != m.BinaryPath(i) {
		t.Fatalf("rollback did not restore the old service: %+v %+v %v", got, driver, err)
	}
	after, _ := os.ReadFile(m.TokenPath(i.Name))
	if string(token) != string(after) {
		t.Fatal("upgrade changed credentials")
	}
	m.Check = func(context.Context, Instance) error { return nil }
	updated, err := m.Update(context.Background(), i.Name, "2.57.0")
	if err != nil || updated.Version != "2.57.0" || updated.PreviousVersion != "2.56.0" {
		t.Fatalf("healthy update: %+v %v", updated, err)
	}
	restored, err := m.Rollback(context.Background(), i.Name)
	if err != nil || restored.Version != "2.56.0" || restored.PreviousVersion != "2.57.0" {
		t.Fatalf("explicit rollback: %+v %v", restored, err)
	}
}

func TestConfigurationRollsBackWhenClientProfileCannotBeSaved(t *testing.T) {
	m := fixtureManager(t)
	driver := &fixtureService{}
	m.Service = driver
	m.Check = func(context.Context, Instance) error { return nil }
	i, err := m.Setup(context.Background(), Options{Name: "home", Vault: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// An unwritable atomic-write destination reproduces a credential-store
	// failure without relying on Unix permission semantics or root privileges.
	if err := os.Mkdir(config.CredentialsPath()+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(context.Background(), i.Name, map[string]string{"base-path": "/changed"}); err == nil {
		t.Fatal("profile failure was ignored")
	}
	got, err := m.Load(i.Name)
	if err != nil || got.BasePath != "" || !driver.active {
		t.Fatalf("configuration was not restored: %+v %v", got, err)
	}
	if s := config.LoadWorkspaces().FindServer(i.Name); s == nil || s.URL != i.URL() {
		t.Fatal("saved profile no longer names the active server")
	}
}
