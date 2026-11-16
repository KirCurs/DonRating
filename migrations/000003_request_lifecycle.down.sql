DROP INDEX IF EXISTS rating_requests_active_expiry;
DROP INDEX IF EXISTS rating_requests_one_active_per_device;

ALTER TABLE rating_requests
    DROP CONSTRAINT IF EXISTS rating_requests_answer_check,
    DROP CONSTRAINT IF EXISTS rating_requests_expiry_check,
    DROP CONSTRAINT IF EXISTS rating_requests_status_check,
    DROP COLUMN answered_at,
    DROP COLUMN expires_at,
    DROP COLUMN created_at,
    DROP COLUMN status;
