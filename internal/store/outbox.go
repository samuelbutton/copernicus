package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/adapter"
	"github.com/samuelbutton/copernicus/internal/exchange"
	"github.com/samuelbutton/copernicus/internal/request"
)

//go:embed outbox-v3.sql
var outboxSchema string

const maxOutboxJobs = 10000
const maxOutboxBytes = 64 << 20
const MaxPublishBatch = 1000

type OutboxStatus struct {
	Destination string `json:"exchange_dir"`
	Pending     int    `json:"pending"`
	Published   int    `json:"published"`
	Bytes       int64  `json:"bytes"`
}

// Outbox reports delivery state without loading job bodies or changing storage.
func (s *Store) Outbox(ctx context.Context) (OutboxStatus, error) {
	var status OutboxStatus
	err := s.db.QueryRowContext(ctx, `SELECT coalesce((SELECT path FROM exchange_destination), ''),
		coalesce(sum(published = 0), 0), coalesce(sum(published = 1), 0), coalesce(sum(length(content)), 0) FROM outbox`).Scan(&status.Destination, &status.Pending, &status.Published, &status.Bytes)
	return status, err
}

func saveJobs(ctx context.Context, tx *sql.Tx, snapshot request.Snapshot) error {
	jobs, err := adapter.Jobs(snapshot)
	if err != nil {
		return fmt.Errorf("adapt request: %w", err)
	}
	for _, job := range jobs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(job_id, execution_id, request_id, content, content_hash) VALUES (?, ?, ?, ?, ?)`, job.ID, job.ExecutionID, snapshot.Submission.ID, string(job.Content), job.SHA256); err != nil {
			return fmt.Errorf("save outgoing job: %w", err)
		}
	}
	return checkOutboxLimits(ctx, tx)
}

func checkOutboxLimits(ctx context.Context, tx *sql.Tx) error {
	var count, size int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(length(content)), 0) FROM outbox").Scan(&count, &size); err != nil {
		return err
	}
	if count > maxOutboxJobs || size > maxOutboxBytes {
		return fmt.Errorf("outbox exceeds %d jobs or %d bytes", maxOutboxJobs, maxOutboxBytes)
	}
	return nil
}

// upgradeOutbox uses accepted snapshots, never the current catalog. The caller's
// schema transaction includes every backfilled job and the version change.
func upgradeOutbox(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, outboxSchema); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM requests ORDER BY id LIMIT ?", maxRequests+1)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	if len(ids) > maxRequests {
		return errors.New("too many saved requests to upgrade")
	}
	for _, id := range ids {
		record, _, err := readRequest(ctx, tx, id)
		if err != nil {
			return err
		}
		var snapshot request.Snapshot
		if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
			return err
		}
		if err := saveJobs(ctx, tx, snapshot); err != nil {
			return fmt.Errorf("backfill request %s: %w", id, err)
		}
	}
	return nil
}

func (s *Store) bindExchange(ctx context.Context, path string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO exchange_destination VALUES (1, ?) ON CONFLICT DO NOTHING", path); err != nil {
		return err
	}
	var accepted string
	if err := tx.QueryRowContext(ctx, "SELECT path FROM exchange_destination WHERE singleton = 1").Scan(&accepted); err != nil {
		return err
	}
	if accepted != path {
		return errors.New("database is bound to a different exchange directory; inspect with outbox show")
	}
	return tx.Commit()
}

// PublishOutbox acknowledges each file only after durable publication. Failure
// leaves that job pending, including process exit after rename but before ack.
// Concurrent publishers may deliver the same bytes; only one records the ack.
func (s *Store) PublishOutbox(ctx context.Context, directory string, limit int) (count int, err error) {
	if limit < 1 || limit > MaxPublishBatch {
		return 0, fmt.Errorf("limit must be 1–%d", MaxPublishBatch)
	}
	destination, err := exchange.Open(directory)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, destination.Close()) }()
	if err := s.bindExchange(ctx, destination.Path); err != nil {
		return 0, err
	}
	for range limit {
		var sequence int64
		var id, executionID, requestID, data, digest string
		err := s.db.QueryRowContext(ctx, `SELECT sequence, job_id, execution_id, request_id, CASE WHEN length(content) <= 1048576 THEN content ELSE NULL END, content_hash FROM outbox WHERE published = 0 ORDER BY sequence LIMIT 1`).Scan(&sequence, &id, &executionID, &requestID, &data, &digest)
		if errors.Is(err, sql.ErrNoRows) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		if len(data) > compatibility.MaxJobBytes || request.Hash([]byte(data)) != digest {
			return count, errors.New("outbox content hash or size mismatch")
		}
		job, err := compatibility.ParseJob([]byte(data))
		if err != nil {
			return count, err
		}
		if job.JobID != id || job.ExecutionID != executionID || job.CorrelationID != requestID || (job.JobKind != "run" && job.JobKind != "analysis") {
			return count, errors.New("outbox job identity mismatch")
		}
		if err := destination.Publish(ctx, []byte(data)); err != nil {
			return count, fmt.Errorf("publish %s (still pending): %w", id, err)
		}
		result, err := s.db.ExecContext(ctx, "UPDATE outbox SET published = 1 WHERE sequence = ? AND content_hash = ? AND published = 0", sequence, digest)
		if err != nil {
			return count, fmt.Errorf("job file published; acknowledgement failed, retry publication: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return count, err
		}
		count += int(n)
	}
	return count, nil
}
