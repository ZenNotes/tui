package search

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Fold lower-cases a string and strips diacritics, so "resume" finds
// "Résumé". Nonspacing marks (unicode category Mn) are what accents
// decompose into under NFD.
func Fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return strings.ToLower(s)
	}
	return strings.ToLower(out)
}

// foldRune folds one rune to its base form, or "" when it is a mark.
func foldRune(r rune) string {
	return Fold(string(r))
}

// MatchPositions reports which runes of label a query matches as an
// in-order subsequence, case- and accent-insensitively. It returns nil when
// the query does not match. Used to underline matches in pickers.
func MatchPositions(query, label string) []int {
	q := []rune(Fold(query))
	if len(q) == 0 {
		return []int{}
	}
	positions := []int{}
	qi := 0
	for i, r := range []rune(label) {
		folded := []rune(foldRune(r))
		if len(folded) == 0 {
			continue
		}
		if folded[0] == q[qi] {
			positions = append(positions, i)
			qi++
			if qi == len(q) {
				return positions
			}
		}
	}
	return nil
}
