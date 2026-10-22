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

// The same names the app's recordPageCandidates gives, so the two link the
// same files.
func TestRecordPageCandidatesMatchTheApp(t *testing.T) {
	d := testDoc(t)
	want := []string{
		"Books.base/What- a -guide--.md",
		`Books.base/What: a "guide"?.md`,
		"Books.base/pages/What- a -guide--.md",
		`Books.base/pages/What: a "guide"?.md`,
	}
	if got := d.RecordPageCandidates(`What: a "guide"?`); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates:\n got %q\nwant %q", got, want)
	}
	// A slash in a title never makes a path of it.
	if got := d.RecordPageCandidates("A/B"); !reflect.DeepEqual(got, []string{"Books.base/A-B.md", "Books.base/pages/A-B.md"}) {
		t.Fatalf("slash: %q", got)
	}
}

// A page file already in the folder, unmapped, is linked rather than copied;
// one another row maps is never taken, and twins share no file.
func TestPlanRecordPagesLinksPageFilesAlreadyThere(t *testing.T) {
	d := testDoc(t)
	d.Rows = []Row{
		{ID: "r1", Cells: map[string]string{"f0": "r1", "f1": "Dune"}},
		{ID: "r2", Cells: map[string]string{"f0": "r2", "f1": "Kindred"}},
		{ID: "r3", Cells: map[string]string{"f0": "r3", "f1": "Emma"}},
		{ID: "r4", Cells: map[string]string{"f0": "r4", "f1": "Dune"}},
	}
	d.Pages = map[string]string{"r9": "Books.base/Emma.md"}
	files := map[string]bool{"Books.base/Dune.md": true, "Books.base/pages/Kindred.md": true, "Books.base/Emma.md": true}
	plan, err := d.PlanRecordPages(func(p string) (bool, error) { return files[p], nil })
	if err != nil {
		t.Fatal(err)
	}
	adopted := [][2]string{}
	for _, a := range plan.Adopt {
		adopted = append(adopted, [2]string{a.Row.ID, a.Page})
	}
	if !reflect.DeepEqual(adopted, [][2]string{{"r1", "Books.base/Dune.md"}, {"r2", "Books.base/pages/Kindred.md"}}) {
		t.Fatalf("adopted: %v", adopted)
	}
	missing := []string{}
	for _, row := range plan.Missing {
		missing = append(missing, row.ID)
	}
	// Emma's file belongs to another row; the second Dune finds its file taken.
	if !reflect.DeepEqual(missing, []string{"r3", "r4"}) {
		t.Fatalf("missing: %v", missing)
	}
}
