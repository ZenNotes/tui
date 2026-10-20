package vim

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// translatePattern turns a Vim (magic) pattern into a Go regexp source:
// `\<` and `\>` become word boundaries, `\(` `\|` `\{` `\+` `\?` `\=` are
// the grouping and repetition operators, and the bare forms of those
// characters are literals. `\c` and `\C` set case handling; `\v` switches to
// very magic (Go syntax as is) and `\V` to a literal pattern.
func translatePattern(pattern string) (source string, forceIgnore, forceMatch bool) {
	var b strings.Builder
	runes := []rune(pattern)
	veryMagic, noMagic := false, false
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			i++
			switch next {
			case '<', '>':
				b.WriteString(`\b`)
			case 'c':
				forceIgnore = true
			case 'C':
				forceMatch = true
			case 'v':
				veryMagic = true
			case 'V':
				noMagic = true
			case 'm', 'M':
			case '(', ')', '|', '{', '}', '+', '?', '=':
				if next == '=' {
					b.WriteString("?")
				} else {
					b.WriteRune(next)
				}
			case 't':
				b.WriteString(`\t`)
			case 'n':
				b.WriteString(`\n`)
			case 's', 'S', 'd', 'D', 'w', 'W', 'b', 'B':
				b.WriteString(`\` + string(next))
			case 'a':
				b.WriteString(`[A-Za-z]`)
			case 'l':
				b.WriteString(`[a-z]`)
			case 'u':
				b.WriteString(`[A-Z]`)
			default:
				b.WriteString(regexp.QuoteMeta(string(next)))
			}
			continue
		}
		if noMagic {
			b.WriteString(regexp.QuoteMeta(string(r)))
			continue
		}
		if veryMagic {
			b.WriteRune(r)
			continue
		}
		switch r {
		case '(', ')', '|', '{', '}', '+', '?', '=':
			b.WriteString(regexp.QuoteMeta(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), forceIgnore, forceMatch
}

func (e *Editor) compilePattern(pattern string) (*regexp.Regexp, error) {
	source, forceIgnore, forceMatch := translatePattern(pattern)
	ignore := e.opts.IgnoreCase
	if e.opts.SmartCase && ignore {
		for _, r := range pattern {
			if unicode.IsUpper(r) {
				ignore = false
				break
			}
		}
	}
	if forceIgnore {
		ignore = true
	}
	if forceMatch {
		ignore = false
	}
	if ignore {
		source = "(?i)" + source
	}
	return regexp.Compile(source)
}

// searchNext finds the nth match of the last search from `from` in
// direction dir, wrapping when wrapscan is on.
func (e *Editor) searchNext(from Pos, dir, n int, report bool) (Pos, bool) {
	if e.lastSearch == "" {
		e.setError("E35: No previous regular expression")
		return from, false
	}
	re, err := e.compilePattern(e.lastSearch)
	if err != nil {
		e.setError("E486: Pattern not found: " + e.lastSearch)
		return from, false
	}
	p := from
	for i := 0; i < n; i++ {
		next, wrapped, ok := e.findMatch(re, p, dir)
		if !ok {
			e.setError("E486: Pattern not found: " + e.lastSearch)
			return from, false
		}
		if wrapped && report {
			if dir > 0 {
				e.setMsg("search hit BOTTOM, continuing at TOP")
			} else {
				e.setMsg("search hit TOP, continuing at BOTTOM")
			}
		}
		p = next
	}
	e.highlight = true
	return p, true
}

// findMatch is the next match strictly after (or before) from.
func (e *Editor) findMatch(re *regexp.Regexp, from Pos, dir int) (Pos, bool, bool) {
	lineCount := e.buf.LineCount()
	matchesOn := func(line int) [][2]int {
		return runeMatches(re, e.buf.LineString(line))
	}
	if dir > 0 {
		for offset := 0; offset <= lineCount; offset++ {
			line := (from.Line + offset) % lineCount
			wrapped := from.Line+offset >= lineCount
			if wrapped && !e.opts.WrapScan {
				return from, false, false
			}
			for _, m := range matchesOn(line) {
				if offset == 0 && !wrapped && m[0] <= from.Col {
					continue
				}
				if offset == lineCount && m[0] > from.Col {
					continue
				}
				return Pos{line, m[0]}, wrapped, true
			}
		}
		return from, false, false
	}
	for offset := 0; offset <= lineCount; offset++ {
		line := ((from.Line-offset)%lineCount + lineCount) % lineCount
		wrapped := from.Line-offset < 0
		if wrapped && !e.opts.WrapScan {
			return from, false, false
		}
		ms := matchesOn(line)
		for i := len(ms) - 1; i >= 0; i-- {
			m := ms[i]
			if offset == 0 && !wrapped && m[0] >= from.Col {
				continue
			}
			if offset == lineCount && m[0] < from.Col {
				continue
			}
			return Pos{line, m[0]}, wrapped, true
		}
	}
	return from, false, false
}

// runeMatches lists match spans on a line in rune columns.
func runeMatches(re *regexp.Regexp, s string) [][2]int {
	locs := re.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return nil
	}
	out := make([][2]int, 0, len(locs))
	byteToRune := map[int]int{}
	ri := 0
	for bi := range s {
		byteToRune[bi] = ri
		ri++
	}
	byteToRune[len(s)] = ri
	for _, loc := range locs {
		start, end := byteToRune[loc[0]], byteToRune[loc[1]]
		if end == start {
			end = start + 1
		}
		out = append(out, [2]int{start, end})
	}
	return out
}

// SearchHighlights lists highlighted spans on a line while search
// highlighting is on.
func (e *Editor) SearchHighlights(line int) [][2]int {
	if !e.highlight || e.lastSearch == "" {
		return nil
	}
	re, err := e.compilePattern(e.lastSearch)
	if err != nil {
		return nil
	}
	return runeMatches(re, e.buf.LineString(line))
}

// HighlightOn reports whether search matches are shown.
func (e *Editor) HighlightOn() bool { return e.highlight && e.lastSearch != "" }

// LastSearch is the last pattern.
func (e *Editor) LastSearch() string { return e.lastSearch }

// runSearch is `/` or `?` submitted from the command line.
func (e *Editor) runSearch(pattern string, dir, count int) {
	if pattern == "" {
		pattern = e.lastSearch
	}
	if pattern == "" {
		e.setError("E35: No previous regular expression")
		return
	}
	if _, err := e.compilePattern(pattern); err != nil {
		e.setError("E486: Pattern not found: " + pattern)
		return
	}
	e.lastSearch = pattern
	e.searchDir = dir
	e.highlight = true
	p, ok := e.searchNext(e.cmd.searchOrg, dir, max(1, count), true)
	if !ok {
		e.cursor = e.buf.clampNormal(e.cmd.searchOrg)
		return
	}
	e.pushJump()
	e.cursor = e.buf.clampNormal(p)
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
}

// --- substitute ---

type substitution struct {
	pattern string
	repl    string
	flags   string
}

// parseSubstitute reads `/pat/repl/flags` with any delimiter.
func parseSubstitute(args string) (pattern, repl, flags string, ok bool) {
	if args == "" {
		return "", "", "", false
	}
	delim := []rune(args)[0]
	if unicode.IsLetter(delim) || unicode.IsDigit(delim) || delim == '\\' || delim == '"' || delim == '|' {
		return "", "", "", false
	}
	rest := []rune(args)[1:]
	parts := []string{}
	var cur strings.Builder
	for i := 0; i < len(rest); i++ {
		r := rest[i]
		if r == '\\' && i+1 < len(rest) {
			if rest[i+1] == delim {
				cur.WriteRune(delim)
				i++
				continue
			}
			cur.WriteRune(r)
			cur.WriteRune(rest[i+1])
			i++
			continue
		}
		if r == delim && len(parts) < 2 {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	parts = append(parts, cur.String())
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], strings.TrimSpace(parts[2]), true
}

// expandReplacement applies `&`, `\0`..`\9`, `\n`, `\t`, `\\`, `\&`, and
// case modifiers `\u` `\l` `\U` `\L` `\E`.
func expandReplacement(repl string, match []string) string {
	var b strings.Builder
	runes := []rune(repl)
	upperNext, lowerNext := false, false
	upperAll, lowerAll := false, false
	write := func(s string) {
		for _, r := range s {
			switch {
			case upperNext:
				r = unicode.ToUpper(r)
				upperNext = false
			case lowerNext:
				r = unicode.ToLower(r)
				lowerNext = false
			case upperAll:
				r = unicode.ToUpper(r)
			case lowerAll:
				r = unicode.ToLower(r)
			}
			b.WriteRune(r)
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			i++
			switch {
			case next >= '0' && next <= '9':
				idx := int(next - '0')
				if idx < len(match) {
					write(match[idx])
				}
			case next == 'n', next == 'r':
				b.WriteString("\n")
			case next == 't':
				b.WriteString("\t")
			case next == '&':
				b.WriteString("&")
			case next == 'u':
				upperNext = true
			case next == 'l':
				lowerNext = true
			case next == 'U':
				upperAll, lowerAll = true, false
			case next == 'L':
				lowerAll, upperAll = true, false
			case next == 'E' || next == 'e':
				upperAll, lowerAll = false, false
			default:
				b.WriteRune(next)
			}
			continue
		}
		if r == '&' {
			write(match[0])
			continue
		}
		write(string(r))
	}
	return b.String()
}

// substitute runs :s over a line range and returns the replacement count.
func (e *Editor) substitute(start, end int, pattern, repl, flags string) (int, error) {
	if pattern == "" {
		pattern = e.lastSearch
	}
	if pattern == "" {
		return 0, errString("E35: No previous regular expression")
	}
	re, err := e.compilePattern(pattern)
	if err != nil {
		return 0, errString("E486: Pattern not found: " + pattern)
	}
	if strings.Contains(flags, "i") {
		re, _ = regexp.Compile("(?i)" + re.String())
	}
	global := strings.Contains(flags, "g")
	countOnly := strings.Contains(flags, "n")
	e.lastSearch = pattern
	e.lastSubst = &substitution{pattern: pattern, repl: repl, flags: flags}
	e.highlight = true
	total := 0
	lastLine := -1
	l := start
	for l <= end && l < e.buf.LineCount() {
		text := e.buf.LineString(l)
		locs := re.FindAllStringSubmatchIndex(text, -1)
		if len(locs) == 0 {
			l++
			continue
		}
		if !global {
			locs = locs[:1]
		}
		total += len(locs)
		lastLine = l
		if countOnly {
			l++
			continue
		}
		var b strings.Builder
		last := 0
		for _, loc := range locs {
			groups := make([]string, len(loc)/2)
			for g := 0; g < len(loc)/2; g++ {
				if loc[2*g] >= 0 {
					groups[g] = text[loc[2*g]:loc[2*g+1]]
				}
			}
			b.WriteString(text[last:loc[0]])
			b.WriteString(expandReplacement(repl, groups))
			last = loc[1]
		}
		b.WriteString(text[last:])
		next := b.String()
		if strings.Contains(next, "\n") {
			parts := strings.Split(next, "\n")
			e.buf.ReplaceLine(l, []rune(parts[0]))
			e.buf.InsertLines(l+1, parts[1:])
			end += len(parts) - 1
			l += len(parts) - 1
		} else {
			e.buf.ReplaceLine(l, []rune(next))
		}
		l++
	}
	if total == 0 {
		return 0, errString("E486: Pattern not found: " + pattern)
	}
	if countOnly {
		e.setMsg(strconv.Itoa(total) + " matches")
		return total, nil
	}
	if lastLine >= 0 {
		e.cursor = Pos{lastLine, firstNonBlank(e.buf.Line(lastLine))}
	}
	if total > 2 || lastLine != start {
		e.setMsg(strconv.Itoa(total) + " substitutions")
	}
	return total, nil
}

func (e *Editor) repeatSubstitute(keepFlags bool) {
	if e.lastSubst == nil {
		e.setError("E35: No previous regular expression")
		return
	}
	flags := ""
	if keepFlags {
		flags = e.lastSubst.flags
	}
	if _, err := e.substitute(e.cursor.Line, e.cursor.Line, e.lastSubst.pattern, e.lastSubst.repl, flags); err != nil {
		e.setError(err.Error())
	}
}

type errString string

func (s errString) Error() string { return string(s) }
