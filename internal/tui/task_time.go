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
