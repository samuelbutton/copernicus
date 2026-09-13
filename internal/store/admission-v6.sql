CREATE TABLE team_budgets (
 team_id TEXT PRIMARY KEY,
 tick_limit INTEGER NOT NULL CHECK(tick_limit BETWEEN 0 AND 1000000000000)
) STRICT;
CREATE TABLE budget_reservations (
 request_id TEXT PRIMARY KEY REFERENCES requests(id),
 team_id TEXT NOT NULL REFERENCES team_budgets(team_id),
 ticks INTEGER NOT NULL CHECK(ticks BETWEEN 0 AND 1000000000000)
) STRICT;
CREATE INDEX budget_team ON budget_reservations(team_id);
CREATE TRIGGER reservation_immutable BEFORE UPDATE ON budget_reservations
BEGIN SELECT RAISE(ABORT,'budget reservation is immutable'); END;
CREATE TRIGGER reservation_retained BEFORE DELETE ON budget_reservations
BEGIN SELECT RAISE(ABORT,'budget reservation is immutable'); END;
CREATE TRIGGER budget_admission BEFORE INSERT ON budget_reservations
WHEN NEW.ticks > (SELECT tick_limit FROM team_budgets WHERE team_id=NEW.team_id)
 - (SELECT coalesce(sum(ticks),0) FROM budget_reservations WHERE team_id=NEW.team_id)
BEGIN SELECT RAISE(ABORT,'team budget exceeded'); END;
CREATE TRIGGER budget_floor BEFORE UPDATE ON team_budgets
WHEN NEW.team_id != OLD.team_id OR NEW.tick_limit < (SELECT coalesce(sum(ticks),0) FROM budget_reservations WHERE team_id=OLD.team_id)
BEGIN SELECT RAISE(ABORT,'budget cannot undercut accepted reservations'); END;
CREATE TABLE saved_comparisons (
 cache_key TEXT PRIMARY KEY CHECK(length(cache_key)=64),
 inputs TEXT NOT NULL CHECK(json_valid(inputs) AND length(CAST(inputs AS BLOB))<=4194304),
 content TEXT NOT NULL CHECK(json_valid(content) AND length(CAST(content AS BLOB))<=4194304),
 content_hash TEXT NOT NULL CHECK(length(content_hash)=64)
) STRICT;
CREATE TRIGGER comparison_immutable BEFORE UPDATE ON saved_comparisons
BEGIN SELECT RAISE(ABORT,'saved comparison is immutable'); END;
PRAGMA user_version = 6;
