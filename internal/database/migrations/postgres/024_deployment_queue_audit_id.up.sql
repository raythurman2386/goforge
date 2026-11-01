-- The audit row that queued a deployment (an API v1 approval or a UI
-- deployment.trigger). The worker writes deployment.run.completed, .failed,
-- or .canceled with details.audit_id set to it when the deployment ends.
-- No foreign key: UI audit rows are written in the background and can land
-- after the queue row. Existing rows keep NULL (no link).
ALTER TABLE deployment_queue ADD COLUMN IF NOT EXISTS audit_id UUID;
