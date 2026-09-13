package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelbutton/copernicus/internal/store"
)

func TestLifecycleCommands(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "catalog.sqlite")
	if err := run(ctx, []string{"catalog", "import", "--db", db, "--file", "../../examples/catalog.json"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, createArgs(db), io.Discard); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(ctx, []string{"request", "status", "--db", db, "--id", "review-one"}, &output); err != nil {
		t.Fatal(err)
	}
	var progress store.RequestProgress
	if err := json.Unmarshal(output.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Total != 3 || progress.Completed != 0 || progress.Passed != 0 {
		t.Fatal("invented completion")
	}
	args := []string{"results", "import", "--db", db, "--exchange-dir", "../../compatibility/contract/v1/examples/valid"}
	for range 2 {
		if err := run(ctx, args, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	output.Reset()
	if err := run(ctx, []string{"results", "show", "--db", db, "--limit", "1"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"unassigned_results":2`) || !strings.Contains(output.String(), `"next_after"`) {
		t.Fatal(output.String())
	}
	if err := run(ctx, []string{"results", "rebuild", "--db", db, "--exchange-dir", "../../compatibility/contract/v1/examples/valid"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(ctx, []string{"request", "status", "--db", db, "--id", "review-one"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Completed != 0 {
		t.Fatal("unknown execution assigned by correlation label")
	}
}

func TestLifecycleHelpAndInvalidArgumentsDoNotWrite(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "missing.sqlite")
	for _, args := range [][]string{{"results", "import", "--help"}, {"results", "rebuild", "--help"}, {"results", "show", "--help"}, {"request", "status", "--help"}, {"serve", "--help"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out); err != nil || !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("help %v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"results", "import", "--db", db}, {"results", "show", "--db", db}, {"results", "show", "--db", db, "--limit", "101"}, {"results", "rebuild", "--db", db, "--exchange-dir", root, "extra"}, {"request", "status", "--db", db, "--id", "review-one"}, {"serve", "--db", db, "--port", "-1"}, {"serve", "--db", db}, {"serve", "--db", db, "--host", "0.0.0.0"}} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("help or arguments created files")
	}
}
