package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalysisArgumentsAndReadOnlySelection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	for _, command := range []string{"create", "list", "status"} {
		var output bytes.Buffer
		if err := run(ctx, []string{"analysis", command, "--db", path, "--help"}, &output); err != nil || !strings.Contains(output.String(), "Usage:") {
			t.Fatal("analysis help", err)
		}
	}
	for _, args := range [][]string{
		{"analysis", "create", "--db", path, "--request", "review-one", "--template", "edge-v2"},
		{"analysis", "create", "--db", path, "--request", "INVALID", "--template", "edge-v2"},
		{"analysis", "create", "--db", path, "--request", "review-one", "--template", "edge-v2", "--analysis", "original"},
		{"analysis", "list", "--db", path, "--request", "review-one", "--template", "edge-v2"},
		{"analysis", "status", "--db", path, "--request", "review-one", "--analysis", "BAD"},
	} {
		var output bytes.Buffer
		if err := run(ctx, args, &output); err == nil || output.Len() != 0 {
			t.Fatal("accepted invalid arguments", args)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("arguments created a database")
	}
	var output bytes.Buffer
	if err := run(ctx, []string{"catalog", "import", "--db", path, "--file", "../../examples/catalog.json"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, createArgs(path), &output); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"analysis", "list", "--db", path, "--request", "review-one"},
		{"analysis", "status", "--db", path, "--request", "review-one"},
	} {
		output.Reset()
		if err := run(ctx, args, &output); err != nil || !strings.Contains(output.String(), "original") {
			t.Fatal("original selection unavailable", err)
		}
	}
	output.Reset()
	if err := run(ctx, []string{"analysis", "status", "--db", path, "--request", "review-one", "--analysis", "missing"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("unknown selection used original")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("selection read changed database", err)
	}
}
