DROP INDEX IF EXISTS idx_deployment_logs_timestamp;
DROP INDEX IF EXISTS idx_deployment_logs_deployment_id;
DROP TABLE IF EXISTS deployment_logs;
DROP INDEX IF EXISTS idx_deployments_created_at;
DROP INDEX IF EXISTS idx_deployments_status;
DROP INDEX IF EXISTS idx_deployments_environment_id;
DROP TABLE IF EXISTS deployments;
