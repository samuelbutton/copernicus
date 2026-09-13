package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
)

func fixture(t *testing.T) catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func submission() Submission {
	return Submission{ID: "review-one", CollectionID: "all-tests", ControllerID: "baseline", Requester: "reviewer", Priority: 1, Seed: 23, Repeat: 2}
}
func resolve(t *testing.T, c catalog.Catalog, s Submission) Snapshot {
	t.Helper()
	source, err := compatibility.Yamata()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Resolve(c, s, source)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func frozenValues(s Snapshot) []Frozen {
	values := append([]Frozen{s.Source, s.Controller, s.Collection}, s.Suites...)
	for _, e := range s.Executions {
		values = append(values, e.Test, e.Scenario, e.RunTemplate, e.AnalysisTemplate, e.Simulator)
	}
	return values
}
func TestSnapshotHashesAndIsolation(t *testing.T) {
	c := fixture(t)
	s := submission()
	snapshot := resolve(t, c, s)
	first, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != Resolved || len(snapshot.Executions) != 3 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Executions[0].Test.ID != "stopped-obstacle" || snapshot.Executions[1].Test.ID != "empty-lane" || snapshot.Executions[2].Test.ID != "moving-obstacle" {
		t.Fatal("lost collection order")
	}
	seen := map[string]bool{}
	for _, e := range snapshot.Executions {
		if e.Status != Ready || e.Reason != "" || e.Seed != 23 || e.Repeat != 2 || seen[e.ID] {
			t.Fatalf("execution = %+v", e)
		}
		seen[e.ID] = true
	}
	for _, v := range frozenValues(snapshot) {
		if v.SHA256 != Hash(v.Content) {
			t.Fatalf("wrong hash for %q", v.ID)
		}
		var content map[string]json.RawMessage
		if err := json.Unmarshal(v.Content, &content); err != nil {
			t.Fatal(err)
		}
		if _, ok := content["id"]; ok {
			t.Fatal("catalog label leaked into content hash")
		}
	}
	raw, _ := json.Marshal(s)
	if first.SnapshotSHA256 != Hash(first.Snapshot) || first.SubmissionSHA256 != Hash(raw) {
		t.Fatal("incorrect record hashes")
	}
	again, _ := Encode(resolve(t, c, s))
	if !reflect.DeepEqual(first, again) {
		t.Fatal("same inputs changed bytes")
	}
	c.Suites[0].TestIDs[0] = "moving-obstacle"
	c.Scenarios[1].Obstacles[0].PositionMM = 30000
	c.Controllers[0].Version = 2
	unchanged, _ := Encode(snapshot)
	if !reflect.DeepEqual(first, unchanged) {
		t.Fatal("snapshot aliases catalog memory")
	}
}

func TestContentHashExcludesCatalogLabel(t *testing.T) {
	a := fixture(t).Scenarios[0]
	b := a
	b.ID = "renamed"
	frozenA, err := Freeze(a.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	frozenB, err := Freeze(b.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	if frozenA.SHA256 != frozenB.SHA256 {
		t.Fatal("label changed scenario content identity")
	}
	b.StartSpeedMMS++
	changed, _ := Freeze(b.ID, b)
	if changed.SHA256 == frozenA.SHA256 {
		t.Fatal("content change did not change hash")
	}
}

func TestDistinctTestsHaveDistinctExecutions(t *testing.T) {
	c := fixture(t)
	copy := c.Tests[0]
	copy.ID = "same-inputs"
	c.Tests = append(c.Tests, copy)
	c.Suites[0].TestIDs = append(c.Suites[0].TestIDs, copy.ID)
	s := resolve(t, c, submission())
	seen := map[string]bool{}
	for _, e := range s.Executions {
		if seen[e.ID] {
			t.Fatal("distinct tests share an execution")
		}
		seen[e.ID] = true
	}
	if len(seen) != 4 {
		t.Fatal("distinct test was dropped")
	}
}

func TestIncompatibleInputsAreRecorded(t *testing.T) {
	cases := []struct {
		name, reason string
		change       func(*catalog.Catalog)
	}{
		{"type", "UNSUPPORTED_SCENARIO_TYPE", func(c *catalog.Catalog) { c.Scenarios[1].Type = "other" }},
		{"mismatch", "SCENARIO_TYPE_MISMATCH", func(c *catalog.Catalog) { c.RunTemplates[0].ScenarioType = "other" }},
		{"simulator", "UNSUPPORTED_SIMULATOR", func(c *catalog.Catalog) { c.RunTemplates[0].Simulator.Version = 2 }},
		{"controller", "UNSUPPORTED_CONTROLLER", func(c *catalog.Catalog) { c.Controllers[0].Name = "unavailable" }},
		{"metric", "UNSUPPORTED_METRIC_VERSION", func(c *catalog.Catalog) { c.AnalysisTemplates[0].CollisionCount.Version = 2 }},
		{"limits", "UNSUPPORTED_LIMITS", func(c *catalog.Catalog) { c.RunTemplates[0].TickMS = 1001 }},
		{"collision-limit", "UNSUPPORTED_LIMITS", func(c *catalog.Catalog) { c.AnalysisTemplates[0].CollisionCount.Maximum = 1 }},
		{"geometry", "UNSUPPORTED_GEOMETRY", func(c *catalog.Catalog) { c.Scenarios[1].Obstacles[0].SpeedMMS = 1000001 }},
		{"coast", "UNSUPPORTED_GEOMETRY", func(c *catalog.Catalog) {
			c.Scenarios[1].StartPositionMM = 999999998
			c.Scenarios[1].GoalPositionMM = 1000000000
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := fixture(t)
			tt.change(&c)
			s := resolve(t, c, submission())
			if s.Status != ResolutionFailed || s.Executions[0].Status != ResolutionFailed || s.Executions[0].Reason != tt.reason {
				t.Fatalf("resolution = %+v", s.Executions[0])
			}
			if tt.name == "type" && s.Executions[1].Status != Ready {
				t.Fatal("unrelated test marked incompatible")
			}
		})
	}
}

func TestInvalidSubmissionAndMissingInputs(t *testing.T) {
	source, _ := compatibility.Yamata()
	for _, change := range []func(*Submission){func(s *Submission) { s.ID = "" }, func(s *Submission) { s.Requester = "../bad" }, func(s *Submission) { s.Priority = 4 }, func(s *Submission) { s.Seed = -1 }, func(s *Submission) { s.Repeat = 100001 }, func(s *Submission) { s.CollectionID = "missing" }, func(s *Submission) { s.ControllerID = "missing" }} {
		s := submission()
		change(&s)
		if _, err := Resolve(fixture(t), s, source); err == nil {
			t.Fatal("accepted invalid submission")
		}
	}
	c := fixture(t)
	c.Collections[0].SuiteIDs = []string{}
	if _, err := Resolve(c, submission(), source); err == nil {
		t.Fatal("accepted empty request")
	}
	c = fixture(t)
	c.Tests[0].ScenarioID = "missing"
	if _, err := Resolve(c, submission(), source); err == nil {
		t.Fatal("accepted broken reference")
	}
}

func TestRequestBounds(t *testing.T) {
	c := fixture(t)
	template := c.Tests[0]
	for i := 0; i < MaxTests; i++ {
		v := template
		v.ID = fmt.Sprintf("extra-%d", i)
		c.Tests = append(c.Tests, v)
		c.Suites[0].TestIDs = append(c.Suites[0].TestIDs, v.ID)
	}
	source, _ := compatibility.Yamata()
	if _, err := Resolve(c, submission(), source); err == nil {
		t.Fatal("accepted oversized test selection")
	}
	s := resolve(t, fixture(t), submission())
	s.Executions[0].Scenario.Content = json.RawMessage(`{"padding":"` + strings.Repeat("x", MaxSnapshotBytes) + `"}`)
	if _, err := Encode(s); err == nil {
		t.Fatal("accepted oversized snapshot")
	}
}

func FuzzResolve(f *testing.F) {
	source, err := compatibility.Yamata()
	if err != nil {
		f.Fatal(err)
	}
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data, int64(0), 0)
	f.Fuzz(func(t *testing.T, data []byte, seed int64, repeat int) {
		c, err := catalog.Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		input := submission()
		input.Seed = seed
		input.Repeat = repeat
		snapshot, err := Resolve(c, input, source)
		if err != nil {
			return
		}
		first, err := Encode(snapshot)
		if err != nil {
			return
		}
		second, err := Resolve(c, input, source)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Encode(second)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Fatal("resolution is not deterministic")
		}
		for _, v := range frozenValues(snapshot) {
			if Hash(v.Content) != v.SHA256 {
				t.Fatal("frozen content hash mismatch")
			}
		}
	})
}

func TestSelectedSuitesAreFrozenWithoutChangingCatalog(t *testing.T) {
	c := fixture(t)
	original, _ := json.Marshal(c)
	input := submission()
	input.SuiteIDs = []string{"smoke"}
	snapshot := resolve(t, c, input)
	if len(snapshot.Executions) != 2 || len(snapshot.Suites) != 1 || snapshot.Suites[0].ID != "smoke" {
		t.Fatal("selection did not narrow request")
	}
	input.SuiteIDs[0] = "obstacles"
	if snapshot.Submission.SuiteIDs[0] != "smoke" {
		t.Fatal("submission aliases caller")
	}
	after, _ := json.Marshal(c)
	if !bytes.Equal(original, after) {
		t.Fatal("selection changed catalog")
	}
	for _, suites := range [][]string{{}, {"unknown"}, {"smoke", "smoke"}, {"../smoke"}} {
		input.SuiteIDs = suites
		source, _ := compatibility.Yamata()
		if _, err := Resolve(c, input, source); err == nil {
			t.Fatalf("accepted suites %v", suites)
		}
	}
	legacy, _ := json.Marshal(submission())
	if strings.Contains(string(legacy), "suite_ids") {
		t.Fatal("changed legacy submission identity")
	}
}
