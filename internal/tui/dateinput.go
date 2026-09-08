package tui

import (
	"strconv"
	"strings"
	"time"
)

// parseDateInput accepts what a person types into a date cell: an ISO
// date, today/tomorrow/yesterday, a weekday name (the coming one), or a
// relative offset like +3d, -1w, +2m. It returns the ISO form. Empty input
// clears the cell.
func parseDateInput(text string, now time.Time) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return "", true
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "today", "tod", "now":
		return day(today), true
	case "tomorrow", "tom", "tmr":
		return day(today.AddDate(0, 0, 1)), true
	case "yesterday", "yst":
		return day(today.AddDate(0, 0, -1)), true
	case "next week":
		return day(today.AddDate(0, 0, 7)), true
	case "next month":
		return day(today.AddDate(0, 1, 0)), true
	}
	for _, layout := range []string{"2006-01-02", "2006-1-2", "2006/01/02", "2006/1/2", "Jan 2 2006", "January 2 2006", "2 Jan 2006", "02.01.2006"} {
		if t, err := time.Parse(layout, text); err == nil {
			return day(t), true
		}
	}
	weekdays := map[string]time.Weekday{
		"mon": time.Monday, "monday": time.Monday,
		"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
		"wed": time.Wednesday, "wednesday": time.Wednesday,
		"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
		"fri": time.Friday, "friday": time.Friday,
		"sat": time.Saturday, "saturday": time.Saturday,
		"sun": time.Sunday, "sunday": time.Sunday,
	}
	if wd, ok := weekdays[strings.TrimPrefix(s, "next ")]; ok {
		delta := (int(wd) - int(today.Weekday()) + 7) % 7
		if delta == 0 {
			delta = 7
		}
		return day(today.AddDate(0, 0, delta)), true
	}
	if len(s) >= 2 && (s[0] == '+' || s[0] == '-') {
		unit := s[len(s)-1]
		num := s[1 : len(s)-1]
		if unit >= '0' && unit <= '9' {
			unit = 'd'
			num = s[1:]
		}
		n, err := strconv.Atoi(num)
		if err == nil {
			if s[0] == '-' {
				n = -n
			}
			switch unit {
			case 'd':
				return day(today.AddDate(0, 0, n)), true
			case 'w':
				return day(today.AddDate(0, 0, 7*n)), true
			case 'm':
				return day(today.AddDate(0, n, 0)), true
			case 'y':
				return day(today.AddDate(n, 0, 0)), true
			}
		}
	}
	return "", false
}
