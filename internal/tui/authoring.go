package tui

import (
	"fmt"
	"github.com/ZenNotes/tui/internal/vim"
	"regexp"
	"strings"
	"unicode"
)

func inMarkdownFence(lines []string, line int) bool {
	fence := ""
	for i := 0; i < line && i < len(lines); i++ {
		s := strings.TrimSpace(lines[i])
		s = strings.TrimLeft(s, "> ")
		if strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") {
			count := 0
			for count < len(s) && s[count] == s[0] {
				count++
			}
			mark := s[:count]
			if fence == "" {
				fence = mark
			} else if mark[0] == fence[0] && len(mark) >= len(fence) && strings.TrimSpace(s[count:]) == "" {
				fence = ""
			}
		}
	}
	return fence != ""
}

var blockSnippetRe = regexp.MustCompile("^([\\t ]*(?:(?:[-+*]|[0-9]+[.)])[\\t ]+|>[\\t ]?)*)((?:`{3,}|~{3,}).*|\\$\\$)$")

func (a *App) markdownSnippet(buf *noteBuffer, k vim.Key) bool {
	if !a.prefs.MarkdownSnippets || buf.ed.Mode() != vim.ModeInsert {
		return false
	}
	cur := buf.ed.Cursor()
	line := buf.ed.Line(cur.Line)
	r := []rune(line)
	if cur.Col > len(r) {
		return false
	}
	before, after := string(r[:cur.Col]), string(r[cur.Col:])
	if inMarkdownFence(buf.ed.Lines(), cur.Line) {
		return false
	}
	if k.Is("enter") && cur.Col == len(r) {
		m := blockSnippetRe.FindStringSubmatch(line)
		if m != nil {
			token := "$$"
			if strings.HasPrefix(m[2], "`") {
				token = strings.Repeat("`", len(m[2])-len(strings.TrimLeft(m[2], "`")))
			} else if strings.HasPrefix(m[2], "~") {
				token = strings.Repeat("~", len(m[2])-len(strings.TrimLeft(m[2], "~")))
			}
			indent := strings.Map(func(r rune) rune {
				if r == '\t' {
					return r
				}
				return ' '
			}, m[1])
			if cur.Line+1 < buf.ed.LineCount() && strings.TrimSpace(buf.ed.Line(cur.Line+1)) == token {
				return false
			}
			buf.ed.InsertAtCursor("\n" + indent + "\n" + indent + token)
			buf.ed.SetCursor(vim.Pos{Line: cur.Line + 1, Col: len([]rune(indent))})
			return true
		}
	}
	if !k.IsRune(' ') {
		return false
	}
	for _, pair := range [][2]string{{"**", "**"}, {"__", "__"}, {"~~", "~~"}, {"==", "=="}, {"[[", "]]"}, {"%%", "%%"}, {"`", "`"}} {
		open, close := pair[0], pair[1]
		if !strings.HasSuffix(before, open) {
			continue
		}
		prefix := strings.TrimSuffix(before, open)
		if strings.HasSuffix(prefix, "\\") || strings.HasSuffix(prefix, open[:1]) || strings.HasPrefix(after, close) || strings.Contains(after, close) {
			continue
		}
		if open != "`" && strings.Count(prefix, "`")%2 != 0 {
			return false
		}
		if open == close && strings.Count(prefix, open)%2 != 0 {
			continue
		}
		buf.ed.InsertAtCursor(close)
		buf.ed.SetCursor(cur)
		return true
	}
	return false
}
func (a *App) markdownCompletion() {
	buf := a.activeBuffer()
	if buf == nil || buf.loading {
		return
	}
	cur := buf.ed.Cursor()
	line := buf.ed.LineRunes(cur.Line)
	end := min(cur.Col, len(line))
	before := string(line[:end])
	if inMarkdownFence(buf.ed.Lines(), cur.Line) {
		a.notify("Completion is unavailable inside code blocks")
		return
	}
	start := end
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	prefix := string(line[start:end])
	choices := []string{}
	suffix := ""
	if strings.HasPrefix(strings.TrimSpace(before), "```") {
		start = strings.Index(before, "```") + 3
		choices = []string{"bash", "css", "go", "html", "javascript", "json", "markdown", "python", "rust", "sql", "swift", "typescript", "yaml"}
	} else if i := strings.LastIndex(before, "[!"); i >= 0 {
		start = len([]rune(before[:i+2]))
		choices = []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION", "INFO", "TODO", "SUCCESS", "QUESTION", "FAILURE", "DANGER", "BUG", "EXAMPLE", "QUOTE"}
		suffix = "]"
	} else if strings.HasPrefix(prefix, "#") || strings.HasPrefix(strings.TrimSpace(before), "tags:") {
		if strings.HasPrefix(strings.TrimSpace(before), "tags:") {
			// YAML flow sequences allow commas without spaces and quoted tags.
			for start < end && strings.ContainsRune("[,'\"", line[start]) {
				start++
			}
			for i := start; i < end; i++ {
				if line[i] == ',' {
					start = i + 1
				}
			}
			for start < end && (unicode.IsSpace(line[start]) || strings.ContainsRune("['\"", line[start])) {
				start++
			}
		}
		if strings.HasPrefix(prefix, "#") {
			start++
		}
		if a.idx != nil {
			for _, t := range a.idx.tags {
				choices = append(choices, t.tag)
			}
		}
	} else {
		a.notify("Complete a #tag, tags: value, [!callout or ```language")
		return
	}
	items := []paletteItem{}
	for _, s := range choices {
		items = append(items, paletteItem{label: s, id: s})
	}
	query := string(line[min(start, end):end])
	version := buf.ed.Version()
	p := &palette{title: "Markdown completion", input: newTextInput(query), items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		if buf.ed.Version() != version {
			a.notify("Note changed; reopen completion")
			return
		}
		rest := string(line[end:])
		if suffix != "" && strings.HasPrefix(rest, suffix) {
			suffix = ""
		}
		next := string(line[:start]) + it.id + suffix + rest
		buf.ed.ReplaceLineText(cur.Line, next)
		buf.ed.SetCursor(vim.Pos{Line: cur.Line, Col: start + len([]rune(it.id+suffix))})
	}}
	p.refilter(a)
	a.overlay = p
}
func tableCells(line string) []string {
	s := strings.TrimSpace(line)
	if !strings.Contains(s, "|") {
		return nil
	}
	if strings.HasPrefix(s, "|") {
		s = s[1:]
	}
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, "\\|") {
		s = s[:len(s)-1]
	}
	out := []string{}
	var b strings.Builder
	escaped := false
	ticks := false
	for _, r := range s {
		if r == '`' && !escaped {
			ticks = !ticks
		}
		if r == '|' && !escaped && !ticks {
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		} else {
			b.WriteRune(r)
		}
		if r == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
	}
	out = append(out, strings.TrimSpace(b.String()))
	return out
}

var tableSeparator = regexp.MustCompile(`^:?-{3,}:?$`)

func editMarkdownTable(body string, line, col int, action string) (string, error) {
	lines := strings.Split(body, "\n")
	if line < 0 || line >= len(lines) || inMarkdownFence(lines, line) {
		return body, fmt.Errorf("place the cursor in a Markdown table outside code fences")
	}
	start, end := line, line
	for start > 0 && len(tableCells(lines[start-1])) > 1 {
		start--
	}
	for end+1 < len(lines) && len(tableCells(lines[end+1])) > 1 {
		end++
	}
	if start+1 > end {
		return body, fmt.Errorf("table needs a header and separator")
	}
	header := tableCells(lines[start])
	sep := tableCells(lines[start+1])
	if len(header) < 2 || len(sep) != len(header) {
		return body, fmt.Errorf("invalid table header")
	}
	for _, s := range sep {
		if !tableSeparator.MatchString(s) {
			return body, fmt.Errorf("invalid table separator")
		}
	}
	rows := [][]string{}
	for _, s := range lines[start : end+1] {
		cells := tableCells(s)
		if len(cells) != len(header) {
			return body, fmt.Errorf("table rows must have matching column counts before editing")
		}
		rows = append(rows, cells)
	}
	prefix := []rune(lines[line])
	col = min(col, len(prefix))
	column := max(0, len(tableCells(string(prefix[:col])))-1)
	column = min(column, len(header)-1)
	row := line - start
	switch action {
	case "row-above", "row-below":
		idx := max(2, row)
		if action == "row-below" {
			idx = max(2, row+1)
		}
		blank := make([]string, len(header))
		rows = append(rows, nil)
		copy(rows[idx+1:], rows[idx:])
		rows[idx] = blank
	case "row-delete":
		if row < 2 {
			return body, fmt.Errorf("header rows cannot be removed")
		}
		rows = append(rows[:row], rows[row+1:]...)
	case "column-left", "column-right":
		idx := column
		if action == "column-right" {
			idx++
		}
		for i, cells := range rows {
			cells = append(cells, "")
			copy(cells[idx+1:], cells[idx:])
			cells[idx] = ""
			if i == 1 {
				cells[idx] = "---"
			}
			rows[i] = cells
		}
	case "column-delete":
		if len(header) <= 2 {
			return body, fmt.Errorf("keep at least two columns")
		}
		for i, cells := range rows {
			rows[i] = append(cells[:column], cells[column+1:]...)
		}
	case "align-left":
		rows[1][column] = ":---"
	case "align-center":
		rows[1][column] = ":---:"
	case "align-right":
		rows[1][column] = "---:"
	default:
		return body, fmt.Errorf("unknown table action: %s", action)
	}
	out := []string{}
	for _, cells := range rows {
		out = append(out, "| "+strings.Join(cells, " | ")+" |")
	}
	result := append([]string{}, lines[:start]...)
	result = append(result, out...)
	result = append(result, lines[end+1:]...)
	return strings.Join(result, "\n"), nil
}
func (a *App) tableMenu() {
	items := []paletteItem{}
	for _, s := range []string{"row-above", "row-below", "row-delete", "column-left", "column-right", "column-delete", "align-left", "align-center", "align-right"} {
		items = append(items, paletteItem{label: s, id: s})
	}
	a.overlay = &palette{title: "Markdown table", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		buf := a.activeBuffer()
		if buf == nil {
			return
		}
		cur := buf.ed.Cursor()
		text, err := editMarkdownTable(buf.ed.Text(), cur.Line, cur.Col, it.id)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		buf.ed.ReplaceText(text)
		buf.ed.SetCursor(cur)
	}}
}
func (a *App) formatMenu() {
	a.showMenu("Markdown formatting", []menuItem{
		{key: "b", label: "Bold text", run: func(a *App) { a.formatPrompt("**", "**") }}, {key: "i", label: "Italic text", run: func(a *App) { a.formatPrompt("*", "*") }}, {key: "c", label: "Inline code", run: func(a *App) { a.formatPrompt("`", "`") }}, {key: "h", label: "Highlight", run: func(a *App) { a.formatPrompt("==", "==") }}, {key: "t", label: "Table actions", run: func(a *App) { a.tableMenu() }}, {key: "a", label: "Complete tag / callout / language", run: func(a *App) { a.markdownCompletion() }},
	})
}
func (a *App) formatPrompt(open, close string) {
	buf := a.activeBuffer()
	if buf == nil {
		return
	}
	a.promptFor("Insert formatted text", "", "", func(a *App, s string) { buf.ed.InsertAtCursor(open + s + close) })
}
