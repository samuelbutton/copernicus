// Package store owns the local catalog and request database.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/samuelbutton/copernicus/internal/catalog"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

//go:embed requests-v2.sql
var requestsSchema string

const applicationID = 1129333588

// Store owns the catalog connection pool. Callers close it when their command ends.
type Store struct{ db *sql.DB }

// Open creates a catalog only when create is true. Inspection uses SQLite's
// read-only mode and never initializes or upgrades a database.
func Open(ctx context.Context, path string, create bool) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("database path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) && create {
		f, openErr := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if openErr != nil && !errors.Is(openErr, os.ErrExist) {
			return nil, fmt.Errorf("create database: %w", openErr)
		}
		if f != nil {
			if err := f.Close(); err != nil {
				return nil, fmt.Errorf("close new database: %w", err)
			}
		}
		info, err = os.Lstat(absolute)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("database must be a regular file, not a symbolic link")
	}
	mode := "ro"
	lock := "deferred"
	if create {
		mode = "rw"
		lock = "immediate"
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := url.Values{"mode": {mode}, "_txlock": {lock}, "_pragma": {"foreign_keys(1)", "busy_timeout(3000)", "synchronous(FULL)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open catalog: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	if err := s.initialize(ctx, create); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initialize(ctx context.Context, create bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin catalog check: %w", err)
	}
	defer tx.Rollback()
	var app, version int
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return fmt.Errorf("read catalog identity: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read catalog version: %w", err)
	}
	if app == applicationID && (version >= 1 && version <= 4) {
		if create && version == 1 {
			if _, err := tx.ExecContext(ctx, requestsSchema); err != nil {
				return fmt.Errorf("upgrade request schema: %w", err)
			}
		}
		if create && version < 3 {
			if err := upgradeOutbox(ctx, tx); err != nil {
				return fmt.Errorf("upgrade outbox: %w", err)
			}
		}
		if create && version < 4 {
			if _, err := tx.ExecContext(ctx, resultsSchema); err != nil {
				return fmt.Errorf("upgrade result schema: %w", err)
			}
		}
		return tx.Commit()
	}
	if !create || app != 0 || version != 0 {
		return fmt.Errorf("unsupported catalog database: application %d, version %d", app, version)
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
		return fmt.Errorf("inspect empty database: %w", err)
	}
	if count != 0 {
		return errors.New("refusing to initialize a nonempty database")
	}
	if _, err := tx.ExecContext(ctx, schema+"\n"+requestsSchema+"\n"+outboxSchema+"\n"+resultsSchema); err != nil {
		return fmt.Errorf("create catalog schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit catalog schema: %w", err)
	}
	return nil
}

// Import adds a complete batch in one transaction. Repeated identifiers are
// errors, including unchanged definitions. A failed batch leaves all records intact.
func (s *Store) Import(ctx context.Context, c catalog.Catalog) error {
	if err := c.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()
	if err := insertDefinitions(ctx, tx, "scenarios", c.Scenarios, func(v catalog.Scenario) string { return v.ID }); err != nil {
		return err
	}
	if err := insertDefinitions(ctx, tx, "controllers", c.Controllers, func(v catalog.Controller) string { return v.ID }); err != nil {
		return err
	}
	if err := insertDefinitions(ctx, tx, "run_templates", c.RunTemplates, func(v catalog.RunTemplate) string { return v.ID }); err != nil {
		return err
	}
	if err := insertDefinitions(ctx, tx, "analysis_templates", c.AnalysisTemplates, func(v catalog.AnalysisTemplate) string { return v.ID }); err != nil {
		return err
	}
	for _, v := range c.Tests {
		if _, err := tx.ExecContext(ctx, "INSERT INTO tests VALUES (?, ?, ?, ?)", v.ID, v.ScenarioID, v.RunTemplateID, v.AnalysisTemplateID); err != nil {
			return fmt.Errorf("insert test %q (identifier or reference): %w", v.ID, err)
		}
	}
	for _, v := range c.Suites {
		if _, err := tx.ExecContext(ctx, "INSERT INTO suites VALUES (?)", v.ID); err != nil {
			return fmt.Errorf("insert suite %q: %w", v.ID, err)
		}
		for i, id := range v.TestIDs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO suite_tests VALUES (?, ?, ?)", v.ID, i, id); err != nil {
				return fmt.Errorf("suite %q test %q: %w", v.ID, id, err)
			}
		}
	}
	for _, v := range c.Collections {
		if _, err := tx.ExecContext(ctx, "INSERT INTO collections VALUES (?)", v.ID); err != nil {
			return fmt.Errorf("insert collection %q: %w", v.ID, err)
		}
		for i, id := range v.SuiteIDs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO collection_suites VALUES (?, ?, ?)", v.ID, i, id); err != nil {
				return fmt.Errorf("collection %q suite %q: %w", v.ID, id, err)
			}
		}
	}
	if err := checkStoreLimits(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	return nil
}

// Table names below are internal constants, never user-controlled SQL identifiers.
func insertDefinitions[T any](ctx context.Context, tx *sql.Tx, table string, values []T, id func(T) string) error {
	for _, v := range values {
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encode %s: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" VALUES (?, ?)", id(v), string(data)); err != nil {
			return fmt.Errorf("insert %s %q: %w", table, id(v), err)
		}
	}
	return nil
}

// Read returns a consistent catalog with definitions sorted by identifier and
// membership arrays in their original order.
func (s *Store) Read(ctx context.Context) (catalog.Catalog, error) {
	c := catalog.Catalog{Version: 1}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, fmt.Errorf("begin catalog read: %w", err)
	}
	defer tx.Rollback()
	c, err = readCatalog(ctx, tx)
	if err != nil {
		return c, err
	}
	if err := tx.Commit(); err != nil {
		return c, fmt.Errorf("finish catalog read: %w", err)
	}
	return c, nil
}

func readCatalog(ctx context.Context, tx *sql.Tx) (catalog.Catalog, error) {
	c := catalog.Catalog{Version: 1}
	var err error
	if err := checkStoreLimits(ctx, tx); err != nil {
		return c, err
	}
	if c.Scenarios, err = readDefinitions[catalog.Scenario](ctx, tx, "scenarios"); err != nil {
		return c, err
	}
	if c.Controllers, err = readDefinitions[catalog.Controller](ctx, tx, "controllers"); err != nil {
		return c, err
	}
	if c.RunTemplates, err = readDefinitions[catalog.RunTemplate](ctx, tx, "run_templates"); err != nil {
		return c, err
	}
	if c.AnalysisTemplates, err = readDefinitions[catalog.AnalysisTemplate](ctx, tx, "analysis_templates"); err != nil {
		return c, err
	}
	c.Tests = []catalog.Test{}
	c.Suites = []catalog.Suite{}
	c.Collections = []catalog.Collection{}
	rows, err := tx.QueryContext(ctx, "SELECT id, scenario_id, run_template_id, analysis_template_id FROM tests ORDER BY id LIMIT ?", catalog.MaxEntries+1)
	if err != nil {
		return c, fmt.Errorf("read tests: %w", err)
	}
	for rows.Next() {
		var v catalog.Test
		if err := rows.Scan(&v.ID, &v.ScenarioID, &v.RunTemplateID, &v.AnalysisTemplateID); err != nil {
			rows.Close()
			return c, err
		}
		c.Tests = append(c.Tests, v)
	}
	if err := finishRows(rows); err != nil {
		return c, err
	}
	for _, table := range []string{"suites", "collections"} {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM "+table+" ORDER BY id LIMIT ?", catalog.MaxEntries+1)
		if err != nil {
			return c, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return c, err
			}
			if table == "suites" {
				c.Suites = append(c.Suites, catalog.Suite{ID: id, TestIDs: []string{}})
			} else {
				c.Collections = append(c.Collections, catalog.Collection{ID: id, SuiteIDs: []string{}})
			}
		}
		if err := finishRows(rows); err != nil {
			return c, err
		}
	}
	for i := range c.Suites {
		if c.Suites[i].TestIDs, err = readMembers(ctx, tx, "SELECT test_id FROM suite_tests WHERE suite_id = ? ORDER BY position LIMIT ?", c.Suites[i].ID); err != nil {
			return c, err
		}
	}
	for i := range c.Collections {
		if c.Collections[i].SuiteIDs, err = readMembers(ctx, tx, "SELECT suite_id FROM collection_suites WHERE collection_id = ? ORDER BY position LIMIT ?", c.Collections[i].ID); err != nil {
			return c, err
		}
	}
	// An initialized, empty catalog is valid to inspect before its first import.
	if len(c.Scenarios)+len(c.Controllers)+len(c.RunTemplates)+len(c.AnalysisTemplates)+len(c.Tests)+len(c.Suites)+len(c.Collections) > 0 {
		if err := c.Validate(); err != nil {
			return c, fmt.Errorf("stored catalog: %w", err)
		}
	}
	return c, nil
}

func readDefinitions[T any](ctx context.Context, tx *sql.Tx, table string) ([]T, error) {
	values := []T{}
	rows, err := tx.QueryContext(ctx, "SELECT content FROM "+table+" ORDER BY id LIMIT ?", catalog.MaxEntries+1)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var v T
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil, fmt.Errorf("decode stored %s: %w", table, err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func readMembers(ctx context.Context, tx *sql.Tx, query, id string) ([]string, error) {
	ids := []string{}
	rows, err := tx.QueryContext(ctx, query, id, catalog.MaxEntries+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func finishRows(rows *sql.Rows) error { return errors.Join(rows.Err(), rows.Close()) }

func checkStoreLimits(ctx context.Context, tx *sql.Tx) error {
	var total, size int64
	for _, table := range []string{"scenarios", "controllers", "run_templates", "analysis_templates", "tests", "suites", "collections", "suite_tests", "collection_suites"} {
		var count int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return fmt.Errorf("count catalog: %w", err)
		}
		total += count
	}
	for _, table := range []string{"scenarios", "controllers", "run_templates", "analysis_templates"} {
		var bytes int64
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(length(content)), 0) FROM "+table).Scan(&bytes); err != nil {
			return fmt.Errorf("measure catalog: %w", err)
		}
		size += bytes
	}
	if total > catalog.MaxEntries || size > catalog.MaxStoredBytes {
		return fmt.Errorf("catalog exceeds %d records and memberships or %d content bytes", catalog.MaxEntries, catalog.MaxStoredBytes)
	}
	return nil
}
