package vault

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Note comments live beside the note as
// `.zennotes/comments/<note path>.comments.json` (#738). The record and
// its rules mirror shared-domain/note-comments.ts, which the desktop app,
// its MCP server and the Go server all follow: a comment is anchored to
// the text it was written on, carries an optional author (absent for the
// vault's owner) and an optional parentId that threads a reply under a
// top-level comment. Anchor offsets are UTF-16 code units, the unit the
// desktop's editor counts in, so the two sides agree on the same file.

const (
	NoteCommentsDir             = "comments"
	NoteCommentsSuffix          = ".comments.json"
	MaxCommentAnchorTextLength  = 500
	MaxCommentAuthorLength      = 80
	noteCommentsSidecarVersion  = 1
	noteCommentsInternalDirName = ".zennotes"
)

// NoteComment is one stored comment or reply.
type NoteComment struct {
	ID          string `json:"id"`
	NotePath    string `json:"notePath"`
	AnchorStart int    `json:"anchorStart"`
	AnchorEnd   int    `json:"anchorEnd"`
	AnchorText  string `json:"anchorText"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	ResolvedAt  *int64 `json:"resolvedAt"`
	Author      string `json:"author,omitempty"`
	ParentID    string `json:"parentId,omitempty"`
}

var commentSpaceRe = regexp.MustCompile(`\s+`)

// NewCommentID is a random UUID-shaped id.
func NewCommentID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", os.Getpid())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NormalizeCommentAuthor squeezes whitespace and caps the length.
func NormalizeCommentAuthor(value string) string {
	author := strings.TrimSpace(commentSpaceRe.ReplaceAllString(value, " "))
	if r := []rune(author); len(r) > MaxCommentAuthorLength {
		author = string(r[:MaxCommentAuthorLength])
	}
	return author
}

func normalizeAnchorText(value string) string {
	text := strings.TrimSpace(commentSpaceRe.ReplaceAllString(value, " "))
	if r := []rune(text); len(r) > MaxCommentAnchorTextLength {
		text = string(r[:MaxCommentAnchorTextLength])
	}
	return text
}

// NormalizeNoteComment validates one record; a comment with no body is
// dropped (ok false), everything else is coerced into shape.
func NormalizeNoteComment(input NoteComment, notePath string, now int64) (NoteComment, bool) {
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return NoteComment{}, false
	}
	start, end := max(0, input.AnchorStart), max(0, input.AnchorEnd)
	if end < start {
		start, end = end, start
	}
	c := NoteComment{
		ID:          strings.TrimSpace(input.ID),
		NotePath:    notePath,
		AnchorStart: start,
		AnchorEnd:   end,
		AnchorText:  normalizeAnchorText(input.AnchorText),
		Body:        body,
		CreatedAt:   input.CreatedAt,
		UpdatedAt:   input.UpdatedAt,
		ResolvedAt:  input.ResolvedAt,
		Author:      NormalizeCommentAuthor(input.Author),
		ParentID:    strings.TrimSpace(input.ParentID),
	}
	if c.ID == "" {
		c.ID = NewCommentID()
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = now
	}
	return c, true
}

// NormalizeNoteComments validates a whole list: drops malformed and
// duplicate records, keeps creation order, and turns a reply whose parent
// is missing into a comment of its own rather than losing it.
func NormalizeNoteComments(raw []NoteComment, notePath string, now int64) []NoteComment {
	seen := map[string]bool{}
	out := []NoteComment{}
	for _, value := range raw {
		c, ok := NormalizeNoteComment(value, notePath, now)
		if !ok || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	ids := map[string]bool{}
	for _, c := range out {
		ids[c.ID] = true
	}
	for i := range out {
		if p := out[i].ParentID; p != "" && (!ids[p] || p == out[i].ID) {
			out[i].ParentID = ""
		}
	}
	return out
}

// NoteCommentThread is a top-level comment with its replies in order.
type NoteCommentThread struct {
	Comment NoteComment
	Replies []NoteComment
}

// ThreadNoteComments groups comments into threads. A reply to a reply
// lands in the same thread, so a thread stays one level deep.
func ThreadNoteComments(comments []NoteComment) []NoteCommentThread {
	byID := map[string]NoteComment{}
	for _, c := range comments {
		byID[c.ID] = c
	}
	rootOf := func(c NoteComment) NoteComment {
		cur := c
		visited := map[string]bool{}
		for cur.ParentID != "" && !visited[cur.ID] {
			parent, ok := byID[cur.ParentID]
			if !ok {
				break
			}
			visited[cur.ID] = true
			cur = parent
		}
		return cur
	}
	threads := map[string]*NoteCommentThread{}
	order := []string{}
	for _, c := range comments {
		root := rootOf(c)
		if root.ID == c.ID {
			if _, ok := threads[c.ID]; !ok {
				threads[c.ID] = &NoteCommentThread{Comment: c}
			}
			order = append(order, c.ID)
			continue
		}
		t, ok := threads[root.ID]
		if !ok {
			t = &NoteCommentThread{Comment: root}
			threads[root.ID] = t
		}
		t.Replies = append(t.Replies, c)
	}
	out := make([]NoteCommentThread, 0, len(order))
	for _, id := range order {
		out = append(out, *threads[id])
	}
	return out
}

// ThreadRootOf is the top-level comment an id belongs to, or ok false.
func ThreadRootOf(comments []NoteComment, id string) (NoteComment, bool) {
	for _, t := range ThreadNoteComments(comments) {
		if t.Comment.ID == id {
			return t.Comment, true
		}
		for _, r := range t.Replies {
			if r.ID == id {
				return t.Comment, true
			}
		}
	}
	return NoteComment{}, false
}

// --- UTF-16 offsets, the editor's unit ---

// UTF16Length counts the UTF-16 code units of s.
func UTF16Length(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// utf16ToByteOffset maps a UTF-16 offset onto a byte offset, clamped.
func utf16ToByteOffset(s string, units int) int {
	if units <= 0 {
		return 0
	}
	count := 0
	for i, r := range s {
		if count >= units {
			return i
		}
		if r >= 0x10000 {
			count += 2
		} else {
			count++
		}
	}
	return len(s)
}

// ResolveCommentAnchor is where a comment's anchor sits in doc now: the
// stored offsets when the text there still matches, else the first
// occurrence of the anchored text, else the clamped stored offsets. The
// result is in UTF-16 units like the stored anchor.
func ResolveCommentAnchor(c NoteComment, doc string) (from, to int) {
	total := UTF16Length(doc)
	from = max(0, min(total, c.AnchorStart))
	to = max(from, min(total, c.AnchorEnd))
	selected := doc[utf16ToByteOffset(doc, from):utf16ToByteOffset(doc, to)]
	if c.AnchorText == "" || selected == c.AnchorText {
		return from, to
	}
	if at := strings.Index(doc, c.AnchorText); at >= 0 {
		start := UTF16Length(doc[:at])
		return start, start + UTF16Length(c.AnchorText)
	}
	return from, to
}

// LineOfOffset is the 1-based line of a UTF-16 offset in doc.
func LineOfOffset(doc string, units int) int {
	at := utf16ToByteOffset(doc, units)
	return 1 + strings.Count(doc[:at], "\n")
}

// ErrAnchorNotFound says anchor text is not in the note.
var ErrAnchorNotFound = errors.New("anchor_text was not found in the note. Pass the text exactly as it appears (read_note shows it), or omit it for a note-level comment.")

// AnchorForText places a new comment: an exact match of anchorText first,
// then one ignoring case; empty text means a note-level comment at the top.
func AnchorForText(doc, anchorText string) (start, end int, text string, err error) {
	wanted := strings.TrimSpace(anchorText)
	if wanted == "" {
		return 0, 0, "", nil
	}
	at := strings.Index(doc, wanted)
	if at < 0 {
		at = strings.Index(strings.ToLower(doc), strings.ToLower(wanted))
	}
	if at < 0 {
		return 0, 0, "", ErrAnchorNotFound
	}
	byteEnd := at + len(wanted)
	if byteEnd > len(doc) {
		byteEnd = len(doc)
	}
	start = UTF16Length(doc[:at])
	end = start + UTF16Length(doc[at:byteEnd])
	return start, end, normalizeAnchorText(doc[at:byteEnd]), nil
}

// --- the sidecar on disk ---

type noteCommentsEnvelope struct {
	Version  int           `json:"version"`
	Comments []NoteComment `json:"comments"`
}

// ReadNoteComments loads a note's comments; a missing sidecar is empty.
func (v *Vault) ReadNoteComments(rel string) ([]NoteComment, error) {
	notePath := filepath.ToSlash(NormalizeRelPath(rel))
	abs, err := v.commentsPath(rel)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []NoteComment{}, nil
		}
		return nil, err
	}
	now := time.Now().UnixMilli()
	var envelope noteCommentsEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Comments != nil {
		return NormalizeNoteComments(envelope.Comments, notePath, now), nil
	}
	var list []NoteComment
	if err := json.Unmarshal(raw, &list); err != nil {
		return []NoteComment{}, nil
	}
	return NormalizeNoteComments(list, notePath, now), nil
}

// WriteNoteComments replaces a note's comments; an empty list removes the
// sidecar.
func (v *Vault) WriteNoteComments(rel string, comments []NoteComment) ([]NoteComment, error) {
	notePath := filepath.ToSlash(NormalizeRelPath(rel))
	normalized := NormalizeNoteComments(comments, notePath, time.Now().UnixMilli())
	abs, err := v.commentsPath(rel)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return []NoteComment{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(noteCommentsEnvelope{Version: noteCommentsSidecarVersion, Comments: normalized}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return nil, err
	}
	return normalized, nil
}
