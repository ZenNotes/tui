package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// remoteServer serves the recorded responses in testdata/remote-tasks as a
// ZenNotes server and records each note body that a client writes.
type remoteServer struct {
	URL    string
	mu     sync.Mutex
	writes []string
}

func (s *remoteServer) Writes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.writes...)
}

func newRemoteServer(t *testing.T) *remoteServer {
	t.Helper()
	dir := filepath.Join("testdata", "remote-tasks")
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
	s := &remoteServer{}
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
			s.mu.Lock()
			s.writes = append(s.writes, in["path"]+"\n"+in["body"])
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path": in["path"], "title": "x", "folder": "inbox", "tags": []string{}, "wikilinks": []string{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

// useRemoteServer points zn at the server for the rest of the test.
func useRemoteServer(t *testing.T, s *remoteServer) {
	t.Helper()
	isolatedCLI(t)
	t.Setenv("ZENNOTES_SERVER", s.URL)
	t.Setenv("ZENNOTES_REMOTE_TOKEN", "token")
}

// Against a server, `zn task list` prints the same bytes as zn v0.17.1 for
// notes that hold events, work sessions, `until:`, `skip:@` items and block
// ids. The `scheduled` value that the server sends stays in the JSON.
func TestRemoteTaskListMatchesReleasedOutput(t *testing.T) {
	s := newRemoteServer(t)
	useRemoteServer(t, s)
	for _, tc := range []struct {
		golden string
		flags  []string
	}{
		{"remote-tasks.json", []string{"--json"}},
		{"remote-tasks-all.json", []string{"--json", "--all"}},
		{"remote-tasks.txt", nil},
	} {
		want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
		if err != nil {
			t.Fatal(err)
		}
		args := append([]string{"task", "list"}, tc.flags...)
		out := captureOutput(t, func() {
			if code := Main(args); code != 0 {
				t.Errorf("zn %v exited %d", args, code)
			}
		})
		if out != string(want) {
			t.Errorf("zn %v differs from testdata/%s:\n%s", args, tc.golden, out)
		}
	}
}

// Against a server, `zn task toggle` ticks the line and writes nothing else:
// no next date and no copy of a work session.
func TestRemoteToggleOfRepeatingTaskWithSessionOnlyTicks(t *testing.T) {
	s := newRemoteServer(t)
	useRemoteServer(t, s)
	args := []string{"task", "toggle", "inbox/Work.md#0"}
	captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	want := []string{"inbox/Work.md\n# Work\n\n" +
		"- [x] Water plants repeat:mon,thu skip:@Holiday until:2026-12-31 ^ab12cd\n" +
		"- [ ] Send the invoice due:2026-11-12\n" +
		"- [ ] A due:2027-03-08T23:59 @repeat:weekly @until:2027-03-10 @skip:spring-break\n" +
		"- [ ] B due:2027-03-08 @repeat:monthly-15\n"}
	if got := s.Writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes:\n%q\nwant:\n%q", got, want)
	}
}
