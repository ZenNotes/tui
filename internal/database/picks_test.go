package database

import (
	"reflect"
	"testing"
)

func picksDoc(t *testing.T) *Doc {
	t.Helper()
	sidecar, err := ParseObject(`{"version":1,"idFieldId":"f0","fields":[{"id":"f0","name":"id","type":"text","hidden":true},{"id":"f1","name":"Title","type":"text"}],"views":[` +
		`{"id":"shelf","name":"Shelf","type":"table","filters":[],"sorts":[],"pickedRowIds":["r1"]},` +
		`{"id":"all","name":"All","type":"table","filters":[],"sorts":[]},` +
		`{"id":"later","name":"Later","type":"table","filters":[],"sorts":[],"pickedRowIds":["r2"]},` +
		`{"id":"none","name":"None yet","type":"table","filters":[],"sorts":[],"pickedRowIds":[]},` +
		`{"id":"cleared","name":"Cleared","type":"table","filters":[],"sorts":[],"pickedRowIds":null}` +
		`],"activeViewId":"shelf"}`)
	if err != nil {
		t.Fatal(err)
	}
	d := &Doc{Path: "Reading.base/data.csv", Sidecar: sidecar, Rows: []Row{
		{ID: "r1", Cells: map[string]string{"f0": "r1", "f1": "Dune"}},
		{ID: "r2", Cells: map[string]string{"f0": "r2", "f1": "Emma"}},
	}}
	if err := d.Resync(); err != nil {
		t.Fatal(err)
	}
	return d
}

// picks reads a view's pickedRowIds as the sidecar will write them, and
// whether the key is there at all.
func picks(t *testing.T, d *Doc, viewID string) ([]string, bool) {
	t.Helper()
	view := d.viewObject(viewID)
	if view == nil {
		t.Fatalf("no view %q", viewID)
	}
	value, ok := view.Get("pickedRowIds")
	if !ok {
		return nil, false
	}
	list, isList := value.([]any)
	if !isList {
		return nil, true
	}
	return stringsFromArray(list), true
}

// The same rules as addRowInView in the app's shared-domain
// database-records.ts.
func TestAddRowInViewJoinsOnlyThatViewsPicks(t *testing.T) {
	d := picksDoc(t)
	row, changed := d.AddRowInView("shelf")
	if !changed || len(d.Rows) != 3 || d.Rows[2].ID != row.ID {
		t.Fatalf("add in a picks view: changed %v, rows %d", changed, len(d.Rows))
	}
	if got, _ := picks(t, d, "shelf"); !reflect.DeepEqual(got, []string{"r1", row.ID}) {
		t.Fatalf("shelf picks: %v", got)
	}
	// A view with picks nobody is looking at keeps them.
	if got, _ := picks(t, d, "later"); !reflect.DeepEqual(got, []string{"r2"}) {
		t.Fatalf("later picks: %v", got)
	}
	if _, ok := picks(t, d, "all"); ok {
		t.Fatal("a view of every row gained picks")
	}

	// Adding in a view of every row changes no view.
	if _, changed := d.AddRowInView("all"); changed {
		t.Fatal("a view without picks reported a picks change")
	}
	if got, _ := picks(t, d, "shelf"); !reflect.DeepEqual(got, []string{"r1", row.ID}) {
		t.Fatalf("shelf picks after an add elsewhere: %v", got)
	}
	if got, _ := picks(t, d, "later"); !reflect.DeepEqual(got, []string{"r2"}) {
		t.Fatalf("later picks after an add elsewhere: %v", got)
	}
	if _, ok := picks(t, d, "all"); ok {
		t.Fatal("a view of every row gained picks")
	}

	// An empty list is a view of no rows, not of every row: the row joins it.
	row3, changed := d.AddRowInView("none")
	if got, _ := picks(t, d, "none"); !changed || !reflect.DeepEqual(got, []string{row3.ID}) {
		t.Fatalf("empty picks: changed %v, %v", changed, got)
	}
	// null is no picks, as the app reads it.
	if _, changed := d.AddRowInView("cleared"); changed {
		t.Fatal("pickedRowIds: null is not a list of picks")
	}
	// An unknown view adds the row and touches no view.
	before := len(d.Rows)
	if _, changed := d.AddRowInView("gone"); changed || len(d.Rows) != before+1 {
		t.Fatalf("unknown view: changed %v, rows %d", changed, len(d.Rows))
	}
}
