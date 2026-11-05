package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/fswatch"
	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/vault"
)

const (
	// taskWatchSettle lets a burst of server events settle into one scan, as
	// the local watcher batches its file events.
	taskWatchSettle = 300 * time.Millisecond
	// taskWatchRetry is how long a server without a live stream is polled
	// before the stream is tried again.
	taskWatchRetry = 30 * time.Second
)

// taskWatchEvent is one line of `zn task watch --json`.
type taskWatchEvent struct {
	At             string `json:"at"`
	ID             string `json:"id"`
	Path           string `json:"path"`
	Link           string `json:"link"`
	Title          string `json:"title"`
	Kind           string `json:"kind,omitempty"`
	Content        string `json:"content"`
	PreviousStatus string `json:"previousStatus"`
	Status         string `json:"status"`
	LineNumber     int    `json:"lineNumber"`
}

// cmdTaskWatch prints each task whose status changes, as it changes: a task
// note's `status:`, a checkbox line's `@status:`, or its checkbox. It only
// reports. What runs on a change is the caller's own script, never ZenNotes,
// so nothing that executes lives in a synced vault (ZenNotes/zennotes#941).
// The scans use the app's grammar, whose task text leaves the fields out, so
// a status change is never mistaken for an edited task.
func cmdTaskWatch(ctx context.Context, b backend.Backend, args Args) error {
	opts := vault.ParseTasksOptions{IncludeExcluded: args.Bool("include-excluded"), Dialect: vault.DialectApp}
	interval := 5
	if raw, given := args.String("interval"); given {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return fmt.Errorf("--interval is a whole number of seconds, 1 or more, got %q", raw)
		}
		interval = n
	}
	// Watching starts before the first scan, so a change made meanwhile is
	// either in that scan or wakes the next one, never reported late.
	changed, stop, err := watchVaultChanges(ctx, b, time.Duration(interval)*time.Second)
	if err != nil {
		return err
	}
	defer stop()
	first, err := b.ScanTasks(ctx, opts)
	if err != nil {
		return err
	}
	asJSON := args.Bool("json")
	if !asJSON {
		fmt.Fprintln(stderr, oneRow(stderr, "Watching "+b.Label()+" for task status changes. Ctrl+C stops."))
	}
	return followTaskStatuses(ctx, b, opts, first, changed, asJSON)
}

// followTaskStatuses scans again on each signal and reports every task whose
// status moved since it was last seen, until ctx ends or changed closes. A
// failed scan (a server briefly away) is reported and the watch goes on, and
// a task a scan misses keeps its last status, so no change is lost.
func followTaskStatuses(ctx context.Context, b backend.Backend, opts vault.ParseTasksOptions, first []vault.Task, changed <-chan struct{}, asJSON bool) error {
	tracker := vault.NewTaskStatusTracker(first)
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-changed:
			if !ok {
				return nil
			}
		}
		next, err := b.ScanTasks(ctx, opts)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			emitWatchError(err, asJSON)
			continue
		}
		for _, change := range tracker.Update(next) {
			emitTaskChange(change, asJSON)
		}
	}
}

// watchVaultChanges signals each time the vault may have changed, holding at
// most one signal while a scan runs. A local vault reports through its file
// events, batched 300 ms after the last one. A server pushes its changes over
// /api/watch; while that stream is down (a server or proxy without it) the
// server is asked every interval instead.
func watchVaultChanges(ctx context.Context, b backend.Backend, interval time.Duration) (<-chan struct{}, func(), error) {
	changed := make(chan struct{}, 1)
	signal := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	if root := b.Root(); root != "" {
		watcher, err := fswatch.Start(root)
		if err != nil {
			return nil, nil, err
		}
		go func() {
			defer close(changed)
			for {
				if _, ok := watcher.Next(); !ok {
					return
				}
				signal()
			}
		}()
		return changed, watcher.Close, nil
	}
	server, ok := b.(*backend.Remote)
	if !ok {
		return nil, nil, errors.New("this vault cannot be watched")
	}
	ctx, cancel := context.WithCancel(ctx)
	go followServerChanges(ctx, server.Client(), interval, signal)
	return changed, cancel, nil
}

// followServerChanges signals on each change the server pushes, once they
// settle, until ctx ends. Every (re)connection signals too, for whatever
// changed while the stream was down. Without a stream it signals every
// interval, and tries the stream again after taskWatchRetry.
func followServerChanges(ctx context.Context, client *remote.Client, interval time.Duration, signal func()) {
	for ctx.Err() == nil {
		if events, err := client.WatchChanges(ctx); err == nil {
			signal()
			var settle <-chan time.Time
		live:
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-events:
					if !ok || event.Err != nil {
						break live
					}
					settle = time.After(taskWatchSettle)
				case <-settle:
					settle = nil
					signal()
				}
			}
		}
		poll := time.NewTicker(interval)
		retry := time.NewTimer(taskWatchRetry)
		for polling := true; polling; {
			select {
			case <-ctx.Done():
				polling = false
			case <-poll.C:
				signal()
			case <-retry.C:
				polling = false
			}
		}
		poll.Stop()
		retry.Stop()
	}
}

func emitTaskChange(change vault.TaskStatusChange, asJSON bool) {
	t := change.Task
	if asJSON {
		emitJSONLine(taskWatchEvent{
			At:             time.Now().UTC().Format(time.RFC3339),
			ID:             t.ID,
			Path:           t.SourcePath,
			Link:           t.Link,
			Title:          t.NoteTitle,
			Kind:           t.Kind,
			Content:        t.Content,
			PreviousStatus: change.PreviousStatus,
			Status:         change.Status,
			LineNumber:     t.LineNumber,
		})
		return
	}
	emitLine(fmt.Sprintf("%s  %s → %s  %s  %s", time.Now().Format("15:04:05"), change.PreviousStatus, change.Status, truncate(t.Content, 80), t.SourcePath))
}

// emitWatchError reports a failed scan without ending the watch, as JSON on
// stderr when the output is JSON, the way a failed command reports.
func emitWatchError(err error, asJSON bool) {
	if !asJSON {
		emitError(err.Error())
		return
	}
	_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]string{"code": "error", "message": err.Error()}})
}
