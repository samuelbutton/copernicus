package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/samuelbutton/copernicus/internal/httpapi"
	"github.com/samuelbutton/copernicus/internal/store"
)

func runServe(ctx context.Context, args []string, output io.Writer) (err error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", "", "existing database path")
	duckdb := flags.String("duckdb", "bin/duckdb", "DuckDB executable")
	port := flags.Int("port", 8080, "loopback port")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(output, usage)
			return err
		}
		return err
	}
	if *dbPath == "" || flags.NArg() != 0 || *port < 0 || *port > 65535 {
		return errors.New("provide --db PATH and --port 0–65535")
	}
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	db, err := store.Open(startup, *dbPath, false)
	if err != nil {
		cancel()
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	_, err = db.IndexStatus(startup)
	cancel()
	if err != nil {
		return fmt.Errorf("server requires schema 4; run a write command to upgrade: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: httpapi.Handler(db, listener.Addr().String(), *duckdb), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	if _, err := fmt.Fprintf(output, "Read-only API: http://%s\n", listener.Addr()); err != nil {
		return errors.Join(err, listener.Close())
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			<-done
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
