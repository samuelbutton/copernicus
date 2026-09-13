package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/samuelbutton/copernicus/internal/store"
)

func runResults(ctx context.Context, args []string, output io.Writer) (err error) {
	command := args[0]
	if command != "import" && command != "rebuild" && command != "show" {
		return errors.New("unknown results command; use copernicus --help")
	}
	flags := flag.NewFlagSet("results "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "database path")
	var directory, after string
	limit := 50
	if command == "show" {
		flags.StringVar(&after, "after", "", "page cursor")
		flags.IntVar(&limit, "limit", 50, "page size")
	} else {
		flags.StringVar(&directory, "exchange-dir", "", "public exchange directory")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return err
	}
	if *dbPath == "" || command != "show" && directory == "" || command != "import" && flags.NArg() != 0 || limit < 1 || limit > 100 || len(after) > 200 {
		return errors.New("invalid results arguments; use copernicus --help")
	}
	if _, err := os.Lstat(*dbPath); err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	db, err := store.Open(ctx, *dbPath, command != "show")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var result any
	switch command {
	case "import":
		result, err = db.ImportResults(ctx, directory, flags.Args())
	case "rebuild":
		var count int
		count, err = db.RebuildResults(ctx, directory)
		if err != nil {
			return err
		}
		result = struct {
			Results int `json:"indexed_results"`
		}{count}
	case "show":
		status, readErr := db.IndexStatus(ctx)
		if readErr != nil {
			return readErr
		}
		page, readErr := db.Results(ctx, after, limit)
		if readErr != nil {
			return readErr
		}
		result = struct {
			store.IndexStatus
			store.ResultPage
		}{status, page}
	}
	if writeErr := json.NewEncoder(output).Encode(result); writeErr != nil {
		return errors.Join(err, fmt.Errorf("operation may have committed; inspect results show after output failure: %w", writeErr))
	}
	return err
}
