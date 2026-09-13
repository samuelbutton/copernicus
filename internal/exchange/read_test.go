package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samuelbutton/copernicus/compatibility"
)

func copiedContract(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := "../../compatibility/contract/v1/examples/valid"
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPublishedResultAndEventRelationships(t *testing.T) {
	r, err := OpenReader(copiedContract(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, path := range []string{"results/run-1.json", "results/run-error.json", "events/completed-1.json", "events/pending-1.json"} {
		o, err := r.Observe(context.Background(), path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(o.Identities) == 0 {
			t.Fatal("missing identity guards")
		}
	}
}

func TestRejectInvalidPublishedFixtures(t *testing.T) {
	for _, name := range []string{"unknown-version", "changed-bag-hash", "false-pass", "unavailable-pass", "wrong-analysis-id", "outside-evidence", "parent-path", "absolute-path"} {
		t.Run(name, func(t *testing.T) {
			root := copiedContract(t)
			data, err := os.ReadFile("../../compatibility/contract/v1/examples/invalid/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var kind struct {
				Kind string `json:"kind"`
			}
			if err := json.Unmarshal(data, &kind); err != nil {
				t.Fatal(err)
			}
			path := kind.Kind + "s/invalid.json"
			if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
				t.Fatal(err)
			}
			r, err := OpenReader(root)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err := r.Observe(context.Background(), path); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}

func TestChangedAndUnsafeReferencesCannotPass(t *testing.T) {
	for _, kind := range []string{"changed-job", "truncated-bag", "bad-time", "bad-event", "missing-result", "bag-link", "folder-link"} {
		t.Run(kind, func(t *testing.T) {
			root := copiedContract(t)
			path := "events/completed-1.json"
			switch kind {
			case "changed-job":
				file := filepath.Join(root, "jobs/run-1.json")
				data, _ := os.ReadFile(file)
				data = append(data, ' ')
				if err := os.WriteFile(file, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "truncated-bag", "bad-time":
				file := filepath.Join(root, "bags/exec-1.jsonl")
				data, _ := os.ReadFile(file)
				if kind == "truncated-bag" {
					data = data[:len(data)-1]
				} else {
					data = bytes.Replace(data, []byte(`"time_ms":1000`), []byte(`"time_ms":999`), 1)
				}
				if _, err := compatibility.ParseBag(context.Background(), data); err == nil {
					t.Fatal("accepted malformed bag")
				}
				if err := os.WriteFile(file, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "bad-event":
				file := filepath.Join(root, path)
				data, _ := os.ReadFile(file)
				data = bytes.Replace(data, []byte(`"state": "FAIL"`), []byte(`"state": "PASS"`), 1)
				if err := os.WriteFile(file, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-result":
				if err := os.Remove(filepath.Join(root, "results/run-1.json")); err != nil {
					t.Fatal(err)
				}
			case "bag-link":
				file := filepath.Join(root, "bags/exec-1.jsonl")
				if err := os.Rename(file, filepath.Join(root, "bag-copy")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../bag-copy", file); err != nil {
					t.Fatal(err)
				}
			case "folder-link":
				if err := os.Rename(filepath.Join(root, "bags"), filepath.Join(root, "other-bags")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("other-bags", filepath.Join(root, "bags")); err != nil {
					t.Fatal(err)
				}
			}
			r, err := OpenReader(root)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err := r.Observe(context.Background(), path); err == nil {
				t.Fatal("accepted unsafe outcome")
			}
		})
	}
}
