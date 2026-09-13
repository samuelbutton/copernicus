package exchange

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func jobBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../compatibility/contract/v1/examples/valid/jobs/run-1.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func openDirectory(t *testing.T, path string) *Directory {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d
}

func TestConcurrentPublicationNeverReplaces(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		path := t.TempDir()
		first := jobBytes(t)
		second := append([]byte(nil), first...)
		if conflict {
			second = bytes.Replace(second, []byte(`"priority": 1`), []byte(`"priority": 2`), 1)
		}
		var wg sync.WaitGroup
		results := make(chan error, 8)
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			d := openDirectory(t, path)
			data := first
			if i%2 == 1 {
				data = second
			}
			wg.Add(1)
			go func() { defer wg.Done(); <-start; results <- d.Publish(context.Background(), data) }()
		}
		close(start)
		wg.Wait()
		close(results)
		successes, conflicts := 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if conflict && (successes != 4 || conflicts != 4) || !conflict && successes != 8 {
			t.Fatalf("success=%d conflict=%d", successes, conflicts)
		}
		final, err := os.ReadFile(filepath.Join(path, "jobs/run-1.json"))
		if err != nil || (!bytes.Equal(final, first) && !bytes.Equal(final, second)) {
			t.Fatal("partial or changed final bytes")
		}
		info, err := os.Stat(filepath.Join(path, "jobs/run-1.json"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("wrong publication permissions")
		}
		files, err := os.ReadDir(filepath.Join(path, "jobs"))
		if err != nil || len(files) != 1 {
			t.Fatal("temporary files were not cleaned up")
		}
	}
}

func TestPublicationRejectsUnsafeDestinations(t *testing.T) {
	for _, kind := range []string{"jobs-link", "file-link", "fifo", "directory", "different", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			path, outside := t.TempDir(), t.TempDir()
			if kind == "jobs-link" {
				if err := os.Symlink(outside, filepath.Join(path, "jobs")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(path, "jobs"), 0700); err != nil {
					t.Fatal(err)
				}
				name := filepath.Join(path, "jobs/run-1.json")
				var err error
				switch kind {
				case "file-link":
					err = os.Symlink(filepath.Join(outside, "untouched.json"), name)
				case "fifo":
					err = syscall.Mkfifo(name, 0600)
				case "directory":
					err = os.Mkdir(name, 0700)
				case "different":
					err = os.WriteFile(name, []byte("original"), 0600)
				case "oversized":
					err = os.WriteFile(name, bytes.Repeat([]byte("x"), (1<<20)+1), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			d := openDirectory(t, path)
			if err := d.Publish(context.Background(), jobBytes(t)); err == nil {
				t.Fatal("accepted unsafe destination")
			}
			files, err := os.ReadDir(outside)
			if err != nil || len(files) != 0 {
				t.Fatal("wrote outside exchange")
			}
			if kind == "different" {
				data, err := os.ReadFile(filepath.Join(path, "jobs/run-1.json"))
				if err != nil || string(data) != "original" {
					t.Fatal("overwrote conflict")
				}
			}
		})
	}
}

func TestPublicationValidatesBeforeWritingAndIgnoresStaging(t *testing.T) {
	path := t.TempDir()
	d := openDirectory(t, path)
	if err := d.Publish(context.Background(), []byte(`{"kind":"job"}`)); err == nil {
		t.Fatal("published invalid JSON")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Publish(ctx, jobBytes(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	files, err := os.ReadDir(path)
	if err != nil || len(files) != 0 {
		t.Fatal("rejected publication wrote files")
	}
	if err := os.Mkdir(filepath.Join(path, "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(path, "jobs/.copernicus-interrupted.tmp")
	if err := os.WriteFile(stale, []byte("partial bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.Publish(context.Background(), jobBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := d.Publish(context.Background(), jobBytes(t)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(stale)
	if err != nil || string(data) != "partial bytes" {
		t.Fatal("touched another writer's staging file")
	}
}
