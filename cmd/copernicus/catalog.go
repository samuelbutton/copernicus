package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/samuelbutton/copernicus/internal/catalog"
)

func runCatalog(ctx context.Context, args []string, output io.Writer) (err error) {
	command := args[0]
	if command != "import" && command != "show" && command != "expand" {
		return errors.New("unknown catalog command; use copernicus --help")
	}
	flags := flag.NewFlagSet("catalog "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "catalog database path")
	var file, id string
	if command == "import" {
		flags.StringVar(&file, "file", "", "catalog JSON path")
	}
	if command == "expand" {
		flags.StringVar(&id, "collection", "", "collection identifier")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return fmt.Errorf("catalog arguments: %w", err)
	}
	if flags.NArg() != 0 || *dbPath == "" {
		return errors.New("provide --db PATH and no positional arguments")
	}
	var batch catalog.Catalog
	if command == "import" {
		if file == "" {
			return errors.New("import requires --file PATH")
		}
		batch, err = readCatalog(file)
		if err != nil {
			return err
		}
	}
	if command == "expand" {
		if err := catalog.ValidateID(id); err != nil {
			return err
		}
	}
	store, err := catalog.Open(ctx, *dbPath, command == "import")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	if command == "import" {
		if err := store.Import(ctx, batch); err != nil {
			return err
		}
		if _, err := io.WriteString(output, "Catalog imported.\n"); err != nil {
			return fmt.Errorf("catalog committed, but confirmation output failed: %w", err)
		}
		return nil
	}
	stored, err := store.Read(ctx)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if command == "show" {
		if err := encoder.Encode(stored); err != nil {
			return fmt.Errorf("write catalog: %w", err)
		}
		return nil
	}
	tests, err := stored.Expand(id)
	if err != nil {
		return err
	}
	if err := encoder.Encode(tests); err != nil {
		return fmt.Errorf("write expanded tests: %w", err)
	}
	return nil
}

func readCatalog(path string) (catalog.Catalog, error) {
	var empty catalog.Catalog
	info, err := os.Lstat(path)
	if err != nil {
		return empty, fmt.Errorf("inspect import file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return empty, errors.New("import must be a regular file, not a symbolic link")
	}
	f, err := os.Open(path)
	if err != nil {
		return empty, fmt.Errorf("open import file: %w", err)
	}
	defer f.Close()
	return catalog.Decode(f)
}
