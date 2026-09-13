// Package request resolves immutable request inputs without database or file I/O.
package request

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samuelbutton/copernicus/internal/catalog"
)

const (
	SnapshotVersion  = 1
	MaxTests         = 1000
	MaxSnapshotBytes = 16 << 20
	Ready            = "READY"
	ResolutionFailed = "RESOLUTION_FAILED"
	Resolved         = "RESOLVED"
)

var ErrIdentityConflict = errors.New("request identifier already has a different submission")

type Submission struct {
	ID           string `json:"id"`
	CollectionID string `json:"collection_id"`
	ControllerID string `json:"controller_id"`
	Requester    string `json:"requester"`
	Priority     int    `json:"priority"`
	Seed         int64  `json:"seed"`
	Repeat       int    `json:"repeat"`
}

func (s Submission) Validate() error {
	for _, id := range []string{s.ID, s.CollectionID, s.ControllerID, s.Requester} {
		if err := catalog.ValidateID(id); err != nil {
			return err
		}
	}
	if s.Priority < 0 || s.Priority > 3 || s.Seed < 0 || s.Seed > 9007199254740991 || s.Repeat < 0 || s.Repeat > 100000 {
		return errors.New("priority must be 0–3, seed 0–9007199254740991, and repeat 0–100000")
	}
	return nil
}

// Frozen separates a catalog label from the exact JSON bytes identified by SHA256.
type Frozen struct {
	ID      string          `json:"id"`
	SHA256  string          `json:"sha256"`
	Content json.RawMessage `json:"content"`
}

type Execution struct {
	ID               string `json:"id"`
	Test             Frozen `json:"test"`
	Scenario         Frozen `json:"scenario"`
	RunTemplate      Frozen `json:"run_template"`
	AnalysisTemplate Frozen `json:"analysis_template"`
	Simulator        Frozen `json:"simulator"`
	Seed             int64  `json:"seed"`
	Repeat           int    `json:"repeat"`
	Status           string `json:"resolution_status"`
	Reason           string `json:"reason"`
}

type Snapshot struct {
	Version    int         `json:"version"`
	Submission Submission  `json:"submission"`
	Source     Frozen      `json:"source"`
	Collection Frozen      `json:"collection"`
	Suites     []Frozen    `json:"suites"`
	Controller Frozen      `json:"controller"`
	Executions []Execution `json:"executions"`
	Status     string      `json:"resolution_status"`
}

// Record stores hashes beside the immutable encoded snapshot, not inside it.
type Record struct {
	SubmissionSHA256 string          `json:"submission_sha256"`
	SnapshotSHA256   string          `json:"snapshot_sha256"`
	Snapshot         json.RawMessage `json:"snapshot"`
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Freeze uses encoding/json with sorted top-level keys and typed nested fields.
// The catalog identifier is metadata, so renaming it does not change content identity.
func Freeze(id string, value any) (Frozen, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return Frozen{}, fmt.Errorf("encode snapshot content: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Frozen{}, err
	}
	if fields == nil {
		return Frozen{}, errors.New("snapshot content must be an object")
	}
	delete(fields, "id")
	data, err = json.Marshal(fields)
	if err != nil {
		return Frozen{}, err
	}
	return Frozen{ID: id, SHA256: Hash(data), Content: data}, nil
}

func Encode(snapshot Snapshot) (Record, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return Record{}, fmt.Errorf("encode snapshot: %w", err)
	}
	if len(data) > MaxSnapshotBytes {
		return Record{}, fmt.Errorf("snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	submission, err := json.Marshal(snapshot.Submission)
	if err != nil {
		return Record{}, err
	}
	return Record{SubmissionSHA256: Hash(submission), SnapshotSHA256: Hash(data), Snapshot: data}, nil
}
