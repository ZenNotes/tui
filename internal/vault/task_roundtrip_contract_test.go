package vault

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestSharedTaskRoundtripContract(t *testing.T) {
	data, err := os.ReadFile("testdata/task-roundtrip.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion int `json:"schemaVersion"`
		Cases         []struct {
			ID   string `json:"id"`
			Note struct {
				Path   string     `json:"path"`
				Title  string     `json:"title"`
				Folder NoteFolder `json:"folder"`
			} `json:"note"`
			Body              string         `json:"body"`
			ExpectedBody      string         `json:"expectedBody"`
			Due               string         `json:"due"`
			LocalNow          []int          `json:"localNow"`
			TaskIndex         int            `json:"taskIndex"`
			ExpectedBefore    map[string]any `json:"expectedBefore"`
			ExpectedAfter     map[string]any `json:"expectedAfter"`
			ExpectedTaskCount int            `json:"expectedTaskCount"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 || len(fixture.Cases) == 0 {
		t.Fatal("unsupported or empty task contract fixture")
	}
	var provenance struct{ Sha256 string }
	source, err := os.ReadFile("testdata/task-roundtrip.json.source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(source, &provenance); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != provenance.Sha256 {
		t.Fatal("shared fixture checksum differs")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			v := newTestVault(t)
			for _, zone := range []string{"America/Los_Angeles", "Pacific/Auckland"} {
				location, err := time.LoadLocation(zone)
				if err != nil {
					t.Fatal(err)
				}
				due := tc.Due
				if len(tc.LocalNow) == 5 {
					n := tc.LocalNow
					due = TodayISO(time.Date(n[0], time.Month(n[1]), n[2], n[3], n[4], 0, 0, location))
				}
				if actual := SetTaskDue(tc.Body, tc.TaskIndex, due); actual != tc.ExpectedBody {
					t.Fatalf("%s mutation changed unexpected bytes: got %q, want %q", zone, actual, tc.ExpectedBody)
				}
			}
			var originalID string
			// The client transforms Markdown; Go stores those exact bytes and
			// parses the resulting task state for the next client read.
			for index, phase := range []struct {
				body string
				want map[string]any
			}{{tc.Body, tc.ExpectedBefore}, {tc.ExpectedBody, tc.ExpectedAfter}} {
				if _, err := v.WriteNote(tc.Note.Path, phase.body); err != nil {
					t.Fatal(err)
				}
				note, err := v.ReadNote(tc.Note.Path)
				if err != nil {
					t.Fatal(err)
				}
				if note.Body != phase.body {
					t.Fatal("storage changed Markdown bytes")
				}
				tasks := ParseTasks(tc.Note.Path, tc.Note.Title, tc.Note.Folder, note.Body, ParseTasksOptions{Dialect: DialectApp})
				if len(tasks) != tc.ExpectedTaskCount || tc.TaskIndex < 0 || tc.TaskIndex >= len(tasks) {
					t.Fatalf("got %d tasks, want %d with index %d", len(tasks), tc.ExpectedTaskCount, tc.TaskIndex)
				}
				task := tasks[tc.TaskIndex]
				if index == 0 {
					originalID = task.ID
				} else if task.ID != originalID {
					t.Fatal("task identity changed after editing")
				}
				encoded, err := json.Marshal(task)
				if err != nil {
					t.Fatal(err)
				}
				var actual map[string]any
				if err := json.Unmarshal(encoded, &actual); err != nil {
					t.Fatal(err)
				}
				for field, want := range phase.want {
					if !reflect.DeepEqual(actual[field], want) {
						t.Errorf("phase %d field %s: got %#v, want %#v", index, field, actual[field], want)
					}
				}
			}
		})
	}
}
