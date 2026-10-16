-- +goose Up
ALTER TABLE review_run ADD COLUMN round INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_run ADD COLUMN local_result_json TEXT NOT NULL DEFAULT '';
UPDATE review_run SET round = (
    SELECT count(*) FROM review_run earlier
    WHERE earlier.session_id = review_run.session_id AND earlier.target_kind = 'local'
      AND (earlier.created_at < review_run.created_at OR (earlier.created_at = review_run.created_at AND earlier.id <= review_run.id))
) WHERE target_kind = 'local';

-- +goose Down
ALTER TABLE review_run DROP COLUMN local_result_json;
ALTER TABLE review_run DROP COLUMN round;
