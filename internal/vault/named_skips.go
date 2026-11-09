package vault

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SkipResolver gives the skip periods of a name in a `@skip:` field. owner is
// the event that holds the name; it is the zero Event for a task. A name that
// no event note answers gives no period. The resolver can be nil, and then
// every name skips nothing.
type SkipResolver func(owner Event, name string) []SkipPeriod

// normaliseSkipName makes a name or a title comparable. It folds the case,
// turns each run of spaces and hyphens into one hyphen and trims the hyphens
// at both ends.
func normaliseSkipName(s string) string {
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ' ' || r == '-' || r == '\t'
	})
	return strings.Join(parts, "-")
}

// skipNameIndex finds the event note that a name stands for. A name matches an
// event note by its title, and never an inline event or a note in the trash.
type skipNameIndex struct {
	byTitle map[string][]Event
}

func newSkipNameIndex(events []Event) *skipNameIndex {
	byTitle := map[string][]Event{}
	for _, e := range events {
		if e.Kind != EventKindNote || e.Folder == FolderTrash {
			continue
		}
		key := normaliseSkipName(e.NoteTitle)
		byTitle[key] = append(byTitle[key], e)
	}
	return &skipNameIndex{byTitle: byTitle}
}

// lookup gives the one event note that the name answers. It reports false for
// a name that matches no note or more than one note.
func (s *skipNameIndex) lookup(name string) (Event, bool) {
	found := s.byTitle[normaliseSkipName(name)]
	if len(found) != 1 {
		return Event{}, false
	}
	return found[0], true
}

// reaches reports whether the note e waits, through its names, for the note at
// path goal.
func (s *skipNameIndex) reaches(e Event, goal string, seen map[string]bool) bool {
	if e.Path == goal {
		return true
	}
	if seen[e.Path] {
		return false
	}
	seen[e.Path] = true
	for _, name := range e.SkipNames {
		if next, ok := s.lookup(name); ok && s.reaches(next, goal, seen) {
			return true
		}
	}
	return false
}

// problem says why a name gives no period, or "" when it does. ownerPath is
// the path of the event note that holds the name, or "" for a task.
func (s *skipNameIndex) problem(name, ownerPath string) string {
	found := s.byTitle[normaliseSkipName(name)]
	switch {
	case len(found) == 0:
		return "no event note has this name, so the name skips no day"
	case len(found) > 1:
		paths := make([]string, 0, len(found))
		for _, e := range found {
			paths = append(paths, e.Path)
		}
		sort.Strings(paths)
		return fmt.Sprintf("more than one event note has this name (%s), so the name skips no day", strings.Join(paths, ", "))
	case ownerPath != "" && s.reaches(found[0], ownerPath, map[string]bool{}):
		return "the name leads back to this note, so the name skips no day"
	}
	return ""
}

// NamedSkips builds the resolver for a set of events. A name matches an event
// note by its title, and never an inline event or a note in the trash. A name
// that matches no note or more than one note gives no period. A name that
// would make an event wait for itself, directly or through other notes, also
// gives no period.
//
// The periods of a name are the days that each occurrence of the event note
// covers, from the day from to the day to. The occurrences follow the skips
// and the until date of that note.
func NamedSkips(events []Event, weekStart time.Weekday, from, to string) SkipResolver {
	index := newSkipNameIndex(events)
	var resolve SkipResolver
	resolve = func(owner Event, name string) []SkipPeriod {
		target, ok := index.lookup(name)
		if !ok {
			return nil
		}
		if owner.Kind == EventKindNote && index.reaches(target, owner.Path, map[string]bool{}) {
			return nil
		}
		var out []SkipPeriod
		for _, o := range EventOccurrences(target, from, to, weekStart, resolve) {
			end := o.EndDate
			if end == "" {
				end = o.StartDate
			}
			out = append(out, SkipPeriod{Start: o.StartDate, End: end})
		}
		return out
	}
	return resolve
}

// skipPeriodsFor gathers the periods of a list of names from resolve. It
// returns nil when resolve is nil.
func skipPeriodsFor(resolve SkipResolver, owner Event, names []string) []SkipPeriod {
	if resolve == nil {
		return nil
	}
	var out []SkipPeriod
	for _, name := range names {
		out = append(out, resolve(owner, strings.TrimSpace(name))...)
	}
	return out
}

// skipWindowYears is how far past today a roll looks for the days that a
// named skip covers.
const skipWindowYears = 10

// skipResolver gives the resolver that a roll uses. It reads the events of
// the vault at the first name only, so a task without names costs nothing.
func (v *Vault) skipResolver(now time.Time) SkipResolver {
	var resolve SkipResolver
	return func(owner Event, name string) []SkipPeriod {
		if resolve == nil {
			events, err := v.ScanEvents()
			if err != nil {
				return nil
			}
			from := TodayISO(now.AddDate(-1, 0, 0))
			to := TodayISO(now.AddDate(skipWindowYears, 0, 0))
			resolve = NamedSkips(events, v.WeekStart, from, to)
		}
		return resolve(owner, name)
	}
}
