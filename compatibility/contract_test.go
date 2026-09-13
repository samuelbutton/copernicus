package compatibility

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedManifest(t *testing.T) {
	manifest, err := os.ReadFile("contract/v1/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(manifest)
	if hex.EncodeToString(manifestHash[:]) != "9604a125eee522ef7253d37503ec65231bee72fb9884c8cc0fc7410c36188743" {
		t.Fatal("pinned manifest changed; review the contract upgrade")
	}
	seen := map[string]bool{"SHA256SUMS": true}
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || !filepath.IsLocal(parts[1]) || seen[parts[1]] {
			t.Fatal("invalid manifest")
		}
		seen[parts[1]] = true
		data, err := os.ReadFile(filepath.Join("contract/v1", parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != parts[0] {
			t.Fatalf("changed pinned bytes: %s", parts[1])
		}
	}
	err = filepath.WalkDir("contract/v1", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			relative, _ := filepath.Rel("contract/v1", path)
			if !seen[filepath.ToSlash(relative)] {
				t.Errorf("unlisted file %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("contract/v1/exchange.schema.json")
	if err != nil || !bytes.Equal(data, exchangeSchema) {
		t.Fatal("embedded schema drift")
	}
}

func TestPublishedJobsAndRejections(t *testing.T) {
	for _, name := range []string{"run-1", "run-error", "analyze-1"} {
		data, err := os.ReadFile("contract/v1/examples/valid/jobs/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseJob(data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"changed-input-hash", "duplicate-key", "executable-input", "malformed-hash", "unknown-version"} {
		data, err := os.ReadFile("contract/v1/examples/invalid/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseJob(data); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	valid, err := os.ReadFile("contract/v1/examples/valid/jobs/run-1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{
		append(append([]byte{}, valid...), valid...),
		bytes.Replace(valid, []byte(`"seed": 0`), []byte(`"seed": 0.0`), 1),
		bytes.Replace(valid, []byte(`"seed": 0`), []byte(`"seed": 0e0`), 1),
		bytes.Replace(valid, []byte(`"kind"`), []byte(`"Kind"`), 1),
		[]byte(strings.Repeat(" ", MaxJobBytes+1)),
		[]byte("{\"kind\":\"\xff\"}"),
	} {
		if _, err := ParseJob(data); err == nil {
			t.Fatal("accepted ambiguous or invalid JSON")
		}
	}
}

func TestCanonicalInputHash(t *testing.T) {
	data, err := os.ReadFile("contract/v1/examples/valid/jobs/run-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatal(err)
	}
	digest, err := ContentHash(job.Inputs)
	if err != nil || digest != "648cce0ff71c92754f04495fbec40ea0ef6ab8294565fdefce04e6d1ab8b5690" {
		t.Fatalf("public input hash = %s: %v", digest, err)
	}
	a, err := ContentHash([]byte(`{"z":[-0,9007199254740991],"a":{"z":1,"a":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ContentHash([]byte(`{"a":{"a":2,"z":1},"z":[0,9007199254740991]}`))
	if err != nil || a != b {
		t.Fatal("canonical order or number normalization drift")
	}
	for _, raw := range []string{`{"a":1,"a":2}`, `9007199254740992`, `1e1`, `1.0`, strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)} {
		if _, err := ContentHash([]byte(raw)); err == nil {
			t.Fatal("accepted invalid hash input")
		}
	}
}

func FuzzParseJob(f *testing.F) {
	data, err := os.ReadFile("contract/v1/examples/valid/jobs/run-1.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, data []byte) {
		job, err := ParseJob(data)
		if err != nil {
			return
		}
		hash, err := ContentHash(job.Inputs)
		if err != nil || hash != job.InputsHash {
			t.Fatal("accepted bad input hash")
		}
	})
}
