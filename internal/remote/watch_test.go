package remote

import (
	"context"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWatchChangesAuthenticationEventsAndCancellation(t *testing.T) {
	authenticated := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated <- r.URL.Path == "/api/watch" && r.Header.Get("Authorization") == "Bearer test-token"
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"kind":"change","path":"note.md"}`))
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := NewClient(server.URL, "test-token").WatchChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !<-authenticated {
		t.Fatal("missing watch path or authentication")
	}
	select {
	case e := <-events:
		if e.Path != "note.md" || e.Err != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event timeout")
	}
	cancel()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("watch did not close on cancellation")
		}
	}
}

func TestWatchChangesReturnsConnectionError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }))
	defer s.Close()
	if _, err := NewClient(s.URL, "bad").WatchChanges(context.Background()); err == nil {
		t.Fatal("unauthorized connection accepted")
	}
}
