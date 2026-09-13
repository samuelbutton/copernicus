// Package httpapi exposes bounded read-only views of local request state.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/store"
)

// Handler accepts only the actual loopback authority. It grants no cross-origin
// access, accepts no mutations, and limits concurrent expensive file validation.
func Handler(db *store.Store, authority, duckdb string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/comparisons", comparisonHandler(db, duckdb))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { send(w, map[string]string{"status": "live"}, nil) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		_, err := db.IndexStatus(r.Context())
		send(w, map[string]string{"status": "ready"}, err)
	})
	mux.HandleFunc("/api/requests", func(w http.ResponseWriter, r *http.Request) {
		after, limit, ok := page(w, r)
		if !ok {
			return
		}
		if len(after) > 64 {
			failure(w, http.StatusBadRequest, "invalid request cursor")
			return
		}
		result, err := db.Requests(r.Context(), after, limit)
		send(w, result, err)
	})
	mux.HandleFunc("/api/requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !validID(w, r) {
			return
		}
		result, err := db.Request(r.Context(), r.PathValue("id"))
		send(w, result, err)
	})
	mux.HandleFunc("/api/requests/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		if !validID(w, r) {
			return
		}
		result, err := db.Progress(r.Context(), r.PathValue("id"))
		send(w, result, err)
	})
	mux.HandleFunc("/api/results", func(w http.ResponseWriter, r *http.Request) {
		after, limit, ok := page(w, r)
		if !ok {
			return
		}
		result, err := db.Results(r.Context(), after, limit)
		send(w, result, err)
	})
	mux.HandleFunc("/api/index", func(w http.ResponseWriter, r *http.Request) {
		result, err := db.IndexStatus(r.Context())
		send(w, result, err)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { failure(w, http.StatusNotFound, "not found") })
	slots := make(chan struct{}, 8)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if r.Host != authority || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+authority || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			failure(w, http.StatusForbidden, "local origin required")
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			failure(w, http.StatusMethodNotAllowed, "read-only API")
			return
		}
		if r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
			failure(w, http.StatusBadRequest, "request bodies are not accepted")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			failure(w, http.StatusTooManyRequests, "local API busy")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validID(w http.ResponseWriter, r *http.Request) bool {
	if err := catalog.ValidateID(r.PathValue("id")); err != nil {
		failure(w, http.StatusBadRequest, "invalid request identifier")
		return false
	}
	return true
}
func page(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, http.StatusBadRequest, "invalid query parameter")
		return "", 0, false
	}
	limit := 50
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
	}
	if err != nil || limit < 1 || limit > 100 || len(q.Get("after")) > 200 {
		failure(w, http.StatusBadRequest, "invalid pagination")
		return "", 0, false
	}
	for key, values := range q {
		if (key != "limit" && key != "after") || len(values) != 1 {
			failure(w, http.StatusBadRequest, "invalid query parameter")
			return "", 0, false
		}
	}
	return q.Get("after"), limit, true
}
func send(w http.ResponseWriter, value any, err error) {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, http.StatusNotFound, "request not found")
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			failure(w, http.StatusGatewayTimeout, "read timed out")
		} else {
			failure(w, http.StatusServiceUnavailable, "local data unavailable")
		}
		return
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 4<<20 {
		failure(w, http.StatusInternalServerError, "response unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(data, '\n'))
}
func failure(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
