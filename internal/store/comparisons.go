package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samuelbutton/copernicus/internal/comparison"
	"github.com/samuelbutton/copernicus/internal/exchange"
	"github.com/samuelbutton/copernicus/internal/request"
)

// Compare uses frozen request membership and freshly validated result bytes.
// It saves no comparison or analytical database.
func (s *Store) Compare(ctx context.Context, baseline, candidate, duckdb string) (comparison.Report, error) {
	files := map[string][]byte{}
	size := 0
	accept := func(file exchange.ResultFile) error {
		if previous, ok := files[file.Result.JobID]; ok {
			if request.Hash(previous) != file.SHA256 {
				return errors.New("result changed during comparison")
			}
			return nil
		}
		size += len(file.Content)
		if size > comparison.MaxResultBytes {
			return errors.New("comparison exceeds result byte limit")
		}
		files[file.Result.JobID] = file.Content
		return nil
	}
	b, err := s.selection(ctx, baseline, accept)
	if err != nil {
		return comparison.Report{}, err
	}
	c := b
	if baseline != candidate {
		c, err = s.selection(ctx, candidate, accept)
		if err != nil {
			return comparison.Report{}, err
		}
	}
	return comparison.Evaluate(ctx, duckdb, comparison.Plan(b, c), files)
}

func (s *Store) selection(ctx context.Context, id string, accept func(exchange.ResultFile) error) (comparison.Selection, error) {
	var out comparison.Selection
	record, err := s.Request(ctx, id)
	if err != nil {
		return out, err
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		return out, err
	}
	p, err := s.progress(ctx, id, accept)
	if err != nil {
		return out, err
	}
	out = comparison.Selection{RequestID: id, SnapshotHash: record.SnapshotSHA256, ControllerHash: snapshot.Controller.SHA256, Total: p.Total, Completed: p.Completed, Incomplete: p.Incomplete, Passed: p.Passed, Failed: p.Failed, Warnings: p.Warnings, Errors: p.Errors, ResolutionFailed: p.ResolutionFailed, Complete: p.Complete, Entries: map[comparison.Key][]comparison.Entry{}}
	for i, e := range snapshot.Executions {
		state := p.Executions[i]
		key := comparison.Key{Scenario: e.Scenario.SHA256, Run: e.RunTemplate.SHA256, Simulator: e.Simulator.SHA256, Source: snapshot.Source.SHA256, Seed: e.Seed, Repeat: e.Repeat, AnalysisName: e.AnalysisTemplate.ID}
		entry := comparison.Entry{ExecutionID: e.ID, TestID: e.Test.ID, JobID: state.JobID, AnalysisHash: e.AnalysisTemplate.SHA256, State: state.State, Reason: state.Reason}
		if state.Result != nil {
			entry.Result = &comparison.Result{Path: state.Result.Path, SHA256: state.Result.SHA256, AnalysisID: state.Result.AnalysisID}
		}
		out.Entries[key] = append(out.Entries[key], entry)
	}
	return out, nil
}
