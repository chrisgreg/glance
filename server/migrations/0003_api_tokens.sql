CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,             -- first characters, for display
    created_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL DEFAULT ''
);
