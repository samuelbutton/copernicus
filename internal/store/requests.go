package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

const maxRequests = 1000
const maxRequestBytes = 64 << 20

// CreateRequest returns the original snapshot on an identical submission retry.
// Lookup precedes resolution so later catalog edits cannot redefine an accepted ID.
func (s *Store) CreateRequest(ctx context.Context, input request.Submission, source compatibility.Source) (request.Record, error) {
	var empty request.Record
	if err := input.Validate(); err != nil {
		return empty, fmt.Errorf("%w: %w", request.ErrInvalidSubmission, err)
	}
	submission, err := json.Marshal(input)
	if err != nil {
		return empty, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, fmt.Errorf("begin request: %w", err)
	}
	defer tx.Rollback()
	existing, accepted, err := readRequest(ctx, tx, input.ID)
	if err == nil {
		if !bytes.Equal(accepted, submission) {
			return empty, request.ErrIdentityConflict
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	c, err := readCatalog(ctx, tx)
	if err != nil {
		return empty, err
	}
	snapshot, err := request.Resolve(c, input, source)
	if err != nil {
		return empty, fmt.Errorf("%w: %w", request.ErrInvalidSubmission, err)
	}
	record, err := request.Encode(snapshot)
	if err != nil {
		return empty, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO requests VALUES (?, ?, ?, ?, ?)", input.ID, string(submission), record.SubmissionSHA256, string(record.Snapshot), record.SnapshotSHA256); err != nil {
		return empty, fmt.Errorf("save request: %w", err)
	}
	if err := reserveBudget(ctx, tx, snapshot); err != nil {
		return empty, err
	}
	for i, e := range snapshot.Executions {
		if _, err := tx.ExecContext(ctx, "INSERT INTO executions VALUES (?, ?, ?, ?, ?, ?)", e.ID, input.ID, i, e.Test.ID, e.Status, e.Reason); err != nil {
			return empty, fmt.Errorf("save execution: %w", err)
		}
	}
	if err := saveJobs(ctx, tx, snapshot); err != nil {
		return empty, err
	}
	var count, size int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(length(snapshot)), 0) FROM requests").Scan(&count, &size); err != nil {
		return empty, fmt.Errorf("measure requests: %w", err)
	}
	if count > maxRequests || size > maxRequestBytes {
		return empty, fmt.Errorf("request store exceeds %d requests or %d snapshot bytes", maxRequests, maxRequestBytes)
	}
	if err := tx.Commit(); err != nil {
		return empty, fmt.Errorf("commit request: %w", err)
	}
	return record, nil
}

// Request reads one complete saved snapshot without consulting the live catalog.
func (s *Store) Request(ctx context.Context, id string) (request.Record, error) {
	if err := catalog.ValidateID(id); err != nil {
		return request.Record{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return request.Record{}, fmt.Errorf("begin request read: %w", err)
	}
	defer tx.Rollback()
	record, _, err := readRequest(ctx, tx, id)
	if err != nil {
		return record, fmt.Errorf("read request %q: %w", id, err)
	}
	return record, tx.Commit()
}

func readRequest(ctx context.Context, tx *sql.Tx, id string) (request.Record, []byte, error) {
	var record request.Record
	var submission, data string
	// Bound data read even when a database was changed outside this application.
	err := tx.QueryRowContext(ctx, "SELECT submission, submission_hash, snapshot, snapshot_hash FROM requests WHERE id = ? AND length(snapshot) <= ? AND length(submission) <= ?", id, request.MaxSnapshotBytes, request.MaxSubmissionBytes).Scan(&submission, &record.SubmissionSHA256, &data, &record.SnapshotSHA256)
	if err != nil {
		return record, nil, err
	}
	record.Snapshot = []byte(data)
	if request.Hash([]byte(submission)) != record.SubmissionSHA256 || request.Hash(record.Snapshot) != record.SnapshotSHA256 {
		return record, nil, errors.New("stored request hash mismatch")
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		return record, nil, fmt.Errorf("decode saved snapshot: %w", err)
	}
	encoded, err := json.Marshal(snapshot.Submission)
	if err != nil {
		return record, nil, err
	}
	if snapshot.Version != request.SnapshotVersion || snapshot.Submission.ID != id || !bytes.Equal(encoded, []byte(submission)) {
		return record, nil, errors.New("stored request identity mismatch")
	}
	rows, err := tx.QueryContext(ctx, "SELECT id, position, test_id, resolution_status, reason FROM executions WHERE request_id = ? ORDER BY position LIMIT ?", id, request.MaxTests+1)
	if err != nil {
		return record, nil, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var executionID, testID, status, reason string
		var position int
		if err := rows.Scan(&executionID, &position, &testID, &status, &reason); err != nil {
			return record, nil, err
		}
		if n >= len(snapshot.Executions) {
			return record, nil, errors.New("unexpected execution record")
		}
		expected := snapshot.Executions[n]
		if executionID != expected.ID || position != n || testID != expected.Test.ID || status != expected.Status || reason != expected.Reason {
			return record, nil, errors.New("stored execution differs from snapshot")
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return record, nil, err
	}
	if n != len(snapshot.Executions) || n == 0 || n > request.MaxTests {
		return record, nil, errors.New("incomplete request execution records")
	}
	return record, []byte(submission), nil
}
