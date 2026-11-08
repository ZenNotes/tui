package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// remoteFixture serves the recorded responses in
// internal/cli/testdata/remote-tasks as a ZenNotes server. It records each
// note body that a client writes.
type remoteFixture struct {
	URL    string
	mu     sync.Mutex
	writes []string
}

func (f *remoteFixture) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	dir := filepath.Join("..", "cli", "testdata", "remote-tasks")
	file := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var tasks []map[string]any
	if err := json.Unmarshal(file("tasks.json"), &tasks); err != nil {
		t.Fatal(err)
	}
	f := &remoteFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/vault":
			_, _ = w.Write(file("vault.json"))
		case "/api/vault/settings":
			_, _ = w.Write(file("settings.json"))
		case "/api/notes":
			_, _ = w.Write(file("notes.json"))
		case "/api/tasks":
			_, _ = w.Write(file("tasks.json"))
		case "/api/tasks/for":
			out := []map[string]any{}
			for _, task := range tasks {
				if task["sourcePath"] == r.URL.Query().Get("path") {
					out = append(out, task)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/api/notes/read":
			rel := r.URL.Query().Get("path")
			var metas []map[string]any
			_ = json.Unmarshal(file("notes.json"), &metas)
			for _, meta := range metas {
				if meta["path"] == rel {
					meta["body"] = string(file(filepath.Join("notes", filepath.FromSlash(rel))))
					_ = json.NewEncoder(w).Encode(meta)
					return
				}
			}
			http.NotFound(w, r)
		case "/api/notes/write":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.mu.Lock()
			f.writes = append(f.writes, in["path"]+"\n"+in["body"])
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path": in["path"], "title": "x", "folder": "inbox", "tags": []string{}, "wikilinks": []string{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	f.URL = server.URL
	return f
}
