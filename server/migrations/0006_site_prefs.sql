-- Per-site preferences: an accent that overrides the account-wide colour
-- while the site's dashboard is open, and the range its dashboard opens on.
-- Empty means "follow the global setting" / "7d".
ALTER TABLE sites ADD COLUMN accent TEXT NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN default_range TEXT NOT NULL DEFAULT '';
