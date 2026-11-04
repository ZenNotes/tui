package tui

import (
	"reflect"
	"testing"
)

// The connections panel reads a table's `[[C\|d]]` as a link to C on both sides:
// C lists the table as linking to it, and the table lists C, not an unresolved `C\`.
func TestConnectionsPanelReadsAnEscapedPipeLink(t *testing.T) {
	files := map[string]string{
		"Table.md": "| Link | Note |\n| --- | --- |\n| [[C\\|d]] | x |\n| [[Missing\\|m]] | y |\n",
		"C.md":     "# C\n",
	}

	t.Run("outgoing from the table", func(t *testing.T) {
		a, _ := linkTestApp(t, files)
		a.openNote("Table.md", true)
		p := &connectionsPanel{}
		p.refresh(a)
		got := []string{}
		for _, n := range p.outgoing {
			got = append(got, n.Path)
		}
		if want := []string{"C.md"}; !reflect.DeepEqual(got, want) {
			t.Errorf("outgoing = %q, want %q", got, want)
		}
		if want := []string{"Missing"}; !reflect.DeepEqual(p.missing, want) {
			t.Errorf("missing = %q, want %q", p.missing, want)
		}
	})

	t.Run("backlink on the target", func(t *testing.T) {
		a, _ := linkTestApp(t, files)
		a.openNote("C.md", true)
		p := &connectionsPanel{}
		p.refresh(a)
		got := []string{}
		for _, n := range p.backlinks {
			got = append(got, n.Path)
		}
		if want := []string{"Table.md"}; !reflect.DeepEqual(got, want) {
			t.Errorf("backlinks = %q, want %q", got, want)
		}
	})
}

// One note reached by several spellings is one link, and a link to a file that
// exists is not unresolved.
func TestConnectionsPanelListsEachTargetOnce(t *testing.T) {
	a, _ := linkTestApp(t, map[string]string{
		"C.md": "# C\n",
		"Links.md": "[[C]] [[c]] [[C#Part]] [[C^block]] [[C.md]] [[C\\|d]]\n" +
			"![[pic.png]] | ![[pic.png\\|300]] [[Missing]] [[missing]] ![[gone.png]]\n",
		"assets/pic.png": "png",
	})
	a.openNote("Links.md", true)
	p := &connectionsPanel{}
	p.refresh(a)
	got := []string{}
	for _, n := range p.outgoing {
		got = append(got, n.Path)
	}
	if want := []string{"C.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("outgoing = %q, want %q", got, want)
	}
	if want := []string{"Missing", "gone.png"}; !reflect.DeepEqual(p.missing, want) {
		t.Errorf("missing = %q, want %q", p.missing, want)
	}
}
