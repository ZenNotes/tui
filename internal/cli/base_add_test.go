package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The schema.json the app writes (JSON.stringify with two spaces and a
// trailing newline) for a database with three views: Shelf and Later show
// only their picked rows, All shows every row. Shelf's picks sit between
// other keys, so a rewrite that moved them would show.
const pickedSchemaFormat = `{
  "version": 1,
  "idFieldId": "f_id",
  "fields": [
    {
      "id": "f_id",
      "name": "id",
      "type": "text",
      "hidden": true
    },
    {
      "id": "f_title",
      "name": "Title",
      "type": "text"
    }
  ],
  "views": [
    {
      "id": "shelf",
      "name": "Shelf",
      "type": "table",
      "filters": [],
      "sorts": [],
      "pickedRowIds": [
        "r1"
      ],
      "columnOrder": [
        "f_id",
        "f_title"
      ]
    },
    {
      "id": "all",
      "name": "All",
      "type": "table",
      "filters": [],
      "sorts": []
    },
    {
      "id": "later",
      "name": "Later",
      "type": "table",
      "filters": [],
      "sorts": [],
      "pickedRowIds": [
        "r2"
      ]
    }
  ],
  "activeViewId": "%s"
}
`

func pickedReadingVault(t *testing.T) (root, base string) {
	t.Helper()
	isolatedCLI(t)
	root = t.TempDir()
	base = filepath.Join(root, "inbox", "Reading.base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "data.csv"), []byte("id,Title\nr1,Dune\nr2,Emma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, base
}

// openOn writes the schema with the database opening on activeViewID and
// returns the bytes written.
func openOn(t *testing.T, base, activeViewID string) string {
	t.Helper()
	text := fmt.Sprintf(pickedSchemaFormat, activeViewID)
	if err := os.WriteFile(filepath.Join(base, "schema.json"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return text
}

func runBaseAdd(t *testing.T, root string, flags ...string) string {
	t.Helper()
	args := append([]string{"base", "add", "Reading", "--vault", root}, flags...)
	return captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// As + Row does in the app's grid, a row `zn base add` makes joins the picks
// of the view the database opens on (activeViewId), so that view shows it
// the next time the database is opened. The same rule as the desktop CLI's
// cmdBaseAdd (addRowInView).
func TestBaseAddJoinsThePicksOfTheViewTheDatabaseOpensOn(t *testing.T) {
	root, base := pickedReadingVault(t)
	before := openOn(t, base, "shelf")
	out := runBaseAdd(t, root, "--set", "Title=Ulysses", "--json")
	var added map[string]any
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	id, _ := added["id"].(string)
	if added["ok"] != true || id == "" || added["title"] != "Ulysses" || added["pickedInView"] != "Shelf" {
		t.Fatalf("json: %v", added)
	}
	// Shelf gains the row, in place; Later keeps its own picks, and All
	// gains none. Nothing else in schema.json moves.
	want := strings.Replace(before, "\"r1\"\n      ],", "\"r1\",\n        \""+id+"\"\n      ],", 1)
	if got := readFile(t, filepath.Join(base, "schema.json")); got != want {
		t.Fatalf("schema.json:\n%s\nwant:\n%s", got, want)
	}
	if got := readFile(t, filepath.Join(base, "data.csv")); got != "id,Title\nr1,Dune\nr2,Emma\n"+id+",Ulysses\n" {
		t.Fatalf("data.csv: %q", got)
	}
}

// Opening on a view of every row, the add changes no view's picks, and
// schema.json is not rewritten at all.
func TestBaseAddLeavesEveryViewAloneWhenTheOpeningViewHasNoPicks(t *testing.T) {
	root, base := pickedReadingVault(t)
	before := openOn(t, base, "all")
	out := runBaseAdd(t, root, "--set", "Title=Middlemarch")
	if !strings.HasPrefix(out, "Added Middlemarch (") || strings.Contains(out, "picked rows") {
		t.Fatalf("output: %q", out)
	}
	if got := readFile(t, filepath.Join(base, "schema.json")); got != before {
		t.Fatalf("schema.json changed:\n%s", got)
	}
	if got := readFile(t, filepath.Join(base, "data.csv")); !strings.Contains(got, ",Middlemarch\n") {
		t.Fatalf("data.csv: %q", got)
	}

	out = runBaseAdd(t, root, "--set", "Title=Persuasion", "--json")
	var added map[string]any
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, ok := added["pickedInView"]; ok || added["title"] != "Persuasion" {
		t.Fatalf("json: %v", added)
	}
}

// The text output says where the row went, before the page line, and a
// record page and the picks land in schema.json together.
func TestBaseAddSaysWhichViewsPicksTheRowJoined(t *testing.T) {
	root, base := pickedReadingVault(t)
	openOn(t, base, "later")
	out := runBaseAdd(t, root, "--set", "Title=Kindred", "--page")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "Added Kindred (") ||
		lines[1] != "  added to the picked rows of the Later view" ||
		lines[2] != "  page: inbox/Reading.base/Kindred.md" {
		t.Fatalf("output: %q", out)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(lines[0], "Added Kindred ("), ")")
	var schema struct {
		Views []struct {
			ID           string    `json:"id"`
			PickedRowIDs *[]string `json:"pickedRowIds"`
		} `json:"views"`
		ActiveViewID string            `json:"activeViewId"`
		Pages        map[string]string `json:"pages"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(base, "schema.json"))), &schema); err != nil {
		t.Fatal(err)
	}
	picks := map[string][]string{}
	for _, v := range schema.Views {
		if v.PickedRowIDs != nil {
			picks[v.ID] = *v.PickedRowIDs
		}
	}
	if !reflect.DeepEqual(picks, map[string][]string{"shelf": {"r1"}, "later": {"r2", id}}) {
		t.Fatalf("picks: %v", picks)
	}
	if schema.ActiveViewID != "later" || schema.Pages[id] != "Kindred.md" {
		t.Fatalf("active %q, pages %v", schema.ActiveViewID, schema.Pages)
	}
}
