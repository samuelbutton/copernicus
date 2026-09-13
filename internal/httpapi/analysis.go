package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
	"github.com/samuelbutton/copernicus/internal/store"
)

func analysisQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "invalid analysis query")
		return "", false
	}
	for key, values := range query {
		if key != "analysis" || len(values) != 1 {
			failure(w, 400, "invalid analysis query")
			return "", false
		}
	}
	selection := query.Get("analysis")
	if selection == "" {
		selection = store.OriginalAnalysis
	}
	if catalog.ValidateID(selection) != nil {
		failure(w, 400, "invalid analysis identifier")
		return "", false
	}
	return selection, true
}
func analysisHandler(db *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validID(w, r) {
			return
		}
		if r.URL.RawQuery != "" {
			failure(w, 400, "analysis selection accepts no query")
			return
		}
		if r.Method == http.MethodGet {
			result, err := db.Analyses(r.Context(), r.PathValue("id"))
			send(w, result, err)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 1025))
		if err != nil || len(data) > 1024 {
			failure(w, 400, "invalid analysis submission")
			return
		}
		// The public JSON decoder rejects duplicate fields, excess nesting and
		// non-integer numbers; the exact field map rejects aliases and unknown keys.
		if _, err := compatibility.ContentHash(data); err != nil {
			failure(w, 400, "invalid analysis submission")
			return
		}
		var fields map[string]json.RawMessage
		var templateID string
		if json.Unmarshal(data, &fields) != nil || len(fields) != 1 || fields["template_id"] == nil || json.Unmarshal(fields["template_id"], &templateID) != nil || catalog.ValidateID(templateID) != nil {
			failure(w, 400, "provide a scoring template identifier")
			return
		}
		result, err := db.CreateAnalysis(r.Context(), r.PathValue("id"), templateID)
		if errors.Is(err, request.ErrInvalidSubmission) {
			failure(w, 400, "new scores need a supported template and validated original recordings for every test")
			return
		}
		result.Jobs = nil
		send(w, result, err)
	}
}
