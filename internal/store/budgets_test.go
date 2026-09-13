package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/samuelbutton/copernicus/internal/catalog"
)

func TestAdmissionIsAtomicAndRetrySafe(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	if err := s.SetBudget(ctx, DefaultTeam, 299); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal("over-budget request accepted", err)
	}
	for _, table := range []string{"requests", "executions", "outbox", "budget_reservations"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("rejected request left state", table, n, err)
		}
	}
	if err := s.SetBudget(ctx, DefaultTeam, 300); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 6)
	source := requestSource(t)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(ctx, path, true)
			if err != nil {
				failures <- err
				return
			}
			defer db.Close()
			_, err = db.CreateRequest(ctx, requestInput(), source)
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	budgets, err := s.Budgets(ctx)
	if err != nil || len(budgets) != 1 || budgets[0].Reserved != 300 || budgets[0].Remaining != 0 {
		t.Fatal("retry charged more than once", budgets, err)
	}
	next := requestInput()
	next.ID = "another"
	if _, err := s.CreateRequest(ctx, next, source); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal("exhausted team accepted work", err)
	}
	next.TeamID = "missing"
	if _, err := s.CreateRequest(ctx, next, source); !errors.Is(err, ErrTeamUnavailable) {
		t.Fatal("unconfigured team accepted work", err)
	}
	if err := s.SetBudget(ctx, "second", 300); err != nil {
		t.Fatal(err)
	}
	next.TeamID = "second"
	if _, err := s.CreateRequest(ctx, next, source); err != nil {
		t.Fatal("separate team blocked", err)
	}
	if err := s.SetBudget(ctx, DefaultTeam, 299); err == nil {
		t.Fatal("limit undercut reservation")
	}
	for _, sql := range []string{"UPDATE budget_reservations SET ticks=0", "DELETE FROM budget_reservations"} {
		if _, err := s.db.Exec(sql); err == nil {
			t.Fatal("reservation changed")
		}
	}
	if err := s.SetBudget(ctx, DefaultTeam, 600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER fail_jobs BEFORE INSERT ON outbox BEGIN SELECT RAISE(ABORT,'test failure'); END;"); err != nil {
		t.Fatal(err)
	}
	next.ID = "rollback"
	next.TeamID = ""
	if _, err := s.CreateRequest(ctx, next, source); err == nil {
		t.Fatal("partial creation accepted")
	}
	budgets, err = s.Budgets(ctx)
	if err != nil || budgets[0].Reserved != 300 {
		t.Fatal("failed job write charged budget", err)
	}
}

func TestConcurrentAdmissionDoesNotOverspend(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	if err := s.SetBudget(ctx, DefaultTeam, 300); err != nil {
		t.Fatal(err)
	}
	source := requestSource(t)
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(ctx, path, true)
			if err != nil {
				results <- err
				return
			}
			defer db.Close()
			input := requestInput()
			input.ID = fmt.Sprintf("parallel-%d", i)
			_, err = db.CreateRequest(ctx, input, source)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrBudgetExceeded) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 3 {
		t.Fatal("concurrent overspend", accepted, rejected)
	}
}

func TestAdmissionChargesReadyTicksOnlyAndReanalysisIsFree(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	input := requestInput()
	input.Repeat = 100000
	if err := s.SetBudget(ctx, DefaultTeam, 200); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, input, requestSource(t)); err != nil {
		t.Fatal("repeat was treated as multiplier", err)
	}
	other, _ := populated(t)
	analysisFixture(t, other)
	before, err := other.Budgets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.CreateAnalysis(ctx, requestInput().ID, "edge-v2"); err != nil {
		t.Fatal(err)
	}
	after, err := other.Budgets(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("reanalysis reserved simulation", err)
	}
	// A failed resolution has no dispatchable job and consumes zero ticks.
	failed, _ := populated(t)
	extra := example(t)
	extra.Scenarios[0].ID = "unavailable"
	extra.Scenarios[0].Type = "unsupported"
	c := catalog.Catalog{Version: 1, Scenarios: extra.Scenarios[:1], Tests: []catalog.Test{{ID: "unavailable", ScenarioID: "unavailable", RunTemplateID: "lane-v1", AnalysisTemplateID: "standard-v1"}}, Suites: []catalog.Suite{{ID: "unavailable", TestIDs: []string{"unavailable"}}}, Collections: []catalog.Collection{{ID: "unavailable", SuiteIDs: []string{"unavailable"}}}}
	if err := failed.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := failed.SetBudget(ctx, DefaultTeam, 0); err != nil {
		t.Fatal(err)
	}
	input = requestInput()
	input.CollectionID = "unavailable"
	if _, err := failed.CreateRequest(ctx, input, requestSource(t)); err != nil {
		t.Fatal(err)
	}
	budgets, err := failed.Budgets(ctx)
	if err != nil || budgets[0].Reserved != 0 {
		t.Fatal("resolution failure charged budget", err)
	}
}

func TestAdmissionUpgradePreservesLegacyRequests(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	record, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	// Restore the exact schema-five layout around accepted data.
	if _, err := s.db.Exec("DROP TABLE budget_reservations; DROP TABLE team_budgets; DROP TABLE saved_comparisons; PRAGMA user_version=5;"); err != nil {
		t.Fatal(err)
	}
	reader := openStore(t, path, false)
	var version int
	if err := reader.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("read-only migration", err)
	}
	upgraded := openStore(t, path, true)
	got, err := upgraded.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(record, got) {
		t.Fatal("migration changed request", err)
	}
	budgets, err := upgraded.Budgets(ctx)
	if err != nil || len(budgets) != 1 || budgets[0].Reserved != 300 {
		t.Fatal("legacy work was not reserved", err)
	}
	if _, err := upgraded.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	after, err := upgraded.Budgets(ctx)
	if err != nil || !reflect.DeepEqual(budgets, after) {
		t.Fatal("legacy retry charged twice", err)
	}
}

func TestLegacyReservationsAboveDefaultKeepTheirAllowance(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	s := openStore(t, path, true)
	c := example(t)
	c.RunTemplates[0].MaxTicks = 100000
	base := c.Tests[0]
	c.Tests = []catalog.Test{}
	ids := []string{}
	for i := range 101 {
		test := base
		test.ID = fmt.Sprintf("legacy-%d", i)
		ids = append(ids, test.ID)
		c.Tests = append(c.Tests, test)
	}
	c.Suites = []catalog.Suite{{ID: "smoke", TestIDs: ids}}
	c.Collections = []catalog.Collection{{ID: "all-tests", SuiteIDs: []string{"smoke"}}}
	if err := s.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBudget(ctx, DefaultTeam, 20000000); err != nil {
		t.Fatal(err)
	}
	record, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE budget_reservations; DROP TABLE team_budgets; DROP TABLE saved_comparisons; PRAGMA user_version=5;"); err != nil {
		t.Fatal(err)
	}
	upgraded := openStore(t, path, true)
	budgets, err := upgraded.Budgets(ctx)
	if err != nil || budgets[0].Reserved != 10100000 || budgets[0].Limit != 10100000 || budgets[0].Remaining != 0 {
		t.Fatal("legacy allowance lost", budgets, err)
	}
	got, err := upgraded.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(record, got) {
		t.Fatal("legacy snapshot changed", err)
	}
}
