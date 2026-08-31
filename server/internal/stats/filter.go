package stats

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
	"github.com/chrisgreg/glance/server/internal/rollup"
)

// Filters narrows a view to the visitors who matched every entry: a
// dimension name to the key clicked in its card. An empty key is a real
// value (direct traffic, unknown country), so presence matters, not
// emptiness.
type Filters map[string]string

// ParseFilters reads one query parameter per dimension. "Other" is a fold
// the rollup makes, not a value, so it is never a filter.
func ParseFilters(q url.Values) Filters {
	f := Filters{}
	for _, dim := range Dims {
		if !q.Has(dim) {
			continue
		}
		v := q.Get(dim)
		if v == "Other" {
			continue
		}
		f[dim] = v
	}
	return f
}

// Keys lists the active dimensions in a stable order.
func (f Filters) Keys() []string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Query encodes the filters as URL parameters.
func (f Filters) Query() url.Values {
	q := url.Values{}
	for _, k := range f.Keys() {
		q.Set(k, f[k])
	}
	return q
}

// matching builds a CTE naming the (day, visitor) pairs that satisfy every
// filter within [from, to). Referrers only appear on a visitor's landing
// view, so filtering rows directly would hide the rest of their visit;
// filtering visitors keeps it.
func (f Filters) matching(siteID string, from, to time.Time) (string, []any) {
	var parts []string
	var args []any
	for _, dim := range f.Keys() {
		col := rollup.DimColumn[dim]
		kind := "pageview"
		if dim == "event" {
			kind = "event"
		}
		parts = append(parts, fmt.Sprintf(`SELECT DISTINCT substr(ts, 1, 10) AS d, visitor AS v FROM events
			WHERE site_id = ? AND ts >= ? AND ts < ? AND kind = ? AND %s = ?`, col))
		args = append(args, siteID, ids.Format(from), ids.Format(to), kind, f[dim])
	}
	return "WITH matched AS (" + strings.Join(parts, " INTERSECT ") + ")", args
}

// filteredEvents is the page-view rows of matching visitors.
const filteredEvents = ` FROM events e JOIN matched m ON substr(e.ts, 1, 10) = m.d AND e.visitor = m.v
	WHERE e.site_id = ? AND e.ts >= ? AND e.ts < ? AND e.kind = 'pageview'`

// FilteredSummary is Summary computed from raw events for the visitors
// matching f. It only reaches as far back as raw events are kept, so the
// caller clips from to the retention window and flags the truncation.
func (s *Store) FilteredSummary(ctx context.Context, siteID, rng string, now time.Time, top int, f Filters, retentionDays int) (Summary, error) {
	from, to, bucket := Window(rng, now)
	out := Summary{Range: rng, From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), Bucket: bucket, Breakdowns: map[string][]Row{}, Filters: f, RetentionDays: retentionDays}
	floor := now.UTC().AddDate(0, 0, -retentionDays).Truncate(24 * time.Hour)
	if floor.After(from) {
		out.Truncated = true
		from = floor
		out.From = from.Format(time.RFC3339)
	}
	var err error
	if out.Series, err = s.filteredSeries(ctx, siteID, from, to, bucket, f); err != nil {
		return out, err
	}
	if out.Totals, err = s.filteredTotals(ctx, siteID, from, to, f); err != nil {
		return out, err
	}
	// The previous window is only comparable when it is fully retained.
	span := to.Sub(from)
	if !from.Add(-span).Before(floor) {
		if out.Previous, err = s.filteredTotals(ctx, siteID, from.Add(-span), from, f); err != nil {
			return out, err
		}
	}
	for _, dim := range Dims {
		if out.Breakdowns[dim], err = s.filteredBreakdown(ctx, siteID, dim, from, to, top, f); err != nil {
			return out, err
		}
	}
	out.Markers = []Marker{}
	return out, nil
}

// FilteredBreakdown is Breakdown for the visitors matching f.
func (s *Store) FilteredBreakdown(ctx context.Context, siteID, dim, rng string, now time.Time, limit int, f Filters, retentionDays int) ([]Row, error) {
	from, to, _ := Window(rng, now)
	if floor := now.UTC().AddDate(0, 0, -retentionDays).Truncate(24 * time.Hour); floor.After(from) {
		from = floor
	}
	return s.filteredBreakdown(ctx, siteID, dim, from, to, limit, f)
}

func (s *Store) filteredTotals(ctx context.Context, siteID string, from, to time.Time, f Filters) (Totals, error) {
	cte, args := f.matching(siteID, from, to)
	args = append(args, siteID, ids.Format(from), ids.Format(to))
	var t Totals
	err := s.db.QueryRowContext(ctx, cte+` SELECT COUNT(*), COUNT(DISTINCT m.d || m.v)`+filteredEvents, args...).Scan(&t.Pageviews, &t.Visitors)
	return t, err
}

func (s *Store) filteredSeries(ctx context.Context, siteID string, from, to time.Time, bucket string, f Filters) ([]Point, error) {
	width := 10
	step, layout := 24*time.Hour, "2006-01-02"
	if bucket == "hour" {
		width, step, layout = 13, time.Hour, "2006-01-02T15"
	}
	cte, args := f.matching(siteID, from, to)
	args = append(args, siteID, ids.Format(from), ids.Format(to))
	rows, err := s.db.QueryContext(ctx, cte+fmt.Sprintf(` SELECT substr(e.ts, 1, %d), COUNT(*), COUNT(DISTINCT e.visitor)`, width)+filteredEvents+
		fmt.Sprintf(` GROUP BY substr(e.ts, 1, %d)`, width), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]Point{}
	for rows.Next() {
		var k string
		var p Point
		if err := rows.Scan(&k, &p.Pageviews, &p.Visitors); err != nil {
			return nil, err
		}
		got[k] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Point{}
	for t := from; t.Before(to); t = t.Add(step) {
		p := got[t.Format(layout)]
		p.T = t.Format(time.RFC3339)
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) filteredBreakdown(ctx context.Context, siteID, dim string, from, to time.Time, top int, f Filters) ([]Row, error) {
	col := rollup.DimColumn[dim]
	cte, args := f.matching(siteID, from, to)
	args = append(args, siteID, ids.Format(from), ids.Format(to))
	query := cte + ` SELECT e.` + col + `, COUNT(*), COUNT(DISTINCT m.d || m.v)` + filteredEvents
	if dim == "event" {
		// Custom events by the matching visitors, counted like the rollup does.
		query = cte + ` SELECT e.name, COUNT(*), COUNT(DISTINCT m.d || m.v) FROM events e JOIN matched m ON substr(e.ts, 1, 10) = m.d AND e.visitor = m.v
			WHERE e.site_id = ? AND e.ts >= ? AND e.ts < ? AND e.kind = 'event'`
	}
	if dim == "region" || dim == "utm_source" || dim == "utm_campaign" {
		query += ` AND e.` + col + ` != ''`
	}
	query += ` GROUP BY 1 ORDER BY 2 DESC, 1 ASC LIMIT ?`
	args = append(args, top)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Key, &r.Pageviews, &r.Visitors); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
