package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/samuelbutton/copernicus/internal/request"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/samuelbutton/copernicus/internal/comparison"
)

func TestSavedComparisonRevalidatesEvidenceAndPreservesViews(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	_, root, result := analysisFixture(t, s)
	options := comparison.Options{Filter: "all", Sort: "id", Limit: 1}
	read := func(db *Store, engine string, o comparison.Options) comparison.Report {
		t.Helper()
		r, err := db.CompareView(ctx, requestInput().ID, requestInput().ID, OriginalAnalysis, OriginalAnalysis, engine, o)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := read(s, "../../bin/duckdb", options)
	if first.View.CacheState != "saved" || first.View.CacheKey == "" || first.Counts.Compared != 1 {
		t.Fatal("terminal result was not saved")
	}
	hit := read(openStore(t, path, false), "/missing-query-engine", options)
	if hit.View.CacheState != "hit" || hit.View.CacheKey != first.View.CacheKey {
		t.Fatal("read-only cache miss")
	}
	for _, change := range []func(*comparison.Options){func(o *comparison.Options) { o.Filter = "regressions" }, func(o *comparison.Options) { o.Sort = "id-desc" }, func(o *comparison.Options) { o.Limit = 2 }, func(o *comparison.Options) { o.After = first.Rows[0].ID }} {
		changed := options
		change(&changed)
		r := read(s, "../../bin/duckdb", changed)
		if r.View.CacheState != "saved" || r.View.CacheKey == first.View.CacheKey || r.Counts.Rows != 1 || r.Baseline.Total != 1 {
			t.Fatal("view omitted from identity or denominator")
		}
	}
	bytes, err := os.ReadFile(filepath.Join(root, result))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, result)); err != nil {
		t.Fatal(err)
	}
	partial := read(s, "/missing-query-engine", options)
	if partial.View.CacheState != "bypass_partial" || partial.View.CacheKey != "" || partial.Counts.Incomplete != 1 || partial.Baseline.Completed != 0 {
		t.Fatal("missing evidence reused saved scores")
	}
	if err := os.WriteFile(filepath.Join(root, result), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	restored := read(s, "/missing-query-engine", options)
	if restored.View.CacheState != "hit" {
		t.Fatal("restored evidence did not match cache")
	}
	if _, err := s.RebuildResults(ctx, root); err != nil {
		t.Fatal(err)
	}
	if read(s, "/missing-query-engine", options).View.CacheState != "hit" {
		t.Fatal("rebuild changed exact inputs")
	}
	if _, err := s.db.Exec("UPDATE saved_comparisons SET content='{}'"); err == nil {
		t.Fatal("saved comparison was mutable")
	}
	if _, err := s.CreateAnalysis(ctx, requestInput().ID, "edge-v2"); err != nil {
		t.Fatal(err)
	}
	next, err := s.CompareView(ctx, requestInput().ID, requestInput().ID, OriginalAnalysis, "edge-v2", "/missing-query-engine", options)
	if err != nil || next.View.CacheState != "bypass_partial" || next.Counts.Incomparable != 1 {
		t.Fatal("new analysis reused original cache", err)
	}
}
func TestPartialComparisonNeverSavesEvenWhenFiltered(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, _ := mappedOutcome(t, s, root)
	if _, err := s.ImportResults(ctx, root, []string{result}); err != nil {
		t.Fatal(err)
	}
	r, err := s.CompareView(ctx, requestInput().ID, requestInput().ID, OriginalAnalysis, OriginalAnalysis, "../../bin/duckdb", comparison.Options{Filter: "regressions", Sort: "id", Limit: 1})
	if err != nil || r.View.CacheState != "bypass_partial" || len(r.Rows) != 0 || r.Counts.Incomplete != 2 || r.Baseline.Total != 3 {
		t.Fatal("filtered partial lost denominator", err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM saved_comparisons").Scan(&n); err != nil || n != 0 {
		t.Fatal("partial view was cached", err)
	}
}

func TestSavedComparisonEvictionPreservesRequestState(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	record, _, _ := analysisFixture(t, s)
	before, err := s.Budgets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := ""
	for i := range 129 {
		inputs := []byte(fmt.Sprintf(`{"view":%d}`, i))
		key := request.Hash(inputs)
		if i == 0 {
			first = key
		}
		if err := s.saveComparison(ctx, key, inputs, comparison.Report{}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM saved_comparisons").Scan(&count); err != nil || count != 128 {
		t.Fatal("cache count not bounded", err)
	}
	if _, err := s.savedComparison(ctx, first, []byte(`{"view":0}`)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("oldest page retained", err)
	}
	after, err := s.Budgets(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("eviction changed reservations", err)
	}
	got, err := s.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(record, got) {
		t.Fatal("eviction changed request", err)
	}
}
