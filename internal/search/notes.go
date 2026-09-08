// Package search is the note-search scoring the app's search palette uses:
// exact and prefix matches first, then word starts, then substrings, then a
// fuzzy subsequence for short queries; `#tag` tokens narrow the set.
package search

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// Entry is a note prepared for scoring.
type Entry struct {
	Note         vault.NoteMeta
	titleLower   string
	pathLower    string
	excerptLower string
	tags         string
	tagsLower    []string
}

// Index prepares notes for repeated searches.
func Index(notes []vault.NoteMeta) []Entry {
	out := make([]Entry, 0, len(notes))
	for _, n := range notes {
		tagsLower := make([]string, len(n.Tags))
		for i, t := range n.Tags {
			tagsLower[i] = Fold(t)
		}
		out = append(out, Entry{
			Note:         n,
			titleLower:   Fold(n.Title),
			pathLower:    Fold(n.Path),
			excerptLower: Fold(n.Excerpt),
			tags:         strings.Join(tagsLower, " "),
			tagsLower:    tagsLower,
		})
	}
	return out
}

// exactFirstMinQueryChars: at four characters and above, a real substring
// match is strong enough to suppress incidental subsequences.
const exactFirstMinQueryChars = 4

func parseQuery(query string) (freeText string, tagTokens []string) {
	text := []string{}
	for _, token := range strings.Fields(query) {
		if strings.HasPrefix(token, "#") && len(token) > 1 {
			tagTokens = append(tagTokens, Fold(token[1:]))
		} else {
			text = append(text, token)
		}
	}
	return strings.TrimSpace(strings.Join(text, " ")), tagTokens
}

func (e Entry) matchesTags(tokens []string) bool {
	for _, tag := range tokens {
		found := false
		for _, t := range e.tagsLower {
			if t == tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func isWordBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ':', '_', '-', '/':
		return true
	}
	return false
}

func scoreExact(query, text string) float64 {
	if query == "" {
		return 1
	}
	if text == "" {
		return 0
	}
	if text == query {
		return 1000
	}
	if strings.HasPrefix(text, query) {
		return 900 - float64(len(text))*0.5
	}
	index := strings.Index(text, query)
	if index == -1 {
		return 0
	}
	for index != -1 {
		if index == 0 || isWordBoundary(text[index-1]) {
			return 700 - float64(len(text))*0.5
		}
		next := strings.Index(text[index+1:], query)
		if next == -1 {
			break
		}
		index = index + 1 + next
	}
	return 500 - float64(len(text))*0.5
}

func scoreMatch(query, text string, allowFuzzy bool) float64 {
	exact := scoreExact(query, text)
	if exact > 0 || !allowFuzzy || len(query) > len(text) {
		return exact
	}
	i, gaps, prev := 0, 0, -1
	qr, tr := []rune(query), []rune(text)
	for j := 0; j < len(tr) && i < len(qr); j++ {
		if tr[j] == qr[i] {
			if prev == -1 {
				gaps += j
			} else {
				gaps += j - prev - 1
			}
			prev = j
			i++
		}
	}
	if i == len(qr) {
		return max(1, 200-float64(gaps)*3-float64(len(tr))*0.2)
	}
	return 0
}

func (e Entry) score(query string, allowFuzzy bool) float64 {
	return max(
		scoreMatch(query, e.titleLower, allowFuzzy)*0.7,
		scoreMatch(query, e.pathLower, allowFuzzy)*0.25,
		scoreMatch(query, e.excerptLower, allowFuzzy)*0.2,
		scoreMatch(query, e.tags, allowFuzzy)*0.1,
	)
}

type scored struct {
	entry Entry
	score float64
}

// Notes runs a query over the index. With no free text the live notes come
// back in their given order, tag filters applied; quick notes first and most
// recent first when quickFirst is set.
func Notes(index []Entry, query string, limit int, quickFirst bool) []vault.NoteMeta {
	freeText, tagTokens := parseQuery(query)
	if limit <= 0 {
		return nil
	}
	if freeText == "" {
		live := []Entry{}
		for _, e := range index {
			if e.Note.Folder != vault.FolderTrash && e.matchesTags(tagTokens) {
				live = append(live, e)
			}
		}
		if quickFirst {
			sort.SliceStable(live, func(i, j int) bool {
				a, b := live[i].Note, live[j].Note
				if (a.Folder == vault.FolderQuick) != (b.Folder == vault.FolderQuick) {
					return a.Folder == vault.FolderQuick
				}
				return a.UpdatedAt > b.UpdatedAt
			})
		}
		out := []vault.NoteMeta{}
		for i, e := range live {
			if i >= limit {
				break
			}
			out = append(out, e.Note)
		}
		return out
	}
	prepared := Fold(freeText)
	collect := func(allowFuzzy bool) []scored {
		matches := []scored{}
		for _, e := range index {
			if e.Note.Folder == vault.FolderTrash || !e.matchesTags(tagTokens) {
				continue
			}
			s := e.score(prepared, allowFuzzy)
			if s <= 0 {
				continue
			}
			matches = append(matches, scored{e, s})
		}
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].score != matches[j].score {
				return matches[i].score > matches[j].score
			}
			return matches[i].entry.Note.UpdatedAt > matches[j].entry.Note.UpdatedAt
		})
		return matches
	}
	var matches []scored
	if len([]rune(prepared)) >= exactFirstMinQueryChars {
		matches = collect(false)
	}
	if len(matches) == 0 {
		matches = collect(true)
	}
	out := []vault.NoteMeta{}
	for i, m := range matches {
		if i >= limit {
			break
		}
		out = append(out, m.entry.Note)
	}
	return out
}

// Fuzzy is a general-purpose scorer for palettes over arbitrary labels:
// exact, prefix, word-start, substring, then subsequence. 0 means no match.
func Fuzzy(query, label string) float64 {
	q := Fold(strings.TrimSpace(query))
	if q == "" {
		return 1
	}
	return scoreMatch(q, Fold(label), true)
}
