package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"io"
	"os"

	"github.com/samuelbutton/copernicus/internal/store"
)

func runAnalysis(ctx context.Context, args []string, output io.Writer) (err error) {
	if len(args) == 0 || (args[0] != "create" && args[0] != "list" && args[0] != "status") {
		return errors.New("use analysis create, list, or status")
	}
	command := args[0]
	flags := flag.NewFlagSet("analysis", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "database path")
	id := flags.String("request", "", "request identifier")
	template := flags.String("template", "", "scoring template identifier")
	selection := flags.String("analysis", store.OriginalAnalysis, "selected scores")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return err
	}
	if *dbPath == "" || *id == "" || flags.NArg() != 0 || command == "create" && *template == "" {
		return errors.New("provide --db PATH --request ID and --template ID for creation")
	}
	invalid := false
	flags.Visit(func(f *flag.Flag) {
		if command != "create" && f.Name == "template" || command != "status" && f.Name == "analysis" {
			invalid = true
		}
	})
	if invalid {
		return errors.New("unsupported analysis option for this command")
	}
	if catalog.ValidateID(*id) != nil || command == "create" && catalog.ValidateID(*template) != nil || command == "status" && catalog.ValidateID(*selection) != nil {
		return errors.New("invalid analysis identifier")
	}
	if _, err := os.Lstat(*dbPath); err != nil {
		return fmt.Errorf("inspect catalog: %w", err)
	}
	db, err := store.Open(ctx, *dbPath, command == "create")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var result any
	switch command {
	case "create":
		result, err = db.CreateAnalysis(ctx, *id, *template)
	case "list":
		result, err = db.Analyses(ctx, *id)
	case "status":
		result, err = db.ProgressAnalysis(ctx, *id, *selection)
	}
	if err != nil {
		return err
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		if command == "create" {
			return fmt.Errorf("analysis selection saved; repeat the same command to retrieve it: %w", err)
		}
		return err
	}
	return nil
}
