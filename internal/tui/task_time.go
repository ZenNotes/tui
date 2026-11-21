package tui

import "fmt"

// formatTaskClock renders a 24-hour `HH:MM` the way the `time_format`
// preference asks: `14:00` for "24h", `2:00 PM` for "12h". A value that is not
// a valid `HH:MM` is returned unchanged.
func formatTaskClock(clock, timeFormat string) string {
	var h, m int
	if len(clock) != 5 || clock[2] != ':' {
		return clock
	}
	if _, err := fmt.Sscanf(clock, "%02d:%02d", &h, &m); err != nil || h > 23 || m > 59 {
		return clock
	}
	if timeFormat != "12h" {
		return clock
	}
	suffix := "AM"
	if h >= 12 {
		suffix = "PM"
	}
	h %= 12
	if h == 0 {
		h = 12
	}
	return fmt.Sprintf("%d:%02d %s", h, m, suffix)
}

// formatWhenRange renders a `when:` range for display. A range inside one day
// reads `2026-11-03 2:00 PM–3:30 PM`. A range across days reads `2027-03-13 –
// 2027-03-21` or `2026-11-03 10:00 PM – 2026-11-04 2:00 AM`. The times follow
// the `time_format` preference.
func formatWhenRange(startDate, startTime, endDate, endTime, timeFormat string) string {
	if endDate == "" || endDate == startDate {
		return withTaskClock(startDate, startTime, endTime, timeFormat)
	}
	return withTaskClock(startDate, startTime, "", timeFormat) + " – " +
		withTaskClock(endDate, endTime, "", timeFormat)
}

// withTaskClock appends a formatted time to a date. With an end time, the time
// is a range such as `14:00–15:30`. With no time it returns the date alone.
func withTaskClock(date, clock, end, timeFormat string) string {
	if clock == "" {
		return date
	}
	text := date + " " + formatTaskClock(clock, timeFormat)
	if end != "" {
		text += "–" + formatTaskClock(end, timeFormat)
	}
	return text
}
