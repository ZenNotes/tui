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

// A sequence diagram the library draws in full is still wider than a narrow
// pane; it shows its source instead of being cut off.
func TestMermaidSequenceTooWideShowsSource(t *testing.T) {
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

func zenTable(md string, width int) string {
	a := mermaidTestApp()
	a.prefs.PreviewStyle = "zen"
	return joinPreview(a.renderPreviewLinesFrom(md, width, "x.md"))
}

// In the zen style a wikilink's pipe split the cell in two.
func TestZenTableKeepsAliasInOneCell(t *testing.T) {
	all := zenTable("| Link | Why |\n|---|---|\n| [[Bookmarks|bookmarks]] | unescaped |\n| [[Bookmarks\\|escaped]] | escaped |\n| a \\| b | literal |", 60)
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
	all := zenTable(md, 40)
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
	all := zenTable("Name | Value\n-----|------\none  | 1", 40)
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

// mermaidTestWidths runs from a narrow split to a very wide pane.
var mermaidTestWidths = []int{4, 7, 12, 22, 31, 40, 60, 80, 120, 200, 300}

// assertMermaidKeeps checks that every piece of a diagram can be read at every
// width, however it was wrapped.
func assertMermaidKeeps(t *testing.T, src string, want []string) {
	t.Helper()
	for _, width := range mermaidTestWidths {
		lines := mermaidTestApp().mermaidLines(src, 3, width)
		assertFits(t, lines, width)
		all := joinPreview(lines)
		flat := squeezed(all)
		for _, piece := range want {
			if !strings.Contains(flat, squeezed(piece)) {
				t.Fatalf("width %d loses %q:\n%s", width, piece, all)
			}
		}
		for _, l := range lines {
			if l.srcLine != 3 {
				t.Fatalf("row maps to source line %d, want the block's 3", l.srcLine)
			}
		}
	}
}

// A message longer than the space between two participants is whole, even in a
// pane far wider than the diagram, where the library clipped it at its own edge.
func TestMermaidSequenceLongMessageIsWholeAtEveryWidth(t *testing.T) {
	long := "a really long message about the thing we need to discuss"
	src := "sequenceDiagram\n  Alice->>Bob: " + long + "\n  Bob-->>Alice: ok"
	assertMermaidKeeps(t, src, []string{"Alice", "Bob", long, "ok"})
	if all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 300)); !strings.Contains(all, "source") {
		t.Fatalf("no note that the source is shown:\n%s", all)
	}
}

// The library's sequence drawing has no place for these, or its parser skips
// them, so each one has to bring the source up.
func TestMermaidSequenceShowsSourceForWhatItCannotDraw(t *testing.T) {
	for _, tc := range []struct{ name, statement, want string }{
		{"note", "Note over Alice,Bob: the secret note", "the secret note"},
		{"loop", "loop every minute\n  Alice->>Bob: ping\nend", "every minute"},
		{"alternative", "alt is ready\n  Alice->>Bob: go\nelse is not ready\n  Alice->>Bob: wait\nend", "is not ready"},
		{"numbering", "autonumber", "autonumber"},
		{"title", "title: The handshake", "The handshake"},
		{"lost message", "Alice-xBob: dropped", "Alice-xBob"},
		{"async message", "Alice-)Bob: later", "Alice-)Bob"},
		{"two-way message", "Alice<<->>Bob: both ways", "Alice<<->>Bob"},
		{"no arrowhead", "Alice->Bob: plain line", "Alice->Bob"},
		{"activation", "Alice->>+Bob: wake up", "Alice->>+Bob"},
		{"activate", "activate Bob", "activate Bob"},
		{"semicolon in a message", "Alice->>Bob: first; second part", "second part"},
		{"name starting with a block word", "parser->>Bob: parsed", "parser->>Bob"},
		{"accented message", "Alice->>Bob: héllo wörld", "héllo wörld"},
		{"accented name", "Renée->>Bob: hi", "Renée"},
		{"wide message", "Alice->>Bob: 日本語", "日本語"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "sequenceDiagram\n  Alice->>Bob: hello\n  " + tc.statement
			assertMermaidKeeps(t, src, []string{tc.want})
			if all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 300)); !strings.Contains(all, "source") {
				t.Fatalf("no note that the source is shown:\n%s", all)
			}
		})
	}
}

// What the drawing shows in full stays a drawing: declared names, messages in
// both directions, a dashed reply and a message to itself.
func TestMermaidPlainSequenceIsStillDrawn(t *testing.T) {
	src := `sequenceDiagram
  %% who talks to whom
  participant A as Alice
  actor B as Bob
  A->>B: hello
  B-->>A: hi back
  B->>B: think
  api-gateway->>A: done`
	all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 120))
	if strings.Contains(all, "source") || !strings.Contains(all, "┌") {
		t.Fatalf("a plain sequence diagram is drawn:\n%s", all)
	}
	for _, want := range []string{"Alice", "Bob", "api-gateway", "hello", "hi back", "think", "done", "╌"} {
		if !strings.Contains(all, want) {
			t.Fatalf("drawing loses %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "A->>B") {
		t.Fatalf("a plain sequence diagram shows its source:\n%s", all)
	}
}

const mermaidTestER = `erDiagram
  ZEBRA ||--o{ APPLE : eats
  APPLE }o--|| MANGO : "grows on"
  MANGO |o--o| ZEBRA : tempts
  KIWI ||..|{ MANGO : "sits by"
  MANGO {
    string name PK "the common name"
    int weight
  }`

// Every relationship has its cardinality and label, entities keep the author's
// order, and attributes keep their comments.
func TestMermaidERKeepsEveryRelationship(t *testing.T) {
	assertMermaidKeeps(t, mermaidTestER, []string{
		"ZEBRA ||--o{ APPLE : eats",
		`APPLE }o--|| MANGO : "grows on"`,
		"MANGO |o--o| ZEBRA : tempts",
		`KIWI ||..|{ MANGO : "sits by"`,
		`string name PK "the common name"`,
		"int weight",
	})
	all := joinPreview(mermaidTestApp().mermaidLines(mermaidTestER, 0, 200))
	if !strings.Contains(all, "source") {
		t.Fatalf("no note that the source is shown:\n%s", all)
	}
	if strings.Index(all, "ZEBRA") > strings.Index(all, "APPLE") || strings.Index(all, "APPLE") > strings.Index(all, "KIWI") {
		t.Fatalf("entities are not in the author's order:\n%s", all)
	}
}

// With two entities the library draws one relationship and may swap the boxes,
// which puts each cardinality on the wrong entity.
func TestMermaidERWithTwoEntitiesKeepsTheRelationship(t *testing.T) {
	src := "erDiagram\n  ZEBRA ||--o{ APPLE : eats\n  ZEBRA ||--|| APPLE : guards"
	assertMermaidKeeps(t, src, []string{"ZEBRA ||--o{ APPLE : eats", "ZEBRA ||--|| APPLE : guards"})
}

// The same note always reads the same: the library takes entities and classes
// out of a map, so its order changes from one parse to the next.
func TestMermaidERAndClassReadTheSameEveryTime(t *testing.T) {
	for _, src := range []string{mermaidTestER, mermaidTestClass} {
		first := joinPreview(mermaidTestApp().mermaidLines(src, 0, 120))
		for i := 0; i < 30; i++ {
			if got := joinPreview(mermaidTestApp().mermaidLines(src, 0, 120)); got != first {
				t.Fatalf("parse %d reads differently:\n%s\n--- first:\n%s", i, got, first)
			}
		}
	}
}

const mermaidTestClass = `classDiagram
  Zebra <|-- Animal
  Car *-- Engine : has
  Car "1" --> "4" Wheel : rolls on
  Duck ..|> Swimmer
  Goose<|--Gosling
  Fox <-- Hen
  class Animal {
    <<abstract>>
    +String name
    +eat(food, amount) void
    +create()$ Animal
    +speak()* String
  }
  namespace Zoo {
    class Tiger
  }
  note for Car "a note about cars"`

// Relationship types, cardinalities and labels, method arguments and return
// types, static and abstract marks, namespaces and notes can all be read.
func TestMermaidClassKeepsMembersAndRelationships(t *testing.T) {
	assertMermaidKeeps(t, mermaidTestClass, []string{
		"Zebra <|-- Animal",
		"Car *-- Engine : has",
		`Car "1" --> "4" Wheel : rolls on`,
		"Duck ..|> Swimmer",
		"Goose<|--Gosling",
		"Fox <-- Hen",
		"<<abstract>>",
		"+String name",
		"+eat(food, amount) void",
		"+create()$ Animal",
		"+speak()* String",
		"namespace Zoo",
		"class Tiger",
		`note for Car "a note about cars"`,
	})
	all := joinPreview(mermaidTestApp().mermaidLines(mermaidTestClass, 0, 200))
	if !strings.Contains(all, "source") {
		t.Fatalf("no note that the source is shown:\n%s", all)
	}
	if strings.Index(all, "Zebra") > strings.Index(all, "Car") || strings.Index(all, "Car") > strings.Index(all, "Tiger") {
		t.Fatalf("classes are not in the author's order:\n%s", all)
	}
}

// A pane narrower than the preview's old floor of ten cells cut the end off every
// row, so wrapping has to follow the pane all the way down.
func TestMermaidSourceIsWholeInAVeryNarrowPane(t *testing.T) {
	src := "erDiagram\n  ZEBRA ||--o{ APPLE : eats"
	a, _ := linkTestApp(t, map[string]string{"Index.md": "```mermaid\n" + src + "\n```\n"})
	a.openNote("Index.md", true)
	tb, buf := a.activeTab(), a.activeBuffer()
	if tb == nil || buf == nil {
		t.Fatal("no open note")
	}
	for _, w := range []int{5, 7, 9, 11} {
		out := stripAnsi(a.renderPreview(tb, buf, w, 200, false))
		for _, row := range strings.Split(out, "\n") {
			if got := cellWidth(row); got != w {
				t.Fatalf("row is %d cells in a pane of %d: %q", got, w, row)
			}
		}
		if flat := squeezed(out); !strings.Contains(flat, squeezed("ZEBRA ||--o{ APPLE : eats")) {
			t.Fatalf("pane of %d loses the relationship:\n%s", w, out)
		}
	}
}
