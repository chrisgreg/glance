package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chrisgreg/glance/server/internal/rollup"
	"github.com/chrisgreg/glance/server/internal/stats"
)

func TestFilteredStats(t *testing.T) {
	s := newServer(t, "", "")
	fixed := time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC)
	s.Now = func() time.Time { return fixed }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"domain": "example.com"}, nil)
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	hit := func(ua, ip, url, ref, tz, name string) {
		body := map[string]any{"s": site.ID, "n": name, "u": url, "r": ref, "w": 1440, "tz": tz}
		if rr := do(t, h, "POST", "/api/v1/collect", body, map[string]string{"User-Agent": ua, "X-Forwarded-For": ip}); rr.Code != 202 {
			t.Fatalf("collect: %d", rr.Code)
		}
	}
	// A: lands from Google, then browses two more pages (same-site referrer becomes direct), fires signup.
	hit(chromeMac, "1.1.1.1", "https://example.com/", "https://www.google.com/", "Europe/London", "pageview")
	hit(chromeMac, "1.1.1.1", "https://example.com/pricing", "https://example.com/", "Europe/London", "pageview")
	hit(chromeMac, "1.1.1.1", "https://example.com/docs", "", "Europe/London", "pageview")
	hit(chromeMac, "1.1.1.1", "https://example.com/pricing", "", "Europe/London", "signup")
	// B: direct, on a phone from the US, one page.
	hit(safariPhone, "2.2.2.2", "https://example.com/blog", "", "America/New_York", "pageview")
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := rollup.Run(context.Background(), s.DB, s.Log, fixed); err != nil {
		t.Fatal(err)
	}

	get := func(path string) stats.Summary {
		rr := do(t, h, "GET", path, nil, nil)
		if rr.Code != 200 {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
		var resp struct {
			Stats stats.Summary `json:"stats"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		return resp.Stats
	}
	find := func(sum stats.Summary, dim, key string) stats.Row {
		for _, r := range sum.Breakdowns[dim] {
			if r.Key == key {
				return r
			}
		}
		return stats.Row{Key: "missing"}
	}

	// Referrer filter is visitor-scoped: every page A viewed, not only the landing page.
	sum := get("/api/v1/sites/" + site.ID + "/stats?range=7d&ref=google.com")
	if sum.Totals.Pageviews != 3 || sum.Totals.Visitors != 1 || sum.Filters["ref"] != "google.com" {
		t.Fatalf("google filter: %+v filters=%v", sum.Totals, sum.Filters)
	}
	if r := find(sum, "page", "/pricing"); r.Pageviews != 1 {
		t.Fatalf("pages for google visitors should include /pricing: %+v", sum.Breakdowns["page"])
	}
	if r := find(sum, "page", "/blog"); r.Key != "missing" {
		t.Fatalf("B's page must not appear under the google filter")
	}
	if r := find(sum, "country", "GB"); r.Visitors != 1 {
		t.Fatalf("country under filter: %+v", sum.Breakdowns["country"])
	}
	if r := find(sum, "event", "signup"); r.Pageviews != 1 {
		t.Fatalf("events under filter: %+v", sum.Breakdowns["event"])
	}
	if !sum.PreviousUnavailable {
		t.Fatalf("7d filtered with 7d retention cannot compare with the week before")
	}
	if len(sum.Series) != 7*24 || sum.Truncated {
		t.Fatalf("7d filtered series: %d points, truncated=%v", len(sum.Series), sum.Truncated)
	}
	bucket := 0
	for _, p := range sum.Series {
		if p.T == "2026-09-03T12:00:00Z" {
			bucket = p.Pageviews
		}
	}
	if bucket != 3 {
		t.Fatalf("filtered views should land in their hour: %d", bucket)
	}

	// Direct is a real value: an empty ref param filters on it.
	sum = get("/api/v1/sites/" + site.ID + "/stats?range=7d&ref=")
	if sum.Totals.Visitors != 2 {
		t.Fatalf("direct filter should include both visitors (A had direct views too): %+v", sum.Totals)
	}
	// Two filters intersect.
	sum = get("/api/v1/sites/" + site.ID + "/stats?range=7d&ref=&country=US")
	if sum.Totals.Visitors != 1 || sum.Totals.Pageviews != 1 || find(sum, "device", "Mobile").Pageviews != 1 {
		t.Fatalf("direct+US: %+v %+v", sum.Totals, sum.Breakdowns["device"])
	}
	// An event filter selects the visitors who fired it.
	sum = get("/api/v1/sites/" + site.ID + "/stats?range=7d&event=signup")
	if sum.Totals.Visitors != 1 || sum.Totals.Pageviews != 3 || find(sum, "ref", "google.com").Visitors != 1 {
		t.Fatalf("event filter: %+v %+v", sum.Totals, sum.Breakdowns["ref"])
	}
	// "Other" is never a filter; a long range past retention is flagged.
	sum = get("/api/v1/sites/" + site.ID + "/stats?range=30d&page=Other&country=GB")
	if _, ok := sum.Filters["page"]; ok {
		t.Fatalf("Other must be ignored: %v", sum.Filters)
	}
	// Seven retained days plus today.
	if !sum.Truncated || sum.RetentionDays != 7 || len(sum.Series) != 8 {
		t.Fatalf("30d with 7d retention: truncated=%v days=%d points=%d", sum.Truncated, sum.RetentionDays, len(sum.Series))
	}

	// The view-all breakdown honours filters too.
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/breakdown?dim=page&range=7d&country=US", nil, nil)
	var bd struct{ Rows []stats.Row }
	_ = json.Unmarshal(rr.Body.Bytes(), &bd)
	if rr.Code != 200 || len(bd.Rows) != 1 || bd.Rows[0].Key != "/blog" {
		t.Fatalf("filtered breakdown: %d %s", rr.Code, rr.Body)
	}
}
