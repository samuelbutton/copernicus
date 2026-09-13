package compatibility

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

type Reference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Metric struct {
	Value         *int64 `json:"value"`
	Version       int    `json:"version"`
	Unit          string `json:"unit"`
	Pass          *bool  `json:"pass"`
	EvidenceTicks []int  `json:"evidence_ticks"`
}
type Result struct {
	ExecutionID      string            `json:"execution_id"`
	JobID            string            `json:"job_id"`
	AttemptID        string            `json:"attempt_id"`
	CorrelationID    string            `json:"correlation_id"`
	Job              Reference         `json:"job"`
	InputsHash       string            `json:"inputs_hash"`
	Bag              *Reference        `json:"bag"`
	AnalysisID       *string           `json:"analysis_id"`
	AnalysisTemplate json.RawMessage   `json:"analysis_template"`
	AnalysisHash     *string           `json:"analysis_hash"`
	Status           string            `json:"status"`
	FailureClass     *string           `json:"failure_class"`
	Metrics          map[string]Metric `json:"metrics"`
}

func (r Result) Identity() string {
	analysis := "run"
	if r.AnalysisID != nil {
		analysis = *r.AnalysisID
	}
	return "result/" + r.ExecutionID + "/" + analysis
}

type Event struct {
	ExecutionID   string     `json:"execution_id"`
	JobID         string     `json:"job_id"`
	AttemptID     string     `json:"attempt_id"`
	CorrelationID string     `json:"correlation_id"`
	EventID       string     `json:"event_id"`
	Sequence      int64      `json:"sequence"`
	State         string     `json:"state"`
	AnalysisID    *string    `json:"analysis_id"`
	Result        *Reference `json:"result"`
}
type BagHeader struct {
	ExecutionID string `json:"execution_id"`
	InputsHash  string `json:"inputs_hash"`
	TickMS      int64  `json:"tick_ms"`
	RecordCount int    `json:"record_count"`
}

func Terminal(state string) bool {
	return state == "PASS" || state == "FAIL" || state == "WARN" || state == "ERROR"
}

func ParseEvent(data []byte) (Event, error) {
	var event Event
	if err := ValidateDocument(data, "event", &event); err != nil {
		return event, err
	}
	sum := hashBytes([]byte(event.JobID + "\n" + event.AttemptID + "\n" + strconv.FormatInt(event.Sequence, 10)))
	if event.EventID != sum {
		return event, errors.New("event identity mismatch")
	}
	if Terminal(event.State) != (event.Result != nil) {
		return event, errors.New("event result availability mismatch")
	}
	if (event.State == "PENDING" || event.State == "RUNNING") && event.AnalysisID != nil || event.State == "ANALYZING" && event.AnalysisID == nil {
		return event, errors.New("event analysis availability mismatch")
	}
	return event, nil
}

func CheckEventResult(e Event, r Result) error {
	if e.ExecutionID != r.ExecutionID || e.JobID != r.JobID || e.AttemptID != r.AttemptID || e.CorrelationID != r.CorrelationID || e.State != r.Status || !equalOptional(e.AnalysisID, r.AnalysisID) {
		return errors.New("event differs from result")
	}
	return nil
}
func equalOptional(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// CheckResult verifies published relationships and score semantics. It does not
// rerun simulation or scoring, and accepts explicitly unavailable metric values.
func CheckResult(r Result, j Job, bag BagHeader) error {
	if r.ExecutionID != j.ExecutionID || r.JobID != j.JobID || r.CorrelationID != j.CorrelationID || r.InputsHash != j.InputsHash {
		return errors.New("result differs from job")
	}
	if (r.Status == "ERROR") != (r.FailureClass != nil) {
		return errors.New("result failure class contradicts status")
	}
	if r.AnalysisID == nil {
		if r.Status != "ERROR" || j.JobKind != "run" || *r.FailureClass == "analysis_failure" || r.Bag != nil || r.AnalysisHash != nil || string(r.AnalysisTemplate) != "null" || r.Metrics != nil {
			return errors.New("invalid run failure")
		}
		return nil
	}
	if r.Bag == nil || r.AnalysisHash == nil || bag.ExecutionID != r.ExecutionID {
		return errors.New("missing analysis or wrong recording")
	}
	var inputs struct {
		AnalysisTemplate json.RawMessage `json:"analysis_template"`
		Bag              *Reference      `json:"bag"`
		RunTemplate      struct {
			TickMS int64 `json:"tick_ms"`
		} `json:"run_template"`
		Limits struct {
			MaxTicks int `json:"max_ticks"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(j.Inputs, &inputs); err != nil {
		return err
	}
	if j.JobKind == "run" && (bag.InputsHash != j.InputsHash || bag.TickMS != inputs.RunTemplate.TickMS || bag.RecordCount > inputs.Limits.MaxTicks+1) {
		return errors.New("recording differs from run inputs")
	}
	if j.JobKind == "analysis" && (inputs.Bag == nil || *inputs.Bag != *r.Bag) {
		return errors.New("analysis uses a different recording")
	}
	hash, err := ContentHash(r.AnalysisTemplate)
	if err != nil {
		return err
	}
	want, err := ContentHash(inputs.AnalysisTemplate)
	if err != nil {
		return err
	}
	if hash != want || hash != *r.AnalysisHash || *r.AnalysisID != hashBytes([]byte(r.ExecutionID+"\n"+r.Bag.SHA256+"\n"+hash)) {
		return errors.New("analysis identity or hash mismatch")
	}
	if r.Status == "ERROR" {
		if r.Metrics != nil || (*r.FailureClass != "worker_failure" && *r.FailureClass != "analysis_failure") {
			return errors.New("invalid analysis failure")
		}
		return nil
	}
	return checkMetrics(r, bag.RecordCount)
}

func checkMetrics(r Result, records int) error {
	if len(r.Metrics) != 3 {
		return errors.New("completed analysis requires three metrics")
	}
	var limits map[string]struct {
		Version    int   `json:"version"`
		MinimumMM  int64 `json:"minimum_mm"`
		MinimumPPM int64 `json:"minimum_ppm"`
	}
	if err := json.Unmarshal(r.AnalysisTemplate, &limits); err != nil {
		return err
	}
	status := "PASS"
	for name, unit := range map[string]string{"collision_count": "count", "minimum_obstacle_gap": "mm", "goal_progress": "ppm"} {
		metric := r.Metrics[name]
		if metric.Version != limits[name].Version || metric.Unit != unit {
			return errors.New("metric version or unit mismatch")
		}
		for _, tick := range metric.EvidenceTicks {
			if tick >= records {
				return errors.New("evidence outside recording")
			}
		}
		if metric.Value == nil {
			if metric.Pass != nil || len(metric.EvidenceTicks) != 0 {
				return errors.New("unavailable metric cannot pass or cite evidence")
			}
			if status != "FAIL" {
				status = "WARN"
			}
			continue
		}
		if metric.Pass == nil {
			return errors.New("available metric requires a pass flag")
		}
		value, pass := *metric.Value, false
		switch name {
		case "collision_count":
			if value < 0 {
				return errors.New("negative collision count")
			}
			pass = value == 0
		case "minimum_obstacle_gap":
			pass = value >= limits[name].MinimumMM
		case "goal_progress":
			if value < 0 || value > 1000000 {
				return errors.New("progress outside range")
			}
			pass = value >= limits[name].MinimumPPM
		}
		if pass != *metric.Pass {
			return errors.New("metric pass flag contradicts score limit")
		}
		if !pass {
			status = "FAIL"
		}
	}
	if status != r.Status {
		return errors.New("result status contradicts metrics")
	}
	return nil
}

const MaxBagBytes = 16 << 20

// ParseBag checks every line before a referenced recording can support a result.
func ParseBag(ctx context.Context, data []byte) (BagHeader, error) {
	var header BagHeader
	if len(data) > MaxBagBytes || !bytes.HasSuffix(data, []byte("\n")) {
		return header, errors.New("recording exceeds 16 MiB or lacks final newline")
	}
	lines := bytes.SplitN(data[:len(data)-1], []byte("\n"), 100003)
	if err := ValidateDocument(lines[0], "bag", &header); err != nil {
		return BagHeader{}, err
	}
	if len(lines)-1 != header.RecordCount {
		return BagHeader{}, errors.New("recording count mismatch")
	}
	for i, line := range lines[1:] {
		if err := ctx.Err(); err != nil {
			return BagHeader{}, err
		}
		var tick struct {
			Tick   int   `json:"tick"`
			TimeMS int64 `json:"time_ms"`
		}
		if err := ValidateDocument(line, "tick", &tick); err != nil {
			return BagHeader{}, fmt.Errorf("tick %d: %w", i, err)
		}
		if tick.Tick != i || tick.TimeMS != int64(i)*header.TickMS {
			return BagHeader{}, errors.New("unordered recording ticks")
		}
	}
	return header, nil
}
