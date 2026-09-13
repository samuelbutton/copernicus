package request

import (
	"fmt"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
)

// Resolve freezes one collection. Incompatible tests remain in the snapshot as
// explicit resolution failures; no engine command or job is created here.
func Resolve(c catalog.Catalog, submission Submission, source compatibility.Source) (Snapshot, error) {
	var out Snapshot
	if err := submission.Validate(); err != nil {
		return out, err
	}
	if err := source.Validate(); err != nil {
		return out, err
	}
	if err := c.Validate(); err != nil {
		return out, err
	}
	tests, err := c.Expand(submission.CollectionID)
	if err != nil {
		return out, err
	}
	if len(tests) == 0 || len(tests) > MaxTests {
		return out, fmt.Errorf("request must select 1–%d unique tests", MaxTests)
	}
	controllers := index(c.Controllers, func(v catalog.Controller) string { return v.ID })
	controller, ok := controllers[submission.ControllerID]
	if !ok {
		return out, fmt.Errorf("controller %q not found", submission.ControllerID)
	}
	collections := index(c.Collections, func(v catalog.Collection) string { return v.ID })
	suites := index(c.Suites, func(v catalog.Suite) string { return v.ID })
	scenarios := index(c.Scenarios, func(v catalog.Scenario) string { return v.ID })
	runs := index(c.RunTemplates, func(v catalog.RunTemplate) string { return v.ID })
	analyses := index(c.AnalysisTemplates, func(v catalog.AnalysisTemplate) string { return v.ID })
	out = Snapshot{Version: SnapshotVersion, Submission: submission, Status: Resolved, Suites: []Frozen{}, Executions: []Execution{}}
	// All values below are concrete JSON-only model types; this helper retains
	// one error path while keeping the relationship mapping explicit.
	var freezeErr error
	size := 0
	freeze := func(id string, value any) Frozen {
		if freezeErr != nil {
			return Frozen{}
		}
		v, err := Freeze(id, value)
		if err != nil {
			freezeErr = err
			return Frozen{}
		}
		size += len(v.Content)
		if size > MaxSnapshotBytes {
			freezeErr = fmt.Errorf("snapshot content exceeds %d bytes", MaxSnapshotBytes)
		}
		return v
	}
	out.Source = freeze("yamata", source)
	out.Collection = freeze(submission.CollectionID, collections[submission.CollectionID])
	out.Controller = freeze(controller.ID, controller)
	for _, id := range collections[submission.CollectionID].SuiteIDs {
		out.Suites = append(out.Suites, freeze(id, suites[id]))
	}
	for _, test := range tests {
		scenario, ok := scenarios[test.ScenarioID]
		if !ok {
			return Snapshot{}, fmt.Errorf("scenario %q not found", test.ScenarioID)
		}
		run, ok := runs[test.RunTemplateID]
		if !ok {
			return Snapshot{}, fmt.Errorf("run template %q not found", test.RunTemplateID)
		}
		analysis, ok := analyses[test.AnalysisTemplateID]
		if !ok {
			return Snapshot{}, fmt.Errorf("analysis template %q not found", test.AnalysisTemplateID)
		}
		e := Execution{Test: freeze(test.ID, test), Scenario: freeze(scenario.ID, scenario), RunTemplate: freeze(run.ID, run), AnalysisTemplate: freeze(analysis.ID, analysis), Simulator: freeze(run.Simulator.Name, run.Simulator), Seed: submission.Seed, Repeat: submission.Repeat, Status: Ready}
		if freezeErr != nil {
			return Snapshot{}, freezeErr
		}
		e.Reason = compatibilityFailure(scenario, run, analysis, controller)
		if e.Reason != "" {
			e.Status = ResolutionFailed
			out.Status = ResolutionFailed
		}
		identity := struct {
			Submission                                                Submission
			TestID, Test, Scenario, Run, Analysis, Controller, Source string
		}{submission, test.ID, e.Test.SHA256, e.Scenario.SHA256, e.RunTemplate.SHA256, e.AnalysisTemplate.SHA256, out.Controller.SHA256, out.Source.SHA256}
		key, err := Freeze("execution", identity)
		if err != nil {
			return Snapshot{}, err
		}
		e.ID = "e" + key.SHA256[:63]
		out.Executions = append(out.Executions, e)
	}
	return out, freezeErr
}

func index[T any](values []T, id func(T) string) map[string]T {
	result := make(map[string]T, len(values))
	for _, value := range values {
		result[id(value)] = value
	}
	return result
}

// These checks describe the pinned lane example. The file-contract adapter will
// also validate complete jobs before publication.
func compatibilityFailure(s catalog.Scenario, r catalog.RunTemplate, a catalog.AnalysisTemplate, c catalog.Controller) string {
	if s.Type != "lane" {
		return "UNSUPPORTED_SCENARIO_TYPE"
	}
	if s.Type != r.ScenarioType {
		return "SCENARIO_TYPE_MISMATCH"
	}
	if r.Simulator.Name != "lane" || r.Simulator.Version != 1 {
		return "UNSUPPORTED_SIMULATOR"
	}
	if (c.Name != "baseline" && c.Name != "candidate") || c.Version != 1 {
		return "UNSUPPORTED_CONTROLLER"
	}
	if a.CollisionCount.Version != 1 || (a.MinimumObstacleGap.Version != 1 && a.MinimumObstacleGap.Version != 2) || a.GoalProgress.Version != 1 {
		return "UNSUPPORTED_METRIC_VERSION"
	}
	if r.TickMS > 1000 || r.MaxTicks > 100000 || r.TimeoutMS > 600000 || a.MinimumObstacleGap.MinimumMM > 1000000000 {
		return "UNSUPPORTED_LIMITS"
	}
	if s.GoalPositionMM > 1000000000 || len(s.Obstacles) > 32 {
		return "UNSUPPORTED_GEOMETRY"
	}
	if !bodySupported(s.StartPositionMM, s.StartSpeedMMS, s.VehicleLengthMM, r) {
		return "UNSUPPORTED_GEOMETRY"
	}
	for _, o := range s.Obstacles {
		if !bodySupported(o.PositionMM, o.SpeedMMS, o.LengthMM, r) {
			return "UNSUPPORTED_GEOMETRY"
		}
	}
	return ""
}
func bodySupported(position, speed, length int64, r catalog.RunTemplate) bool {
	if position > 1000000000 || speed > 1000000 || length > 1000000 {
		return false
	}
	// Bounds are checked before multiplication, so the coast calculation fits int64.
	return position+speed*r.TickMS*r.MaxTicks/1000 <= 1000000000
}
