// Command copernicus manages a local catalog of simulation tests.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const usage = `Copernicus — local simulation review

Usage:
  copernicus [--help | -h | help]
  copernicus catalog import --db PATH --file PATH
  copernicus catalog show --db PATH
  copernicus catalog expand --db PATH --collection ID
  copernicus catalog set-suite --db PATH --suite ID --tests ID,ID
  copernicus request create --db PATH --id ID --collection ID --controller ID --requester ID
                           [--priority 0..3] [--seed N] [--repeat N]
  copernicus request show --db PATH --id ID
  copernicus request status --db PATH --id ID
  copernicus results import --db PATH --exchange-dir PATH [events/FILE.json results/FILE.json ...]
  copernicus results rebuild --db PATH --exchange-dir PATH
  copernicus results show --db PATH [--after CURSOR] [--limit 1..100]
  copernicus compare --db PATH --baseline ID --candidate ID [--duckdb PATH]
  copernicus serve --db PATH [--port PORT] [--duckdb PATH] [--web-dir PATH]
  copernicus outbox show --db PATH
  copernicus outbox publish --db PATH --exchange-dir PATH [--limit 1..1000]

Import validates a catalog and adds its records in one transaction.
Show prints the stored definitions. Expand prints ordered, unique tests.
Help creates no files and starts no services.
Requests freeze inputs and save compatible jobs in a durable outbox.
Outbox publication delivers job files. Run it again to resume pending delivery.
Publication does not start workers or import results.
Result import validates public files and records repeat-safe progress.
Compare reads selected outcomes through DuckDB; its default path is bin/duckdb.
Serve exposes a read-only HTTP API on 127.0.0.1.
Adding --web-dir web/dist serves the review interface and enables request creation.
See docs/lifecycle.md for completion checks and index recovery.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "copernicus:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 || len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		if _, err := io.WriteString(output, usage); err != nil {
			return fmt.Errorf("write help: %w", err)
		}
		return nil
	}
	if args[0] == "serve" {
		return runServe(ctx, args[1:], output)
	}
	if args[0] == "compare" {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return runCompare(ctx, args[1:], output)
	}
	if len(args) < 2 || (args[0] != "catalog" && args[0] != "request" && args[0] != "outbox" && args[0] != "results") {
		return errors.New("unsupported arguments; use copernicus --help")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if args[0] == "results" {
		return runResults(ctx, args[1:], output)
	}
	if args[0] == "outbox" {
		return runOutbox(ctx, args[1:], output)
	}
	if args[0] == "request" {
		return runRequest(ctx, args[1:], output)
	}
	return runCatalog(ctx, args[1:], output)
}
