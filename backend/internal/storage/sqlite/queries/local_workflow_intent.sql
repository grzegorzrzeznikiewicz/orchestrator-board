-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

-- name: InsertLocalWorkflowIntent :execrows
INSERT INTO local_workflow_intent(session_id,request_id,prompt,turn_id,created_at)
VALUES(?,?,?,?,?) ON CONFLICT(session_id,request_id) DO NOTHING;

-- name: GetLocalWorkflowIntent :one
SELECT * FROM local_workflow_intent WHERE session_id=? AND request_id=?;

-- name: ListPendingLocalWorkflowIntents :many
SELECT * FROM local_workflow_intent WHERE state='pending' ORDER BY rowid;

-- name: ConsumeLocalWorkflowIntent :execrows
UPDATE local_workflow_intent SET state='consumed',workflow_id=@workflow_id,consumed_at=@consumed_at
WHERE session_id=@session_id AND request_id=@request_id AND state='pending';

-- name: PendingUnboundLocalWorkflowIntent :one
SELECT * FROM local_workflow_intent WHERE session_id=? AND state='pending' AND turn_id=''
AND request_id LIKE 'spawn:%' ORDER BY rowid DESC LIMIT 1;
