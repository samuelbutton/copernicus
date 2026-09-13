package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
	"github.com/samuelbutton/copernicus/internal/store"
)

func TestReviewServerCreationBoundary(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<html>review</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := ReviewHandler(db, "127.0.0.1:8080", "../../bin/duckdb", assets)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"id":"browser-one","collection_id":"all-tests","controller_id":"baseline","requester":"reviewer","priority":1,"seed":0,"repeat":0,"suite_ids":["smoke"]}`
	call := func(method, path, body, origin, content, header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", content)
		req.Header.Set("X-Copernicus-Request", header)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		return response
	}
	for _, tt := range []struct{ origin, content, header string }{
		{"", "application/json", "1"}, {"null", "application/json", "1"}, {"https://remote.invalid", "application/json", "1"}, {"http://127.0.0.1:8080", "text/plain", "1"}, {"http://127.0.0.1:8080", "application/json", ""},
	} {
		if r := call("POST", "/api/requests", body, tt.origin, tt.content, tt.header); r.Code != 403 {
			t.Fatalf("write boundary: %d", r.Code)
		}
	}
	for _, bad := range []string{body + body, strings.Replace(body, `"id":`, `"ID":`, 1), strings.Replace(body, `"id":`, `"id":"other","id":`, 1), strings.Replace(body, `"priority":1,`, "", 1), strings.Replace(body, `["smoke"]`, `[]`, 1), strings.Replace(body, `["smoke"]`, `null`, 1), strings.Repeat(" ", request.MaxSubmissionBytes+1)} {
		if r := call("POST", "/api/requests", bad, "http://127.0.0.1:8080", "application/json", "1"); r.Code != 400 {
			t.Fatalf("bad body: %d", r.Code)
		}
	}
	for range 2 {
		if r := call("POST", "/api/requests", body, "http://127.0.0.1:8080", "application/json", "1"); r.Code != 200 {
			t.Fatalf("create: %d %s", r.Code, r.Body.String())
		}
	}
	changed := strings.Replace(body, `"baseline"`, `"candidate"`, 1)
	if r := call("POST", "/api/requests", changed, "http://127.0.0.1:8080", "application/json", "1"); r.Code != 409 {
		t.Fatal("identity conflict not reported")
	}
	page, err := db.Requests(ctx, "", 100)
	if err != nil || len(page.IDs) != 1 {
		t.Fatal("duplicate request")
	}
	record, err := db.Request(ctx, "browser-one")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Executions) != 2 {
		t.Fatal("suite selection ignored")
	}
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"GET", "/api/catalog", 200}, {"GET", "/private.txt", 404}, {"GET", "/api/requests/INVALID/executions/unknown", 400}, {"GET", "/api/requests/INVALID/executions/unknown/ticks/0", 400}, {"POST", "/api/index", 405}, {"GET", "/api/requests/browser-one/executions/" + snapshot.Executions[0].ID, 200}, {"GET", "/api/requests/browser-one/executions/unknown", 404},
	} {
		r := call(tt.method, tt.path, "", "", "", "")
		if tt.path != "/" && !json.Valid(r.Body.Bytes()) {
			t.Fatal("invalid JSON response")
		}
		if r.Code != tt.status {
			t.Fatalf("%s: %d %s", tt.path, r.Code, r.Body.String())
		}
	}
	if err := os.Symlink(filepath.Join(assets, "index.html"), filepath.Join(assets, "linked.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewHandler(db, "127.0.0.1:8080", "", assets); err == nil {
		t.Fatal("accepted linked assets")
	}
}
