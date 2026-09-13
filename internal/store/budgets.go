package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samuelbutton/copernicus/internal/catalog"
	"github.com/samuelbutton/copernicus/internal/request"
)

//go:embed admission-v6.sql
var admissionSchema string

const DefaultTeam = "local"
const DefaultTickLimit int64 = 10000000
const MaxTickLimit int64 = 1000000000000

var ErrBudgetExceeded = errors.New("team simulation budget exceeded")
var ErrTeamUnavailable = errors.New("team budget is not configured")

type Budget struct {
	TeamID    string `json:"team_id"`
	Limit     int64  `json:"tick_limit"`
	Reserved  int64  `json:"reserved_ticks"`
	Remaining int64  `json:"remaining_ticks"`
}

func budgetTeam(input request.Submission) string {
	if input.TeamID == "" {
		return DefaultTeam
	}
	return input.TeamID
}
func (s *Store) Budgets(ctx context.Context) ([]Budget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.team_id,b.tick_limit,coalesce(sum(r.ticks),0) FROM team_budgets b LEFT JOIN budget_reservations r ON r.team_id=b.team_id GROUP BY b.team_id ORDER BY b.team_id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Budget{}
	for rows.Next() {
		var b Budget
		if err := rows.Scan(&b.TeamID, &b.Limit, &b.Reserved); err != nil {
			return nil, err
		}
		b.Remaining = b.Limit - b.Reserved
		out = append(out, b)
	}
	if len(out) > 1000 {
		return nil, errors.New("too many teams")
	}
	return out, rows.Err()
}

// SetBudget changes a cumulative limit without releasing accepted reservations.
func (s *Store) SetBudget(ctx context.Context, team string, limit int64) error {
	if catalog.ValidateID(team) != nil || limit < 0 || limit > MaxTickLimit {
		return errors.New("provide a team and tick limit between 0 and 1000000000000")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_budgets VALUES(?,?) ON CONFLICT(team_id) DO UPDATE SET tick_limit=excluded.tick_limit`, team, limit); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM team_budgets").Scan(&count); err != nil {
		return err
	}
	if count > 1000 {
		return errors.New("too many teams")
	}
	return tx.Commit()
}
func reserveBudget(ctx context.Context, tx *sql.Tx, snapshot request.Snapshot) error {
	ticks, err := request.SimulationTicks(snapshot)
	if err != nil {
		return err
	}
	team := budgetTeam(snapshot.Submission)
	var remaining int64
	err = tx.QueryRowContext(ctx, `SELECT tick_limit-(SELECT coalesce(sum(ticks),0) FROM budget_reservations WHERE team_id=?) FROM team_budgets WHERE team_id=?`, team, team).Scan(&remaining)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTeamUnavailable
	}
	if err != nil {
		return err
	}
	if ticks > remaining {
		return fmt.Errorf("%w: team %s needs %d ticks; %d remain", ErrBudgetExceeded, team, ticks, remaining)
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO budget_reservations VALUES(?,?,?)", snapshot.Submission.ID, team, ticks)
	return err
}

func upgradeAdmission(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, admissionSchema); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM requests ORDER BY id LIMIT 1001")
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	if len(ids) > maxRequests {
		return errors.New("too many legacy requests")
	}
	snapshots := []request.Snapshot{}
	totals := map[string]int64{DefaultTeam: 0}
	for _, id := range ids {
		record, _, err := readRequest(ctx, tx, id)
		if err != nil {
			return err
		}
		var snapshot request.Snapshot
		if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
			return err
		}
		cost, err := request.SimulationTicks(snapshot)
		if err != nil {
			return err
		}
		team := budgetTeam(snapshot.Submission)
		totals[team] += cost
		snapshots = append(snapshots, snapshot)
	}
	// Legacy admissions remain valid. Charge their frozen cost and leave no
	// extra headroom when it already exceeds the normal default allowance.
	for team, reserved := range totals {
		if _, err := tx.ExecContext(ctx, "INSERT INTO team_budgets VALUES(?,?)", team, max(DefaultTickLimit, reserved)); err != nil {
			return err
		}
	}
	for _, snapshot := range snapshots {
		if err := reserveBudget(ctx, tx, snapshot); err != nil {
			return err
		}
	}
	return nil
}
