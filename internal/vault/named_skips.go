package vault

import (
	"strings"
	"time"
)

// SkipResolver gives the skip periods of an `@name` item. owner is the event
// that holds the item; it is the zero Event for a task. A name that no event
// note answers gives no period. The resolver can be nil, and then every name
// skips nothing.
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
	byTitle := map[string][]Event{}
	for _, e := range events {
		if e.Kind != EventKindNote || e.Folder == FolderTrash {
			continue
		}
		key := normaliseSkipName(e.NoteTitle)
		byTitle[key] = append(byTitle[key], e)
	}
	lookup := func(name string) (Event, bool) {
		found := byTitle[normaliseSkipName(name)]
		if len(found) != 1 {
			return Event{}, false
		}
		return found[0], true
	}
	// reaches reports whether the note at path waits, through its names, for
	// the note at goal.
	var reaches func(e Event, goal string, seen map[string]bool) bool
	reaches = func(e Event, goal string, seen map[string]bool) bool {
		if e.Path == goal {
			return true
		}
		if seen[e.Path] {
			return false
		}
		seen[e.Path] = true
		for _, name := range e.SkipNames {
			if next, ok := lookup(name); ok && reaches(next, goal, seen) {
				return true
			}
		}
		return false
	}
	var resolve SkipResolver
	resolve = func(owner Event, name string) []SkipPeriod {
		target, ok := lookup(name)
		if !ok {
			return nil
		}
		if owner.Kind == EventKindNote && reaches(target, owner.Path, map[string]bool{}) {
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
