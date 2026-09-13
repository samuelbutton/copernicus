package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxImportBytes = 1 << 20

// Decode bounds input and rejects ambiguous JSON before decoding typed records.
func Decode(r io.Reader) (Catalog, error) {
	c := emptyCatalog()
	data, err := io.ReadAll(io.LimitReader(r, MaxImportBytes+1))
	if err != nil {
		return c, fmt.Errorf("read catalog: %w", err)
	}
	if len(data) > MaxImportBytes {
		return c, fmt.Errorf("catalog exceeds %d bytes", MaxImportBytes)
	}
	if !utf8.Valid(data) {
		return c, errors.New("catalog must contain valid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := checkJSON(d, 0); err != nil {
		return c, fmt.Errorf("catalog JSON: %w", err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return c, errors.New("catalog must contain exactly one JSON object")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("decode catalog: %w", err)
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func checkJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("nesting exceeds 32 levels")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		if token == nil {
			return errors.New("null is not a catalog value")
		}
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("unexpected delimiter")
	}
	keys := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("object key must be a string")
			}
			// Catalog field names use lowercase ASCII. This also prevents the typed
			// decoder from accepting case-insensitive aliases of the same field.
			for _, c := range name {
				if c != '_' && (c < 'a' || c > 'z') {
					return fmt.Errorf("invalid field name %q", name)
				}
			}
			if keys[name] {
				return fmt.Errorf("duplicate field %q", name)
			}
			keys[name] = true
		}
		if err := checkJSON(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}
