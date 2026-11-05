package tui

import (
	"context"
	"fmt"
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

// A pipe of an alias's own is not a column rule.
func TestGlamourTableAliasKeepsItsOwnPipe(t *testing.T) {
	all := joinPreview(glamourTestLines(t, "| A | B |\n|---|---|\n| [[Note|a\\|b]] | x |\n| plain text here | y |", 60))
	if !strings.Contains(all, "a|b") {
		t.Fatalf("the alias changed:\n%s", all)
	}
}

// A state's class is not its label.
func TestMermaidStateClasses(t *testing.T) {
	all := mermaidText40("stateDiagram-v2\n  A:::x --> B\n  B --> C:::y : go")
	if strings.Contains(all, "::") || strings.Contains(all, "source") || strings.Count(all, "┌") != 3 || !strings.Contains(all, "go") {
		t.Fatalf("classes read as labels:\n%s", all)
	}
	if all := mermaidText40("stateDiagram-v2\n  A --> B:::x"); strings.Contains(all, ":") || strings.Contains(all, "x") {
		t.Fatalf("a class became a label:\n%s", all)
	}
}

// A comment ends its line, whatever it holds.
func TestMermaidCommentHidesItsStatements(t *testing.T) {
	for _, src := range []string{"graph TD\n  A --> B %% c; D --> E", "graph TD\n  A --> B; %% D --> E; F --> G"} {
		all := mermaidText40(src)
		if strings.Count(all, "┌") != 2 || strings.Contains(all, "source") {
			t.Fatalf("%q draws from its comment:\n%s", src, all)
		}
	}
	assertDrawn(t, "stateDiagram-v2\n  A --> B : 100%% sure", "100%% sure")
}

// A closer the parser left behind means it ended a label early.
func TestMermaidLeftoverClosersShowSource(t *testing.T) {
	for _, src := range []string{"graph TD\n  A --> C(f(x))", "graph TD\n  A --> C[a[1]]", "graph TD\n  A --> C{a{b}}", "graph TD\n  A((a(b))) --> C"} {
		assertSource(t, src)
	}
}

func TestMermaidEntitiesDecode(t *testing.T) {
	assertDrawn(t, "graph TD\n  A[\"a #amp; b #35; #lt;c#gt; #nosuch;\"] --> B", "a & b # <c> #nosuch;", "B")
}

func TestMermaidEmptyDiagramSaysSo(t *testing.T) {
	for _, src := range []string{"graph TD", "flowchart LR", "stateDiagram-v2", "graph TD\n  classDef a fill:#fff"} {
		if all := mermaidText40(src); !strings.Contains(all, "nothing to draw") {
			t.Fatalf("%q says nothing:\n%s", src, all)
		}
	}
}

// IDs may be in any script, and may hold dots.
func TestMermaidIDsInAnyScript(t *testing.T) {
	assertDrawn(t, "graph TD\n  节点A --> 节点B\n  节点B --> Größe\n  Größe -- 是的 --> a.b", "节点A", "节点B", "Größe", "是的", "a.b")
	assertDrawn(t, "stateDiagram-v2\n  开始 --> 结束 : 完成", "开始", "结束", "完成")
	same := mermaidText40("graph TD\n  节点[Same] --> 其他[Same]")
	if !strings.Contains(same, "Same [节点]") || !strings.Contains(same, "Same [其他]") {
		t.Fatalf("an ID shows its stand-in:\n%s", same)
	}
	assertDrawn(t, "graph TD\n  znidn0z --> 节点", "znidn0z", "节点")
}

func TestMermaidCRLFFrontMatter(t *testing.T) {
	assertDrawn(t, "---\r\ntitle: Hello\r\n---\r\nflowchart LR\r\n  A --> B\r\n", "A", "B")
}

// Long inputs stay quick: a row of many links, and a label of many icons.
func TestLongInputsAreLinear(t *testing.T) {
	row := "|" + strings.Repeat(" [[a|b]]", 40000) + " | c |"
	start := time.Now()
	if cells := previewTableCells(row); len(cells) != 2 {
		t.Fatalf("got %d cells", len(cells))
	}
	var label strings.Builder
	for r := rune(0xF0000); r < 0xF0000+60000; r++ {
		label.WriteRune(r)
	}
	if _, _, err := parseFlowchart("graph TD\n  A[\"" + label.String() + "\"] --> B"); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("took %s", took)
	}
}

// A table's rows need no outer pipes once its separator has been seen.
func TestZenTableKeepsBareRows(t *testing.T) {
	all := zenTableText("| a | b |\n|---|---|\n1 | 2\n| 3 | 4 |", 40)
	if strings.Contains(all, "1 | 2") || !strings.Contains(all, "│ 1") {
		t.Fatalf("a bare row left the table:\n%s", all)
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
		{"wide letter ending a message", "Alice->>Bob: done 🎉", "done 🎉"},
		{"wide name", "participant 日", "participant 日"},
		{"tab in a message", "Alice->>Bob: a\tb", "Alice->>Bob"},
		{"combining mark in a message", "Alice->>Bob: cafe\u0301", "cafe\u0301"},
		{"late alias", "participant Alice as Al", "Alice as Al"},
		{"stick figure", "actor Carol", "actor Carol"},
		{"name ending in a dash", "Alice--->>Bob: q", "Alice--->>Bob"},
		{"name holding an arrow", "Alice->>Bob->>Carol: q", "Bob->>Carol"},
		{"line break in a message", "Alice->>Bob: one<br/>two", "one<br/>two"},
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
  %% who talks to whom; and why
  participant A as Alice
  participant B as Bob
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

// A name declared twice keeps only its first label, so the second is not drawn.
func TestMermaidSequenceRepeatedAliasShowsSource(t *testing.T) {
	src := "sequenceDiagram\n  participant A as Alice\n  participant A as Alicia\n  A->>B: hi"
	assertMermaidKeeps(t, src, []string{"Alice", "Alicia", "A->>B: hi"})
	if all := joinPreview(mermaidTestApp().mermaidLines(src, 0, 200)); !strings.Contains(all, "source") {
		t.Fatalf("no note that the source is shown:\n%s", all)
	}
}

// A fence the note never closes still holds its last line.
func TestMermaidUnclosedFenceKeepsTheLastStatement(t *testing.T) {
	for _, tc := range []struct{ name, md, want string }{
		{"sequence", "```mermaid\nsequenceDiagram\n  A->>B: one\n  B->>A: lastmsg", "lastmsg"},
		{"er", "```mermaid\nerDiagram\n  ZEBRA ||--o{ APPLE : lastrel", "lastrel"},
		{"flowchart", "```mermaid\ngraph TD\n  A --> B\n  B --> LastNode", "LastNode"},
		{"one statement", "```mermaid\ngraph TD", "graph TD"},
		{"trailing newline", "```mermaid\nsequenceDiagram\n  A->>B: lastmsg\n", "lastmsg"},
		{"closed", "```mermaid\nsequenceDiagram\n  A->>B: lastmsg\n```\n", "lastmsg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if all := joinPreview(glamourTestLines(t, tc.md, 60)); !strings.Contains(all, tc.want) {
				t.Fatalf("loses %q:\n%s", tc.want, all)
			}
		})
	}
	if all := joinPreview(glamourTestLines(t, "```mermaid\nsequenceDiagram\n  A->>B: hi\n```\n", 60)); strings.Contains(all, "```") {
		t.Fatalf("a closed fence shows its closing line:\n%s", all)
	}
}

// A note written on Windows comes through an embed with its carriage returns.
func TestMermaidEmbeddedCRLFNoteHasCleanRows(t *testing.T) {
	for name, fence := range map[string]string{
		"class":    "classDiagram\r\n  Zebra <|-- Animal\r\n",
		"er":       "erDiagram\r\n  ZEBRA ||--o{ APPLE : eats\r\n",
		"sequence": "sequenceDiagram\r\n  Alice->>Bob: hello\r\n",
		"graph":    "graph TD\r\n  A --> B\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := linkTestApp(t, map[string]string{"Crlf.md": "```mermaid\r\n" + fence + "```\r\n"})
			lines, err := a.renderGlamourLinesFrom("![[Crlf]]\n", 60, "Index.md")
			if err != nil {
				t.Fatal(err)
			}
			all := joinPreview(lines)
			if strings.Contains(all, "\r") {
				t.Fatalf("carriage return in a row: %q", all)
			}
			if strings.Contains(all, "mermaid:") {
				t.Fatalf("does not parse:\n%s", all)
			}
		})
	}
}

// Formula source wraps in a narrow pane rather than ending in an ellipsis.
func TestMathSourceWrapsInANarrowPane(t *testing.T) {
	for _, width := range []int{5, 8, 12, 20, 80} {
		lines := mermaidTestApp().mathLines("$$x^2+y^2+a_long_term_here$$", 0, width)
		all := joinPreview(lines)
		if strings.Contains(all, "…") || !strings.Contains(squeezed(strings.ReplaceAll(all, "▎", "")), "x^2+y^2+a_long_term_here") {
			t.Fatalf("width %d cuts the formula:\n%s", width, all)
		}
	}
}

// The library draws on a grid of width times height, so a huge diagram shows its
// source before that grid is allocated.
func TestMermaidHugeSequenceShowsSourceWithoutDrawing(t *testing.T) {
	var many strings.Builder
	many.WriteString("sequenceDiagram\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&many, "  participant P%d\n", i)
	}
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&many, "  P%d->>P%d: m\n", i, (i+1)%2000)
	}
	if parsed := parseMermaidText(many.String()); !parsed.partial || parsed.rows != nil {
		t.Fatal("a huge diagram is drawn")
	}
	small := parseMermaidText("sequenceDiagram\n  A->>B: hi")
	if small.partial || small.rows == nil {
		t.Fatal("a small diagram is not drawn")
	}
}

// The preview follows the pane all the way down, so nothing it draws may assume
// a minimum width.
func TestPreviewSurvivesVeryNarrowPanes(t *testing.T) {
	note := `---
title: Rich
---
# A heading that is rather long

A paragraph with a [[Target|link]], a [web link](https://example.com), and ` + "`code`" + `.

- a list item that goes on for a while
- [ ] a task that goes on for a while
  - nested item

> a quote that goes on

> [!note] A callout
> with a body

| one | two |
|-----|-----|
| [[Target|x]] | y |

---

` + "```go\nfunc main() {}\n```" + `

` + "```mermaid\ngraph LR\n  A[One long label] -->|edge| B\n  B --> A\n```" + `

` + "```mermaid\nsequenceDiagram\n  A->>B: hi\n```" + `

$$x^2+y^2$$

![[Target]]

![[missing.png]]

![a picture](missing.png)

![a file](report.pdf)

https://www.youtube.com/watch?v=abc123
`
	for _, style := range []string{"auto", "zen"} {
		a, _ := linkTestApp(t, map[string]string{"Index.md": note, "Target.md": "# Target\n\nbody\n"})
		a.prefs.PreviewStyle = style
		a.openNote("Index.md", true)
		tb, buf := a.activeTab(), a.activeBuffer()
		if tb == nil || buf == nil {
			t.Fatal("no open note")
		}
		for w := 1; w <= 16; w++ {
			a.renderPreview(tb, buf, w, 400, true)
		}
	}
}

// flowRows draws a flowchart as plain rows at a width.
func flowRows(t *testing.T, src string, width int) []string {
	t.Helper()
	f, partial, err := parseFlowchart(src)
	if err != nil || partial {
		t.Fatalf("parse: %v, partial %v", err, partial)
	}
	return f.rows(width)
}

func TestWideFlowchartDrawsARowOnlyForAStraightRun(t *testing.T) {
	straight := flowRows(t, "graph LR\nA[One]-->B[Two]-->C[Three]", 200)
	if !strings.Contains(strings.Join(straight, "\n"), "│ One │─▶│ Two │─▶│ Three │") {
		t.Errorf("a straight run left its row:\n%s", strings.Join(straight, "\n"))
	}

	for _, tc := range []struct {
		name, src string
		listed    string // the row under its source that names the edge
	}{
		{"back to an earlier box", "graph LR\nA[One]-->B[Two]-->C[Three]\nC-->A", "└─▶ One"},
		{"skips a box", "graph LR\nA[One]-->B[Two]\nA-->C[Three]\nB-->C", "├─▶ Three"},
		{"a loop with a label", "graph LR\nA[One]-->B[Two]\nB-->|again|A", "└─▶ One (again)"},
		{"a second edge to the next box", "graph LR\nA[One]-->B[Two]\nA-.->B", "├╌▶ Two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := flowRows(t, tc.src, 200)
			text := strings.Join(rows, "\n")
			if !strings.Contains(text, tc.listed) {
				t.Errorf("the edge is not listed as %q:\n%s", tc.listed, text)
			}
			for _, row := range rows {
				if strings.Count(row, "┐") > 1 || strings.Contains(row, "│─▶│") {
					t.Errorf("boxes are side by side, so they read as a sequence:\n%s", text)
					break
				}
			}
		})
	}
}

// Boxes with no edge between them get no stroke in a column either.
func TestFlowchartColumnDrawsNoStrokeBetweenUnjoinedBoxes(t *testing.T) {
	rows := flowRows(t, "graph LR\nA[One]-->B[Two]\nA-->C[Three]", 200)
	text := strings.Join(rows, "\n")
	two := strings.Index(text, "Two")
	three := strings.LastIndex(text, "Three")
	if two < 0 || three < 0 {
		t.Fatalf("a box is missing:\n%s", text)
	}
	if between := text[two:three]; strings.ContainsAny(between, "▼┬▶") {
		t.Errorf("a stroke joins Two to Three, which share no edge:\n%s", text)
	}
}

func TestWideFlowchartKeepsEveryNodeAndEdgeAtAnyWidth(t *testing.T) {
	src := "graph LR\nA[One]-->B[Two]\nB-->|yes|C[Three]\nB-->|no|A\nC-.->A\nD[Alone]"
	for _, width := range []int{12, 30, 80, 200, 400} {
		text := strings.Join(flowRows(t, src, width), "\n")
		for _, want := range []string{"One", "Two", "Three", "Alone", "yes", "no"} {
			if !strings.Contains(text, want) {
				t.Errorf("width %d lost %q:\n%s", width, want, text)
			}
		}
	}
}

func TestMermaidEntityKeepsItsStatementWhole(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"graph TD\nA[d #35; e]-->B", "d # e"},
		{"graph TD\nA[\"d #35; e\"]-->B", "d # e"},
		{"graph TD\nA[x #amp; y]-->B", "x & y"},
		{"graph TD\nA[x #quot;q#quot; y]-->B", `x "q" y`},
		{"graph TD\nA-->|n #35;1|B", "n #1"},
		{"graph TD; A-->B; B-->C", "C"},
	} {
		text := strings.Join(flowRows(t, tc.src, 60), "\n")
		if !strings.Contains(text, tc.want) {
			t.Errorf("%q drew:\n%s", tc.src, text)
		}
	}
}

// An arrow no diagram defines stays in view as written, rather than draw a box
// called `A-` or lose the edge.
func TestMermaidLongerSequenceArrowShowsItsSource(t *testing.T) {
	a := mermaidTestApp()
	for _, src := range []string{"sequenceDiagram\nA--->>B: hi", "graph LR\nA--->>B"} {
		text := ""
		for _, l := range a.mermaidTextLines(parseMermaidText(src), src, 0, 60) {
			text += stripAnsi(l.text) + "\n"
		}
		if !strings.Contains(text, "A--->>B") || !strings.Contains(text, "source.") {
			t.Errorf("%q drew:\n%s", src, text)
		}
	}
}

// A colour's semicolon ends its statement, so what follows it is drawn.
func TestMermaidColourKeepsItsSemicolon(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want []string
	}{
		{"graph TD\nA[One]-->B[Two]; style A fill:#f9f; B-->C[Three]", []string{"One", "Two", "Three"}},
		{"graph LR; classDef big fill:#f96; A[One]:::big-->B[Two]; B-->C[Three]", []string{"One", "Two", "Three"}},
		{"graph TD\nA[One]-->B[Two]; style A fill: #123; B-->C[Three]", []string{"One", "Three"}},
		{"graph TD\nA[One]-->B[Two]; style A fill:#f9f,stroke:#333; B-->C[Three]", []string{"One", "Three"}},
		{"graph TD\nA[One #35; two]-->B[Two]; style A fill:#f9f; B-->C[Three]", []string{"One # two", "Three"}},
	} {
		text := strings.Join(flowRows(t, tc.src, 60), "\n")
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("%q has no %q:\n%s", tc.src, want, text)
			}
		}
	}
}

func TestMermaidLabelDropsInlineTags(t *testing.T) {
	text := strings.Join(flowRows(t, "graph TD\nA[\"<b>bold</b> x\"]-->|<i>lab</i>|B[\"<span style='color:red'>red</span>\"]\nC[\"a <b>\"]-->D[\"keep <thing> here\"]", 60), "\n")
	for _, want := range []string{"bold x", "lab", "red", "keep <thing> here"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
	for _, bad := range []string{"<b>", "</b>", "<i>", "<span", "</span>"} {
		if strings.Contains(text, bad) {
			t.Errorf("%q is still there:\n%s", bad, text)
		}
	}
}

// A pane too narrow for a box still shows every node and edge, and fits.
func TestMermaidFitsPanesOfOneToEightCells(t *testing.T) {
	a := mermaidTestApp()
	src := "graph TD\nA[One two]-->B[Two]\nB-->|yes|A\nsubgraph G\nC[Three]\nend"
	parsed := parseMermaidText(src)
	for width := 1; width <= 16; width++ {
		lines := a.mermaidTextLines(parsed, src, 0, width)
		text := ""
		for _, l := range lines {
			plain := stripAnsi(l.text)
			if got := cellWidth(plain); got > width {
				t.Errorf("width %d: row %q is %d cells", width, plain, got)
			}
			text += plain
		}
		text = squeezed(text)
		for _, want := range []string{"One", "Two", "yes", "Three"} {
			if !strings.Contains(text, want) {
				t.Errorf("width %d lost %q: %q", width, want, text)
			}
		}
	}
	// A wide character has nowhere to go in one cell, and the drawing still ends.
	if got := flowWrap("＠＠", 1); len(got) != 2 {
		t.Errorf("flowWrap gave %q", got)
	}
	// A source shown instead of a drawing fits as well.
	for width := 1; width <= 8; width++ {
		for _, l := range a.mermaidTextLines(parseMermaidText("classDiagram\nA <|-- B"), "classDiagram\nA <|-- B", 0, width) {
			if got := cellWidth(stripAnsi(l.text)); got > width {
				t.Errorf("source at width %d: %q is %d cells", width, stripAnsi(l.text), got)
			}
		}
	}
}
