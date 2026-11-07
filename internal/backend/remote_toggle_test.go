package backend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A remote toggle only flips the checkbox: the server parses the note, so zn
// never inserts the next occurrence of a repeating task there.
func TestRemoteToggleNeverInsertsNextOccurrence(t *testing.T) {
	const path = "inbox/plants.md"
	var mu sync.Mutex
	body := "intro\n- [ ] Water plants due:2026-11-02 repeat:mon,thu\noutro\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/notes/read":
			_ = json.NewEncoder(w).Encode(map[string]any{"path": path, "title": "plants", "folder": "inbox", "body": body})
		case "/api/notes/write":
			var in struct{ Path, Body string }
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			body = in.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"path": path, "title": "plants", "folder": "inbox"})
		case "/api/tasks/for":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": path + "#0", "sourcePath": path, "taskIndex": 0, "content": "Water plants", "checked": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	b, err := New(Target{Kind: KindRemote, BaseURL: server.URL, AuthToken: "t"}, Options{
		Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ToggleTask(t.Context(), path+"#0", 0); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := "intro\n- [x] Water plants due:2026-11-02 repeat:mon,thu\noutro\n"
	if body != want {
		t.Fatalf("remote note:\n%s\nwant:\n%s", body, want)
	}
}
