package catalog

import "fmt"

// Expand follows collection order, then suite order. A shared test appears at
// its first occurrence only. Missing references fail instead of hiding tests.
func (c Catalog) Expand(id string) ([]Test, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	tests := make(map[string]Test, len(c.Tests))
	for _, v := range c.Tests {
		tests[v.ID] = v
	}
	suites := make(map[string]Suite, len(c.Suites))
	for _, v := range c.Suites {
		suites[v.ID] = v
	}
	var selected *Collection
	for i := range c.Collections {
		if c.Collections[i].ID == id {
			selected = &c.Collections[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("collection %q not found", id)
	}
	out := []Test{}
	seen := make(map[string]bool)
	for _, suiteID := range selected.SuiteIDs {
		suite, ok := suites[suiteID]
		if !ok {
			return nil, fmt.Errorf("suite %q not found", suiteID)
		}
		for _, testID := range suite.TestIDs {
			test, ok := tests[testID]
			if !ok {
				return nil, fmt.Errorf("test %q not found", testID)
			}
			if !seen[testID] {
				out = append(out, test)
				seen[testID] = true
			}
		}
	}
	return out, nil
}
