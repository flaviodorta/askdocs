CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Opaque session tokens live in an httpOnly cookie (recorded decision); only
-- the SHA-256 of the token is stored, so a leaked table can't impersonate.
CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions (user_id);

-- Pre-auth rows have no owner. This wipes dev-era data on purpose (documented
-- trade-off): the alternative — a nullable user_id — would poison every
-- ownership check from here on.
DELETE FROM conversations;
DELETE FROM documents;

ALTER TABLE documents
    ADD COLUMN user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE conversations
    ADD COLUMN user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE;

CREATE INDEX documents_user_idx ON documents (user_id);
CREATE INDEX conversations_user_idx ON conversations (user_id);
