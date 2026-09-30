package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthVerifiesVersionVaultAndAuthentication(t *testing.T) {
	m := fixtureManager(t)
	for _, tc := range []struct{ name, version, root, noAuth string }{
		{name: "healthy"},
		{name: "wrong version", version: "2.55.0"},
		{name: "wrong vault", root: t.TempDir()},
		{name: "missing auth", noAuth: "enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var i Instance
			token := ""
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token && r.URL.Path == "/api/vault" && tc.noAuth == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case "/api/healthz":
					json.NewEncoder(w).Encode(map[string]bool{"ok": true})
				case "/api/version":
					version := i.Version
					if tc.version != "" {
						version = tc.version
					}
					json.NewEncoder(w).Encode(map[string]string{"version": version})
				case "/api/vault":
					root := i.Vault
					if tc.root != "" {
						root = tc.root
					}
					json.NewEncoder(w).Encode(map[string]string{"root": root})
				}
			}))
			defer srv.Close()
			var err error
			i, err = m.Install(context.Background(), Options{Name: strings.ReplaceAll(tc.name, " ", "-"), Vault: t.TempDir(), Bind: strings.TrimPrefix(srv.URL, "http://")})
			if err != nil {
				t.Fatal(err)
			}
			token, _ = m.token(i.Name)
			err = m.checkServer(context.Background(), i)
			if (err == nil) != (tc.name == "healthy") {
				t.Fatalf("%s: %v", tc.name, err)
			}
		})
	}
}

func TestUpdatingStoppedServerReturnsItToStopped(t *testing.T) {
	m := fixtureManager(t)
	s := &fixtureService{}
	m.Service = s
	m.Check = func(context.Context, Instance) error { return nil }
	i, err := m.Install(context.Background(), Options{Name: "home", Vault: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	i, err = m.Update(context.Background(), i.Name, "2.57.0")
	if err != nil || s.active || i.Version != "2.57.0" {
		t.Fatalf("stopped server update: %+v %v active=%t", i, err, s.active)
	}
}
