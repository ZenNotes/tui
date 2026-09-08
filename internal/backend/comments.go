package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// Comment operations shared by the CLI and the MCP server, mirroring the
// desktop's comment-ops.ts: threads are read against the note body so
// each carries the line its anchor sits on now.

// CommentView is one comment or reply as tools and commands present it.
type CommentView struct {
	ID        string  `json:"id"`
	Author    *string `json:"author"`
	Body      string  `json:"body"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
}

// CommentThreadView is a top-level comment with its replies.
type CommentThreadView struct {
	CommentView
	AnchorText string        `json:"anchorText"`
	Line       int           `json:"line"`
	Resolved   bool          `json:"resolved"`
	ResolvedAt *int64        `json:"resolvedAt"`
	Replies    []CommentView `json:"replies"`
}

func viewOf(c vault.NoteComment) CommentView {
	v := CommentView{ID: c.ID, Body: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if c.Author != "" {
		author := c.Author
		v.Author = &author
	}
	return v
}

// ListCommentThreads reads a note's threads, unresolved ones unless
// includeResolved.
func ListCommentThreads(ctx context.Context, b Backend, rel string, includeResolved bool) ([]CommentThreadView, error) {
	note, err := b.ReadNote(ctx, rel)
	if err != nil {
		return nil, err
	}
	comments, err := b.ListComments(ctx, rel)
	if err != nil {
		return nil, err
	}
	out := []CommentThreadView{}
	for _, t := range vault.ThreadNoteComments(comments) {
		if !includeResolved && t.Comment.ResolvedAt != nil {
			continue
		}
		from, _ := vault.ResolveCommentAnchor(t.Comment, note.Body)
		view := CommentThreadView{
			CommentView: viewOf(t.Comment),
			AnchorText:  t.Comment.AnchorText,
			Line:        vault.LineOfOffset(note.Body, from),
			Resolved:    t.Comment.ResolvedAt != nil,
			ResolvedAt:  t.Comment.ResolvedAt,
			Replies:     []CommentView{},
		}
		for _, r := range t.Replies {
			view.Replies = append(view.Replies, viewOf(r))
		}
		out = append(out, view)
	}
	return out, nil
}

func threadByID(ctx context.Context, b Backend, rel, id string) (CommentThreadView, error) {
	threads, err := ListCommentThreads(ctx, b, rel, true)
	if err != nil {
		return CommentThreadView{}, err
	}
	for _, t := range threads {
		if t.ID == id {
			return t, nil
		}
	}
	return CommentThreadView{}, fmt.Errorf("Thread %s vanished on %s", id, rel)
}

// AddCommentInput starts a thread.
type AddCommentInput struct {
	Path       string
	Body       string
	AnchorText string
	Author     string
}

// AddComment starts a thread, anchored to text from the note when given.
func AddComment(ctx context.Context, b Backend, in AddCommentInput) (CommentThreadView, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return CommentThreadView{}, errors.New("body must not be empty")
	}
	note, err := b.ReadNote(ctx, in.Path)
	if err != nil {
		return CommentThreadView{}, err
	}
	start, end, text, err := vault.AnchorForText(note.Body, in.AnchorText)
	if err != nil {
		return CommentThreadView{}, err
	}
	current, err := b.ListComments(ctx, in.Path)
	if err != nil {
		return CommentThreadView{}, err
	}
	now := time.Now().UnixMilli()
	draft := vault.NoteComment{
		ID:          vault.NewCommentID(),
		NotePath:    in.Path,
		AnchorStart: start,
		AnchorEnd:   end,
		AnchorText:  text,
		Body:        body,
		Author:      vault.NormalizeCommentAuthor(in.Author),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := b.WriteComments(ctx, in.Path, append(current, draft)); err != nil {
		return CommentThreadView{}, err
	}
	return threadByID(ctx, b, in.Path, draft.ID)
}

// ReplyInput answers in a thread.
type ReplyInput struct {
	Path   string
	ID     string
	Body   string
	Author string
}

// ReplyToComment answers a thread; id may be the thread or any reply in it.
func ReplyToComment(ctx context.Context, b Backend, in ReplyInput) (CommentThreadView, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return CommentThreadView{}, errors.New("body must not be empty")
	}
	current, err := b.ListComments(ctx, in.Path)
	if err != nil {
		return CommentThreadView{}, err
	}
	root, ok := vault.ThreadRootOf(current, in.ID)
	if !ok {
		return CommentThreadView{}, fmt.Errorf("No comment with id %s on %s. Use list_comments to find ids.", in.ID, in.Path)
	}
	now := time.Now().UnixMilli()
	draft := vault.NoteComment{
		ID:          vault.NewCommentID(),
		NotePath:    in.Path,
		AnchorStart: root.AnchorStart,
		AnchorEnd:   root.AnchorEnd,
		AnchorText:  root.AnchorText,
		Body:        body,
		Author:      vault.NormalizeCommentAuthor(in.Author),
		ParentID:    root.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := b.WriteComments(ctx, in.Path, append(current, draft)); err != nil {
		return CommentThreadView{}, err
	}
	return threadByID(ctx, b, in.Path, root.ID)
}

// ResolveComment marks a thread resolved, or reopens it.
func ResolveComment(ctx context.Context, b Backend, rel, id string, resolved bool) (CommentThreadView, error) {
	current, err := b.ListComments(ctx, rel)
	if err != nil {
		return CommentThreadView{}, err
	}
	root, ok := vault.ThreadRootOf(current, id)
	if !ok {
		return CommentThreadView{}, fmt.Errorf("No comment with id %s on %s. Use list_comments to find ids.", id, rel)
	}
	now := time.Now().UnixMilli()
	for i := range current {
		if current[i].ID == root.ID {
			if resolved {
				at := now
				current[i].ResolvedAt = &at
			} else {
				current[i].ResolvedAt = nil
			}
			current[i].UpdatedAt = now
		}
	}
	if _, err := b.WriteComments(ctx, rel, current); err != nil {
		return CommentThreadView{}, err
	}
	return threadByID(ctx, b, rel, root.ID)
}
