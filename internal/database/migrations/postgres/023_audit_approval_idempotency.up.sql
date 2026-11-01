-- One approval_ref runs an API v1 change on a target at most once. The
-- approved-change row carries details.idempotency_key (a hash of the action
-- target); a second insert with the same user, action, approval_ref, and key
-- fails, and the API answers with the first run instead.
CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_logs_api_v1_idempotency
    ON audit_logs (user_id, action, (details->>'approval_ref'), (details->>'idempotency_key'))
    WHERE (details->>'idempotency_key') IS NOT NULL;

-- Finds the .completed or .failed row of an approved change.
CREATE INDEX IF NOT EXISTS idx_audit_logs_outcome_audit_id
    ON audit_logs ((details->>'audit_id'))
    WHERE (details->>'audit_id') IS NOT NULL;
