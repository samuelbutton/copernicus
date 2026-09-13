package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBudgetCommandsAndRejectedAdmission(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "catalog.sqlite")
	var out bytes.Buffer
	for _, command := range []string{"set", "show"} {
		if err := run(ctx, []string{"budget", command, "--db", db, "--help"}, &out); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("help wrote database")
	}
	if err := run(ctx, []string{"catalog", "import", "--db", db, "--file", "../../examples/catalog.json"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"budget", "set", "--db", db, "--team", "local", "--ticks", "299"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, createArgs(db), &out); err == nil || out.Len() != 0 {
		t.Fatal("over-budget submission printed acceptance")
	}
	if err := run(ctx, []string{"budget", "set", "--db", db, "--team", "local", "--ticks", "300"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, createArgs(db), &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, []string{"budget", "show", "--db", db}, &out); err != nil {
		t.Fatal(err)
	}
	first := out.String()
	if err := run(ctx, createArgs(db), &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, []string{"budget", "show", "--db", db}, &out); err != nil || out.String() != first {
		t.Fatal("CLI retry charged twice", err)
	}
}
