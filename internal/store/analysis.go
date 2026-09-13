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
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/exchange"
	"github.com/samuelbutton/copernicus/internal/request"
)

//go:embed analysis-v5.sql
var analysisSchema string

const OriginalAnalysis = "original"

// AnalysisSelection binds one template to an exact job for every frozen execution.
// The reserved original selection uses each execution's original scoring template.
type AnalysisSelection struct {
	ID       string            `json:"id"`
	Template *request.Frozen   `json:"template"`
	Jobs     map[string]string `json:"jobs,omitempty"`
}

func readAnalysis(ctx context.Context, tx *sql.Tx, id, selection string) (AnalysisSelection, error) {
	out := AnalysisSelection{ID: selection}
	if selection == OriginalAnalysis {
		return out, nil
	}
	if err := catalog.ValidateID(selection); err != nil {
		return out, err
	}
	var content, hash string
	if err := tx.QueryRowContext(ctx, "SELECT content,content_hash FROM analysis_selections WHERE request_id=? AND id=? AND length(CAST(content AS BLOB))<=1048576", id, selection).Scan(&content, &hash); err != nil {
		return out, err
	}
	if request.Hash([]byte(content)) != hash {
		return out, errors.New("analysis selection hash mismatch")
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return out, err
	}
	if out.ID != selection || out.Template == nil || out.Template.ID != selection || request.Hash(out.Template.Content) != out.Template.SHA256 || len(out.Jobs) == 0 || len(out.Jobs) > request.MaxTests {
		return out, errors.New("invalid saved analysis selection")
	}
	for execution, job := range out.Jobs {
		if catalog.ValidateID(execution) != nil || catalog.ValidateID(job) != nil {
			return out, errors.New("invalid analysis job mapping")
		}
	}
	return out, nil
}
func (s *Store) Analysis(ctx context.Context, id, selection string) (AnalysisSelection, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AnalysisSelection{}, err
	}
	defer tx.Rollback()
	if _, _, err := readRequest(ctx, tx, id); err != nil {
		return AnalysisSelection{}, err
	}
	out, err := readAnalysis(ctx, tx, id, selection)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Store) Analyses(ctx context.Context, id string) ([]AnalysisSelection, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, _, err := readRequest(ctx, tx, id); err != nil {
		return nil, err
	}
	out := []AnalysisSelection{{ID: OriginalAnalysis}}
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	if version >= 5 {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM analysis_selections WHERE request_id=? ORDER BY id LIMIT 1001", id)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, value)
		}
		if err := finishRows(rows); err != nil {
			return nil, err
		}
		if len(ids) > 1000 {
			return nil, errors.New("too many analysis selections")
		}
		for _, value := range ids {
			entry, err := readAnalysis(ctx, tx, id, value)
			if err != nil {
				return nil, err
			}
			entry.Jobs = nil
			out = append(out, entry)
		}
	}
	return out, tx.Commit()
}

// CreateAnalysis atomically saves a complete selection and any new outgoing jobs.
// An accepted retry succeeds even after a recording becomes unavailable.
func (s *Store) CreateAnalysis(ctx context.Context, id, templateID string) (AnalysisSelection, error) {
	var empty AnalysisSelection
	if catalog.ValidateID(id) != nil || catalog.ValidateID(templateID) != nil || templateID == OriginalAnalysis {
		return empty, fmt.Errorf("%w: invalid analysis selection", request.ErrInvalidSubmission)
	}
	accepted, err := s.Analysis(ctx, id, templateID)
	if err == nil {
		return accepted, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	record, err := s.Request(ctx, id)
	if err != nil {
		return empty, err
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		return empty, err
	}
	outcomes := map[string]compatibility.Result{}
	progress, err := s.progress(ctx, id, func(file exchange.ResultFile) error { outcomes[file.Result.ExecutionID] = file.Result; return nil })
	if err != nil {
		return empty, err
	}
	if len(outcomes) != len(snapshot.Executions) || !progress.Complete {
		return empty, fmt.Errorf("%w: every test needs a validated original recording", request.ErrInvalidSubmission)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	// Another caller can accept the same selection during file validation.
	accepted, err = readAnalysis(ctx, tx, id, templateID)
	if err == nil {
		return accepted, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	var content string
	if err := tx.QueryRowContext(ctx, "SELECT content FROM analysis_templates WHERE id=?", templateID).Scan(&content); err != nil {
		return empty, fmt.Errorf("%w: scoring template unavailable", request.ErrInvalidSubmission)
	}
	var template catalog.AnalysisTemplate
	if err := json.Unmarshal([]byte(content), &template); err != nil {
		return empty, err
	}
	frozen, err := request.Freeze(templateID, template)
	if err != nil {
		return empty, err
	}
	out := AnalysisSelection{ID: templateID, Template: &frozen, Jobs: map[string]string{}}
	for _, execution := range snapshot.Executions {
		result := outcomes[execution.ID]
		if result.Bag == nil {
			return empty, fmt.Errorf("%w: a selected result has no recording", request.ErrInvalidSubmission)
		}
		job, err := adapter.AnalysisJob(snapshot, execution, frozen, *result.Bag)
		if err != nil {
			return empty, fmt.Errorf("%w: %w", request.ErrInvalidSubmission, err)
		}
		// An unchanged scoring configuration already belongs to the original run.
		if frozen.SHA256 == execution.AnalysisTemplate.SHA256 {
			out.Jobs[execution.ID] = result.JobID
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(job_id,execution_id,request_id,content,content_hash) VALUES(?,?,?,?,?) ON CONFLICT(job_id) DO NOTHING`, job.ID, execution.ID, id, string(job.Content), job.SHA256); err != nil {
			return empty, err
		}
		var hash string
		if err := tx.QueryRowContext(ctx, "SELECT content_hash FROM outbox WHERE job_id=?", job.ID).Scan(&hash); err != nil {
			return empty, err
		}
		if hash != job.SHA256 {
			return empty, errors.New("analysis job identity conflict")
		}
		out.Jobs[execution.ID] = job.ID
	}
	if err := checkOutboxLimits(ctx, tx); err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return empty, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO analysis_selections VALUES(?,?,?,?)", id, templateID, string(encoded), request.Hash(encoded)); err != nil {
		return empty, err
	}
	var count, size int
	if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(CAST(content AS BLOB))),0) FROM analysis_selections").Scan(&count, &size); err != nil {
		return empty, err
	}
	if count > 1000 || size > 64<<20 {
		return empty, errors.New("analysis selections exceed storage limits")
	}
	return out, tx.Commit()
}
