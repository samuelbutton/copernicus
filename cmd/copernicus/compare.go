package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/samuelbutton/copernicus/internal/comparison"
	"github.com/samuelbutton/copernicus/internal/store"
	"os"
)

func runCompare(ctx context.Context, args []string, output io.Writer) (err error) {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "database path")
	baseline := flags.String("baseline", "", "baseline request")
	candidate := flags.String("candidate", "", "candidate request")
	baselineAnalysis := flags.String("baseline-analysis", store.OriginalAnalysis, "baseline scoring selection")
	candidateAnalysis := flags.String("candidate-analysis", store.OriginalAnalysis, "candidate scoring selection")
	save := flags.Bool("save", false, "save terminal comparison pages")
	filter := flags.String("filter", "all", "row filter")
	sort := flags.String("sort", "id", "row order")
	after := flags.String("after", "", "row cursor")
	limit := flags.Int("limit", 2000, "page size")
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
	options := comparison.Options{Filter: *filter, Sort: *sort, After: *after, Limit: *limit}
	if err := options.Validate(); err != nil {
		return err
	}
	if _, err := os.Lstat(*dbPath); err != nil {
		return err
	}
	db, err := store.Open(ctx, *dbPath, *save)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	result, err := db.CompareView(ctx, *baseline, *candidate, *baselineAnalysis, *candidateAnalysis, *duckdb, options)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
