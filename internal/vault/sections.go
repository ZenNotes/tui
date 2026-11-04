package vault

import (
	"fmt"
	"regexp"
	"strings"
)

// A note's sections, for reading and editing part of a note by its heading
// instead of by line numbers or the whole body. Headings come from Outline, so
// frontmatter and fenced code never count.

// NoteSection is a heading and what sits under it, up to the next heading of
// the same or a higher level. Lines are zero-based, as in InsertAtLine and a
// task's LineNumber; End is exclusive.
type NoteSection struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"lineNumber"`
	// BodyStart is the first line after the heading (a setext heading has two).
	BodyStart int `json:"-"`
	// OwnEnd is where its own text stops: its first subheading, else End.
	OwnEnd int `json:"-"`
	End    int `json:"endLineNumber"`
}

func splitNoteLines(body string) []string {
	return strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
}

// NoteSections lists every heading of a note with its extent.
func NoteSections(body string) []NoteSection {
	lines := splitNoteLines(body)
	outline := Outline(body)
	out := make([]NoteSection, 0, len(outline))
	for i, item := range outline {
		line := item.Line - 1
		start := line + 1
		// Outline's own test: a heading with only spaces after its `#`s is ATX too,
		// and treating it as setext would skip the first line of its text.
		if !atxHeadingRe.MatchString(lines[line]) {
			start = line + 2
		}
		end := len(lines)
		for _, later := range outline[i+1:] {
			if later.Level <= item.Level {
				end = later.Line - 1
				break
			}
		}
		ownEnd := end
		if i+1 < len(outline) && outline[i+1].Line-1 < end {
			ownEnd = outline[i+1].Line - 1
		}
		out = append(out, NoteSection{Level: item.Level, Text: item.Text, Line: line, BodyStart: start, OwnEnd: ownEnd, End: end})
	}
	return out
}

// FindSection picks the section a heading names, ignoring case and any leading
// `#`s. line, when >= 0, says which one when a heading appears more than once.
func FindSection(body, heading string, line int) (NoteSection, error) {
	sections := NoteSections(body)
	want := strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(heading), "#")))
	matches := []NoteSection{}
	for _, s := range sections {
		if strings.ToLower(s.Text) == want && (line < 0 || s.Line == line) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		names := make([]string, 0, len(sections))
		for _, s := range sections {
			if len(names) == 20 {
				names = append(names, "...")
				break
			}
			names = append(names, s.Text)
		}
		if len(names) == 0 {
			return NoteSection{}, fmt.Errorf("No heading %q: this note has no headings.", heading)
		}
		return NoteSection{}, fmt.Errorf("No heading %q. Headings: %s.", heading, strings.Join(names, " | "))
	}
	at := make([]string, len(matches))
	for i, s := range matches {
		at[i] = fmt.Sprint(s.Line)
	}
	return NoteSection{}, fmt.Errorf("Heading %q appears more than once, on lines %s; pass lineNumber to pick one.", heading, strings.Join(at, ", "))
}

func trimBlankLines(lines []string) (lead int, core []string) {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	for lead < end && strings.TrimSpace(lines[lead]) == "" {
		lead++
	}
	return lead, lines[lead:end]
}

// SectionBody is the text under a heading, without the heading or the blank
// lines around it.
func SectionBody(body string, s NoteSection) string {
	lines := splitNoteLines(body)
	_, core := trimBlankLines(lines[min(s.BodyStart, s.End):s.End])
	return strings.Join(core, "\n")
}

var sectionListItemRe = regexp.MustCompile(`^\s*(?:[-+*]|\d+[.)])\s`)

// SectionEditMode says where EditSectionInBody puts the text.
type SectionEditMode string

const (
	SectionReplace SectionEditMode = "replace"
	SectionAppend  SectionEditMode = "append"
	SectionPrepend SectionEditMode = "prepend"
)

// EditSectionInBody replaces what sits under a heading, subheadings included,
// or adds text at the start of it or at the end of its own text, above any
// subheading. The heading and everything else keep their bytes, and one blank
// line separates the edit from the next heading.
func EditSectionInBody(body string, s NoteSection, mode SectionEditMode, text string) string {
	lines := splitNoteLines(body)
	start := min(s.BodyStart, s.End)
	end := s.End
	if mode == SectionAppend {
		end = max(start, s.OwnEnd)
	}
	lead, core := trimBlankLines(lines[start:end])
	inserted := []string{}
	if t := strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n"); t != "" {
		inserted = strings.Split(t, "\n")
	}

	content := []string{}
	switch mode {
	case SectionReplace:
		content = inserted
	case SectionPrepend:
		content = append(content, inserted...)
		if len(core) > 0 {
			content = append(append(content, ""), core...)
		}
	default:
		content = append(content, core...)
		if len(core) > 0 && len(inserted) > 0 {
			// A list grows by one item; anything else gets its own paragraph.
			if !sectionListItemRe.MatchString(core[len(core)-1]) || !sectionListItemRe.MatchString(inserted[0]) {
				content = append(content, "")
			}
		}
		content = append(content, inserted...)
	}

	out := make([]string, 0, len(lines)+len(inserted)+2)
	out = append(out, lines[:start]...)
	if len(content) > 0 {
		// Keep a heading's blank line as written; an empty section gets one.
		if len(core) == 0 || mode == SectionReplace {
			lead = 1
		}
		for i := 0; i < lead; i++ {
			out = append(out, "")
		}
		out = append(out, content...)
	}
	// A section that ended the note still ends it with a newline.
	out = append(out, "")
	if end < len(lines) {
		out = append(out, lines[end:]...)
	}
	edited := strings.Join(out, "\n")
	// A Windows note stays one, the new lines included. A note mixing endings
	// comes out with plain newlines.
	if crlf := strings.Count(body, "\r\n"); crlf > 0 && crlf == strings.Count(body, "\n") {
		return strings.ReplaceAll(edited, "\n", "\r\n")
	}
	return edited
}
