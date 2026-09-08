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

func queryFrom(args Args) string {
	if q, ok := args.String("query"); ok {
		return q
	}
	return strings.Join(args.Positionals, " ")
}

func cmdSearch(ctx context.Context, b backend.Backend, args Args) error {
	query := queryFrom(args)
	if strings.TrimSpace(query) == "" {
		return errors.New("zn search requires a query.")
	}
	limit := args.IntOr("limit", 50)
	matches, err := b.SearchText(ctx, strings.TrimSpace(query), limit)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(matches)
		return nil
	}
	if len(matches) == 0 {
		emitLine(fmt.Sprintf("No matches for %q.", query))
		return nil
	}
	for _, m := range matches {
		emitLine(fmt.Sprintf("%s  %s", pad(fmt.Sprintf("%s:%d", m.Path, m.LineNumber), 48), truncate(m.LineText, 100)))
	}
	return nil
}

func cmdSearchTitle(ctx context.Context, b backend.Backend, args Args) error {
	query := queryFrom(args)
	if strings.TrimSpace(query) == "" {
		return errors.New("zn search-title requires a query.")
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	limit := args.IntOr("limit", 20)
	all, err := b.ListNotes(ctx)
	if err != nil {
		return err
	}
	matches := []vault.NoteMeta{}
	for _, n := range all {
		if n.Folder != vault.FolderTrash && strings.Contains(strings.ToLower(n.Title), needle) {
			matches = append(matches, n)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].UpdatedAt > matches[j].UpdatedAt })
	if len(matches) > limit {
		matches = matches[:limit]
	}
	if args.Bool("json") {
		emitJSON(matches)
		return nil
	}
	if len(matches) == 0 {
		emitLine(fmt.Sprintf("No notes matching %q.", query))
		return nil
	}
	for _, n := range matches {
		emitLine(n.Path)
	}
	return nil
}

func cmdBacklinks(ctx context.Context, b backend.Backend, args Args) error {
	rel := args.Str("path")
	if rel == "" {
		rel = args.Positional(0)
	}
	if rel == "" {
		return errors.New("zn backlinks requires a note path.")
	}
	refs, err := b.Backlinks(ctx, rel)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(refs)
		return nil
	}
	if len(refs) == 0 {
		emitLine(fmt.Sprintf("No notes link to %s.", rel))
		return nil
	}
	for _, ref := range refs {
		emitLine(ref.Path)
	}
	return nil
}
