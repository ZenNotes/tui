package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/releases"
	"github.com/ZenNotes/tui/internal/server"
)

type managedServerResult struct {
	reader *textReader
	body   string
	err    error
}

func (a *App) managedOperation(title string, work func(context.Context) (string, error)) {
	if a.managedServerBusy {
		a.notifyError("A managed server operation is still running")
		return
	}
	a.managedServerBusy = true
	r := &textReader{title: title, body: "Working… You can dismiss this panel and keep editing. The result will appear when ready."}
	a.overlay = r
	ctx := a.ctx
	a.queue(func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		body, err := work(ctx)
		return managedServerResult{reader: r, body: body, err: err}
	})
}

func (a *App) finishManagedOperation(result managedServerResult) {
	a.managedServerBusy = false
	if result.err != nil {
		result.reader.body = result.err.Error()
		a.notifyError(result.reader.title + ": " + result.err.Error())
	} else {
		result.reader.body = result.body
		a.notify(result.reader.title + " complete")
	}
}

func managedJSON(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	return string(raw), err
}

func (a *App) managedServers() {
	m := server.DefaultManager()
	instances, err := m.List()
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	items := []paletteItem{{label: "＋ Install a server on this machine…", id: "install"}}
	for _, i := range instances {
		items = append(items, paletteItem{id: i.Name, label: i.Name, detail: i.URL(), hint: i.Version, data: i})
	}
	a.overlay = &palette{title: "Managed servers · this machine", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		if it.id == "install" {
			a.managedSetup()
		} else {
			a.managedServerMenu(it.data.(server.Instance))
		}
	}}
}

func (a *App) managedSetup() {
	a.promptFor("Server name", "home", "A unique name for this local instance", func(a *App, name string) {
		initial := "~/Notes"
		if a.opts.Target.Kind == backend.KindLocal {
			initial = a.opts.Target.Root
		}
		a.promptFor("Vault folder", initial, "Notes to serve; runtime files and tokens are stored separately", func(a *App, vault string) {
			a.promptFor("Listen address", "127.0.0.1:7878", "IP:port · loopback keeps the server on this machine", func(a *App, bind string) {
				a.managedOperation("Install server "+name, func(ctx context.Context) (string, error) {
					i, err := server.DefaultManager().Setup(ctx, server.Options{Name: name, Vault: vault, Bind: bind, NoDefault: true})
					return managedJSON(i, err)
				})
			})
		})
	})
}

func (a *App) managedServerMenu(i server.Instance) {
	m := server.DefaultManager()
	a.showMenu("Server "+i.Name, []menuItem{
		{key: "s", label: "Status and authenticated health", run: func(a *App) {
			a.managedOperation("Server status", func(ctx context.Context) (string, error) { s, err := m.Status(ctx, i.Name); return managedJSON(s, err) })
		}},
		{key: "c", label: "Connect to this vault", run: func(a *App) {
			a.switchTarget(backend.Target{Kind: backend.KindRemote, Name: i.Name, BaseURL: i.URL(), AuthToken: config.LoadToken(i.URL())})
		}},
		{key: "a", label: "Start service", run: func(a *App) {
			a.managedOperation("Start server", func(ctx context.Context) (string, error) {
				if err := m.Start(ctx, i.Name); err != nil {
					return "", err
				}
				return "Running at " + i.URL(), m.Register(i, false)
			})
		}},
		{key: "x", label: "Stop service", run: func(a *App) {
			a.managedOperation("Stop server", func(ctx context.Context) (string, error) { return "Server stopped", m.Stop(ctx, i.Name) })
		}},
		{key: "r", label: "Restart service", run: func(a *App) {
			a.managedOperation("Restart server", func(ctx context.Context) (string, error) {
				if err := m.Stop(ctx, i.Name); err != nil {
					return "", err
				}
				return "Server restarted", m.Start(ctx, i.Name)
			})
		}},
		{key: "l", label: "View recent logs", run: func(a *App) {
			a.managedOperation("Server logs", func(context.Context) (string, error) {
				var b strings.Builder
				err := m.Logs(i.Name, &b)
				return b.String(), err
			})
		}},
		{key: "v", label: "Check for updates", run: func(a *App) {
			a.managedOperation("Server release check", func(ctx context.Context) (string, error) {
				r, err := m.Releases.Lookup(ctx, releases.Server, "")
				return fmt.Sprintf("Installed: %s\nAvailable: %s", i.Version, r.Version()), err
			})
		}},
		{key: "u", label: "Update to latest or a version…", run: func(a *App) {
			a.promptFor("Server version", "latest", "Stable version, e.g. 2.56.0. The prior version is retained for rollback.", func(a *App, version string) {
				a.managedOperation("Update server", func(ctx context.Context) (string, error) {
					i, err := m.Update(ctx, i.Name, version)
					return managedJSON(i, err)
				})
			})
		}},
		{key: "b", label: "Roll back to previous version", run: func(a *App) {
			a.managedOperation("Roll back server", func(ctx context.Context) (string, error) {
				i, err := m.Rollback(ctx, i.Name)
				return managedJSON(i, err)
			})
		}},
		{key: "e", label: "Change listen address…", run: func(a *App) {
			a.promptFor("Listen address", i.Bind, "IP:port; verified before accepting the change", func(a *App, bind string) {
				a.managedOperation("Configure server", func(ctx context.Context) (string, error) {
					i, err := m.Configure(ctx, i.Name, map[string]string{"bind": bind})
					return managedJSON(i, err)
				})
			})
		}},
	})
}
