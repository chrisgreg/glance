CREATE TABLE polar_connections (
    site_id        TEXT PRIMARY KEY,
    access_token   TEXT NOT NULL,
    server         TEXT NOT NULL DEFAULT 'https://api.polar.sh',
    product_ids    TEXT NOT NULL DEFAULT '',   -- comma separated; empty = every product
    webhook_secret TEXT NOT NULL DEFAULT '',
    connected_at   TEXT NOT NULL,
    synced_at      TEXT NOT NULL DEFAULT '',
    sync_error     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE polar_orders (
    site_id         TEXT NOT NULL,
    order_id        TEXT NOT NULL,
    created_at      TEXT NOT NULL,             -- RFC 3339 UTC
    status          TEXT NOT NULL,
    paid            INTEGER NOT NULL,
    net_amount      INTEGER NOT NULL,          -- cents, after discounts, before tax
    refunded_amount INTEGER NOT NULL,          -- cents
    currency        TEXT NOT NULL,
    country         TEXT NOT NULL DEFAULT '',
    product         TEXT NOT NULL DEFAULT '',
    ref             TEXT NOT NULL DEFAULT '',  -- first-touch referrer host
    source          TEXT NOT NULL DEFAULT '',  -- first-touch utm_source / ?ref=
    campaign        TEXT NOT NULL DEFAULT '',
    landing         TEXT NOT NULL DEFAULT '',  -- first-touch landing path
    PRIMARY KEY (site_id, order_id)
);

CREATE INDEX polar_orders_site_created ON polar_orders (site_id, created_at);
