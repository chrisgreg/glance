ALTER TABLE events ADD COLUMN region TEXT NOT NULL DEFAULT '';        -- time-zone city, e.g. "London"
ALTER TABLE events ADD COLUMN utm_source TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN utm_campaign TEXT NOT NULL DEFAULT '';
