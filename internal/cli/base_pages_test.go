package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// `zn base pages` gives every record of a database converted from a CSV its
// record page, so each one can be linked; the desktop's "Create pages for
// all records".
func basePagesVault(t *testing.T) (root, base string) {
	t.Helper()
	isolatedCLI(t)
	root = t.TempDir()
	base = filepath.Join(root, "inbox", "Books.base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	csv := "id,Title,Author\n1,Dune,Herbert\n2,Kindred,Butler\n3,Emma,Austen\n4,,Nobody\n"
	if err := os.WriteFile(filepath.Join(base, "data.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, base
}

type basePagesResult struct {
	OK     bool `json:"ok"`
	DryRun bool `json:"dryRun"`
	Pages  []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Page  string `json:"page"`
	} `json:"pages"`
	Existing int `json:"existing"`
	Untitled int `json:"untitled"`
}

func runBasePages(t *testing.T, root string, flags ...string) basePagesResult {
	t.Helper()
	args := append([]string{"base", "pages", "Books", "--json", "--vault", root}, flags...)
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	var result basePagesResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("zn %v: %v\n%s", args, err, out)
	}
	return result
}

func mdFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func TestBasePagesCreatesAPageForEveryTitledRecord(t *testing.T) {
	root, base := basePagesVault(t)
	result := runBasePages(t, root)
	titles := []string{}
	for _, p := range result.Pages {
		titles = append(titles, p.Title)
	}
	if !result.OK || result.DryRun || !reflect.DeepEqual(titles, []string{"Dune", "Kindred", "Emma"}) || result.Untitled != 1 || result.Existing != 0 {
		t.Fatalf("result: %+v", result)
	}
	if got := mdFiles(t, base); !reflect.DeepEqual(got, []string{"Dune.md", "Emma.md", "Kindred.md"}) {
		t.Fatalf("pages on disk: %v", got)
	}
	// The page the desktop's own "open" makes: properties as frontmatter, the title as its heading.
	page, err := os.ReadFile(filepath.Join(base, "Kindred.md"))
	if err != nil || string(page) != "---\nAuthor: Butler\n---\n# Kindred\n\n" {
		t.Fatalf("Kindred.md: %q %v", page, err)
	}
	// schema.json maps each row to its page, relative to the folder as the app keeps it.
	raw, err := os.ReadFile(filepath.Join(base, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Pages map[string]string `json:"pages"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Pages) != 3 || schema.Pages[result.Pages[1].ID] != "Kindred.md" {
		t.Fatalf("schema pages: %v", schema.Pages)
	}

	// A second run finds every page and writes nothing.
	again := runBasePages(t, root)
	if len(again.Pages) != 0 || again.Existing != 3 || again.Untitled != 1 {
		t.Fatalf("second run: %+v", again)
	}
	if got := mdFiles(t, base); len(got) != 3 {
		t.Fatalf("second run wrote pages: %v", got)
	}
}

func TestBasePagesDryRunWritesNothing(t *testing.T) {
	root, base := basePagesVault(t)
	result := runBasePages(t, root, "--dry-run")
	if !result.DryRun || len(result.Pages) != 3 || result.Pages[0].Page != "" {
		t.Fatalf("dry run: %+v", result)
	}
	if got := mdFiles(t, base); len(got) != 0 {
		t.Fatalf("dry run wrote pages: %v", got)
	}
}

func TestBasePagesRefusesALooseCSV(t *testing.T) {
	isolatedCLI(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inbox", "Books.csv"), []byte("id,Title\n1,Dune\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var code int
	captureOutput(t, func() { code = Main([]string{"base", "pages", "Books", "--vault", root}) })
	if code == 0 {
		t.Fatal("a loose .csv has no record pages; zn base pages should refuse it")
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "inbox")); len(entries) != 1 {
		t.Fatalf("refusal wrote files: %v", entries)
	}
}

// A workaround script wrote a page file per record but no schema.json entry:
// those files are linked as they are, never shadowed by a `Title 2.md`.
func TestBasePagesLinksPageFilesAScriptWrote(t *testing.T) {
	root, base := basePagesVault(t)
	notes := "---\nAuthor: Herbert\n---\n# Dune\n\nMy notes on Dune.\n"
	if err := os.WriteFile(filepath.Join(base, "Dune.md"), []byte(notes), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"base", "pages", "Books", "--json", "--vault", root}
	out := captureOutput(t, func() {
		if code := Main(args); code != 0 {
			t.Errorf("zn %v exited %d", args, code)
		}
	})
	var result struct {
		Pages  []struct{ Title, Page string } `json:"pages"`
		Linked []struct{ Title, Page string } `json:"linked"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(result.Linked) != 1 || result.Linked[0].Title != "Dune" || len(result.Pages) != 2 {
		t.Fatalf("result: %+v", result)
	}
	if got := mdFiles(t, base); !reflect.DeepEqual(got, []string{"Dune.md", "Emma.md", "Kindred.md"}) {
		t.Fatalf("pages on disk: %v", got)
	}
	if kept, _ := os.ReadFile(filepath.Join(base, "Dune.md")); string(kept) != notes {
		t.Fatalf("the script's page changed: %q", kept)
	}
	raw, _ := os.ReadFile(filepath.Join(base, "schema.json"))
	if !strings.Contains(string(raw), `"Dune.md"`) {
		t.Fatalf("schema does not map Dune.md: %s", raw)
	}
}
