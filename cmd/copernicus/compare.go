package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/samuelbutton/copernicus/internal/store"
)

func runCompare(ctx context.Context, args []string, output io.Writer) (err error) {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "database path")
	baseline := flags.String("baseline", "", "baseline request")
	candidate := flags.String("candidate", "", "candidate request")
	duckdb := flags.String("duckdb", "bin/duckdb", "DuckDB executable")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return err
	}
	if *dbPath == "" || *baseline == "" || *candidate == "" || *duckdb == "" || flags.NArg() != 0 {
		return errors.New("provide --db PATH --baseline ID --candidate ID [--duckdb PATH]")
	}
	db, err := store.Open(ctx, *dbPath, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	result, err := db.Compare(ctx, *baseline, *candidate, *duckdb)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
