package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/exchange"
	"github.com/samuelbutton/copernicus/internal/request"
)

// Review joins one frozen execution to freshly validated outcome evidence.
// It accepts identifiers, never caller-supplied filesystem paths.
type Review struct {
	RequestID    string                `json:"request_id"`
	SnapshotHash string                `json:"snapshot_sha256"`
	Controller   request.Frozen        `json:"controller"`
	Source       request.Frozen        `json:"source"`
	Execution    request.Execution     `json:"execution"`
	Progress     ExecutionProgress     `json:"progress"`
	Outcome      *compatibility.Result `json:"outcome"`
}

func (s *Store) Review(ctx context.Context, id, executionID string) (Review, error) {
	var out Review
	record, err := s.Request(ctx, id)
	if err != nil {
		return out, err
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		return out, err
	}
	found := false
	for _, execution := range snapshot.Executions {
		if execution.ID == executionID {
			out.Execution = execution
			found = true
			break
		}
	}
	if !found {
		return out, sql.ErrNoRows
	}
	out.RequestID, out.SnapshotHash, out.Controller, out.Source = id, record.SnapshotSHA256, snapshot.Controller, snapshot.Source
	progress, err := s.progress(ctx, id, func(file exchange.ResultFile) error {
		if file.Result.ExecutionID == executionID {
			result := file.Result
			out.Outcome = &result
		}
		return nil
	})
	if err != nil {
		return Review{}, err
	}
	for _, execution := range progress.Executions {
		if execution.ExecutionID == executionID {
			out.Progress = execution
			break
		}
	}
	return out, nil
}

// Evidence returns only a tick cited by the currently validated selected result.
func (s *Store) Evidence(ctx context.Context, id, executionID string, tick int) (json.RawMessage, error) {
	review, err := s.Review(ctx, id, executionID)
	if err != nil {
		return nil, err
	}
	if review.Outcome == nil || review.Outcome.Bag == nil {
		return nil, sql.ErrNoRows
	}
	cited := false
	for _, metric := range review.Outcome.Metrics {
		for _, value := range metric.EvidenceTicks {
			if value == tick {
				cited = true
			}
		}
	}
	if !cited {
		return nil, sql.ErrNoRows
	}
	var source string
	if err := s.db.QueryRowContext(ctx, "SELECT path FROM exchange_destination").Scan(&source); err != nil {
		return nil, err
	}
	reader, err := exchange.OpenReader(source)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return reader.Tick(ctx, *review.Outcome.Bag, tick)
}
