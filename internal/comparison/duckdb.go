package comparison

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const DuckDBVersion = "v1.5.5"
const MaxResultBytes = 64 << 20

//go:embed deltas.sql
var deltaSQL string

type pair struct {
	ID        string `json:"id"`
	Baseline  string `json:"baseline_job"`
	Candidate string `json:"candidate_job"`
}
type metricRow struct {
	ID         string `json:"id"`
	Compatible bool   `json:"compatible"`
	Metric
}

// Evaluate queries exact validated bytes in private temporary files. DuckDB
// never reopens the mutable exchange, SQLite database, or user configuration.
func Evaluate(ctx context.Context, executable string, report Report, files map[string][]byte) (out Report, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pairs := []pair{}
	selected := map[string]bool{}
	for _, row := range report.Rows {
		if row.Kind == "COMPARED" {
			b, c := row.Baseline[0].JobID, row.Candidate[0].JobID
			pairs = append(pairs, pair{row.ID, b, c})
			selected[b] = true
			selected[c] = true
		}
	}
	if len(pairs) == 0 {
		return report, ctx.Err()
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return out, fmt.Errorf("DuckDB %s is required; run make install-duckdb or provide --duckdb PATH: %w", DuckDBVersion, err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return out, err
	}
	dir, err := os.MkdirTemp("", "copernicus-comparison-")
	if err != nil {
		return out, err
	}
	defer func(created string) { err = errors.Join(err, os.RemoveAll(created)) }(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return out, err
	}
	version, err := execute(ctx, path, dir, "SELECT version() AS version;", 4096)
	if err != nil {
		return out, err
	}
	var versions []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(version, &versions); err != nil || len(versions) != 1 || versions[0].Version != DuckDBVersion {
		return out, fmt.Errorf("comparison requires DuckDB %s", DuckDBVersion)
	}
	jobs := make([]string, 0, len(selected))
	for job := range selected {
		jobs = append(jobs, job)
	}
	sort.Strings(jobs)
	paths := []string{}
	size := 0
	for i, job := range jobs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		data, ok := files[job]
		if !ok {
			return out, errors.New("selected result bytes unavailable")
		}
		size += len(data)
		if size > MaxResultBytes {
			return out, errors.New("comparison exceeds result byte limit")
		}
		name := filepath.Join(dir, fmt.Sprintf("result-%d.json", i))
		if err := os.WriteFile(name, data, 0600); err != nil {
			return out, err
		}
		paths = append(paths, name)
	}
	pairPath := filepath.Join(dir, "pairs.json")
	data, err := json.Marshal(pairs)
	if err != nil {
		return out, err
	}
	if err := os.WriteFile(pairPath, data, 0600); err != nil {
		return out, err
	}
	allowed := append(append([]string{}, paths...), pairPath)
	sql := "SET threads=1; SET memory_limit='128MB'; SET temp_directory=''; SET autoinstall_known_extensions=false; SET autoload_known_extensions=false; SET allowed_paths=" + sqlList(allowed) + "; SET enable_external_access=false; SET lock_configuration=true;\n" +
		"CREATE VIEW pairs AS SELECT * FROM read_json(" + literal(pairPath) + ", columns={id:'VARCHAR', baseline_job:'VARCHAR', candidate_job:'VARCHAR'});\n" +
		"CREATE VIEW outcomes AS SELECT json FROM read_json_objects(" + sqlList(paths) + ");\n" + deltaSQL
	data, err = execute(ctx, path, dir, sql, 4<<20)
	if err != nil {
		return out, err
	}
	var metrics []metricRow
	if err := json.Unmarshal(data, &metrics); err != nil {
		return out, fmt.Errorf("decode DuckDB metrics: %w", err)
	}
	if len(metrics) != len(pairs)*3 {
		return out, errors.New("incomplete DuckDB metric rows")
	}
	out = report
	out.Rows = append([]Row{}, report.Rows...)
	byID := map[string]int{}
	seen := map[string]bool{}
	for i, row := range out.Rows {
		if row.Kind == "COMPARED" {
			byID[row.ID] = i
		}
	}
	for _, m := range metrics {
		i, ok := byID[m.ID]
		identity := m.ID + "/" + m.Name
		if !ok || seen[identity] || (m.Name != "collision_count" && m.Name != "minimum_obstacle_gap" && m.Name != "goal_progress") {
			return Report{}, errors.New("unexpected DuckDB metric identity")
		}
		seen[identity] = true
		row := &out.Rows[i]
		if !m.Compatible {
			row.Kind, row.Reason = "INCOMPARABLE", "METRIC_VERSION_OR_UNIT_CHANGED"
			row.StatusChanged = nil
		}
		row.Metrics = append(row.Metrics, m.Metric)
	}
	for i := range out.Rows {
		if out.Rows[i].Kind != "COMPARED" {
			out.Rows[i].Metrics = []Metric{}
		}
	}
	out.recount()
	return out, ctx.Err()
}

// Paths are locally generated values, quoted as SQL literals, never SQL syntax.
func literal(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func sqlList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = literal(v)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("DuckDB output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func execute(ctx context.Context, path, dir, sql string, limit int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, "-init", os.DevNull, "-batch", "-bail", "-json", ":memory:")
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + dir, "TMPDIR=" + dir, "TZ=UTC"}
	cmd.Stdin = strings.NewReader(sql)
	stdout, stderr := &boundedBuffer{limit: limit}, &boundedBuffer{limit: 8192}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("DuckDB query failed: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
