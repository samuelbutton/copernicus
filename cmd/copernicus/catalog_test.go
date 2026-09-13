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

	"github.com/samuelbutton/copernicus/internal/catalog"
)

func TestCatalogCommands(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog ? #.sqlite")
	var output bytes.Buffer
	args := []string{"catalog", "import", "--db", path, "--file", "../../examples/catalog.json"}
	if err := run(ctx, args, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Catalog imported.\n" {
		t.Fatalf("confirmation = %q", output.String())
	}
	output.Reset()
	if err := run(ctx, []string{"catalog", "show", "--db", path}, &output); err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Decode(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Scenarios) != 3 || len(c.Controllers) != 2 {
		t.Fatal("missing definitions")
	}
	output.Reset()
	if err := run(ctx, []string{"catalog", "expand", "--db", path, "--collection", "all-tests"}, &output); err != nil {
		t.Fatal(err)
	}
	var tests []catalog.Test
	if err := json.Unmarshal(output.Bytes(), &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) != 3 || tests[0].ID != "stopped-obstacle" || tests[1].ID != "empty-lane" || tests[2].ID != "moving-obstacle" {
		t.Fatalf("expansion = %v", tests)
	}
	output.Reset()
	if err := run(ctx, args, &output); err == nil || output.Len() != 0 {
		t.Fatalf("duplicate import: %v, %q", err, output.String())
	}
}

func TestCatalogRejectsBeforeCreatingDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.sqlite")
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"version":1,"tests":[{"id":"bad"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"catalog", "import", "--db", path, "--file", invalid},
		{"catalog", "import", "--db", path},
		{"catalog", "import", "--file", "../../examples/catalog.json"},
		{"catalog", "show", "--db", path},
		{"catalog", "expand", "--db", path, "--collection", "../bad"},
		{"catalog", "show", "--db", path, "extra"},
		{"catalog", "unknown"},
	} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if output.Len() != 0 {
			t.Fatal("failure wrote successful output")
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("created database for %v", args)
		}
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"catalog", "import", "--db", path, "--help"}, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("help created database")
	}
}

func TestImportOutputFailureReportsCommittedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	want := errors.New("closed output")
	err := run(context.Background(), []string{"catalog", "import", "--db", path, "--file", "../../examples/catalog.json"}, failedWriter{want})
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "committed") {
		t.Fatalf("error = %v", err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"catalog", "expand", "--db", path, "--collection", "all-tests"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "moving-obstacle") {
		t.Fatal("committed import missing")
	}
}
