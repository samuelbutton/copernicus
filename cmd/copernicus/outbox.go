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

func runOutbox(ctx context.Context, args []string, output io.Writer) (err error) {
	command := args[0]
	if command != "publish" && command != "show" {
		return errors.New("unknown outbox command; use copernicus --help")
	}
	flags := flag.NewFlagSet("outbox "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "database path")
	var directory string
	limit := 100
	if command == "publish" {
		flags.StringVar(&directory, "exchange-dir", "", "existing exchange directory")
		flags.IntVar(&limit, "limit", 100, "maximum jobs per invocation")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return fmt.Errorf("outbox arguments: %w", err)
	}
	if flags.NArg() != 0 || *dbPath == "" {
		return errors.New("provide --db PATH and no positional arguments")
	}
	if command == "publish" && (directory == "" || limit < 1 || limit > store.MaxPublishBatch) {
		return errors.New("provide --exchange-dir PATH and --limit 1–1000")
	}
	if _, err := os.Lstat(*dbPath); err != nil {
		return fmt.Errorf("inspect catalog: %w", err)
	}
	db, err := store.Open(ctx, *dbPath, command == "publish")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	acknowledged := 0
	if command == "publish" {
		acknowledged, err = db.PublishOutbox(ctx, directory, limit)
		if err != nil {
			return fmt.Errorf("outbox stopped after %d acknowledgements; inspect outbox show and retry: %w", acknowledged, err)
		}
	}
	status, err := db.Outbox(ctx)
	if err != nil {
		return fmt.Errorf("read outbox (requires schema 3; a write command upgrades older databases): %w", err)
	}
	result := struct {
		store.OutboxStatus
		Acknowledged *int `json:"acknowledged,omitempty"`
	}{OutboxStatus: status}
	if command == "publish" {
		result.Acknowledged = &acknowledged
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		if command == "publish" {
			return fmt.Errorf("publication completed, but confirmation output failed; inspect outbox show: %w", err)
		}
		return fmt.Errorf("write outbox status: %w", err)
	}
	return nil
}
