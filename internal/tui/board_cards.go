package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/termsafe"
	"github.com/ZenNotes/tui/internal/vault"
)

// A Tasks board card is as tall as what it has to say: a title of one to
// three rows, a row of dates and the source note when there is one worth
// naming, and a row of chosen frontmatter fields. Cards used to be two rows
// each, so a task note read as its own name twice.

// cardFace is one card's rows before the selection is painted on.
type cardFace struct {
	title []string
	// meta holds the styled parts of the second row; metaPlain the same text
	// for a selected card, which takes the selection's colours instead.
	meta      []string
	metaPlain string
	fields    string
}

func (f cardFace) height() int {
	n := len(f.title)
	if len(f.meta) > 0 {
		n++
	}
	if f.fields != "" {
		n++
	}
	return n
}

// cardSpan is where a drawn card sits under its column's header, for clicks
// and for menus that open next to the selection.
type cardSpan struct{ top, height int }

// cardFace lays a card out for a column colW cells wide.
func (v *tasksView) cardFace(a *App, c vault.Task, colW int) cardFace {
	th := a.theme
	text := c.Content
	if c.Kind == "file" {
		text = c.NoteTitle
	}
	face := cardFace{title: wrapCardTitle(text, max(1, colW-1), a.prefs.KanbanCardTitleLines)}
	plain := []string{}
	add := func(style lipgloss.Style, s string) {
		face.meta = append(face.meta, style.Render(s))
		plain = append(plain, s)
	}
	// The deadline first, then the planned day behind the calendar's plan
	// mark, so a narrow card cut short still shows the date it is late by.
	// The date a planned day took over from a daily note is not the card's.
	if deadline := vault.TaskDeadline(c); deadline != "" {
		if vault.IsOverdue(c, time.Now()) {
			add(th.Overdue, deadline)
		} else {
			add(th.Dim, deadline)
		}
	}
	if plan := vault.PlannedDay(c); plan != "" && !c.Checked && !c.Cancelled {
		add(th.Dim, calScheduledMark+plan)
	}
	if c.Priority != "" {
		add(lipgloss.NewStyle().Foreground(th.Purple), "!"+c.Priority)
	}
	if c.Waiting {
		add(th.Dim, "waiting")
	}
	// The source note is worth a row only when it says something the title
	// does not: a task note's title is its note's name.
	if !strings.EqualFold(strings.TrimSpace(c.NoteTitle), strings.TrimSpace(text)) {
		add(th.Muted, truncateCells(c.NoteTitle, max(4, colW-2-cellWidth(strings.Join(plain, " ")))))
	}
	face.metaPlain = strings.Join(plain, " ")
	// A column too narrow for "risk: high · sprint: 3" shows "high · 3"
	// before it cuts anything: the values are what differs between cards.
	labelled, bare := a.cardFieldLine(c)
	if cellWidth(labelled) > max(1, colW-1) {
		labelled = bare
	}
	face.fields = truncateCells(labelled, max(1, colW-1))
	return face
}

// wrapCardTitle breaks a title over at most maxLines rows of width cells, on
// spaces where it can. What does not fit ends the last row with an ellipsis.
func wrapCardTitle(text string, width, maxLines int) []string {
	text = strings.Join(strings.Fields(termsafe.Line(text)), " ")
	if maxLines <= 1 || cellWidth(text) <= width {
		return []string{truncateCells(text, width)}
	}
	lines := []string{}
	rest := text
	for len(lines) < maxLines-1 && cellWidth(rest) > width {
		cut, cells, lastSpace := 0, 0, -1
		for i, r := range rest {
			w := runeWidth(r)
			if cells+w > width {
				break
			}
			if r == ' ' {
				lastSpace = i
			}
			cells += w
			cut = i + len(string(r))
		}
		// A space right at the limit ends the row too.
		if cut < len(rest) && rest[cut] == ' ' {
			lastSpace = cut
		}
		if lastSpace > 0 {
			cut = lastSpace
		}
		if cut == 0 {
			break
		}
		lines = append(lines, rest[:cut])
		rest = strings.TrimLeft(rest[cut:], " ")
	}
	return append(lines, truncateCells(rest, width))
}

// --- frontmatter fields on cards ---

// splitCardFields reads the one-line form of kanban_card_fields: names
// between commas, lower-cased as frontmatter keys are.
func splitCardFields(s string) []string {
	out := []string{}
	for _, f := range strings.Split(s, ",") {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// cardFieldEntry is the frontmatter of one source note, as of stamp, the
// note's modification time in the index when it was read.
type cardFieldEntry struct {
	stamp  int64
	fields map[string][]string
}

type cardFieldsLoadedMsg struct {
	epoch   *int
	entries map[string]cardFieldEntry
}

// cardFieldLine is the row of chosen fields under a card's title, such as
// "risk: high · sprint: 3", and the same row without the names. An
// `@key:value` on the task's own line wins over the note's frontmatter, and
// a field the card has no value for is left out.
func (a *App) cardFieldLine(c vault.Task) (labelled, bare string) {
	keys := a.prefs.KanbanCardFields
	if len(keys) == 0 {
		return "", ""
	}
	var note map[string][]string
	if a.cardFieldsEpoch == a.epoch {
		note = a.cardFields[c.SourcePath].fields
	}
	parts, values := []string{}, []string{}
	for _, key := range keys {
		val := strings.TrimSpace(c.Fields[key])
		if val == "" {
			val = strings.TrimSpace(strings.Join(note[key], ", "))
		}
		if val != "" {
			val = termsafe.Line(val)
			parts = append(parts, key+": "+val)
			values = append(values, val)
		}
	}
	return strings.Join(parts, " · "), strings.Join(values, " · ")
}

// ensureCardFields reads the frontmatter of every note behind a card that
// the cache has not seen, or saw before the note last changed. The tasks
// scan carries only the fields it parses itself, and a server's scan cannot
// be asked for more, so the notes are read here, off the UI's goroutine.
func (a *App) ensureCardFields(cols []kanbanColumn) {
	if len(a.prefs.KanbanCardFields) == 0 || a.idx == nil || a.backend == nil {
		return
	}
	if a.cardFieldsEpoch != a.epoch || a.cardFields == nil {
		a.cardFieldsEpoch = a.epoch
		a.cardFields = map[string]cardFieldEntry{}
		a.cardFieldsAsked = map[string]int64{}
	}
	want := map[string]int64{}
	for _, col := range cols {
		for _, c := range col.cards {
			stamp := a.idx.byPath[c.SourcePath].UpdatedAt
			if e, ok := a.cardFields[c.SourcePath]; ok && e.stamp == stamp {
				continue
			}
			if asked, ok := a.cardFieldsAsked[c.SourcePath]; ok && asked == stamp {
				continue
			}
			a.cardFieldsAsked[c.SourcePath] = stamp
			want[c.SourcePath] = stamp
		}
	}
	if len(want) == 0 {
		return
	}
	b, epoch := a.backend, a.epoch
	a.queue(func() tea.Msg {
		entries := map[string]cardFieldEntry{}
		for path, stamp := range want {
			entry := cardFieldEntry{stamp: stamp}
			// A note that cannot be read still gets its entry, or every
			// refresh would ask for it again.
			if content, err := b.ReadNote(context.Background(), path); err == nil {
				if block, _, ok := vault.Frontmatter(content.Body); ok {
					entry.fields = vault.ParseFrontmatterFields(block)
				}
			}
			entries[path] = entry
		}
		return cardFieldsLoadedMsg{epoch: epoch, entries: entries}
	})
}
