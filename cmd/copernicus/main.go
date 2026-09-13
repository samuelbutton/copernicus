// Command copernicus is the entry point for the local review project.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const usage = `Copernicus — local simulation review

Usage:
  copernicus [--help | -h | help]

Show this help without creating files or starting services.
This version provides the command entry point and a separate web introduction.
Request creation and result import are not available yet.
See README.md for build commands and the local web preview.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "copernicus:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) > 1 || len(args) == 1 && args[0] != "--help" && args[0] != "-h" && args[0] != "help" {
		return errors.New("unsupported arguments; use copernicus --help")
	}
	if _, err := io.WriteString(output, usage); err != nil {
		return fmt.Errorf("write help: %w", err)
	}
	return nil
}
