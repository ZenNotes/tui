// Package remotetest is an in-memory ZenNotes server for tests: the note
// routes a read, change and write needs, with or without conditional note
// writes (znserver supportsConditionalNoteWrites).
package remotetest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
)

// Write is one write the server answered: what was sent and whether it was
// refused.
type Write struct {
	Path, Body, Base string
	// SentBase says the request carried a baseSha256 field at all.
	SentBase bool
	Refused  bool
}

// Server holds notes by vault-relative path.
type Server struct {
	*httptest.Server
	// Conditional is a server that hands out sha256 and honours baseSha256.
	// Without it the server is an older one: no hash, base ignored.
	Conditional bool
	// BeforeWrite runs before each write is applied, for a test to land a save
	// from another device in between a read and a write (with Set).
	BeforeWrite func(path string)

	mu     sync.Mutex
	notes  map[string]string
	writes []Write
	reads  int
}

// New starts a server holding notes and stops it with the test.
func New(t testing.TB, conditional bool, notes map[string]string) *Server {
	s := &Server{Conditional: conditional, notes: map[string]string{}}
	for k, v := range notes {
		s.notes[k] = v
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Sha256 is the hash the server hands out for body.
func Sha256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Set is a save from another device.
func (s *Server) Set(rel, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes[rel] = body
}

// Delete removes a note, as another device would.
func (s *Server) Delete(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.notes, rel)
}

// Body is a note's current text and whether it exists.
func (s *Server) Body(rel string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.notes[rel]
	return body, ok
}

// Writes lists every write the server answered, in order.
func (s *Server) Writes() []Write {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Write(nil), s.writes...)
}

// Reads counts the note reads served.
func (s *Server) Reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func (s *Server) meta(rel, body string) map[string]any {
	title := strings.TrimSuffix(path.Base(rel), ".md")
	folder := "inbox"
	if i := strings.Index(rel, "/"); i > 0 {
		folder = rel[:i]
	}
	m := map[string]any{"path": rel, "title": title, "folder": folder, "createdAt": 1, "updatedAt": 2, "size": len(body), "tags": []string{}, "wikilinks": []string{}, "excerpt": ""}
	if s.Conditional {
		m["sha256"] = Sha256(body)
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/capabilities":
		writeJSON(w, map[string]any{"version": "test", "supportsConditionalNoteWrites": s.Conditional, "supportsSymlinkMarks": true})
	case "/api/vault":
		writeJSON(w, map[string]any{"root": "/srv/notes", "name": "notes", "linkedSystemFolders": []string{}})
	case "/api/vault/settings":
		writeJSON(w, map[string]any{"primaryNotesLocation": "inbox"})
	case "/api/notes":
		s.mu.Lock()
		keys := make([]string, 0, len(s.notes))
		for k := range s.notes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := []map[string]any{}
		for _, k := range keys {
			m := s.meta(k, s.notes[k])
			delete(m, "sha256") // listings carry no hash
			out = append(out, m)
		}
		s.mu.Unlock()
		writeJSON(w, out)
	case "/api/folders", "/api/assets":
		writeJSON(w, []any{})
	case "/api/tasks":
		writeJSON(w, []any{})
	case "/api/tasks/for":
		rel := r.URL.Query().Get("path")
		s.mu.Lock()
		body := s.notes[rel]
		s.mu.Unlock()
		tasks := vault.ParseTasks(rel, strings.TrimSuffix(path.Base(rel), ".md"), vault.FolderInbox, body, vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectCLI})
		if tasks == nil {
			tasks = []vault.Task{}
		}
		writeJSON(w, tasks)
	case "/api/notes/read":
		rel := r.URL.Query().Get("path")
		s.mu.Lock()
		s.reads++
		body, ok := s.notes[rel]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "note not found", http.StatusNotFound)
			return
		}
		m := s.meta(rel, body)
		m["body"] = body
		writeJSON(w, m)
	case "/api/notes/create":
		var req struct{ Folder, Title, Subpath string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rel := path.Join(req.Folder, req.Subpath, req.Title+".md")
		s.mu.Lock()
		s.notes[rel] = ""
		m := s.meta(rel, "")
		s.mu.Unlock()
		delete(m, "sha256") // as znserver: a create hands out no hash
		writeJSON(w, m)
	case "/api/notes/write":
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rel, _ := raw["path"].(string)
		body, _ := raw["body"].(string)
		base, sentBase := raw["baseSha256"].(string)
		if s.BeforeWrite != nil {
			s.BeforeWrite(rel)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		write := Write{Path: rel, Body: body, Base: base, SentBase: sentBase}
		if s.Conditional && base != "" {
			current, ok := s.notes[rel]
			if !ok || !strings.EqualFold(Sha256(current), base) {
				write.Refused = true
				s.writes = append(s.writes, write)
				http.Error(w, "note changed since it was read", http.StatusConflict)
				return
			}
		}
		s.writes = append(s.writes, write)
		s.notes[rel] = body
		writeJSON(w, s.meta(rel, body))
	default:
		http.NotFound(w, r)
	}
}
