CREATE TABLE google_connections (
    site_id       TEXT PRIMARY KEY,
    property      TEXT NOT NULL DEFAULT '',   -- Search Console siteUrl, e.g. sc-domain:example.com
    email         TEXT NOT NULL DEFAULT '',
    refresh_token TEXT NOT NULL,
    connected_at  TEXT NOT NULL,
    synced_at     TEXT NOT NULL DEFAULT '',
    sync_error    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE search_terms (
    site_id     TEXT NOT NULL,
    day         TEXT NOT NULL,               -- YYYY-MM-DD
    query       TEXT NOT NULL,
    clicks      INTEGER NOT NULL,
    impressions INTEGER NOT NULL,
    position    REAL NOT NULL,
    PRIMARY KEY (site_id, day, query)
);
