package vault

import (
	"strings"
	"time"
)

// RollTaskFileInBody completes or cancels a task note. A note whose `repeat`
// rule is valid and that is not already finished does not stay done: its
// `due` date moves to the next date, `status` goes back to `open`, and
// `lastCompleted` records today. The rest of the frontmatter,
// `skip` and `until` lines included, and the whole body stay as they are. A stale
// `completedDate` is removed, because an open note has none.
//
// The next date and the offset follow the rules of RollTaskInBody: the rule
// counts from `due`, else today, and from today when it ends in `/done`. The
// date keeps its `THH:MM`, and a note with no date gets `due`. A `scheduled`
// key is not read and stays as written.
//
// Every other case is the plain status change, SetTaskFileStatus or
// SetTaskFileCancelled: no rule, an invalid rule, unticking a finished note,
// reopening a cancelled one, or a rule with no date left to land on, such as
// one that has reached its `until` date. rolled
// reports whether the dates advanced.
//
// Only local notes roll. A remote vault's server parses the note itself, so
// remote toggles use ToggleFileTaskInBody and never advance a note.
func RollTaskFileInBody(body string, action RollAction, now time.Time, weekStart time.Weekday) (next string, rolled bool) {
	return RollTaskFileInBodyWith(body, action, now, weekStart, nil)
}

// RollTaskFileInBodyWith is RollTaskFileInBody with a resolver for the `@name`
// items of the `skip` key. The days that names gives for a name count as skip
// periods. A nil names leaves these items without effect.
func RollTaskFileInBodyWith(body string, action RollAction, now time.Time, weekStart time.Weekday, names SkipResolver) (next string, rolled bool) {
	task, ok := parseTaskFile("", "", FolderInbox, body, DialectCLI)
	if !ok || task.Repeat == "" || task.Checked || task.Cancelled {
		return plainTaskFileRoll(body, action, task.Checked, task.Cancelled, now), false
	}
	rule, valid := ParseRepeatRule(task.Repeat)
	if !valid {
		return plainTaskFileRoll(body, action, false, false, now), false
	}
	_, periods, _ := ParseSkipItems(strings.Join(task.Skip, ","))

	block, _, _ := Frontmatter(body)
	fields := ParseFrontmatterFields(block)
	for _, value := range fields["skip"] {
		_, _, skipNames := ParseSkipItems(value)
		periods = append(periods, skipPeriodsFor(names, Event{}, skipNames)...)
	}
	due := Unquote(strings.TrimSpace(firstScalar(fields["due"])))

	days, following, found := nextRollDate(rule, task.Due, periods, task.Until, now, weekStart)
	if !found {
		return plainTaskFileRoll(body, action, false, false, now), false
	}

	updates := []frontmatterLine{
		{key: "status", value: StrPtr("open")},
		{key: "completedDate"},
	}
	if task.Due == "" {
		updates = append(updates, frontmatterLine{key: "due", value: StrPtr(following)})
	} else {
		updates = append(updates, frontmatterLine{key: "due", value: StrPtr(shiftDateTime(due, days))})
	}
	updates = append(updates, frontmatterLine{key: "lastCompleted", value: StrPtr(TodayISO(now))})
	return setFrontmatterLines(body, updates), true
}

// plainTaskFileRoll is the status change a task note gets when it does not
// advance: completing flips done and open, cancelling flips cancelled and
// open.
func plainTaskFileRoll(body string, action RollAction, checked, cancelled bool, now time.Time) string {
	if action == RollCancel {
		return SetTaskFileCancelled(body, !cancelled)
	}
	return ToggleFileTaskInBody(body, checked, now)
}

// frontmatterLine is one scalar frontmatter line to set, or to remove when
// value is nil.
type frontmatterLine struct {
	key   string
	value *string
}

// setFrontmatterLines sets or removes scalar frontmatter lines in place. A key
// that was written as a block list loses its list item lines too, so the new
// value is not left above stale items. Unlike UpdateFrontmatterFields it
// writes a value as given, so a date-time such as
// `2026-11-09T09:30` is not quoted, and it keeps the quotes a value already
// had. Keys match case-insensitively and keep the casing they were written
// with. A key the block lacks is appended, and a note without frontmatter is
// returned as it is. Line endings are normalised to LF.
func setFrontmatterLines(body string, updates []frontmatterLine) string {
	norm := strings.ReplaceAll(body, "\r\n", "\n")
	block, rest, ok := Frontmatter(norm)
	if !ok {
		return body
	}
	pending := append([]frontmatterLine(nil), updates...)
	var out []string
	dropItems := false
	for _, line := range strings.Split(block, "\n") {
		if dropItems {
			// Blank and comment lines do not end a list, as the frontmatter
			// reader skips them, so the items after them are dropped too.
			if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
				out = append(out, line)
				continue
			}
			if fmListItemRe.MatchString(line) {
				continue
			}
		}
		dropItems = false
		km := fmKeyRe.FindStringSubmatch(line)
		hit := -1
		if km != nil {
			for i, u := range pending {
				if strings.EqualFold(u.key, km[1]) {
					hit = i
					break
				}
			}
		}
		if hit < 0 {
			out = append(out, line)
			continue
		}
		u := pending[hit]
		pending = append(pending[:hit], pending[hit+1:]...)
		dropItems = true
		if u.value == nil {
			continue
		}
		value := *u.value
		old := strings.TrimSpace(line[strings.IndexByte(line, ':')+1:])
		if old != "" && (old[0] == '"' || old[0] == '\'') && old[0] == old[len(old)-1] {
			value = string(old[0]) + value + string(old[0])
		}
		out = append(out, km[1]+": "+value)
	}
	for _, u := range pending {
		if u.value != nil {
			out = append(out, u.key+": "+*u.value)
		}
	}
	return "---\n" + strings.Join(out, "\n") + "\n---\n" + rest
}
