CREATE TABLE documents (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    filename     text NOT NULL,
    content_type text NOT NULL,
    status       text NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued', 'processing', 'ready', 'failed')),
    error        text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- The ingestion queue is this table: workers claim rows by status.
CREATE INDEX documents_status_idx ON documents (status, created_at);

-- The embedding vector(N) column is added by a later migration, once the
-- embedding model (and therefore the dimension) is chosen in Phase 3.
CREATE TABLE chunks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    idx         integer NOT NULL,
    text        text NOT NULL,
    UNIQUE (document_id, idx)
);
