package remote

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type httpContract struct {
	SchemaVersion int
	Protocol      string
	MountPaths    []string
	Note          struct {
		Path, Body, UpdatedBody         string
		AssetEmbeds, UpdatedAssetEmbeds []string
	}
	RequiredCapabilities []string
	RequiredNoteFields   []string
	Errors               struct {
		Unauthenticated, MissingNote, DirectoryAsNote int
		Challenge                                     string
	}
}

const contractToken = "isolated-tui-contract-token"

func readHTTPContract(t *testing.T) httpContract {
	t.Helper()
	data, err := os.ReadFile("testdata/self-hosted-http.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/self-hosted-http.json.source.json")
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct{ Sha256 string }
	if err = json.Unmarshal(source, &provenance); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != provenance.Sha256 {
		t.Fatal("shared HTTP fixture checksum differs")
	}
	var fixture httpContract
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 || fixture.Protocol != "self-hosted-http-v1" {
		t.Fatal("unsupported HTTP contract")
	}
	return fixture
}

// The same client assertions run against a deterministic fixture in ordinary CI
// and an independently supplied server binary in the cross-repository rehearsal.
func checkHTTPClientContract(t *testing.T, base string, f httpContract) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	anonymous := NewClient(base, "")
	caps, err := anonymous.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range f.RequiredCapabilities {
		if _, ok := caps[key]; !ok {
			t.Errorf("missing capability %s", key)
		}
	}
	if _, err := anonymous.ReadNote(ctx, f.Note.Path); StatusOf(err) != f.Errors.Unauthenticated {
		t.Fatalf("anonymous read: %v", err)
	}
	client := NewClient(base, contractToken)
	checkWireNote := func(expectedEmbeds []string) {
		t.Helper()
		var raw map[string]json.RawMessage
		if err := client.Get(ctx, "/api/notes/read?path="+url.QueryEscape(f.Note.Path), &raw); err != nil {
			t.Fatal(err)
		}
		for _, field := range f.RequiredNoteFields {
			if _, ok := raw[field]; !ok {
				t.Errorf("missing wire field %s", field)
			}
		}
		var embeds []string
		if err := json.Unmarshal(raw["assetEmbeds"], &embeds); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(embeds, expectedEmbeds) {
			t.Fatalf("wire embeds: got %#v want %#v", embeds, expectedEmbeds)
		}
	}
	checkWireNote(f.Note.AssetEmbeds)
	note, err := client.ReadNote(ctx, f.Note.Path)
	if err != nil {
		t.Fatal(err)
	}
	if note.Body != f.Note.Body {
		t.Fatalf("initial note differs: %+v", note)
	}
	if note.Link == "" {
		t.Fatal("remote note did not acquire its desktop deep link")
	}
	meta, err := client.WriteNote(ctx, f.Note.Path, f.Note.UpdatedBody)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Path != f.Note.Path {
		t.Fatalf("write metadata differs: %+v", meta)
	}
	checkWireNote(f.Note.UpdatedAssetEmbeds)
	updated, err := client.ReadNote(ctx, f.Note.Path)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != f.Note.UpdatedBody {
		t.Fatalf("updated Markdown bytes changed: %q", updated.Body)
	}
	for path, status := range map[string]int{"missing.md": f.Errors.MissingNote, "inbox": f.Errors.DirectoryAsNote} {
		if _, err := client.ReadNote(ctx, path); StatusOf(err) != status {
			t.Errorf("read %s: expected status %d, got %v", path, status, err)
		}
	}
	wrong := NewClient(base, "incorrect-isolated-token")
	if _, err := wrong.WriteNote(ctx, f.Note.Path, "must not replace content"); StatusOf(err) != f.Errors.Unauthenticated {
		t.Fatalf("wrong-token write: %v", err)
	}
	retained, err := client.ReadNote(ctx, f.Note.Path)
	if err != nil || retained.Body != f.Note.UpdatedBody {
		t.Fatalf("unauthorized write changed note: %v", err)
	}
}

func TestSharedHTTPClientContract(t *testing.T) {
	fixture := readHTTPContract(t)
	for _, mount := range fixture.MountPaths {
		t.Run("mount="+mount, func(t *testing.T) {
			body := fixture.Note.Body
			metadata := func() map[string]any {
				embeds := fixture.Note.AssetEmbeds
				if body == fixture.Note.UpdatedBody {
					embeds = fixture.Note.UpdatedAssetEmbeds
				}
				return map[string]any{"path": fixture.Note.Path, "title": "Contract", "folder": "inbox", "siblingOrder": 0, "createdAt": 1, "updatedAt": 2, "size": len(body), "tags": []string{}, "wikilinks": []string{}, "assetEmbeds": embeds, "hasAttachments": true, "excerpt": "Contract"}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if !strings.HasPrefix(r.URL.Path, mount+"/api/") {
					http.NotFound(w, r)
					return
				}
				path := strings.TrimPrefix(r.URL.Path, mount)
				if path == "/api/capabilities" {
					caps := map[string]any{}
					for _, key := range fixture.RequiredCapabilities {
						caps[key] = true
					}
					caps["version"] = "fixture"
					caps["platform"] = "linux"
					json.NewEncoder(w).Encode(caps)
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+contractToken {
					w.Header().Set("WWW-Authenticate", fixture.Errors.Challenge)
					http.Error(w, "unauthorized", fixture.Errors.Unauthenticated)
					return
				}
				switch path {
				case "/api/notes/read":
					if r.Method != http.MethodGet {
						t.Errorf("unexpected read method: %s", r.Method)
					}
					rel := r.URL.Query().Get("path")
					if rel == "inbox" {
						http.Error(w, "directory", fixture.Errors.DirectoryAsNote)
						return
					}
					if rel != fixture.Note.Path {
						http.Error(w, "missing", fixture.Errors.MissingNote)
						return
					}
					note := metadata()
					note["body"] = body
					json.NewEncoder(w).Encode(note)
				case "/api/notes/write":
					if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
						t.Error("invalid write transport")
					}
					var payload struct{ Path, Body string }
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
						http.Error(w, "bad request", 400)
						return
					}
					if payload.Path != fixture.Note.Path {
						t.Errorf("wrong relative path: %q", payload.Path)
					}
					body = payload.Body
					json.NewEncoder(w).Encode(metadata())
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			checkHTTPClientContract(t, server.URL+mount, fixture)
		})
	}
}

func TestIndependentServerHTTPContract(t *testing.T) {
	binary := os.Getenv("ZENNOTES_SERVER_CONTRACT_BINARY")
	if binary == "" {
		t.Skip("set ZENNOTES_SERVER_CONTRACT_BINARY to an independently built server to run the cross-repository rehearsal")
	}
	fixture := readHTTPContract(t)
	for _, mount := range fixture.MountPaths {
		t.Run("mount="+mount, func(t *testing.T) {
			root := t.TempDir()
			vaultRoot := filepath.Join(root, "vault")
			path := filepath.Join(vaultRoot, filepath.FromSlash(fixture.Note.Path))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(fixture.Note.Body), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			log, err := os.Create(filepath.Join(root, "server.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			command := exec.Command(binary)
			command.Dir = root
			command.Env = append(os.Environ(), "ZENNOTES_BIND="+address, "ZENNOTES_DEFAULT_VAULT_PATH="+vaultRoot, "ZENNOTES_CONFIG_PATH="+filepath.Join(root, "server.json"), "ZENNOTES_BROWSE_ROOTS="+vaultRoot, "ZENNOTES_AUTH_TOKEN="+contractToken, "ZENNOTES_BASE_PATH="+mount)
			command.Stdout = log
			command.Stderr = log
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { command.Process.Kill(); command.Wait() })
			base := "http://" + address + mount
			deadline := time.Now().Add(10 * time.Second)
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				_, err := NewClient(base, "").Capabilities(ctx)
				cancel()
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server did not become ready: ", err)
				}
				time.Sleep(50 * time.Millisecond)
			}
			checkHTTPClientContract(t, base, fixture)
			stored, err := os.ReadFile(path)
			if err != nil || string(stored) != fixture.Note.UpdatedBody {
				t.Fatalf("server persisted different bytes: %v", err)
			}
		})
	}
}
