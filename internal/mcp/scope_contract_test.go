package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The shared scoped-access contract (ZenNotes/tui#21): vault trees, scopes,
// tool calls and the bytes a client must get back. The desktop's bundled MCP
// server runs the same cases in apps/desktop/src/mcp/scope-contract.test.ts
// (ZenNotes/zennotes), whose packages/bridge-contract/fixtures/mcp-scope.json
// this is a copy of; see docs/shared-contracts.md. When a case fails, settle
// which behavior is intended before changing anything; never copy this
// engine's output into the fixture.

type scopeContractFlags struct {
	Allow    []string `json:"allow"`
	Deny     []string `json:"deny"`
	Access   string   `json:"access"`
	ReadOnly bool     `json:"readOnly"`
}

type scopeContractExpect struct {
	Error      *string         `json:"error"`
	OK         bool            `json:"ok"`
	NotOffered bool            `json:"notOffered"`
	Paths      *[]string       `json:"paths"`
	Folders    *[]string       `json:"folders"`
	Match      json.RawMessage `json:"match"`
	Contains   []string        `json:"contains"`
	Excludes   []string        `json:"excludes"`
	AbsentKeys []string        `json:"absentKeys"`
}

type scopeContract struct {
	SchemaVersion int `json:"schemaVersion"`
	Vaults        map[string]struct {
		Files    map[string]string `json:"files"`
		Symlinks map[string]string `json:"symlinks"`
		Outside  map[string]string `json:"outside"`
	} `json:"vaults"`
	FlagCases []struct {
		ID    string             `json:"id"`
		Flags scopeContractFlags `json:"flags"`
		Scope *struct {
			Access string   `json:"access"`
			Allow  []string `json:"allow"`
			Deny   []string `json:"deny"`
		} `json:"scope"`
		Error string `json:"error"`
	} `json:"flagCases"`
	Cases []struct {
		ID    string              `json:"id"`
		Vault string              `json:"vault"`
		Flags *scopeContractFlags `json:"flags"`
		Tools *struct {
			Count       *int                      `json:"count"`
			Exactly     []string                  `json:"exactly"`
			Include     []string                  `json:"include"`
			Absent      []string                  `json:"absent"`
			Annotations map[string]map[string]any `json:"annotations"`
		} `json:"tools"`
		Calls []struct {
			Tool   string              `json:"tool"`
			Args   map[string]any      `json:"args"`
			Expect scopeContractExpect `json:"expect"`
		} `json:"calls"`
		Disk *struct {
			Exists []string          `json:"exists"`
			Absent []string          `json:"absent"`
			Equals map[string]string `json:"equals"`
		} `json:"disk"`
	} `json:"cases"`
}

func loadScopeContract(t *testing.T) scopeContract {
	t.Helper()
	data, err := os.ReadFile("testdata/mcp-scope.json")
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct{ Sha256 string }
	source, err := os.ReadFile("testdata/mcp-scope.json.source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(source, &provenance); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != provenance.Sha256 {
		t.Fatal("shared fixture checksum differs")
	}
	var c scopeContract
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.SchemaVersion != 1 || len(c.Cases) == 0 {
		t.Fatal("unsupported or empty scope contract fixture")
	}
	return c
}

func TestSharedScopeFlagContract(t *testing.T) {
	for _, tc := range loadScopeContract(t).FlagCases {
		t.Run(tc.ID, func(t *testing.T) {
			s, err := NewScope(tc.Flags.Access, tc.Flags.ReadOnly, tc.Flags.Allow, tc.Flags.Deny)
			if tc.Error != "" {
				if err == nil || err.Error() != tc.Error {
					t.Fatalf("got %v, want %q", err, tc.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.Access.String() != tc.Scope.Access || !reflect.DeepEqual(s.Allow, tc.Scope.Allow) || !reflect.DeepEqual(s.Deny, tc.Scope.Deny) {
				t.Fatalf("got %+v, want %+v", s, *tc.Scope)
			}
		})
	}
}

// contractCall is a tool call that may fail either way a client sees it: a
// tool result marked as an error, or a protocol error for a tool the server
// does not offer.
func contractCall(cs *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), true
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

// matchContract checks every key the fixture names; arrays match element for
// element.
func matchContract(t *testing.T, actual, expected any, where string) {
	t.Helper()
	switch want := expected.(type) {
	case []any:
		got, ok := actual.([]any)
		if !ok || len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", where, actual, expected)
			return
		}
		for i := range want {
			matchContract(t, got[i], want[i], fmt.Sprintf("%s[%d]", where, i))
		}
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			t.Errorf("%s: got %v, want an object", where, actual)
			return
		}
		for key, value := range want {
			matchContract(t, got[key], value, where+"."+key)
		}
	default:
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("%s: got %v, want %v", where, actual, expected)
		}
	}
}

// vaultSpellings is a needle with $VAULT replaced by the vault root as a
// result can spell it: as is in an error message, and JSON-escaped inside a
// result object, where a Windows root's backslashes are doubled.
func vaultSpellings(needle, root string) []string {
	escaped, _ := json.Marshal(root)
	return []string{
		strings.ReplaceAll(needle, "$VAULT", root),
		strings.ReplaceAll(needle, "$VAULT", string(escaped[1:len(escaped)-1])),
	}
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func TestSharedScopeContract(t *testing.T) {
	contract := loadScopeContract(t)
	for _, tc := range contract.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			seed := contract.Vaults[tc.Vault]
			base := t.TempDir()
			root := filepath.Join(base, "vault")
			write := func(dir string, files map[string]string) {
				for rel, body := range files {
					abs := filepath.Join(dir, filepath.FromSlash(rel))
					if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			write(root, seed.Files)
			write(filepath.Join(base, "outside"), seed.Outside)
			for rel, target := range seed.Symlinks {
				if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Fatal(err)
				}
			}

			var scope *Scope
			if tc.Flags != nil {
				s, err := NewScope(tc.Flags.Access, tc.Flags.ReadOnly, tc.Flags.Allow, tc.Flags.Deny)
				if err != nil {
					t.Fatal(err)
				}
				scope = &s
			}
			cs := runScoped(t, root, scope)

			if tc.Tools != nil {
				listed := listedTools(t, cs)
				names := []string{}
				for name := range listed {
					names = append(names, name)
				}
				sort.Strings(names)
				if tc.Tools.Count != nil && len(names) != *tc.Tools.Count {
					t.Errorf("lists %d tools, want %d: %v", len(names), *tc.Tools.Count, names)
				}
				if tc.Tools.Exactly != nil && !reflect.DeepEqual(names, sortedCopy(tc.Tools.Exactly)) {
					t.Errorf("lists %v, want %v", names, sortedCopy(tc.Tools.Exactly))
				}
				for _, name := range tc.Tools.Include {
					if _, ok := listed[name]; !ok {
						t.Errorf("%s is not listed", name)
					}
				}
				for _, name := range tc.Tools.Absent {
					if _, ok := listed[name]; ok {
						t.Errorf("%s is listed", name)
					}
				}
				for name, want := range tc.Tools.Annotations {
					tool, ok := listed[name]
					if !ok {
						t.Errorf("%s is not listed", name)
						continue
					}
					raw, _ := json.Marshal(tool.Annotations)
					got := map[string]any{}
					_ = json.Unmarshal(raw, &got)
					if !reflect.DeepEqual(got, want) {
						t.Errorf("%s annotations: got %v, want %v", name, got, want)
					}
				}
			}

			for _, call := range tc.Calls {
				argsJSON, _ := json.Marshal(call.Args)
				where := call.Tool + " " + string(argsJSON)
				text, failed := contractCall(cs, call.Tool, call.Args)
				e := call.Expect
				if e.NotOffered {
					if !failed {
						t.Errorf("%s: should not be offered, got %s", where, text)
					}
					continue
				}
				if e.Error != nil && (!failed || text != "Error: "+*e.Error) {
					t.Errorf("%s:\n got %q\nwant %q", where, text, "Error: "+*e.Error)
				}
				if (e.OK || e.Paths != nil || e.Folders != nil || e.Match != nil || e.AbsentKeys != nil) && failed {
					t.Errorf("%s failed: %s", where, text)
					continue
				}
				if e.Paths != nil {
					var notes []struct {
						Path string `json:"path"`
					}
					if err := json.Unmarshal([]byte(text), &notes); err != nil {
						t.Errorf("%s: %v\n%s", where, err, text)
						continue
					}
					got := []string{}
					for _, n := range notes {
						got = append(got, n.Path)
					}
					if got = sortedCopy(got); !reflect.DeepEqual(got, sortedCopy(*e.Paths)) {
						t.Errorf("%s:\n got %v\nwant %v", where, got, sortedCopy(*e.Paths))
					}
				}
				if e.Folders != nil {
					var folders []struct {
						Folder  string `json:"folder"`
						Subpath string `json:"subpath"`
					}
					if err := json.Unmarshal([]byte(text), &folders); err != nil {
						t.Errorf("%s: %v\n%s", where, err, text)
						continue
					}
					got := []string{}
					for _, f := range folders {
						got = append(got, f.Folder+":"+f.Subpath)
					}
					if got = sortedCopy(got); !reflect.DeepEqual(got, sortedCopy(*e.Folders)) {
						t.Errorf("%s:\n got %v\nwant %v", where, got, sortedCopy(*e.Folders))
					}
				}
				if e.Match != nil {
					var actual, expected any
					if err := json.Unmarshal([]byte(text), &actual); err != nil {
						t.Errorf("%s: %v\n%s", where, err, text)
						continue
					}
					_ = json.Unmarshal(e.Match, &expected)
					matchContract(t, actual, expected, where)
				}
				if e.AbsentKeys != nil {
					object := map[string]any{}
					_ = json.Unmarshal([]byte(text), &object)
					for _, key := range e.AbsentKeys {
						if _, ok := object[key]; ok {
							t.Errorf("%s: has %q", where, key)
						}
					}
				}
				for _, needle := range e.Contains {
					if !containsAny(text, vaultSpellings(needle, root)) {
						t.Errorf("%s: lacks %q in %s", where, needle, text)
					}
				}
				for _, needle := range e.Excludes {
					if containsAny(text, vaultSpellings(needle, root)) {
						t.Errorf("%s: has %q in %s", where, needle, text)
					}
				}
			}

			if tc.Disk != nil {
				at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
				for _, rel := range tc.Disk.Exists {
					if _, err := os.Lstat(at(rel)); err != nil {
						t.Errorf("%s should exist: %v", rel, err)
					}
				}
				for _, rel := range tc.Disk.Absent {
					if _, err := os.Lstat(at(rel)); err == nil {
						t.Errorf("%s should be absent", rel)
					}
				}
				for rel, want := range tc.Disk.Equals {
					got, err := os.ReadFile(at(rel))
					if err != nil || string(got) != want {
						t.Errorf("%s: got %q (%v), want %q", rel, got, err, want)
					}
				}
			}
		})
	}
}
