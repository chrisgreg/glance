package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chrisgreg/glance/server/internal/polar"
)

const polarProduct = "prod-fplxi"

func polarOrder(id, created string, net int, extra map[string]any) map[string]any {
	o := map[string]any{
		"id": id, "created_at": created, "status": "paid", "paid": true,
		"net_amount": net, "total_amount": net, "currency": "gbp", "refunded_amount": 0,
		"product_id": polarProduct, "product": map[string]any{"id": polarProduct, "name": "Season Pass"},
		"billing_address": map[string]any{"country": "GB"},
		"metadata":        map[string]any{},
	}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

// fakePolar serves two pages of orders and rejects bad tokens.
func fakePolar(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orders/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer polar_oat_good" {
			http.Error(w, `{"error":"invalid_token"}`, 401)
			return
		}
		// The connect check asks for one order with no filter; syncs must filter.
		if r.URL.Query().Get("limit") != "1" && r.URL.Query().Get("product_id") != polarProduct {
			http.Error(w, `{"error":"missing product filter"}`, 400)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var items []map[string]any
		switch page {
		case 1:
			// 100 rows so the client asks for page 2.
			for i := 0; i < 100; i++ {
				items = append(items, polarOrder(fmt.Sprintf("old-%d", i), "2026-06-01T10:00:00Z", 100, nil))
			}
		case 2:
			items = []map[string]any{
				polarOrder("o-1", "2026-09-01T10:00:00Z", 1999, map[string]any{"metadata": map[string]any{
					"attr_ref": "https://www.google.com/", "attr_landing": "https://example.com/pricing?utm_source=newsletter&utm_campaign=launch"}}),
				polarOrder("o-2", "2026-09-02T10:00:00Z", 1999, map[string]any{"metadata": map[string]any{
					"attr_ref": "https://news.ycombinator.com/item?id=1", "attr_landing": "https://example.com/"}}),
				polarOrder("o-3", "2026-09-02T12:00:00Z", 1999, map[string]any{"status": "refunded", "refunded_amount": 1999}),
				polarOrder("o-4", "2026-09-03T12:00:00Z", 1999, map[string]any{"status": "pending", "paid": false}),
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "pagination": map[string]any{"total_count": 104, "max_page": 2}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func signPolar(secret, id, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestPolarRevenue(t *testing.T) {
	s := newServer(t, "chris", "correct-horse")
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	s.Polar.Now = s.Now
	p := fakePolar(t)
	h := s.Handler()
	admin := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "chris", "correct-horse")
	}

	rr := admin("POST", "/api/v1/sites", map[string]any{"domain": "example.com"})
	var site struct{ ID string }
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	base := "/api/v1/sites/" + site.ID

	if rr := admin("GET", base+"/polar", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"connected":false`) || !strings.Contains(rr.Body.String(), "/api/v1/polar/webhook/"+site.ID) {
		t.Fatalf("status: %d %s", rr.Code, rr.Body)
	}
	if rr := admin("GET", base+"/revenue", nil); rr.Code != 404 {
		t.Fatalf("revenue before connect: %d", rr.Code)
	}

	// A bad token is refused at connect time.
	rr = admin("PUT", base+"/polar", map[string]any{"access_token": "nope", "server": p.URL, "product_ids": polarProduct})
	if rr.Code != 422 || !strings.Contains(rr.Body.String(), "polar returned 401") {
		t.Fatalf("bad token: %d %s", rr.Code, rr.Body)
	}
	rr = admin("PUT", base+"/polar", map[string]any{"access_token": "polar_oat_good", "server": "http://insecure", "product_ids": polarProduct})
	if rr.Code != 422 {
		t.Fatalf("http server must be refused: %d %s", rr.Code, rr.Body)
	}

	rr = admin("PUT", base+"/polar", map[string]any{"access_token": "polar_oat_good", "server": p.URL, "product_ids": " " + polarProduct + " ,", "webhook_secret": "whsec_topsecret"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"connected":true`) || !strings.Contains(rr.Body.String(), `"has_webhook_secret":true`) {
		t.Fatalf("connect: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), "polar_oat_good") || strings.Contains(rr.Body.String(), "topsecret") {
		t.Fatalf("secrets leaked: %s", rr.Body)
	}
	// Sync in the request so the assertions do not race the detached pull.
	if rr := admin("POST", base+"/polar/sync", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"orders":104`) {
		t.Fatalf("sync: %d %s", rr.Code, rr.Body)
	}

	rr = admin("GET", base+"/revenue?range=7d", nil)
	var rev Revenue
	_ = json.Unmarshal(rr.Body.Bytes(), &rev)
	if rr.Code != 200 || rev.Currency != "gbp" {
		t.Fatalf("revenue: %d %s", rr.Code, rr.Body)
	}
	// Two paid orders count; the refunded one nets to zero, the pending one is excluded.
	if rev.Totals.Revenue != 3998 || rev.Totals.Orders != 3 {
		t.Fatalf("totals: %+v", rev.Totals)
	}
	if len(rev.Series) != 7*24 {
		t.Fatalf("7d is hourly: %d points", len(rev.Series))
	}
	bucket := -1
	for _, pt := range rev.Series {
		if pt.T == "2026-09-01T10:00:00Z" {
			bucket = pt.Revenue
		}
	}
	if bucket != 1999 {
		t.Fatalf("order should land in its hour bucket, got %d: %+v", bucket, rev.Series[:3])
	}
	find := func(dim, key string) int {
		for _, r := range rev.Breakdowns[dim] {
			if r.Key == key {
				return r.Revenue
			}
		}
		return -1
	}
	if find("ref", "google.com") != 1999 || find("ref", "news.ycombinator.com") != 1999 || find("source", "newsletter") != 1999 ||
		find("campaign", "launch") != 1999 || find("landing", "/pricing") != 1999 || find("country", "GB") != 3998 || find("product", "Season Pass") != 3998 {
		t.Fatalf("breakdowns: %+v", rev.Breakdowns)
	}
	if find("ref", "") != 0 {
		t.Fatalf("refunded order should show as zero, unattributed: %+v", rev.Breakdowns["ref"])
	}

	// Webhook: unsigned is refused; signed upserts, including a refund that reduces revenue.
	body, _ := json.Marshal(map[string]any{"type": "order.refunded", "data": polarOrder("o-1", "2026-09-01T10:00:00Z", 1999, map[string]any{"status": "partially_refunded", "refunded_amount": 999})})
	wh := "/api/v1/polar/webhook/" + site.ID
	if rr := do(t, h, "POST", wh, string(body), nil); rr.Code != 401 {
		t.Fatalf("unsigned webhook: %d %s", rr.Code, rr.Body)
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	hdr := map[string]string{"webhook-id": "msg_1", "webhook-timestamp": ts, "webhook-signature": "v1,bogus " + signPolar("topsecret", "msg_1", ts, body)}
	if rr := do(t, h, "POST", wh, string(body), hdr); rr.Code != 200 {
		t.Fatalf("signed webhook: %d %s", rr.Code, rr.Body)
	}
	old := map[string]string{"webhook-id": "msg_2", "webhook-timestamp": "1000", "webhook-signature": signPolar("topsecret", "msg_2", "1000", body)}
	if rr := do(t, h, "POST", wh, string(body), old); rr.Code != 401 {
		t.Fatalf("stale timestamp: %d", rr.Code)
	}
	// A foreign product's order is acknowledged and ignored.
	foreign, _ := json.Marshal(map[string]any{"type": "order.paid", "data": polarOrder("f-1", "2026-09-03T10:00:00Z", 5000, map[string]any{"product_id": "other", "product": map[string]any{"id": "other", "name": "Other"}})})
	fh := map[string]string{"webhook-id": "msg_3", "webhook-timestamp": ts, "webhook-signature": signPolar("topsecret", "msg_3", ts, foreign)}
	if rr := do(t, h, "POST", wh, string(foreign), fh); rr.Code != 200 {
		t.Fatalf("foreign webhook: %d %s", rr.Code, rr.Body)
	}
	rr = admin("GET", base+"/revenue?range=7d", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &rev)
	if rev.Totals.Revenue != 2999 || rev.Totals.Orders != 3 {
		t.Fatalf("after refund webhook: %+v", rev.Totals)
	}

	if rr := admin("DELETE", base+"/polar", nil); rr.Code != 204 {
		t.Fatalf("disconnect: %d", rr.Code)
	}
	if rr := do(t, h, "POST", wh, string(body), hdr); rr.Code != 404 {
		t.Fatalf("webhook after disconnect: %d", rr.Code)
	}
}

func TestParseOrderDefaults(t *testing.T) {
	o, ok := polar.ParseOrder(polar.RawOrder{"id": "x", "created_at": "2026-01-01T00:00:00Z", "status": "refunded", "net_amount": 500.0}, "example.com")
	if !ok || !o.Paid || o.RefundedAmount != 500 {
		t.Fatalf("refunded without refunded_amount should net to zero: %+v", o)
	}
	if _, ok := polar.ParseOrder(polar.RawOrder{"id": "x"}, ""); ok {
		t.Fatal("missing created_at must be rejected")
	}
}
