package compatibility

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const MaxJobBytes = 1 << 20

//go:embed contract/v1/exchange.schema.json
var exchangeSchema []byte

// Job is the public envelope. Inputs remain JSON so validation cannot silently
// discard fields while translating or checking saved outbox bytes.
type Job struct {
	ContractVersion int             `json:"contract_version"`
	Kind            string          `json:"kind"`
	ExecutionID     string          `json:"execution_id"`
	JobID           string          `json:"job_id"`
	JobKind         string          `json:"job_kind"`
	Priority        int             `json:"priority"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
	Inputs          json.RawMessage `json:"inputs"`
	InputsHash      string          `json:"inputs_hash"`
}

var documentSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(exchangeSchema))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(localOnly{})
	const base = "https://yamata.invalid/contract/v1/exchange.schema.json"
	if err := c.AddResource(base, value); err != nil {
		return nil, err
	}
	schemas := map[string]*jsonschema.Schema{}
	for _, kind := range []string{"job", "result", "event", "bag", "tick"} {
		schema, err := c.Compile(base + "#/$defs/" + kind)
		if err != nil {
			return nil, err
		}
		schemas[kind] = schema
	}
	return schemas, nil
})

type localOnly struct{}

func (localOnly) Load(string) (any, error) { return nil, errors.New("external schemas are disabled") }

// ParseJob checks the pinned schema, unambiguous integer JSON, and input hash.
// It validates file structure, not local implementation availability or results.
func ParseJob(data []byte) (Job, error) {
	var job Job
	if err := ValidateDocument(data, "job", &job); err != nil {
		return job, err
	}
	digest, err := ContentHash(job.Inputs)
	if err != nil {
		return job, err
	}
	if digest != job.InputsHash {
		return job, errors.New("job inputs hash mismatch")
	}
	return job, nil
}

// ContentHash implements the public restricted input encoding: recursively
// sorted object keys, retained array order, and shortest decimal integers.
func ContentHash(data []byte) (string, error) {
	value, err := decode(data)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hashBytes(canonical), nil
}

func decode(data []byte) (any, error) {
	if len(data) > MaxJobBytes || !utf8.Valid(data) {
		return nil, errors.New("contract JSON must be UTF-8 and at most 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	value, err := readValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected one JSON value")
	}
	return value, nil
}

func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, errors.New("JSON nesting exceeds 32 levels")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid object key")
			}
			if _, exists := object[name]; exists {
				return nil, errors.New("duplicate object key")
			}
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		_, err := d.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err := d.Token()
		return array, err
	}
	if n, ok := token.(json.Number); ok {
		integer, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil || integer < -9007199254740991 || integer > 9007199254740991 {
			return nil, errors.New("expected an exact decimal JSON integer")
		}
		return json.Number(strconv.FormatInt(integer, 10)), nil
	}
	if _, ok := token.(json.Delim); ok {
		return nil, errors.New("unexpected JSON delimiter")
	}
	return token, nil
}

// ValidateDocument checks a complete JSON record against one pinned schema.
// Relationship checks remain necessary before accepting a result or event.
func ValidateDocument(data []byte, kind string, target any) error {
	value, err := decode(data)
	if err != nil {
		return err
	}
	schemas, err := documentSchemas()
	if err != nil {
		return err
	}
	schema, ok := schemas[kind]
	if !ok {
		return errors.New("unsupported document kind")
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("invalid %s schema: %w", kind, err)
	}
	return json.Unmarshal(data, target)
}

func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
