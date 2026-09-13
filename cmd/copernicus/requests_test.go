package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelbutton/copernicus/internal/request"
)

func createArgs(db string) []string {
	return []string{"request", "create", "--db", db, "--id", "review-one", "--collection", "all-tests", "--controller", "baseline", "--requester", "reviewer"}
}
func TestRequestCommandsAndSuiteEdit(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "catalog.sqlite")
	var output bytes.Buffer
	if err := run(ctx, []string{"catalog", "import", "--db", db, "--file", "../../examples/catalog.json"}, &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(ctx, createArgs(db), &output); err != nil {
		t.Fatal(err)
	}
	first := output.String()
	var record request.Record
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if request.Hash(record.Snapshot) != record.SnapshotSHA256 {
		t.Fatal("CLI changed hashed snapshot bytes")
	}
	output.Reset()
	if err := run(ctx, []string{"catalog", "set-suite", "--db", db, "--suite", "smoke", "--tests", "empty-lane"}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Suite updated.\n" {
		t.Fatal("missing suite confirmation")
	}
	for _, args := range [][]string{createArgs(db), {"request", "show", "--db", db, "--id", "review-one"}} {
		output.Reset()
		if err := run(ctx, args, &output); err != nil {
			t.Fatal(err)
		}
		if output.String() != first {
			t.Fatal("saved request output changed")
		}
	}
	output.Reset()
	conflict := append(createArgs(db), "--priority", "3")
	if err := run(ctx, conflict, &output); !errors.Is(err, request.ErrIdentityConflict) || output.Len() != 0 {
		t.Fatalf("conflict = %v", err)
	}
	for _, tests := range []string{"missing", "empty-lane,empty-lane"} {
		if err := run(ctx, []string{"catalog", "set-suite", "--db", db, "--suite", "smoke", "--tests", tests}, &output); err == nil {
			t.Fatal("invalid suite edit accepted")
		}
	}
	output.Reset()
	if err := run(ctx, []string{"catalog", "set-suite", "--db", db, "--suite", "smoke", "--tests="}, &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(ctx, []string{"request", "show", "--db", db, "--id", "review-one"}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != first {
		t.Fatal("clearing suite changed saved request")
	}
}

func TestRequestHelpAndInvalidArgumentsDoNotWrite(t *testing.T) {
	db := filepath.Join(t.TempDir(), "missing.sqlite")
	for _, args := range [][]string{{"request", "create", "--db", db, "--help"}, {"request", "show", "--help"}, {"catalog", "set-suite", "--help"}} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "Usage:") {
			t.Fatal("missing help")
		}
	}
	for _, args := range [][]string{createArgs(db), {"request", "create"}, {"request", "show", "--db", db, "--id", "review-one"}, {"catalog", "set-suite", "--db", db, "--suite", "smoke"}, append(createArgs(db), "--seed", "-1"), append(createArgs(db), "unexpected")} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if output.Len() != 0 {
			t.Fatal("error wrote success output")
		}
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("arguments or help created database")
	}
}

func TestRequestConfirmationFailurePreservesSnapshot(t *testing.T) {
	db := filepath.Join(t.TempDir(), "catalog.sqlite")
	ctx := context.Background()
	var output bytes.Buffer
	if err := run(ctx, []string{"catalog", "import", "--db", db, "--file", "../../examples/catalog.json"}, &output); err != nil {
		t.Fatal(err)
	}
	want := errors.New("closed output")
	err := run(ctx, createArgs(db), failedWriter{want})
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "saved") {
		t.Fatalf("error = %v", err)
	}
	output.Reset()
	if err := run(ctx, []string{"request", "show", "--db", db, "--id", "review-one"}, &output); err != nil {
		t.Fatal(err)
	}
	first := output.String()
	output.Reset()
	if err := run(ctx, createArgs(db), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != first {
		t.Fatal("confirmation retry changed snapshot")
	}
}
