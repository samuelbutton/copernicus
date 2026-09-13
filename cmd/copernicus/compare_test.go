package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/samuelbutton/copernicus/internal/comparison"
)

func TestCompareCommand(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "catalog.sqlite")
	if err := run(ctx, []string{"catalog", "import", "--db", db, "--file", "../../examples/catalog.json"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, createArgs(db), io.Discard); err != nil {
		t.Fatal(err)
	}
	args := []string{"compare", "--db", db, "--baseline", "review-one", "--candidate", "review-one"}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(ctx, args, &out); err != nil {
		t.Fatal(err)
	}
	var r comparison.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Counts.Rows != 3 || r.Counts.Incomplete != 3 || r.Baseline.Completed != 0 || r.Candidate.Completed != 0 {
		t.Fatalf("report=%+v", r)
	}
	if err := run(ctx, args, failedWriter{err: io.ErrClosedPipe}); err == nil {
		t.Fatal("output failure ignored")
	}
	if err := run(ctx, []string{"compare", "--help"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"compare"}, {"compare", "--db", db, "--baseline", "missing", "--candidate", "review-one"}, append(args, "extra")} {
		if err := run(ctx, bad, io.Discard); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	after, err := os.ReadFile(db)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("comparison wrote database")
	}
}
