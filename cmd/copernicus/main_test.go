package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			if err := run(context.Background(), args, &output); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Usage:", "copernicus", "does not start workers or import results"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("help = %q, want %q", output.String(), want)
				}
			}
		})
	}
}

func TestRejectArgumentsWithoutOutput(t *testing.T) {
	for _, args := range [][]string{{"run"}, {"--unknown"}, {"--help", "extra"}, {"help", "run"}, {""}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			if err := run(context.Background(), args, &output); err == nil {
				t.Fatal("expected argument error")
			}
			if output.Len() != 0 {
				t.Fatalf("output = %q, want empty", output.String())
			}
		})
	}
}

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }

func TestHelpPreservesOutputFailure(t *testing.T) {
	want := errors.New("closed output")
	if err := run(context.Background(), nil, failedWriter{want}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
