package vim

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Cmdline is the command line's current state for rendering.
func (e *Editor) Cmdline() (kind rune, text string, cursor int, active bool) {
	if e.mode != ModeCmdline {
		return 0, "", 0, false
	}
	return e.cmd.kind, string(e.cmd.text), e.cmd.pos, true
}

// OpenCmdline starts an ex or search line from the host.
func (e *Editor) OpenCmdline(kind rune, initial string) {
	e.openCmdline(kind, 0, false)
	e.cmd.text = []rune(initial)
	e.cmd.pos = len(e.cmd.text)
}

func (e *Editor) openCmdline(kind rune, count int, hasCount bool) {
	if e.inVisual() {
		e.exitVisual()
	}
	e.cmd = cmdlineState{kind: kind, histIdx: -1, searchOrg: e.cursor}
	if kind == ':' && hasCount {
		if count <= 1 {
			e.cmd.text = []rune(".")
		} else {
			e.cmd.text = []rune(".,.+" + strconv.Itoa(count-1))
		}
		e.cmd.pos = len(e.cmd.text)
	}
	if kind != ':' && hasCount {
		e.cmd.original = []rune(strconv.Itoa(count))
	}
	e.mode = ModeCmdline
}

func (e *Editor) history(kind rune) *[]string {
	if e.histories == nil {
		e.histories = map[rune]*[]string{}
	}
	key := kind
	if kind == '?' {
		key = '/'
	}
	if e.histories[key] == nil {
		h := []string{}
		e.histories[key] = &h
	}
	return e.histories[key]
}

func (e *Editor) cmdlineKey(k Key) {
	c := &e.cmd
	if e.opSearch != nil {
		e.opSearch.keys = append(e.opSearch.keys, k)
	}
	switch {
	case k.Is("esc") || k.IsCtrl('c') || k.IsCtrl('['):
		e.cancelCmdline()
		return
	case k.Is("enter") || k.IsCtrl('j') || k.IsCtrl('m'):
		e.submitCmdline()
		return
	case k.Is("backspace") || k.IsCtrl('h'):
		if len(c.text) == 0 {
			e.cancelCmdline()
			return
		}
		if c.pos > 0 {
			c.text = append(c.text[:c.pos-1], c.text[c.pos:]...)
			c.pos--
		}
	case k.Is("delete"):
		if c.pos < len(c.text) {
			c.text = append(c.text[:c.pos], c.text[c.pos+1:]...)
		}
	case k.Is("left"):
		if c.pos > 0 {
			c.pos--
		}
	case k.Is("right"):
		if c.pos < len(c.text) {
			c.pos++
		}
	case k.Is("home") || k.IsCtrl('b'):
		c.pos = 0
	case k.Is("end") || k.IsCtrl('e'):
		c.pos = len(c.text)
	case k.IsCtrl('u'):
		c.text = c.text[c.pos:]
		c.pos = 0
	case k.IsCtrl('w'):
		i := c.pos
		for i > 0 && isBlank(c.text[i-1]) {
			i--
		}
		for i > 0 && !isBlank(c.text[i-1]) {
			i--
		}
		c.text = append(c.text[:i], c.text[c.pos:]...)
		c.pos = i
	case k.Is("up") || k.IsCtrl('p'):
		e.cmdlineHistory(-1)
	case k.Is("down") || k.IsCtrl('n'):
		e.cmdlineHistory(1)
	case k.Is("tab"):
		if c.kind == ':' {
			e.cmdlineComplete(k.Shift)
			return
		}
	case k.IsCtrl('r'):
		c.pendingRegister = true
		return
	case k.Printable():
		if c.pendingRegister {
			c.pendingRegister = false
			if k.Rune == 'w' {
				c.insertText(e.WordUnderCursor())
			} else if reg, ok := e.readRegister(k.Rune); ok {
				c.insertText(strings.TrimSuffix(reg.Text, "\n"))
			}
			break
		}
		c.insertText(string(k.Rune))
	default:
		return
	}
	c.completions = nil
	if c.kind != ':' {
		e.incrementalSearch()
	}
}

func (c *cmdlineState) insertText(s string) {
	r := []rune(s)
	next := make([]rune, 0, len(c.text)+len(r))
	next = append(next, c.text[:c.pos]...)
	next = append(next, r...)
	next = append(next, c.text[c.pos:]...)
	c.text = next
	c.pos += len(r)
}

func (e *Editor) cmdlineHistory(dir int) {
	h := *e.history(e.cmd.kind)
	if len(h) == 0 {
		return
	}
	if e.cmd.histIdx < 0 {
		e.cmd.saved = string(e.cmd.text)
		e.cmd.histIdx = len(h)
	}
	e.cmd.histIdx += dir
	if e.cmd.histIdx < 0 {
		e.cmd.histIdx = 0
	}
	if e.cmd.histIdx >= len(h) {
		e.cmd.histIdx = -1
		e.cmd.text = []rune(e.cmd.saved)
		e.cmd.pos = len(e.cmd.text)
		return
	}
	e.cmd.text = []rune(h[e.cmd.histIdx])
	e.cmd.pos = len(e.cmd.text)
}

var builtinExNames = []string{
	"substitute", "s", "global", "g", "vglobal", "v", "delete", "d", "yank", "y", "move", "m", "copy", "co", "t",
	"join", "j", "normal", "norm", "sort", "nohlsearch", "noh", "set", "undo", "redo", "registers", "reg", "display",
	"marks", "put", "pu", "retab", "left", "right", "center",
}

// cmdlineComplete cycles command names matching the typed prefix.
func (e *Editor) cmdlineComplete(backward bool) {
	c := &e.cmd
	if c.completions == nil {
		text := string(c.text[:c.pos])
		word := text
		if i := strings.LastIndexAny(text, " "); i >= 0 {
			word = text[i+1:]
		}
		base := text[:len(text)-len(word)]
		var names []string
		hostOwned := false
		if e.hooks.ExComplete != nil {
			if keep, cands := e.hooks.ExComplete(text); cands != nil && strings.HasPrefix(text, keep) {
				base, word = keep, text[len(keep):]
				names = cands
				hostOwned = true
			}
		}
		if !hostOwned || base == "" {
			names = append(append([]string(nil), builtinExNames...), names...)
		}
		c.compPrefix = word
		c.compBase = base
		seen := map[string]bool{}
		matches := []string{}
		lowerWord := strings.ToLower(word)
		for _, n := range names {
			if strings.HasPrefix(strings.ToLower(n), lowerWord) && !seen[n] {
				seen[n] = true
				matches = append(matches, n)
			}
		}
		if !hostOwned {
			sort.Strings(matches)
		}
		if len(matches) == 0 {
			return
		}
		c.completions = append(matches, word)
		c.compIdx = -1
	}
	if backward {
		c.compIdx--
		if c.compIdx < 0 {
			c.compIdx = len(c.completions) - 1
		}
	} else {
		c.compIdx = (c.compIdx + 1) % len(c.completions)
	}
	tail := string(c.text[c.pos:])
	c.text = []rune(c.compBase + c.completions[c.compIdx] + tail)
	c.pos = len([]rune(c.compBase + c.completions[c.compIdx]))
}

// Completions lists the current completion candidates (for a wildmenu).
func (e *Editor) Completions() ([]string, int) {
	if e.mode != ModeCmdline || len(e.cmd.completions) == 0 {
		return nil, -1
	}
	return e.cmd.completions[:len(e.cmd.completions)-1], e.cmd.compIdx
}

func (e *Editor) incrementalSearch() {
	pattern := string(e.cmd.text)
	if pattern == "" {
		e.cursor = e.buf.clampNormal(e.cmd.searchOrg)
		e.incremental = nil
		return
	}
	re, err := e.compilePattern(pattern)
	if err != nil {
		return
	}
	dir := 1
	if e.cmd.kind == '?' {
		dir = -1
	}
	p, _, ok := e.findMatch(re, e.cmd.searchOrg, dir)
	if !ok {
		e.cursor = e.buf.clampNormal(e.cmd.searchOrg)
		e.incremental = nil
		return
	}
	e.cursor = e.buf.clampNormal(p)
	e.incremental = re
	e.ensureCursorVisible()
}

// IncrementalHighlights lists spans of the pattern being typed on a line.
func (e *Editor) IncrementalHighlights(line int) [][2]int {
	if e.mode != ModeCmdline || e.incremental == nil {
		return nil
	}
	return runeMatches(e.incremental, e.buf.LineString(line))
}

func (e *Editor) cancelCmdline() {
	if e.cmd.kind != ':' {
		e.cursor = e.buf.clampNormal(e.cmd.searchOrg)
	}
	e.incremental = nil
	e.opSearch = nil
	e.mode = ModeNormal
	e.cmd = cmdlineState{}
	e.ensureCursorVisible()
}

func (e *Editor) submitCmdline() {
	kind := e.cmd.kind
	text := string(e.cmd.text)
	count := 1
	if s := string(e.cmd.original); s != "" {
		count, _ = strconv.Atoi(s)
	}
	e.incremental = nil
	e.mode = ModeNormal
	if strings.TrimSpace(text) != "" {
		h := e.history(kind)
		*h = append(*h, text)
		if len(*h) > 100 {
			*h = (*h)[len(*h)-100:]
		}
	}
	switch kind {
	case '/', '?':
		dir := 1
		if kind == '?' {
			dir = -1
		}
		if pending := e.opSearch; pending != nil {
			e.opSearch = nil
			e.runOperatorSearch(pending, text, dir)
			break
		}
		e.runSearch(text, dir, count)
	default:
		if text != "" {
			e.lastExLine = text
		}
		e.executeExLine(text)
	}
	e.cmd = cmdlineState{}
	e.cursor = e.buf.clampNormal(e.cursor)
	if e.mode == ModeNormal {
		e.ensureCursorVisible()
	}
}

// --- ex parsing ---

// parseRange reads an address range off the front of a command line.
func (e *Editor) parseRange(line string) (Range, string, error) {
	rest := strings.TrimLeft(line, " \t:")
	rng := Range{}
	if strings.HasPrefix(rest, "%") {
		rng = Range{Start: 0, End: e.buf.LineCount() - 1, Given: true}
		rest = rest[1:]
		return rng, strings.TrimLeft(rest, " "), nil
	}
	addrs := []int{}
	for {
		addr, next, ok, err := e.parseAddress(rest)
		if err != nil {
			return Range{}, "", err
		}
		if !ok {
			break
		}
		addrs = append(addrs, addr)
		rest = next
		if strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, ";") {
			if rest[0] == ';' {
				e.cursor = e.buf.clampNormal(Pos{addr, 0})
			}
			rest = rest[1:]
			continue
		}
		break
	}
	switch len(addrs) {
	case 0:
	case 1:
		rng = Range{Start: addrs[0], End: addrs[0], Given: true}
	default:
		rng = Range{Start: addrs[len(addrs)-2], End: addrs[len(addrs)-1], Given: true}
	}
	if rng.Given {
		if rng.Start > rng.End {
			rng.Start, rng.End = rng.End, rng.Start
		}
		rng.Start = min(max(0, rng.Start), e.buf.LineCount()-1)
		rng.End = min(max(0, rng.End), e.buf.LineCount()-1)
	}
	return rng, strings.TrimLeft(rest, " "), nil
}

// parseAddress reads one address: N, ., $, 'x, /pat/, ?pat?, with +N/-N
// offsets; ok is false when there is none.
func (e *Editor) parseAddress(s string) (int, string, bool, error) {
	if s == "" {
		return 0, s, false, nil
	}
	base := -1
	rest := s
	switch {
	case s[0] >= '0' && s[0] <= '9':
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		n, _ := strconv.Atoi(s[:i])
		base = n - 1
		rest = s[i:]
	case s[0] == '.':
		base = e.cursor.Line
		rest = s[1:]
	case s[0] == '$':
		base = e.buf.LineCount() - 1
		rest = s[1:]
	case s[0] == '\'' && len(s) > 1:
		p, ok := e.markPos(rune(s[1]))
		if !ok {
			return 0, s, false, fmt.Errorf("E20: Mark not set")
		}
		base = p.Line
		rest = s[2:]
	case s[0] == '/' || s[0] == '?':
		delim := s[0]
		end := strings.IndexByte(s[1:], delim)
		var pattern string
		if end < 0 {
			pattern = s[1:]
			rest = ""
		} else {
			pattern = s[1 : 1+end]
			rest = s[2+end:]
		}
		if pattern == "" {
			pattern = e.lastSearch
		}
		re, err := e.compilePattern(pattern)
		if err != nil {
			return 0, s, false, fmt.Errorf("E486: Pattern not found: %s", pattern)
		}
		dir := 1
		if delim == '?' {
			dir = -1
		}
		from := Pos{e.cursor.Line, 0}
		if dir > 0 {
			from = Pos{e.cursor.Line, e.buf.LineLen(e.cursor.Line)}
		}
		p, _, ok := e.findMatch(re, from, dir)
		if !ok {
			return 0, s, false, fmt.Errorf("E486: Pattern not found: %s", pattern)
		}
		base = p.Line
	case s[0] == '+' || s[0] == '-':
		base = e.cursor.Line
	default:
		return 0, s, false, nil
	}
	for len(rest) > 0 && (rest[0] == '+' || rest[0] == '-') {
		sign := 1
		if rest[0] == '-' {
			sign = -1
		}
		rest = rest[1:]
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		n := 1
		if i > 0 {
			n, _ = strconv.Atoi(rest[:i])
			rest = rest[i:]
		}
		base += sign * n
	}
	return base, rest, true, nil
}

// executeExLine runs one command line, ranges included.
func (e *Editor) executeExLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	rng, rest, err := e.parseRange(line)
	if err != nil {
		e.setError(err.Error())
		return
	}
	if rest == "" {
		if rng.Given {
			e.GotoLine(rng.End + 1)
		}
		return
	}
	// Command name: letters, or a single symbol like & < > = !.
	name := ""
	i := 0
	for i < len(rest) && (unicode.IsLetter(rune(rest[i])) || rest[i] == '_') {
		i++
	}
	if i == 0 {
		name = rest[:1]
		i = 1
	} else {
		name = rest[:i]
	}
	bang := false
	if i < len(rest) && rest[i] == '!' {
		bang = true
		i++
	}
	args := strings.TrimSpace(rest[i:])
	cmd := ExCommand{Name: name, Args: args, Bang: bang, Range: rng, Raw: line}
	if e.builtinEx(cmd) {
		return
	}
	if e.hostEx(cmd) {
		return
	}
	e.setError("E492: Not an editor command: " + line)
}

func (e *Editor) rangeOrCurrent(r Range) (int, int) {
	if r.Given {
		return r.Start, r.End
	}
	return e.cursor.Line, e.cursor.Line
}

// builtinEx runs the commands the buffer owns.
func (e *Editor) builtinEx(cmd ExCommand) bool {
	start, end := e.rangeOrCurrent(cmd.Range)
	switch cmd.Name {
	case "s", "substitute", "&", "~":
		e.beginChange()
		defer e.commitChange()
		if cmd.Name == "&" || cmd.Name == "~" {
			if e.lastSubst == nil {
				e.setError("E35: No previous regular expression")
				return true
			}
			flags := cmd.Args
			if _, err := e.substitute(start, end, e.lastSubst.pattern, e.lastSubst.repl, flags); err != nil {
				e.setError(err.Error())
			}
			return true
		}
		pattern, repl, flags, ok := parseSubstitute(cmd.Args)
		if !ok {
			if e.lastSubst == nil {
				e.setError("E35: No previous regular expression")
				return true
			}
			pattern, repl, flags = e.lastSubst.pattern, e.lastSubst.repl, strings.TrimSpace(cmd.Args)
		}
		if strings.Contains(flags, "&") && e.lastSubst != nil {
			flags = strings.ReplaceAll(flags, "&", "") + e.lastSubst.flags
		}
		if _, err := e.substitute(start, end, pattern, repl, flags); err != nil {
			e.setError(err.Error())
		}
		return true
	case "g", "global", "v", "vglobal":
		e.beginChange()
		defer e.commitChange()
		e.globalCommand(cmd, cmd.Name == "v" || cmd.Name == "vglobal" || (cmd.Bang && (cmd.Name == "g" || cmd.Name == "global")))
		return true
	case "d", "delete":
		e.beginChange()
		defer e.commitChange()
		reg, count := parseRegisterAndCount(cmd.Args)
		if count > 0 {
			start = end
			end = min(e.buf.LineCount()-1, start+count-1)
		}
		e.deleteSpan(span{start: Pos{start, 0}, end: Pos{end, 0}, linewise: true}, reg)
		return true
	case "y", "yank":
		reg, count := parseRegisterAndCount(cmd.Args)
		if count > 0 {
			start = end
			end = min(e.buf.LineCount()-1, start+count-1)
		}
		e.yankLines(start, end, reg)
		return true
	case "m", "move":
		if !looksLikeAddress(cmd.Args) {
			return false
		}
		e.beginChange()
		defer e.commitChange()
		target, _, ok, err := e.parseAddress(cmd.Args)
		if err != nil || !ok {
			if cmd.Args == "0" {
				target, ok = -1, true
			} else {
				e.setError("E14: Invalid address")
				return true
			}
		}
		e.moveLines(start, end, target)
		return true
	case "t", "co", "copy":
		e.beginChange()
		defer e.commitChange()
		target, _, ok, err := e.parseAddress(cmd.Args)
		if err != nil || (!ok && cmd.Args != "0") {
			e.setError("E14: Invalid address")
			return true
		}
		if cmd.Args == "0" {
			target = -1
		}
		lines := e.buf.Lines()[start : end+1]
		e.buf.InsertLines(target+1, lines)
		e.cursor = Pos{target + len(lines), firstNonBlank(e.buf.Line(target + len(lines)))}
		return true
	case ">", "<":
		e.beginChange()
		defer e.commitChange()
		times := 1 + strings.Count(cmd.Args, cmd.Name)
		e.shiftLines(start, end, cmd.Name == ">", times)
		e.cursor = Pos{end, firstNonBlank(e.buf.Line(end))}
		return true
	case "j", "join":
		e.beginChange()
		defer e.commitChange()
		count := end - start + 1
		if n, err := strconv.Atoi(strings.TrimSpace(cmd.Args)); err == nil {
			count = n
		}
		if !cmd.Range.Given || count < 2 {
			count = max(count, 2)
		}
		e.joinLines(start, count, !cmd.Bang)
		return true
	case "norm", "normal":
		e.beginChange()
		defer e.commitChange()
		keys := ParseKeys(cmd.Args)
		for l := start; l <= end; l++ {
			e.cursor = e.buf.clampNormal(Pos{l, 0})
			e.pending = nil
			for _, k := range keys {
				e.HandleKey(k)
			}
			if e.mode == ModeInsert || e.mode == ModeReplace {
				e.HandleKey(KeyEsc)
			}
			if e.inVisual() {
				e.exitVisual()
			}
			if e.mode == ModeCmdline {
				e.cancelCmdline()
			}
		}
		return true
	case "sort":
		e.beginChange()
		defer e.commitChange()
		if !cmd.Range.Given {
			start, end = 0, e.buf.LineCount()-1
		}
		e.sortLines(start, end, cmd.Bang, cmd.Args)
		return true
	case "noh", "nohlsearch":
		e.highlight = false
		return true
	case "set", "se":
		e.setOption(cmd.Args)
		return true
	case "undo", "u":
		e.Undo()
		return true
	case "redo", "red":
		e.Redo()
		return true
	case "reg", "registers", "di", "display":
		e.setMsg(e.registerListing())
		return true
	case "marks":
		e.setMsg(e.markListing())
		return true
	case "pu", "put":
		e.beginChange()
		defer e.commitChange()
		reg := rune(0)
		if a := strings.TrimSpace(cmd.Args); a != "" {
			reg = []rune(a)[0]
		}
		r, ok := e.readRegister(reg)
		if !ok {
			e.setError("E353: Nothing in register")
			return true
		}
		lines := strings.Split(strings.TrimSuffix(r.Text, "\n"), "\n")
		at := end + 1
		if cmd.Bang {
			at = start
		}
		e.buf.InsertLines(at, lines)
		e.cursor = Pos{at + len(lines) - 1, 0}
		return true
	case "=":
		e.setMsg(strconv.Itoa(end + 1))
		return true
	case "k", "mark", "ma":
		if a := strings.TrimSpace(cmd.Args); a != "" {
			e.marks[[]rune(a)[0]] = Pos{end, 0}
		}
		return true
	case "retab":
		e.beginChange()
		defer e.commitChange()
		for l := 0; l < e.buf.LineCount(); l++ {
			s := e.buf.LineString(l)
			if strings.Contains(s, "\t") {
				e.buf.ReplaceLine(l, []rune(strings.ReplaceAll(s, "\t", strings.Repeat(" ", e.opts.TabSize))))
			}
		}
		return true
	}
	return false
}

func looksLikeAddress(args string) bool {
	a := strings.TrimSpace(args)
	if a == "" {
		return false
	}
	switch a[0] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.', '$', '\'', '+', '-', '/', '?':
		return true
	}
	return false
}

func parseRegisterAndCount(args string) (rune, int) {
	args = strings.TrimSpace(args)
	reg := rune(0)
	if args != "" && !unicode.IsDigit(rune(args[0])) {
		reg = []rune(args)[0]
		args = strings.TrimSpace(args[len(string(reg)):])
	}
	count := 0
	if n, err := strconv.Atoi(args); err == nil {
		count = n
	}
	return reg, count
}

func (e *Editor) moveLines(start, end, target int) {
	if target >= start && target <= end {
		if target == end {
			return
		}
		e.setError("E134: Cannot move a range of lines into itself")
		return
	}
	lines := e.buf.DeleteLines(start, end)
	if target > end {
		target -= len(lines)
	}
	e.buf.InsertLines(target+1, lines)
	e.cursor = Pos{target + len(lines), 0}
	e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
}

func (e *Editor) globalCommand(cmd ExCommand, invert bool) {
	args := cmd.Args
	if args == "" {
		e.setError("E35: No previous regular expression")
		return
	}
	delim := []rune(args)[0]
	rest := []rune(args)[1:]
	end := -1
	for i, r := range rest {
		if r == delim && (i == 0 || rest[i-1] != '\\') {
			end = i
			break
		}
	}
	var pattern, sub string
	if end < 0 {
		pattern = string(rest)
		sub = "p"
	} else {
		pattern = string(rest[:end])
		sub = strings.TrimSpace(string(rest[end+1:]))
	}
	if pattern == "" {
		pattern = e.lastSearch
	}
	re, err := e.compilePattern(pattern)
	if err != nil {
		e.setError("E486: Pattern not found: " + pattern)
		return
	}
	e.lastSearch = pattern
	start, stop := 0, e.buf.LineCount()-1
	if cmd.Range.Given {
		start, stop = cmd.Range.Start, cmd.Range.End
	}
	if sub == "" {
		sub = "p"
	}
	marked := []int{}
	for l := start; l <= stop && l < e.buf.LineCount(); l++ {
		if re.MatchString(e.buf.LineString(l)) != invert {
			marked = append(marked, l)
		}
	}
	// Run the command on each marked line, tracking line shifts.
	shift := 0
	before := e.buf.LineCount()
	for _, l := range marked {
		line := l + shift
		if line < 0 || line >= e.buf.LineCount() {
			continue
		}
		e.cursor = e.buf.clampNormal(Pos{line, 0})
		countBefore := e.buf.LineCount()
		e.executeExLine(sub)
		shift += e.buf.LineCount() - countBefore
	}
	_ = before
}

func (e *Editor) sortLines(start, end int, reverse bool, args string) {
	lines := e.buf.Lines()[start : end+1]
	unique := strings.Contains(args, "u")
	ignoreCase := strings.Contains(args, "i")
	numeric := strings.Contains(args, "n")
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if numeric {
			na, nb := leadingNumber(a), leadingNumber(b)
			if na != nb {
				return na < nb
			}
		}
		if ignoreCase {
			a, b = strings.ToLower(a), strings.ToLower(b)
		}
		return a < b
	})
	if reverse {
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
	}
	if unique {
		out := []string{}
		for i, l := range lines {
			if i > 0 && (l == lines[i-1] || (ignoreCase && strings.EqualFold(l, lines[i-1]))) {
				continue
			}
			out = append(out, l)
		}
		lines = out
	}
	e.buf.ReplaceLines(start, end, lines)
	e.cursor = e.buf.clampNormal(Pos{start, 0})
}

func leadingNumber(s string) int {
	i := 0
	for i < len(s) && !(s[i] >= '0' && s[i] <= '9') && s[i] != '-' {
		i++
	}
	j := i
	if j < len(s) && s[j] == '-' {
		j++
	}
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	n, _ := strconv.Atoi(s[i:j])
	return n
}

// setOption handles the :set options the buffer understands.
func (e *Editor) setOption(args string) {
	for _, opt := range strings.Fields(args) {
		name, value, hasValue := strings.Cut(opt, "=")
		on := true
		if strings.HasPrefix(name, "no") && !hasValue {
			on = false
			name = name[2:]
		}
		if strings.HasSuffix(name, "!") {
			name = strings.TrimSuffix(name, "!")
			on = !e.optionBool(name)
		}
		switch name {
		case "ic", "ignorecase":
			e.opts.IgnoreCase = on
		case "scs", "smartcase":
			e.opts.SmartCase = on
		case "ws", "wrapscan":
			e.opts.WrapScan = on
		case "hls", "hlsearch":
			e.highlight = on
		case "ts", "tabstop", "sw", "shiftwidth":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				e.opts.TabSize = n
			}
		case "so", "scrolloff":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				e.opts.ScrollOff = n
			}
		default:
			if e.hooks.ExCommand != nil {
				e.hostEx(ExCommand{Name: "set", Args: opt, Raw: "set " + opt})
			}
		}
	}
}

func (e *Editor) optionBool(name string) bool {
	switch name {
	case "ic", "ignorecase":
		return e.opts.IgnoreCase
	case "scs", "smartcase":
		return e.opts.SmartCase
	case "ws", "wrapscan":
		return e.opts.WrapScan
	case "hls", "hlsearch":
		return e.highlight
	}
	return false
}

func (e *Editor) registerListing() string {
	names := []rune{}
	for r := range e.registers {
		names = append(names, r)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	parts := []string{}
	for _, r := range names {
		text := strings.ReplaceAll(e.registers[r].Text, "\n", "^J")
		if len([]rune(text)) > 40 {
			text = string([]rune(text)[:40]) + "…"
		}
		parts = append(parts, fmt.Sprintf("\"%c %s", r, text))
	}
	if len(parts) == 0 {
		return "--- Registers --- (empty)"
	}
	return "--- Registers --- " + strings.Join(parts, " | ")
}

func (e *Editor) markListing() string {
	names := []rune{}
	for r := range e.marks {
		names = append(names, r)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	parts := []string{}
	for _, r := range names {
		p := e.marks[r]
		parts = append(parts, fmt.Sprintf("%c %d:%d", r, p.Line+1, p.Col+1))
	}
	if len(parts) == 0 {
		return "--- Marks --- (none)"
	}
	return "--- Marks --- " + strings.Join(parts, "  ")
}

// ExecuteEx runs an ex command line from the host (`:` prefix optional).
func (e *Editor) ExecuteEx(line string) {
	e.executeExLine(strings.TrimPrefix(line, ":"))
	e.cursor = e.buf.clampNormal(e.cursor)
	e.ensureCursorVisible()
}

// runOperatorSearch finishes `d/pat<CR>`: the search target is the motion.
func (e *Editor) runOperatorSearch(pending *pendingOpSearch, pattern string, dir int) {
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
	e.cursor = e.buf.clampNormal(pending.from)
	target, ok := e.searchNext(pending.from, dir, max(1, pending.count), false)
	if !ok {
		return
	}
	versionBefore := e.version
	e.beginChange()
	entered := e.applyOperatorResult(pending.op, pending.from, motionResult{pos: target, jump: true}, pending.register, false)
	if entered {
		if !e.replaying {
			e.recordingIns = true
			e.changeKeys = append([]Key(nil), pending.keys...)
		}
		return
	}
	e.commitChange()
	if e.version != versionBefore && !e.replaying {
		e.last = lastChange{keys: stripCount(pending.keys), register: pending.register}
	}
}
