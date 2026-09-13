package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
	"github.com/samuelbutton/copernicus/internal/store"
)

func runRequest(ctx context.Context, args []string, output io.Writer) (err error) {
	command := args[0]
	if command != "create" && command != "show" {
		return errors.New("unknown request command; use copernicus --help")
	}
	flags := flag.NewFlagSet("request "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "database path")
	var input request.Submission
	flags.StringVar(&input.ID, "id", "", "submission identity")
	if command == "create" {
		flags.StringVar(&input.CollectionID, "collection", "", "collection identifier")
		flags.StringVar(&input.ControllerID, "controller", "", "controller identifier")
		flags.StringVar(&input.Requester, "requester", "", "requester identifier")
		flags.IntVar(&input.Priority, "priority", 1, "priority class")
		flags.Int64Var(&input.Seed, "seed", 0, "random seed")
		flags.IntVar(&input.Repeat, "repeat", 0, "repeat number")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return fmt.Errorf("request arguments: %w", err)
	}
	if flags.NArg() != 0 || *dbPath == "" {
		return errors.New("provide --db PATH and no positional arguments")
	}
	if command == "create" {
		if err := input.Validate(); err != nil {
			return err
		}
	} else {
		if err := catalog.ValidateID(input.ID); err != nil {
			return err
		}
	}
	// Requests require an existing catalog; argument mistakes do not create databases.
	if _, err := os.Lstat(*dbPath); err != nil {
		return fmt.Errorf("inspect catalog: %w", err)
	}
	db, err := store.Open(ctx, *dbPath, command == "create")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var record request.Record
	if command == "create" {
		source, err := compatibility.Yamata()
		if err != nil {
			return err
		}
		record, err = db.CreateRequest(ctx, input, source)
		if err != nil {
			return err
		}
	} else {
		record, err = db.Request(ctx, input.ID)
		if err != nil {
			return err
		}
	}
	// The stored JSON bytes are printed without reformatting their hashed content.
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := output.Write(append(data, '\n')); err != nil {
		if command == "create" {
			return fmt.Errorf("request saved, but confirmation output failed; inspect with request show: %w", err)
		}
		return fmt.Errorf("write request: %w", err)
	}
	return nil
}
