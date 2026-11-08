package vault

import (
	"os"
	"sort"
	"strings"
)

// EventListNotes gives the vault path of each live note whose frontmatter
// says `page-type: event-list`, in path order.
func (v *Vault) EventListNotes() []string {
	var out []string
	v.walkNotes(LiveFolders, false, func(f noteFile) {
		body, err := os.ReadFile(f.abs)
		if err == nil && IsEventListNote(string(body)) {
			out = append(out, v.relPosix(f.abs))
		}
	})
	sort.Strings(out)
	return out
}

// SetTaskBlockID ends the task line at taskIndex with a new block id, when
// the line has no block id yet. newID gives the id and runs only when the
// line needs one. A line that already has a block id stays as it is. It
// returns the new body, the block id that the line now ends with, and false
// when the body has no task at that index.
func SetTaskBlockID(body string, taskIndex int, newID func() string) (next, blockID string, ok bool) {
	existing := ""
	editTaskAtIndex(body, taskIndex, func(m []string) (string, bool) {
		_, existing = splitBlockID(strings.TrimRight(m[3], "\r"))
		return "", false
	})
	if existing != "" {
		return body, existing, true
	}
	id := ""
	next, ok = editTaskAtIndex(body, taskIndex, func(m []string) (string, bool) {
		id = newID()
		tail, cr := strings.TrimSuffix(m[3], "\r"), ""
		if tail != m[3] {
			cr = "\r"
		}
		return m[1] + m[2] + strings.TrimRight(tail, " \t") + " ^" + id + cr, true
	})
	if !ok {
		return body, "", false
	}
	return next, id, true
}

// SessionLine is the list line of a work session: the text, a link to the
// task and the `when:` value.
func SessionLine(text, link, when string) string {
	return "- " + strings.TrimSpace(text) + " task:" + link + " when:" + when
}

// AppendLine puts line at the end of body as a new last line. A last line
// that has no line ending gets one first. Line endings stay as the note has
// them.
func AppendLine(body, line string) string {
	eol := "\n"
	if strings.Contains(body, "\r\n") {
		eol = "\r\n"
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += eol
	}
	return body + line + eol
}
