package cli

import (
	"context"

	"encoding/json"
	"fmt"
	"github.com/ZenNotes/tui/internal/backend"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func doctorVault(t *testing.T, notes map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{".zennotes/vault.json": `{"primaryNotesLocation":"root"}`}
	for rel, body := range notes {
		files[rel] = body
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runDoctor(t *testing.T, args ...string) (int, string) {
	t.Helper()
	isolatedCLI(t)
	code := -1
	out := captureOutput(t, func() { code = Main(append([]string{"doctor"}, args...)) })
	return code, out
}

func warnChecks(checks []diagnosticCheck) []diagnosticCheck {
	out := []diagnosticCheck{}
	for _, c := range checks {
		if c.Warn {
			out = append(out, c)
		}
	}
	return out
}

func doctorJSON(t *testing.T, out string) (bool, []diagnosticCheck) {
	t.Helper()
	var report struct {
		OK     bool              `json:"ok"`
		Checks []diagnosticCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor json: %v\n%s", err, out)
	}
	return report.OK, report.Checks
}

func TestDoctorWarnsAboutTaskValuesNoAppReads(t *testing.T) {
	root := doctorVault(t, map[string]string{
		"Notes/A.md": "- [ ] a @repeat:fortnightly\n- [ ] b due:soon\n- [ ] c ^id\n- [ ] d ^id\n",
	})
	code, out := runDoctor(t, "--vault", root)
	if code != 0 {
		t.Fatalf("a warning changed the exit status to %d:\n%s", code, out)
	}
	for _, want := range []string{
		"WARN  ",
		"      Notes/A.md:1  @repeat:fortnightly  no app reads this rule, so the task does not repeat",
		"      Notes/A.md:2  due:soon  no app reads this as a date, so the task has no due date",
		"      Notes/A.md:3,4  ^id  links go to the first line with this id",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("a warning shows as a failure:\n%s", out)
	}
}

func TestDoctorTaskValueWarningStopsAtTenLines(t *testing.T) {
	var body strings.Builder
	for i := 1; i <= 13; i++ {
		fmt.Fprintf(&body, "- [ ] t%d due:bad\n", i)
	}
	root := doctorVault(t, map[string]string{"Notes/A.md": body.String()})
	code, out := runDoctor(t, "--vault", root)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if n := strings.Count(out, "  due:bad  "); n != 10 {
		t.Errorf("%d problem lines, want 10:\n%s", n, out)
	}
	if !strings.Contains(out, "      +3 more") {
		t.Errorf("no +3 more line:\n%s", out)
	}
}

func TestDoctorJSONMarksTaskValueWarning(t *testing.T) {
	root := doctorVault(t, map[string]string{"Notes/A.md": "- [ ] a due:soon\n"})
	code, out := runDoctor(t, "--vault", root, "--json")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	ok, checks := doctorJSON(t, out)
	warns := warnChecks(checks)
	if !ok || len(warns) != 1 {
		t.Fatalf("ok %v, warnings %+v", ok, warns)
	}
	if !warns[0].OK || len(warns[0].Hint) != 1 || !strings.HasPrefix(warns[0].Hint[0], "Notes/A.md:1  due:soon  ") {
		t.Errorf("warning %+v", warns[0])
	}
}

func TestDoctorShowsNoTaskValueWarningWithoutProblems(t *testing.T) {
	root := doctorVault(t, map[string]string{
		"Notes/A.md": "- [ ] a @repeat:weekly due:2025-01-02 ^id\n```\n- [ ] x due:soon\n```\n",
	})
	code, out := runDoctor(t, "--vault", root)
	if code != 0 || strings.Contains(out, "WARN") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	_, jsonOut := runDoctor(t, "--vault", root, "--json")
	if _, checks := doctorJSON(t, jsonOut); len(warnChecks(checks)) != 0 {
		t.Fatalf("warnings %+v", checks)
	}
}

func TestDoctorSkipsTaskValuesOfRemoteVault(t *testing.T) {
	var mu sync.Mutex
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	_, out := runDoctor(t, "--server", server.URL, "--token", "t", "--json")
	_, checks := doctorJSON(t, out)
	if len(warnChecks(checks)) != 0 {
		t.Fatalf("warnings %+v", checks)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range paths {
		if strings.Contains(p, "/notes") || strings.Contains(p, "/tasks") {
			t.Errorf("doctor read %s from a remote vault", p)
		}
	}
}

func TestDoctorShowsNoTaskValueWarningForAMissingVault(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	code, out := runDoctor(t, "--vault", missing, "--json")
	if code == 0 {
		t.Fatalf("a missing vault gave exit 0:\n%s", out)
	}
	ok, checks := doctorJSON(t, strings.SplitN(out, "\n{\"error\"", 2)[0])
	if ok || len(warnChecks(checks)) != 0 {
		t.Fatalf("ok %v, checks %+v", ok, checks)
	}
	failed := false
	for _, c := range checks {
		if !c.OK {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("no failed check:\n%s", out)
	}
}

func TestDoctorTaskValueCheckThatDoesNotFinishIsAWarning(t *testing.T) {
	root := doctorVault(t, map[string]string{"Notes/A.md": "- [ ] a due:soon\n"})
	b, err := OpenBackend(backend.Target{Kind: backend.KindLocal, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checks := taskValueChecks(ctx, b.(*backend.Local).Vault())
	found := false
	for _, c := range checks {
		if c.Name == "task values" {
			found = true
			if !c.OK || !c.Warn || !strings.Contains(c.Message, "did not finish") {
				t.Errorf("check %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("checks %+v", checks)
	}
}
