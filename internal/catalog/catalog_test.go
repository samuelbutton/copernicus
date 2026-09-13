package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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
func openStore(t *testing.T, path string, create bool) *Store {
	t.Helper()
	s, err := Open(context.Background(), path, create)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func readStore(t *testing.T, s *Store) Catalog {
	t.Helper()
	c, err := s.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestPersistentCatalogAndOrderedExpansion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	s, err := Open(ctx, path, true)
	if err != nil {
		t.Fatal(err)
	}
	original := example(t)
	if err := s.Import(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openStore(t, path, false)
	c := readStore(t, reader)
	tests, err := c.Expand("all-tests")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, v := range tests {
		ids = append(ids, v.ID)
	}
	if !reflect.DeepEqual(ids, []string{"stopped-obstacle", "empty-lane", "moving-obstacle"}) {
		t.Fatalf("expanded %v", ids)
	}
	if !reflect.DeepEqual(c.Controllers, original.Controllers) || !reflect.DeepEqual(c.RunTemplates, original.RunTemplates) || !reflect.DeepEqual(c.AnalysisTemplates, original.AnalysisTemplates) {
		t.Fatal("definition content changed")
	}
	for _, s := range c.Scenarios {
		for _, want := range original.Scenarios {
			if s.ID == want.ID && !reflect.DeepEqual(s, want) {
				t.Fatalf("scenario changed: %s", s.ID)
			}
		}
	}
	if _, err := c.Expand("missing"); err == nil {
		t.Fatal("accepted unknown collection")
	}
	if err := reader.Import(ctx, original); err == nil {
		t.Fatal("read-only store accepted a write")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("inspection changed database bytes")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions = %o", info.Mode().Perm())
	}
}

func TestInvalidBatchRollsBack(t *testing.T) {
	for name, change := range map[string]func(*Catalog){
		"scenario reference": func(c *Catalog) { c.Tests[0].ScenarioID = "missing" },
		"run reference":      func(c *Catalog) { c.Tests[0].RunTemplateID = "missing" },
		"analysis reference": func(c *Catalog) { c.Tests[0].AnalysisTemplateID = "missing" },
		"test reference":     func(c *Catalog) { c.Suites[0].TestIDs[0] = "missing" },
		"suite reference":    func(c *Catalog) { c.Collections[0].SuiteIDs[0] = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			s := openStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"), true)
			c := example(t)
			change(&c)
			if err := s.Import(context.Background(), c); err == nil {
				t.Fatal("accepted invalid reference")
			}
			got := readStore(t, s)
			if len(got.Scenarios)+len(got.Tests)+len(got.Controllers)+len(got.Collections) != 0 {
				t.Fatal("partial import survived")
			}
			if err := s.Import(context.Background(), example(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExistingIDsAndIncrementalReferences(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"), true)
	c := example(t)
	if err := s.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	before := readStore(t, s)
	c.Scenarios[0].ID = "new-scenario"
	if err := s.Import(ctx, c); err == nil {
		t.Fatal("accepted persisted duplicate")
	}
	if !reflect.DeepEqual(readStore(t, s), before) {
		t.Fatal("duplicate changed existing records or inserted partial batch")
	}
	additions := Catalog{Version: 1, Suites: []Suite{{ID: "extra", TestIDs: []string{"empty-lane", "moving-obstacle"}}, {ID: "empty", TestIDs: []string{}}}, Collections: []Collection{{ID: "subset", SuiteIDs: []string{"empty", "extra"}}, {ID: "empty", SuiteIDs: []string{}}}}
	if err := s.Import(ctx, additions); err != nil {
		t.Fatal(err)
	}
	stored := readStore(t, s)
	got, err := stored.Expand("subset")
	if err != nil || len(got) != 2 || got[0].ID != "empty-lane" || got[1].ID != "moving-obstacle" {
		t.Fatalf("expansion = %v, %v", got, err)
	}
	got, err = stored.Expand("empty")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty expansion = %v, %v", got, err)
	}
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

func TestConcurrentInitializationAndImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	c := example(t)
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(context.Background(), path, true)
			if err != nil {
				results <- err
				return
			}
			defer s.Close()
			results <- s.Import(context.Background(), c)
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			t.Errorf("unexpected concurrent failure: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful imports = %d", successes)
	}
	got := readStore(t, openStore(t, path, false))
	if len(got.Tests) != 3 || len(got.Controllers) != 2 {
		t.Fatal("lost or duplicated catalog")
	}
}

func TestDatabaseBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.sqlite")
	if _, err := Open(context.Background(), path, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing read: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created database")
	}
	s := openStore(t, path, true)
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	for _, create := range []bool{false, true} {
		if _, err := Open(context.Background(), path, create); err == nil {
			t.Fatal("accepted future schema")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("changed future database")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), link, true); err == nil {
		t.Fatal("accepted database symlink")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestAbruptTransactionExit(t *testing.T) {
	if path := os.Getenv("COPERNICUS_TEST_CRASH_DB"); path != "" {
		s, err := Open(context.Background(), path, true)
		if err != nil {
			os.Exit(20)
		}
		tx, err := s.db.Begin()
		if err != nil {
			os.Exit(21)
		}
		if _, err := tx.Exec("INSERT INTO controllers VALUES ('partial', ?)", `{"id":"partial","name":"baseline","version":1}`); err != nil {
			os.Exit(22)
		}
		os.Exit(23)
	}
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	s := openStore(t, path, true)
	if err := s.Import(context.Background(), example(t)); err != nil {
		t.Fatal(err)
	}
	before := readStore(t, s)
	child := exec.Command(os.Args[0], "-test.run=^TestAbruptTransactionExit$")
	child.Env = append(os.Environ(), "COPERNICUS_TEST_CRASH_DB="+path)
	err := child.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child = %v", err)
	}
	if !reflect.DeepEqual(readStore(t, s), before) {
		t.Fatal("uncommitted record survived process exit")
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
