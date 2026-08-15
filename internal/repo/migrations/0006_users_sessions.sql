-- User accounts and server-side sessions.
-- Registration is closed: accounts are created via cmd/user (CLI), not
-- through the web UI.
--
-- Sessions store only the SHA-256 hash of a random 32-byte token; the
-- cookie value itself is never persisted, so a database leak does not
-- expose live session tokens. The CSRF token is per-session and checked
-- on every state-changing request.

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Emails are normalized to lowercase before insert; the expression
-- index is the database-level backstop for that rule.
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_email ON users (lower(email));

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);
