package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

func cmdTaskList(ctx context.Context, b backend.Backend, args Args) error {
	showAll := args.Bool("all")
	onlyUnchecked := args.Bool("unchecked")
	includeExcluded := args.Bool("include-excluded")
	tag := strings.ToLower(strings.TrimPrefix(args.Str("tag"), "#"))
	tasks, err := b.ScanTasks(ctx, vault.ParseTasksOptions{IncludeExcluded: includeExcluded, Dialect: vault.DialectCLI})
	if err != nil {
		return err
	}
	filtered := []vault.Task{}
	for _, t := range tasks {
		if !showAll {
			// A forwarded record's live copy sits in the destination note
			// and lists on its own, so showing both doubled every carried
			// task. `--all` still surfaces the records.
			if onlyUnchecked {
				if t.Checked || t.Forwarded {
					continue
				}
			} else if t.Checked || t.Waiting || t.Forwarded {
				continue
			}
		}
		if tag != "" && !hasTag(t.Tags, tag) {
			continue
		}
		filtered = append(filtered, t)
	}
	if args.Bool("json") {
		emitJSON(filtered)
		return nil
	}
	if len(filtered) == 0 {
		emitLine("No tasks found.")
		return nil
	}
	for _, t := range filtered {
		box := "[ ]"
		switch {
		case t.Checked:
			box = "[x]"
		case t.Forwarded:
			box = "[>]"
		case t.Cancelled:
			box = "[-]"
		case t.InProgress:
			box = "[/]"
		case t.Waiting:
			box = "[~]"
		}
		due := ""
		if t.Due != "" {
			due = "  due:" + t.Due
		}
		pri := ""
		if t.Priority != "" {
			pri = "  !" + t.Priority
		}
		emitLine(fmt.Sprintf("%s  %s  %s%s%s", box, pad(t.ID, 40), truncate(t.Content, 80), due, pri))
	}
	return nil
}

func cmdTaskToggle(ctx context.Context, b backend.Backend, args Args) error {
	id := args.Str("id")
	if id == "" {
		id = args.Positional(0)
	}
	if id == "" {
		return errors.New("zn task toggle requires a task id from `zn task list`.")
	}
	next, err := b.ToggleTask(ctx, id, vault.DialectCLI)
	if err != nil {
		return err
	}
	if next == nil {
		return fmt.Errorf("Task %s no longer exists at that location, the file may have changed. Run `zn task list` again.", id)
	}
	if args.Bool("json") {
		emitJSON(next)
		return nil
	}
	state := "open"
	switch {
	case next.Checked:
		state = "done"
	case next.InProgress:
		state = "in progress"
	case next.Waiting:
		state = "waiting"
	}
	emitOK(fmt.Sprintf("Toggled %s → %s", id, state))
	return nil
}

type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// TagCounts tallies every tag across live notes, most used first.
func TagCounts(notes []vault.NoteMeta) []tagCount {
	counts := map[string]int{}
	for _, n := range notes {
		if n.Folder == vault.FolderTrash {
			continue
		}
		for _, t := range n.Tags {
			counts[strings.ToLower(t)]++
		}
	}
	out := make([]tagCount, 0, len(counts))
	for tag, count := range counts {
		out = append(out, tagCount{Tag: tag, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

func cmdTagList(ctx context.Context, b backend.Backend, args Args) error {
	all, err := b.ListNotes(ctx)
	if err != nil {
		return err
	}
	ordered := TagCounts(all)
	if args.Bool("json") {
		emitJSON(ordered)
		return nil
	}
	if len(ordered) == 0 {
		emitLine("No tags in vault.")
		return nil
	}
	widest := 0
	for _, t := range ordered {
		widest = max(widest, len([]rune(t.Tag)))
	}
	for _, t := range ordered {
		emitLine(fmt.Sprintf("#%s  %d", pad(t.Tag, widest), t.Count))
	}
	return nil
}

func cmdTagFind(ctx context.Context, b backend.Backend, args Args) error {
	tag := args.Str("tag")
	if tag == "" {
		tag = args.Positional(0)
	}
	tag = strings.ToLower(strings.TrimPrefix(tag, "#"))
	if tag == "" {
		return errors.New("zn tag find requires a tag name (e.g. `zn tag find idea`).")
	}
	limit := args.IntOr("limit", 200)
	all, err := b.ListNotes(ctx)
	if err != nil {
		return err
	}
	matches := []vault.NoteMeta{}
	for _, n := range all {
		if n.Folder != vault.FolderTrash && hasTag(n.Tags, tag) {
			matches = append(matches, n)
		}
	}
	sortByUpdated(matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}
	if args.Bool("json") {
		emitJSON(matches)
		return nil
	}
	if len(matches) == 0 {
		emitLine(fmt.Sprintf("No notes tagged #%s.", tag))
		return nil
	}
	for _, n := range matches {
		emitLine(n.Path)
	}
	return nil
}
