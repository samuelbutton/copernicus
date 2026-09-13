package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
	"github.com/samuelbutton/copernicus/internal/store"
)

func TestReadOnlyAPIAndLocalBoundary(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := store.Open(ctx, path, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	source, err := compatibility.Yamata()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"review-one", "review-two"} {
		if _, err := db.CreateRequest(ctx, request.Submission{ID: id, CollectionID: "all-tests", ControllerID: "baseline", Requester: "reviewer", Priority: 1}, source); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler(db, "127.0.0.1:8080", "../../bin/duckdb")
	for _, tt := range []struct {
		path   string
		status int
	}{
		{"/api/requests/review-one/analyses", 200}, {"/api/requests/unknown/analyses", 404},
		{"/api/requests/review-one/status?analysis=original", 200}, {"/api/requests/review-one/status?analysis=missing", 404},
		{"/api/requests/review-one/status?analysis=original&analysis=other", 400}, {"/api/requests/review-one/status?analysis=BAD", 400},
		{"/api/comparisons?baseline=review-one&candidate=review-two&baseline_analysis=original&candidate_analysis=original", 200},
		{"/api/comparisons?baseline=review-one&candidate=review-two&candidate_analysis=missing", 404},
		{"/api/comparisons?baseline=review-one&candidate=review-two&candidate_analysis=original&candidate_analysis=original", 400},
		{"/healthz", 200}, {"/readyz", 200}, {"/api/requests", 200}, {"/api/requests?limit=1", 200},
		{"/api/requests/review-one", 200}, {"/api/requests/review-two/status", 200}, {"/api/requests/unknown/status", 404},
		{"/api/requests/INVALID/status", 400}, {"/api/results", 200}, {"/api/index", 200}, {"/unknown", 404},
		{"/api/results?limit=101", 400}, {"/api/results?limit=0", 400}, {"/api/results?limit=1&limit=2", 400}, {"/api/results?wrong=1", 400}, {"/api/results?limit=1;ignored=2", 400},
		{"/api/comparisons?baseline=review-one&candidate=review-two&limit=1", 200},
		{"/api/comparisons?baseline=review-one&candidate=unknown", 404},
		{"/api/comparisons?baseline=review-one", 400},
		{"/api/comparisons?baseline=review-one&candidate=review-two&duckdb=other", 400},
		{"/api/comparisons?baseline=review-one&candidate=review-two&limit=0", 400},
	} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8080"+tt.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tt.status {
			t.Fatalf("%s: %d %s", tt.path, response.Code, response.Body.String())
		}
		if !json.Valid(response.Body.Bytes()) || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("invalid response or cache/origin policy")
		}
		if tt.path == "/api/requests?limit=1" && !strings.Contains(response.Body.String(), `"next_after":"review-one"`) {
			t.Fatal("missing stable cursor")
		}
		if tt.path == "/api/requests/review-two/status" {
			var progress store.RequestProgress
			if err := json.Unmarshal(response.Body.Bytes(), &progress); err != nil {
				t.Fatal(err)
			}
			if progress.RequestID != "review-two" || progress.Total != 3 || progress.Completed != 0 || progress.Complete {
				t.Fatal("wrong request denominator")
			}
		}
		if tt.path == "/api/comparisons?baseline=review-one&candidate=review-two&limit=1" {
			if !strings.Contains(response.Body.String(), `"rows":3`) || !strings.Contains(response.Body.String(), `"incomplete":3`) || !strings.Contains(response.Body.String(), `"next_after"`) {
				t.Fatal("comparison pagination lost full denominators")
			}
		}
	}
	for _, tt := range []struct {
		method, host, origin, fetch string
		status                      int
	}{
		{"POST", "127.0.0.1:8080", "", "", 405}, {"OPTIONS", "127.0.0.1:8080", "", "", 405},
		{"GET", "remote.invalid:8080", "", "", 403}, {"GET", "127.0.0.1:8080", "https://remote.invalid", "", 403},
		{"GET", "127.0.0.1:8080", "null", "", 403}, {"GET", "127.0.0.1:8080", "", "cross-site", 403},
		{"GET", "127.0.0.1:8080", "http://127.0.0.1:8080", "same-origin", 200},
	} {
		req := httptest.NewRequest(tt.method, "http://127.0.0.1:8080/api/index", nil)
		req.Host = tt.host
		req.Header.Set("Origin", tt.origin)
		req.Header.Set("Sec-Fetch-Site", tt.fetch)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tt.status {
			t.Fatalf("boundary %+v = %d", tt, response.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/index", strings.NewReader("body"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 400 {
		t.Fatal("accepted GET body")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("API changed database")
	}
}
