// Package periodic locates daily, weekly and monthly notes from the vault's
// directory and title patterns, and formats dates for template variables.
// Pattern tokens mirror the desktop's vault-layout.ts: yyyy yy MMMM MMM MM M
// dd d EEEE EEE ww w, with quoted literals.
package periodic

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZenNotes/zennotescli/internal/vault"
)

var patternTokens = []string{"yyyy", "yy", "MMMM", "MMM", "MM", "M", "dd", "d", "EEEE", "EEE", "ww", "w"}

type patternPart struct {
	literal bool
	value   string
}

func parsePattern(pattern string) []patternPart {
	parts := []patternPart{}
	literal := ""
	quoted := false
	flush := func() {
		if literal != "" {
			parts = append(parts, patternPart{literal: true, value: literal})
			literal = ""
		}
	}
	for i := 0; i < len(pattern); {
		ch := pattern[i]
		if ch == '\'' {
			if i+1 < len(pattern) && pattern[i+1] == '\'' {
				literal += "'"
				i += 2
				continue
			}
			quoted = !quoted
			i++
			continue
		}
		if !quoted {
			matched := ""
			for _, tok := range patternTokens {
				if strings.HasPrefix(pattern[i:], tok) {
					matched = tok
					break
				}
			}
			if matched != "" {
				flush()
				parts = append(parts, patternPart{value: matched})
				i += len(matched)
				continue
			}
		}
		literal += string(ch)
		i++
	}
	flush()
	return parts
}

func pad2(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// ISOWeek is the ISO 8601 week number and week-year.
func ISOWeek(date time.Time) (year, week int) {
	return date.ISOWeek()
}

// MondayOfISOWeek is the Monday (local midnight) of an ISO week.
func MondayOfISOWeek(weekYear, week int) time.Time {
	jan4 := time.Date(weekYear, time.January, 4, 0, 0, 0, 0, time.Local)
	dayNum := int(jan4.Weekday())
	if dayNum == 0 {
		dayNum = 7
	}
	return jan4.AddDate(0, 0, -(dayNum-1)+(week-1)*7)
}

// FormatPattern renders a date with a directory or title pattern. year and
// week override the date's own for weekly notes, which anchor to the ISO
// week-year.
func FormatPattern(date time.Time, pattern string, year, week int) string {
	if year == 0 {
		year = date.Year()
	}
	if week == 0 {
		_, week = date.ISOWeek()
	}
	var b strings.Builder
	for _, part := range parsePattern(pattern) {
		if part.literal {
			b.WriteString(part.value)
			continue
		}
		switch part.value {
		case "yyyy":
			b.WriteString(strconv.Itoa(year))
		case "yy":
			b.WriteString(pad2(year % 100))
		case "MMMM":
			b.WriteString(date.Month().String())
		case "MMM":
			b.WriteString(date.Month().String()[:3])
		case "MM":
			b.WriteString(pad2(int(date.Month())))
		case "M":
			b.WriteString(strconv.Itoa(int(date.Month())))
		case "dd":
			b.WriteString(pad2(date.Day()))
		case "d":
			b.WriteString(strconv.Itoa(date.Day()))
		case "EEEE":
			b.WriteString(date.Weekday().String())
		case "EEE":
			b.WriteString(date.Weekday().String()[:3])
		case "ww":
			b.WriteString(pad2(week))
		case "w":
			b.WriteString(strconv.Itoa(week))
		}
	}
	return b.String()
}

type patternMatch struct {
	year, month, day, week             int
	hasYear, hasMonth, hasDay, hasWeek bool
}

func (m *patternMatch) set(key string, value int) bool {
	switch key {
	case "year":
		if m.hasYear && m.year != value {
			return false
		}
		m.year, m.hasYear = value, true
	case "month":
		if m.hasMonth && m.month != value {
			return false
		}
		m.month, m.hasMonth = value, true
	case "day":
		if m.hasDay && m.day != value {
			return false
		}
		m.day, m.hasDay = value, true
	case "week":
		if m.hasWeek && m.week != value {
			return false
		}
		m.week, m.hasWeek = value, true
	}
	return true
}

func (m *patternMatch) merge(other patternMatch) bool {
	if other.hasYear && !m.set("year", other.year) {
		return false
	}
	if other.hasMonth && !m.set("month", other.month) {
		return false
	}
	if other.hasDay && !m.set("day", other.day) {
		return false
	}
	if other.hasWeek && !m.set("week", other.week) {
		return false
	}
	return true
}

func matchPattern(pattern, text string) (patternMatch, bool) {
	type capture struct {
		token string
		index int
	}
	captures := []capture{}
	var src strings.Builder
	src.WriteString("^")
	index := 1
	for _, part := range parsePattern(pattern) {
		if part.literal {
			src.WriteString(regexp.QuoteMeta(part.value))
			continue
		}
		switch part.value {
		case "yyyy":
			captures = append(captures, capture{part.value, index})
			index++
			src.WriteString(`(\d{4})`)
		case "yy":
			captures = append(captures, capture{part.value, index})
			index++
			src.WriteString(`(\d{2})`)
		case "MM", "M", "dd", "d", "ww", "w":
			captures = append(captures, capture{part.value, index})
			index++
			src.WriteString(`(\d{1,2})`)
		default:
			src.WriteString(`[^/]+`)
		}
	}
	src.WriteString("$")
	re, err := regexp.Compile(src.String())
	if err != nil {
		return patternMatch{}, false
	}
	m := re.FindStringSubmatch(text)
	if m == nil {
		return patternMatch{}, false
	}
	out := patternMatch{}
	for _, c := range captures {
		raw := m[c.index]
		if raw == "" {
			continue
		}
		value, _ := strconv.Atoi(raw)
		switch c.token {
		case "yyyy":
			if !out.set("year", value) {
				return patternMatch{}, false
			}
		case "yy":
			if value >= 70 {
				value += 1900
			} else {
				value += 2000
			}
			if !out.set("year", value) {
				return patternMatch{}, false
			}
		case "MM", "M":
			if !out.set("month", value) {
				return patternMatch{}, false
			}
		case "dd", "d":
			if !out.set("day", value) {
				return patternMatch{}, false
			}
		case "ww", "w":
			if !out.set("week", value) {
				return patternMatch{}, false
			}
		}
	}
	return out, true
}

// A directory pattern is only formatted when it clearly holds date tokens:
// a quote, two or more tokens, or a year or week token. `Daily Notes` has a
// lone `d` and must stay literal.
func shouldFormatDirectory(pattern string) bool {
	tokens := 0
	for _, part := range parsePattern(pattern) {
		if part.literal {
			continue
		}
		tokens++
		if part.value == "yyyy" || part.value == "yy" || part.value == "ww" {
			return true
		}
	}
	return strings.Contains(pattern, "'") || tokens >= 2
}

func formatDirectory(date time.Time, pattern string, year, week int) string {
	if !shouldFormatDirectory(pattern) {
		return pattern
	}
	return FormatPattern(date, pattern, year, week)
}

func matchDirectory(pattern, text string) (patternMatch, bool) {
	if !shouldFormatDirectory(pattern) {
		return patternMatch{}, pattern == text
	}
	return matchPattern(pattern, text)
}

// primaryRelative strips a leading `inbox/` from a subpath in the classic
// layout, where the setting names a directory under the inbox.
func primaryRelative(subpath string, primaryAtRoot bool) string {
	if primaryAtRoot {
		return subpath
	}
	if subpath == "inbox" {
		return ""
	}
	return strings.TrimPrefix(subpath, "inbox/")
}

// Location is where a periodic note lives: its title and its subpath under
// the inbox bucket.
type Location struct {
	Title   string
	Subpath string
}

// Kind is daily, weekly or monthly.
type Kind string

const (
	Daily   Kind = "daily"
	Weekly  Kind = "weekly"
	Monthly Kind = "monthly"
)

func settingsFor(kind Kind, s vault.VaultSettings) vault.PeriodicNotes {
	switch kind {
	case Weekly:
		return s.WeeklyNotes
	case Monthly:
		return s.MonthlyNotes
	}
	return s.DailyNotes
}

func defaultTitle(kind Kind) string {
	switch kind {
	case Weekly:
		return vault.DefaultWeeklyNoteTitlePattern
	case Monthly:
		return vault.DefaultMonthlyNoteTitlePattern
	}
	return vault.DefaultDailyNoteTitlePattern
}

func defaultDirectory(kind Kind) string {
	switch kind {
	case Weekly:
		return vault.DefaultWeeklyNotesDirectory
	case Monthly:
		return vault.DefaultMonthlyNotesDirectory
	}
	return vault.DefaultDailyNotesDirectory
}

func locationForPattern(kind Kind, date time.Time, pattern vault.DateNotePattern, primaryAtRoot bool) Location {
	anchor := date
	year, week := 0, 0
	switch kind {
	case Weekly:
		wy, wk := date.ISOWeek()
		anchor = MondayOfISOWeek(wy, wk)
		year, week = wy, wk
	case Monthly:
		anchor = time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	}
	dir := strings.Trim(strings.TrimSpace(formatDirectory(anchor, pattern.Directory, year, week)), "/")
	if dir == "" {
		dir = defaultDirectory(kind)
	}
	titlePattern := pattern.TitlePattern
	if titlePattern == "" {
		titlePattern = defaultTitle(kind)
	}
	title := strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-").Replace(FormatPattern(anchor, titlePattern, year, week)))
	if title == "" {
		title = defaultTitle(kind)
	}
	return Location{Title: title, Subpath: primaryRelative(dir, primaryAtRoot)}
}

func currentPattern(p vault.PeriodicNotes) vault.DateNotePattern {
	return vault.DateNotePattern{Directory: p.Directory, TitlePattern: p.TitlePattern, Locale: p.Locale}
}

// LocationFor is where the note for date lives under the current pattern.
func LocationFor(kind Kind, date time.Time, settings vault.VaultSettings, primaryAtRoot bool) Location {
	return locationForPattern(kind, date, currentPattern(settingsFor(kind, settings)), primaryAtRoot)
}

// RelPathFor is the vault-relative path of the note for date.
func RelPathFor(kind Kind, date time.Time, settings vault.VaultSettings, primaryAtRoot bool, paths map[string]string) string {
	loc := LocationFor(kind, date, settings, primaryAtRoot)
	dir := ""
	if !primaryAtRoot {
		dir = vault.ResolveFolderPath(vault.FolderInbox, paths)
	}
	if loc.Subpath != "" {
		if dir != "" {
			dir += "/"
		}
		dir += loc.Subpath
	}
	if dir != "" {
		return dir + "/" + loc.Title + ".md"
	}
	return loc.Title + ".md"
}

func allPatterns(p vault.PeriodicNotes) []vault.DateNotePattern {
	out := []vault.DateNotePattern{currentPattern(p)}
	out = append(out, p.LegacyPatterns...)
	return out
}

// DateOf recognizes a periodic note from its inbox subpath and title,
// trying the current pattern and then the legacy ones. Daily returns the day,
// weekly the Monday of the week, monthly the first of the month.
func DateOf(kind Kind, subpath, title string, settings vault.VaultSettings, primaryAtRoot bool) (time.Time, bool) {
	for _, pattern := range allPatterns(settingsFor(kind, settings)) {
		dirPattern := primaryRelative(strings.Trim(pattern.Directory, "/"), primaryAtRoot)
		if dirPattern == "" {
			dirPattern = defaultDirectory(kind)
		}
		dirMatch, ok := matchDirectory(dirPattern, subpath)
		if !ok {
			continue
		}
		titlePattern := pattern.TitlePattern
		if titlePattern == "" {
			titlePattern = defaultTitle(kind)
		}
		titleMatch, ok := matchPattern(titlePattern, title)
		if !ok {
			continue
		}
		merged := patternMatch{}
		if !merged.merge(dirMatch) || !merged.merge(titleMatch) {
			continue
		}
		if date, ok := resolveDate(kind, merged, pattern, primaryAtRoot, subpath, title); ok {
			return date, true
		}
	}
	return time.Time{}, false
}

func resolveDate(kind Kind, n patternMatch, pattern vault.DateNotePattern, primaryAtRoot bool, subpath, title string) (time.Time, bool) {
	if !n.hasYear {
		return time.Time{}, false
	}
	roundTrips := func(date time.Time) bool {
		loc := locationForPattern(kind, date, pattern, primaryAtRoot)
		return loc.Title == title && loc.Subpath == subpath
	}
	switch kind {
	case Daily:
		if n.hasMonth && n.hasDay {
			date := time.Date(n.year, time.Month(n.month), n.day, 0, 0, 0, 0, time.Local)
			if date.Year() != n.year || int(date.Month()) != n.month || date.Day() != n.day {
				return time.Time{}, false
			}
			return date, true
		}
		firstMonth, lastMonth := 1, 12
		if n.hasMonth {
			firstMonth, lastMonth = n.month, n.month
		}
		for month := firstMonth; month <= lastMonth; month++ {
			days := time.Date(n.year, time.Month(month)+1, 0, 0, 0, 0, 0, time.Local).Day()
			for day := 1; day <= days; day++ {
				if n.hasDay && day != n.day {
					continue
				}
				date := time.Date(n.year, time.Month(month), day, 0, 0, 0, 0, time.Local)
				if n.hasWeek {
					if _, wk := date.ISOWeek(); wk != n.week {
						continue
					}
				}
				if roundTrips(date) {
					return date, true
				}
			}
		}
	case Weekly:
		if n.hasWeek {
			monday := MondayOfISOWeek(n.year, n.week)
			if wy, _ := monday.ISOWeek(); wy == n.year {
				return monday, true
			}
			return time.Time{}, false
		}
		for week := 1; week <= 53; week++ {
			monday := MondayOfISOWeek(n.year, week)
			if wy, _ := monday.ISOWeek(); wy != n.year {
				continue
			}
			if roundTrips(monday) {
				return monday, true
			}
		}
	case Monthly:
		if n.hasMonth {
			if n.month < 1 || n.month > 12 {
				return time.Time{}, false
			}
			return time.Date(n.year, time.Month(n.month), 1, 0, 0, 0, 0, time.Local), true
		}
		for month := 1; month <= 12; month++ {
			date := time.Date(n.year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
			if roundTrips(date) {
				return date, true
			}
		}
	}
	return time.Time{}, false
}

// --- template date formatting ---

var templateDateRe = regexp.MustCompile(`\[([^\]]*)\]|YYYY|YY|yyyy|yy|MMMM|MMM|MM|M|EEEE|EEE|dddd|ddd|dd|d|DD|D|ww|w|HH|mm|ss`)

// FormatTemplateDate formats a date with a `{{date:FORMAT}}` token string.
// Both the moment-style (YYYY/DD/dddd) and the date-fns-style (yyyy/dd/EEEE)
// vocabularies work; `[brackets]` protect literal letters.
func FormatTemplateDate(date time.Time, format string) string {
	_, week := date.ISOWeek()
	return templateDateRe.ReplaceAllStringFunc(format, func(match string) string {
		if strings.HasPrefix(match, "[") {
			return match[1 : len(match)-1]
		}
		switch match {
		case "YYYY", "yyyy":
			return strconv.Itoa(date.Year())
		case "YY", "yy":
			return pad2(date.Year() % 100)
		case "MMMM":
			return date.Month().String()
		case "MMM":
			return date.Month().String()[:3]
		case "MM":
			return pad2(int(date.Month()))
		case "M":
			return strconv.Itoa(int(date.Month()))
		case "DD", "dd":
			return pad2(date.Day())
		case "D", "d":
			return strconv.Itoa(date.Day())
		case "dddd", "EEEE":
			return date.Weekday().String()
		case "ddd", "EEE":
			return date.Weekday().String()[:3]
		case "ww":
			return pad2(week)
		case "w":
			return strconv.Itoa(week)
		case "HH":
			return pad2(date.Hour())
		case "mm":
			return pad2(date.Minute())
		case "ss":
			return pad2(date.Second())
		}
		return match
	})
}
