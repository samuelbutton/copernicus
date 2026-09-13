// Package comparison pairs frozen test selections before querying metric deltas.
package comparison

import (
	"encoding/json"
	"sort"

	"github.com/samuelbutton/copernicus/internal/request"
)

const QueryVersion = 2

// Key excludes controller content and catalog labels other than analysis name.
type Key struct {
	Scenario     string `json:"scenario_sha256"`
	Run          string `json:"run_template_sha256"`
	Simulator    string `json:"simulator_sha256"`
	Source       string `json:"source_sha256"`
	Seed         int64  `json:"seed"`
	Repeat       int    `json:"repeat"`
	AnalysisName string `json:"analysis_name"`
}

func (k Key) ID() string { data, _ := json.Marshal(k); return request.Hash(data) }

type Result struct {
	Path       string  `json:"path"`
	SHA256     string  `json:"sha256"`
	AnalysisID *string `json:"analysis_id"`
}

type Entry struct {
	ExecutionID  string  `json:"execution_id"`
	TestID       string  `json:"test_id"`
	JobID        string  `json:"job_id,omitempty"`
	AnalysisHash string  `json:"analysis_template_sha256"`
	State        string  `json:"state"`
	Reason       string  `json:"reason,omitempty"`
	Result       *Result `json:"result"`
}

type Selection struct {
	AnalysisSelection string          `json:"analysis_selection"`
	RequestID         string          `json:"request_id"`
	SnapshotHash      string          `json:"snapshot_sha256"`
	ControllerHash    string          `json:"controller_sha256"`
	Total             int             `json:"total"`
	Completed         int             `json:"completed"`
	Incomplete        int             `json:"incomplete"`
	Passed            int             `json:"passed"`
	Failed            int             `json:"failed"`
	Warnings          int             `json:"warnings"`
	Errors            int             `json:"errors"`
	ResolutionFailed  int             `json:"resolution_failed"`
	Complete          bool            `json:"complete"`
	Entries           map[Key][]Entry `json:"-"`
}

type Metric struct {
	Name          string `json:"name"`
	Unit          string `json:"unit"`
	Version       int    `json:"version"`
	Baseline      *int64 `json:"baseline"`
	Candidate     *int64 `json:"candidate"`
	Delta         *int64 `json:"delta"`
	BaselinePass  *bool  `json:"baseline_pass"`
	CandidatePass *bool  `json:"candidate_pass"`
	Change        string `json:"change"`
}

type Row struct {
	ID            string   `json:"id"`
	Key           Key      `json:"key"`
	Kind          string   `json:"kind"`
	Reason        string   `json:"reason,omitempty"`
	Baseline      []Entry  `json:"baseline"`
	Candidate     []Entry  `json:"candidate"`
	StatusChanged *bool    `json:"status_changed"`
	Metrics       []Metric `json:"metrics"`
}

type Counts struct {
	Rows               int `json:"rows"`
	Compared           int `json:"compared"`
	Incomparable       int `json:"incomparable"`
	Incomplete         int `json:"incomplete"`
	Errors             int `json:"errors"`
	Added              int `json:"added"`
	Removed            int `json:"removed"`
	StatusChanges      int `json:"status_changes"`
	MetricPairs        int `json:"metric_pairs"`
	AvailableMetrics   int `json:"available_metrics"`
	UnavailableMetrics int `json:"unavailable_metrics"`
	Regressions        int `json:"regressions"`
	Improvements       int `json:"improvements"`
	Unchanged          int `json:"unchanged"`
}

type Report struct {
	View         *ViewInfo `json:"view,omitempty"`
	QueryVersion int       `json:"query_version"`
	Baseline     Selection `json:"baseline"`
	Candidate    Selection `json:"candidate"`
	Counts       Counts    `json:"counts"`
	Rows         []Row     `json:"rows"`
}

// Plan keeps ambiguous matches as groups; it never pairs labels arbitrarily or
// multiplies duplicate-content tests through a many-to-many join.
func Plan(baseline, candidate Selection) Report {
	out := Report{QueryVersion: QueryVersion, Baseline: baseline, Candidate: candidate, Rows: []Row{}}
	keys := map[Key]bool{}
	for key := range baseline.Entries {
		keys[key] = true
	}
	for key := range candidate.Entries {
		keys[key] = true
	}
	for key := range keys {
		row := Row{ID: key.ID(), Key: key, Baseline: append([]Entry{}, baseline.Entries[key]...), Candidate: append([]Entry{}, candidate.Entries[key]...), Metrics: []Metric{}}
		sort.Slice(row.Baseline, func(i, j int) bool { return row.Baseline[i].ExecutionID < row.Baseline[j].ExecutionID })
		sort.Slice(row.Candidate, func(i, j int) bool { return row.Candidate[i].ExecutionID < row.Candidate[j].ExecutionID })
		switch {
		case len(row.Baseline) == 0:
			row.Kind = "ADDED"
		case len(row.Candidate) == 0:
			row.Kind = "REMOVED"
		case len(row.Baseline) != 1 || len(row.Candidate) != 1:
			row.Kind, row.Reason = "INCOMPARABLE", "AMBIGUOUS_PAIRING"
		case row.Baseline[0].AnalysisHash != row.Candidate[0].AnalysisHash:
			row.Kind, row.Reason = "INCOMPARABLE", "ANALYSIS_CONTENT_CHANGED"
		case row.Baseline[0].Result == nil || row.Candidate[0].Result == nil:
			row.Kind, row.Reason = "INCOMPLETE", "RESULT_UNAVAILABLE"
		case row.Baseline[0].State == "ERROR" || row.Candidate[0].State == "ERROR":
			row.Kind, row.Reason = "ERROR", "EXECUTION_ERROR"
		default:
			row.Kind = "COMPARED"
			changed := row.Baseline[0].State != row.Candidate[0].State
			row.StatusChanged = &changed
		}
		out.Rows = append(out.Rows, row)
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].ID < out.Rows[j].ID })
	out.recount()
	return out
}

func (r *Report) recount() {
	r.Counts = Counts{Rows: len(r.Rows)}
	for _, row := range r.Rows {
		switch row.Kind {
		case "COMPARED":
			r.Counts.Compared++
		case "INCOMPARABLE":
			r.Counts.Incomparable++
		case "INCOMPLETE":
			r.Counts.Incomplete++
		case "ERROR":
			r.Counts.Errors++
		case "ADDED":
			r.Counts.Added++
		case "REMOVED":
			r.Counts.Removed++
		}
		if row.StatusChanged != nil && *row.StatusChanged {
			r.Counts.StatusChanges++
		}
		for _, metric := range row.Metrics {
			r.Counts.MetricPairs++
			switch metric.Change {
			case "UNAVAILABLE":
				r.Counts.UnavailableMetrics++
			case "REGRESSION":
				r.Counts.Regressions++
				r.Counts.AvailableMetrics++
			case "IMPROVEMENT":
				r.Counts.Improvements++
				r.Counts.AvailableMetrics++
			case "UNCHANGED":
				r.Counts.Unchanged++
				r.Counts.AvailableMetrics++
			}
		}
	}
}
