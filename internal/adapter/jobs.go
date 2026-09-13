// Package adapter translates frozen requests into the pinned public file contract.
package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

// Job retains exact publication bytes. Retrying delivery never encodes it again.
type Job struct {
	ID, ExecutionID, SHA256 string
	Content                 []byte
}

// Jobs emits one run job per READY execution, preserving snapshot order.
// A resolution failure emits no job; malformed READY content rejects the batch.
func Jobs(snapshot request.Snapshot) ([]Job, error) {
	if snapshot.Version != request.SnapshotVersion {
		return nil, errors.New("unsupported snapshot version")
	}
	if err := snapshot.Submission.Validate(); err != nil {
		return nil, err
	}
	if len(snapshot.Executions) == 0 || len(snapshot.Executions) > request.MaxTests {
		return nil, errors.New("invalid execution count")
	}
	var source compatibility.Source
	if err := thaw(snapshot.Source, &source); err != nil {
		return nil, err
	}
	pinned, err := compatibility.Yamata()
	if err != nil {
		return nil, err
	}
	if source != pinned {
		return nil, errors.New("snapshot source differs from the pinned adapter")
	}
	var controller catalog.Controller
	if err := thaw(snapshot.Controller, &controller); err != nil {
		return nil, err
	}
	jobs := []Job{}
	seen := map[string]bool{}
	for _, execution := range snapshot.Executions {
		if err := catalog.ValidateID(execution.ID); err != nil {
			return nil, err
		}
		if seen[execution.ID] {
			return nil, errors.New("duplicate execution identifier")
		}
		seen[execution.ID] = true
		if execution.Status == request.ResolutionFailed && execution.Reason != "" {
			continue
		}
		if execution.Status != request.Ready || execution.Reason != "" {
			return nil, errors.New("invalid execution resolution")
		}
		job, err := runJob(snapshot, execution, controller)
		if err != nil {
			return nil, fmt.Errorf("execution %s: %w", execution.ID, err)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func runJob(snapshot request.Snapshot, e request.Execution, controller catalog.Controller) (Job, error) {
	var scenario catalog.Scenario
	var run catalog.RunTemplate
	var analysis catalog.AnalysisTemplate
	var simulator catalog.Implementation
	for _, part := range []struct {
		frozen request.Frozen
		target any
	}{
		{e.Scenario, &scenario}, {e.RunTemplate, &run}, {e.AnalysisTemplate, &analysis}, {e.Simulator, &simulator},
	} {
		if err := thaw(part.frozen, part.target); err != nil {
			return Job{}, err
		}
	}
	if simulator != run.Simulator || e.Seed != snapshot.Submission.Seed || e.Repeat != snapshot.Submission.Repeat {
		return Job{}, errors.New("inconsistent frozen inputs")
	}
	if reason := request.CompatibilityFailure(scenario, run, analysis, controller); reason != "" {
		return Job{}, fmt.Errorf("incompatible inputs: %s", reason)
	}
	inputs := struct {
		Scenario    json.RawMessage `json:"scenario"`
		Controller  json.RawMessage `json:"controller"`
		Simulator   json.RawMessage `json:"simulator"`
		RunTemplate struct {
			TickMS int64 `json:"tick_ms"`
		} `json:"run_template"`
		AnalysisTemplate json.RawMessage `json:"analysis_template"`
		Seed             int64           `json:"seed"`
		Repeat           int             `json:"repeat"`
		Limits           struct {
			MaxTicks  int64 `json:"max_ticks"`
			TimeoutMS int64 `json:"timeout_ms"`
		} `json:"limits"`
	}{Scenario: e.Scenario.Content, Controller: snapshot.Controller.Content, Simulator: e.Simulator.Content, AnalysisTemplate: e.AnalysisTemplate.Content, Seed: e.Seed, Repeat: e.Repeat}
	inputs.RunTemplate.TickMS = run.TickMS
	inputs.Limits.MaxTicks, inputs.Limits.TimeoutMS = run.MaxTicks, run.TimeoutMS
	raw, err := json.Marshal(inputs)
	if err != nil {
		return Job{}, err
	}
	digest, err := compatibility.ContentHash(raw)
	if err != nil {
		return Job{}, err
	}
	// A prefixed digest stays within the public 64-character identifier limit.
	id := "j" + request.Hash([]byte("run\n" + e.ID))[:63]
	job := compatibility.Job{ContractVersion: 1, Kind: "job", ExecutionID: e.ID, JobID: id, JobKind: "run", Priority: snapshot.Submission.Priority, CorrelationID: snapshot.Submission.ID, Inputs: raw, InputsHash: digest}
	data, err := json.Marshal(job)
	if err != nil {
		return Job{}, err
	}
	data = append(data, '\n')
	if _, err := compatibility.ParseJob(data); err != nil {
		return Job{}, err
	}
	return Job{ID: id, ExecutionID: e.ID, SHA256: request.Hash(data), Content: data}, nil
}

func thaw(f request.Frozen, target any) error {
	if request.Hash(f.Content) != f.SHA256 {
		return errors.New("frozen content hash mismatch")
	}
	d := json.NewDecoder(bytes.NewReader(f.Content))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("decode frozen %s: %w", f.ID, err)
	}
	return nil
}
