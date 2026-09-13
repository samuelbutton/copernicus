package catalog

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func example(t *testing.T) Catalog {
	t.Helper()
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestValidation(t *testing.T) {
	changes := map[string]func(*Catalog){
		"unknown version":        func(c *Catalog) { c.Version = 2 },
		"scenario duplicate":     func(c *Catalog) { c.Scenarios = append(c.Scenarios, c.Scenarios[0]) },
		"controller duplicate":   func(c *Catalog) { c.Controllers = append(c.Controllers, c.Controllers[0]) },
		"run duplicate":          func(c *Catalog) { c.RunTemplates = append(c.RunTemplates, c.RunTemplates[0]) },
		"analysis duplicate":     func(c *Catalog) { c.AnalysisTemplates = append(c.AnalysisTemplates, c.AnalysisTemplates[0]) },
		"test duplicate":         func(c *Catalog) { c.Tests = append(c.Tests, c.Tests[0]) },
		"suite duplicate":        func(c *Catalog) { c.Suites = append(c.Suites, c.Suites[0]) },
		"collection duplicate":   func(c *Catalog) { c.Collections = append(c.Collections, c.Collections[0]) },
		"suite member duplicate": func(c *Catalog) { c.Suites[0].TestIDs = append(c.Suites[0].TestIDs, c.Suites[0].TestIDs[0]) },
		"collection member duplicate": func(c *Catalog) {
			c.Collections[0].SuiteIDs = append(c.Collections[0].SuiteIDs, c.Collections[0].SuiteIDs[0])
		},
		"identifier":         func(c *Catalog) { c.Tests[0].ID = "../bad" },
		"geometry":           func(c *Catalog) { c.Scenarios[0].VehicleLengthMM = 0 },
		"obstacle":           func(c *Catalog) { c.Scenarios[1].Obstacles[0].LengthMM = -1 },
		"missing obstacles":  func(c *Catalog) { c.Scenarios[0].Obstacles = nil },
		"controller version": func(c *Catalog) { c.Controllers[0].Version = 0 },
		"run limit":          func(c *Catalog) { c.RunTemplates[0].MaxTicks = 0 },
		"metric limit":       func(c *Catalog) { c.AnalysisTemplates[0].GoalProgress.MinimumPPM = 1000001 },
		"metric version":     func(c *Catalog) { c.AnalysisTemplates[0].MinimumObstacleGap.Version = 0 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c := example(t)
			change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
}

func TestStrictJSON(t *testing.T) {
	for _, input := range []string{
		`{"version":1,"version":1}`, `{"version":1,"Version":1}`, `{"version":1,"surprise":true}`, `{"version":1} {}`, `null`, `[]`, `{"version":1,"controllers":null}`,
		`{"version":1,"controllers":[{"id":"a","name":"a","version":1,"command":"run"}]}`,
		`{"version":1,"controllers":[{"id":"a","name":"a","version":1,"version":2}]}`,
		`{"version":1.5}`, "\xff", strings.Repeat(" ", MaxImportBytes+1), strings.Repeat("[", 34) + strings.Repeat("]", 34),
	} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid JSON: %.80q", input)
		}
	}
}

func FuzzDecode(f *testing.F) {
	data, err := os.ReadFile("../../examples/catalog.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Decode(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c, again) {
			t.Fatal("round trip changed catalog")
		}
	})
}
