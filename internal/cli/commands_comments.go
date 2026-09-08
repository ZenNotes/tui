package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
)

// `zn comment`: a note discussed like an issue, the same four verbs the
// desktop CLI has (#738).

func commentWhen(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}

func commentWho(author *string) string {
	if author == nil || strings.TrimSpace(*author) == "" {
		return "You"
	}
	return *author
}

func printThread(t backend.CommentThreadView) {
	state := ""
	if t.Resolved {
		state = "  (resolved)"
	}
	emitLine(fmt.Sprintf("%s  %s  %s  line %d%s", t.ID, commentWho(t.Author), commentWhen(t.CreatedAt), t.Line, state))
	if t.AnchorText != "" {
		emitLine("  > " + t.AnchorText)
	}
	emitLine("  " + strings.ReplaceAll(t.Body, "\n", "\n  "))
	for _, r := range t.Replies {
		emitLine(fmt.Sprintf("    %s  %s  %s", r.ID, commentWho(r.Author), commentWhen(r.CreatedAt)))
		emitLine("      " + strings.ReplaceAll(r.Body, "\n", "\n      "))
	}
}

func commentPath(args Args, usage string) (string, error) {
	rel := strings.TrimSpace(args.Positional(0))
	if rel == "" {
		return "", errors.New("Usage: " + usage)
	}
	return rel, nil
}

// commentBody reads the body from --body, the positional, or stdin with
// `--body -`.
func commentBody(args Args, positional int, usage string) (string, error) {
	if flag, ok := args.String("body"); ok {
		if flag == "-" {
			flag = ReadStdin()
		}
		if strings.TrimSpace(flag) != "" {
			return flag, nil
		}
	}
	if v := strings.TrimSpace(args.Positional(positional)); v != "" {
		return v, nil
	}
	return "", errors.New("Usage: " + usage)
}

func cmdCommentList(ctx context.Context, b backend.Backend, args Args) error {
	rel, err := commentPath(args, "zn comment list <path> [--all] [--json]")
	if err != nil {
		return err
	}
	threads, err := backend.ListCommentThreads(ctx, b, rel, args.Bool("all"))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(threads)
		return nil
	}
	if len(threads) == 0 {
		if args.Bool("all") {
			emitLine("No comments.")
		} else {
			emitLine("No open comments. Pass --all to include resolved ones.")
		}
		return nil
	}
	for i, t := range threads {
		if i > 0 {
			emitLine("")
		}
		printThread(t)
	}
	return nil
}

func cmdCommentAdd(ctx context.Context, b backend.Backend, args Args) error {
	usage := `zn comment add <path> "<body>" [--anchor "<text from the note>"] [--author <name>]`
	rel, err := commentPath(args, usage)
	if err != nil {
		return err
	}
	body, err := commentBody(args, 1, usage)
	if err != nil {
		return err
	}
	thread, err := backend.AddComment(ctx, b, backend.AddCommentInput{Path: rel, Body: body, AnchorText: args.Str("anchor"), Author: args.Str("author")})
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(thread)
		return nil
	}
	where := ""
	if thread.AnchorText != "" {
		where = fmt.Sprintf(", line %d", thread.Line)
	}
	emitOK(fmt.Sprintf("Commented on %s (%s%s)", rel, thread.ID, where))
	return nil
}

func cmdCommentReply(ctx context.Context, b backend.Backend, args Args) error {
	usage := `zn comment reply <path> <id> "<body>" [--author <name>]`
	rel, err := commentPath(args, usage)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args.Str("id"))
	if id == "" {
		id = strings.TrimSpace(args.Positional(1))
	}
	if id == "" {
		return errors.New("Usage: " + usage)
	}
	body, err := commentBody(args, 2, usage)
	if err != nil {
		return err
	}
	thread, err := backend.ReplyToComment(ctx, b, backend.ReplyInput{Path: rel, ID: id, Body: body, Author: args.Str("author")})
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(thread)
		return nil
	}
	noun := "replies"
	if len(thread.Replies) == 1 {
		noun = "reply"
	}
	emitOK(fmt.Sprintf("Replied in %s on %s (%d %s)", thread.ID, rel, len(thread.Replies), noun))
	return nil
}

func cmdCommentResolve(ctx context.Context, b backend.Backend, args Args) error {
	usage := "zn comment resolve <path> <id> [--reopen]"
	rel, err := commentPath(args, usage)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args.Str("id"))
	if id == "" {
		id = strings.TrimSpace(args.Positional(1))
	}
	if id == "" {
		return errors.New("Usage: " + usage)
	}
	reopen := args.Bool("reopen")
	thread, err := backend.ResolveComment(ctx, b, rel, id, !reopen)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(thread)
		return nil
	}
	verb := "Resolved"
	if reopen {
		verb = "Reopened"
	}
	emitOK(fmt.Sprintf("%s %s on %s", verb, thread.ID, rel))
	return nil
}
