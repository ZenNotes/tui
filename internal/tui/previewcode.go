package tui

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gmtext "github.com/yuin/goldmark/text"
)

// The Glamour preview rewrites Markdown before rendering and restyles the rows
// after it, and neither step may touch code. What counts as code comes from the
// parser Glamour itself uses, so it holds in quotes, list items, indented blocks
// and code spans that run over a line, exactly where Glamour will draw code.

// codeParser matches the extensions Glamour configures in glamour.go.
var codeParser = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList)).Parser()

type codeKind uint8

const (
	notCode codeKind = iota
	spanCode
	blockCode
)

// markdownCode marks each byte of text with the kind of code it renders as.
// only says the whole text is a single code block.
func markdownCode(text string) (mask []codeKind, only bool) {
	src := []byte(text)
	doc := codeParser.Parse(gmtext.NewReader(src))
	mask = make([]codeKind, len(src))
	mark := func(s gmtext.Segment, kind codeKind) {
		for i := s.Start; i < s.Stop; i++ {
			mask[i] = kind
		}
	}
	markLines := func(lines *gmtext.Segments) {
		for i := 0; i < lines.Len(); i++ {
			mark(lines.At(i), blockCode)
		}
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.FencedCodeBlock:
			if v.Info != nil {
				mark(v.Info.Segment, blockCode)
			}
			markLines(v.Lines())
		case *ast.CodeBlock:
			markLines(v.Lines())
		case *ast.CodeSpan:
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					mark(t.Segment, spanCode)
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if first := doc.FirstChild(); first != nil && first == doc.LastChild() {
		switch first.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			only = true
		}
	}
	return mask, only
}

// isCodeOnly says text renders as a single code block and nothing else.
func isCodeOnly(text string) bool {
	_, only := markdownCode(text)
	return only
}

// Guards stand in for the characters the preview's rewrites and row styling look
// for, so those simply find nothing in code. Noncharacters do not occur in real
// text, and each takes one cell like the character it replaces, so wrapping is
// unchanged. A note that does hold one of them gets the character it stands for.
const (
	guardBracket      = "\uFDD0" // [ and ] make every wikilink, embed, task state
	guardBracketClose = "\uFDD7" // and callout the rewrites look for
	guardToken        = "\uFDD1" // ⟦ and ⟧ are how a link token looks to the row
	guardTokenClose   = "\uFDD8" // styling
	guardBullet       = "\uFDD2" // • precedes the task glyphs postStyleRow pulls out
	guardHash         = "\uFDD3"
	guardAt           = "\uFDD4"
	guardBang         = "\uFDD5"
	guardColon        = "\uFDD6"
	guardItem         = "\uFDD9" // follows a marker in code, see codeItemRe
	guardGap          = "\uFDDA" // fills a blank line, see markItemContent
	guardLine         = "\uFDDB" // starts an item's paragraph line, see markItemContent
)

var (
	// A code block's syntax highlighting has to see its real characters, so it
	// only carries the guards no language gives a meaning to. mapRows spares
	// a block that is all code the row styling, and the rest of its characters
	// with it.
	guardBlockCode = strings.NewReplacer("[", guardBracket, "]", guardBracketClose, "⟦", guardToken, "⟧", guardTokenClose, "•", guardBullet)
	guardSpanCode  = strings.NewReplacer("[", guardBracket, "]", guardBracketClose, "⟦", guardToken, "⟧", guardTokenClose, "•", guardBullet, "#", guardHash, "@", guardAt, "!", guardBang, ":", guardColon)
	// unguardBracket runs before rendering, since the rewrites are done by then.
	unguardBracket = strings.NewReplacer(guardBracket, "[", guardBracketClose, "]")
	// unguardRow runs last, once every step has passed over a row. guardItem is
	// one cell more than the source, which only costs a row a cell of padding.
	unguardRow = strings.NewReplacer(guardToken, "⟦", guardTokenClose, "⟧", guardBullet, "•", guardHash, "#", guardAt, "@", guardBang, "!", guardColon, ":", guardItem, "", guardGap, "")
	// codeItemRe finds a code line that would pass for a list item once it is a
	// row (itemMarkerRe). In a block that is not all code, mapRows counts rows by
	// their markers, so such a line carries guardItem after its marker.
	codeItemRe = regexp.MustCompile(`(?m)^([ \t]*` + itemMarkerPattern + `)(\s|$)`)
)

// guardCode swaps the characters the preview looks for in text's code.
func guardCode(text string) string {
	mask, only := markdownCode(text)
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		kind := mask[i]
		j := i
		for j < len(text) && mask[j] == kind {
			j++
		}
		switch kind {
		case spanCode:
			out.WriteString(guardSpanCode.Replace(text[i:j]))
		case blockCode:
			code := text[i:j]
			if !only {
				code = codeItemRe.ReplaceAllString(code, "${1}"+guardItem+"${2}")
			}
			out.WriteString(guardBlockCode.Replace(code))
		default:
			out.WriteString(text[i:j])
		}
		i = j
	}
	return out.String()
}

// blockSource is what a block hands the renderer. Content cut off from its list
// item loses the indent that would read as code on its own. GFM ends a table cell
// at any bare pipe, even inside code, which would cut `[[Note|alias]]` in two.
func blockSource(b mdBlock, lines []string) string {
	rows := lines[b.start:b.end]
	if b.listChild {
		rows = dedent(rows)
	}
	text := strings.Join(rows, "\n")
	if b.kind == "list" {
		text = markItemContent(text)
	}
	return escapeTableCodePipes(text)
}

// markItemContent prepares what a list item holds beyond its marker line, keeping
// every line in place.
//
// Glamour runs an item's paragraphs together with no space between their words,
// and drops the blank line before a quote. Those blank lines are filled, so the
// first reads as one paragraph whose lines Glamour keeps, and the filler leaves a
// blank row.
//
// Rows are matched to list items by their markers, which says nothing about a
// paragraph, so each of its lines starts with guardLine for mapRows to find. A line
// is left out where the guard would change how it reads, and its links then show
// on the row before it: emphasis may stop opening, and a bare URL stop being one.
func markItemContent(text string) string {
	lines := strings.Split(text, "\n")
	plain := true
	for _, l := range lines {
		if !blankRe.MatchString(l) && !listLineRe.MatchString(l) {
			plain = false
			break
		}
	}
	if plain {
		return text
	}
	src := []byte(text)
	lineOf := lineLocator(src)
	_ = ast.Walk(codeParser.Parse(gmtext.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindListItem {
			return ast.WalkContinue, nil
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if c.Kind() != ast.KindParagraph && c.Kind() != ast.KindTextBlock {
				continue
			}
			marked := c.Lines()
			for i := 0; i < marked.Len(); i++ {
				l := lineOf(marked.At(i).Start)
				body := strings.TrimLeft(lines[l], " \t")
				if !listLineRe.MatchString(lines[l]) && guardLineFits(body) {
					lines[l] = lines[l][:len(lines[l])-len(body)] + guardLine + body
				}
			}
			next := c.NextSibling()
			if c.Kind() != ast.KindParagraph || marked.Len() == 0 || next == nil {
				continue
			}
			switch next.Kind() {
			case ast.KindParagraph, ast.KindBlockquote:
				to := firstLineOf(next, lineOf)
				if to < 0 {
					continue
				}
				indent := lines[to][:len(lines[to])-len(strings.TrimLeft(lines[to], " \t"))]
				for i := lineOf(marked.At(marked.Len()-1).Start) + 1; i < to; i++ {
					lines[i] = indent + guardGap
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(lines, "\n")
}

func guardLineFits(body string) bool {
	if body == "" || strings.ContainsRune("*_~", rune(body[0])) {
		return false
	}
	lower := strings.ToLower(body)
	return !strings.HasPrefix(lower, "http") && !strings.HasPrefix(lower, "www.") && !strings.HasPrefix(lower, "mailto:")
}

// firstLineOf finds the line a block starts on, or -1.
func firstLineOf(n ast.Node, lineOf func(int) int) int {
	line := -1
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && c.Type() == ast.TypeBlock && c.Lines().Len() > 0 {
			line = lineOf(c.Lines().At(0).Start)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return line
}

// lineLocator returns a function from a byte offset of src to its line number.
func lineLocator(src []byte) func(int) int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return func(offset int) int { return sort.SearchInts(starts, offset+1) - 1 }
}

// dedent removes the indent the lines share.
func dedent(lines []string) []string {
	indent := -1
	for _, l := range lines {
		if n := len(l) - len(strings.TrimLeft(l, " \t")); !blankRe.MatchString(l) && (indent < 0 || n < indent) {
			indent = n
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l[min(max(indent, 0), len(l)):]
	}
	return out
}

// escapeTableCodePipes escapes the pipes in the code spans of text's table rows;
// GFM drops the backslash again. Goldmark says which rows those are, wherever
// the table sits, so a fence or a lone row that only looks like one is left alone.
func escapeTableCodePipes(text string) string {
	if !strings.Contains(text, "|") || !strings.Contains(text, "`") {
		return text
	}
	rows := tableRows(text)
	if len(rows) == 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for i := range rows {
		lines[i] = escapeCodePipes(lines[i])
	}
	return strings.Join(lines, "\n")
}

// tableRows lists the lines of text that hold a table cell's content.
func tableRows(text string) map[int]bool {
	src := []byte(text)
	lineOf := lineLocator(src)
	rows := map[int]bool{}
	_ = ast.Walk(codeParser.Parse(gmtext.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != east.KindTable {
			return ast.WalkContinue, nil
		}
		_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
			if t, ok := c.(*ast.Text); ok && entering {
				rows[lineOf(t.Segment.Start)] = true
			}
			return ast.WalkContinue, nil
		})
		return ast.WalkSkipChildren, nil
	})
	return rows
}

// escapeCodePipes escapes the bare pipes inside a table row's code spans, read
// the way CommonMark does: a backslash escapes a backtick outside a span only,
// and a span closes on a backtick run of its own length.
func escapeCodePipes(row string) string {
	var out strings.Builder
	last := 0
	for i := 0; i < len(row); {
		switch row[i] {
		case '\\':
			i += 2
		case '`':
			n := leadingBackticks(row[i:])
			closer := -1
			for j := i + n; j < len(row); {
				if row[j] != '`' {
					j++
					continue
				}
				m := leadingBackticks(row[j:])
				if m == n {
					closer = j
					break
				}
				j += m
			}
			if closer < 0 {
				i += n
				continue
			}
			for k := i + n; k < closer; k++ {
				if row[k] == '|' && row[k-1] != '\\' {
					out.WriteString(row[last:k])
					out.WriteByte('\\')
					last = k
				}
			}
			i = closer + n
		default:
			i++
		}
	}
	out.WriteString(row[last:])
	return out.String()
}

func leadingBackticks(s string) int {
	return len(s) - len(strings.TrimLeft(s, "`"))
}

// listBlockEnd returns one past the last line of the list starting at line i.
// Each block is parsed alone, so what is indented under an item has to stay in
// the block, past blank lines, or it would read as an indented code block.
func listBlockEnd(lines []string, i int, latex bool) int {
	var fenceChar byte
	fenceLen := 0
	j := i
	for j < len(lines) {
		line := lines[j]
		blank := blankRe.MatchString(line)
		switch {
		case fenceChar != 0:
			// A fence holds blank lines, and ends with its list item if unclosed.
			if !blank && !listIndented(line) {
				return j
			}
			if fenceCloses(strings.TrimLeft(line, " \t"), fenceChar, fenceLen) {
				fenceChar = 0
			}
			j++
		case blank:
			k := j
			for k < len(lines) && blankRe.MatchString(lines[k]) {
				k++
			}
			if k == len(lines) || !listIndented(lines[k]) || standsAlone(lines, k, latex) {
				return j
			}
			j = k
		case j > i && !listLineRe.MatchString(line) && !listIndented(line):
			return j
		default:
			fenceChar, fenceLen, _ = fenceOpens(strings.TrimLeft(listLineRe.ReplaceAllString(line, ""), " \t"))
			j++
		}
	}
	return j
}

func listIndented(line string) bool {
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

// standsAlone says line k is drawn by embedBlockLines when it is a block of its
// own: an embed, a video link or math. Those end a list block, and markListChildren
// keeps what follows from reading as code.
func standsAlone(lines []string, k int, latex bool) bool {
	line := lines[k]
	if mathInlineOpen.MatchString(line) || mathFenceRe.MatchString(line) || latex && texBlockEnd(lines, k) > 0 {
		return true
	}
	if k+1 < len(lines) && !blankRe.MatchString(lines[k+1]) {
		return false
	}
	if m := bareURLLineRe.FindStringSubmatch(line); m != nil {
		_, ok := videoProvider(m[1])
		return ok
	}
	return embedLineRe.MatchString(line) || embedMDLineRe.MatchString(line)
}

// markListChildren flags the indented blocks that follow a list's standalone
// content, which belong to the list even though they are blocks of their own.
func markListChildren(blocks []mdBlock, lines []string) {
	inList := false
	for i := range blocks {
		b := &blocks[i]
		switch {
		case b.kind == "blank":
		case inList && b.kind != "fence" && listIndented(lines[b.start]):
			b.listChild = true
		default:
			inList = b.kind == "list"
		}
	}
}

// previewFenceRe is a fence marker as CommonMark has it: spaces only, up to three.
// The editor's fenceRe also takes a tab, which reads as indented code here.
var previewFenceRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// fenceOpens reads a line that opens a code fence: a backtick fence's info
// string holds no backtick, or the line is an inline code span instead.
func fenceOpens(line string) (char byte, n int, ok bool) {
	m := previewFenceRe.FindStringSubmatch(line)
	if m == nil || (m[1][0] == '`' && strings.Contains(line[len(m[0]):], "`")) {
		return 0, 0, false
	}
	return m[1][0], len(m[1]), true
}

func isFenceOpen(line string) bool {
	_, _, ok := fenceOpens(line)
	return ok
}

// fenceCloses says a line closes a fence of char and length n: the same
// character, at least as long, with nothing after it.
func fenceCloses(line string, char byte, n int) bool {
	m := previewFenceRe.FindStringSubmatch(line)
	return m != nil && m[1][0] == char && len(m[1]) >= n && strings.TrimSpace(line[len(m[0]):]) == ""
}
