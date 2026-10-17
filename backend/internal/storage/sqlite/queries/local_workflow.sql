-- name: InsertLocalWorkflow :exec
INSERT INTO local_workflow(id,session_id,request_id,version,phase,closed_at,record_json) VALUES(?,?,?,?,?,?,?);

-- name: GetLocalWorkflow :one
SELECT * FROM local_workflow WHERE id=?;

-- name: CurrentLocalWorkflow :one
SELECT * FROM local_workflow WHERE session_id=? ORDER BY closed_at IS NULL DESC, rowid DESC LIMIT 1;

-- name: LocalWorkflowByRequest :one
SELECT * FROM local_workflow WHERE session_id=? AND request_id=?;

-- name: ListOpenLocalWorkflows :many
SELECT * FROM local_workflow WHERE closed_at IS NULL ORDER BY rowid;

-- name: AdvanceLocalWorkflow :execrows
UPDATE local_workflow SET version=@next_version,phase=@phase,closed_at=@closed_at,record_json=@record_json WHERE id=@id AND version=@expected_version AND closed_at IS NULL;

-- name: InsertLocalWorkflowOperation :exec
INSERT INTO local_workflow_operation(id,workflow_id,cycle,kind,attempt,state,client_message_id,record_json) VALUES(?,?,?,?,?,?,?,?);

-- name: CompleteLocalWorkflowOperation :execrows
UPDATE local_workflow_operation SET state=@state,record_json=@record_json WHERE id=@id AND workflow_id=@workflow_id AND state IN ('reserved','running','uncertain');

-- name: ListLocalWorkflowOperations :many
SELECT * FROM local_workflow_operation WHERE workflow_id=? ORDER BY rowid;

-- name: InsertLocalWorkflowTest :exec
INSERT INTO local_workflow_test(id,operation_id,command_index,record_json) VALUES(?,?,?,?);

-- name: ListLocalWorkflowTests :many
SELECT t.* FROM local_workflow_test t JOIN local_workflow_operation o ON o.id=t.operation_id WHERE o.workflow_id=? ORDER BY o.rowid,t.command_index;

-- name: LocalWorkflowBlocksWorker :one
SELECT EXISTS(SELECT 1 FROM local_workflow WHERE session_id=? AND closed_at IS NULL AND (phase IN ('testing','reviewing','stopping') OR EXISTS (SELECT 1 FROM local_workflow_operation o WHERE o.workflow_id=local_workflow.id AND o.kind IN ('tests','review') AND o.state IN ('reserved','running','uncertain'))));

-- name: BindReviewWorkflowOperation :execrows
UPDATE review_run SET workflow_operation_id=@operation_id WHERE id=@id AND workflow_operation_id='';

-- name: ReviewByWorkflowOperation :one
SELECT id FROM review_run WHERE workflow_operation_id=?;

-- name: InsertLocalWorkflowDecision :exec
INSERT INTO local_workflow_decision(id,workflow_id,request_id,record_json) VALUES(?,?,?,?);

-- name: ListLocalWorkflowDecisions :many
SELECT record_json FROM local_workflow_decision WHERE workflow_id=? ORDER BY rowid;

-- name: LocalWorkflowDecisionByRequest :one
SELECT record_json FROM local_workflow_decision WHERE workflow_id=? AND request_id=?;
