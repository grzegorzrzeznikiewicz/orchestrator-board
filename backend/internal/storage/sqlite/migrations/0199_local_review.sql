-- Local review owns a content identity rather than a provider URL/head SHA.
-- Existing rows keep the PR target and all original result/history columns.
-- +goose Up
ALTER TABLE review_run ADD COLUMN target_kind TEXT NOT NULL DEFAULT 'pr' CHECK (target_kind IN ('pr', 'local'));
ALTER TABLE review_run ADD COLUMN snapshot_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN snapshot_path TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN snapshot_observation TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN snapshot_epoch TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN invalidated_at TIMESTAMP;
ALTER TABLE review_run ADD COLUMN invalidation_reason TEXT NOT NULL DEFAULT '';

-- +goose StatementBegin
CREATE UNIQUE INDEX idx_review_run_local_running
ON review_run (session_id, harness)
WHERE target_kind = 'local' AND status = 'running' AND invalidated_at IS NULL;
-- +goose StatementEnd

-- Invalidation must notify existing review subscribers even if the historical
-- status/verdict is unchanged. The existing result CDC trigger remains intact.
-- +goose StatementBegin
CREATE TRIGGER local_review_invalidation_cdc
AFTER UPDATE OF invalidated_at ON review_run
WHEN OLD.invalidated_at IS NULL AND NEW.invalidated_at IS NOT NULL
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES ((SELECT project_id FROM sessions WHERE id = NEW.session_id), NEW.session_id,
            'review_run_updated', json_object('id', NEW.id, 'sessionId', NEW.session_id,
            'targetKind', NEW.target_kind, 'snapshotId', NEW.snapshot_id,
            'invalidationReason', NEW.invalidation_reason), datetime('now'));
END;
-- +goose StatementEnd

-- AO records worker activity before dispatch, so an active/idle cycle cannot revive a result.
-- +goose StatementBegin
CREATE TRIGGER local_review_worker_activity
AFTER UPDATE OF activity_state ON sessions
WHEN NEW.activity_state != 'idle'
BEGIN
 UPDATE review_run SET invalidated_at=CURRENT_TIMESTAMP,
 invalidation_reason='Executor resumed editing'
 WHERE session_id=NEW.id AND target_kind='local' AND invalidated_at IS NULL;
END;
-- +goose StatementEnd

-- Native chat messages are durable before provider dispatch, including queued turns.
-- Reviewer turns must never invalidate their own worker snapshot.
-- +goose StatementBegin
CREATE TRIGGER local_review_worker_turn
AFTER INSERT ON conversation_turns
WHEN NEW.handled_by_review_id IS NULL AND NEW.state IN ('queued','running')
BEGIN
 UPDATE review_run SET invalidated_at=CURRENT_TIMESTAMP,
 invalidation_reason='Executor received new work'
 WHERE session_id=NEW.handled_by_session_id AND target_kind='local' AND invalidated_at IS NULL;
 UPDATE sessions SET activity_state='active',activity_last_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
 WHERE id=NEW.handled_by_session_id AND EXISTS
 (SELECT 1 FROM projects WHERE projects.id=sessions.project_id AND json_extract(config,'$.reviewMode')='local');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER local_review_worker_turn;
DROP TRIGGER local_review_worker_activity;
DROP TRIGGER local_review_invalidation_cdc;
DROP INDEX idx_review_run_local_running;
-- Rolling back a local feature must not reinterpret local history as PR reviews.
DELETE FROM review_run WHERE target_kind = 'local';
ALTER TABLE review_run DROP COLUMN invalidation_reason;
ALTER TABLE review_run DROP COLUMN invalidated_at;
ALTER TABLE review_run DROP COLUMN snapshot_epoch;
ALTER TABLE review_run DROP COLUMN snapshot_observation;
ALTER TABLE review_run DROP COLUMN snapshot_path;
ALTER TABLE review_run DROP COLUMN snapshot_id;
ALTER TABLE review_run DROP COLUMN target_kind;
