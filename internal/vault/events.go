package vault

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// EventKind says where an event is written.
type EventKind string

const (
	// EventKindNote is an event that is a whole note.
	EventKindNote EventKind = "note"
	// EventKindInline is an event that is one list line.
	EventKindInline EventKind = "inline"
)

// EventNotePageType is the `page-type` value that marks a note as one event.
const EventNotePageType = "event"

// EventListPageType is the `page-type` value that marks a note as a list of
// events. Such a note makes no event of its own.
const EventListPageType = "event-list"

// Event is a plan for a day or a time, written in a note. It has no state:
// it is never done, and it never rolls.
type Event struct {
	// Path is the note that holds the event.
	Path      string
	NoteTitle string
	Folder    NoteFolder
	// Line is zero-based over the whole file, as Task.LineNumber is. It is 0
	// for an event note, and Kind tells that case from an inline event on the
	// first line.
	Line int
	Kind EventKind
	Text string
	// When is the value of the `when` token or key as written.
	When      string
	StartDate string
	StartTime string
	// EndDate is empty when the event ends on its start date without a time,
	// and when the event has no end.
	EndDate string
	EndTime string
	// Repeat is the rule in lower case, without the `after-` prefix.
	Repeat string
	// Until is set only when Repeat is set.
	Until string
	// Skip holds the date items and SkipNames the names.
	Skip      []string
	SkipNames []string
	TaskLink  string
	BlockID   string
}

var (
	eventListItemRe = regexp.MustCompile(`^\s*(?:>\s*)*(?:[-+*]|\d+[.)])\s+(.*)$`)
	eventCheckboxRe = regexp.MustCompile(`^\[.\]`)
	eventWhenRe     = regexp.MustCompile(`(?:^|\s)when:(\S+)`)
	eventRepeatRe   = regexp.MustCompile(`(?i)(?:^|\s)@repeat:([\p{L}\d][\p{L}\d/_-]*)`)
	eventSkipRe     = skipFieldRe
	eventUntilRe    = untilFieldRe
	// A `task:` value that starts with `[[` ends at the first `]]`, spaces
	// included. Any other value ends at the first space.
	eventTaskRe = regexp.MustCompile(`(?i)(?:^|\s)task:(\[\[.*?\]\]|\S+)`)
)

// IsEventListNote reports whether the frontmatter of a note says
// `page-type: event-list`. It is matched as `page-type: task` is.
func IsEventListNote(body string) bool {
	block, _, ok := Frontmatter(strings.ReplaceAll(body, "\r\n", "\n"))
	return ok && hasPageType(block, EventListPageType)
}

// ParseEvents reads the events of one note: the note itself when it is an
// event note, then each list line that holds exactly one lower-case `when:`
// token with a real date. A checkbox line is a task and not an event. Lines
// in frontmatter, in fenced code and in inline code are not read. The
// frontmatter key `events: false` (or `off`) stops the inline events.
func ParseEvents(path, title string, folder NoteFolder, body string) []Event {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	out := []Event{}
	rest := normalized
	skipLines := 0
	inlineOn := true
	if block, after, ok := Frontmatter(normalized); ok {
		rest = after
		skipLines = strings.Count(normalized[:len(normalized)-len(after)], "\n")
		fm := ParseFrontmatterFields(block)
		if hasPageType(block, EventNotePageType) {
			if e, ok := eventFromNote(path, title, folder, fm); ok {
				out = append(out, e)
			}
		}
		switch strings.ToLower(Unquote(firstScalar(fm["events"]))) {
		case "false", "off":
			inlineOn = false
		}
	}
	if !inlineOn {
		return out
	}
	inFence := false
	fenceMarker := ""
	for i, line := range strings.Split(rest, "\n") {
		if fm := taskFenceRe.FindStringSubmatch(line); fm != nil {
			if !inFence {
				inFence, fenceMarker = true, fm[2]
			} else if fm[2] == fenceMarker {
				inFence, fenceMarker = false, ""
			}
			continue
		}
		if inFence {
			continue
		}
		if e, ok := eventFromLine(line); ok {
			e.Path, e.NoteTitle, e.Folder = path, title, folder
			e.Line = skipLines + i
			out = append(out, e)
		}
	}
	return out
}

// eventFromNote reads the frontmatter of an event note. It gives no event
// when the `when` key does not start with a real date.
func eventFromNote(path, title string, folder NoteFolder, fm map[string][]string) (Event, bool) {
	when := Unquote(firstScalar(fm["when"]))
	startDate, startTime, endDate, endTime, _ := ParseWhenValue(when)
	if startDate == "" {
		return Event{}, false
	}
	text := title
	if t := strings.TrimSpace(firstScalar(fm["title"])); t != "" {
		text = t
	}
	e := Event{
		Path: path, NoteTitle: title, Folder: folder, Kind: EventKindNote, Text: text,
		When: when, StartDate: startDate, StartTime: startTime, EndDate: endDate, EndTime: endTime,
		Repeat:   eventRepeat(frontmatterRepeat(firstScalar(fm["repeat"]))),
		TaskLink: eventNoteTaskLink(fm["task"]),
	}
	e.Skip, e.SkipNames = eventSkip(fm["skip"])
	if e.Repeat != "" {
		e.Until = validUntil(Unquote(firstScalar(fm["until"])))
	}
	return e, true
}

// eventNoteTaskLink gives the `task` link of an event note. The frontmatter
// reader splits an unquoted `[[Note]]` value as a list that has one item
// `[Note]`. This function puts the second pair of brackets back.
func eventNoteTaskLink(v []string) string {
	first := firstScalar(v)
	if len(v) > 0 && strings.HasPrefix(first, "[") && !strings.HasPrefix(first, "[[") &&
		strings.HasSuffix(v[len(v)-1], "]") {
		return "[" + strings.Join(v, ",") + "]"
	}
	return Unquote(first)
}

// eventFromLine reads one line as an inline event. The caller sets the note
// fields and the line number.
func eventFromLine(line string) (Event, bool) {
	m := eventListItemRe.FindStringSubmatch(line)
	if m == nil || eventCheckboxRe.MatchString(m[1]) {
		return Event{}, false
	}
	text, blockID := splitBlockID(m[1])
	// Tokens are found in a copy where inline code is blank, so a token in
	// code is not read. The copy keeps every offset of the text.
	masked := inlineCodeRe.ReplaceAllStringFunc(text, func(s string) string {
		return strings.Repeat(" ", len(s))
	})
	whens := eventWhenRe.FindAllStringSubmatchIndex(masked, -1)
	if len(whens) != 1 {
		return Event{}, false
	}
	when := masked[whens[0][2]:whens[0][3]]
	if !startsWithRealDate(when) {
		return Event{}, false
	}
	startDate, startTime, endDate, endTime, _ := ParseWhenValue(when)
	e := Event{
		Kind: EventKindInline, When: when, BlockID: blockID,
		StartDate: startDate, StartTime: startTime, EndDate: endDate, EndTime: endTime,
	}
	spans := [][]int{{whens[0][0], whens[0][1]}}
	for _, m := range eventRepeatRe.FindAllStringSubmatchIndex(masked, -1) {
		spans = append(spans, m[:2])
		if e.Repeat == "" {
			e.Repeat = eventRepeat(frontmatterRepeat(masked[m[2]:m[3]]))
		}
	}
	for _, m := range eventSkipRe.FindAllStringSubmatchIndex(masked, -1) {
		spans = append(spans, m[:2])
		dates, names := eventSkip([]string{masked[m[2]:m[3]]})
		e.Skip = append(e.Skip, dates...)
		e.SkipNames = append(e.SkipNames, names...)
	}
	untilTokens := eventUntilRe.FindAllStringSubmatchIndex(masked, -1)
	for _, m := range untilTokens {
		spans = append(spans, m[:2])
	}
	if len(untilTokens) > 0 && e.Repeat != "" {
		m := untilTokens[0]
		e.Until = validUntil(masked[m[2]:m[3]])
	}
	for i, m := range eventTaskRe.FindAllStringSubmatchIndex(masked, -1) {
		spans = append(spans, m[:2])
		if i == 0 {
			e.TaskLink = masked[m[2]:m[3]]
		}
	}
	e.Text = strings.TrimSpace(wsCollapseRe.ReplaceAllString(cutSpans(text, spans), " "))
	return e, true
}

// startsWithRealDate reports whether a `when:` value begins with a real
// `YYYY-MM-DD` date that is followed by the end of the value, a `T`, a `-`, a
// `/` or `..`.
func startsWithRealDate(value string) bool {
	if len(value) < 10 || !IsValidISODate(value[:10]) {
		return false
	}
	rest := value[10:]
	return rest == "" || rest[0] == 'T' || rest[0] == '-' || rest[0] == '/' || strings.HasPrefix(rest, "..")
}

// cutSpans replaces each span of text with one space. The spans are byte
// ranges that do not overlap, in any order.
func cutSpans(text string, spans [][]int) string {
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] > spans[j][0] })
	end := len(text) + 1
	for _, s := range spans {
		if s[1] > end {
			continue
		}
		text = text[:s[0]] + " " + text[s[1]:]
		end = s[0]
	}
	return text
}

// eventRepeat drops the `after-` prefix of a rule, which an event does not use.
func eventRepeat(rule string) string {
	return strings.TrimPrefix(rule, "after-")
}

// eventSkip splits skip values into the date items and the names. A value
// holds one item.
func eventSkip(values []string) (dates, names []string) {
	for _, v := range values {
		items, _, found := ParseSkipItems(v)
		dates = append(dates, items...)
		names = append(names, found...)
	}
	return dates, names
}

// ScanEvents reads the events of every live note that ScanTasks reads, with
// the same exclusions. A note in the trash gives no event.
func (v *Vault) ScanEvents() ([]Event, error) {
	excluded, included := v.Settings().TasksFolderLists()
	files := []noteFile{}
	v.walkNotes(LiveFolders, false, func(f noteFile) { files = append(files, f) })
	groups := make([][]Event, len(files))
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for i, f := range files {
		rel := v.relPosix(f.abs)
		if IsPathExcludedFromTasks(rel, excluded, included) {
			continue
		}
		wg.Add(1)
		go func(i int, f noteFile, rel string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := os.ReadFile(f.abs)
			if err != nil {
				return
			}
			title := strings.TrimSuffix(filepath.Base(f.abs), filepath.Ext(f.abs))
			groups[i] = ParseEvents(rel, title, f.folder, string(body))
		}(i, f, rel)
	}
	wg.Wait()
	out := []Event{}
	for _, g := range groups {
		out = append(out, g...)
	}
	return out, nil
}
