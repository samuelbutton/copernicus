package request

import (
	"encoding/json"
	"errors"
	"github.com/samuelbutton/copernicus/internal/catalog"
)

// SimulationTicks reserves the maximum cost of ready executions, not wall time.
// A repeat identifies one execution; it is not a multiplier.
func SimulationTicks(snapshot Snapshot) (int64, error) {
	var total int64
	if len(snapshot.Executions) > MaxTests {
		return 0, errors.New("too many executions")
	}
	for _, execution := range snapshot.Executions {
		if execution.Status == ResolutionFailed {
			continue
		}
		if execution.Status != Ready || Hash(execution.RunTemplate.Content) != execution.RunTemplate.SHA256 {
			return 0, errors.New("invalid run template")
		}
		var run catalog.RunTemplate
		if err := json.Unmarshal(execution.RunTemplate.Content, &run); err != nil {
			return 0, err
		}
		if run.MaxTicks < 1 || run.MaxTicks > 100000 {
			return 0, errors.New("unsupported simulation tick bound")
		}
		total += run.MaxTicks
	}
	return total, nil
}
