package database

import (
	"errors"
	"reflect"
	"testing"
)

// The same rules as the desktop's "Create pages for all records".
func TestPlanRecordPages(t *testing.T) {
	d := testDoc(t)
	d.Rows = []Row{
		{ID: "r1", Cells: map[string]string{"f0": "r1", "f1": "Dune"}},
		{ID: "r2", Cells: map[string]string{"f0": "r2", "f1": "Emma"}},
		{ID: "r3", Cells: map[string]string{"f0": "r3", "f1": "Kindred"}},
		{ID: "r4", Cells: map[string]string{"f0": "r4", "f1": "   "}},
	}
	d.Pages = map[string]string{"r1": "Books.base/Dune.md", "r2": "Books.base/Emma.md"}
	plan, err := d.PlanRecordPages(func(p string) (bool, error) { return p == "Books.base/Dune.md", nil })
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, row := range plan.Missing {
		ids = append(ids, row.ID)
	}
	// Emma's page was deleted, so she gets a new one; the blank title is left out.
	if !reflect.DeepEqual(ids, []string{"r2", "r3"}) || plan.Existing != 1 || plan.Untitled != 1 {
		t.Fatalf("plan: missing %v, existing %d, untitled %d", ids, plan.Existing, plan.Untitled)
	}
}

// A read that fails is not a missing page: planning stops instead of
// minting a second page for a record that has one.
func TestPlanRecordPagesStopsOnAReadError(t *testing.T) {
	d := testDoc(t)
	d.Pages = map[string]string{"r1": "Books.base/Dune.md"}
	boom := errors.New("server unreachable")
	if _, err := d.PlanRecordPages(func(string) (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Fatalf("want the read error, got %v", err)
	}
}
