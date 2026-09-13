package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

func TestComparisonUsesExactSelectionsAndReadableResults(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	root := t.TempDir()
	b := requestInput()
	c := b
	c.ID = "candidate-request"
	c.ControllerID = "candidate"
	for _, input := range []request.Submission{b, c} {
		if _, err := s.CreateRequest(ctx, input, requestSource(t)); err != nil {
			t.Fatal(err)
		}
	}
	bpath, _ := mappedOutcomeForRequest(t, s, root, b.ID)
	cpath, _ := mappedOutcomeForRequest(t, s, root, c.ID)
	value := document(t, filepath.Join(root, bpath))
	value["status"] = "PASS"
	metrics := value["metrics"].(map[string]any)
	for _, name := range []string{"collision_count", "goal_progress"} {
		m := metrics[name].(map[string]any)
		m["pass"] = true
		if name == "collision_count" {
			m["value"] = 0
		} else {
			m["value"] = 1000000
		}
	}
	writeDocument(t, filepath.Join(root, bpath), value)
	if _, err := s.ImportResults(ctx, root, []string{bpath, cpath}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Compare(ctx, b.ID, c.ID, "../../bin/duckdb")
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts.Rows != 3 || r.Counts.Compared != 1 || r.Counts.Incomplete != 2 || r.Baseline.Total != 3 || r.Baseline.Completed != 1 || r.Baseline.Passed != 1 || r.Candidate.Completed != 1 || r.Candidate.Failed != 1 || r.Counts.StatusChanges != 1 {
		t.Fatalf("comparison=%+v", r)
	}
	if r.Counts.MetricPairs != 3 || r.Counts.AvailableMetrics+r.Counts.UnavailableMetrics != 3 || r.Counts.Regressions+r.Counts.Improvements+r.Counts.Unchanged != r.Counts.AvailableMetrics {
		t.Fatal("metric denominators")
	}
	again, err := s.Compare(ctx, b.ID, c.ID, "../../bin/duckdb")
	if err != nil || !reflect.DeepEqual(r, again) {
		t.Fatal("comparison changed without input changes")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("comparison wrote database")
	}
	if err := os.Remove(filepath.Join(root, bpath)); err != nil {
		t.Fatal(err)
	}
	r, err = s.Compare(ctx, b.ID, c.ID, "../../bin/duckdb")
	if err != nil || r.Counts.Compared != 0 || r.Counts.Incomplete != 3 || r.Baseline.Passed != 0 || r.Baseline.Completed != 0 {
		t.Fatalf("missing result=%+v %v", r, err)
	}
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	changed := b
	changed.ID = "changed-membership"
	if _, err := s.CreateRequest(ctx, changed, requestSource(t)); err != nil {
		t.Fatal(err)
	}
	r, err = s.Compare(ctx, b.ID, changed.ID, "../../bin/duckdb")
	if err != nil || r.Counts.Removed != 1 || r.Counts.Incomplete != 2 || r.Baseline.Total != 3 || r.Candidate.Total != 2 {
		t.Fatalf("membership=%+v %v", r, err)
	}
	if _, err := s.Compare(ctx, b.ID, "absent", "../../bin/duckdb"); err == nil {
		t.Fatal("missing request accepted")
	}
}
