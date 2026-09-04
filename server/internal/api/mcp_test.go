package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chrisgreg/glance/server/internal/polar"
	"github.com/chrisgreg/glance/server/internal/rollup"
)

type authed struct {
	token string
	base  http.RoundTripper
}

func (a authed) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(r)
}

func TestMCPEndToEnd(t *testing.T) {
	s := newServer(t, "chris", "correct-horse")
	s.MCPToken = "sixteen-char-token-ok"
	s.TrustProxy = true
	fixed := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return fixed }
	h := s.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Seed one site with traffic through the admin API + collect, then roll up.
	req := httptest.NewRequest("POST", "/api/v1/sites", strings.NewReader(`{"name":"Uini","domain":"uini.io"}`))
	req.SetBasicAuth("chris", "correct-horse")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	for i := 0; i < 6; i++ {
		body := `{"s":"` + site.ID + `","n":"pageview","u":"https://uini.io/docs","r":"https://www.google.com/","w":1440,"tz":"Europe/London"}`
		do(t, h, "POST", "/api/v1/collect", body, map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "1.1.1." + string(rune('1'+i))})
	}
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := rollup.Run(context.Background(), s.DB, s.Log, fixed); err != nil {
		t.Fatal(err)
	}

	// Wrong token is refused; no token falls back to the admin login (absent here).
	if rr := do(t, h, "POST", "/mcp", `{}`, map[string]string{"Authorization": "Bearer wrong"}); rr.Code != 401 {
		t.Fatalf("bad token: %d", rr.Code)
	}
	if rr := do(t, h, "POST", "/mcp", `{}`, nil); rr.Code != 401 {
		t.Fatalf("no auth: %d", rr.Code)
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	transport := &sdk.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: authed{token: s.MCPToken, base: http.DefaultTransport}}}
	sess, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"list_sites", "overview", "site_stats", "breakdown", "search_terms", "revenue"} {
		if !names[want] {
			t.Fatalf("tool %s missing: %v", want, names)
		}
	}

	res, err := sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "overview", Arguments: map[string]any{"range": "week"}})
	if err != nil || res.IsError {
		t.Fatalf("overview: %v %+v", err, res)
	}
	out, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(out), `"visitors":6`) || !strings.Contains(string(out), `"domain":"uini.io"`) || !strings.Contains(string(out), `"top_referrer":{"key":"google.com"`) {
		t.Fatalf("overview payload: %s", out)
	}
	res, err = sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "breakdown", Arguments: map[string]any{"site": "uini", "dim": "pages"}})
	if err != nil || res.IsError {
		t.Fatalf("breakdown: %v %+v", err, res)
	}
	out, _ = json.Marshal(res.StructuredContent)
	if !strings.Contains(string(out), `"key":"/docs"`) {
		t.Fatalf("breakdown payload: %s", out)
	}
	res, _ = sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "site_stats", Arguments: map[string]any{"site": "nope.example"}})
	if res == nil || !res.IsError {
		t.Fatal("unknown site should be a tool error")
	}

	// Filters narrow site_stats to matching visitors and are echoed back; with
	// 7-day retention the previous week is unavailable, so no delta is invented.
	res, err = sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "site_stats", Arguments: map[string]any{"site": "uini", "filters": map[string]string{"ref": "google.com"}}})
	if err != nil || res.IsError {
		t.Fatalf("filtered site_stats: %v %+v", err, res)
	}
	out, _ = json.Marshal(res.StructuredContent)
	if !strings.Contains(string(out), `"filters":{"ref":"google.com"}`) || !strings.Contains(string(out), `"previous_unavailable":true`) || !strings.Contains(string(out), `"visitors_delta_pct":null`) {
		t.Fatalf("filtered payload: %s", out)
	}
	res, err = sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "site_stats", Arguments: map[string]any{"site": "uini", "filters": map[string]string{"ref": "bing.com"}}})
	if err != nil || res.IsError {
		t.Fatalf("filtered site_stats: %v %+v", err, res)
	}
	out, _ = json.Marshal(res.StructuredContent)
	if !strings.Contains(string(out), `"totals":{"pageviews":0,"visitors":0}`) {
		t.Fatalf("bing filter should match nobody: %s", out)
	}

	// Revenue: not connected reads as such; once orders exist, attribution is counted.
	res, err = sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "revenue", Arguments: map[string]any{"site": "uini"}})
	if err != nil || res.IsError {
		t.Fatalf("revenue: %v %+v", err, res)
	}
	out, _ = json.Marshal(res.StructuredContent)
	if !strings.Contains(string(out), `"connected":false`) {
		t.Fatalf("revenue before connect: %s", out)
	}
	ctx := context.Background()
	if err := s.Polar.Store.Save(ctx, polar.Connection{SiteID: site.ID, AccessToken: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Polar.Store.UpsertOrders(ctx, site.ID, []polar.Order{
		{OrderID: "a", CreatedAt: fixed.Add(-time.Hour), Status: "paid", Paid: true, NetAmount: 1000, Currency: "gbp", Ref: "google.com", Landing: "/docs"},
		{OrderID: "b", CreatedAt: fixed.Add(-2 * time.Hour), Status: "paid", Paid: true, NetAmount: 500, Currency: "gbp"},
	}); err != nil {
		t.Fatal(err)
	}
	res, err = sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "revenue", Arguments: map[string]any{"site": "uini", "range": "7d"}})
	if err != nil || res.IsError {
		t.Fatalf("revenue: %v %+v", err, res)
	}
	out, _ = json.Marshal(res.StructuredContent)
	for _, want := range []string{`"connected":true`, `"currency":"gbp"`, `"totals":{"orders":2,"revenue":1500}`, `"visitors":6`, `"revenue_per_visitor":250`, `"attributed_orders":1`, `"unattributed_orders":1`} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("revenue payload missing %s: %s", want, out)
		}
	}
}
