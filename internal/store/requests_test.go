package store

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
	"modernc.org/sqlite"
)

func requestInput() request.Submission {
	return request.Submission{ID: "review-one", CollectionID: "all-tests", ControllerID: "baseline", Requester: "reviewer", Priority: 1}
}
func requestSource(t *testing.T) compatibility.Source {
	t.Helper()
	s, err := compatibility.Yamata()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func populated(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	s := openStore(t, path, true)
	if err := s.Import(context.Background(), example(t)); err != nil {
		t.Fatal(err)
	}
	return s, path
}
func TestRequestSurvivesSuiteEditAndRetry(t *testing.T) {
	ctx := context.Background()
	s, path := populated(t)
	input := requestInput()
	source := requestSource(t)
	original, err := s.CreateRequest(ctx, input, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{"empty-lane"}}); err != nil {
		t.Fatal(err)
	}
	read, err := s.Request(ctx, input.ID)
	if err != nil || !reflect.DeepEqual(original, read) {
		t.Fatalf("snapshot changed: %v", err)
	}
	retry, err := s.CreateRequest(ctx, input, source)
	if err != nil || !reflect.DeepEqual(original, retry) {
		t.Fatalf("retry changed snapshot: %v", err)
	}
	input.ID = "review-two"
	next, err := s.CreateRequest(ctx, input, source)
	if err != nil {
		t.Fatal(err)
	}
	if next.SnapshotSHA256 == original.SnapshotSHA256 {
		t.Fatal("suite edit did not change next snapshot")
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(next.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Executions[0].Test.ID != "empty-lane" || snapshot.Executions[1].Test.ID != "moving-obstacle" {
		t.Fatal("new request ignored suite edit")
	}
	for _, change := range []func(*request.Submission){func(s *request.Submission) { s.ControllerID = "candidate" }, func(s *request.Submission) { s.Requester = "other" }, func(s *request.Submission) { s.Priority = 3 }, func(s *request.Submission) { s.Seed = 1 }, func(s *request.Submission) { s.Repeat = 1 }, func(s *request.Submission) { s.CollectionID = "another" }} {
		changed := requestInput()
		change(&changed)
		if _, err := s.CreateRequest(ctx, changed, source); !errors.Is(err, request.ErrIdentityConflict) {
			t.Fatalf("conflict = %v", err)
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openStore(t, path, false)
	read, err = reader.Request(ctx, "review-one")
	if err != nil || !reflect.DeepEqual(read, original) {
		t.Fatalf("reopened snapshot: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("request inspection changed database")
	}
}

func TestAtomicResolutionAndImmutability(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	if _, err := s.db.Exec("CREATE TRIGGER reject_second BEFORE INSERT ON executions WHEN NEW.position = 1 BEGIN SELECT RAISE(ABORT, 'test write failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err == nil {
		t.Fatal("accepted partial write")
	}
	for _, table := range []string{"requests", "executions"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("partial request committed")
		}
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_second"); err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"UPDATE requests SET snapshot = '{}'", "DELETE FROM requests", "UPDATE executions SET reason = 'changed'", "DELETE FROM executions"} {
		if _, err := s.db.Exec(statement); err == nil {
			t.Fatalf("allowed %s", statement)
		}
	}
	actual, err := s.Request(ctx, requestInput().ID)
	if err != nil || !reflect.DeepEqual(accepted, actual) {
		t.Fatal("mutation changed snapshot")
	}
	// Invalid suite edits roll back their preceding membership deletion.
	before := readStore(t, s)
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{"missing"}}); err == nil {
		t.Fatal("accepted missing test")
	}
	if !reflect.DeepEqual(before, readStore(t, s)) {
		t.Fatal("failed edit changed suite")
	}
}

func TestResolutionFailureIsDurable(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"), true)
	c := example(t)
	c.Scenarios[1].Type = "unavailable"
	if err := s.Import(ctx, c); err != nil {
		t.Fatal(err)
	}
	record, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != request.ResolutionFailed || snapshot.Executions[0].Reason != "UNSUPPORTED_SCENARIO_TYPE" {
		t.Fatal("failure was not saved")
	}
	var failed, ready int
	if err := s.db.QueryRow("SELECT count(*) FROM executions WHERE resolution_status = 'RESOLUTION_FAILED'").Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM executions WHERE resolution_status = 'READY'").Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if failed != 1 || ready != 2 {
		t.Fatalf("failed=%d ready=%d", failed, ready)
	}
	retry, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil || !reflect.DeepEqual(record, retry) {
		t.Fatal("retry replaced resolution failure")
	}
}

func TestConcurrentRequestIdentity(t *testing.T) {
	_, path := populated(t)
	source := requestSource(t)
	var wg sync.WaitGroup
	records := make(chan request.Record, 6)
	failures := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(context.Background(), path, true)
			if err != nil {
				failures <- err
				return
			}
			defer s.Close()
			record, err := s.CreateRequest(context.Background(), requestInput(), source)
			if err != nil {
				failures <- err
				return
			}
			records <- record
		}()
	}
	wg.Wait()
	close(records)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var hash string
	n := 0
	for record := range records {
		if hash != "" && hash != record.SnapshotSHA256 {
			t.Fatal("concurrent submissions differ")
		}
		hash = record.SnapshotSHA256
		n++
	}
	if n != 6 {
		t.Fatalf("successful calls = %d", n)
	}
	s := openStore(t, path, false)
	var requests, executions int
	if err := s.db.QueryRow("SELECT count(*) FROM requests").Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM executions").Scan(&executions); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || executions != 3 {
		t.Fatalf("requests=%d executions=%d", requests, executions)
	}
}

func TestSchemaOneUpgradePreservesCatalog(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	old := &Store{db: db}
	if err := old.Import(ctx, example(t)); err != nil {
		t.Fatal(err)
	}
	before := readStore(t, old)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	readOnly := openStore(t, path, false)
	var version int
	if err := readOnly.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatal("inspection migrated schema")
	}
	upgraded := openStore(t, path, true)
	if err := upgraded.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatal("upgrade did not run")
	}
	if !reflect.DeepEqual(before, readStore(t, upgraded)) {
		t.Fatal("upgrade changed catalog")
	}
	if _, err := upgraded.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
}

func TestRequestProcessExitBeforeCommit(t *testing.T) {
	if path := os.Getenv("COPERNICUS_REQUEST_CRASH_DB"); path != "" {
		if err := sqlite.RegisterScalarFunction("crash_request_test", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(23); return nil, nil }); err != nil {
			t.Fatal(err)
		}
		s, err := Open(context.Background(), path, true)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.CreateRequest(context.Background(), requestInput(), requestSource(t))
		t.Fatalf("expected abrupt exit, got %v", err)
	}
	ctx := context.Background()
	s, path := populated(t)
	if _, err := s.db.Exec("CREATE TRIGGER crash_request AFTER INSERT ON executions WHEN NEW.position = 0 BEGIN SELECT crash_request_test(); END"); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRequestProcessExitBeforeCommit$")
	child.Env = append(os.Environ(), "COPERNICUS_REQUEST_CRASH_DB="+path)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child = %v: %s", err, output)
	}
	if _, err := s.db.Exec("DROP TRIGGER crash_request"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"requests", "executions"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("process exit left partial request")
		}
	}
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
}
