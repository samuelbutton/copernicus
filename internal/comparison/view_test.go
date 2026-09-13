package comparison

import (
	"bytes"
	"testing"
)

func TestViewIdentityAndPagination(t *testing.T) {
	report := Report{QueryVersion: QueryVersion, Rows: []Row{{ID: "a", Kind: "INCOMPLETE"}, {ID: "b", Kind: "ERROR"}, {ID: "c", Kind: "COMPARED", Metrics: []Metric{{Change: "REGRESSION"}}}}}
	report.recount()
	options := Options{Filter: "all", Sort: "id-desc", Limit: 2}
	first := Select(report, options)
	if first.Rows[0].ID != "c" || first.Rows[1].ID != "b" || first.View.NextAfter != "b" || first.View.MatchedRows != 3 || first.Counts.Rows != 3 {
		t.Fatal("descending page")
	}
	options.After = first.View.NextAfter
	second := Select(report, options)
	if len(second.Rows) != 1 || second.Rows[0].ID != "a" || second.View.NextAfter != "" {
		t.Fatal("page skipped or duplicated rows")
	}
	options.Filter = "regressions"
	options.After = ""
	filtered := Select(report, options)
	if len(filtered.Rows) != 1 || filtered.View.MatchedRows != 1 || filtered.Counts.Rows != 3 {
		t.Fatal("filter changed full counts")
	}
	before, err := CacheInputs(report, options)
	if err != nil {
		t.Fatal(err)
	}
	report.QueryVersion++
	after, err := CacheInputs(report, options)
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("query version omitted from key")
	}
	for _, invalid := range []Options{{Filter: "bad", Sort: "id", Limit: 1}, {Filter: "all", Sort: "bad", Limit: 1}, {Filter: "all", Sort: "id", Limit: 0}, {Filter: "all", Sort: "id", Limit: 1, After: "bad"}} {
		if invalid.Validate() == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
