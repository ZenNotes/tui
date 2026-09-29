package remote

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"time"
)

// ChangeEvent is the desktop/server watcher protocol.
type ChangeEvent struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Scope string `json:"scope,omitempty"`
	Err   error  `json:"-"`
}

func (c *Client) WatchChanges(ctx context.Context) (<-chan ChangeEvent, error) {
	headers := http.Header{}
	if c.AuthToken != "" {
		headers.Set("Authorization", "Bearer "+c.AuthToken)
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, c.BaseURL+"/api/watch", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return nil, err
	}
	events := make(chan ChangeEvent, 8)
	go func() {
		defer close(events)
		defer conn.CloseNow()
		for {
			_, data, err := conn.Read(ctx)
			ev := ChangeEvent{}
			if err != nil {
				ev.Err = err
			} else if err = json.Unmarshal(data, &ev); err != nil {
				continue
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
			if ev.Err != nil {
				return
			}
		}
	}()
	return events, nil
}
