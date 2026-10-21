-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

-- +goose Up
CREATE TABLE local_workflow_intent (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 request_id TEXT NOT NULL CHECK(length(request_id) BETWEEN 1 AND 256 AND instr(request_id,char(10))=0 AND instr(request_id,char(13))=0),
 prompt TEXT NOT NULL CHECK(length(trim(prompt)) > 0),
 turn_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','consumed')),
 workflow_id TEXT NOT NULL DEFAULT '',
 created_at DATETIME NOT NULL,
 consumed_at DATETIME,
 PRIMARY KEY(session_id,request_id),
 CHECK((state='pending' AND consumed_at IS NULL) OR (state='consumed' AND consumed_at IS NOT NULL))
);
CREATE INDEX local_workflow_intent_pending ON local_workflow_intent(state,created_at);

-- +goose StatementBegin
CREATE TRIGGER local_workflow_intent_insert_cdc AFTER INSERT ON local_workflow_intent
BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
 VALUES((SELECT project_id FROM sessions WHERE id=NEW.session_id),NEW.session_id,'session_updated',json_object('sessionId',NEW.session_id),datetime('now'));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER local_workflow_intent_update_cdc AFTER UPDATE ON local_workflow_intent
BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
 VALUES((SELECT project_id FROM sessions WHERE id=NEW.session_id),NEW.session_id,'session_updated',json_object('sessionId',NEW.session_id),datetime('now'));
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER local_workflow_intent_update_cdc;
DROP TRIGGER local_workflow_intent_insert_cdc;
DROP TABLE local_workflow_intent;
