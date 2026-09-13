CREATE TABLE imported_identities (
    id TEXT PRIMARY KEY,
    hash TEXT NOT NULL CHECK(length(hash) = 64)
) STRICT;
CREATE TRIGGER imported_identity_immutable BEFORE UPDATE ON imported_identities
BEGIN SELECT RAISE(ABORT, 'published identities are immutable'); END;
CREATE TRIGGER imported_identity_retained BEFORE DELETE ON imported_identities
BEGIN SELECT RAISE(ABORT, 'published identities must be retained'); END;
CREATE TABLE indexed_results (
    id TEXT PRIMARY KEY REFERENCES imported_identities(id),
    job_id TEXT NOT NULL UNIQUE,
    execution_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    job_hash TEXT NOT NULL CHECK(length(job_hash) = 64),
    path TEXT NOT NULL,
    hash TEXT NOT NULL CHECK(length(hash) = 64),
    content TEXT NOT NULL CHECK(json_valid(content) AND length(CAST(content AS BLOB)) <= 1048576)
) STRICT;
CREATE INDEX indexed_execution ON indexed_results(execution_id);
CREATE TABLE imported_events (
    event_id TEXT PRIMARY KEY,
    hash TEXT NOT NULL CHECK(length(hash) = 64),
    job_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK(sequence > 0),
    state TEXT NOT NULL CHECK(state IN ('PENDING','RUNNING','ANALYZING','PASS','FAIL','WARN','ERROR')),
    UNIQUE(job_id, attempt_id, sequence)
) STRICT;
CREATE INDEX events_by_job ON imported_events(job_id);
PRAGMA user_version = 4;
