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
			if len(values) != 1 || (key != "baseline" && key != "candidate" && key != "after" && key != "limit") {
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
		result, err := db.Compare(r.Context(), q.Get("baseline"), q.Get("candidate"), duckdb)
		if err != nil {
			send(w, nil, err)
			return
		}
		start := 0
		for start < len(result.Rows) && result.Rows[start].ID <= after {
			start++
		}
		end := min(start+limit, len(result.Rows))
		next := ""
		if end < len(result.Rows) {
			next = result.Rows[end-1].ID
		}
		result.Rows = result.Rows[start:end]
		send(w, struct {
			Report comparison.Report `json:"comparison"`
			Next   string            `json:"next_after,omitempty"`
		}{result, next}, nil)
	}
}
