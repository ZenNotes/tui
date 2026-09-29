package tui

import (
	"context"
	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/remote"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

type remoteWatchConnectedMsg struct {
	epoch  *int
	events <-chan remote.ChangeEvent
	err    error
}
type remoteWatchEventMsg struct {
	epoch  *int
	event  remote.ChangeEvent
	events <-chan remote.ChangeEvent
	ok     bool
}
type remoteWatchRetryMsg struct{ epoch *int }
type remoteDebounceMsg struct{ epoch *int }

func (a *App) startRemoteWatch() {
	if a.opts.Target.Kind != backend.KindRemote {
		return
	}
	if a.remoteWatchCancel != nil {
		a.remoteWatchCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.remoteWatchCancel = cancel
	epoch, target := a.epoch, a.opts.Target
	a.remoteState = "connecting"
	a.queue(func() tea.Msg {
		events, err := remote.NewClient(target.BaseURL, target.AuthToken).WatchChanges(ctx)
		return remoteWatchConnectedMsg{epoch, events, err}
	})
}
func remoteWatchCmd(epoch *int, events <-chan remote.ChangeEvent) tea.Cmd {
	return func() tea.Msg { event, ok := <-events; return remoteWatchEventMsg{epoch, event, events, ok} }
}
func (a *App) retryRemoteWatch() tea.Cmd {
	a.remoteState = "polling · reconnecting"
	epoch := a.epoch
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return remoteWatchRetryMsg{epoch} })
}
