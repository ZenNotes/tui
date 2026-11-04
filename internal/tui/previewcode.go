package tui

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gmtext "github.com/yuin/goldmark/text"

	"github.com/ZenNotes/tui/internal/vault"
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
// for, so those simply find nothing in code. Noncharacters never occur in real
// text, and each takes one cell like the character it replaces, so wrapping is
// unchanged.
const (
	guardBracket = "﷐" // [ starts every wikilink, embed, task state and callout
	guardToken   = "﷑" // ⟦ is how a link token looks to postStyleRow
	guardBullet  = "﷒" // • precedes the task glyphs postStyleRow pulls out
	guardHash    = "﷓"
	guardAt      = "﷔"
	guardBang    = "﷕"
	guardColon   = "﷖"
)

var (
	// A code block's syntax highlighting has to see its real characters, so it
	// only carries the guards no language gives a meaning to. mapRows spares
	// a block that is all code the row styling, and the rest of its characters
	// with it.
	guardBlockCode = strings.NewReplacer("[", guardBracket, "⟦", guardToken, "•", guardBullet)
	guardSpanCode  = strings.NewReplacer("[", guardBracket, "⟦", guardToken, "•", guardBullet, "#", guardHash, "@", guardAt, "!", guardBang, ":", guardColon)
	// unguardBracket runs before rendering, since the rewrites are done by then.
	unguardBracket = strings.NewReplacer(guardBracket, "[")
	// unguardRow runs last, once the row styling has passed over a row.
	unguardRow = strings.NewReplacer(guardToken, "⟦", guardBullet, "•", guardHash, "#", guardAt, "@", guardBang, "!", guardColon, ":")
)

// guardCode swaps the characters the preview looks for in text's code.
func guardCode(text string) string {
	mask, _ := markdownCode(text)
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
			out.WriteString(guardBlockCode.Replace(text[i:j]))
		default:
			out.WriteString(text[i:j])
		}
		i = j
	}
	return out.String()
}

// inertCode blanks the link and task syntax in text's code, so a scan for those
// only finds what renders as one. The words stay: a link written around a code
// span, as in [`name`](url), keeps its label and target.
func inertCode(text string, mask []codeKind) string {
	out := []byte(text)
	for i, kind := range mask {
		if kind == notCode {
			continue
		}
		switch out[i] {
		case '[', ']', '(', ')', ':':
			out[i] = ' '
		}
	}
	return string(out)
}

// tableDelimRe is GFM's delimiter row, which makes rows a table at all. A single
// dash is enough for GFM, unlike the stricter tableSepRe.
var tableDelimRe = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)

// blockSource is what a block hands the renderer. GFM ends a table cell at any
// bare pipe, even inside code, which would cut `[[Note|alias]]` in two, so a
// table's code spans get their pipes escaped; GFM drops the backslash again.
func blockSource(b mdBlock, lines []string) string {
	rows := lines[b.start:b.end]
	if b.kind != "table" || len(rows) < 2 || !tableDelimRe.MatchString(rows[1]) {
		return strings.Join(rows, "\n")
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = escapeCodePipes(row)
	}
	return strings.Join(out, "\n")
}

// escapeCodePipes escapes the bare pipes inside a table row's code spans.
func escapeCodePipes(row string) string {
	if !strings.Contains(row, "`") {
		return row
	}
	mask := vault.CodeMask(row)
	var out strings.Builder
	for i := 0; i < len(row); i++ {
		if row[i] == '|' && mask[i] && (i == 0 || row[i-1] != '\\') {
			out.WriteByte('\\')
		}
		out.WriteByte(row[i])
	}
	return out.String()
}

// fenceOpens reads a line that opens a code fence: a backtick fence's info
// string holds no backtick, or the line is an inline code span instead.
func fenceOpens(line string) (char byte, n int, ok bool) {
	m := fenceRe.FindStringSubmatch(line)
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
	m := fenceRe.FindStringSubmatch(line)
	return m != nil && m[1][0] == char && len(m[1]) >= n && strings.TrimSpace(line[len(m[0]):]) == ""
}
