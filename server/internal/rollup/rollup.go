// Package rollup rebuilds hourly and daily statistics from raw events.
//
// Today and yesterday (UTC) are rebuilt on every run because raw events for
// those days are still arriving; older days are final. Visitor hashes are
// day-scoped, so a day's visitor count is exact and multi-day totals are the
// sum of daily uniques.
package rollup

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// Dimensions stored in daily_stats.
var Dims = []string{"page", "ref", "country", "region", "device", "browser", "os", "event", "utm_source", "utm_campaign"}

// DimColumn is the events column behind each dimension.
var DimColumn = map[string]string{
	"page": "path", "ref": "ref_host", "country": "country", "region": "region", "device": "device", "browser": "browser", "os": "os", "event": "name",
	"utm_source": "utm_source", "utm_campaign": "utm_campaign",
}

// MaxKeysPerDim caps distinct keys kept per dimension per day; the rest are
// folded into "Other" so one noisy site cannot fill the disk.
const MaxKeysPerDim = 500

// Run rebuilds every site's rollups for today and yesterday.
func Run(ctx context.Context, db *sql.DB, log *slog.Logger, now time.Time) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM sites`)
	if err != nil {
		return err
	}
	var siteIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		siteIDs = append(siteIDs, id)
	}
	rows.Close()
	today := now.UTC().Truncate(24 * time.Hour)
	for _, id := range siteIDs {
		for _, day := range []time.Time{today.AddDate(0, 0, -1), today} {
			if err := Day(ctx, db, id, day); err != nil {
				log.Error("rollup.failed", "site", id, "day", day.Format("2006-01-02"), "error", err.Error())
			}
		}
	}
	return nil
}

// Day rebuilds one site's rollups for the UTC day starting at day.
func Day(ctx context.Context, db *sql.DB, siteID string, day time.Time) error {
	day = day.UTC().Truncate(24 * time.Hour)
	dayKey := day.Format("2006-01-02")
	from, to := ids.Format(day), ids.Format(day.AddDate(0, 0, 1))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Hourly: pageviews and distinct visitors per hour.
	if _, err := tx.ExecContext(ctx, `DELETE FROM hourly_stats WHERE site_id = ? AND hour >= ? AND hour < ?`, siteID, dayKey+"T00", dayKey+"T24"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO hourly_stats (site_id, hour, pageviews, visitors)
		SELECT site_id, substr(ts, 1, 13), SUM(kind = 'pageview'), COUNT(DISTINCT visitor)
		FROM events WHERE site_id = ? AND ts >= ? AND ts < ? GROUP BY substr(ts, 1, 13)`, siteID, from, to); err != nil {
		return err
	}

	// Daily: totals plus each dimension.
	if _, err := tx.ExecContext(ctx, `DELETE FROM daily_stats WHERE site_id = ? AND day = ?`, siteID, dayKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
		SELECT site_id, ?, 'total', '', SUM(kind = 'pageview'), COUNT(DISTINCT visitor)
		FROM events WHERE site_id = ? AND ts >= ? AND ts < ? HAVING COUNT(*) > 0`, dayKey, siteID, from, to); err != nil {
		return err
	}
	for _, dim := range Dims {
		col := DimColumn[dim]
		kindFilter := `kind = 'pageview'`
		if dim == "event" {
			kindFilter = `kind = 'event'`
		}
		if dim == "region" || dim == "utm_source" || dim == "utm_campaign" {
			kindFilter += ` AND ` + col + ` != ''`
		}
		// Top keys by pageviews (or event count), capped.
		q := fmt.Sprintf(`INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
			SELECT site_id, ?, ?, %s, COUNT(*), COUNT(DISTINCT visitor)
			FROM events WHERE site_id = ? AND ts >= ? AND ts < ? AND %s
			GROUP BY %s ORDER BY COUNT(*) DESC LIMIT ?`, col, kindFilter, col)
		if _, err := tx.ExecContext(ctx, q, dayKey, dim, siteID, from, to, MaxKeysPerDim); err != nil {
			return err
		}
		// Fold the remainder into "Other".
		q = fmt.Sprintf(`INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
			SELECT ?, ?, ?, 'Other', COUNT(*), COUNT(DISTINCT visitor) FROM events
			WHERE site_id = ? AND ts >= ? AND ts < ? AND %s
			AND %s NOT IN (SELECT key FROM daily_stats WHERE site_id = ? AND day = ? AND dim = ?)
			HAVING COUNT(*) > 0
			ON CONFLICT(site_id, day, dim, key) DO UPDATE SET pageviews = pageviews + excluded.pageviews, visitors = visitors + excluded.visitors`, kindFilter, col)
		if _, err := tx.ExecContext(ctx, q, siteID, dayKey, dim, siteID, from, to, siteID, dayKey, dim); err != nil {
			return err
		}
	}
	return tx.Commit()
}
