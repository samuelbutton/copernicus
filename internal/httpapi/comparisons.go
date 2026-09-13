package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/comparison"
	"github.com/samuelbutton/copernicus/internal/store"
)

func comparisonHandler(db *store.Store, duckdb string) http.HandlerFunc {
	comparisons := make(chan struct{}, 1)
	return func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || catalog.ValidateID(q.Get("baseline")) != nil || catalog.ValidateID(q.Get("candidate")) != nil {
			failure(w, http.StatusBadRequest, "provide baseline and candidate request identifiers")
			return
		}
		for key, values := range q {
			if len(values) != 1 || (key != "baseline" && key != "candidate" && key != "after" && key != "limit" && key != "baseline_analysis" && key != "candidate_analysis" && key != "filter" && key != "sort") {
				failure(w, http.StatusBadRequest, "invalid query parameter")
				return
			}
		}
		after, limit := q.Get("after"), 50
		if q.Get("limit") != "" {
			limit, err = strconv.Atoi(q.Get("limit"))
		}
		if err != nil || limit < 1 || limit > 100 || len(after) > 64 {
			failure(w, http.StatusBadRequest, "invalid pagination")
			return
		}
		select {
		case comparisons <- struct{}{}:
			defer func() { <-comparisons }()
		default:
			w.Header().Set("Retry-After", "1")
			failure(w, http.StatusTooManyRequests, "comparison busy")
			return
		}
		baselineAnalysis, candidateAnalysis := q.Get("baseline_analysis"), q.Get("candidate_analysis")
		if baselineAnalysis == "" {
			baselineAnalysis = store.OriginalAnalysis
		}
		if candidateAnalysis == "" {
			candidateAnalysis = store.OriginalAnalysis
		}
		if catalog.ValidateID(baselineAnalysis) != nil || catalog.ValidateID(candidateAnalysis) != nil {
			failure(w, 400, "invalid analysis selection")
			return
		}
		filter, sort := q.Get("filter"), q.Get("sort")
		if filter == "" {
			filter = "all"
		}
		if sort == "" {
			sort = "id"
		}
		options := comparison.Options{Filter: filter, Sort: sort, After: after, Limit: limit}
		if err := options.Validate(); err != nil {
			failure(w, 400, "invalid comparison view")
			return
		}
		result, err := db.CompareView(r.Context(), q.Get("baseline"), q.Get("candidate"), baselineAnalysis, candidateAnalysis, duckdb, options)
		if err != nil {
			send(w, nil, err)
			return
		}

		send(w, struct {
			Report comparison.Report `json:"comparison"`
			Next   string            `json:"next_after,omitempty"`
		}{result, result.View.NextAfter}, nil)
	}
}
