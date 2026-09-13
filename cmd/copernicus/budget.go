package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/samuelbutton/copernicus/internal/store"
	"io"
	"os"
)

func runBudget(ctx context.Context, args []string, output io.Writer) (err error) {
	command := args[0]
	if command != "set" && command != "show" {
		return errors.New("use budget set or show")
	}
	flags := flag.NewFlagSet("budget "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("db", "", "database path")
	team := flags.String("team", "", "team identifier")
	ticks := flags.Int64("ticks", -1, "cumulative simulation tick limit")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return err
	}
	if *path == "" || flags.NArg() != 0 || command == "set" && (*team == "" || *ticks < 0) || command == "show" && (*team != "" || *ticks != -1) {
		return errors.New("provide --db PATH; set also requires --team ID --ticks N")
	}
	if _, err := os.Lstat(*path); err != nil {
		return err
	}
	db, err := store.Open(ctx, *path, command == "set")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if command == "set" {
		if err := db.SetBudget(ctx, *team, *ticks); err != nil {
			return err
		}
	}
	budgets, err := db.Budgets(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(budgets)
}
