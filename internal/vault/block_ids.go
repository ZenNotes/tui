package vault

import (
	"math/rand"
	"os"
	"regexp"
	"strings"
)

// blockIDChars are the characters of a new block id.
const blockIDChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// blockIDLen is the length of a new block id.
const blockIDLen = 6

// trailingBlockIDRe matches a block id that ends a line: a `^` and the id
// after white space, then only white space to the end. Group 1 is the id.
var trailingBlockIDRe = regexp.MustCompile(`(?:^|\s)\^([A-Za-z0-9-]+)\s*$`)

// splitBlockID cuts a trailing block id off text. It returns the text without
// the id and the id, or the text unchanged and "" when no id ends the text.
func splitBlockID(text string) (rest, id string) {
	loc := trailingBlockIDRe.FindStringSubmatchIndex(text)
	if loc == nil {
		return text, ""
	}
	return text[:loc[0]], text[loc[2]:loc[3]]
}

// stripBlockID removes a trailing block id from a line.
func stripBlockID(line string) string {
	rest, _ := splitBlockID(line)
	return rest
}

// NewBlockID gives an id of six characters from `a-z0-9` that is not in
// existing. rnd is the source of random numbers; nil uses the global source.
func NewBlockID(existing map[string]bool, rnd *rand.Rand) string {
	pick := rand.Intn
	if rnd != nil {
		pick = rnd.Intn
	}
	for {
		id := make([]byte, blockIDLen)
		for i := range id {
			id[i] = blockIDChars[pick(len(blockIDChars))]
		}
		if !existing[string(id)] {
			return string(id)
		}
	}
}

// VaultBlockIDs lists every block id that ends a line in any note of the
// vault, whether the line is a task or not.
func (v *Vault) VaultBlockIDs() (map[string]bool, error) {
	ids := map[string]bool{}
	var firstErr error
	v.walkNotes(AllFolders, false, func(f noteFile) {
		body, err := os.ReadFile(f.abs)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		for _, line := range strings.Split(string(body), "\n") {
			if _, id := splitBlockID(strings.TrimRight(line, "\r")); id != "" {
				ids[id] = true
			}
		}
	})
	return ids, firstErr
}
