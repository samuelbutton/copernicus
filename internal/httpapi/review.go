package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/request"
	"github.com/samuelbutton/copernicus/internal/store"
)

// ReviewHandler loads the built interface once. Only index.html and generated
// JavaScript/CSS assets are exposed, with no directory listings or symlinks.
func ReviewHandler(db *store.Store, authority, duckdb, directory string) (http.Handler, error) {
	assets := map[string][]byte{}
	total := 0
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("web assets cannot contain symbolic links")
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if name != "index.html" && !(strings.HasPrefix(name, "assets/") && (strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css"))) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return errors.New("invalid web asset")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (16<<20)+1))
		if err := errors.Join(readErr, f.Close()); err != nil {
			return err
		}
		total += len(data)
		if total > 16<<20 {
			return errors.New("web assets exceed 16 MiB")
		}
		assets["/"+name] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(assets["/index.html"]) == 0 {
		return nil, errors.New("build the web interface before serving it")
	}
	return handler(db, authority, duckdb, assets), nil
}

func createRequest(db *store.Store, w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		failure(w, 400, "request creation accepts no query parameters")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, request.MaxSubmissionBytes+1))
	if err != nil || len(data) > request.MaxSubmissionBytes || !utf8.Valid(data) {
		failure(w, 400, "invalid request body")
		return
	}
	// Reject duplicate, case-folded, missing, null, and unknown fields before the
	// typed decoder. Submission objects contain only scalars and one string array.
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		failure(w, 400, "provide a request object")
		return
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{"id": true, "collection_id": true, "controller_id": true, "requester": true, "priority": true, "seed": true, "repeat": true, "suite_ids": true}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] || fields[name] != nil {
			failure(w, 400, "invalid request fields")
			return
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			failure(w, 400, "invalid request value")
			return
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		failure(w, 400, "invalid request object")
		return
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		failure(w, 400, "provide one request object")
		return
	}
	for name := range allowed {
		if name != "suite_ids" && fields[name] == nil {
			failure(w, 400, "missing request field")
			return
		}
	}
	var input request.Submission
	if json.Unmarshal(data, &input) != nil || input.Validate() != nil {
		failure(w, 400, "check request fields and selected suites")
		return
	}
	source, err := compatibility.Yamata()
	if err != nil {
		send(w, nil, err)
		return
	}
	record, err := db.CreateRequest(r.Context(), input, source)
	if errors.Is(err, request.ErrIdentityConflict) {
		failure(w, 409, "this request name already has different inputs")
		return
	}
	if errors.Is(err, request.ErrInvalidSubmission) {
		failure(w, 400, "selected tests are unavailable; reload the catalog and check your selection")
		return
	}
	// Return only its identity: a valid large snapshot must not turn a successful
	// creation into an oversized HTTP error. Inputs are read per execution.
	send(w, struct {
		ID   string `json:"request_id"`
		Hash string `json:"snapshot_sha256"`
	}{input.ID, record.SnapshotSHA256}, err)
}
