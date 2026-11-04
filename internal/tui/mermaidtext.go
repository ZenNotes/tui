package tui

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/yashikota/mermaigo/pkg/mermaid"
	"github.com/yashikota/mermaigo/pkg/mermaid/ascii"
	"github.com/yashikota/mermaigo/pkg/mermaid/parser"
	"github.com/yashikota/mermaigo/pkg/mermaid/types"
)

// The text form of a Mermaid diagram.
//
// The library's text renderer puts flowchart nodes in one straight line and
// routes nothing: left-to-right boxes overlap, an edge back to an earlier node
// and every edge label are dropped, and a branch is drawn through the boxes in
// between. No width fixes that, so zn keeps the library's parser and draws
// flowcharts and state diagrams itself, at the width of the pane: a row of
// boxes when a left-to-right diagram fits, otherwise a column. An edge to the
// box drawn next is an arrow between them, and any other edge is listed under
// its source by the name of its target, so every node and edge can be read at
// any width.
//
// The library's sequence drawing is kept only when it shows everything in the
// source, and when it fits. Its class and ER drawings never do: relationships,
// their types and labels, method arguments and comments are lost, and the order
// changes from one parse to the next, so those diagrams show their source.
//
// When the parser cannot have read a whole statement, the block shows its
// source and says so, since a tidy drawing with an edge missing misleads.

// mermaidText is one parsed diagram, cached by content. Only the layout
// depends on the width.
type mermaidText struct {
	flow    *flowchart // drawn here, at any width
	rows    []string   // the library's drawing, at its own width
	err     string     // why it could not be parsed
	partial bool       // a drawing would lose part of the source
}

func parseMermaidText(src string) *mermaidText {
	switch mermaid.DetectDiagramType(src) {
	case mermaid.DiagramTypeSequence:
		rows := sequenceRows(src)
		return &mermaidText{rows: rows, partial: rows == nil}
	case mermaid.DiagramTypeClass, mermaid.DiagramTypeER:
		return &mermaidText{partial: true}
	}
	flow, partial, err := parseFlowchart(src)
	if err != nil {
		return &mermaidText{err: err.Error()}
	}
	// A diagram with no node yet, as while one is being typed, has nothing to draw.
	return &mermaidText{flow: flow, partial: partial || len(flow.order) == 0}
}

// mermaidTextLines lays a parsed diagram out for a pane width.
func (a *App) mermaidTextLines(t *mermaidText, src string, srcLine, width int) []previewLine {
	th := a.theme
	inner := width - 1 // the row's leading space
	source := func(note string, noteStyle func(...string) string) []previewLine {
		out := []previewLine{}
		for _, row := range wrapCommentText(note, inner) {
			out = append(out, previewLine{text: " " + noteStyle(row), srcLine: srcLine})
		}
		for _, row := range wrapCommentText(strings.ReplaceAll(src, "\t", "  "), inner) {
			out = append(out, previewLine{text: " " + th.Code.Render(row), srcLine: srcLine})
		}
		return out
	}
	var rows []string
	switch {
	case t.err != "":
		return source("mermaid: "+firstLine(t.err), th.StatusError.Render)
	case t.flow != nil && len(t.flow.order) == 0 && !t.flow.unread:
		return source("This diagram has nothing to draw yet.", th.Muted.Render)
	case t.partial:
		return source("zn cannot draw all of this diagram as text, so this is its source.", th.Muted.Render)
	case t.flow != nil:
		rows = t.flow.rows(inner)
	default:
		rows = t.rows
		for _, row := range rows {
			if cellWidth(row) > inner {
				return source("This diagram is wider than the pane, so this is its source.", th.Muted.Render)
			}
		}
	}
	out := make([]previewLine, 0, len(rows))
	for _, row := range rows {
		out = append(out, previewLine{text: " " + th.Base.Render(row), srcLine: srcLine})
	}
	return out
}

// --- sequence diagrams ---

// mermaidSequenceMaxCells caps the library's drawing, which allocates a grid of
// its width times its height before anything else happens.
const mermaidSequenceMaxCells = 1 << 20

var (
	mermaidParticipantRe = regexp.MustCompile(`^participant\s+(\S+)(?:\s+as\s+.+)?$`)
	// Only the arrows the drawing tells apart: it gives every other kind the
	// same head, and it has no place for activation marks.
	mermaidMessageRe = regexp.MustCompile(`^([^\s<]+?)\s*--?>>\s*([^\s+-]\S*?)\s*:\s*(.+)$`)
	// A name holding an arrow is more likely a misread statement than a name.
	mermaidArrowInNameRe = regexp.MustCompile(`->|-$`)
)

// sequenceRows is the library's drawing of a sequence diagram, or nil when that
// would lose anything. Only participants and plain messages are drawn, so a
// note, a block or any statement not known here brings up the source.
func sequenceRows(src string) []string {
	statements := []string{}
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "%%") {
			continue
		}
		for _, s := range strings.Split(line, ";") {
			if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "%%") {
				statements = append(statements, s)
			}
		}
	}
	messages := [][]string{}
	seen := map[string]bool{}
	for _, s := range statements[1:] {
		if m := mermaidMessageRe.FindStringSubmatch(s); m != nil {
			if mermaidArrowInNameRe.MatchString(m[1]) || mermaidArrowInNameRe.MatchString(m[2]) {
				return nil
			}
			seen[m[1]], seen[m[2]] = true, true
			messages = append(messages, m[1:])
		} else if m := mermaidParticipantRe.FindStringSubmatch(s); m != nil && !seen[m[1]] {
			// The parser keeps a name's first label and ignores any later one.
			seen[m[1]] = true
		} else {
			return nil
		}
	}

	// The parser skips a name like parser->>db, reading it as a par block.
	diagram := parser.ParseSequenceDiagram(statements)
	if len(diagram.Messages) != len(messages) {
		return nil
	}
	for i, m := range diagram.Messages {
		if m.From != messages[i][0] || m.To != messages[i][1] || m.Label != messages[i][2] {
			return nil
		}
		if !sequenceLabelDrawable(m.Label) || strings.Contains(strings.ToLower(m.Label), "<br") {
			return nil
		}
	}
	width, height := 1, 7
	for _, actor := range diagram.Actors {
		if !sequenceLabelDrawable(actor.Label) {
			return nil
		}
		width += max(len(actor.Label)+4, 9) + 10
	}
	longest := 0
	for _, m := range diagram.Messages {
		longest = max(longest, len(m.Label))
		height += 4
	}
	if (width+longest+11)*height > mermaidSequenceMaxCells {
		return nil
	}

	// The drawing stops at its own right edge and spaces out a label with
	// non-ASCII letters, so each label must be there whole, on its own row.
	rendered := ascii.RenderSequenceAscii(diagram, nil)
	rows := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(rows) < 3 {
		return nil
	}
	boxes := rows[1]
	for _, actor := range diagram.Actors {
		i := strings.Index(boxes, actor.Label)
		if i < 0 {
			return nil
		}
		boxes = boxes[i+len(actor.Label):]
	}
	at := 3 // below the participants' boxes
	for _, m := range diagram.Messages {
		for at < len(rows) && !strings.Contains(rows[at], m.Label) {
			at++
		}
		if at == len(rows) {
			return nil
		}
		at++
	}
	return rows
}

// sequenceLabelDrawable says each rune of a label takes one cell. The library's
// grid holds one rune per cell, so a wide letter, a combining mark, a tab or a
// control character moves everything after it on the row.
func sequenceLabelDrawable(label string) bool {
	for _, r := range label {
		if runewidth.RuneWidth(r) != 1 {
			return false
		}
	}
	return true
}

// --- reading the source ---

// flowchart is a parsed flowchart or state diagram with clean labels.
type flowchart struct {
	graph       *types.MermaidGraph
	order       []string // node IDs in the order they are drawn
	leftToRight bool
	bySource    map[string][]*types.MermaidEdge
	unread      bool // the parser skipped part of the source
}

var (
	// A quote mark opens quoted text only where a label can start, so the 5"
	// in A[5" disk] is text. A backslash escapes a quote mark inside.
	mermaidQuotedRe    = regexp.MustCompile(`(^|[\[({|>/\\\s])"((?:[^"\\\n]|\\.)*)"`)
	mermaidFrontRe     = regexp.MustCompile(`(?s)^\s*---\r?\n.*?\n---[ \t]*\r?\n`)
	mermaidHeadRe      = regexp.MustCompile(`(?i)^(graph|flowchart)(?:-elk)?(\s+\w+)?$`)
	mermaidStateHeadRe = regexp.MustCompile(`(?i)^stateDiagram`)
	mermaidBreakRe     = regexp.MustCompile(`(?i)<br\s*/?>`)
	// Statements that draw nothing, which the parser would otherwise read as a
	// node with that name. An ID may start with the same word, so the word has
	// to end here and the statement has to hold no arrow.
	mermaidIgnoredRe = regexp.MustCompile(`^(?:(?:linkStyle|click|accTitle|accDescr|title|classDef|class|style)(?:\s|:)|direction\s+(?i:TD|TB|LR|BT|RL)$)`)
	// Statements that shape the diagram without naming a node or an edge.
	mermaidStructureRe = regexp.MustCompile(`^(?:subgraph\s|end$|state\s.*\{$|\}$)`)
	mermaidGroupOpenRe = regexp.MustCompile(`^(?:subgraph\s|state\s.*\{$)`)
	// What is left of a statement without its labels shows what it asks for,
	// spelled in any way Mermaid allows.
	mermaidLabelsRe = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)|\{[^}]*\}|\|[^|]*\|`)
	mermaidFlagRe   = regexp.MustCompile(`(\w)>[^\]]*\]`)
	mermaidArrowRe  = regexp.MustCompile(`<?(?:-{2,}|={2,}|-\.+-|~{3,})[>ox]?`)
	// Shapes that open doubled, taken out whole before the plain labels, so a
	// closer left over after that means the parser ended a label early.
	mermaidDoubledRe = regexp.MustCompile(`\(\(\(.*?\)\)\)|\(\(.*?\)\)|\[\[.*?\]\]|\{\{.*?\}\}|\(\[.*?\]\)|\[\(.*?\)\]`)
	// One node in what is left: an ID and a class.
	mermaidNodeRe = regexp.MustCompile(`^\s*(\w+(?:-\w+)*)\s*(?::::[\w-]+)?\s*$`)
	// A circle or cross on an edge, not the start of edge text: A --x B.
	mermaidEndMarkRe = regexp.MustCompile(`(?:--|==)[ox]\s`)
	// A word that could be an ID, in any script.
	mermaidWordRe = regexp.MustCompile(`[\p{L}\p{N}_][\p{L}\p{N}\p{M}_.]*`)
	// Mermaid's #name; and #35; entities.
	mermaidEntityRe = regexp.MustCompile(`#(\w+);`)
	// Spellings the parser does not know, rewritten to ones it does.
	mermaidSpellings = []struct {
		re *regexp.Regexp
		to string
	}{
		// With no space before an arrow, the parser reads A-->B[x] as one node
		// in the flag shape, A>text].
		{regexp.MustCompile(`(\w)(<?(?:-{2,}|={2,}|-\.+-))`), "$1 $2"},
		{regexp.MustCompile(`-{3,}>`), "-->"},
		{regexp.MustCompile(`={3,}>`), "==>"},
		{regexp.MustCompile(`-\.{2,}->`), "-.->"},
		{regexp.MustCompile(`(^|[^-])--\s*([^-\[\](){}|<>](?:-?[^-\[\](){}|<>])*?)\s*-->`), "$1-->|$2|"},
		{regexp.MustCompile(`(^|[^-])-\.\s*([^-.\[\](){}|<>]+?)\s*\.->`), "$1-.->|$2|"},
		{regexp.MustCompile(`(^|[^=])==\s*([^=\[\](){}|<>]+?)\s*==>`), "$1==>|$2|"},
	}
)

// mermaidMaskRunes picks two private-use runes the source does not hold. The
// parser ends a label at its first closing bracket and splits statements at
// every semicolon, quoted or not, so quoted text is hidden from it behind
// these. A note may hold any fixed choice, as Nerd Font icons do.
func mermaidMaskRunes(src string) (open, shut string) {
	held := map[rune]bool{}
	for _, r := range src {
		if r >= 0xF0000 {
			held[r] = true
		}
	}
	found := []string{}
	for r := rune(0xF0000); len(found) < 2; r++ {
		if !held[r] {
			found = append(found, string(r))
		}
	}
	return found[0], found[1]
}

// mermaidPlainID says the parser can read a word as a node ID as it is. Its IDs
// are ASCII, so others are renamed while it reads them.
func mermaidPlainID(word string) bool {
	for _, r := range word {
		if r > 127 || r == '.' {
			return false
		}
	}
	return true
}

// mermaidStateClasses takes the :::class off a state statement's nodes, since
// the state parser reads any colon as the start of a label.
func mermaidStateClasses(line string) string {
	for from := 0; ; {
		i := strings.Index(line[from:], ":")
		if i < 0 {
			return line
		}
		i += from
		if !strings.HasPrefix(line[i:], ":::") {
			return line
		}
		end := i + 3
		for end < len(line) && (line[end] == '-' || line[end] == '_' || line[end] >= '0' && line[end] <= '9' || line[end] >= 'a' && line[end] <= 'z' || line[end] >= 'A' && line[end] <= 'Z') {
			end++
		}
		line = line[:i] + line[end:]
		from = i
	}
}

// mermaidComment cuts a trailing %% comment from a raw line, before its
// statements are split. After a label's colon, %% is the label's text.
func mermaidComment(raw string) string {
	blanked := mermaidLabelsRe.ReplaceAllStringFunc(raw, func(m string) string { return strings.Repeat(" ", len(m)) })
	i := strings.Index(blanked, "%%")
	if i < 0 || strings.Contains(strings.ReplaceAll(blanked[:i], ":::", ""), ":") {
		return raw
	}
	return raw[:i]
}

// mermaidStatement readies one statement for the parser: nothing for one that
// draws nothing, and known spellings and IDs it can read outside its labels.
func mermaidStatement(line string, rename func(string) string) string {
	blanked := mermaidLabelsRe.ReplaceAllStringFunc(line, func(m string) string { return strings.Repeat(" ", len(m)) })
	if line == "" || mermaidIgnoredRe.MatchString(line) && !mermaidArrowRe.MatchString(blanked) {
		return ""
	}
	respell := func(segment string) string {
		return mermaidRespell(mermaidWordRe.ReplaceAllStringFunc(segment, rename))
	}
	var b strings.Builder
	last := 0
	for _, label := range mermaidLabelsRe.FindAllStringIndex(line, -1) {
		before := respell(line[last:label[0]])
		if line[label[0]] == '|' {
			before = strings.TrimRight(before, " \t") // the parser wants -->|text|
		}
		b.WriteString(before)
		b.WriteString(line[label[0]:label[1]])
		last = label[1]
	}
	b.WriteString(respell(line[last:]))
	return b.String()
}

func mermaidRespell(s string) string {
	if mermaidEndMarkRe.MatchString(s) {
		return s
	}
	for _, spelling := range mermaidSpellings {
		s = spelling.re.ReplaceAllString(s, spelling.to)
	}
	return s
}

// parseFlowchart reads a flowchart or state diagram. partial says some of the
// source did not make it into the graph.
func parseFlowchart(src string) (f *flowchart, partial bool, err error) {
	src = mermaidFrontRe.ReplaceAllString(strings.TrimPrefix(src, "\ufeff"), "")
	open, shut := mermaidMaskRunes(src)
	maskedRe := regexp.MustCompile(open + `(\d+)` + shut)
	wholeRe := regexp.MustCompile(`^"` + open + `\d+` + shut + `"$`)
	// Quoted text ending mid-word means a stray quote mark paired with another.
	badCloseRe := regexp.MustCompile(shut + `"[^\s\])}|/\;:]`)
	quoted := []string{}
	masked := mermaidQuotedRe.ReplaceAllStringFunc(src, func(q string) string {
		m := mermaidQuotedRe.FindStringSubmatch(q)
		quoted = append(quoted, strings.ReplaceAll(m[2], `\"`, `"`))
		return m[1] + `"` + open + strconv.Itoa(len(quoted)-1) + shut + `"`
	})
	partial = badCloseRe.MatchString(masked)
	// Renamed IDs use a prefix the source does not hold.
	idPrefix := "znid"
	for strings.Contains(src, idPrefix) {
		idPrefix += "x"
	}
	idPrefix += "n"
	renamedRe := regexp.MustCompile(idPrefix + `(\d+)z`)
	names := []string{}
	renames := map[string]string{}
	rename := func(word string) string {
		if mermaidPlainID(word) {
			return word
		}
		if renames[word] == "" {
			renames[word] = idPrefix + strconv.Itoa(len(names)) + "z"
			names = append(names, word)
		}
		return renames[word]
	}
	lines := []string{}
	for _, raw := range strings.Split(masked, "\n") {
		for _, line := range strings.Split(mermaidComment(raw), ";") {
			if line = mermaidStatement(strings.TrimSpace(line), rename); line != "" {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) > 0 {
		if m := mermaidHeadRe.FindStringSubmatch(lines[0]); m != nil {
			direction := strings.TrimSpace(m[2])
			if direction == "" {
				direction = "TD"
			}
			lines[0] = m[1] + " " + direction
		}
	}
	isState := len(lines) > 0 && mermaidStateHeadRe.MatchString(lines[0])
	if isState {
		for i := 1; i < len(lines); i++ {
			lines[i] = mermaidStateClasses(lines[i])
		}
	}
	graph, err := parser.ParseMermaid(strings.Join(lines, "\n"))
	if err != nil {
		return nil, false, err
	}
	f = &flowchart{graph: graph, leftToRight: graph.Direction == types.DirectionLR || graph.Direction == types.DirectionRL}

	// Read each statement alone as well: that shows whether all of it was
	// understood, and finds the label of a node first used without one, which
	// the whole-diagram parse drops.
	depth := 0
	statements := []*types.MermaidGraph{}
	for _, line := range lines[1:] {
		one, err := parser.ParseMermaid(lines[0] + "\n" + line)
		if err != nil {
			return nil, false, err
		}
		statements = append(statements, one)
		if !mermaidStatementRead(line, one, isState) {
			partial = true
		}
		for id, n := range one.Nodes {
			known := graph.Nodes[id]
			switch {
			case known == nil || n.Label == id || n.Label == known.Label:
			case known.Label == id:
				known.Label, known.Shape = n.Label, n.Shape
			default:
				partial = true // two labels for one node, and one would be lost
			}
		}
		// A group never closed is not in the graph at all.
		switch {
		case mermaidGroupOpenRe.MatchString(line):
			depth++
		case (line == "end" || line == "}") && depth > 0:
			depth--
		}
	}

	if depth > 0 {
		partial = true
	}

	// Nodes run in source order: along the edges, then any with no edge.
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] && graph.Nodes[id] != nil {
			seen[id] = true
			f.order = append(f.order, id)
		}
	}
	for _, e := range graph.Edges {
		add(e.Source)
		add(e.Target)
	}
	for _, one := range statements {
		ids := make([]string, 0, len(one.Nodes))
		for id := range one.Nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			add(id)
		}
	}

	clean := func(s string) string {
		s = strings.TrimSpace(s)
		if wholeRe.MatchString(s) {
			s = strings.Trim(s, `"`)
		}
		s = maskedRe.ReplaceAllStringFunc(s, func(m string) string {
			n, _ := strconv.Atoi(maskedRe.FindStringSubmatch(m)[1])
			return quoted[n]
		})
		s = renamedRe.ReplaceAllStringFunc(s, func(m string) string {
			n, _ := strconv.Atoi(renamedRe.FindStringSubmatch(m)[1])
			return names[n]
		})
		s = mermaidEntityRe.ReplaceAllStringFunc(s, func(m string) string {
			name := m[1 : len(m)-1]
			if name[0] >= '0' && name[0] <= '9' {
				name = "#" + name
			}
			if text := html.UnescapeString("&" + name + ";"); text != "&"+name+";" {
				return text
			}
			return m
		})
		return strings.TrimSpace(mermaidBreakRe.ReplaceAllString(s, "\n"))
	}
	sameLabel := map[string]int{}
	for id, n := range graph.Nodes {
		n.Label = clean(n.Label)
		if n.Label == "" {
			n.Label = clean(id)
		}
		switch n.Shape {
		case types.ShapeStateStart:
			n.Label = "start"
		case types.ShapeStateEnd:
			n.Label = "end"
		default:
			sameLabel[n.Label]++
		}
	}
	// A listed edge names its target by label, so two nodes with one label
	// show their IDs too. Every start or end of a state diagram means the same.
	for id, n := range graph.Nodes {
		if sameLabel[n.Label] > 1 {
			n.Label += " [" + clean(id) + "]"
		}
	}
	f.bySource = map[string][]*types.MermaidEdge{}
	for _, e := range graph.Edges {
		f.bySource[e.Source] = append(f.bySource[e.Source], e)
	}
	for _, e := range graph.Edges {
		e.Label = clean(e.Label)
	}
	var cleanGroups func(groups []*types.MermaidSubgraph)
	cleanGroups = func(groups []*types.MermaidSubgraph) {
		for _, g := range groups {
			g.Label = clean(g.Label)
			cleanGroups(g.Children)
		}
	}
	cleanGroups(graph.Subgraphs)
	f.unread = partial
	return f, partial, nil
}

// mermaidStatementRead says the parser took in all of one statement: it found
// every edge the statement asks for and exactly the nodes it names.
func mermaidStatementRead(line string, one *types.MermaidGraph, isState bool) bool {
	if strings.Contains(line, "@{") {
		return false // a node's shape and label in the newer syntax
	}
	if isState {
		// A transition's label is free text, and [*] is not a node ID.
		if i := strings.Index(line, ":"); i >= 0 {
			line = line[:i]
		}
	}
	bare := mermaidFlagRe.ReplaceAllString(mermaidLabelsRe.ReplaceAllString(mermaidDoubledRe.ReplaceAllString(line, ""), ""), "$1")
	if mermaidStructureRe.MatchString(line) && !mermaidArrowRe.MatchString(bare) {
		return true
	}
	groups := mermaidArrowRe.Split(bare, -1)
	edges := 0
	for i := 1; i < len(groups); i++ {
		edges += (1 + strings.Count(groups[i-1], "&")) * (1 + strings.Count(groups[i], "&"))
	}
	if edges != len(one.Edges) || len(one.Nodes) == 0 {
		return false
	}
	if isState {
		return true
	}
	named := map[string]bool{}
	for _, group := range groups {
		for _, node := range strings.Split(group, "&") {
			m := mermaidNodeRe.FindStringSubmatch(node)
			if m == nil || one.Nodes[m[1]] == nil {
				return false
			}
			named[m[1]] = true
		}
	}
	return len(named) == len(one.Nodes)
}

// --- drawing ---

// rows draws the diagram no wider than width.
func (f *flowchart) rows(width int) []string {
	var out []string
	if f.leftToRight {
		out = f.rowsAcross(width)
	}
	if out == nil {
		out = f.rowsDown(width)
	}
	return append(out, f.groupRows(width)...)
}

// outgoing splits a node's edges: the first to the node drawn next goes
// between their boxes, the others are listed by name.
func (f *flowchart) outgoing(i int) (next *types.MermaidEdge, others []*types.MermaidEdge) {
	for _, e := range f.bySource[f.order[i]] {
		if next == nil && i+1 < len(f.order) && e.Target == f.order[i+1] {
			next = e
			continue
		}
		others = append(others, e)
	}
	return next, others
}

func (f *flowchart) label(id string) string {
	return f.graph.Nodes[id].Label
}

// flowName is a node's label on one line, for naming it in a list.
func (f *flowchart) flowName(id string) string {
	return strings.ReplaceAll(f.label(id), "\n", " ")
}

// edgeTarget names where a listed edge goes, with its label.
func (f *flowchart) edgeTarget(e *types.MermaidEdge) string {
	text := f.flowName(e.Target)
	if e.Label != "" {
		text += " (" + strings.ReplaceAll(e.Label, "\n", " ") + ")"
	}
	return text
}

// flowWrap wraps text at spaces, keeping a flag like -m whole, and breaks a
// word only when it alone is wider than the width.
func flowWrap(text string, width int) []string {
	width = max(2, width) // one wide character always fits
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		if cellWidth(line) <= width {
			out = append(out, strings.TrimRight(line, " "))
			continue
		}
		row := ""
		for _, word := range strings.Fields(line) {
			if row != "" && cellWidth(row)+1+cellWidth(word) <= width {
				row += " " + word
				continue
			}
			if row != "" {
				out = append(out, row)
			}
			pieces := strings.Split(xansi.Hardwrap(word, width, false), "\n")
			out = append(out, pieces[:len(pieces)-1]...)
			row = pieces[len(pieces)-1]
		}
		out = append(out, row)
	}
	return out
}

// padCells pads plain text by the width it was measured with. padRight
// measures with a second function and cuts what that one finds too wide.
func padCells(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-cellWidth(s)))
}

// flowBox draws a labelled box; shaft marks where an edge leaves its bottom.
func flowBox(label string, inner int, shaft bool) []string {
	lines := flowWrap(label, inner)
	w := 1
	for _, l := range lines {
		w = max(w, cellWidth(l))
	}
	out := []string{"┌" + strings.Repeat("─", w+2) + "┐"}
	for _, l := range lines {
		out = append(out, "│ "+padCells(l, w)+" │")
	}
	if shaft {
		return append(out, "└─┬"+strings.Repeat("─", w)+"┘")
	}
	return append(out, "└"+strings.Repeat("─", w+2)+"┘")
}

func flowShaft(e *types.MermaidEdge) string {
	switch e.Style {
	case types.EdgeStyleDotted:
		return "┆"
	case types.EdgeStyleThick:
		return "┃"
	}
	return "│"
}

// flowArrow is an edge's horizontal stroke with its arrowheads, and an edge
// label, when it has one, in the middle of the stroke.
func flowArrow(e *types.MermaidEdge, label string) string {
	line := "─"
	switch e.Style {
	case types.EdgeStyleDotted:
		line = "╌"
	case types.EdgeStyleThick:
		line = "━"
	}
	start, end := line, line
	if e.HasArrowStart {
		start = "◀" + line
	}
	if e.HasArrowEnd {
		end = "▶"
	}
	if label != "" {
		return start + " " + label + " " + line + end
	}
	return start + end
}

// rowsDown is the column of boxes, which fits any width.
func (f *flowchart) rowsDown(width int) []string {
	out := []string{}
	for i, id := range f.order {
		next, others := f.outgoing(i)
		out = append(out, flowBox(f.label(id), width-4, next != nil || len(others) > 0)...)
		for k, e := range others {
			branch, cont := "  ├", "  │"
			if k == len(others)-1 && next == nil {
				branch, cont = "  └", "   "
			}
			head := branch + flowArrow(e, "") + " "
			pad := strings.Repeat(" ", cellWidth(head)-cellWidth(cont))
			for j, row := range flowWrap(f.edgeTarget(e), width-cellWidth(head)) {
				if j == 0 {
					out = append(out, head+row)
				} else {
					out = append(out, cont+pad+row)
				}
			}
		}
		switch {
		case next != nil:
			shaft := "  " + flowShaft(next)
			if next.HasArrowStart {
				out = append(out, "  ▲")
			}
			if next.Label != "" {
				for _, row := range flowWrap(next.Label, width-5) {
					out = append(out, shaft+" "+row)
				}
			} else if next.Style != types.EdgeStyleSolid {
				out = append(out, shaft)
			}
			if next.HasArrowEnd {
				out = append(out, "  ▼")
			} else {
				out = append(out, shaft)
			}
		case i+1 < len(f.order):
			out = append(out, "")
		}
	}
	return out
}

// rowsAcross is one row of boxes, or nil when that is wider than width.
func (f *flowchart) rowsAcross(width int) []string {
	type piece struct {
		box       []string
		w         int
		connector string
	}
	pieces := make([]piece, len(f.order))
	listed := []string{}
	height, total := 0, 0
	for i, id := range f.order {
		box := flowBox(f.label(id), 1<<16, false)
		next, others := f.outgoing(i)
		connector := ""
		switch {
		case next != nil:
			connector = flowArrow(next, strings.ReplaceAll(next.Label, "\n", " "))
		case i+1 < len(f.order):
			connector = "  "
		}
		pieces[i] = piece{box: box, w: cellWidth(box[0]), connector: connector}
		total += pieces[i].w + cellWidth(connector)
		height = max(height, len(box))
		for _, e := range others {
			listed = append(listed, f.flowName(id)+" "+flowArrow(e, "")+" "+f.edgeTarget(e))
		}
	}
	if total > width {
		return nil
	}
	out := []string{}
	for y := 0; y < height; y++ {
		var b strings.Builder
		for _, p := range pieces {
			cell := ""
			if y < len(p.box) {
				cell = p.box[y]
			}
			b.WriteString(padCells(cell, p.w))
			if y == 1 {
				b.WriteString(p.connector)
			} else {
				b.WriteString(strings.Repeat(" ", cellWidth(p.connector)))
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	for _, text := range listed {
		out = append(out, flowWrap(text, width)...)
	}
	return out
}

// groupRows names each subgraph or composite state and the nodes in it.
func (f *flowchart) groupRows(width int) []string {
	out := []string{}
	var walk func(groups []*types.MermaidSubgraph, indent string)
	walk = func(groups []*types.MermaidSubgraph, indent string) {
		for _, g := range groups {
			names := make([]string, 0, len(g.NodeIDs))
			for _, id := range g.NodeIDs {
				if f.graph.Nodes[id] != nil {
					names = append(names, f.flowName(id))
				}
			}
			text := "▸ " + strings.ReplaceAll(g.Label, "\n", " ")
			if len(names) > 0 {
				text += ": " + strings.Join(names, ", ")
			}
			for _, row := range flowWrap(text, width-cellWidth(indent)) {
				out = append(out, indent+row)
			}
			// Very deep nesting stops indenting rather than outgrow the pane.
			if cellWidth(indent)+2 <= width/2 {
				walk(g.Children, indent+"  ")
			} else {
				walk(g.Children, indent)
			}
		}
	}
	walk(f.graph.Subgraphs, "")
	if len(out) > 0 {
		return append([]string{""}, out...)
	}
	return out
}
