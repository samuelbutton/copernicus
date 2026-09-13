package store

import (
	"context"
	"fmt"
	"github.com/samuelbutton/copernicus/internal/catalog"
)

// ReplaceSuite changes only an existing suite's ordered membership. Existing
// request snapshots have no foreign keys to these mutable membership rows.
func (s *Store) ReplaceSuite(ctx context.Context, suite catalog.Suite) error {
	if err := (catalog.Catalog{Version: 1, Suites: []catalog.Suite{suite}}).Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin suite update: %w", err)
	}
	defer tx.Rollback()
	var exists string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM suites WHERE id = ?", suite.ID).Scan(&exists); err != nil {
		return fmt.Errorf("suite %q: %w", suite.ID, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM suite_tests WHERE suite_id = ?", suite.ID); err != nil {
		return fmt.Errorf("clear membership: %w", err)
	}
	for i, id := range suite.TestIDs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO suite_tests VALUES (?, ?, ?)", suite.ID, i, id); err != nil {
			return fmt.Errorf("suite %q test %q: %w", suite.ID, id, err)
		}
	}
	if err := checkStoreLimits(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit suite update: %w", err)
	}
	return nil
}
