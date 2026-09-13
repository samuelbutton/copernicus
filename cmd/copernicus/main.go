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

Import validates a catalog and adds its records in one transaction.
Show prints the stored definitions. Expand prints ordered, unique tests.
Help creates no files and starts no services.
Requests freeze inputs and report resolution failures without dispatching jobs.
Job submission and result import are not available yet.
See README.md, docs/test-model.md, and docs/requests.md for examples and cleanup.
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
	if len(args) < 2 || (args[0] != "catalog" && args[0] != "request") {
		return errors.New("unsupported arguments; use copernicus --help")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if args[0] == "request" {
		return runRequest(ctx, args[1:], output)
	}
	return runCatalog(ctx, args[1:], output)
}
