-- Account role. Existing rows stay users. Admin is granted by
-- GOFORGE_ADMIN_EMAIL, not by being the earliest account.

ALTER TABLE users
    ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'user'
    CHECK (role IN ('admin', 'user'));
