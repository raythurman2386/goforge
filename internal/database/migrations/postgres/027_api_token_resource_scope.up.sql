-- API token resource scoping. A token may list project IDs and service
-- instance IDs. Both empty (the default, so every existing token) means
-- host-wide: any project or service the owner has, subject to the scopes.
-- Any non-empty list restricts /api/v1 and /mcp to the listed projects
-- (with their environments, deployments, and containers) and the listed
-- services. The scopes column still decides read, write, and protected.
-- Plain arrays like scopes: the IDs are checked against the owner at create
-- time and on every request, and a deleted project or service simply stops
-- matching anything.
ALTER TABLE api_tokens
    ADD COLUMN IF NOT EXISTS project_ids UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS service_ids UUID[] NOT NULL DEFAULT '{}';
