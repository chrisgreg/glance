package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrisgreg/glance/server/internal/auth"
	"github.com/chrisgreg/glance/server/internal/database"
	"github.com/chrisgreg/glance/server/internal/events"
	"github.com/chrisgreg/glance/server/internal/favicons"
	"github.com/chrisgreg/glance/server/internal/rollup"
	"github.com/chrisgreg/glance/server/internal/searchconsole"
	"github.com/chrisgreg/glance/server/internal/settings"
	"github.com/chrisgreg/glance/server/internal/sites"
	"github.com/chrisgreg/glance/server/internal/stats"
	"github.com/chrisgreg/glance/server/internal/tokens"
)

func newServer(t *testing.T, user, pass string) *Server {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Server{
		DB: db, Log: log, Sites: sites.New(db), Settings: settings.New(db), Writer: events.NewWriter(db, log),
		Stats: stats.New(db), Favicons: favicons.New(db), Admin: auth.NewAdmin(user, pass, auth.NewSessionStore(db)),
		Tokens: tokens.New(db), RetentionDays: 7,
		Google: searchconsole.NewService(searchconsole.NewStore(db), searchconsole.NewClient("", ""), log),
	}
}

func do(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "203.0.113.9:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

type httptestRecorder = httptest.ResponseRecorder

// doAs is do with HTTP Basic credentials.
func doAs(t *testing.T, h http.Handler, method, path string, body any, user, pass string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "203.0.113.9:1234"
	req.SetBasicAuth(user, pass)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

const chromeMac = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
const safariPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"

func TestAuthSplit(t *testing.T) {
	s := newServer(t, "chris", "correct-horse")
	h := s.Handler()
	if rr := do(t, h, "GET", "/api/v1/sites", nil, nil); rr.Code != 401 {
		t.Fatalf("sites without login: %d", rr.Code)
	}
	if rr := do(t, h, "POST", "/api/v1/collect", `{"s":"nope"}`, nil); rr.Code != 202 {
		t.Fatalf("collect must be public: %d", rr.Code)
	}
	if rr := do(t, h, "GET", "/glance.js", nil, nil); rr.Code != 200 || !strings.Contains(rr.Header().Get("Content-Type"), "javascript") || !strings.Contains(rr.Body.String(), "sendBeacon") {
		t.Fatalf("snippet: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	if rr := do(t, h, "OPTIONS", "/api/v1/collect", nil, nil); rr.Code != 204 || rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d", rr.Code)
	}
}

func TestIngestRollupStats(t *testing.T) {
	s := newServer(t, "", "")
	fixed := time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC)
	s.Now = func() time.Time { return fixed }
	h := s.Handler()
	ctx := context.Background()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "Example", "domain": "https://www.Example.com/"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body)
	}
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	if site.Domain != "example.com" {
		t.Fatalf("domain normalised: %q", site.Domain)
	}

	hit := func(ua, ip, url, ref, tz, name string) {
		body := map[string]any{"s": site.ID, "n": name, "u": url, "r": ref, "w": 1440, "tz": tz}
		if rr := do(t, h, "POST", "/api/v1/collect", body, map[string]string{"User-Agent": ua, "X-Forwarded-For": ip}); rr.Code != 202 {
			t.Fatalf("collect: %d", rr.Code)
		}
	}
	s.TrustProxy = true
	// Visitor A: three pageviews and an event. Visitor B: one pageview from Google on a phone.
	hit(chromeMac, "1.1.1.1", "https://example.com/", "", "Europe/London", "pageview")
	hit(chromeMac, "1.1.1.1", "https://example.com/pricing?x=1", "https://example.com/", "Europe/London", "pageview")
	hit(chromeMac, "1.1.1.1", "https://example.com/pricing", "", "Europe/London", "pageview")
	hit(chromeMac, "1.1.1.1", "https://example.com/pricing", "", "Europe/London", "signup")
	hit(safariPhone, "2.2.2.2", "https://www.example.com/blog", "https://www.google.com/", "America/New_York", "pageview")
	// Rejected: wrong host, and a bot.
	hit(chromeMac, "3.3.3.3", "https://evil.example.org/", "", "Europe/London", "pageview")
	hit("Googlebot/2.1", "4.4.4.4", "https://example.com/", "", "", "pageview")

	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	var raw int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&raw)
	if raw != 5 {
		t.Fatalf("raw events: want 5, got %d", raw)
	}
	var ipLeak int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE visitor LIKE '%1.1.1.1%'`).Scan(&ipLeak)
	if ipLeak != 0 {
		t.Fatal("IP stored in visitor column")
	}

	if err := rollup.Run(ctx, s.DB, s.Log, fixed); err != nil {
		t.Fatal(err)
	}
	if err := rollup.Run(ctx, s.DB, s.Log, fixed); err != nil { // idempotent
		t.Fatal(err)
	}

	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/stats?range=7d", nil, nil)
	if rr.Code != 200 {
		t.Fatalf("stats: %d %s", rr.Code, rr.Body)
	}
	var resp struct {
		Stats stats.Summary `json:"stats"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	st := resp.Stats
	if st.Totals.Pageviews != 4 || st.Totals.Visitors != 2 {
		t.Fatalf("totals: %+v", st.Totals)
	}
	if len(st.Series) != 7*24 {
		t.Fatalf("7d series should be hourly: %d", len(st.Series))
	}
	find := func(dim, key string) stats.Row {
		for _, r := range st.Breakdowns[dim] {
			if r.Key == key {
				return r
			}
		}
		t.Fatalf("%s/%s missing: %+v", dim, key, st.Breakdowns[dim])
		return stats.Row{}
	}
	if r := find("page", "/pricing"); r.Pageviews != 2 || r.Visitors != 1 {
		t.Fatalf("/pricing: %+v", r)
	}
	if r := find("ref", "google.com"); r.Pageviews != 1 {
		t.Fatalf("google ref: %+v", r)
	}
	if r := find("ref", ""); r.Pageviews != 3 {
		t.Fatalf("direct: %+v (same-site referrer should be direct)", r)
	}
	if r := find("country", "GB"); r.Visitors != 1 {
		t.Fatalf("GB: %+v", r)
	}
	find("country", "US")
	if r := find("device", "Mobile"); r.Pageviews != 1 {
		t.Fatalf("mobile: %+v", r)
	}
	if r := find("browser", "Chrome"); r.Pageviews != 3 {
		t.Fatalf("chrome: %+v", r)
	}
	if r := find("event", "signup"); r.Pageviews != 1 || r.Visitors != 1 {
		t.Fatalf("event: %+v", r)
	}
	if st.Breakdowns["page"][0].Key != "/pricing" {
		t.Fatalf("pages should be sorted by views: %+v", st.Breakdowns["page"])
	}

	// Index card and export.
	rr = do(t, h, "GET", "/api/v1/sites", nil, nil)
	var list struct{ Sites []siteView }
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Sites) != 1 || list.Sites[0].Card.Visitors != 2 || list.Sites[0].Card.Pageviews != 4 || len(list.Sites[0].Card.Spark) != 14 {
		t.Fatalf("card: %+v", list.Sites[0].Card)
	}
	if rr := do(t, h, "GET", "/api/v1/export", nil, nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"dim":"page"`) {
		t.Fatalf("export: %d", rr.Code)
	}
	if rr := do(t, h, "DELETE", "/api/v1/sites/"+site.ID, nil, nil); rr.Code != 204 {
		t.Fatalf("delete: %d", rr.Code)
	}
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM daily_stats`).Scan(&raw)
	if raw != 0 {
		t.Fatal("delete left rollups behind")
	}
}

func TestVisitorHashIsDayScoped(t *testing.T) {
	s := newServer(t, "", "")
	ctx := context.Background()
	d1 := time.Date(2026, 9, 3, 23, 59, 0, 0, time.UTC)
	d2 := d1.Add(2 * time.Minute)
	s1, _ := s.Settings.Salt(ctx, d1)
	s1again, _ := s.Settings.Salt(ctx, d1)
	s2, _ := s.Settings.Salt(ctx, d2)
	if s1 == "" || s1 != s1again {
		t.Fatal("salt must be stable within a day")
	}
	if s1 == s2 {
		t.Fatal("salt must rotate across the day boundary")
	}
	if events.VisitorHash(s1, "site", "1.1.1.1", "ua") == events.VisitorHash(s2, "site", "1.1.1.1", "ua") {
		t.Fatal("hash must differ across days")
	}
	if len(events.VisitorHash(s1, "site", "1.1.1.1", "ua")) != 16 {
		t.Fatal("hash length")
	}
}
