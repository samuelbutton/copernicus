package adapter

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

// AnalysisJob pins the original recording and a complete scoring configuration.
// Identity follows the public analysis identity, so equivalent templates reuse jobs.
func AnalysisJob(snapshot request.Snapshot, execution request.Execution, template request.Frozen, bag compatibility.Reference) (Job, error) {
	var source compatibility.Source
	if err := thaw(snapshot.Source, &source); err != nil {
		return Job{}, err
	}
	pinned, err := compatibility.Yamata()
	if err != nil {
		return Job{}, err
	}
	if source != pinned || execution.Status != request.Ready {
		return Job{}, errors.New("unsupported execution source or resolution")
	}
	var analysis catalog.AnalysisTemplate
	if err := thaw(template, &analysis); err != nil {
		return Job{}, err
	}
	analysis.ID = template.ID
	if err := (catalog.Catalog{Version: 1, AnalysisTemplates: []catalog.AnalysisTemplate{analysis}}).Validate(); err != nil {
		return Job{}, err
	}
	if reason := request.AnalysisCompatibilityFailure(analysis); reason != "" {
		return Job{}, fmt.Errorf("unsupported scoring configuration: %s", reason)
	}
	inputs, err := json.Marshal(struct {
		Bag      compatibility.Reference `json:"bag"`
		Template json.RawMessage         `json:"analysis_template"`
	}{bag, template.Content})
	if err != nil {
		return Job{}, err
	}
	digest, err := compatibility.ContentHash(inputs)
	if err != nil {
		return Job{}, err
	}
	analysisHash, err := compatibility.ContentHash(template.Content)
	if err != nil {
		return Job{}, err
	}
	identity := request.Hash([]byte(execution.ID + "\n" + bag.SHA256 + "\n" + analysisHash))
	id := "a" + identity[:63]
	job := compatibility.Job{ContractVersion: 1, Kind: "job", JobKind: "analysis", JobID: id, ExecutionID: execution.ID, Priority: snapshot.Submission.Priority, CorrelationID: snapshot.Submission.ID, Inputs: inputs, InputsHash: digest}
	data, err := json.Marshal(job)
	if err != nil {
		return Job{}, err
	}
	data = append(data, '\n')
	if _, err := compatibility.ParseJob(data); err != nil {
		return Job{}, err
	}
	return Job{ID: id, ExecutionID: execution.ID, SHA256: request.Hash(data), Content: data}, nil
}
