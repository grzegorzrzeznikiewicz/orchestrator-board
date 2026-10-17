-- +goose Up
CREATE TABLE local_workflow (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 request_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version > 0),
 phase TEXT NOT NULL CHECK(phase IN ('waiting_executor','testing','reviewing','fixing','ready','blocked','stopping','stopped')),
 closed_at DATETIME,
 record_json TEXT NOT NULL CHECK(json_valid(record_json)),
 UNIQUE(session_id, request_id)
);
CREATE UNIQUE INDEX local_workflow_open ON local_workflow(session_id) WHERE closed_at IS NULL;
CREATE TABLE local_workflow_operation (
 id TEXT PRIMARY KEY,
 workflow_id TEXT NOT NULL REFERENCES local_workflow(id) ON DELETE CASCADE,
 cycle INTEGER NOT NULL CHECK(cycle > 0),
 kind TEXT NOT NULL CHECK(kind IN ('tests','review','fix')),
 attempt INTEGER NOT NULL CHECK(attempt > 0),
 state TEXT NOT NULL CHECK(state IN ('reserved','running','succeeded','failed','interrupted','uncertain')),
 client_message_id TEXT NOT NULL DEFAULT '',
 record_json TEXT NOT NULL CHECK(json_valid(record_json)),
 UNIQUE(workflow_id, cycle, kind, attempt)
);
CREATE UNIQUE INDEX local_workflow_delivery ON local_workflow_operation(client_message_id) WHERE client_message_id != '';
CREATE TABLE local_workflow_test (
 id TEXT PRIMARY KEY,
 operation_id TEXT NOT NULL REFERENCES local_workflow_operation(id) ON DELETE CASCADE,
 command_index INTEGER NOT NULL,
 record_json TEXT NOT NULL CHECK(json_valid(record_json)),
 UNIQUE(operation_id, command_index)
);
ALTER TABLE review_run ADD COLUMN workflow_operation_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX review_workflow_operation ON review_run(workflow_operation_id) WHERE workflow_operation_id != '';

-- +goose StatementBegin
CREATE TRIGGER local_workflow_insert_cdc AFTER INSERT ON local_workflow
BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
 VALUES((SELECT project_id FROM sessions WHERE id=NEW.session_id),NEW.session_id,'session_updated',json_object('workflowId',NEW.id,'sessionId',NEW.session_id),datetime('now'));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER local_workflow_update_cdc AFTER UPDATE ON local_workflow
BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
 VALUES((SELECT project_id FROM sessions WHERE id=NEW.session_id),NEW.session_id,'session_updated',json_object('workflowId',NEW.id,'sessionId',NEW.session_id),datetime('now'));
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER local_workflow_update_cdc;
DROP TRIGGER local_workflow_insert_cdc;
DROP INDEX review_workflow_operation;
ALTER TABLE review_run DROP COLUMN workflow_operation_id;
DROP TABLE local_workflow_test;
DROP TABLE local_workflow_operation;
DROP TABLE local_workflow;
