package vault

import (
	"errors"
	"strings"
)

// The body transforms below are pure, so the remote backend applies the
// identical edit to a body it fetched over HTTP and `zn append` means one
// thing wherever the vault lives.

func trimTrailingNewlines(s string) string {
	return strings.TrimRight(s, "\n")
}

// AppendToBody is the body with text added after a blank line.
func AppendToBody(body, text string) string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	sep := ""
	if !strings.HasSuffix(normalized, "\n") && len(normalized) != 0 {
		sep = "\n"
	}
	blank := ""
	if len(normalized) > 0 {
		blank = "\n"
	}
	return normalized + sep + blank + trimTrailingNewlines(text) + "\n"
}

// PrependToBody is the body with text inserted at the top, below any
// frontmatter.
func PrependToBody(body, text string) string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	snippet := trimTrailingNewlines(text) + "\n\n"
	if m := frontmatterRe.FindStringIndex(normalized); m != nil {
		return normalized[:m[1]] + snippet + normalized[m[1]:]
	}
	return snippet + normalized
}

// ReplaceInBody is a literal find-and-replace; all replaces every
// occurrence, otherwise only the first.
func ReplaceInBody(body, find, replace string, all bool) (string, int, error) {
	if find == "" {
		return body, 0, errors.New("find is required")
	}
	if all {
		count := strings.Count(body, find)
		return strings.ReplaceAll(body, find, replace), count, nil
	}
	idx := strings.Index(body, find)
	if idx < 0 {
		return body, 0, nil
	}
	return body[:idx] + replace + body[idx+len(find):], 1, nil
}

// InsertAtLineInBody inserts text before the zero-based lineNumber, clamped
// to the body.
func InsertAtLineInBody(body string, lineNumber int, text string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	clamped := max(0, min(len(lines), lineNumber))
	inserted := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+len(inserted))
	out = append(out, lines[:clamped]...)
	out = append(out, inserted...)
	out = append(out, lines[clamped:]...)
	return strings.Join(out, "\n")
}
