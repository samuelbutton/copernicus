package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

func TestReviewEvidenceIsScopedAndRevalidated(t *testing.T) {
	ctx := context.Background()
	s, _ := populated(t)
	input := requestInput()
	input.SuiteIDs = []string{"smoke"}
	record, err := s.CreateRequest(ctx, input, requestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot request.Snapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	execution := snapshot.Executions[0].ID
	review, err := s.Review(ctx, input.ID, execution)
	if err != nil || review.Outcome != nil || review.Progress.State != "QUEUED" {
		t.Fatalf("queued review: %+v %v", review, err)
	}
	if _, err := s.Review(ctx, input.ID, "unknown"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("accepted unrelated execution")
	}
	if err := s.ReplaceSuite(ctx, catalog.Suite{ID: "smoke", TestIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, input, requestSource(t))
	if err != nil || retry.SnapshotSHA256 != record.SnapshotSHA256 {
		t.Fatal("retry lost frozen selection")
	}
	root := t.TempDir()
	path, _ := mappedOutcome(t, s, root)
	if _, err := s.ImportResults(ctx, root, []string{path}); err != nil {
		t.Fatal(err)
	}
	review, err = s.Review(ctx, input.ID, execution)
	if err != nil || review.Outcome == nil || review.Progress.Result == nil {
		t.Fatal("missing reviewed outcome")
	}
	tick := -1
	for _, metric := range review.Outcome.Metrics {
		if len(metric.EvidenceTicks) > 0 {
			tick = metric.EvidenceTicks[0]
			break
		}
	}
	if tick < 0 {
		t.Fatal("fixture lacks evidence")
	}
	data, err := s.Evidence(ctx, input.ID, execution, tick)
	var parsed struct {
		Tick int `json:"tick"`
	}
	if err != nil || json.Unmarshal(data, &parsed) != nil || parsed.Tick != tick {
		t.Fatalf("evidence: %s %v", data, err)
	}
	if _, err := s.Evidence(ctx, input.ID, execution, 100000); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("uncited tick accepted")
	}
	if err := os.Remove(filepath.Join(root, review.Outcome.Bag.Path)); err != nil {
		t.Fatal(err)
	}
	review, err = s.Review(ctx, input.ID, execution)
	if err != nil || review.Outcome != nil || review.Progress.State != "INCOMPLETE" {
		t.Fatal("missing recording retained scores")
	}
	if _, err := s.Evidence(ctx, input.ID, execution, tick); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing recording retained evidence")
	}
}
