package database

import (
	"strings"
	"testing"
)

func testDoc(t *testing.T) *Doc {
	t.Helper()
	sidecar, err := ParseObject(`{"version":1,"idFieldId":"f0","fields":[{"id":"f0","name":"id","type":"text","hidden":true},{"id":"f1","name":"Name","type":"text"},{"id":"f2","name":"Status","type":"select","options":[{"id":"o1","value":"todo"}]}],"views":[{"id":"v1","name":"Table","type":"table","filters":[],"sorts":[],"columnOrder":["f0","f1","f2"],"hiddenFieldIds":["f0"]}],"activeViewId":"v1"}`)
	if err != nil {
		t.Fatal(err)
	}
	d := &Doc{Path: "Books.base/data.csv", Sidecar: sidecar, Rows: []Row{{ID: "r1", Cells: map[string]string{"f0": "r1", "f1": "Dune", "f2": "todo"}}}}
	if err := d.Resync(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAddRenameRetypeDeleteField(t *testing.T) {
	d := testDoc(t)
	f, err := d.AddField("Name", "select")
	if err != nil || f.Name != "Name 2" || len(f.Options) != 0 {
		t.Fatalf("add: %+v %v", f, err)
	}
	if cols := d.VisibleColumns("v1"); len(cols) != 3 || cols[2].ID != f.ID {
		t.Fatalf("new field should be the last visible column: %+v", cols)
	}
	if err := d.RenameField(f.ID, "Genre"); err != nil || d.FieldByID(f.ID).Name != "Genre" {
		t.Fatal("rename")
	}
	if err := d.RetypeField(f.ID, "number"); err != nil || d.FieldByID(f.ID).Type != "number" {
		t.Fatal("retype")
	}
	if err := d.DeleteField("f0"); err == nil {
		t.Fatal("id field must not be deletable")
	}
	if err := d.DeleteField("f2"); err != nil || d.FieldByID("f2") != nil {
		t.Fatal("delete")
	}
	if _, ok := d.Rows[0].Cells["f2"]; ok {
		t.Fatal("cells of a deleted field should go")
	}
	text, _ := Stringify(d.Sidecar)
	if strings.Contains(text, `"f2"`) {
		t.Fatalf("sidecar still references the deleted field: %s", text)
	}
}

func TestMoveHideAndViews(t *testing.T) {
	d := testDoc(t)
	if err := d.MoveColumn("v1", "f2", "left"); err != nil {
		t.Fatal(err)
	}
	if cols := d.VisibleColumns("v1"); cols[0].ID != "f2" || cols[1].ID != "f1" {
		t.Fatalf("move left: %+v", cols)
	}
	if err := d.SetFieldHidden("v1", "f1", true); err != nil {
		t.Fatal(err)
	}
	if cols := d.VisibleColumns("v1"); len(cols) != 1 || cols[0].ID != "f2" {
		t.Fatalf("hidden: %+v", cols)
	}
	if err := d.SetViewSorts("v1", []SortRule{{FieldID: "f1", Direction: "desc"}}); err != nil || d.Views[0].Sorts[0].Direction != "desc" {
		t.Fatal("sorts")
	}
	if err := d.SetViewFilters("v1", []FilterRule{{FieldID: "f2", Op: "is", Value: "todo"}}, "or"); err != nil || d.Views[0].FilterConjunction != "or" || d.Views[0].Filters[0].Value != "todo" {
		t.Fatal("filters")
	}
	v, err := d.AddView("board")
	if err != nil || v.Type != "board" || v.GroupByFieldID != "f2" || d.ActiveViewID != v.ID {
		t.Fatalf("board view: %+v %v", v, err)
	}
	if err := d.RenameView(v.ID, "Kanban"); err != nil || d.ViewByID(v.ID).Name != "Kanban" {
		t.Fatal("rename view")
	}
	if err := d.RemoveView(v.ID); err != nil || len(d.Views) != 1 || d.ActiveViewID != "v1" {
		t.Fatal("remove view")
	}
	if err := d.RemoveView("v1"); err == nil {
		t.Fatal("the last view must stay")
	}
}

func TestNoteLinksAndDuplicate(t *testing.T) {
	links := SplitNoteLinks("[[Dune]] and [[Neuromancer|cyber]], text")
	if len(links) != 2 || links[1] != "Neuromancer" {
		t.Fatalf("links %v", links)
	}
	if JoinNoteLinks(links) != "[[Dune]] [[Neuromancer]]" {
		t.Fatal("join")
	}
	d := testDoc(t)
	row, ok := d.DuplicateRow("r1")
	if !ok || len(d.Rows) != 2 || d.Rows[1].ID != row.ID || d.Rows[1].Cells["f1"] != "Dune" || d.Rows[1].Cells["f0"] != row.ID {
		t.Fatalf("duplicate: %+v", d.Rows)
	}
}
