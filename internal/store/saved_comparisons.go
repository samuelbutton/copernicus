package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/samuelbutton/copernicus/internal/comparison"
	"github.com/samuelbutton/copernicus/internal/request"
)

const maxComparisonBytes = 4 << 20

// CompareView validates current inputs before attempting any saved-result lookup.
// Read-only stores may reuse saved pages but never create or evict entries.
func (s *Store) CompareView(ctx context.Context, baseline, candidate, baselineAnalysis, candidateAnalysis, duckdb string, options comparison.Options) (comparison.Report, error) {
	if err := options.Validate(); err != nil {
		return comparison.Report{}, err
	}
	plan, files, err := s.comparisonInputs(ctx, baseline, candidate, baselineAnalysis, candidateAnalysis)
	if err != nil {
		return comparison.Report{}, err
	}
	terminal := plan.Baseline.Complete && plan.Candidate.Complete
	var inputs []byte
	key := ""
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return comparison.Report{}, err
	}
	if terminal && version >= 6 {
		inputs, err = comparison.CacheInputs(plan, options)
		if err != nil {
			return comparison.Report{}, err
		}
		if len(inputs) > maxComparisonBytes {
			return comparison.Report{}, errors.New("comparison inputs exceed limit")
		}
		key = request.Hash(inputs)
		cached, err := s.savedComparison(ctx, key, inputs)
		if err == nil {
			cached.View.CacheState = "hit"
			return cached, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return comparison.Report{}, err
		}
	}
	report, err := comparison.Evaluate(ctx, duckdb, plan, files)
	if err != nil {
		return report, err
	}
	report = comparison.Select(report, options)
	report.View.CacheState = "read_only"
	if !terminal {
		report.View.CacheState = "bypass_partial"
		return report, nil
	}
	report.View.CacheKey = key
	if s.writable && version >= 6 {
		report.View.CacheState = "saved"
		if err := s.saveComparison(ctx, key, inputs, report); err != nil {
			return comparison.Report{}, err
		}
	}
	return report, nil
}
func (s *Store) savedComparison(ctx context.Context, key string, inputs []byte) (comparison.Report, error) {
	var report comparison.Report
	var stored, content, hash string
	err := s.db.QueryRowContext(ctx, `SELECT inputs,content,content_hash FROM saved_comparisons WHERE cache_key=? AND length(CAST(inputs AS BLOB))<=? AND length(CAST(content AS BLOB))<=?`, key, maxComparisonBytes, maxComparisonBytes).Scan(&stored, &content, &hash)
	if err != nil {
		return report, err
	}
	if stored != string(inputs) || request.Hash([]byte(stored)) != key || request.Hash([]byte(content)) != hash {
		return report, errors.New("saved comparison integrity check failed")
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return report, err
	}
	if report.View == nil || report.View.CacheKey != key || report.QueryVersion != comparison.QueryVersion || !report.Baseline.Complete || !report.Candidate.Complete {
		return report, errors.New("invalid saved comparison")
	}
	return report, nil
}
func (s *Store) saveComparison(ctx context.Context, key string, inputs []byte, report comparison.Report) error {
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if len(data) > maxComparisonBytes {
		return errors.New("saved comparison exceeds limit")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO saved_comparisons VALUES(?,?,?,?) ON CONFLICT(cache_key) DO NOTHING", key, string(inputs), string(data), request.Hash(data)); err != nil {
		return err
	}
	// FIFO bounds keep this derived cache local and disposable. Eviction never
	// touches requests, reservations, selected analyses, or published evidence.
	for {
		var count, size int
		if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(CAST(inputs AS BLOB))+length(CAST(content AS BLOB))),0) FROM saved_comparisons").Scan(&count, &size); err != nil {
			return err
		}
		if count <= 128 && size <= 32<<20 {
			break
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM saved_comparisons WHERE rowid=(SELECT min(rowid) FROM saved_comparisons)"); err != nil {
			return err
		}
	}
	return tx.Commit()
}
