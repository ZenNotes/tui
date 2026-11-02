package mcp

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/search"
)

// Scoped access (ZenNotes/tui#21, docs/ideas/mcp-scoped-access.md): what an
// MCP client may see and change, fixed when the server starts from its own
// launch flags. No tool argument can name or widen a scope; tools only narrow
// inside it. The rules match the desktop's bundled server so a client gets
// the same answers whichever binary its config launches.

// Access is how much a client may do to what it can see.
type Access int

const (
	// AccessRead offers the read tools only.
	AccessRead Access = iota
	// AccessComment adds the comment tools.
	AccessComment
	// AccessWrite is every tool the paths allow.
	AccessWrite
)

func (a Access) String() string {
	switch a {
	case AccessRead:
		return "read"
	case AccessComment:
		return "comment"
	default:
		return "write"
	}
}

// ParseAccess reads `read`, `comment` or `write`.
func ParseAccess(raw string) (Access, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "read", "read-only", "readonly":
		return AccessRead, nil
	case "comment":
		return AccessComment, nil
	case "write", "edit":
		return AccessWrite, nil
	}
	return AccessRead, fmt.Errorf("--access must be read, comment or write, not %q", raw)
}

// Scope is an access level plus the paths a client may see. Allow empty means
// the whole vault; a non-empty Allow shows only what it names (default-deny),
// and Deny hides paths even inside it.
type Scope struct {
	Access Access
	Allow  []string
	Deny   []string
}

// Restricted says whether the scope narrows the paths at all.
func (s Scope) Restricted() bool { return len(s.Allow) > 0 || len(s.Deny) > 0 }

// Active says whether the scope changes anything compared with full access.
func (s Scope) Active() bool { return s.Restricted() || s.Access != AccessWrite }

// NewScope builds a scope from `zn mcp` flags. readOnly lowers access to read
// whatever access says; an empty access is write, today's behavior.
func NewScope(access string, readOnly bool, allow, deny []string) (Scope, error) {
	s := Scope{Access: AccessWrite}
	if strings.TrimSpace(access) != "" {
		a, err := ParseAccess(access)
		if err != nil {
			return Scope{}, err
		}
		s.Access = a
	}
	if readOnly {
		s.Access = AccessRead
	}
	var err error
	if s.Allow, err = normalizeScopeEntries("--allow-path", allow); err != nil {
		return Scope{}, err
	}
	if s.Deny, err = normalizeScopeEntries("--deny-path", deny); err != nil {
		return Scope{}, err
	}
	return s, nil
}

// normalizeScopeEntries validates entries the way the Tasks folder lists are
// validated (forward slashes, trimmed segments, no `.` or `..`, at most 512
// characters, duplicates dropped), and refuses hidden entries, which never
// hold anything a client can see. A leading slash is accepted: an entry is
// always vault-relative. A bad entry is an error, never a silent widening.
func normalizeScopeEntries(flag string, values []string) ([]string, error) {
	out := []string{}
	seen := map[string]struct{}{}
	for _, raw := range values {
		parts := []string{}
		for _, seg := range strings.Split(strings.ReplaceAll(raw, "\\", "/"), "/") {
			s := strings.TrimSpace(seg)
			if s == "" {
				continue
			}
			if s == "." || s == ".." {
				return nil, fmt.Errorf("%s %q: . and .. are not allowed; name a folder or note inside the vault", flag, raw)
			}
			if strings.HasPrefix(s, ".") {
				return nil, fmt.Errorf("%s %q names a hidden folder, which an assistant never sees", flag, raw)
			}
			if strings.ContainsRune(s, 0) {
				return nil, fmt.Errorf("%s %q is not a valid path", flag, raw)
			}
			parts = append(parts, s)
		}
		if len(parts) == 0 {
			return nil, fmt.Errorf("%s %q is empty; name a folder or note inside the vault", flag, raw)
		}
		joined := strings.Join(parts, "/")
		if len(joined) > 512 {
			return nil, fmt.Errorf("%s %q is longer than 512 characters", flag, raw)
		}
		if _, dup := seen[joined]; dup {
			continue
		}
		seen[joined] = struct{}{}
		out = append(out, joined)
	}
	return out, nil
}

// inside is the segment-prefix rule of the Tasks folder lists: `Projects`
// covers `Projects/x.md`, never `Projects2/x.md`.
func inside(p, entry string) bool {
	return p == entry || strings.HasPrefix(p, entry+"/")
}

// insideFolded is inside after case and accent folding. Deny entries match
// this way on purpose: on a case- or normalization-insensitive volume a
// near-miss must over-hide, never leak.
func insideFolded(p, entry string) bool {
	return inside(search.Fold(p), search.Fold(entry))
}

// --- tools ---

type toolClass int

const (
	classRead toolClass = iota
	classComment
	classWrite
	// classUnscopedWrite acts on content regardless of path (empty_trash
	// deletes every trashed note), so no path-restricted client gets it.
	classUnscopedWrite
)

// toolClasses says what each tool needs. A tool missing here is treated as
// classUnscopedWrite, and a test fails until it is classified.
var toolClasses = map[string]toolClass{
	"vault_info":         classRead,
	"list_notes":         classRead,
	"list_folders":       classRead,
	"list_assets":        classRead,
	"read_note":          classRead,
	"search_text":        classRead,
	"search_by_title":    classRead,
	"search_by_tag":      classRead,
	"list_tags":          classRead,
	"backlinks":          classRead,
	"list_tasks":         classRead,
	"list_comments":      classRead,
	"add_comment":        classComment,
	"reply_to_comment":   classComment,
	"resolve_comment":    classComment,
	"toggle_task":        classWrite,
	"write_note":         classWrite,
	"append_to_note":     classWrite,
	"prepend_to_note":    classWrite,
	"insert_at_line":     classWrite,
	"replace_in_note":    classWrite,
	"create_note":        classWrite,
	"rename_note":        classWrite,
	"move_note":          classWrite,
	"duplicate_note":     classWrite,
	"archive_note":       classWrite,
	"unarchive_note":     classWrite,
	"move_to_trash":      classWrite,
	"restore_from_trash": classWrite,
	"delete_note":        classWrite,
	"create_folder":      classWrite,
	"rename_folder":      classWrite,
	"delete_folder":      classWrite,
	"empty_trash":        classUnscopedWrite,
}

// destructiveTools may destroy content rather than add to it or move it.
var destructiveTools = map[string]bool{
	"write_note":      true,
	"replace_in_note": true,
	"delete_note":     true,
	"delete_folder":   true,
	"empty_trash":     true,
}

func classOf(name string) toolClass {
	if c, ok := toolClasses[name]; ok {
		return c
	}
	return classUnscopedWrite
}

// toolAllowed says whether a scope offers a tool. A nil scope is full access.
func toolAllowed(name string, s *Scope) bool {
	if s == nil {
		return true
	}
	switch classOf(name) {
	case classRead:
		return true
	case classComment:
		return s.Access >= AccessComment
	case classWrite:
		return s.Access >= AccessWrite
	default:
		return s.Access >= AccessWrite && !s.Restricted()
	}
}

// toolAnnotations tells clients which tools only read and which can destroy,
// so a client that confirms destructive calls can. The vault is a closed
// world.
func toolAnnotations(name string) *sdk.ToolAnnotations {
	closed := false
	if classOf(name) == classRead {
		return &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closed}
	}
	destructive := destructiveTools[name]
	return &sdk.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed}
}

// accessError answers a call to a tool above the scope's access level. It
// names no path, so it says nothing about the vault.
func accessError(s Scope) error {
	switch s.Access {
	case AccessRead:
		return errors.New("This assistant can only read this vault.")
	case AccessComment:
		return errors.New("This assistant can only read this vault and comment on notes.")
	}
	return errors.New("This assistant cannot use that tool on a vault it only partly sees.")
}

// scopeInstructions is appended to the server instructions while a scope is
// active, so the model neither guesses at hidden notes nor tries tools it
// does not have.
func scopeInstructions(s Scope) string {
	text := "\n\n## Access\n\n"
	if s.Restricted() {
		text += "This server shows part of the user's vault. Paths and notes you are not shown do not exist for you: do not guess at them, and treat a [[link]] that does not resolve as pointing outside what you can see. "
	}
	switch s.Access {
	case AccessRead:
		text += "You can read but not change anything. "
	case AccessComment:
		text += "You can read and comment on notes, but not change them. "
	}
	return text + "Call `vault_info` to see what you can reach."
}

// describeScope is the scope as vault_info reports it. Deny entries are never
// named: the names alone can say too much.
func describeScope(s Scope, allow []string, missingAllow []string, blocked error) orderedResult {
	allowOut := append([]string{}, allow...)
	sort.Strings(allowOut)
	missing := append([]string{}, missingAllow...)
	sort.Strings(missing)
	result := orderedResult{
		{"level", s.Access.String()},
		{"partial", s.Restricted()},
		{"allow", allowOut},
		{"missingAllow", missing},
	}
	if blocked != nil {
		result = append(result, struct {
			key   string
			value any
		}{"blocked", blocked.Error()})
	}
	return result
}
