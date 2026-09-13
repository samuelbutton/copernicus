package comparison

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func selections() (Selection, Selection) {
	key := Key{Scenario: "scenario", Run: "run", Simulator: "simulator", Source: "source", AnalysisName: "analysis"}
	b := Selection{RequestID: "baseline", Entries: map[Key][]Entry{key: {{ExecutionID: "b", JobID: "b", AnalysisHash: "same", State: "PASS", Result: &Result{Path: "results/b.json"}}}}}
	c := Selection{RequestID: "candidate", Entries: map[Key][]Entry{key: {{ExecutionID: "c", JobID: "c", AnalysisHash: "same", State: "FAIL", Result: &Result{Path: "results/c.json"}}}}}
	return b, c
}

func TestPairingAndDenominators(t *testing.T) {
	for _, kind := range []string{"COMPARED", "INCOMPARABLE", "INCOMPLETE", "ERROR", "ADDED", "REMOVED", "AMBIGUOUS"} {
		t.Run(kind, func(t *testing.T) {
			b, c := selections()
			var key Key
			for k := range b.Entries {
				key = k
			}
			switch kind {
			case "INCOMPARABLE":
				c.Entries[key][0].AnalysisHash = "changed"
			case "INCOMPLETE":
				c.Entries[key][0].Result = nil
				c.Entries[key][0].State = "QUEUED"
			case "ERROR":
				c.Entries[key][0].State = "ERROR"
			case "ADDED":
				delete(b.Entries, key)
			case "REMOVED":
				delete(c.Entries, key)
			case "AMBIGUOUS":
				c.Entries[key] = append(c.Entries[key], c.Entries[key][0])
			}
			r := Plan(b, c)
			want := kind
			if kind == "AMBIGUOUS" {
				want = "INCOMPARABLE"
			}
			if len(r.Rows) != 1 || r.Rows[0].Kind != want || r.Counts.Rows != 1 {
				t.Fatalf("report=%+v", r)
			}
			if r.Counts.Compared+r.Counts.Incomparable+r.Counts.Incomplete+r.Counts.Errors+r.Counts.Added+r.Counts.Removed != 1 {
				t.Fatal("row denominator")
			}
			if kind != "COMPARED" && (r.Rows[0].StatusChanged != nil || len(r.Rows[0].Metrics) != 0) {
				t.Fatal("uncomparable row has deltas")
			}
		})
	}
	b, c := selections()
	var key Key
	for k := range c.Entries {
		key = k
	}
	entries := c.Entries[key]
	delete(c.Entries, key)
	key.Seed++
	c.Entries[key] = entries
	r := Plan(b, c)
	if r.Counts.Added != 1 || r.Counts.Removed != 1 {
		t.Fatal("different seed was paired")
	}
}

func TestDuckDBDeltasAndIsolation(t *testing.T) {
	data, err := os.ReadFile("../../compatibility/contract/v1/examples/valid/results/run-1.json")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, id := range []string{"b", "c"} {
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		value["job_id"] = id
		m := value["metrics"].(map[string]any)
		collision := m["collision_count"].(map[string]any)
		collision["value"] = 0
		collision["pass"] = true
		if id == "c" {
			collision["value"] = 1
			collision["pass"] = false
		}
		gap := m["minimum_obstacle_gap"].(map[string]any)
		gap["value"] = nil
		gap["pass"] = nil
		files[id], err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	b, c := selections()
	plan := Plan(b, c)
	// Exercise spaces and quotes in the temporary directory, without SQL syntax injection.
	parent := filepath.Join(t.TempDir(), "quoted ' directory")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", parent)
	r, err := Evaluate(context.Background(), "../../bin/duckdb", plan, files)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts.Compared != 1 || r.Counts.MetricPairs != 3 || r.Counts.Regressions != 1 || r.Counts.Unchanged != 1 || r.Counts.UnavailableMetrics != 1 || r.Counts.AvailableMetrics != 2 || r.Counts.StatusChanges != 1 {
		t.Fatalf("counts=%+v", r.Counts)
	}
	m := r.Rows[0].Metrics[0]
	if m.Name != "collision_count" || m.Delta == nil || *m.Delta != 1 || m.BaselinePass == nil || !*m.BaselinePass || m.CandidatePass == nil || *m.CandidatePass {
		t.Fatalf("metric=%+v", m)
	}
	if len(plan.Rows[0].Metrics) != 0 {
		t.Fatal("query mutated input plan")
	}
	remaining, err := os.ReadDir(parent)
	if err != nil || len(remaining) != 0 {
		t.Fatal("query left temporary files")
	}
	_, err = Evaluate(context.Background(), "/missing/duckdb", plan, files)
	if err == nil {
		t.Fatal("missing engine accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Evaluate(ctx, "../../bin/duckdb", plan, files); err == nil {
		t.Fatal("ignored cancellation")
	}
	// Same selected bytes produce exact zero deltas for every available metric.
	plan = Plan(b, b)
	equal, err := Evaluate(context.Background(), "../../bin/duckdb", plan, files)
	if err != nil {
		t.Fatal(err)
	}
	if equal.Counts.Regressions != 0 || equal.Counts.Improvements != 0 || equal.Counts.Unchanged != 2 {
		t.Fatalf("equal=%+v", equal.Counts)
	}
	again, err := Evaluate(context.Background(), "../../bin/duckdb", plan, files)
	if err != nil || !reflect.DeepEqual(equal, again) {
		t.Fatal("query is not stable")
	}
}

func TestMetricVersionAndUnitConflictsSuppressAllDeltas(t *testing.T) {
	for _, field := range []string{"unit", "version"} {
		t.Run(field, func(t *testing.T) {
			b, c := selections()
			files := map[string][]byte{}
			for _, id := range []string{"b", "c"} {
				data, err := os.ReadFile("../../compatibility/contract/v1/examples/valid/results/run-1.json")
				if err != nil {
					t.Fatal(err)
				}
				var value map[string]any
				if err := json.Unmarshal(data, &value); err != nil {
					t.Fatal(err)
				}
				value["job_id"] = id
				if id == "c" {
					m := value["metrics"].(map[string]any)["minimum_obstacle_gap"].(map[string]any)
					if field == "unit" {
						m[field] = "m"
					} else {
						m[field] = 2
					}
				}
				files[id], err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			r, err := Evaluate(context.Background(), "../../bin/duckdb", Plan(b, c), files)
			if err != nil {
				t.Fatal(err)
			}
			if r.Counts.Incomparable != 1 || r.Counts.Compared != 0 || r.Counts.MetricPairs != 0 || r.Rows[0].StatusChanged != nil || len(r.Rows[0].Metrics) != 0 {
				t.Fatalf("conflict=%+v", r)
			}
		})
	}
}
