-- +goose Up
CREATE TABLE local_workflow_decision (
 id TEXT PRIMARY KEY,
 workflow_id TEXT NOT NULL REFERENCES local_workflow(id) ON DELETE CASCADE,
 request_id TEXT NOT NULL,
 record_json TEXT NOT NULL CHECK(json_valid(record_json)),
 UNIQUE(workflow_id, request_id)
);

-- +goose Down
DROP TABLE local_workflow_decision;
