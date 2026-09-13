DROP TRIGGER outbox_ready;
DROP TRIGGER outbox_immutable;
DROP TRIGGER outbox_no_delete;
DROP TRIGGER outbox_no_reset;
DROP INDEX outbox_pending;
ALTER TABLE outbox RENAME TO old_outbox;
CREATE TABLE outbox (
    sequence INTEGER PRIMARY KEY,
    job_id TEXT NOT NULL UNIQUE,
    execution_id TEXT NOT NULL REFERENCES executions(id),
    request_id TEXT NOT NULL REFERENCES requests(id),
    content TEXT NOT NULL CHECK(json_valid(content) AND length(content) <= 1048576),
    content_hash TEXT NOT NULL CHECK(length(content_hash) = 64),
    published INTEGER NOT NULL DEFAULT 0 CHECK(published IN (0, 1))
) STRICT;
CREATE INDEX outbox_pending ON outbox(sequence) WHERE published = 0;
CREATE TRIGGER outbox_ready BEFORE INSERT ON outbox
WHEN NOT EXISTS (SELECT 1 FROM executions WHERE id = NEW.execution_id
    AND request_id = NEW.request_id AND resolution_status = 'READY')
BEGIN SELECT RAISE(ABORT, 'outbox requires a ready execution'); END;
CREATE TRIGGER outbox_immutable BEFORE UPDATE OF sequence, job_id, execution_id, request_id, content, content_hash ON outbox
BEGIN SELECT RAISE(ABORT, 'outbox content is immutable'); END;
CREATE TRIGGER outbox_no_delete BEFORE DELETE ON outbox
BEGIN SELECT RAISE(ABORT, 'outbox content is immutable'); END;
CREATE TRIGGER outbox_no_reset BEFORE UPDATE OF published ON outbox WHEN OLD.published = 1 AND NEW.published != 1
BEGIN SELECT RAISE(ABORT, 'publication cannot be reset'); END;
INSERT INTO outbox SELECT * FROM old_outbox;
DROP TABLE old_outbox;
CREATE UNIQUE INDEX one_run_per_execution ON outbox(execution_id) WHERE json_extract(content, '$.job_kind') = 'run';
CREATE TABLE analysis_selections (
 request_id TEXT NOT NULL REFERENCES requests(id),
 id TEXT NOT NULL,
 content TEXT NOT NULL CHECK(json_valid(content) AND length(CAST(content AS BLOB)) <= 1048576),
 content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
 PRIMARY KEY(request_id,id)
) STRICT;
CREATE TRIGGER analysis_selection_immutable BEFORE UPDATE ON analysis_selections
BEGIN SELECT RAISE(ABORT, 'analysis selection is immutable'); END;
CREATE TRIGGER analysis_selection_retained BEFORE DELETE ON analysis_selections
BEGIN SELECT RAISE(ABORT, 'analysis selection is immutable'); END;
PRAGMA user_version = 5;
