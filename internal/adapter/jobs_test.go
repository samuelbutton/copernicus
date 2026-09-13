package adapter

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

func snapshot(t *testing.T, controller string) request.Snapshot {
	t.Helper()
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	source, err := compatibility.Yamata()
	if err != nil {
		t.Fatal(err)
	}
	s, err := request.Resolve(c, request.Submission{ID: "review-one", CollectionID: "all-tests", ControllerID: controller, Requester: "reviewer", Priority: 3, Seed: 9007199254740991, Repeat: 100000}, source)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFrozenJobMappingAndStableBytes(t *testing.T) {
	for _, controller := range []string{"baseline", "candidate"} {
		s := snapshot(t, controller)
		jobs, err := Jobs(s)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 3 {
			t.Fatal("lost executions")
		}
		for i, job := range jobs {
			parsed, err := compatibility.ParseJob(job.Content)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.ExecutionID != s.Executions[i].ID || parsed.CorrelationID != s.Submission.ID || parsed.Priority != 3 || job.SHA256 != request.Hash(job.Content) {
				t.Fatal("wrong identity or file hash")
			}
			var input map[string]json.RawMessage
			if err := json.Unmarshal(parsed.Inputs, &input); err != nil {
				t.Fatal(err)
			}
			if string(input["seed"]) != "9007199254740991" || string(input["repeat"]) != "100000" {
				t.Fatal("lost integer precision")
			}
			for key, frozen := range map[string]request.Frozen{"scenario": s.Executions[i].Scenario, "controller": s.Controller, "simulator": s.Executions[i].Simulator, "analysis_template": s.Executions[i].AnalysisTemplate} {
				if !bytes.Equal(input[key], frozen.Content) {
					t.Fatalf("changed %s", key)
				}
			}
			var run, limits map[string]int64
			if err := json.Unmarshal(input["run_template"], &run); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(input["limits"], &limits); err != nil {
				t.Fatal(err)
			}
			var frozen catalog.RunTemplate
			if err := json.Unmarshal(s.Executions[i].RunTemplate.Content, &frozen); err != nil {
				t.Fatal(err)
			}
			if len(run) != 1 || run["tick_ms"] != frozen.TickMS || len(limits) != 2 || limits["max_ticks"] != frozen.MaxTicks || limits["timeout_ms"] != frozen.TimeoutMS {
				t.Fatal("incorrect run template translation")
			}
		}
		again, err := Jobs(s)
		if err != nil || !reflect.DeepEqual(jobs, again) {
			t.Fatal("job retry changed bytes")
		}
	}
}

func TestAdapterMatchesPublishedExample(t *testing.T) {
	data, err := os.ReadFile("../../compatibility/contract/v1/examples/valid/jobs/run-1.json")
	if err != nil {
		t.Fatal(err)
	}
	public, err := compatibility.ParseJob(data)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Scenario         catalog.Scenario         `json:"scenario"`
		Controller       catalog.Controller       `json:"controller"`
		Simulator        catalog.Implementation   `json:"simulator"`
		RunTemplate      catalog.RunTemplate      `json:"run_template"`
		AnalysisTemplate catalog.AnalysisTemplate `json:"analysis_template"`
		Limits           struct {
			MaxTicks  int64 `json:"max_ticks"`
			TimeoutMS int64 `json:"timeout_ms"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(public.Inputs, &input); err != nil {
		t.Fatal(err)
	}
	s := snapshot(t, "candidate")
	s.Submission.Seed, s.Submission.Repeat = 0, 0
	s.Executions = s.Executions[:1]
	e := &s.Executions[0]
	e.Seed, e.Repeat = 0, 0
	input.RunTemplate.ScenarioType = "lane"
	input.RunTemplate.Simulator = input.Simulator
	input.RunTemplate.MaxTicks, input.RunTemplate.TimeoutMS = input.Limits.MaxTicks, input.Limits.TimeoutMS
	for _, v := range []struct {
		target *request.Frozen
		value  any
	}{{&e.Scenario, input.Scenario}, {&s.Controller, input.Controller}, {&e.Simulator, input.Simulator}, {&e.RunTemplate, input.RunTemplate}, {&e.AnalysisTemplate, input.AnalysisTemplate}} {
		*v.target, err = request.Freeze("example", v.value)
		if err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := Jobs(s)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := compatibility.ParseJob(jobs[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if actual.InputsHash != public.InputsHash {
		t.Fatal("adapter differs from published input hash")
	}
}

func TestAdapterFailsClosed(t *testing.T) {
	s := snapshot(t, "baseline")
	s.Executions[0].Status, s.Executions[0].Reason = request.ResolutionFailed, "UNSUPPORTED_SCENARIO_TYPE"
	s.Status = request.ResolutionFailed
	jobs, err := Jobs(s)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("failed execution was dispatched: %v", err)
	}
	for _, change := range []func(*request.Snapshot){
		func(s *request.Snapshot) { s.Version++ },
		func(s *request.Snapshot) { s.Executions[0].Scenario.SHA256 = "changed" },
		func(s *request.Snapshot) { s.Executions[0].Seed++ },
		func(s *request.Snapshot) { s.Executions[0].Status = "UNKNOWN" },
		func(s *request.Snapshot) { s.Executions[1].ID = s.Executions[0].ID },
		func(s *request.Snapshot) {
			s.Source.Content = json.RawMessage(`{"repository":"https://github.com/samuelbutton/yamata","revision":"0000000000000000000000000000000000000000","contract_version":1}`)
			s.Source.SHA256 = request.Hash(s.Source.Content)
		},
		func(s *request.Snapshot) {
			s.Executions[0].Scenario.Content = bytes.Replace(s.Executions[0].Scenario.Content, []byte(`"type":"lane"`), []byte(`"type":"unknown"`), 1)
			s.Executions[0].Scenario.SHA256 = request.Hash(s.Executions[0].Scenario.Content)
		},
	} {
		s := snapshot(t, "baseline")
		change(&s)
		if _, err := Jobs(s); err == nil {
			t.Fatal("accepted invalid READY snapshot")
		}
	}
}
