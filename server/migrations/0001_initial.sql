CREATE TABLE sites (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    domain       TEXT NOT NULL UNIQUE,
    home_country TEXT NOT NULL DEFAULT '',   -- ISO 3166-1 alpha-2; arcs converge here
    favicon      BLOB,
    favicon_type TEXT NOT NULL DEFAULT '',
    favicon_at   TEXT NOT NULL DEFAULT '',
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- Raw events. Short retention; rollups are the source of truth for the UI.
CREATE TABLE events (
    id       INTEGER PRIMARY KEY,
    site_id  TEXT NOT NULL,
    ts       TEXT NOT NULL,
    kind     TEXT NOT NULL,              -- pageview | event
    name     TEXT NOT NULL DEFAULT '',   -- custom event name
    path     TEXT NOT NULL DEFAULT '',
    ref_host TEXT NOT NULL DEFAULT '',   -- '' = direct
    country  TEXT NOT NULL DEFAULT '',   -- ISO alpha-2, '' = unknown
    device   TEXT NOT NULL DEFAULT '',
    browser  TEXT NOT NULL DEFAULT '',
    os       TEXT NOT NULL DEFAULT '',
    visitor  TEXT NOT NULL               -- day-scoped hash; never an IP
);
CREATE INDEX events_site_ts ON events(site_id, ts);

CREATE TABLE hourly_stats (
    site_id   TEXT NOT NULL,
    hour      TEXT NOT NULL,             -- YYYY-MM-DDTHH (UTC)
    pageviews INTEGER NOT NULL DEFAULT 0,
    visitors  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, hour)
);

CREATE TABLE daily_stats (
    site_id   TEXT NOT NULL,
    day       TEXT NOT NULL,             -- YYYY-MM-DD (UTC)
    dim       TEXT NOT NULL,             -- total | page | ref | country | device | browser | os | event
    key       TEXT NOT NULL,
    pageviews INTEGER NOT NULL DEFAULT 0,
    visitors  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, day, dim, key)
);

-- Cached referrer favicons.
CREATE TABLE favicons (
    host       TEXT PRIMARY KEY,
    data       BLOB,
    type       TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL,
    failed     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash    TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
