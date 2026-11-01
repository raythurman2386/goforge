-- A service can be marked protected on its service page. /api/v1 and the MCP
-- tools treat a protected service like one listed in
-- GOFORGE_PROTECTED_SERVICES: stop, config edit, and image update need a
-- token with the protected scope. The two sources are merged; the env list
-- cannot be turned off from the UI.
ALTER TABLE service_instances
    ADD COLUMN IF NOT EXISTS protected BOOLEAN NOT NULL DEFAULT FALSE;
