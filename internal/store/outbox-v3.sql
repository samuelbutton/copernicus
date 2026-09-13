CREATE TABLE outbox (
    sequence INTEGER PRIMARY KEY,
    job_id TEXT NOT NULL UNIQUE,
    execution_id TEXT NOT NULL UNIQUE REFERENCES executions(id),
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
CREATE TABLE exchange_destination (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    path TEXT NOT NULL CHECK(length(path) > 0)
) STRICT;
CREATE TRIGGER destination_immutable BEFORE UPDATE ON exchange_destination
BEGIN SELECT RAISE(ABORT, 'exchange destination is immutable'); END;
CREATE TRIGGER destination_no_delete BEFORE DELETE ON exchange_destination
BEGIN SELECT RAISE(ABORT, 'exchange destination is immutable'); END;
PRAGMA user_version = 3;
