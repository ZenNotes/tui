package vault

import (
	"regexp"
	"strings"
)

var eventNoteWhenRe = regexp.MustCompile(`(?i)^(when\s*:\s*)(.*?)(\r?)$`)

// SetEventWhen writes a new `when` value for an event. An inline event has
// only its `when:` token replaced. An event note has only the value of its
// `when` key replaced, with the quotes it had. The event gives the place and
// the value that it was read with. The function returns false and the body
// unchanged when the body no longer holds that event there.
func SetEventWhen(body string, e Event, when string) (string, bool) {
	if e.Kind == EventKindNote {
		return setEventNoteWhen(body, e, when)
	}
	lines := strings.Split(body, "\n")
	line, ok := inlineEventLine(lines, e)
	if !ok {
		return body, false
	}
	cr := ""
	if strings.HasSuffix(line, "\r") {
		line, cr = strings.TrimSuffix(line, "\r"), "\r"
	}
	masked := inlineCodeRe.ReplaceAllStringFunc(line, func(s string) string {
		return strings.Repeat(" ", len(s))
	})
	m := eventWhenRe.FindStringSubmatchIndex(masked)
	if m == nil {
		return body, false
	}
	lines[e.Line] = line[:m[2]] + when + line[m[3]:] + cr
	return strings.Join(lines, "\n"), true
}

// DeleteEventLine removes the list line of an inline event and no other
// line. It returns false and the body unchanged when the body no longer
// holds that event there.
func DeleteEventLine(body string, e Event) (string, bool) {
	lines := strings.Split(body, "\n")
	if _, ok := inlineEventLine(lines, e); !ok {
		return body, false
	}
	return strings.Join(append(lines[:e.Line:e.Line], lines[e.Line+1:]...), "\n"), true
}

// inlineEventLine gives the line at the place of an inline event when that
// line still reads as the event with the same `when:` value.
func inlineEventLine(lines []string, e Event) (string, bool) {
	if e.Kind != EventKindInline || e.Line < 0 || e.Line >= len(lines) {
		return "", false
	}
	line := lines[e.Line]
	read, ok := eventFromLine(strings.TrimSuffix(line, "\r"))
	if !ok || read.When != e.When {
		return "", false
	}
	return line, true
}

func setEventNoteWhen(body string, e Event, when string) (string, bool) {
	m := frontmatterRe.FindStringSubmatchIndex(body)
	if m == nil || m[2] < 0 {
		return body, false
	}
	start := m[2]
	for _, line := range strings.SplitAfter(body[m[2]:m[3]], "\n") {
		parts := eventNoteWhenRe.FindStringSubmatch(strings.TrimSuffix(line, "\n"))
		if parts == nil {
			start += len(line)
			continue
		}
		old := parts[2]
		if Unquote(old) != e.When {
			return body, false
		}
		value := when
		if old != "" && (old[0] == '"' || old[0] == '\'') {
			value = old[:1] + when + old[:1]
		}
		next := parts[1] + value + parts[3]
		if strings.HasSuffix(line, "\n") {
			next += "\n"
		}
		return body[:start] + next + body[start+len(line):], true
	}
	return body, false
}
