package backend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A remote toggle follows the server: the server parses the note itself, so
// zn rolls the values that the desktop app rolls and ticks every other task
// without a copy. The dates are far ahead, so the result is the same on every
// day.
func TestRemoteToggleRollsOnlyUpstreamRules(t *testing.T) {
	const path = "inbox/plants.md"
	for _, tc := range []struct{ name, line, want string }{
		{
			"a rule the desktop app reads rolls",
			"- [ ] Water plants due:2999-01-01 @repeat:weekly @remind:9am",
			"- [ ] Water plants due:2999-01-08 @repeat:weekly @remind:9am\n- [x] Water plants due:2999-01-01\n",
		},
		{
			"a rule only zn reads is a plain tick",
			"- [ ] Water plants due:2999-01-01 @repeat:monthly-15",
			"- [x] Water plants due:2999-01-01 @repeat:monthly-15\n",
		},
		{
			"a bare repeat token is a plain tick",
			"- [ ] Water plants due:2999-01-01 repeat:mon,thu",
			"- [x] Water plants due:2999-01-01 repeat:mon,thu\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			body := "intro\n" + tc.line + "\noutro\n"
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
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"id": path + "#0", "sourcePath": path, "taskIndex": 0, "content": "Water plants", "checked": false},
						{"id": path + "#1", "sourcePath": path, "taskIndex": 1, "content": "Water plants", "checked": true},
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			b, err := New(Target{Kind: KindRemote, BaseURL: server.URL, AuthToken: "t"}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.ToggleTask(t.Context(), path+"#0", 0); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			want := "intro\n" + tc.want + "outro\n"
			if body != want {
				t.Fatalf("remote note:\n%s\nwant:\n%s", body, want)
			}
		})
	}
}
