package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chrisgreg/glance/server/internal/searchconsole"
)

// fakeGoogle stands in for the OAuth and Search Console endpoints.
func fakeGoogle(t *testing.T) *httptest.Server {
	t.Helper()
	idToken := "h." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"chris@example.com"}`)) + ".s"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "good-code" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "at-1", "refresh_token": "rt-1", "id_token": idToken})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt-1" {
				http.Error(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`, 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "at-2"})
		default:
			http.Error(w, "bad grant", 400)
		}
	})
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /webmasters/v3/sites", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"siteEntry":[{"siteUrl":"https://other.example/","permissionLevel":"siteOwner"},{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"},{"siteUrl":"sc-domain:nope.example","permissionLevel":"siteUnverifiedUser"}]}`))
	})
	mux.HandleFunc("POST /webmasters/v3/sites/{property}/searchAnalytics/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-2" || r.PathValue("property") != "sc-domain:example.com" {
			http.Error(w, `{"error":{"message":"forbidden"}}`, 403)
			return
		}
		_, _ = w.Write([]byte(`{"rows":[
			{"keys":["2026-09-01","glance analytics"],"clicks":5,"impressions":40,"position":3.2},
			{"keys":["2026-09-02","glance analytics"],"clicks":7,"impressions":60,"position":2.8},
			{"keys":["2026-09-02","self hosted analytics"],"clicks":2,"impressions":90,"position":9.1}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGoogleSearchConsole(t *testing.T) {
	s := newServer(t, "chris", "correct-horse")
	s.Now = func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }
	g := fakeGoogle(t)
	client := searchconsole.NewClient("cid", "secret")
	client.AuthURL, client.TokenURL, client.RevokeURL, client.APIURL = g.URL+"/auth", g.URL+"/token", g.URL+"/revoke", g.URL
	s.Google = searchconsole.NewService(searchconsole.NewStore(s.DB), client, s.Log)
	s.Google.Now = s.Now
	h := s.Handler()
	admin := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "chris", "correct-horse")
	}

	rr := admin("POST", "/api/v1/sites", map[string]any{"domain": "example.com"})
	var site struct{ ID string }
	_ = json.Unmarshal(rr.Body.Bytes(), &site)

	// Not connected yet, but configured.
	rr = admin("GET", "/api/v1/sites/"+site.ID+"/google", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"configured":true`) || !strings.Contains(rr.Body.String(), `"connected":false`) {
		t.Fatalf("status: %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"redirect_uri":"http://example.com/api/v1/google/callback"`) {
		t.Fatalf("redirect uri: %s", rr.Body)
	}
	if rr := do(t, h, "GET", "/api/v1/sites/"+site.ID+"/google/connect", nil, nil); rr.Code != 401 {
		t.Fatalf("connect must need login: %d", rr.Code)
	}

	// Read-only API tokens may look but not connect.
	rr = admin("POST", "/api/v1/tokens", map[string]any{"name": "agent"})
	var minted struct{ Secret string }
	_ = json.Unmarshal(rr.Body.Bytes(), &minted)
	tokenHdr := map[string]string{"Authorization": "Bearer " + minted.Secret}
	if rr := do(t, h, "GET", "/api/v1/sites/"+site.ID+"/google", nil, tokenHdr); rr.Code != 200 {
		t.Fatalf("token status: %d", rr.Code)
	}
	if rr := do(t, h, "GET", "/api/v1/sites/"+site.ID+"/google/connect", nil, tokenHdr); rr.Code != 403 {
		t.Fatalf("token must not connect: %d %s", rr.Code, rr.Body)
	}

	// Connect sends the browser to Google with a one-shot state.
	rr = admin("GET", "/api/v1/sites/"+site.ID+"/google/connect", nil)
	if rr.Code != 302 {
		t.Fatalf("connect: %d %s", rr.Code, rr.Body)
	}
	consent, err := url.Parse(rr.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(consent.String(), g.URL+"/auth") {
		t.Fatalf("consent url: %s", rr.Header().Get("Location"))
	}
	q := consent.Query()
	if q.Get("client_id") != "cid" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" || !strings.Contains(q.Get("scope"), "webmasters.readonly") {
		t.Fatalf("consent params: %v", q)
	}
	state := q.Get("state")

	// Declining at Google bounces back to the site, keeping the state alive.
	rr = do(t, h, "GET", "/api/v1/google/callback?state="+state+"&error=access_denied", nil, nil)
	if rr.Code != 302 || rr.Header().Get("Location") != "/s/"+site.ID+"?google_error=access_denied" {
		t.Fatalf("declined: %d %s", rr.Code, rr.Header().Get("Location"))
	}

	// A bad code fails and bounces back to the site with the error.
	rr = do(t, h, "GET", "/api/v1/google/callback?state="+state+"&code=bad", nil, nil)
	if rr.Code != 302 || !strings.Contains(rr.Header().Get("Location"), "google_error=") {
		t.Fatalf("bad code: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	// The state was consumed; replaying it is refused.
	rr = do(t, h, "GET", "/api/v1/google/callback?state="+state+"&code=good-code", nil, nil)
	if !strings.Contains(rr.Header().Get("Location"), "expired") {
		t.Fatalf("replayed state should be stale: %s", rr.Header().Get("Location"))
	}

	// Fresh state, good code: connected, property matched to the domain.
	rr = admin("GET", "/api/v1/sites/"+site.ID+"/google/connect", nil)
	consent, _ = url.Parse(rr.Header().Get("Location"))
	rr = do(t, h, "GET", "/api/v1/google/callback?state="+consent.Query().Get("state")+"&code=good-code", nil, nil)
	if rr.Code != 302 || rr.Header().Get("Location") != "/s/"+site.ID+"?google=connected" {
		t.Fatalf("callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	rr = admin("POST", "/api/v1/sites/"+site.ID+"/google/sync", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"property":"sc-domain:example.com"`) || !strings.Contains(rr.Body.String(), `"email":"chris@example.com"`) {
		t.Fatalf("sync: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), "rt-1") {
		t.Fatalf("refresh token must never be returned: %s", rr.Body)
	}

	rr = admin("GET", "/api/v1/sites/"+site.ID+"/search-terms?range=7d", nil)
	var terms struct {
		Rows []searchconsole.Term
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &terms)
	if rr.Code != 200 || len(terms.Rows) != 2 {
		t.Fatalf("terms: %d %s", rr.Code, rr.Body)
	}
	if top := terms.Rows[0]; top.Query != "glance analytics" || top.Clicks != 12 || top.Impressions != 100 || top.Position < 2.9 || top.Position > 3.0 {
		t.Fatalf("aggregate: %+v", top)
	}

	// Switching property clears the rows until the next sync.
	rr = admin("PATCH", "/api/v1/sites/"+site.ID+"/google", map[string]string{"property": "https://other.example/"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"property":"https://other.example/"`) {
		t.Fatalf("set property: %d %s", rr.Code, rr.Body)
	}
	rr = admin("GET", "/api/v1/sites/"+site.ID+"/search-terms?range=7d", nil)
	if !strings.Contains(rr.Body.String(), `"rows":[]`) {
		t.Fatalf("rows should be cleared: %s", rr.Body)
	}

	// Disconnect forgets everything.
	if rr := admin("DELETE", "/api/v1/sites/"+site.ID+"/google", nil); rr.Code != 204 {
		t.Fatalf("disconnect: %d %s", rr.Code, rr.Body)
	}
	rr = admin("GET", "/api/v1/sites/"+site.ID+"/google", nil)
	if !strings.Contains(rr.Body.String(), `"connected":false`) {
		t.Fatalf("after disconnect: %s", rr.Body)
	}
	if rr := admin("POST", "/api/v1/sites/"+site.ID+"/google/sync", nil); rr.Code != 404 {
		t.Fatalf("sync when disconnected: %d", rr.Code)
	}
}

func TestGoogleUnconfigured(t *testing.T) {
	s := newServer(t, "", "")
	h := s.Handler()
	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"domain": "example.com"}, nil)
	var site struct{ ID string }
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	if rr := do(t, h, "GET", "/api/v1/sites/"+site.ID+"/google", nil, nil); !strings.Contains(rr.Body.String(), `"configured":false`) {
		t.Fatalf("status: %s", rr.Body)
	}
	if rr := do(t, h, "GET", "/api/v1/sites/"+site.ID+"/google/connect", nil, nil); rr.Code != 409 {
		t.Fatalf("connect without client: %d %s", rr.Code, rr.Body)
	}
}

func TestMatchProperty(t *testing.T) {
	props := []string{"https://www.example.com/", "https://example.com/", "sc-domain:example.com"}
	if got := searchconsole.MatchProperty("example.com", props); got != "sc-domain:example.com" {
		t.Fatalf("domain property first: %s", got)
	}
	if got := searchconsole.MatchProperty("example.com", props[:2]); got != "https://example.com/" {
		t.Fatalf("bare https next: %s", got)
	}
	if got := searchconsole.MatchProperty("example.com", props[:1]); got != "https://www.example.com/" {
		t.Fatalf("www fallback: %s", got)
	}
	if got := searchconsole.MatchProperty("example.com", []string{"https://other.example/"}); got != "" {
		t.Fatalf("no match: %s", got)
	}
}
