package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutboxCLIWorkflow(t *testing.T) {
	ctx := context.Background()
	db, exchange := filepath.Join(t.TempDir(), "catalog.sqlite"), t.TempDir()
	if err := run(ctx, []string{"catalog", "import", "--db", db, "--file", "../../examples/catalog.json"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, createArgs(db), io.Discard); err != nil {
		t.Fatal(err)
	}
	show := []string{"outbox", "show", "--db", db}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(ctx, show, &output); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Pending, Published, Acknowledged int
		Destination                      string `json:"exchange_dir"`
	}
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Pending != 3 || status.Published != 0 || status.Destination != "" {
		t.Fatalf("status = %+v", status)
	}
	after, err := os.ReadFile(db)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("show changed database")
	}
	publish := []string{"outbox", "publish", "--db", db, "--exchange-dir", exchange, "--limit", "1"}
	output.Reset()
	if err := run(ctx, publish, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Pending != 2 || status.Published != 1 || status.Acknowledged != 1 {
		t.Fatalf("status = %+v", status)
	}
	want := errors.New("closed output")
	err = run(ctx, publish, failedWriter{want})
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "publication completed") {
		t.Fatalf("output error = %v", err)
	}
	output.Reset()
	if err := run(ctx, show, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Pending != 1 || status.Published != 2 {
		t.Fatal("output failure lost acknowledged delivery")
	}
	if err := run(ctx, publish, io.Discard); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Join(exchange, "jobs"))
	if err != nil || len(files) != 3 {
		t.Fatal("missing published jobs")
	}
}

func TestOutboxHelpAndArgumentsDoNotCreateFiles(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	db, exchange := filepath.Join(path, "missing.sqlite"), filepath.Join(path, "missing-exchange")
	for _, args := range [][]string{
		{"outbox", "show", "--help"}, {"outbox", "publish", "--db", db, "--help"},
	} {
		var output bytes.Buffer
		if err := run(ctx, args, &output); err != nil || !strings.Contains(output.String(), "Usage:") {
			t.Fatal("missing outbox help")
		}
	}
	for _, args := range [][]string{
		{"outbox"}, {"outbox", "unknown"}, {"outbox", "show"}, {"outbox", "show", "--db", db},
		{"outbox", "publish", "--db", db},
		{"outbox", "publish", "--db", db, "--exchange-dir", exchange},
		{"outbox", "publish", "--db", db, "--exchange-dir", exchange, "--limit", "0"},
		{"outbox", "publish", "--db", db, "--exchange-dir", exchange, "--limit", "1001"},
		{"outbox", "publish", "--db", db, "--exchange-dir", exchange, "extra"},
	} {
		if err := run(ctx, args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	files, err := os.ReadDir(path)
	if err != nil || len(files) != 0 {
		t.Fatal("arguments or help wrote files")
	}
}
