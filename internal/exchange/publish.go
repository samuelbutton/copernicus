// Package exchange publishes immutable job files on local filesystems.
package exchange

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/samuelbutton/copernicus/compatibility"
)

var ErrConflict = errors.New("published job has different bytes")

// Directory holds directory descriptors across publication. Callers must not
// move or replace the exchange while commands use it.
type Directory struct {
	Path string
	root *os.Root
	jobs *os.Root
	dir  *os.File
}

// Open requires an existing exchange directory and resolves aliases once.
// The jobs directory is created lazily, after durable destination binding.
func Open(path string) (*Directory, error) {
	if path == "" {
		return nil, errors.New("exchange directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve exchange: %w", err)
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}
	return &Directory{Path: resolved, root: root}, nil
}

func (d *Directory) Close() error {
	var err error
	if d.dir != nil {
		err = d.dir.Close()
	}
	if d.jobs != nil {
		err = errors.Join(err, d.jobs.Close())
	}
	return errors.Join(err, d.root.Close())
}

func (d *Directory) prepare() error {
	if d.jobs != nil {
		return nil
	}
	if err := d.root.Mkdir("jobs", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := d.root.Lstat("jobs")
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("jobs must be a directory, not a symbolic link")
	}
	jobs, err := d.root.OpenRoot("jobs")
	if err != nil {
		return err
	}
	dir, err := jobs.Open(".")
	if err != nil {
		return errors.Join(err, jobs.Close())
	}
	// Persist creation of jobs/ before acknowledging any file within it.
	parent, err := d.root.Open(".")
	if err == nil {
		err = errors.Join(parent.Sync(), parent.Close())
	}
	if err != nil {
		return errors.Join(err, dir.Close(), jobs.Close())
	}
	d.jobs, d.dir = jobs, dir
	return nil
}

// Publish synchronizes validated bytes, atomically renames without replacement,
// and synchronizes the directory. Identical existing bytes are a safe retry.
func (d *Directory) Publish(ctx context.Context, data []byte) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := compatibility.ParseJob(data)
	if err != nil {
		return err
	}
	if err := d.prepare(); err != nil {
		return fmt.Errorf("prepare jobs: %w", err)
	}
	name := job.JobID + ".json"
	// .tmp files are never selected by consumers. Random names permit competing
	// publishers without a persistent lock or stale lock recovery protocol.
	temporary := ".copernicus-" + rand.Text() + ".tmp"
	f, err := d.jobs.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		removeErr := d.jobs.Remove(temporary)
		if !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return fmt.Errorf("stage job: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := renameExclusive(int(d.dir.Fd()), temporary, name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("publish job: %w", err)
		}
		if err := d.identical(name, data); err != nil {
			return err
		}
	}
	if err := d.dir.Sync(); err != nil {
		return fmt.Errorf("synchronize jobs: %w", err)
	}
	return nil
}

func (d *Directory) identical(name string, expected []byte) error {
	// Reject links and non-regular files, including pipes, without blocking.
	f, err := d.jobs.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("existing job: %w", err)
	}
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return errors.Join(ErrConflict, statErr, f.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(f, compatibility.MaxJobBytes+1))
	if readErr == nil && !bytes.Equal(data, expected) {
		readErr = ErrConflict
	}
	if readErr == nil {
		readErr = f.Sync()
	}
	return errors.Join(readErr, f.Close())
}
