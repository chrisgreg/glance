// Package stats answers dashboard queries from the rollup tables only.
package stats

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// Ranges the dashboard supports.
var Ranges = []string{"24h", "7d", "30d", "90d"}

// Point is one bucket of the time series.
type Point struct {
	T         string `json:"t"` // bucket start, RFC 3339 UTC
	Pageviews int    `json:"pageviews"`
	Visitors  int    `json:"visitors"`
}

// Row is one breakdown entry.
type Row struct {
	Key       string `json:"key"`
	Pageviews int    `json:"pageviews"`
	Visitors  int    `json:"visitors"`
}

// Totals for a window.
type Totals struct {
	Pageviews int `json:"pageviews"`
	Visitors  int `json:"visitors"`
}

// Marker is a notable referrer in one bucket, drawn as a favicon above the
// chart. Only available while the raw events are still retained.
type Marker struct {
	T        string `json:"t"`
	Ref      string `json:"ref"`
	Visitors int    `json:"visitors"`
}

// Summary is the dashboard payload for one site and range.
type Summary struct {
	Range      string           `json:"range"`
	From       string           `json:"from"`
	To         string           `json:"to"`
	Bucket     string           `json:"bucket"` // hour | day
	Totals     Totals           `json:"totals"`
	Previous   Totals           `json:"previous"` // the equal window before From
	Series     []Point          `json:"series"`
	Markers    []Marker         `json:"markers"`
	Breakdowns map[string][]Row `json:"breakdowns"`
	// Set on filtered views, which come from raw events rather than rollups.
	Filters   Filters `json:"filters,omitempty"`
	Truncated bool    `json:"truncated,omitempty"` // range reached past retained raw events
	// PreviousUnavailable means the comparison window fell outside retained
	// raw events, so Previous is not a real zero.
	PreviousUnavailable bool `json:"previous_unavailable,omitempty"`
	RetentionDays       int  `json:"retention_days,omitempty"`
}

// Window returns [from, to) for a range ending now, plus the bucket size.
func Window(rng string, now time.Time) (from, to time.Time, bucket string) {
	now = now.UTC()
	switch rng {
	case "24h":
		to = now.Truncate(time.Hour).Add(time.Hour)
		return to.Add(-24 * time.Hour), to, "hour"
	case "7d":
		to = now.Truncate(24*time.Hour).AddDate(0, 0, 1)
		return to.AddDate(0, 0, -7), to, "hour"
	case "90d":
		to = now.Truncate(24*time.Hour).AddDate(0, 0, 1)
		return to.AddDate(0, 0, -90), to, "day"
	default:
		to = now.Truncate(24*time.Hour).AddDate(0, 0, 1)
		return to.AddDate(0, 0, -30), to, "day"
	}
}

// ValidRange reports whether r is supported.
func ValidRange(r string) bool {
	for _, x := range Ranges {
		if x == r {
			return true
		}
	}
	return false
}

// Store queries rollups.
type Store struct{ db *sql.DB }

// New returns a Store.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Summary builds the dashboard payload.
func (s *Store) Summary(ctx context.Context, siteID, rng string, now time.Time, top int) (Summary, error) {
	from, to, bucket := Window(rng, now)
	out := Summary{Range: rng, From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), Bucket: bucket, Breakdowns: map[string][]Row{}}

	series, err := s.series(ctx, siteID, from, to, bucket)
	if err != nil {
		return out, err
	}
	out.Series = series
	for _, p := range series {
		out.Totals.Pageviews += p.Pageviews
		out.Totals.Visitors += p.Visitors
	}
	span := to.Sub(from)
	prev, err := s.series(ctx, siteID, from.Add(-span), from, bucket)
	if err != nil {
		return out, err
	}
	for _, p := range prev {
		out.Previous.Pageviews += p.Pageviews
		out.Previous.Visitors += p.Visitors
	}
	for _, dim := range Dims {
		rows, err := s.breakdown(ctx, siteID, dim, from, to, top)
		if err != nil {
			return out, err
		}
		out.Breakdowns[dim] = rows
	}
	out.Markers, err = s.markers(ctx, siteID, from, to, bucket)
	if err != nil {
		return out, err
	}
	return out, nil
}

// markers finds, per bucket, the referrer that brought the most visitors,
// keeping only those worth pointing at: at least two visitors, or the whole
// bucket when it is small. Reads raw events, so it only covers the retained
// window; buckets older than that simply have no marker. Capped at eight.
func (s *Store) markers(ctx context.Context, siteID string, from, to time.Time, bucket string) ([]Marker, error) {
	keyLen := 13
	layout := "2006-01-02T15"
	if bucket == "day" {
		keyLen, layout = 10, "2006-01-02"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT substr(ts, 1, ?), ref_host, COUNT(DISTINCT visitor) AS v FROM events
		WHERE site_id = ? AND ts >= ? AND ts < ? AND kind = 'pageview' AND ref_host != ''
		GROUP BY substr(ts, 1, ?), ref_host ORDER BY v DESC`, keyLen, siteID, from.Format("2006-01-02T15:04:05"), to.Format("2006-01-02T15:04:05"), keyLen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var all []Marker
	for rows.Next() {
		var k, ref string
		var v int
		if err := rows.Scan(&k, &ref, &v); err != nil {
			return nil, err
		}
		if seen[k] || v < 2 {
			continue
		}
		t, err := time.Parse(layout, k)
		if err != nil {
			continue
		}
		seen[k] = true
		all = append(all, Marker{T: t.Format(time.RFC3339), Ref: ref, Visitors: v})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Rows arrived strongest first. Drop a marker when a stronger one for the
	// same referrer sits in the neighbouring bucket, so icons never touch.
	stepD := time.Hour
	if bucket == "day" {
		stepD = 24 * time.Hour
	}
	out := []Marker{}
	for _, m := range all {
		mt, _ := time.Parse(time.RFC3339, m.T)
		adjacent := false
		for _, k := range out {
			kt, _ := time.Parse(time.RFC3339, k.T)
			if k.Ref == m.Ref && kt.Sub(mt).Abs() <= stepD {
				adjacent = true
				break
			}
		}
		if adjacent {
			continue
		}
		out = append(out, m)
		if len(out) >= 8 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

// series returns one point per bucket in [from, to), zero-filled.
func (s *Store) series(ctx context.Context, siteID string, from, to time.Time, bucket string) ([]Point, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if bucket == "hour" {
		rows, err = s.db.QueryContext(ctx, `SELECT hour, pageviews, visitors FROM hourly_stats WHERE site_id = ? AND hour >= ? AND hour < ?`,
			siteID, from.Format("2006-01-02T15"), to.Format("2006-01-02T15"))
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT day, pageviews, visitors FROM daily_stats WHERE site_id = ? AND dim = 'total' AND day >= ? AND day < ?`,
			siteID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
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
	var out []Point
	step, layout := 24*time.Hour, "2006-01-02"
	if bucket == "hour" {
		step, layout = time.Hour, "2006-01-02T15"
	}
	for t := from; t.Before(to); t = t.Add(step) {
		p := got[t.Format(layout)]
		p.T = t.Format(time.RFC3339)
		out = append(out, p)
	}
	return out, nil
}

// breakdown sums a dimension over whole days covering [from, to).
func (s *Store) breakdown(ctx context.Context, siteID, dim string, from, to time.Time, top int) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, SUM(pageviews), SUM(visitors) FROM daily_stats
		WHERE site_id = ? AND dim = ? AND day >= ? AND day <= ? GROUP BY key ORDER BY SUM(pageviews) DESC, key ASC LIMIT ?`,
		siteID, dim, from.Format("2006-01-02"), to.Add(-time.Second).Format("2006-01-02"), top)
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

// Breakdown returns every key for one dimension over a range, best first.
func (s *Store) Breakdown(ctx context.Context, siteID, dim, rng string, now time.Time, limit int) ([]Row, error) {
	from, to, _ := Window(rng, now)
	return s.breakdown(ctx, siteID, dim, from, to, limit)
}

// Dims are the breakdown dimensions the dashboard can ask for.
var Dims = []string{"page", "ref", "country", "region", "device", "browser", "os", "event", "utm_source", "utm_campaign"}

// ValidDim reports whether dim is a stored breakdown dimension.
func ValidDim(dim string) bool {
	for _, d := range Dims {
		if d == dim {
			return true
		}
	}
	return false
}

// SiteCard is the per-site summary on the index page.
type SiteCard struct {
	Visitors  int     `json:"visitors"`  // last 7 days
	Previous  int     `json:"previous"`  // the 7 days before
	Pageviews int     `json:"pageviews"` // last 7 days
	Spark     []Point `json:"spark"`     // daily, last 14 days
}

// Card returns the index-page numbers for a site.
func (s *Store) Card(ctx context.Context, siteID string, now time.Time) (SiteCard, error) {
	to := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	pts, err := s.series(ctx, siteID, to.AddDate(0, 0, -14), to, "day")
	if err != nil {
		return SiteCard{}, err
	}
	var c SiteCard
	c.Spark = pts
	for i, p := range pts {
		if i < 7 {
			c.Previous += p.Visitors
		} else {
			c.Visitors += p.Visitors
			c.Pageviews += p.Pageviews
		}
	}
	return c, nil
}

// Live is the realtime picture: distinct visitors in the last five minutes,
// by country, plus the most recent hits so the UI can animate arrivals.
type Live struct {
	Total     int       `json:"total"`
	Countries []Row     `json:"countries"`
	Recent    []LiveHit `json:"recent"`
	// Minutes holds distinct visitors per minute for the last 30 minutes,
	// oldest first, and Total30 the distinct visitors across them.
	Minutes []int `json:"minutes"`
	Total30 int   `json:"total_30m"`
}

// LiveHit is one recent page view.
type LiveHit struct {
	At      string `json:"at"`
	Country string `json:"country"`
	Path    string `json:"path"`
}

// LiveSnapshot reads the last five minutes of raw events for a site.
func (s *Store) LiveSnapshot(ctx context.Context, siteID string, now time.Time) (Live, error) {
	since := now.UTC().Add(-5 * time.Minute).Format("2006-01-02T15:04:05")
	out := Live{Countries: []Row{}, Recent: []LiveHit{}}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT visitor) FROM events WHERE site_id = ? AND ts >= ?`, siteID, since).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT country, COUNT(DISTINCT visitor), COUNT(*) FROM events WHERE site_id = ? AND ts >= ? GROUP BY country ORDER BY COUNT(DISTINCT visitor) DESC`, siteID, since)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Key, &r.Visitors, &r.Pageviews); err != nil {
			rows.Close()
			return out, err
		}
		out.Countries = append(out.Countries, r)
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT ts, country, path FROM events WHERE site_id = ? AND ts >= ? AND kind = 'pageview' ORDER BY ts DESC LIMIT 20`, siteID, since)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var h LiveHit
		if err := rows.Scan(&h.At, &h.Country, &h.Path); err != nil {
			rows.Close()
			return out, err
		}
		out.Recent = append(out.Recent, h)
	}
	rows.Close()

	// Per-minute visitors for the last 30 minutes.
	start := now.UTC().Truncate(time.Minute).Add(-29 * time.Minute)
	out.Minutes = make([]int, 30)
	rows, err = s.db.QueryContext(ctx, `SELECT substr(ts, 1, 16), COUNT(DISTINCT visitor) FROM events WHERE site_id = ? AND ts >= ? GROUP BY substr(ts, 1, 16)`, siteID, start.Format("2006-01-02T15:04:05"))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return out, err
		}
		if t, err := time.Parse("2006-01-02T15:04", k); err == nil {
			if i := int(t.Sub(start) / time.Minute); i >= 0 && i < 30 {
				out.Minutes[i] = n
			}
		}
	}
	rows.Close()
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT visitor) FROM events WHERE site_id = ? AND ts >= ?`, siteID, start.Format("2006-01-02T15:04:05")).Scan(&out.Total30); err != nil {
		return out, err
	}
	return out, nil
}

// LiveVisitors counts distinct visitors seen in the last five minutes from raw events.
func (s *Store) LiveVisitors(ctx context.Context, siteID string, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT visitor) FROM events WHERE site_id = ? AND ts >= ?`, siteID, now.UTC().Add(-5*time.Minute).Format("2006-01-02T15:04:05")).Scan(&n)
	return n, err
}
