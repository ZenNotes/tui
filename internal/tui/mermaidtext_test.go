package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
)

// greedDiagram is the block from the Discord report: six steps in a line, a
// fullwidth at sign, and a last edge that loops back to the first step.
const greedDiagram = `flowchart LR
  A["edit files in ＠"] --> B["jj diff / jj status"]
  B --> C["jj describe -m message"]
  C --> D["jj new - start next change"]
  D --> E["jj bookmark set name -r ＠"]
  E --> F["jj git push --bookmark name"]
  F --> A`

func mermaidTestApp() *App {
	return &App{theme: buildTheme(true), prefs: prefsView{Prefs: config.DefaultPrefs()}}
}

// squeezed drops every box-drawing rune and all whitespace, so a label reads
// the same however it was wrapped.
func squeezed(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ' ', r == '\n', r == '│', r == '─', r == '┌', r == '┐', r == '└', r == '┘', r == '┬', r == '├', r == '▶', r == '▼':
			return -1
		}
		return r
	}, s)
}

func assertFits(t *testing.T, lines []previewLine, width int) {
	t.Helper()
	for _, l := range lines {
		plain := stripAnsi(l.text)
		if got := cellWidth(plain); got > width {
			t.Fatalf("row is %d cells in a pane of %d: %q", got, width, plain)
		}
		if strings.Contains(plain, "…") {
			t.Fatalf("row was cut off: %q", plain)
		}
	}
}

// Every step of the reported diagram can be read at every width, with no
// source quotes and with the edge that loops back to the start.
func TestMermaidReportedDiagramIsCompleteAtEveryWidth(t *testing.T) {
	labels := []string{"edit files in ＠", "jj diff / jj status", "jj describe -m message", "jj new - start next change", "jj bookmark set name -r ＠", "jj git push --bookmark name"}
	for _, width := range []int{12, 20, 31, 40, 60, 80, 120, 200, 300} {
		lines := mermaidTestApp().mermaidLines(greedDiagram, 7, width)
		assertFits(t, lines, width)
		all := joinPreview(lines)
		if strings.Contains(all, `"`) {
			t.Fatalf("width %d keeps the source quotes:\n%s", width, all)
		}
		flat := squeezed(all)
		for _, label := range labels {
			if !strings.Contains(flat, squeezed(label)) {
				t.Fatalf("width %d loses %q:\n%s", width, label, all)
			}
		}
		if !strings.Contains(all, "-m") || !strings.Contains(all, "-r") {
			t.Fatalf("width %d splits a flag:\n%s", width, all)
		}
		// The loop back: the last box leads to the first step, so its label
		// shows a second time.
		if n := strings.Count(flat, squeezed(labels[0])); n != 2 {
			t.Fatalf("width %d shows the loop back to the first step %d times, want the box and the edge:\n%s", width, n, all)
		}
		for _, l := range lines {
			if l.srcLine != 7 {
				t.Fatalf("row maps to source line %d, want the block's 7", l.srcLine)
			}
		}
	}
}

func TestMermaidLeftToRightStaysThatWayWhenItFits(t *testing.T) {
	all := joinPreview(mermaidTestApp().mermaidLines("graph LR\n  A[One] --> B[Two] --> C[Three]", 0, 80))
	if rows := strings.Split(all, "\n"); len(rows) != 3 {
		t.Fatalf("a short left-to-right chain is one row of boxes:\n%s", all)
	}
	for _, want := range []string{"One", "Two", "Three", "▶"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
	// The same chain, too wide for the pane, goes top-down.
	narrow := joinPreview(mermaidTestApp().mermaidLines("graph LR\n  A[One] --> B[Two] --> C[Three]", 0, 16))
	if !strings.Contains(narrow, "▼") || strings.Contains(narrow, "▶") {
		t.Fatalf("a chain that does not fit runs top-down:\n%s", narrow)
	}
}

// Edge labels, branches and joins were all dropped or drawn over.
func TestMermaidBranchesAndEdgeLabelsSurvive(t *testing.T) {
	src := "graph TD\n  A[Start] -->|yes| B[One]\n  A -->|no| C[Two]\n  B --> D[End]\n  C --> D"
	all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 40))
	for _, want := range []string{"Start", "One", "Two", "End", "yes", "no"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
	// One does not lead to Two, so no arrow may join their boxes.
	rows := strings.Split(all, "\n")
	for i, row := range rows {
		if strings.Contains(row, "│ Two") && strings.Contains(rows[i-2], "▼") {
			t.Fatalf("draws an edge from One to Two that the diagram does not have:\n%s", all)
		}
	}
}

// Quoted labels are how Mermaid lets a label hold brackets, parentheses and
// semicolons; the parser ended the label at the first one and lost the edge.
func TestMermaidQuotedLabelsKeepPunctuationAndEdges(t *testing.T) {
	src := "graph TD\n  A[\"has [brackets] (and) {braces}; and more\"] --> B(\"f(x)\")\n  B --> C"
	all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 60))
	for _, want := range []string{"has [brackets] (and) {braces}; and more", "f(x)", "C"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, `"`) {
		t.Fatalf("keeps the quotes:\n%s", all)
	}
}

// A node first used bare and labelled later keeps the label.
func TestMermaidLabelGivenAfterFirstUse(t *testing.T) {
	all := joinPreview(mermaidTestApp().mermaidLines("graph TD\n  F --> A[First]\n  C[Third] --> F[\"Named later\"]", 0, 40))
	if !strings.Contains(all, "Named later") {
		t.Fatalf("lost the label given after the first use:\n%s", all)
	}
}

func TestMermaidCommonEdgeSpellings(t *testing.T) {
	for _, src := range []string{
		"graph TD\n  A -- yes --> B",
		"graph TD\n  A -. yes .-> B",
		"graph TD\n  A == yes ==> B",
		"graph TD\n  A -->|yes| B",
		"graph TD\n  A --yes--> B",
	} {
		all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 40))
		if !strings.Contains(all, "yes") || !strings.Contains(all, "│ B") || strings.Contains(all, "source") {
			t.Fatalf("%q loses a part:\n%s", src, all)
		}
	}
	long := joinPreview(mermaidTestApp().mermaidLines("graph TD\n  A ---> B", 0, 40))
	if !strings.Contains(long, "│ B") || strings.Contains(long, "source") {
		t.Fatalf("a longer arrow is still an edge:\n%s", long)
	}
}

// What the parser cannot read must not vanish: the block shows its source,
// and says why.
func TestMermaidUnreadableEdgeShowsSource(t *testing.T) {
	for _, src := range []string{
		"graph TD\n  A --x B",
		"graph TD\n  A ~~~ B",
		"graph TD\n  A[x[1]] --> B",
		"graph TD\n  A@{ shape: rect, label: \"Named\" } --> B",
		"stateDiagram-v2\n  A --> B\n  note right of A: an aside",
	} {
		all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 60))
		if !strings.Contains(all, "source") {
			t.Fatalf("%q has no note saying the source is shown:\n%s", src, all)
		}
		for _, line := range strings.Split(src, "\n") {
			if !strings.Contains(all, strings.TrimSpace(line)) {
				t.Fatalf("%q loses the line %q:\n%s", src, line, all)
			}
		}
	}
}

func TestMermaidStateDiagramShowsLabels(t *testing.T) {
	all := joinPreview(mermaidTestApp().mermaidLines("stateDiagram-v2\n  [*] --> Idle\n  Idle --> Busy : go\n  Busy --> [*]", 0, 40))
	for _, want := range []string{"start", "Idle", "go", "Busy", "end"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
}

func TestMermaidSubgraphNamesItsNodes(t *testing.T) {
	all := joinPreview(mermaidTestApp().mermaidLines("graph TD\n  subgraph one [\"My long group name\"]\n    A[First] --> B[Second]\n  end\n  B --> C[Third]", 0, 30))
	flat := squeezed(all)
	if !strings.Contains(flat, squeezed("My long group name")) || strings.Contains(all, `"`) {
		t.Fatalf("the group's name is lost or quoted:\n%s", all)
	}
	assertFits(t, mermaidTestApp().mermaidLines("graph TD\n  subgraph one [\"My long group name\"]\n    A[First] --> B[Second]\n  end\n  B --> C[Third]", 0, 30), 30)
}

// With mmdc and a terminal that shows pictures the block is a picture, and
// the whole diagram as text until the picture is ready.
func TestMermaidPictureStillReplacesTheText(t *testing.T) {
	a, done := newRenderTestApp(t, config.DefaultPrefs(), "mmdc")
	png := tinyPNG(t)
	fakeTools(t, func(ctx context.Context) ([]byte, error) { return png, nil })
	first := a.mermaidLines(greedDiagram, 0, 40)
	if isPicture(first) || !strings.Contains(joinPreview(first), "jj git push") {
		t.Fatalf("before the picture is ready, the text shows:\n%s", joinPreview(first))
	}
	waitRenders(t, done, 1, 5*time.Second)
	if lines := a.mermaidLines(greedDiagram, 0, 40); !isPicture(lines) {
		t.Fatalf("no picture once it is ready:\n%s", joinPreview(lines))
	}
}

// Parsing is cached by content, and no width shows another width's layout.
func TestMermaidLayoutFollowsWidthFromOneCachedParse(t *testing.T) {
	a := mermaidTestApp()
	wide := joinPreview(a.mermaidLines(greedDiagram, 0, 300))
	narrow := joinPreview(a.mermaidLines(greedDiagram, 0, 30))
	if wide == narrow {
		t.Fatal("wide and narrow panes share one layout")
	}
	if got := len(a.diagrams().text); got != 1 {
		t.Fatalf("cached %d parses of one diagram, want 1", got)
	}
	if joinPreview(a.mermaidLines(greedDiagram, 0, 300)) != wide {
		t.Fatal("the wide layout changed after a narrow one was drawn")
	}
}

// Sequence, class and ER diagrams still come from the library; when one is
// wider than the pane it shows its source instead of being cut off.
func TestMermaidOtherDiagramsTooWideShowSource(t *testing.T) {
	src := "sequenceDiagram\n  participant Alice\n  participant Bob\n  Alice->>Bob: hello"
	wide := mermaidTestApp().mermaidLines(src, 0, 80)
	if all := joinPreview(wide); strings.Contains(all, "source") || !strings.Contains(all, "Alice") || !strings.Contains(all, "┌") {
		t.Fatalf("a sequence diagram that fits is drawn:\n%s", all)
	}
	narrow := mermaidTestApp().mermaidLines(src, 0, 22)
	assertFits(t, narrow, 22)
	all := joinPreview(narrow)
	if !strings.Contains(all, "source") {
		t.Fatalf("no note that the source is shown:\n%s", all)
	}
	if !strings.Contains(squeezed(all), "Alice->>Bob:hello") {
		t.Fatalf("source line lost:\n%s", all)
	}
}

func TestMermaidUnsupportedTypeWrapsItsSource(t *testing.T) {
	src := "pie title Pets adopted by volunteers over the whole of last year\n  \"Dogs\" : 386"
	lines := mermaidTestApp().mermaidLines(src, 0, 30)
	all := joinPreview(lines)
	if !strings.Contains(squeezed(all), squeezed("Pets adopted by volunteers over the whole of last year")) {
		t.Fatalf("source cut off:\n%s", all)
	}
	assertFits(t, lines, 30)
}

// --- tables and aliases in the reading view ---

func glamourTestLines(t *testing.T, md string, width int) []previewLine {
	t.Helper()
	lines, err := mermaidTestApp().renderGlamourLinesFrom(md, width, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// A wikilink in a table cell must not push the column rule out of line.
func TestGlamourTableColumnsLineUpWithAliases(t *testing.T) {
	md := "| Link | Why |\n|------|-----|\n| [[Bookmarks|bookmarks]] | pipe |\n| [[Bookmarks]] | plain |\n| plain text here | third |"
	want := -1
	for _, l := range glamourTestLines(t, md, 60) {
		plain := stripAnsi(l.text)
		i := strings.IndexAny(plain, "│┼")
		if i < 0 {
			t.Fatalf("no column rule in %q", plain)
		}
		col := cellWidth(plain[:i])
		if want >= 0 && col != want {
			t.Fatalf("column rule at %d, want %d, in %q", col, want, plain)
		}
		want = col
	}
}

// tableColumn is the text of one column of drawn table rows, squeezed.
func tableColumn(rows []string, col int, rule string) string {
	var b strings.Builder
	for _, row := range rows {
		if parts := strings.Split(row, rule); col < len(parts) {
			b.WriteString(parts[col])
		}
	}
	return squeezed(b.String())
}

// An alias that wraps shows no token brackets, in a paragraph or a table.
func TestGlamourWrappedAliasShowsNoTokenBrackets(t *testing.T) {
	para := "padding padding padding padding padding padding [[Bookmarks|the long alias of this note]] padding"
	table := "| Link | Why | More | Most |\n|---|---|---|---|\n| [[Bookmarks|the long alias of this note]] | because | filler text here | more filler text |"
	for _, width := range []int{20, 30, 40, 50, 60, 70} {
		all := joinPreview(glamourTestLines(t, para, width))
		if strings.ContainsAny(all, "⟦⟧\u00a0") || !strings.Contains(squeezed(all), "thelongaliasofthisnote") {
			t.Fatalf("paragraph at %d:\n%s", width, all)
		}
		all = joinPreview(glamourTestLines(t, table, width))
		if strings.ContainsAny(all, "⟦⟧\u00a0") || !strings.Contains(tableColumn(strings.Split(all, "\n"), 0, "│"), "thelongaliasofthisnote") {
			t.Fatalf("table at %d:\n%s", width, all)
		}
	}
}

// A table with no outer pipes lines up too.
func TestGlamourBareTableColumnsLineUp(t *testing.T) {
	lines := glamourTestLines(t, "Link | Why\n---|---\n[[Bookmarks|bookmarks]] | pipe\nplain text here | third", 60)
	want := -1
	for _, l := range lines {
		plain := stripAnsi(l.text)
		i := strings.IndexAny(plain, "│┼")
		if i < 0 {
			t.Fatalf("no column rule in %q", plain)
		}
		col := cellWidth(plain[:i])
		if want >= 0 && col != want {
			t.Fatalf("column rule at %d, want %d, in %q", col, want, plain)
		}
		want = col
	}
}

func zenTableText(md string, width int) string {
	a := mermaidTestApp()
	a.prefs.PreviewStyle = "zen"
	return joinPreview(a.renderPreviewLinesFrom(md, width, "x.md"))
}

// In the zen style a wikilink's pipe split the cell in two.
func TestZenTableKeepsAliasInOneCell(t *testing.T) {
	all := zenTableText("| Link | Why |\n|---|---|\n| [[Bookmarks|bookmarks]] | unescaped |\n| [[Bookmarks\\|escaped]] | escaped |\n| a \\| b | literal |", 60)
	for _, bad := range []string{"[[", "]]", "\\"} {
		if strings.Contains(all, bad) {
			t.Fatalf("shows %q:\n%s", bad, all)
		}
	}
	for _, want := range []string{"bookmarks", "escaped", "a | b"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
	if rows := strings.Split(all, "\n"); strings.Count(rows[0], "│") != 3 {
		t.Fatalf("want two columns:\n%s", all)
	}
}

// A wide table in the zen style wraps its cells instead of cutting them off.
func TestZenWideTableWrapsCells(t *testing.T) {
	md := "| Command | Notes |\n|---|---|\n| jj describe -m message | Rewrites the change id only when the description is new and pushes nothing at all |"
	all := zenTableText(md, 40)
	if strings.Contains(all, "…") {
		t.Fatalf("cut off:\n%s", all)
	}
	rows := strings.Split(all, "\n")
	if !strings.Contains(tableColumn(rows, 2, "│"), squeezed("Rewrites the change id only when the description is new and pushes nothing at all")) {
		t.Fatalf("a cell lost text:\n%s", all)
	}
	for _, row := range strings.Split(all, "\n") {
		if cellWidth(row) > 40 {
			t.Fatalf("row is wider than the pane: %q", row)
		}
	}
}

func TestZenTableWithoutOuterPipes(t *testing.T) {
	all := zenTableText("Name | Value\n-----|------\none  | 1", 40)
	if strings.Contains(all, "---") || !strings.Contains(all, "│ one") {
		t.Fatalf("a table with no outer pipes is not drawn:\n%s", all)
	}
}

// Obsidian writes [[Note\|alias]] inside tables; the link still goes to Note.
func TestEscapedAliasPipeFollowsTheNote(t *testing.T) {
	links := linksOnLine(`| [[Bookmarks\|escaped]] | why |`)
	if len(links) != 1 || links[0].target != "Bookmarks" {
		t.Fatalf("links = %+v", links)
	}
}

// --- review findings on the first version ---

func mermaidText40(src string) string {
	return joinPreview(mermaidTestApp().mermaidLines(src, 0, 40))
}

// assertDrawn checks a diagram is drawn with these texts, not its source.
func assertDrawn(t *testing.T, src string, wants ...string) {
	t.Helper()
	all := mermaidText40(src)
	if strings.Contains(all, "its source") || strings.Contains(all, "mermaid:") {
		t.Fatalf("%q shows its source, want a drawing:\n%s", src, all)
	}
	flat := squeezed(all)
	for _, want := range wants {
		if !strings.Contains(flat, squeezed(want)) {
			t.Fatalf("%q loses %q:\n%s", src, want, all)
		}
	}
}

// assertSource checks a diagram shows its source with the note.
func assertSource(t *testing.T, src string) {
	t.Helper()
	all := mermaidText40(src)
	if !strings.Contains(all, "its source") {
		t.Fatalf("%q has no note that its source is shown:\n%s", src, all)
	}
	if !strings.Contains(squeezed(all), squeezed(src)) {
		t.Fatalf("%q does not show all of its source:\n%s", src, all)
	}
}

// F1: a label may hold the runes zn could use for its own masks.
func TestMermaidPrivateUseLabelsAreOnlyText(t *testing.T) {
	assertDrawn(t, "graph TD\n  A[\uE00099\uE001] --> B", "\uE00099\uE001", "B")
	assertDrawn(t, "graph TD\n  A[\"secret\"] --> B[\uE0000\uE001]", "secret", "\uE0000\uE001")
	assertDrawn(t, "graph TD\n  A[\"x\"] --> B[\uE00099999999999999999999999\uE001]", "x", "\uE00099999999999999999999999\uE001")
	assertDrawn(t, "graph TD\n  A[\"x\"] --> B[\U000F00000\U000F0001]", "x", "\U000F00000\U000F0001")
}

// F2: an ID may begin with a word that also starts a statement.
func TestMermaidIDsStartingWithKeywords(t *testing.T) {
	assertDrawn(t, "graph TD\n  title-bar[Title bar] --> content[Content]", "Title bar", "Content")
	assertDrawn(t, "graph TD\n  click --> save[Save]", "click", "Save")
	assertDrawn(t, "graph TD\n  click-handler[On click] --> save[Save]", "On click", "Save")
	assertDrawn(t, "graph TD\n  title[Report title] --> body[Body]\n  body --> foot[Foot]", "Report title", "Body", "Foot")
	// The statements themselves still draw nothing.
	all := mermaidText40("graph TD\n  title My chart\n  A --> B\n  click A \"https://example.com\"\n  linkStyle 0 stroke:#f00\n  style A fill:#fff\n  classDef a,b fill:#fff\n  class A,B one two\n  direction LR")
	for _, bad := range []string{"title", "click", "linkStyle", "style", "classDef", "class", "direction", "source"} {
		if strings.Contains(all, bad) {
			t.Fatalf("%q is drawn:\n%s", bad, all)
		}
	}
}

// F3: an edge ending in a circle or a cross is not an edge label.
func TestMermaidCircleAndCrossEdgesAreNotLabels(t *testing.T) {
	assertSource(t, "graph TD\n  A --o B --> C")
	assertSource(t, "graph TD\n  A --x B --> C")
	assertSource(t, "graph TD\n  A ==x B ==> C")
	// The parser reads these as an edge to a node named x or o, and B is lost.
	assertSource(t, "graph TD\n  A ---x B")
	assertSource(t, "graph TD\n  A -.-x B")
	assertSource(t, "graph TD\n  A ---o B")
	// A one-letter label is still a label.
	assertDrawn(t, "graph TD\n  A -- x --> B", "x", "B")
}

// F4: words that start a statement do not excuse a statement from the check.
func TestMermaidStatementWordsDoNotSkipTheCheck(t *testing.T) {
	for _, src := range []string{
		"graph TD\n  endpoint[API] --x client[Client]",
		"graph TD\n  classify --x done",
		"graph TD\n  styles ~~~ done",
		"graph TD\n  direction_a --o B",
		"graph TD\n  subgraphs --x B",
		"graph TD\n  ender --> B --x C",
		"graph TD\n  style --- B",
		"graph TD\n  subgraph --> B",
	} {
		assertSource(t, src)
	}
}

// Words the parser skips over are not read either.
func TestMermaidTrailingWordsShowSource(t *testing.T) {
	assertSource(t, "graph TD\n  A[One] some junk here")
	assertSource(t, "graph TD\n  B --> C extra words")
}

// Every node shape and class shorthand counts as read.
func TestMermaidEveryShapeIsRead(t *testing.T) {
	assertDrawn(t, `graph TD
  A(round) --> B((circle)) --> C{diamond} --> D{{hex}} --> E[(db)] --> F([stad]) --> G[[sub]] --> H>flag] --> I[/tr\] --> J[\rt/] --> K(((dbl)))
  K:::warm --> L[Done]:::cool`, "round", "circle", "diamond", "hex", "db", "stad", "sub", "flag", "tr", "rt", "dbl", "Done")
	assertDrawn(t, "graph TD\n  A & B --> C & D\n  a-b --> c-d", "A", "B", "C", "D", "a-b", "c-d")
}

// F5: a quote mark in a label is not always the start of a quoted label.
func TestMermaidQuotesPairInsideAStatement(t *testing.T) {
	assertDrawn(t, "graph TD\n  A[5\" disk] --> B[3.5\" disk]\n  B --> C[End]", `5" disk`, `3.5" disk`, "End")
	assertSource(t, "graph TD\n  A[\"x; B --> C; D[\"y\"]")
	assertSource(t, "graph TD\n  A[\"x; B --> C; D[\"y\"] --> E")
	assertSource(t, "graph TD\n  A[\"line1\n  line2\"] --> B")
	assertDrawn(t, "graph TD\n  A[\"a;b\"] --> B[\"c;d\"]; B --> C", "a;b", "c;d", "C")
}

// F7: arrows inside a label are text.
func TestMermaidArrowsInLabelsStayText(t *testing.T) {
	assertDrawn(t, "graph TD\n  A[foo ---> bar] --> B", "foo ---> bar", "B")
	assertDrawn(t, "graph TD\n  A[a -- b --> c] --> D", "a -- b --> c", "D")
	all := mermaidText40("graph TD\n  A -->|x -- y --> z| B")
	if !strings.Contains(all, "x -- y --> z") || strings.Contains(all, "source") {
		t.Fatalf("the edge label changed:\n%s", all)
	}
	if strings.Count(all, "┌") != 2 {
		t.Fatalf("draws a node the diagram does not have:\n%s", all)
	}
}

// F8: quote marks inside a label show.
func TestMermaidQuotesInsideLabelsShow(t *testing.T) {
	assertDrawn(t, "graph TD\n  A[say \"hi\" now] --> B", `say "hi" now`, "B")
	assertDrawn(t, "graph TD\n  A[\"say \\\"hi\\\" now\"] --> B", `say "hi" now`, "B")
	assertDrawn(t, "graph TD\n  A[\"say #quot;hi#quot;\"] --> B", `say "hi"`, "B")
	assertDrawn(t, "graph TD\n  A -->|\"a\" or \"b\"| B", `"a" or "b"`, "B")
	assertDrawn(t, "stateDiagram-v2\n  A --> B : say \"hi\"\n  B : the \"big\" one", `say "hi"`, `the "big" one`)
	if all := mermaidText40(`graph TD
  A["say \"hi\" now"] --> B`); strings.Contains(all, `\`) {
		t.Fatalf("keeps a backslash:\n%s", all)
	}
}

// A state's label may hold what looks like an arrow or an ampersand.
func TestMermaidStateLabelsAreFree(t *testing.T) {
	assertDrawn(t, "stateDiagram-v2\n  A --> B : R&D\n  B --> C", "R&D", "C")
	assertDrawn(t, "stateDiagram-v2\n  A --> B : wait -- then go", "wait -- then go")
}

// F10: comments and wrappers that are not part of the diagram.
func TestMermaidCommentsAndWrappers(t *testing.T) {
	assertDrawn(t, "graph TD\n  A --> B; %% done", "A", "B")
	assertDrawn(t, "graph TD\n  A --> B %% to b", "A", "B")
	assertDrawn(t, "graph TD\n  A[100%% done] --> B", "100%% done", "B")
	assertDrawn(t, "---\ntitle: Hello\n---\nflowchart LR\n  A --> B", "A", "B")
	assertDrawn(t, "\ufeffgraph TD\n  A --> B", "A", "B")
	assertDrawn(t, "flowchart-elk TD\n  A --> B", "A", "B")
	assertDrawn(t, "%%{init: {\"theme\":\"dark\"}}%%\nflowchart LR\n  A --> B", "A", "B")
}

// F11: a width that two measures disagree on does not cut the label.
func TestMermaidLoneRegionalIndicatorKeepsLabel(t *testing.T) {
	assertDrawn(t, "graph TD\n  A[\"🇺 love\"] --> B", "love", "B")
	assertDrawn(t, "graph LR\n  A[\"🇺 love\"] --> B", "love", "B")
	if all := zenTableText("| 🇺 flag text | b |\n|---|---|\n| a | c |", 60); !strings.Contains(all, "flag text") {
		t.Fatalf("zen cell cut off:\n%s", all)
	}
}

// F12: layout finds a node's edges without walking all of them.
func TestMermaidEdgesAreIndexedBySource(t *testing.T) {
	flow, partial, err := parseFlowchart("graph TD\n  A --> B\n  A --> C\n  B --> C")
	if err != nil || partial {
		t.Fatalf("parse: partial=%v err=%v", partial, err)
	}
	if len(flow.bySource["A"]) != 2 || len(flow.bySource["B"]) != 1 || len(flow.bySource["C"]) != 0 {
		t.Fatalf("index = %+v", flow.bySource)
	}
}

// F13: a listed edge always says which node it goes to.
func TestMermaidListedEdgesNameTheirTarget(t *testing.T) {
	blank := mermaidText40("graph TD\n  A[\"   \"] --> B[\"<br/>\"]\n  B --> A")
	if !strings.Contains(blank, "▶ A") {
		t.Fatalf("an edge to an empty label names nothing:\n%s", blank)
	}
	same := mermaidText40("graph TD\n  A[Start] --> B[Retry]\n  B --> C[Done]\n  A --> D[Other]\n  D --> E[Retry]\n  C --> E")
	if !strings.Contains(same, "Retry [B]") || !strings.Contains(same, "Retry [E]") {
		t.Fatalf("two nodes with one label cannot be told apart:\n%s", same)
	}
	// Every start and end of a state diagram means the same thing.
	states := mermaidText40("stateDiagram-v2\n  [*] --> A\n  A --> [*]\n  [*] --> B\n  B --> [*]")
	if strings.Contains(states, "_") {
		t.Fatalf("shows an internal name:\n%s", states)
	}
}

// F9: an opening [[ with no link after it is not a link.
func TestZenTableUnterminatedLinkKeepsColumns(t *testing.T) {
	for _, row := range []string{"| x[[1] | y | z |", "| x[[1] | y | z ]] |", "| ]] | [[x|y]] | z |"} {
		cells := previewTableCells(row)
		if len(cells) != 3 {
			t.Fatalf("%q splits into %q", row, cells)
		}
	}
}

// F6: brackets and hard spaces a note holds itself are text.
func TestGlamourKeepsLiteralTokenRunes(t *testing.T) {
	for _, md := range []string{
		"The set ⟦ is open and ⟧ closes.",
		"Semantic ⟦x⟧ brackets next to [[Note|a link]].",
		"- item ⟦ open\n- item ⟧ close",
		"```\nlet a = ⟦1, 2⟧;\n```",
	} {
		all := joinPreview(glamourTestLines(t, md, 60))
		if !strings.Contains(all, "⟦") || !strings.Contains(all, "⟧") {
			t.Fatalf("%q loses a bracket:\n%s", md, all)
		}
	}
	code := joinPreview(glamourTestLines(t, "```\nfoo\u00a0bar\n```", 60))
	if !strings.Contains(code, "foo\u00a0bar") {
		t.Fatalf("code loses a hard space: %q", code)
	}
	table := glamourTestLines(t, "| a | b |\n|---|---|\n| ⟦x | y⟧ |\n| plain | text |", 40)
	all := joinPreview(table)
	if !strings.Contains(all, "⟦x") || !strings.Contains(all, "y⟧") {
		t.Fatalf("a table loses a bracket:\n%s", all)
	}
	want := -1
	for _, l := range table {
		plain := stripAnsi(l.text)
		i := strings.IndexAny(plain, "│┼")
		if col := cellWidth(plain[:max(i, 0)]); want >= 0 && col != want {
			t.Fatalf("column rule at %d, want %d, in %q", col, want, plain)
		} else {
			want = col
		}
	}
}

// F14: a cell keeps its rule in line without gaps after each link.
func TestGlamourTableLinkHasNoExtraSpaces(t *testing.T) {
	all := joinPreview(glamourTestLines(t, "| A | B |\n|---|---|\n| [[x|one]] and [[y|two]] | [[z]] |\n| plain text that is long | b |", 60))
	if !strings.Contains(all, "one and two") {
		t.Fatalf("gaps after the links:\n%s", all)
	}
}

// F15: a zen cell that fits keeps its spacing.
func TestZenCellKeepsRunsOfSpaces(t *testing.T) {
	if all := zenTableText("| a    b | c |\n|---|---|\n| 1 | 2 |", 60); !strings.Contains(all, "a    b") {
		t.Fatalf("spaces collapsed:\n%s", all)
	}
}

// No spaces around an arrow is the most common way to write one.
func TestMermaidArrowsWithoutSpaces(t *testing.T) {
	all := mermaidText40("graph TD\n  A-->B[Second]\n  B-->C[Third]")
	if strings.Contains(all, "source") || strings.Count(all, "┌") != 3 {
		t.Fatalf("not three boxes:\n%s", all)
	}
	for _, want := range []string{"│ A ", "Second", "Third"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
	assertDrawn(t, "graph TD\n  A-->|yes|B[Second]", "yes", "Second")
	assertDrawn(t, "graph TD\n  A[First]-->B[Second]", "First", "Second")
	assertDrawn(t, "graph TD\n  A--yes-->B[Second]", "yes", "Second")
	assertDrawn(t, "graph TD\n  A==>B[Second]\n  B-.->C[Third]\n  C---D", "Second", "Third", "D")
	assertDrawn(t, "graph TD\n  a-b-->c-d", "a-b", "c-d")
}

func TestMermaidMoreEdgeSpellings(t *testing.T) {
	assertDrawn(t, "graph TD\n  A --> |yes| B", "yes", "B")
	assertDrawn(t, "graph TD\n  A -- well-known --> B", "well-known", "B")
	assertSource(t, "graph TD\n  A --- B ---- C ~~~ D")
}

// What the graph cannot hold shows the source rather than going missing.
func TestMermaidLostStructureShowsSource(t *testing.T) {
	assertSource(t, "stateDiagram-v2\n  A : first\n  A : second\n  A --> B")
	assertSource(t, "graph TD\n  A[One] --> B\n  A[Two] --> C")
	assertSource(t, "graph TD\n  subgraph one [Group]\n  A --> B")
	assertSource(t, "stateDiagram-v2\n  state B {\n  X --> Y")
	assertDrawn(t, "graph TD\n  A[One] --> B\n  A[One] --> C\n  end", "One", "B", "C")
}

func TestMermaidDeepGroupsFitThePane(t *testing.T) {
	src := "graph TD\n" + strings.Repeat("  subgraph s [Deep name here]\n", 60) + "  A\n" + strings.Repeat("  end\n", 60)
	assertFits(t, mermaidTestApp().mermaidLines(src, 0, 40), 40)
}

// A pipe of an alias's own is not a column rule.
func TestGlamourTableAliasKeepsItsOwnPipe(t *testing.T) {
	all := joinPreview(glamourTestLines(t, "| A | B |\n|---|---|\n| [[Note|a\\|b]] | x |\n| plain text here | y |", 60))
	if !strings.Contains(all, "a|b") {
		t.Fatalf("the alias changed:\n%s", all)
	}
}
