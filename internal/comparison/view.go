package comparison

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"

	"github.com/samuelbutton/copernicus/internal/request"
)

// Options describe a reproducible filtered page. Row IDs remain stable cursors.
type Options struct {
	Filter string `json:"filter"`
	Sort   string `json:"sort"`
	After  string `json:"after"`
	Limit  int    `json:"limit"`
}

var rowID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (o Options) Validate() error {
	if o.Filter != "all" && o.Filter != "regressions" && o.Filter != "incomplete" && o.Filter != "incomparable" && o.Filter != "errors" {
		return errors.New("unsupported comparison filter")
	}
	if o.Sort != "id" && o.Sort != "id-desc" {
		return errors.New("unsupported comparison sort")
	}
	if o.Limit < 1 || o.Limit > 2000 || o.After != "" && !rowID.MatchString(o.After) {
		return errors.New("invalid comparison page")
	}
	return nil
}

type ViewInfo struct {
	Options
	MatchedRows int    `json:"matched_rows"`
	NextAfter   string `json:"next_after,omitempty"`
	CacheKey    string `json:"cache_key,omitempty"`
	CacheState  string `json:"cache_state"`
}

// CacheInputs includes every frozen selection, result hash, query and view choice.
// The plan contains sorted rows and entries, so map iteration cannot change a key.
func CacheInputs(plan Report, options Options) ([]byte, error) {
	return json.Marshal(struct {
		Plan    Report  `json:"plan"`
		Options Options `json:"options"`
		SQLHash string  `json:"sql_sha256"`
		Engine  string  `json:"engine_version"`
	}{plan, options, request.Hash([]byte(deltaSQL)), DuckDBVersion})
}

// Select preserves unfiltered denominators and gives a separate filtered row count.
func Select(report Report, options Options) Report {
	rows := []Row{}
	for _, row := range report.Rows {
		include := options.Filter == "all" || options.Filter == "incomplete" && row.Kind == "INCOMPLETE" || options.Filter == "incomparable" && row.Kind == "INCOMPARABLE" || options.Filter == "errors" && row.Kind == "ERROR"
		if options.Filter == "regressions" {
			for _, metric := range row.Metrics {
				include = include || metric.Change == "REGRESSION"
			}
		}
		if include {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if options.Sort == "id-desc" {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].ID < rows[j].ID
	})
	info := &ViewInfo{Options: options, MatchedRows: len(rows)}
	start := 0
	if options.After != "" {
		for start < len(rows) && (options.Sort == "id" && rows[start].ID <= options.After || options.Sort == "id-desc" && rows[start].ID >= options.After) {
			start++
		}
	}
	end := min(start+options.Limit, len(rows))
	if end < len(rows) {
		info.NextAfter = rows[end-1].ID
	}
	report.Rows = rows[start:end]
	report.View = info
	return report
}
