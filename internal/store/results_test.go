package store

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/request"
	"modernc.org/sqlite"
)

func document(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func writeDocument(t *testing.T, path string, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

// mappedOutcome constructs synthetic contract-valid output for an accepted job.
// Separate CLI integration checks use real engine-produced results.
func mappedOutcome(t *testing.T, s *Store, root string) (string, string) {
	t.Helper()
	var body string
	if err := s.db.QueryRow("SELECT content FROM outbox ORDER BY sequence LIMIT 1").Scan(&body); err != nil {
		t.Fatal(err)
	}
	job, err := compatibility.ParseJob([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	jobPath := "jobs/" + job.JobID + ".json"
	if err := os.WriteFile(filepath.Join(root, jobPath), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(job.Inputs, &input); err != nil {
		t.Fatal(err)
	}
	var run struct {
		TickMS int64 `json:"tick_ms"`
	}
	if err := json.Unmarshal(input["run_template"], &run); err != nil {
		t.Fatal(err)
	}
	bagPath := "bags/" + job.ExecutionID + ".jsonl"
	bag, err := os.ReadFile("../../compatibility/contract/v1/examples/valid/bags/exec-1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(bag, []byte("\n")), []byte("\n"))
	var output bytes.Buffer
	for i, line := range lines {
		var value map[string]any
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			value["execution_id"] = job.ExecutionID
			value["inputs_hash"] = job.InputsHash
			value["tick_ms"] = run.TickMS
		} else {
			value["time_ms"] = int64(i-1) * run.TickMS
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(data)
		output.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Join(root, "bags"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bagPath), output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	result := document(t, "../../compatibility/contract/v1/examples/valid/results/run-1.json")
	result["job_id"] = job.JobID
	result["execution_id"] = job.ExecutionID
	result["correlation_id"] = job.CorrelationID
	result["attempt_id"] = "attempt-one"
	result["inputs_hash"] = job.InputsHash
	result["job"] = compatibility.Reference{Path: jobPath, SHA256: request.Hash([]byte(body))}
	bagHash := request.Hash(output.Bytes())
	result["bag"] = compatibility.Reference{Path: bagPath, SHA256: bagHash}
	analysisHash, err := compatibility.ContentHash(input["analysis_template"])
	if err != nil {
		t.Fatal(err)
	}
	result["analysis_template"] = input["analysis_template"]
	result["analysis_hash"] = analysisHash
	result["analysis_id"] = request.Hash([]byte(job.ExecutionID + "\n" + bagHash + "\n" + analysisHash))
	path := "results/" + job.JobID + ".json"
	writeDocument(t, filepath.Join(root, path), result)
	event := writeEvent(t, root, path, "FAIL", 4, "attempt-one")
	return path, event
}

func writeEvent(t *testing.T, root, resultPath, state string, sequence int, attempt string) string {
	t.Helper()
	result := document(t, filepath.Join(root, resultPath))
	data, err := os.ReadFile(filepath.Join(root, resultPath))
	if err != nil {
		t.Fatal(err)
	}
	job := result["job_id"].(string)
	id := request.Hash([]byte(job + "\n" + attempt + "\n" + strconv.Itoa(sequence)))
	e := map[string]any{"contract_version": 1, "kind": "event", "execution_id": result["execution_id"], "job_id": job, "attempt_id": attempt, "correlation_id": result["correlation_id"], "event_id": id, "sequence": sequence, "state": state, "analysis_id": nil, "result": nil, "event_type": "execution_transition", "producer": "yamata", "created_at_ms": 0}
	if state == "ANALYZING" || compatibility.Terminal(state) {
		e["analysis_id"] = result["analysis_id"]
	}
	if compatibility.Terminal(state) {
		e["result"] = compatibility.Reference{Path: resultPath, SHA256: request.Hash(data)}
	}
	path := "events/" + id + ".json"
	writeDocument(t, filepath.Join(root, path), e)
	return path
}

func TestResultProgressReplayRebuildAndMissingFiles(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	record, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, event := mappedOutcome(t, s, root)
	pending := writeEvent(t, root, result, "PENDING", 1, "attempt-one")
	if _, err := s.ImportResults(ctx, root, []string{pending}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Progress(ctx, requestInput().ID)
	if err != nil || before.Executions[0].State != "PENDING" || before.Completed != 0 || before.Total != 3 {
		t.Fatalf("before=%+v %v", before, err)
	}
	report, err := s.ImportResults(ctx, root, []string{event})
	if err != nil || report.EventsAdded != 1 || report.ResultsAdded != 1 {
		t.Fatalf("report=%+v %v", report, err)
	}
	first, err := s.Progress(ctx, requestInput().ID)
	if err != nil || first.Completed != 1 || first.Failed != 1 || first.Passed != 0 || first.Incomplete != 2 || first.Complete {
		t.Fatalf("progress=%+v %v", first, err)
	}
	late := writeEvent(t, root, result, "RUNNING", 99, "another-attempt")
	if _, err := s.ImportResults(ctx, root, []string{pending, event, event, late}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Progress(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(first, got) {
		t.Fatal("late event regressed completion")
	}
	index, err := s.IndexStatus(ctx)
	if err != nil || index.Events != 3 || index.Results != 1 || index.Unassigned != 0 {
		t.Fatalf("index=%+v %v", index, err)
	}
	if err := os.RemoveAll(filepath.Join(root, "events")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if n, err := s.RebuildResults(ctx, root); err != nil || n != 1 {
			t.Fatalf("rebuild=%d %v", n, err)
		}
	}
	got, err = s.Progress(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(first, got) {
		t.Fatal("rebuild changed progress")
	}
	saved, err := s.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(record, saved) {
		t.Fatal("import changed request")
	}
	data, err := os.ReadFile(filepath.Join(root, result))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, result), append(data, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = s.Progress(ctx, requestInput().ID)
	if err != nil || got.Completed != 0 || got.Passed != 0 || got.Executions[0].State != "INCOMPLETE" {
		t.Fatal("changed bytes retained completed result")
	}
	if _, err := s.RebuildResults(ctx, root); err == nil {
		t.Fatal("accepted changed identity during rebuild")
	}
	unchanged, err := s.IndexStatus(ctx)
	if err != nil || unchanged != index {
		t.Fatal("failed rebuild replaced index")
	}
	if err := os.WriteFile(filepath.Join(root, result), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = s.Progress(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(first, got) {
		t.Fatal("restored bytes did not recover progress")
	}
	var parsed compatibility.Result
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, parsed.Bag.Path)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Progress(ctx, requestInput().ID)
	if err != nil || got.Completed != 0 {
		t.Fatal("missing recording retained completion")
	}
}

func TestUnknownOutcomeMappingAndConcurrentImport(t *testing.T) {
	ctx := context.Background()
	producer, _ := populated(t)
	if _, err := producer.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_, event := mappedOutcome(t, producer, root)
	s, path := populated(t)
	var wg sync.WaitGroup
	failures := make(chan error, 6)
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
			_, err = db.ImportResults(ctx, root, []string{event})
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
	index, err := s.IndexStatus(ctx)
	if err != nil || index.Events != 1 || index.Results != 1 || index.Unassigned != 1 {
		t.Fatalf("unknown index=%+v %v", index, err)
	}
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	index, err = s.IndexStatus(ctx)
	if err != nil || index.Unassigned != 0 {
		t.Fatal("exact job mapping did not assign result")
	}
	progress, err := s.Progress(ctx, requestInput().ID)
	if err != nil || progress.Completed != 1 {
		t.Fatalf("mapped=%+v %v", progress, err)
	}
	page, err := s.Results(ctx, "", 1)
	if err != nil || len(page.Results) != 1 || page.Results[0].RequestID != requestInput().ID {
		t.Fatalf("page=%+v %v", page, err)
	}
}

func TestImporterProcessExitIsAtomic(t *testing.T) {
	if dbPath := os.Getenv("COPERNICUS_IMPORT_CRASH_DB"); dbPath != "" {
		if err := sqlite.RegisterScalarFunction("crash_import_test", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(23); return nil, nil }); err != nil {
			t.Fatal(err)
		}
		s, err := Open(context.Background(), dbPath, true)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.ImportResults(context.Background(), os.Getenv("COPERNICUS_IMPORT_CRASH_ROOT"), []string{os.Getenv("COPERNICUS_IMPORT_CRASH_EVENT")})
		t.Fatalf("expected exit: %v", err)
	}
	ctx := context.Background()
	s, path := populated(t)
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_, event := mappedOutcome(t, s, root)
	if _, err := s.db.Exec("CREATE TRIGGER crash_import AFTER INSERT ON indexed_results BEGIN SELECT crash_import_test(); END"); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestImporterProcessExitIsAtomic$")
	child.Env = append(os.Environ(), "COPERNICUS_IMPORT_CRASH_DB="+path, "COPERNICUS_IMPORT_CRASH_ROOT="+root, "COPERNICUS_IMPORT_CRASH_EVENT="+event)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child=%v %s", err, output)
	}
	index, err := s.IndexStatus(ctx)
	if err != nil || index.Events != 0 || index.Results != 0 {
		t.Fatal("partial import committed")
	}
	if _, err := s.db.Exec("DROP TRIGGER crash_import"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportResults(ctx, root, []string{event}); err != nil {
		t.Fatal(err)
	}
	index, err = s.IndexStatus(ctx)
	if err != nil || index.Events != 1 || index.Results != 1 {
		t.Fatal("replay failed")
	}
}

func TestTerminalDenominatorsAndResolutionFailures(t *testing.T) {
	for _, state := range []string{"PASS", "FAIL", "WARN", "ERROR"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			s, _ := populated(t)
			if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			result, _ := mappedOutcome(t, s, root)
			value := document(t, filepath.Join(root, result))
			value["status"] = state
			metrics := value["metrics"].(map[string]any)
			if state == "PASS" || state == "WARN" {
				collision := metrics["collision_count"].(map[string]any)
				collision["value"] = 0
				collision["pass"] = true
				goal := metrics["goal_progress"].(map[string]any)
				goal["value"] = 1000000
				goal["pass"] = true
			}
			if state == "WARN" {
				gap := metrics["minimum_obstacle_gap"].(map[string]any)
				gap["value"] = nil
				gap["pass"] = nil
				gap["evidence_ticks"] = []int{}
			}
			if state == "ERROR" {
				value["metrics"] = nil
				value["failure_class"] = "worker_failure"
			}
			writeDocument(t, filepath.Join(root, result), value)
			if _, err := s.ImportResults(ctx, root, []string{result}); err != nil {
				t.Fatal(err)
			}
			p, err := s.Progress(ctx, requestInput().ID)
			if err != nil || p.Completed != 1 || p.Total != 3 || p.Incomplete != 2 || p.Executions[0].State != state {
				t.Fatalf("progress=%+v %v", p, err)
			}
			if p.Passed+p.Failed+p.Warnings+p.Errors != 1 || state != "PASS" && p.Passed != 0 {
				t.Fatal("wrong terminal denominator")
			}
			if state == "PASS" {
				if err := os.Remove(filepath.Join(root, result)); err != nil {
					t.Fatal(err)
				}
				p, err = s.Progress(ctx, requestInput().ID)
				if err != nil || p.Passed != 0 || p.Completed != 0 {
					t.Fatal("missing result counted as pass")
				}
			}
		})
	}
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"), true)
	c := example(t)
	c.Scenarios[1].Type = "unsupported"
	if err := s.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	p, err := s.Progress(ctx, requestInput().ID)
	if err != nil || p.Total != 3 || p.Completed != 0 || p.Incomplete != 3 || p.ResolutionFailed != 1 || p.Complete {
		t.Fatalf("resolution progress=%+v %v", p, err)
	}
}

func TestRejectedEventRemainsReplayableAndConflictsPreserveCounts(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, event := mappedOutcome(t, s, root)
	data, err := os.ReadFile(filepath.Join(root, result))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, result)); err != nil {
		t.Fatal(err)
	}
	report, err := s.ImportResults(ctx, root, []string{event})
	if err == nil || report.Rejected != 1 {
		t.Fatal("accepted event without result")
	}
	index, err := s.IndexStatus(ctx)
	if err != nil || index.Events != 0 || index.Results != 0 {
		t.Fatal("rejected event advanced progress")
	}
	if err := os.WriteFile(filepath.Join(root, result), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportResults(ctx, root, []string{event}); err != nil {
		t.Fatal(err)
	}
	changed := document(t, filepath.Join(root, event))
	changed["created_at_ms"] = 1
	writeDocument(t, filepath.Join(root, event), changed)
	if _, err := s.ImportResults(ctx, root, []string{event}); err == nil {
		t.Fatal("accepted changed event bytes")
	}
	index, err = s.IndexStatus(ctx)
	if err != nil || index.Events != 1 || index.Results != 1 {
		t.Fatal("event conflict changed counts")
	}
}

func TestResultStorageLimitCountsUTF8Bytes(t *testing.T) {
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
	// This valid JSON has fewer than one million characters but exceeds one MiB.
	oversized := `"` + strings.Repeat("界", 400000) + `"`
	if _, err := s.db.Exec("UPDATE indexed_results SET content=?", oversized); err == nil {
		t.Fatal("storage accepted oversized UTF-8 content")
	}
	p, err := s.Progress(ctx, requestInput().ID)
	if err != nil || p.Completed != 1 {
		t.Fatal("rejected oversized content changed progress")
	}
}
