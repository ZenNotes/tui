package vault

import "strings"

// IsBlockIDOnlyLine reports whether text holds a block id and nothing else.
// A repeating task's roll leaves such a line out of the record.
func IsBlockIDOnlyLine(text string) bool {
	from := trailingBlockIDFrom(text)
	return from >= 0 && strings.TrimSpace(withoutBlockID(text, from)) == ""
}
