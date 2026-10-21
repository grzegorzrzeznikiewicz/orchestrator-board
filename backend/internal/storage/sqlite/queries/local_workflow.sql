-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

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

-- name: ListSessionLocalWorkflows :many
SELECT * FROM local_workflow WHERE session_id=? ORDER BY rowid DESC;

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

-- name: SelectLocalWorkflowCardFacts :many
WITH requested AS (
    SELECT CAST(value AS TEXT) AS id, CAST(key AS INTEGER) AS position
    FROM json_each(CAST(sqlc.arg(session_ids) AS TEXT))
)
SELECT s.id AS session_id,
    CAST(CASE WHEN w.id IS NULL THEN '' ELSE json_object(
        'id', w.id, 'sessionId', s.id, 'version', w.version, 'phase', w.phase,
        'cycle', json_extract(w.record_json, '$.cycle'),
        'maxRounds', json_extract(w.record_json, '$.maxRounds'),
        'automatic', json(CASE WHEN json_extract(w.record_json, '$.automatic') THEN 'true' ELSE 'false' END),
        'planState', COALESCE(json_extract(w.record_json, '$.planState'), ''),
        'providerRetry', json(COALESCE(json_extract(w.record_json, '$.providerRetry'), 'null')),
        'stopRequested', json(CASE WHEN json_extract(w.record_json, '$.stopRequested') THEN 'true' ELSE 'false' END),
        'blockCode', COALESCE(json_extract(w.record_json, '$.blockCode'), ''),
        'blockMessage', substr(COALESCE(json_extract(w.record_json, '$.blockMessage'), ''), 1, 4096),
        'verificationPlan', json_object('profile', COALESCE(json_extract(w.record_json, '$.verificationPlan.profile'), ''))
    ) END AS TEXT) AS run_json,
    CAST(COALESCE(latest.kind, '') AS TEXT) AS latest_operation_kind,
    CAST(COALESCE(test_op.id, '') AS TEXT) AS test_operation_id,
    CAST((SELECT COUNT(*) FROM local_workflow_test t WHERE t.operation_id=test_op.id
        AND json_extract(t.record_json, '$.status')='succeeded'
        AND json_extract(t.record_json, '$.exitCode')=0) AS INTEGER) AS tests_passed,
    CAST(COALESCE(json_array_length(json_extract(w.record_json, '$.testCommands')), 0) AS INTEGER) AS tests_total,
    CAST(EXISTS(SELECT 1 FROM local_workflow_intent i WHERE i.session_id=s.id AND i.state='pending') AS INTEGER) AS pending_intent,
    CAST(NOT s.is_terminated AND s.activity_state='idle' AS INTEGER) AS executor_idle,
    CAST(COALESCE(review.id, '') AS TEXT) AS review_run_id,
    CAST(review.id IS NOT NULL AND review.invalidated_at IS NULL AND review.status='complete' AND review.verdict='approved' AS INTEGER) AS review_valid,
    CAST((SELECT COUNT(*) FROM json_each(CASE WHEN json_valid(review.local_result_json) THEN review.local_result_json ELSE '{}' END, '$.findings') f
        WHERE json_extract(f.value, '$.kind')='required') AS INTEGER) AS required_count,
    CAST(COALESCE((SELECT substr(json_extract(f.value, '$.reason'), 1, 1024)
        FROM json_each(CASE WHEN json_valid(review.local_result_json) THEN review.local_result_json ELSE '{}' END, '$.findings') f
        WHERE json_extract(f.value, '$.kind')='required' ORDER BY CAST(f.key AS INTEGER) LIMIT 1), '') AS TEXT) AS required_reason
FROM requested req
JOIN sessions s ON s.id=req.id
LEFT JOIN local_workflow w ON w.rowid=(SELECT rowid FROM local_workflow
    WHERE session_id=s.id ORDER BY closed_at IS NULL DESC, rowid DESC LIMIT 1)
LEFT JOIN local_workflow_operation latest ON latest.rowid=(SELECT rowid FROM local_workflow_operation
    WHERE workflow_id=w.id ORDER BY rowid DESC LIMIT 1)
LEFT JOIN local_workflow_operation test_op ON test_op.rowid=(SELECT rowid FROM local_workflow_operation
    WHERE workflow_id=w.id AND kind='tests' ORDER BY rowid DESC LIMIT 1)
LEFT JOIN local_workflow_operation review_op ON review_op.rowid=(SELECT rowid FROM local_workflow_operation
    WHERE workflow_id=w.id AND kind='review' ORDER BY rowid DESC LIMIT 1)
LEFT JOIN review_run review ON review.id=json_extract(review_op.record_json, '$.reviewRunId') AND review.session_id=s.id
ORDER BY req.position;
