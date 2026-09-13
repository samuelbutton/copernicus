// Package compatibility records the reviewed public execution baseline.
package compatibility

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

//go:embed yamata.json
var yamataJSON []byte

type Source struct {
	Repository      string `json:"repository"`
	Revision        string `json:"revision"`
	ContractVersion int    `json:"contract_version"`
}

// Yamata returns a fresh value; request snapshots retain this source record.
func Yamata() (Source, error) {
	var source Source
	if err := json.Unmarshal(yamataJSON, &source); err != nil {
		return source, fmt.Errorf("read execution source: %w", err)
	}
	return source, source.Validate()
}

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (s Source) Validate() error {
	if s.Repository != "https://github.com/samuelbutton/yamata" || !revisionPattern.MatchString(s.Revision) || s.ContractVersion != 1 {
		return errors.New("unsupported execution source")
	}
	return nil
}
