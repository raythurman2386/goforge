ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_status_check;

UPDATE deployments SET status = 'cancelled' WHERE status = 'canceled';

ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('pending', 'cloning', 'building', 'deploying', 'running', 'failed', 'rolled_back', 'cancelled'));
