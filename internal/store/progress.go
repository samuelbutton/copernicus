package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/exchange"
	"github.com/samuelbutton/copernicus/internal/request"
)

type ResultReference struct {
	Path       string  `json:"path"`
	SHA256     string  `json:"sha256"`
	AnalysisID *string `json:"analysis_id"`
}
type ExecutionProgress struct {
	ExecutionID string           `json:"execution_id"`
	TestID      string           `json:"test_id"`
	JobID       string           `json:"job_id,omitempty"`
	State       string           `json:"state"`
	Reason      string           `json:"reason,omitempty"`
	Result      *ResultReference `json:"result,omitempty"`
}
type RequestProgress struct {
	RequestID        string              `json:"request_id"`
	Total            int                 `json:"total"`
	Completed        int                 `json:"completed"`
	Incomplete       int                 `json:"incomplete"`
	Passed           int                 `json:"passed"`
	Failed           int                 `json:"failed"`
	Warnings         int                 `json:"warnings"`
	Errors           int                 `json:"errors"`
	ResolutionFailed int                 `json:"resolution_failed"`
	Complete         bool                `json:"complete"`
	Executions       []ExecutionProgress `json:"executions"`
}

// Progress reads one consistent database view, then revalidates referenced files
// outside the transaction. A missing or changed file cannot remain a cached pass.
func (s *Store) Progress(ctx context.Context, id string) (RequestProgress, error) {
	out := RequestProgress{RequestID: id, Executions: []ExecutionProgress{}}
	if err := catalog.ValidateID(id); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	record, _, err := readRequest(ctx, tx, id)
	if err != nil {
		return out, err
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		return out, err
	}
	var source string
	if err := tx.QueryRowContext(ctx, "SELECT coalesce((SELECT path FROM exchange_destination), '')").Scan(&source); err != nil {
		return out, err
	}
	type saved struct{ hash, path, fileHash string }
	files := make([]saved, 0, len(snapshot.Executions))
	for _, e := range snapshot.Executions {
		p := ExecutionProgress{ExecutionID: e.ID, TestID: e.Test.ID, State: e.Status, Reason: e.Reason}
		var file saved
		if e.Status == request.Ready {
			var stage, published int
			err := tx.QueryRowContext(ctx, `SELECT o.job_id, o.content_hash, o.published,
			 coalesce((SELECT max(CASE state WHEN 'PENDING' THEN 1 WHEN 'RUNNING' THEN 2 WHEN 'ANALYZING' THEN 3 ELSE 4 END) FROM imported_events WHERE job_id=o.job_id AND execution_id=o.execution_id AND correlation_id=o.request_id),0),
			 coalesce(r.path,''),coalesce(r.hash,'') FROM outbox o LEFT JOIN indexed_results r ON r.job_id=o.job_id AND r.execution_id=o.execution_id AND r.correlation_id=o.request_id AND r.job_hash=o.content_hash WHERE o.execution_id=?`, e.ID).Scan(&p.JobID, &file.hash, &published, &stage, &file.path, &file.fileHash)
			if err != nil {
				return out, err
			}
			p.State = "QUEUED"
			if published == 1 {
				p.State = "PUBLISHED"
			}
			if stage > 0 {
				p.State = map[int]string{1: "PENDING", 2: "RUNNING", 3: "ANALYZING", 4: "INCOMPLETE"}[stage]
			}
		}
		out.Executions = append(out.Executions, p)
		files = append(files, file)
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	var reader *exchange.Reader
	var openErr error
	if source != "" {
		reader, openErr = exchange.OpenReader(source)
		if openErr == nil {
			defer reader.Close()
		}
	}
	for i := range out.Executions {
		if err := ctx.Err(); err != nil {
			return RequestProgress{}, err
		}
		p, file := &out.Executions[i], files[i]
		if file.path != "" {
			p.State, p.Reason = "INCOMPLETE", "RESULT_UNAVAILABLE"
			if reader != nil && openErr == nil {
				o, err := reader.Observe(ctx, file.path)
				if err == nil && o.SHA256 == file.fileHash && o.Result.Result.Job.SHA256 == file.hash && o.Result.Result.JobID == p.JobID && o.Result.Result.ExecutionID == p.ExecutionID && o.Result.Result.CorrelationID == id {
					r := o.Result.Result
					p.State, p.Reason = r.Status, ""
					if r.FailureClass != nil {
						p.Reason = *r.FailureClass
					}
					p.Result = &ResultReference{Path: file.path, SHA256: file.fileHash, AnalysisID: r.AnalysisID}
				}
			}
		}
		switch p.State {
		case "PASS":
			out.Passed++
		case "FAIL":
			out.Failed++
		case "WARN":
			out.Warnings++
		case "ERROR":
			out.Errors++
		case request.ResolutionFailed:
			out.ResolutionFailed++
		}
		if compatibility.Terminal(p.State) {
			out.Completed++
		}
	}
	if err := ctx.Err(); err != nil {
		return RequestProgress{}, err
	}
	out.Total = len(out.Executions)
	out.Incomplete = out.Total - out.Completed
	out.Complete = out.Completed == out.Total
	return out, nil
}

type IndexStatus struct {
	Events     int `json:"processed_events"`
	Results    int `json:"indexed_results"`
	Unassigned int `json:"unassigned_results"`
}

const assignedJoin = ` LEFT JOIN outbox o ON o.job_id=r.job_id AND o.execution_id=r.execution_id AND o.request_id=r.correlation_id AND o.content_hash=r.job_hash `

func (s *Store) IndexStatus(ctx context.Context) (IndexStatus, error) {
	var status IndexStatus
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM imported_events),count(*),coalesce(sum(o.job_id IS NULL),0) FROM indexed_results r`+assignedJoin).Scan(&status.Events, &status.Results, &status.Unassigned)
	return status, err
}

type IndexedResult struct {
	ID          string `json:"id"`
	ExecutionID string `json:"execution_id"`
	JobID       string `json:"job_id"`
	RequestID   string `json:"request_id,omitempty"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	State       string `json:"state"`
}
type ResultPage struct {
	Results   []IndexedResult `json:"results"`
	NextAfter string          `json:"next_after,omitempty"`
}

func (s *Store) Results(ctx context.Context, after string, limit int) (ResultPage, error) {
	out := ResultPage{Results: []IndexedResult{}}
	if limit < 1 || limit > 100 || len(after) > 200 {
		return out, errors.New("result page requires limit 1–100 and a bounded cursor")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.execution_id,r.job_id,coalesce(o.request_id,''),r.path,r.hash FROM indexed_results r`+assignedJoin+`WHERE r.id>? ORDER BY r.id LIMIT ?`, after, limit+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var r IndexedResult
		if err := rows.Scan(&r.ID, &r.ExecutionID, &r.JobID, &r.RequestID, &r.Path, &r.SHA256); err != nil {
			rows.Close()
			return out, err
		}
		out.Results = append(out.Results, r)
	}
	if err := finishRows(rows); err != nil {
		return out, err
	}
	if len(out.Results) > limit {
		out.Results = out.Results[:limit]
		out.NextAfter = out.Results[limit-1].ID
	}
	var source string
	if err := s.db.QueryRowContext(ctx, "SELECT coalesce((SELECT path FROM exchange_destination), '')").Scan(&source); err != nil {
		return out, err
	}
	reader, openErr := exchange.OpenReader(source)
	if openErr == nil {
		defer reader.Close()
	}
	for i := range out.Results {
		if err := ctx.Err(); err != nil {
			return ResultPage{}, err
		}
		r := &out.Results[i]
		r.State = "INCOMPLETE"
		if openErr == nil {
			o, err := reader.Observe(ctx, r.Path)
			if err == nil && o.SHA256 == r.SHA256 && o.Result.Result.Identity() == r.ID {
				r.State = o.Result.Result.Status
			}
		}
	}
	return out, ctx.Err()
}

type RequestPage struct {
	IDs       []string `json:"request_ids"`
	NextAfter string   `json:"next_after,omitempty"`
}

func (s *Store) Requests(ctx context.Context, after string, limit int) (RequestPage, error) {
	out := RequestPage{IDs: []string{}}
	if limit < 1 || limit > 100 || len(after) > 64 {
		return out, errors.New("request page requires limit 1–100 and a bounded cursor")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM requests WHERE id>? ORDER BY id LIMIT ?", after, limit+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		out.IDs = append(out.IDs, id)
	}
	if err := finishRows(rows); err != nil {
		return out, err
	}
	if len(out.IDs) > limit {
		out.IDs = out.IDs[:limit]
		out.NextAfter = out.IDs[limit-1]
	}
	return out, nil
}
