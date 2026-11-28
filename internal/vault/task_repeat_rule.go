package vault

// HasValidRepeatRule says whether text carries an `@repeat:` field whose rule
// reads. A task with any other rule is an ordinary task.
func HasValidRepeatRule(text string) bool {
	field := repeatFieldRe.FindStringSubmatch(text)
	if field == nil {
		return false
	}
	_, ok := parseRepeatRule(field[1])
	return ok
}
