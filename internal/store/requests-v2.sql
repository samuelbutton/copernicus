CREATE TABLE requests (
 id TEXT PRIMARY KEY,
 submission TEXT NOT NULL CHECK(json_valid(submission)),
 submission_hash TEXT NOT NULL CHECK(length(submission_hash) = 64),
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot)),
 snapshot_hash TEXT NOT NULL CHECK(length(snapshot_hash) = 64)
) STRICT;
CREATE TABLE executions (
 id TEXT PRIMARY KEY,
 request_id TEXT NOT NULL REFERENCES requests(id),
 position INTEGER NOT NULL CHECK(position >= 0),
 test_id TEXT NOT NULL,
 resolution_status TEXT NOT NULL CHECK(resolution_status IN ('READY', 'RESOLUTION_FAILED')),
 reason TEXT NOT NULL,
 UNIQUE(request_id, position), UNIQUE(request_id, test_id),
 CHECK((resolution_status = 'READY' AND reason = '') OR
       (resolution_status = 'RESOLUTION_FAILED' AND reason <> ''))
) STRICT;
CREATE TRIGGER requests_immutable_update BEFORE UPDATE ON requests
BEGIN SELECT RAISE(ABORT, 'request snapshots are immutable'); END;
CREATE TRIGGER requests_immutable_delete BEFORE DELETE ON requests
BEGIN SELECT RAISE(ABORT, 'request snapshots are immutable'); END;
CREATE TRIGGER executions_immutable_update
BEFORE UPDATE OF id, request_id, position, test_id, resolution_status, reason ON executions
BEGIN SELECT RAISE(ABORT, 'execution resolution is immutable'); END;
CREATE TRIGGER executions_immutable_delete BEFORE DELETE ON executions
BEGIN SELECT RAISE(ABORT, 'execution resolution is immutable'); END;
PRAGMA user_version = 2;
