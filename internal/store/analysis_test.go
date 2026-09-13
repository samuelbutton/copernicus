package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

func analysisFixture(t *testing.T, s *Store) (request.Record, string, string) {
	t.Helper()
	ctx := context.Background()
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{"stopped-obstacle"}}); err != nil {
		t.Fatal(err)
	}
	input := requestInput()
	input.SuiteIDs = []string{"smoke"}
	record, err := s.CreateRequest(ctx, input, requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	template := example(t).AnalysisTemplates[0]
	template.ID = "edge-v2"
	template.MinimumObstacleGap.Version = 2
	if err := s.Import(ctx, catalog.Catalog{Version: 1, AnalysisTemplates: []catalog.AnalysisTemplate{template}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, _ := mappedOutcome(t, s, root)
	if _, err := s.ImportResults(ctx, root, []string{result}); err != nil {
		t.Fatal(err)
	}
	return record, root, result
}

func TestAnalysisSelectionIdentityAndAtomicity(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	record, root, result := analysisFixture(t, s)
	before, err := s.Progress(ctx, requestInput().ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_analysis BEFORE INSERT ON analysis_selections BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAnalysis(ctx, requestInput().ID, "edge-v2"); err == nil {
		t.Fatal("accepted failed selection write")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM outbox").Scan(&count); err != nil || count != 1 {
		t.Fatal("partial analysis jobs committed", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_analysis"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	values := make(chan AnalysisSelection, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(ctx, path, true)
			if err != nil {
				failures <- err
				return
			}
			defer db.Close()
			v, err := db.CreateAnalysis(ctx, requestInput().ID, "edge-v2")
			if err != nil {
				failures <- err
				return
			}
			values <- v
		}()
	}
	wg.Wait()
	close(failures)
	close(values)
	for err := range failures {
		t.Error(err)
	}
	var saved AnalysisSelection
	for v := range values {
		if saved.ID != "" && !reflect.DeepEqual(saved, v) {
			t.Fatal("retry selected different jobs")
		}
		saved = v
	}
	if saved.ID == "" {
		t.Fatal("no selection accepted")
	}
	for _, statement := range []string{"UPDATE analysis_selections SET id='changed'", "DELETE FROM analysis_selections"} {
		if _, err := s.db.Exec(statement); err == nil {
			t.Fatal("allowed selection mutation")
		}
	}
	pending, err := s.ProgressAnalysis(ctx, requestInput().ID, "edge-v2")
	if err != nil || pending.Completed != 0 || pending.Total != 1 || pending.Executions[0].State != "QUEUED" {
		t.Fatalf("new scores: %+v %v", pending, err)
	}
	after, err := s.Progress(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("new analysis changed original progress", err)
	}
	original, err := s.CreateAnalysis(ctx, requestInput().ID, "standard-v1")
	if err != nil || original.Jobs[before.Executions[0].ExecutionID] != before.Executions[0].JobID {
		t.Fatal("unchanged scoring failed to reuse run", err)
	}
	alias := example(t).AnalysisTemplates[0]
	alias.ID = "edge-alias"
	alias.MinimumObstacleGap.Version = 2
	if err := s.Import(ctx, catalog.Catalog{Version: 1, AnalysisTemplates: []catalog.AnalysisTemplate{alias}}); err != nil {
		t.Fatal(err)
	}
	same, err := s.CreateAnalysis(ctx, requestInput().ID, alias.ID)
	if err != nil || !reflect.DeepEqual(same.Jobs, saved.Jobs) {
		t.Fatal("equivalent template duplicated analysis", err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM outbox").Scan(&count); err != nil || count != 2 {
		t.Fatal("retry created extra jobs", count, err)
	}
	if _, err := s.PublishOutbox(ctx, root, 100); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := s.db.QueryRow("SELECT content FROM outbox WHERE job_id=?", saved.Jobs[before.Executions[0].ExecutionID]).Scan(&body); err != nil {
		t.Fatal(err)
	}
	job, err := compatibility.ParseJob([]byte(body))
	if err != nil || job.JobKind != "analysis" {
		t.Fatal("not a public analysis job", err)
	}
	var input struct {
		Bag compatibility.Reference `json:"bag"`
	}
	if err := json.Unmarshal(job.Inputs, &input); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, input.Bag.Path))
	if err != nil || request.Hash(data) != input.Bag.SHA256 {
		t.Fatal("job changed recording", err)
	}
	frozen, err := s.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(record, frozen) {
		t.Fatal("request changed", err)
	}
	if err := os.Remove(filepath.Join(root, result)); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateAnalysis(ctx, requestInput().ID, "edge-v2")
	if err != nil || !reflect.DeepEqual(saved, retry) {
		t.Fatal("accepted retry required original file", err)
	}
	if _, err := s.ProgressAnalysis(ctx, requestInput().ID, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unknown selection fell back", err)
	}
}

func TestAnalysisRequiresEveryOriginalRecording(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, _ := mappedOutcome(t, s, root)
	if _, err := s.ImportResults(ctx, root, []string{result}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAnalysis(ctx, requestInput().ID, "standard-v1"); !errors.Is(err, request.ErrInvalidSubmission) {
		t.Fatal("partial request was accepted", err)
	}
	s2, _ := populated(t)
	_, root2, result2 := analysisFixture(t, s2)
	template := example(t).AnalysisTemplates[0]
	template.ID = "unsupported"
	template.GoalProgress.Version = 2
	if err := s2.Import(ctx, catalog.Catalog{Version: 1, AnalysisTemplates: []catalog.AnalysisTemplate{template}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unsupported", "original", "missing"} {
		if _, err := s2.CreateAnalysis(ctx, requestInput().ID, id); !errors.Is(err, request.ErrInvalidSubmission) {
			t.Fatal("invalid scoring accepted", id, err)
		}
	}
	value := document(t, filepath.Join(root2, result2))
	bag := value["bag"].(map[string]any)["path"].(string)
	if err := os.Remove(filepath.Join(root2, bag)); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.CreateAnalysis(ctx, requestInput().ID, "edge-v2"); !errors.Is(err, request.ErrInvalidSubmission) {
		t.Fatal("missing recording accepted", err)
	}
}

func TestSchemaFourUpgradePreservesPublishedResults(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(schema + requestsSchema + outboxSchema + resultsSchema); err != nil {
		t.Fatal(err)
	}
	// Install temporary admission tables only while constructing legacy fixture data.
	if _, err := raw.Exec(admissionSchema + "INSERT INTO team_budgets VALUES('local',10000000);"); err != nil {
		t.Fatal(err)
	}
	old := &Store{db: raw}
	if err := old.Import(ctx, example(t)); err != nil {
		t.Fatal(err)
	}
	record, root, _ := analysisFixture(t, old)
	if _, err := old.PublishOutbox(ctx, root, 100); err != nil {
		t.Fatal(err)
	}
	before, err := old.Progress(ctx, requestInput().ID)
	if err != nil {
		t.Fatal(err)
	}
	var body, hash string
	if err := raw.QueryRow("SELECT content,content_hash FROM outbox").Scan(&body, &hash); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("DROP TABLE budget_reservations; DROP TABLE team_budgets; DROP TABLE saved_comparisons; PRAGMA user_version=4;"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	reader := openStore(t, path, false)
	choices, err := reader.Analyses(ctx, requestInput().ID)
	if err != nil || len(choices) != 1 {
		t.Fatal("legacy original selection unavailable", err)
	}
	var version int
	if err := reader.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatal("read migrated database", err)
	}
	upgraded := openStore(t, path, true)
	after, err := upgraded.Progress(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration changed results", err)
	}
	frozen, err := upgraded.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(record, frozen) {
		t.Fatal("migration changed snapshot", err)
	}
	var gotBody, gotHash string
	var sequence, published int
	if err := upgraded.db.QueryRow("SELECT sequence,content,content_hash,published FROM outbox").Scan(&sequence, &gotBody, &gotHash, &published); err != nil || sequence != 1 || published != 1 || body != gotBody || hash != gotHash {
		t.Fatal("migration changed publication", err)
	}
	if _, err := upgraded.CreateAnalysis(ctx, requestInput().ID, "edge-v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.db.Exec("INSERT INTO outbox(job_id,execution_id,request_id,content,content_hash) SELECT 'duplicate',execution_id,request_id,content,content_hash FROM outbox WHERE sequence=1"); err == nil {
		t.Fatal("accepted duplicate original run")
	}
}
