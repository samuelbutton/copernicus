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
	"github.com/samuelbutton/copernicus/internal/exchange"
	"github.com/samuelbutton/copernicus/internal/request"
	"modernc.org/sqlite"
)

func outboxState(t *testing.T, s *Store, pending, published int) OutboxStatus {
	t.Helper()
	status, err := s.Outbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending != pending || status.Published != published {
		t.Fatalf("outbox = %+v", status)
	}
	return status
}
func queuedFiles(t *testing.T, s *Store) map[string][]byte {
	t.Helper()
	rows, err := s.db.Query("SELECT job_id, content FROM outbox ORDER BY sequence")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	files := map[string][]byte{}
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			t.Fatal(err)
		}
		files[id+".json"] = []byte(body)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return files
}
func checkFiles(t *testing.T, path string, expected map[string][]byte) {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(path, "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(expected) {
		t.Fatalf("files=%d expected=%d", len(files), len(expected))
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(path, "jobs", file.Name()))
		if err != nil || !bytes.Equal(data, expected[file.Name()]) {
			t.Fatalf("changed published bytes: %s", file.Name())
		}
		if _, err := compatibility.ParseJob(data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOutboxAtomicSaveAndImmutableRetry(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	if _, err := s.db.Exec("CREATE TRIGGER reject_job BEFORE INSERT ON outbox BEGIN SELECT RAISE(ABORT, 'test outbox failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err == nil {
		t.Fatal("accepted request without outbox")
	}
	for _, table := range []string{"requests", "executions", "outbox"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("partial %s: %v", table, err)
		}
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_job"); err != nil {
		t.Fatal(err)
	}
	record, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	original := queuedFiles(t, s)
	outboxState(t, s, 3, 0)
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{"empty-lane"}}); err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateRequest(ctx, requestInput(), requestSource(t))
	if err != nil || !reflect.DeepEqual(record, again) || !reflect.DeepEqual(original, queuedFiles(t, s)) {
		t.Fatal("retry changed durable work")
	}
	for _, statement := range []string{"UPDATE outbox SET content='{}'", "DELETE FROM outbox", "UPDATE outbox SET job_id='changed'", "UPDATE outbox SET execution_id='changed'"} {
		if _, err := s.db.Exec(statement); err == nil {
			t.Fatal("changed immutable outbox")
		}
	}
	path := t.TempDir()
	if n, err := s.PublishOutbox(ctx, path, 1); err != nil || n != 1 {
		t.Fatalf("first batch=%d %v", n, err)
	}
	outboxState(t, s, 2, 1)
	if n, err := s.PublishOutbox(ctx, path, 100); err != nil || n != 2 {
		t.Fatalf("second batch=%d %v", n, err)
	}
	if n, err := s.PublishOutbox(ctx, path, 100); err != nil || n != 0 {
		t.Fatalf("repeat=%d %v", n, err)
	}
	outboxState(t, s, 0, 3)
	checkFiles(t, path, original)
	if _, err := s.db.Exec("UPDATE outbox SET published=0"); err == nil {
		t.Fatal("reset publication")
	}
	other := t.TempDir()
	if _, err := s.PublishOutbox(ctx, other, 100); err == nil {
		t.Fatal("silently changed destination")
	}
	files, err := os.ReadDir(other)
	if err != nil || len(files) != 0 {
		t.Fatal("wrote to conflicting destination")
	}
}

func TestOutboxOnlyDispatchesReadyTests(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"), true)
	c := example(t)
	c.Scenarios[1].Type = "unsupported"
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
	outboxState(t, s, 2, 0)
	for _, data := range queuedFiles(t, s) {
		job, err := compatibility.ParseJob(data)
		if err != nil || job.ExecutionID == snapshot.Executions[0].ID {
			t.Fatal("dispatched incompatible test")
		}
	}
	if _, err := s.db.Exec("INSERT INTO outbox(job_id,execution_id,request_id,content,content_hash) VALUES ('bad', ?, ?, '{}', ?)", snapshot.Executions[0].ID, requestInput().ID, request.Hash([]byte("{}"))); err == nil {
		t.Fatal("SQL accepted unresolved execution")
	}
}

func TestConcurrentOutboxPublication(t *testing.T) {
	s, path := populated(t)
	if _, err := s.CreateRequest(context.Background(), requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	want := queuedFiles(t, s)
	exchange := t.TempDir()
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(context.Background(), path, true)
			if err != nil {
				results <- err
				return
			}
			defer db.Close()
			_, err = db.PublishOutbox(context.Background(), exchange, 100)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	outboxState(t, s, 0, 3)
	checkFiles(t, exchange, want)
}

func TestOutboxConflictLeavesDeliveryPending(t *testing.T) {
	s, _ := populated(t)
	ctx := context.Background()
	if _, err := s.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
		t.Fatal(err)
	}
	var first string
	if err := s.db.QueryRow("SELECT job_id FROM outbox ORDER BY sequence LIMIT 1").Scan(&first); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "jobs", first+".json")
	if err := os.WriteFile(file, []byte("conflicting original bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PublishOutbox(ctx, path, 100); n != 0 || !errors.Is(err, exchange.ErrConflict) {
		t.Fatalf("conflict = %d, %v", n, err)
	}
	outboxState(t, s, 3, 0)
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "conflicting original bytes" {
		t.Fatal("overwrote conflicting file")
	}
}

func TestOutboxProcessExitRecovery(t *testing.T) {
	if path := os.Getenv("COPERNICUS_OUTBOX_CRASH_DB"); path != "" {
		if err := sqlite.RegisterScalarFunction("crash_outbox_test", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(23); return nil, nil }); err != nil {
			t.Fatal(err)
		}
		s, err := Open(context.Background(), path, true)
		if err != nil {
			t.Fatal(err)
		}
		if os.Getenv("COPERNICUS_OUTBOX_CRASH_PHASE") == "ack" {
			_, err = s.PublishOutbox(context.Background(), os.Getenv("COPERNICUS_OUTBOX_CRASH_EXCHANGE"), 100)
		} else {
			_, err = s.CreateRequest(context.Background(), requestInput(), requestSource(t))
		}
		if err != nil {
			t.Fatal(err)
		}
		os.Exit(23) // The commit case exits immediately after CreateRequest returns.
	}
	for _, phase := range []string{"save", "commit", "ack"} {
		t.Run(phase, func(t *testing.T) {
			s, path := populated(t)
			exchange := t.TempDir()
			if phase == "save" {
				if _, err := s.db.Exec("CREATE TRIGGER crash_outbox AFTER INSERT ON outbox WHEN NEW.sequence=2 BEGIN SELECT crash_outbox_test(); END"); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "ack" {
				if _, err := s.CreateRequest(context.Background(), requestInput(), requestSource(t)); err != nil {
					t.Fatal(err)
				}
				if _, err := s.db.Exec("CREATE TRIGGER crash_outbox BEFORE UPDATE OF published ON outbox BEGIN SELECT crash_outbox_test(); END"); err != nil {
					t.Fatal(err)
				}
			}
			child := exec.Command(os.Args[0], "-test.run=^TestOutboxProcessExitRecovery$")
			child.Env = append(os.Environ(), "COPERNICUS_OUTBOX_CRASH_DB="+path, "COPERNICUS_OUTBOX_CRASH_PHASE="+phase, "COPERNICUS_OUTBOX_CRASH_EXCHANGE="+exchange)
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("child=%v %s", err, output)
			}
			if phase != "commit" {
				if _, err := s.db.Exec("DROP TRIGGER crash_outbox"); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "save" {
				outboxState(t, s, 0, 0)
				if _, err := s.Request(context.Background(), requestInput().ID); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("partial request: %v", err)
				}
				if _, err := s.CreateRequest(context.Background(), requestInput(), requestSource(t)); err != nil {
					t.Fatal(err)
				}
			}
			outboxState(t, s, 3, 0)
			if phase == "commit" {
				files, err := os.ReadDir(exchange)
				if err != nil || len(files) != 0 {
					t.Fatal("published before restart")
				}
			}
			var delivered []byte
			if phase == "ack" {
				files, err := os.ReadDir(filepath.Join(exchange, "jobs"))
				if err != nil || len(files) != 1 {
					t.Fatal("expected one file before acknowledgement")
				}
				delivered, err = os.ReadFile(filepath.Join(exchange, "jobs", files[0].Name()))
				if err != nil {
					t.Fatal(err)
				}
			}
			want := queuedFiles(t, s)
			restarted := openStore(t, path, true)
			if _, err := restarted.PublishOutbox(context.Background(), exchange, 100); err != nil {
				t.Fatal(err)
			}
			outboxState(t, restarted, 0, 3)
			checkFiles(t, exchange, want)
			if delivered != nil {
				job, err := compatibility.ParseJob(delivered)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(want[job.JobID+".json"], delivered) {
					t.Fatal("restart changed pre-ack bytes")
				}
			}
		})
	}
}

func TestSchemaTwoBackfillUsesSnapshots(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "corrupt"}[corrupt], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "old.sqlite")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(schema + "\n" + requestsSchema); err != nil {
				t.Fatal(err)
			}
			s := &Store{db: db}
			c := example(t)
			if err := s.Import(ctx, c); err != nil {
				t.Fatal(err)
			}
			snapshot, err := request.Resolve(c, requestInput(), requestSource(t))
			if err != nil {
				t.Fatal(err)
			}
			record, err := request.Encode(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			submission, err := json.Marshal(snapshot.Submission)
			if err != nil {
				t.Fatal(err)
			}
			hash := record.SnapshotSHA256
			if corrupt {
				hash = request.Hash([]byte("changed"))
			}
			if _, err := db.Exec("INSERT INTO requests VALUES (?, ?, ?, ?, ?)", requestInput().ID, string(submission), record.SubmissionSHA256, string(record.Snapshot), hash); err != nil {
				t.Fatal(err)
			}
			for i, e := range snapshot.Executions {
				if _, err := db.Exec("INSERT INTO executions VALUES (?, ?, ?, ?, ?, ?)", e.ID, requestInput().ID, i, e.Test.ID, e.Status, e.Reason); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{}}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reader := openStore(t, path, false)
			var version int
			if err := reader.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
				t.Fatal("inspection upgraded schema")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("read-only open wrote database")
			}
			upgraded, err := Open(ctx, path, true)
			if corrupt {
				if err == nil {
					upgraded.Close()
					t.Fatal("accepted corrupt migration")
				}
				if err := reader.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
					t.Fatal("partial schema migration")
				}
				var n int
				if err := reader.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='outbox'").Scan(&n); err != nil || n != 0 {
					t.Fatal("partial outbox migration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer upgraded.Close()
			outboxState(t, upgraded, 3, 0)
			read, err := upgraded.Request(ctx, requestInput().ID)
			if err != nil || !reflect.DeepEqual(record, read) {
				t.Fatal("migration changed accepted snapshot")
			}
			if _, err := upgraded.CreateRequest(ctx, requestInput(), requestSource(t)); err != nil {
				t.Fatal(err)
			}
			outboxState(t, upgraded, 3, 0)
		})
	}
}
