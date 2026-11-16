ALTER TABLE rating_requests
    ADD COLUMN status TEXT,
    ADD COLUMN created_at TIMESTAMPTZ,
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD COLUMN answered_at TIMESTAMPTZ;

-- Existing requests have no reliable lifetime. Retain them as expired history;
-- never surface an old request as a newly active prompt. Legacy ratings stay intact.
UPDATE rating_requests
SET status = 'expired',
    created_at = CURRENT_TIMESTAMP - INTERVAL '1 second',
    expires_at = CURRENT_TIMESTAMP;

-- Where a legacy answer does exist, preserve that relationship as closed history.
UPDATE rating_requests rr
SET status = 'answered', answered_at = created_at + INTERVAL '0.5 seconds'
WHERE EXISTS (SELECT 1 FROM ratings r WHERE r.request_id = rr.id);

ALTER TABLE rating_requests
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'pending',
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
    ALTER COLUMN expires_at SET NOT NULL,
    ALTER COLUMN expires_at SET DEFAULT (CURRENT_TIMESTAMP + INTERVAL '30 seconds'),
    ADD CONSTRAINT rating_requests_status_check CHECK (status IN ('pending', 'displayed', 'answered', 'expired')),
    ADD CONSTRAINT rating_requests_expiry_check CHECK (expires_at > created_at),
    ADD CONSTRAINT rating_requests_answer_check CHECK (
        (status = 'answered' AND answered_at IS NOT NULL AND answered_at >= created_at AND answered_at < expires_at)
        OR (status <> 'answered' AND answered_at IS NULL)
    );

CREATE UNIQUE INDEX rating_requests_one_active_per_device
    ON rating_requests (device_id) WHERE status IN ('pending', 'displayed');

CREATE INDEX rating_requests_active_expiry
    ON rating_requests (expires_at) WHERE status IN ('pending', 'displayed');
