package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/ZenNotes/tui/internal/releases"
	"github.com/ZenNotes/tui/internal/server"
)

func cmdServer(ctx context.Context, action string, args Args) error {
	m := server.DefaultManager()
	if action == "list" {
		if len(args.Positionals) != 0 {
			return fmt.Errorf("usage: zn server list")
		}
		items, err := m.List()
		if err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(items)
		} else if len(items) == 0 {
			emitLine("No managed servers. `zn server setup home --vault ~/Notes` installs one on this machine.")
		} else {
			for _, i := range items {
				emitLine(fmt.Sprintf("%s\t%s\t%s\t%s", i.Name, i.Version, i.URL(), i.Vault))
			}
		}
		return nil
	}
	if len(args.Positionals) != 1 {
		return fmt.Errorf("usage: zn server %s <name> (see --help)", action)
	}
	name := args.Positional(0)
	if action == "run" {
		return m.Run(ctx, name, stderr)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var i server.Instance
	var err error
	switch action {
	case "setup", "install":
		opts := server.Options{Name: name, Vault: args.Str("vault"), Bind: args.Str("bind"), BasePath: args.Str("base-path"), Version: args.Str("version"), NoStart: args.Bool("no-start") || action == "install", NoDefault: args.Bool("no-default")}
		i, err = m.Setup(ctx, opts)
	case "start":
		err = m.Start(ctx, name)
	case "stop":
		err = m.Stop(ctx, name)
	case "restart":
		err = m.Stop(ctx, name)
		if err == nil {
			err = m.Start(ctx, name)
		}
	case "status":
		status, err := m.Status(ctx, name)
		if err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(status)
		} else {
			emitLine(fmt.Sprintf("%s %s — %s\nActive: %t  Healthy: %t\nVault: %s", name, status.Version, status.URL, status.Active, status.Healthy, status.Vault))
			if status.Error != "" {
				emitLine(status.Error)
			}
		}
		return nil
	case "logs":
		return m.Logs(name, stdout)
	case "config":
		changes := map[string]string{}
		for _, key := range []string{"vault", "bind", "base-path"} {
			if value, ok := args.String(key); ok {
				changes[key] = value
			}
		}
		if len(changes) == 0 {
			i, err = m.Load(name)
		} else {
			i, err = m.Configure(ctx, name, changes)
		}
	case "update":
		if args.Bool("rollback") && (args.Bool("check") || args.Str("version") != "") {
			return fmt.Errorf("--rollback cannot be combined with --check or --version")
		}
		if args.Bool("check") {
			i, err = m.Load(name)
			if err != nil {
				return err
			}
			r, err := m.Releases.Lookup(ctx, releases.Server, args.Str("version"))
			if err != nil {
				return err
			}
			if args.Bool("json") {
				emitJSON(map[string]any{"name": name, "current": i.Version, "available": r.Version(), "updateAvailable": releases.Newer(r.Version(), i.Version)})
			} else {
				emitLine(fmt.Sprintf("%s: installed %s; release %s", name, i.Version, r.Version()))
			}
			return nil
		}
		if args.Bool("rollback") {
			i, err = m.Rollback(ctx, name)
		} else {
			i, err = m.Update(ctx, name, args.Str("version"))
		}
	default:
		return fmt.Errorf("unknown server action %q", action)
	}
	if err != nil {
		return err
	}
	if i.Name == "" {
		i, err = m.Load(name)
		if err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(i)
	} else {
		emitOK(fmt.Sprintf("%s: %s complete — version %s, %s", i.Name, action, i.Version, i.URL()))
	}
	return nil
}
