package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"sort"

	"github.com/samuelbutton/copernicus/internal/exchange"
)

//go:embed results-v4.sql
var resultsSchema string

const maxIndexBytes = 64 << 20
const maxIndexEntries = 10000

type ImportIssue struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type ImportReport struct {
	Files        int           `json:"files"`
	EventsAdded  int           `json:"events_added"`
	ResultsAdded int           `json:"results_added"`
	Duplicates   int           `json:"duplicates"`
	Rejected     int           `json:"rejected"`
	Issues       []ImportIssue `json:"issues"`
}

// ImportResults validates files outside write transactions. Each event, its
// result, and its deduplication record commit together, so replay repairs exit.
func (s *Store) ImportResults(ctx context.Context, path string, paths []string) (report ImportReport, err error) {
	report.Issues = []ImportIssue{}
	reader, err := exchange.OpenReader(path)
	if err != nil {
		return report, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	if err := s.bindExchange(ctx, reader.Path); err != nil {
		return report, err
	}
	if len(paths) == 0 {
		for _, folder := range []string{"events", "results"} {
			files, err := reader.Candidates(folder)
			if err != nil {
				return report, err
			}
			paths = append(paths, files...)
		}
	}
	if len(paths) > exchange.MaxImportFiles {
		return report, errors.New("too many import files")
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		report.Files++
		observation, readErr := reader.Observe(ctx, path)
		events, results := 0, 0
		if readErr == nil {
			events, results, readErr = s.accept(ctx, observation)
		}
		if readErr != nil {
			report.Rejected++
			if len(report.Issues) < 20 {
				reason := readErr.Error()
				if len(reason) > 300 {
					reason = reason[:300]
				}
				report.Issues = append(report.Issues, ImportIssue{Path: path, Reason: reason})
			}
			continue
		}
		report.EventsAdded += events
		report.ResultsAdded += results
		if events+results == 0 {
			report.Duplicates++
		}
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if report.Rejected > 0 {
		return report, errors.New("some public files were rejected; valid files remain imported, retry after repair")
	}
	return report, nil
}

func (s *Store) accept(ctx context.Context, o exchange.Observation) (int, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := rememberObservation(ctx, tx, o); err != nil {
		return 0, 0, err
	}
	results, err := saveResult(ctx, tx, o.Result)
	if err != nil {
		return 0, 0, err
	}
	events := 0
	if e := o.Event; e != nil {
		r, err := tx.ExecContext(ctx, `INSERT INTO imported_events VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(event_id) DO NOTHING`, e.EventID, o.SHA256, e.JobID, e.ExecutionID, e.CorrelationID, e.AttemptID, e.Sequence, e.State)
		if err != nil {
			return 0, 0, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return 0, 0, err
		}
		events = int(n)
	}
	if err := checkIndexLimits(ctx, tx); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return events, results, nil
}

func rememberObservation(ctx context.Context, tx *sql.Tx, o exchange.Observation) error {
	if o.Result != nil {
		r := o.Result.Result
		if err := checkOwnedJob(ctx, tx, r.JobID, r.ExecutionID, r.CorrelationID, r.Job.SHA256); err != nil {
			return err
		}
	}
	if o.Event != nil {
		e := o.Event
		if err := checkOwnedJob(ctx, tx, e.JobID, e.ExecutionID, e.CorrelationID, ""); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(o.Identities))
	for key := range o.Identities {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, id := range keys {
		hash := o.Identities[id]
		var old string
		err := tx.QueryRowContext(ctx, "SELECT hash FROM imported_identities WHERE id=?", id).Scan(&old)
		if err == nil {
			if old != hash {
				return fmt.Errorf("conflicting published identity %s", id)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO imported_identities VALUES (?, ?)", id, hash); err != nil {
			return err
		}
	}
	return nil
}

func checkOwnedJob(ctx context.Context, tx *sql.Tx, job, execution, correlation, hash string) error {
	var wantExecution, wantRequest, wantHash string
	err := tx.QueryRowContext(ctx, "SELECT execution_id, request_id, content_hash FROM outbox WHERE job_id=?", job).Scan(&wantExecution, &wantRequest, &wantHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if execution != wantExecution || correlation != wantRequest || hash != "" && hash != wantHash {
		return errors.New("public job differs from accepted request")
	}
	return nil
}

func saveResult(ctx context.Context, tx *sql.Tx, file *exchange.ResultFile) (int, error) {
	if file == nil {
		return 0, nil
	}
	r := file.Result
	result, err := tx.ExecContext(ctx, `INSERT INTO indexed_results VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`, r.Identity(), r.JobID, r.ExecutionID, r.CorrelationID, r.Job.SHA256, file.Path, file.SHA256, string(file.Content))
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

func checkIndexLimits(ctx context.Context, tx *sql.Tx) error {
	var results, events, identities, size int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM indexed_results), (SELECT count(*) FROM imported_events), (SELECT count(*) FROM imported_identities), (SELECT coalesce(sum(length(CAST(content AS BLOB))),0) FROM indexed_results)`).Scan(&results, &events, &identities, &size); err != nil {
		return err
	}
	if results > maxIndexEntries || events > maxIndexEntries || identities > maxIndexEntries*10 || size > maxIndexBytes {
		return errors.New("result index exceeds storage limits")
	}
	return nil
}

// RebuildResults stages validated result files, then atomically replaces only
// the derived result index. Identity history, requests, and events are retained.
func (s *Store) RebuildResults(ctx context.Context, path string) (count int, err error) {
	reader, err := exchange.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	if err := s.bindExchange(ctx, reader.Path); err != nil {
		return 0, err
	}
	paths, err := reader.Candidates("results")
	if err != nil {
		return 0, err
	}
	observations := make([]exchange.Observation, 0, len(paths))
	size := 0
	for _, path := range paths {
		o, err := reader.Observe(ctx, path)
		if err != nil {
			return 0, fmt.Errorf("rebuild %s: %w", path, err)
		}
		size += len(o.Result.Content)
		if size > maxIndexBytes {
			return 0, errors.New("rebuild exceeds staging limit")
		}
		observations = append(observations, o)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM indexed_results"); err != nil {
		return 0, err
	}
	for _, o := range observations {
		if err := rememberObservation(ctx, tx, o); err != nil {
			return 0, err
		}
		n, err := saveResult(ctx, tx, o.Result)
		if err != nil {
			return 0, err
		}
		count += n
	}
	if err := checkIndexLimits(ctx, tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
