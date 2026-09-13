// Package catalog defines editable test inputs and ordered test groups.
package catalog

import (
	"fmt"
	"regexp"
)

const MaxEntries = 10000

// MaxStoredBytes bounds decoded definition content across incremental imports.
const MaxStoredBytes = 16 << 20

// Catalog is an import batch. References may name records in this batch or the store.
type Catalog struct {
	Version           int                `json:"version"`
	Scenarios         []Scenario         `json:"scenarios"`
	Controllers       []Controller       `json:"controllers"`
	RunTemplates      []RunTemplate      `json:"run_templates"`
	AnalysisTemplates []AnalysisTemplate `json:"analysis_templates"`
	Tests             []Test             `json:"tests"`
	Suites            []Suite            `json:"suites"`
	Collections       []Collection       `json:"collections"`
}

type Scenario struct {
	ID              string     `json:"id"`
	Type            string     `json:"type"`
	StartPositionMM int64      `json:"start_position_mm"`
	StartSpeedMMS   int64      `json:"start_speed_mm_s"`
	GoalPositionMM  int64      `json:"goal_position_mm"`
	VehicleLengthMM int64      `json:"vehicle_length_mm"`
	Obstacles       []Obstacle `json:"obstacles"`
}

type Obstacle struct {
	PositionMM int64 `json:"position_mm"`
	SpeedMMS   int64 `json:"speed_mm_s"`
	LengthMM   int64 `json:"length_mm"`
}

// Controller references a named implementation; imports never execute it.
type Controller struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type Implementation struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type RunTemplate struct {
	ID           string         `json:"id"`
	ScenarioType string         `json:"scenario_type"`
	Simulator    Implementation `json:"simulator"`
	TickMS       int64          `json:"tick_ms"`
	MaxTicks     int64          `json:"max_ticks"`
	TimeoutMS    int64          `json:"timeout_ms"`
}

type AnalysisTemplate struct {
	ID                 string          `json:"id"`
	CollisionCount     CollisionMetric `json:"collision_count"`
	MinimumObstacleGap GapMetric       `json:"minimum_obstacle_gap"`
	GoalProgress       ProgressMetric  `json:"goal_progress"`
}

type CollisionMetric struct {
	Version int   `json:"version"`
	Maximum int64 `json:"maximum"`
}
type GapMetric struct {
	Version   int   `json:"version"`
	MinimumMM int64 `json:"minimum_mm"`
}
type ProgressMetric struct {
	Version    int   `json:"version"`
	MinimumPPM int64 `json:"minimum_ppm"`
}

type Test struct {
	ID                 string `json:"id"`
	ScenarioID         string `json:"scenario_id"`
	RunTemplateID      string `json:"run_template_id"`
	AnalysisTemplateID string `json:"analysis_template_id"`
}
type Suite struct {
	ID      string   `json:"id"`
	TestIDs []string `json:"test_ids"`
}
type Collection struct {
	ID       string   `json:"id"`
	SuiteIDs []string `json:"suite_ids"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func ValidateID(id string) error {
	if !identifier.MatchString(id) {
		return fmt.Errorf("invalid identifier %q: use 1–64 lowercase letters, digits, underscores, or hyphens; start with a letter", id)
	}
	return nil
}

// Validate checks the batch without reading files or a database. Foreign keys are
// checked inside the import transaction so references can name existing records.
func (c Catalog) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported catalog version %d", c.Version)
	}
	count := len(c.Scenarios) + len(c.Controllers) + len(c.RunTemplates) + len(c.AnalysisTemplates) + len(c.Tests) + len(c.Suites) + len(c.Collections)
	if count == 0 || count > MaxEntries {
		return fmt.Errorf("catalog must contain 1–%d records", MaxEntries)
	}
	seen := make(map[string]bool)
	add := func(kind, id string) error {
		if err := ValidateID(id); err != nil {
			return fmt.Errorf("%s: %w", kind, err)
		}
		key := kind + ":" + id
		if seen[key] {
			return fmt.Errorf("duplicate %s identifier %q", kind, id)
		}
		seen[key] = true
		return nil
	}
	for _, s := range c.Scenarios {
		if err := add("scenario", s.ID); err != nil {
			return err
		}
		if !identifier.MatchString(s.Type) || s.StartPositionMM < 0 || s.StartSpeedMMS < 0 || s.GoalPositionMM <= s.StartPositionMM || s.VehicleLengthMM <= 0 || s.Obstacles == nil || len(s.Obstacles) > MaxEntries {
			return fmt.Errorf("scenario %q: invalid geometry, type, or obstacles", s.ID)
		}
		for _, o := range s.Obstacles {
			if o.PositionMM < 0 || o.SpeedMMS < 0 || o.LengthMM <= 0 {
				return fmt.Errorf("scenario %q: invalid obstacle geometry", s.ID)
			}
		}
	}
	for _, v := range c.Controllers {
		if err := add("controller", v.ID); err != nil {
			return err
		}
		if !identifier.MatchString(v.Name) || v.Version <= 0 {
			return fmt.Errorf("controller %q: invalid implementation reference", v.ID)
		}
	}
	for _, v := range c.RunTemplates {
		if err := add("run template", v.ID); err != nil {
			return err
		}
		if !identifier.MatchString(v.ScenarioType) || !identifier.MatchString(v.Simulator.Name) || v.Simulator.Version <= 0 || v.TickMS <= 0 || v.MaxTicks <= 0 || v.TimeoutMS <= 0 {
			return fmt.Errorf("run template %q: invalid type, simulator, or limits", v.ID)
		}
	}
	for _, v := range c.AnalysisTemplates {
		if err := add("analysis template", v.ID); err != nil {
			return err
		}
		if v.CollisionCount.Version <= 0 || v.CollisionCount.Maximum < 0 || v.MinimumObstacleGap.Version <= 0 || v.MinimumObstacleGap.MinimumMM < 0 || v.GoalProgress.Version <= 0 || v.GoalProgress.MinimumPPM < 0 || v.GoalProgress.MinimumPPM > 1000000 {
			return fmt.Errorf("analysis template %q: invalid metric version or limit", v.ID)
		}
	}
	for _, v := range c.Tests {
		if err := add("test", v.ID); err != nil {
			return err
		}
		for _, id := range []string{v.ScenarioID, v.RunTemplateID, v.AnalysisTemplateID} {
			if err := ValidateID(id); err != nil {
				return fmt.Errorf("test %q reference: %w", v.ID, err)
			}
		}
	}
	members := 0
	checkMembers := func(kind, id string, ids []string) error {
		if err := add(kind, id); err != nil {
			return err
		}
		if ids == nil {
			return fmt.Errorf("%s %q: membership must be an array", kind, id)
		}
		members += len(ids)
		if members > MaxEntries {
			return fmt.Errorf("catalog exceeds %d membership entries", MaxEntries)
		}
		unique := make(map[string]bool, len(ids))
		for _, member := range ids {
			if err := ValidateID(member); err != nil {
				return fmt.Errorf("%s %q reference: %w", kind, id, err)
			}
			if unique[member] {
				return fmt.Errorf("%s %q: duplicate member %q", kind, id, member)
			}
			unique[member] = true
		}
		return nil
	}
	for _, v := range c.Suites {
		if err := checkMembers("suite", v.ID, v.TestIDs); err != nil {
			return err
		}
	}
	for _, v := range c.Collections {
		if err := checkMembers("collection", v.ID, v.SuiteIDs); err != nil {
			return err
		}
	}
	return nil
}

// emptyCatalog keeps JSON arrays explicit, including omitted import sections.
func emptyCatalog() Catalog {
	return Catalog{Scenarios: []Scenario{}, Controllers: []Controller{}, RunTemplates: []RunTemplate{}, AnalysisTemplates: []AnalysisTemplate{}, Tests: []Test{}, Suites: []Suite{}, Collections: []Collection{}}
}
